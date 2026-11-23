//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

//go:build ee

package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"ragflow/internal/agentic_rag"
)

// fakeKV is a minimal in-process KV for tests. The whole point of the KV interface is
// that the store is swappable, so tests need no Kvrocks.
type fakeKV struct {
	mu   sync.Mutex
	data map[string]fakeEntry
}

type fakeEntry struct {
	value   string
	expires time.Time // zero => no expiry
}

func newFakeKV() *fakeKV { return &fakeKV{data: map[string]fakeEntry{}} }

func (k *fakeKV) live(key string, now time.Time) (fakeEntry, bool) {
	e, ok := k.data[key]
	if !ok {
		return fakeEntry{}, false
	}
	if !e.expires.IsZero() && now.After(e.expires) {
		delete(k.data, key)
		return fakeEntry{}, false
	}
	return e, true
}

func (k *fakeKV) Get(_ context.Context, key string) (string, bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	e, ok := k.live(key, time.Now())
	if !ok {
		return "", false, nil
	}
	return e.value, true, nil
}

func (k *fakeKV) Set(_ context.Context, key, value string, ttl time.Duration) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.data[key] = newFakeEntry(value, ttl)
	return nil
}

func (k *fakeKV) SetNX(_ context.Context, key, value string, ttl time.Duration) (bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, ok := k.live(key, time.Now()); ok {
		return false, nil
	}
	k.data[key] = newFakeEntry(value, ttl)
	return true, nil
}

func (k *fakeKV) Del(_ context.Context, keys ...string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, key := range keys {
		delete(k.data, key)
	}
	return nil
}

func (k *fakeKV) Expire(_ context.Context, key string, ttl time.Duration) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	e, ok := k.data[key]
	if !ok {
		return nil
	}
	e.expires = time.Now().Add(ttl)
	k.data[key] = e
	return nil
}

func newFakeEntry(value string, ttl time.Duration) fakeEntry {
	e := fakeEntry{value: value}
	if ttl > 0 {
		e.expires = time.Now().Add(ttl)
	}
	return e
}

func (k *fakeKV) hasTTL(key string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	e, ok := k.data[key]
	return ok && !e.expires.IsZero()
}

func (k *fakeKV) expiry(key string) (time.Time, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	e, ok := k.data[key]
	return e.expires, ok
}

func TestCodexHandleStablePerSession(t *testing.T) {
	kv := newFakeKV()
	tickets := NewCodexTickets(kv)
	ctx := context.Background()

	h1, err := tickets.HandleForSession(ctx, "tenant-a", "sess-1")
	if err != nil || h1 == "" {
		t.Fatalf("HandleForSession: %q, %v", h1, err)
	}
	h2, err := tickets.HandleForSession(ctx, "tenant-a", "sess-1")
	if err != nil || h2 != h1 {
		t.Fatalf("handle not stable per session: %q vs %q (%v)", h1, h2, err)
	}
	h3, _ := tickets.HandleForSession(ctx, "tenant-a", "sess-2")
	if h3 == h1 {
		t.Fatal("distinct sessions share a handle")
	}
	// The handle alone carries no authority.
	if _, ok := tickets.Resolve(ctx, h1); ok {
		t.Fatal("an unbound handle resolved a scope")
	}
	if alive, _ := tickets.HandleAlive(ctx, h1); !alive {
		t.Fatal("fresh handle is not alive")
	}
	// Handles and bindings must carry a TTL in KV (no eternal keys).
	if !kv.hasTTL(codexHandleKey(h1)) {
		t.Error("handle key has no TTL")
	}
}

func TestCodexBindingIsPerTurn(t *testing.T) {
	kv := newFakeKV()
	tickets := NewCodexTickets(kv)
	ctx := context.Background()
	handle, _ := tickets.HandleForSession(ctx, "t1", "s1")

	scope := CodexScope{TenantID: "t1", DatasetIDs: []string{"kb1", "kb2"}, UserID: "u1"}
	if err := tickets.Bind(ctx, handle, scope); err != nil {
		t.Fatalf("Bind refused a live handle: %v", err)
	}
	// The binding is the turn lock: a second Bind while it is held is refused as busy.
	if err := tickets.Bind(ctx, handle, scope); !errors.Is(err, ErrCodexTurnBusy) {
		t.Fatalf("second Bind = %v, want ErrCodexTurnBusy", err)
	}
	if !kv.hasTTL(codexBindKey(handle)) {
		t.Error("binding key has no TTL")
	}
	got, ok := tickets.Resolve(ctx, handle)
	if !ok || got.TenantID != "t1" || len(got.DatasetIDs) != 2 || got.UserID != "u1" {
		t.Fatalf("Resolve = %+v, %v", got, ok)
	}

	// The turn ends: the binding goes, the handle stays (reused by later turns) and the
	// lock is free again.
	tickets.Unbind(ctx, handle)
	if _, ok := tickets.Resolve(ctx, handle); ok {
		t.Fatal("unbound handle still resolved a scope")
	}
	if alive, _ := tickets.HandleAlive(ctx, handle); !alive {
		t.Fatal("Unbind must leave the handle alive")
	}
	if err := tickets.Bind(ctx, handle, scope); err != nil {
		t.Fatalf("re-Bind after Unbind: %v", err)
	}
	tickets.Unbind(ctx, handle)

	if err := tickets.Bind(ctx, "", scope); err == nil {
		t.Fatal("Bind accepted an empty handle")
	}
	if err := tickets.Bind(ctx, "never-minted", scope); err == nil {
		t.Fatal("Bind accepted an unknown handle")
	}
}

func TestCodexRevokeHandleClearsSessionAndBinding(t *testing.T) {
	kv := newFakeKV()
	tickets := NewCodexTickets(kv)
	ctx := context.Background()
	handle, _ := tickets.HandleForSession(ctx, "t1", "s1")
	_ = tickets.Bind(ctx, handle, CodexScope{TenantID: "t1", DatasetIDs: []string{"kb"}})

	tickets.RevokeHandle(ctx, handle)
	if alive, _ := tickets.HandleAlive(ctx, handle); alive {
		t.Fatal("revoked handle is still alive")
	}
	if _, ok := tickets.Resolve(ctx, handle); ok {
		t.Fatal("revoked handle still resolved a scope")
	}
	// The stale session index is gone: the next turn gets a fresh handle.
	again, _ := tickets.HandleForSession(ctx, "t1", "s1")
	if again == handle {
		t.Fatal("session reused a revoked handle")
	}
}

func TestCodexOneShotHandleIsFresh(t *testing.T) {
	kv := newFakeKV()
	tickets := NewCodexTickets(kv)
	ctx := context.Background()
	h1, _ := tickets.HandleForSession(ctx, "t1", "")
	h2, _ := tickets.HandleForSession(ctx, "t1", "")
	if h1 == "" || h2 == "" || h1 == h2 {
		t.Fatalf("one-shot handles must be fresh: %q, %q", h1, h2)
	}
}

// TestCodexBindKeepsHandleAlive pins that Bind refreshes the handle's TTL, so a binding
// can never outlive the handle it authorizes.
func TestCodexBindKeepsHandleAlive(t *testing.T) {
	kv := newFakeKV()
	tickets := NewCodexTickets(kv)
	ctx := context.Background()
	handle, _ := tickets.HandleForSession(ctx, "t1", "s1")

	// Simulate a handle close to expiry, then bind.
	if err := kv.Expire(ctx, codexHandleKey(handle), 2*time.Second); err != nil {
		t.Fatalf("Expire: %v", err)
	}
	short, _ := kv.expiry(codexHandleKey(handle))
	if err := tickets.Bind(ctx, handle, CodexScope{TenantID: "t1"}); err != nil {
		t.Fatalf("Bind refused a live handle: %v", err)
	}
	refreshed, _ := kv.expiry(codexHandleKey(handle))
	// Bind refreshes the handle back to its configured TTL (10m) — clearly past the
	// near-expiry it had, but not unbounded.
	if d := time.Until(refreshed); d < 9*time.Minute || d > 11*time.Minute {
		t.Fatalf("Bind did not refresh the handle to its TTL: %v (from %v)", refreshed, short)
	}
}

// TestCodexResolveUsesBindingUntilUnbind pins the contract: Resolve just reads the live
// binding — it does not re-check the handle. A binding's life is Bind/Unbind plus the TTL.
func TestCodexResolveUsesBindingUntilUnbind(t *testing.T) {
	kv := newFakeKV()
	tickets := NewCodexTickets(kv)
	ctx := context.Background()
	handle, _ := tickets.HandleForSession(ctx, "t1", "s1")
	_ = tickets.Bind(ctx, handle, CodexScope{TenantID: "t1", DatasetIDs: []string{"kb"}})

	if got, ok := tickets.Resolve(ctx, handle); !ok || got.TenantID != "t1" {
		t.Fatalf("Resolve = %+v, %v", got, ok)
	}
	tickets.Unbind(ctx, handle)
	if _, ok := tickets.Resolve(ctx, handle); ok {
		t.Fatal("an unbound handle still resolved a scope")
	}
}

// connectSession wires an in-memory MCP client to the given server.
func connectSession(t *testing.T, server *sdk.Server) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	client := sdk.NewClient(&sdk.Implementation{Name: "codex-test", Version: "0"}, nil)
	st, ct := sdk.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	clientSession, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
	})
	return clientSession
}

func TestCodexToolServerListsExpectedTools(t *testing.T) {
	cs := connectSession(t, NewCodexToolServer(CodexScope{TenantID: "t", DatasetIDs: []string{"kb"}}))
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := make(map[string]bool, len(res.Tools))
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"search_semantic_chunks", "search_bm25_chunks", "grep_chunks", "list_chunks"} {
		if !names[want] {
			t.Errorf("tool %q not exposed", want)
		}
	}
	if names["search_chunks"] {
		t.Error("search_chunks must not be exposed (mode 8 D9)")
	}
}

func TestCodexToolServerRejectsNoScope(t *testing.T) {
	// A conversation with no bound KB: retrieval must fail explicitly, never search an
	// empty/default scope (mode 8 D8).
	cs := connectSession(t, NewCodexToolServer(CodexScope{TenantID: "t"}))
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      "list_chunks",
		Arguments: map[string]any{"doc_id": "d1", "anchor_chunk_ids": []string{"c1"}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError with no KB scope, got %+v", res)
	}
	text := ""
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			text += tc.Text
		}
	}
	if !strings.Contains(text, "no knowledge base") {
		t.Fatalf("error text does not explain the missing scope: %q", text)
	}
}

// web_search is a per-turn capability: a turn that did not enable it must not advertise
// the tool, and the resolver must not even be consulted.
func TestCodexToolServerOmitsWebSearchWithoutOptIn(t *testing.T) {
	var calls int
	SetCodexWebSearchResolver(func(context.Context, CodexScope) agentic_rag.WebSearchFunc {
		calls++
		return func(context.Context, string) ([]agentic_rag.WebResult, error) {
			return []agentic_rag.WebResult{{Title: "T", URL: "https://e/x", Content: "C"}}, nil
		}
	})
	t.Cleanup(func() { SetCodexWebSearchResolver(nil) })

	scope := CodexScope{TenantID: "t", ChatID: "c1", DatasetIDs: []string{"kb"}}
	scope.webSearch = resolveCodexWebSearch(context.Background(), scope)
	cs := connectSession(t, NewCodexToolServer(scope))

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range res.Tools {
		if tool.Name == agentic_rag.WebSearchToolName {
			t.Fatal("web_search advertised for a turn that did not enable it")
		}
	}
	if calls != 0 {
		t.Fatalf("resolver consulted %d times for a turn that did not opt in; want 0", calls)
	}
}

// A turn whose conversation enables web search advertises web_search, and the tool is
// usable even though the conversation binds no knowledge base: web search is a
// capability of the conversation, not of the corpus, so the KB guard must not apply.
func TestCodexToolServerAddsWebSearchWhenTurnEnablesIt(t *testing.T) {
	const query = "public keywords"
	var gotQuery string
	SetCodexWebSearchResolver(func(_ context.Context, scope CodexScope) agentic_rag.WebSearchFunc {
		if scope.ChatID != "c1" {
			t.Errorf("resolver scope chat id = %q, want c1", scope.ChatID)
		}
		return func(_ context.Context, q string) ([]agentic_rag.WebResult, error) {
			gotQuery = q
			return []agentic_rag.WebResult{{Title: "Public source", URL: "https://e/x", Content: "Public facts."}}, nil
		}
	})
	t.Cleanup(func() { SetCodexWebSearchResolver(nil) })

	scope := CodexScope{TenantID: "t", UserID: "u", ChatID: "c1", WebSearch: true}
	scope.webSearch = resolveCodexWebSearch(context.Background(), scope)
	cs := connectSession(t, NewCodexToolServer(scope))

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	found := false
	for _, tool := range res.Tools {
		found = found || tool.Name == agentic_rag.WebSearchToolName
	}
	if !found {
		t.Fatal("web_search not advertised for a turn that enabled it")
	}

	call, err := cs.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      agentic_rag.WebSearchToolName,
		Arguments: map[string]any{"queries": []string{query}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if call.IsError {
		t.Fatalf("web_search failed with no KB scope: %+v", call)
	}
	if gotQuery != query {
		t.Fatalf("provider query = %q, want %q", gotQuery, query)
	}
}

func TestCodexHandlerRejectsUnknownHandle(t *testing.T) {
	h := NewCodexHandler(NewCodexTickets(newFakeKV()))
	req := httptest.NewRequest(http.MethodPost, "/mcp/codex/unknown-token", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unknown handle: status = %d, want 401", w.Code)
	}
}

func TestCodexHandlerRejectsHandleWithNoActiveTurn(t *testing.T) {
	// A known handle with no live binding (between turns) is forbidden: this is the
	// per-turn guarantee that makes a leaked or replayed handle useless.
	kv := newFakeKV()
	tickets := NewCodexTickets(kv)
	handle, _ := tickets.HandleForSession(context.Background(), "t1", "s1")

	h := NewCodexHandler(tickets)
	req := httptest.NewRequest(http.MethodPost, "/mcp/codex/"+handle, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("unbound handle: status = %d, want 403", w.Code)
	}
}

// TestCodexRenewExtendsHandleAndBindingLease pins the running turn's heartbeat: Renew
// extends both the handle TTL and the binding lease, and leaves a released binding alone
// (it must not resurrect one).
func TestCodexRenewExtendsHandleAndBindingLease(t *testing.T) {
	kv := newFakeKV()
	tickets := NewCodexTickets(kv)
	ctx := context.Background()
	handle, _ := tickets.HandleForSession(ctx, "t1", "s1")
	_ = tickets.Bind(ctx, handle, CodexScope{TenantID: "t1"})

	for _, key := range []string{codexHandleKey(handle), codexBindKey(handle)} {
		_ = kv.Expire(ctx, key, time.Second)
	}
	if !tickets.Renew(ctx, handle) {
		t.Fatal("Renew returned false for a live handle")
	}
	for _, key := range []string{codexHandleKey(handle), codexBindKey(handle)} {
		if exp, _ := kv.expiry(key); !exp.After(time.Now().Add(time.Minute)) {
			t.Fatalf("key %q not renewed: %v", key, exp)
		}
	}

	// A released binding must not be brought back by the heartbeat.
	tickets.Unbind(ctx, handle)
	_ = tickets.Renew(ctx, handle)
	if _, ok, _ := kv.Get(ctx, codexBindKey(handle)); ok {
		t.Fatal("Renew resurrected a released binding")
	}
}
