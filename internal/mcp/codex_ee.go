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
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"ragflow/internal/agentic_rag"
	"ragflow/internal/permission"
)

// CodexScope is the tenant + dataset scope a mode 8 turn authorizes. It is the ONLY
// authority for what the bridge may retrieve: tool arguments cannot widen it.
type CodexScope struct {
	TenantID   string   `json:"tenant_id"`
	DatasetIDs []string `json:"dataset_ids"`
	// UserID is the mode 8 turn's acting user. The retrieval tools carry it into
	// their context so the dataset permission check authorizes the same caller who
	// opened the turn (an MCP request has no canvas state to read it from).
	UserID string `json:"user_id"`
	// ChatID is the conversation this turn belongs to. The bridge resolves the
	// conversation's web-search provider from it at request time, so the provider's
	// credentials stay in the database instead of in this binding — which is
	// serialized into the shared KV (see D11).
	ChatID string `json:"chat_id"`
	// WebSearch is this turn's web-search decision: the conversation has a provider
	// AND the request enabled internet (see shouldUseWebSearch). The bridge advertises
	// web_search only when it is set AND a provider actually resolves.
	WebSearch bool `json:"web_search"`
	// webSearch is the resolved callback for THIS process, filled in by the HTTP
	// handler after the binding is read. Unexported — and a function — so it is never
	// part of the KV-serialized binding.
	webSearch agentic_rag.WebSearchFunc
}

// KV is the minimal shared store mode 8 authorization needs. It MUST be backed by a
// store visible to every process that serves the bridge (internal/engine/kvrocks), not
// process memory: the API process mints handles and binds scopes while the process
// serving /mcp/codex resolves them — these may differ (separate MCP server, or multiple
// API replicas).
type KV interface {
	// Get returns the value and whether the key exists.
	Get(ctx context.Context, key string) (string, bool, error)
	// Set writes value with a TTL (ttl <= 0 means no expiry).
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	// SetNX writes value only when the key is absent, reporting whether it wrote.
	SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	// Del removes keys (a missing key is not an error).
	Del(ctx context.Context, keys ...string) error
	// Expire refreshes a key's TTL.
	Expire(ctx context.Context, key string, ttl time.Duration) error
}

// Key layout. The handle is authority-free: it identifies a session, not a scope.
const (
	codexKeyPrefix  = "ragflow:codex:"
	codexHandleKeyF = codexKeyPrefix + "handle:" // -> session key
	codexSessKeyF   = codexKeyPrefix + "sess:"   // session key -> handle
	codexBindKeyF   = codexKeyPrefix + "bind:"   // -> JSON CodexScope (the live turn binding)
)

const (
	// defaultCodexHandleTTL is how long a mode 8 KV entry lives without renewal. The handle
	// is renewed by Bind, HandleForSession, and the running turn's Renew heartbeat, so this
	// bounds the window after the last renewal — a stopped server's leftovers expire within
	// it. The handle is authority-free, but a short TTL still bounds a leaked one.
	defaultCodexHandleTTL = 10 * time.Minute
	// defaultCodexBindTTL bounds a binding's idle life (the turn lease). It only has to
	// EXCEED the binder's renewal cadence so the heartbeat refreshes it before it lapses —
	// it is unrelated to the turn's wall-clock budget: a turn far longer than this keeps
	// its authorization via the heartbeat, while an abandoned turn's lease expires and
	// frees the session. Long enough to survive a hiccup, short enough to reclaim a stuck
	// session promptly.
	defaultCodexBindTTL = 10 * time.Minute
)

// ErrCodexTurnBusy is returned by Bind when the handle already holds a live binding: the
// binding IS the per-session turn lock, so a second concurrent turn must not proceed.
var ErrCodexTurnBusy = errors.New("codex: another turn is already running for this session")

// CodexTickets is the KV-backed mode 8 authorization store. It separates two things:
//
//   - a HANDLE: an unguessable, stable token minted per (tenant, session) and baked into
//     the Codex thread's MCP URL. It carries NO authority — on its own it grants nothing.
//   - a BINDING: the CodexScope a turn installs on its handle when the turn starts and
//     removes when it ends.
//
// The bridge authorizes a request only while a binding is live, so authorization is
// per-TURN even though the URL (and its handle) is fixed at thread creation. A handle
// that leaks or is replayed outside its turn is useless. Bind is not an HTTP route, so a
// Codex peer cannot alter or widen a binding; tool arguments can only narrow the scope.
type CodexTickets struct {
	kv        KV
	handleTTL time.Duration
	bindTTL   time.Duration
}

// NewCodexTickets returns a KV-backed store. A nil kv makes every operation deny, so a
// misconfigured server fails closed rather than acting on an empty scope.
func NewCodexTickets(kv KV) *CodexTickets {
	return &CodexTickets{kv: kv, handleTTL: defaultCodexHandleTTL, bindTTL: defaultCodexBindTTL}
}

func codexHandleKey(token string) string { return codexHandleKeyF + token }
func codexBindKey(token string) string   { return codexBindKeyF + token }

// codexSessionKey is the session index key. '\x00' cannot appear in either id, so the
// concatenation is unambiguous.
func codexSessionKey(tenantID, sessionID string) string {
	return codexSessKeyF + tenantID + "\x00" + sessionID
}

// HandleForSession returns the stable handle for (tenantID, sessionID), minting one on
// first use and refreshing its expiry. An empty sessionID yields a fresh, never-reused
// handle for a one-shot turn (there is no session to share it with).
func (t *CodexTickets) HandleForSession(ctx context.Context, tenantID, sessionID string) (string, error) {
	if t == nil || t.kv == nil {
		return "", fmt.Errorf("codex: authorization store is not configured")
	}
	if sessionID == "" {
		token, err := newCodexToken()
		if err != nil {
			return "", err
		}
		if err := t.kv.Set(ctx, codexHandleKey(token), "", t.handleTTL); err != nil {
			return "", err
		}
		return token, nil
	}

	sessKey := codexSessionKey(tenantID, sessionID)
	if token, ok, err := t.kv.Get(ctx, sessKey); err != nil {
		return "", err
	} else if ok && token != "" {
		if alive, _ := t.HandleAlive(ctx, token); alive {
			// Slide both TTLs so an actively used session keeps its handle (and its
			// baked-in thread URL) valid.
			_ = t.kv.Expire(ctx, sessKey, t.handleTTL)
			_ = t.kv.Expire(ctx, codexHandleKey(token), t.handleTTL)
			return token, nil
		}
		// Stale index (handle expired/revoked): drop it and mint afresh.
		_ = t.kv.Del(ctx, sessKey)
	}

	token, err := newCodexToken()
	if err != nil {
		return "", err
	}
	// Idempotent mint across processes: the loser of the race adopts the winner's token.
	won, err := t.kv.SetNX(ctx, sessKey, token, t.handleTTL)
	if err != nil {
		return "", err
	}
	if !won {
		if existing, ok, err := t.kv.Get(ctx, sessKey); err == nil && ok && existing != "" {
			return existing, nil
		}
		return "", fmt.Errorf("codex: session handle race for %s", tenantID)
	}
	if err := t.kv.Set(ctx, codexHandleKey(token), sessKey, t.handleTTL); err != nil {
		_ = t.kv.Del(ctx, sessKey)
		return "", err
	}
	return token, nil
}

// newCodexToken returns a fresh 32-byte base64url token.
func newCodexToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// RevokeHandle invalidates a handle, its session index, and any live binding.
func (t *CodexTickets) RevokeHandle(ctx context.Context, token string) {
	if t == nil || t.kv == nil || token == "" {
		return
	}
	sessKey, ok, _ := t.kv.Get(ctx, codexHandleKey(token))
	_ = t.kv.Del(ctx, codexBindKey(token), codexHandleKey(token))
	if ok && sessKey != "" {
		_ = t.kv.Del(ctx, sessKey)
	}
}

// HandleAlive reports whether token is a known, unexpired handle.
func (t *CodexTickets) HandleAlive(ctx context.Context, token string) (bool, error) {
	if t == nil || t.kv == nil || token == "" {
		return false, nil
	}
	_, ok, err := t.kv.Get(ctx, codexHandleKey(token))
	return ok, err
}

// Bind atomically ACQUIRES the per-turn binding for an alive handle, or fails with
// ErrCodexTurnBusy when one is already held. The binding doubles as the per-session turn
// lock — a single KV value behind an atomic SetNX — so mutual exclusion holds across
// processes and replicas, not just within one. An unknown or expired handle is reported as
// its own error; callers should distinguish ErrCodexTurnBusy from that failure.
func (t *CodexTickets) Bind(ctx context.Context, token string, scope CodexScope) error {
	if t == nil || t.kv == nil || token == "" {
		return fmt.Errorf("codex: authorization store is not configured")
	}
	if alive, err := t.HandleAlive(ctx, token); err != nil {
		return err
	} else if !alive {
		return fmt.Errorf("codex: unknown or expired handle")
	}
	payload, err := json.Marshal(scope)
	if err != nil {
		return err
	}
	won, err := t.kv.SetNX(ctx, codexBindKey(token), string(payload), t.bindTTL)
	if err != nil {
		return err
	}
	if !won {
		return ErrCodexTurnBusy
	}
	// Keep the handle alive for the whole turn: refreshing its TTL here means a binding
	// can never outlive the handle it authorizes.
	_ = t.kv.Expire(ctx, codexHandleKey(token), t.handleTTL)
	return nil
}

// Renew extends the handle's TTL and the turn's binding lease. The running turn calls it on
// a timer (CodexRenewInterval) so neither the handle nor the binding lapses mid-turn.
// Nothing renews them between turns, so a handle lives at most handleTTL past its last
// turn. It returns false when the handle is no longer alive.
func (t *CodexTickets) Renew(ctx context.Context, token string) bool {
	if t == nil || t.kv == nil || token == "" {
		return false
	}
	alive, err := t.HandleAlive(ctx, token)
	if err != nil || !alive {
		return false
	}
	_ = t.kv.Expire(ctx, codexHandleKey(token), t.handleTTL)
	// Extend the lease only if one is held: a binding that was already released (or never
	// acquired) must not be resurrected by the heartbeat.
	if _, ok, _ := t.kv.Get(ctx, codexBindKey(token)); ok {
		_ = t.kv.Expire(ctx, codexBindKey(token), t.bindTTL)
	}
	return true
}

// Unbind removes the live binding (turn end), leaving the handle alive.
func (t *CodexTickets) Unbind(ctx context.Context, token string) {
	if t == nil || t.kv == nil || token == "" {
		return
	}
	_ = t.kv.Del(ctx, codexBindKey(token))
}

// Resolve returns the LIVE BINDING for a token (the current turn's scope), refreshing its
// idle expiry. It returns false when no turn is bound — the handle alone grants nothing,
// which is what makes authorization per-turn.
//
// It deliberately does NOT re-check the handle here: a binding's life is governed by the
// Bind/Unbind turn lifecycle with the TTL as backstop, and Bind keeps the handle alive for
// the turn, so a bound handle cannot have been evicted under it. This keeps Resolve a
// single read on the hot tool-call path.
func (t *CodexTickets) Resolve(ctx context.Context, token string) (CodexScope, bool) {
	if t == nil || t.kv == nil || token == "" {
		return CodexScope{}, false
	}
	payload, ok, err := t.kv.Get(ctx, codexBindKey(token))
	if err != nil || !ok || payload == "" {
		return CodexScope{}, false
	}
	var scope CodexScope
	if json.Unmarshal([]byte(payload), &scope) != nil {
		return CodexScope{}, false
	}
	_ = t.kv.Expire(ctx, codexBindKey(token), t.bindTTL)
	return scope, true
}

// defaultCodexTickets is the process-wide store: the HTTP bridge resolves against it and
// the mode 8 engine binds into it. It is installed once at boot with the shared KV
// (SetCodexKV); until then every operation denies (fail closed). Tests construct their
// own store with a fake KV and pass it explicitly.
var (
	codexTicketsMu      sync.RWMutex
	defaultCodexTickets *CodexTickets
)

// SetCodexKV installs the KV backing the process-wide handle/binding store. Call it once
// at boot, after the Kvrocks client is up.
func SetCodexKV(kv KV) {
	codexTicketsMu.Lock()
	defaultCodexTickets = NewCodexTickets(kv)
	codexTicketsMu.Unlock()
}

// DefaultCodexTickets returns the process-wide store (never nil; denies until SetCodexKV).
func DefaultCodexTickets() *CodexTickets {
	codexTicketsMu.RLock()
	defer codexTicketsMu.RUnlock()
	if defaultCodexTickets == nil {
		return NewCodexTickets(nil)
	}
	return defaultCodexTickets
}

// WebSearchResolver resolves the conversation's web-search callback for a bound turn.
// It is a seam: the bridge must not import the service layer (the service layer imports
// it), so the API process installs a resolver at boot. A nil resolver, a nil return, or
// scope.WebSearch == false all mean "this turn gets no web_search tool".
//
// The callback (and the provider credentials behind it) is never persisted: the binding
// carries only the conversation id, and the resolver rebuilds the callback from it.
type WebSearchResolver func(ctx context.Context, scope CodexScope) agentic_rag.WebSearchFunc

var (
	codexWebSearchMu sync.RWMutex
	codexWebSearch   WebSearchResolver
)

// SetCodexWebSearchResolver installs the process-wide resolver. Call it once at boot.
func SetCodexWebSearchResolver(r WebSearchResolver) {
	codexWebSearchMu.Lock()
	codexWebSearch = r
	codexWebSearchMu.Unlock()
}

// resolveCodexWebSearch resolves this turn's web-search callback, or nil when the turn
// did not opt in or no resolver/provider is available.
func resolveCodexWebSearch(ctx context.Context, scope CodexScope) agentic_rag.WebSearchFunc {
	if !scope.WebSearch {
		return nil
	}
	codexWebSearchMu.RLock()
	r := codexWebSearch
	codexWebSearchMu.RUnlock()
	if r == nil {
		return nil
	}
	return r(ctx, scope)
}

// codexToolSpec is one tool the bridge exposes, plus how the MCP layer must police it.
type codexToolSpec struct {
	tool einotool.BaseTool
	// requiresKBs gates the call on the turn's knowledge-base scope. The retrieval
	// tools need it — an empty scope must never degrade into an unrestricted search.
	// web_search is a capability of the conversation, not of the corpus, so it does not.
	requiresKBs bool
}

// codexToolSpecs are the tools the Codex bridge exposes for one turn's binding. The
// names and schemas come from the tools themselves (see addCodexTool), so the two never
// drift. search_chunks is intentionally NOT here (see the mode 8 plan, D9); web_search
// is present only when the conversation enables it (scope.webSearch).
func codexToolSpecs(scope CodexScope) []codexToolSpec {
	specs := []codexToolSpec{
		{tool: agentic_rag.NewSearchSemanticChunksTool(scope.TenantID, scope.DatasetIDs), requiresKBs: true},
		{tool: agentic_rag.NewSearchBm25ChunksTool(scope.TenantID, scope.DatasetIDs), requiresKBs: true},
		{tool: agentic_rag.NewGrepChunksTool(scope.TenantID, scope.DatasetIDs), requiresKBs: true},
		{tool: agentic_rag.NewListChunksTool(scope.TenantID, scope.DatasetIDs), requiresKBs: true},
	}
	// Web search is a capability of the CONVERSATION: it is advertised only when this
	// turn's chat enables it and its provider resolves (the handler filled
	// scope.webSearch from the binding's conversation). Same rule the in-process
	// engines apply — an absent capability is never advertised, so the model cannot
	// call a tool that cannot answer.
	if scope.webSearch != nil {
		specs = append(specs, codexToolSpec{tool: agentic_rag.NewWebSearchTool(scope.webSearch)})
	}
	return specs
}

// NewCodexToolServer builds an MCP server whose tools are scoped to one turn's binding.
// The scope is captured at construction (per request); tool arguments can only narrow it
// (resolveDatasetScope inside the tools enforces that), never widen it.
func NewCodexToolServer(scope CodexScope) *sdk.Server {
	server := sdk.NewServer(&sdk.Implementation{Name: "ragflow-codex-mcp", Version: "1.0.0"}, &sdk.ServerOptions{
		Capabilities: &sdk.ServerCapabilities{Tools: &sdk.ToolCapabilities{}},
	})
	for _, spec := range codexToolSpecs(scope) {
		addCodexTool(server, scope, spec)
	}
	return server
}

// addCodexTool registers one agent_rag tool on the MCP server, reusing the tool's own
// description and JSON schema and forwarding calls to its InvokableRun. The MCP layer
// adds exactly two things the tool does not know about: the no-scope guard, and the
// MCP result envelope.
func addCodexTool(server *sdk.Server, scope CodexScope, spec codexToolSpec) {
	invokable, ok := spec.tool.(einotool.InvokableTool)
	if !ok {
		panic("mcp: codex tool is not invokable")
	}
	info, err := spec.tool.Info(context.Background())
	if err != nil {
		panic(fmt.Sprintf("mcp: codex tool Info: %v", err))
	}
	server.AddTool(&sdk.Tool{
		Name:        info.Name,
		Description: info.Desc,
		InputSchema: codexInputSchema(info),
	}, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		// The conversation binds no knowledge base: fail the retrieval call rather
		// than search an empty/default scope. Codex reads the error and carries on.
		if spec.requiresKBs && len(scope.DatasetIDs) == 0 {
			return codexToolError(info.Name + " is unavailable: this conversation has no knowledge base bound"), nil
		}
		// Carry the binding's owner so retrieval's dataset permission check passes:
		// an MCP request has no agent canvas state to read the user from.
		ctx = permission.WithIdentity(ctx, scope.UserID, scope.TenantID)
		args := "{}"
		if req != nil && req.Params != nil && req.Params.Arguments != nil {
			if raw, err := json.Marshal(req.Params.Arguments); err == nil {
				args = string(raw)
			}
		}
		out, err := invokable.InvokableRun(ctx, args)
		if err != nil {
			return codexToolError(fmt.Sprintf("%s: %v", info.Name, err)), nil
		}
		return textResult(out), nil
	})
}

// codexInputSchema marshals the eino tool's own parameter schema for the MCP tool
// declaration, so Codex sees the same contract the in-process agent sees. The schema is
// an object (AddTool requires it); the fallback is only for a tool that declares none.
func codexInputSchema(info *schema.ToolInfo) json.RawMessage {
	if info.ParamsOneOf != nil {
		if js, err := info.ParamsOneOf.ToJSONSchema(); err == nil && js != nil {
			if raw, err := json.Marshal(js); err == nil {
				return raw
			}
		}
	}
	return json.RawMessage(`{"type":"object"}`)
}

func codexToolError(message string) *sdk.CallToolResult {
	result := textResult(message)
	result.IsError = true
	return result
}

// --- HTTP transport ---

// codexScopeKey carries the resolved binding from the auth wrapper into the per-request
// server factory.
type codexScopeKey struct{}

// NewCodexHandler serves the Codex bridge over streamable HTTP at
// POST/GET/DELETE /mcp/codex/{token}. Authorization is per-turn: an unknown handle is
// rejected 401 and a known handle with no active turn 403, both before any server or tool
// is built, so a bad or replayed handle can never reach a retrieval tool.
func NewCodexHandler(tickets *CodexTickets) http.Handler {
	streamable := sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
		scope, _ := r.Context().Value(codexScopeKey{}).(CodexScope)
		return NewCodexToolServer(scope)
	}, &sdk.StreamableHTTPOptions{
		Stateless:                    true,
		JSONResponse:                 true,
		MaxRequestBodyBytes:          1 << 20,
		PropagateRequestCancellation: true,
	})
	mux := http.NewServeMux()
	serve := func(w http.ResponseWriter, r *http.Request) {
		token := codexToken(r)
		scope, ok := tickets.Resolve(r.Context(), token)
		if !ok {
			if alive, _ := tickets.HandleAlive(r.Context(), token); !alive {
				http.Error(w, "unknown or expired handle", http.StatusUnauthorized)
				return
			}
			http.Error(w, "no active turn for this handle", http.StatusForbidden)
			return
		}
		// Resolve the conversation's web-search callback for this request (nil when the
		// turn did not opt in, or no provider resolves). It stays process-local: the
		// binding names the conversation, and the provider's credentials never leave
		// the database.
		scope.webSearch = resolveCodexWebSearch(r.Context(), scope)
		streamable.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), codexScopeKey{}, scope)))
	}
	mux.HandleFunc("POST /mcp/codex/{token}", serve)
	mux.HandleFunc("GET /mcp/codex/{token}", serve)
	mux.HandleFunc("DELETE /mcp/codex/{token}", serve)
	return mux
}

// codexToken extracts the handle from the request path. It prefers PathValue (set when
// this handler's own ServeMux routed the request) and falls back to the path suffix so
// the handler also works when mounted behind another router (e.g. gin.WrapH).
func codexToken(r *http.Request) string {
	if v := r.PathValue("token"); v != "" {
		return v
	}
	const prefix = "/mcp/codex/"
	if p := r.URL.Path; strings.HasPrefix(p, prefix) {
		return strings.TrimPrefix(p, prefix)
	}
	return ""
}
