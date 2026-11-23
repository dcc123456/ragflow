//go:build ee && integration

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

package codexagent_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	codexgo "github.com/yuzhichang/codex-app-server-go"

	"ragflow/internal/codexagent"
	"ragflow/internal/mcp"
)

// testKV is an in-test mcp.KV so the runner's binder needs no Kvrocks.
type testKV struct {
	mu   sync.Mutex
	data map[string]string
}

func newTestKV() *testKV { return &testKV{data: map[string]string{}} }

func (k *testKV) Get(_ context.Context, key string) (string, bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.data[key]
	return v, ok, nil
}

func (k *testKV) Set(_ context.Context, key, value string, _ time.Duration) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.data[key] = value
	return nil
}

func (k *testKV) SetNX(_ context.Context, key, value string, _ time.Duration) (bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, ok := k.data[key]; ok {
		return false, nil
	}
	k.data[key] = value
	return true, nil
}

func (k *testKV) Del(_ context.Context, keys ...string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, key := range keys {
		delete(k.data, key)
	}
	return nil
}

func (k *testKV) Expire(_ context.Context, _ string, _ time.Duration) error { return nil }

// testBinder adapts the ticket store to the runner's TurnBinder seam.
type testBinder struct{ tickets *mcp.CodexTickets }

func (b testBinder) Bind(ctx context.Context, handle string, binding any) error {
	scope, ok := binding.(mcp.CodexScope)
	if !ok {
		return fmt.Errorf("bad binding %T", binding)
	}
	return b.tickets.Bind(ctx, handle, scope)
}

func (b testBinder) Unbind(ctx context.Context, handle string) { b.tickets.Unbind(ctx, handle) }

// TestRunnerAgainstLocalCodex exercises the whole runner against the local codex
// app-server: MCP injection, a real turn, and thread reuse on the second turn.
//
//	RAGFLOW_CODEX_IT=1 OPENAI_BASE_URL=... OPENAI_API_KEY=... OPENAI_MODEL=... \
//	  go test -tags integration -run TestRunnerAgainstLocalCodex ./internal/codexagent/
func TestRunnerAgainstLocalCodex(t *testing.T) {
	if os.Getenv("RAGFLOW_CODEX_IT") != "1" {
		t.Skip("set RAGFLOW_CODEX_IT=1 to run against the local codex app-server")
	}
	bin, err := codexgo.FindBinary()
	if err != nil {
		t.Skipf("codex binary not found: %v", err)
	}

	// A live MCP bridge, scoped to one KB, behind an httptest server. The handle carries
	// no authority: the runner binds the turn's scope to it (testBinder) and is what makes
	// the bridge authorize retrieval.
	tickets := mcp.NewCodexTickets(newTestKV())
	handle, err := tickets.HandleForSession(context.Background(), "it-tenant", "it-session")
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	srv := httptest.NewServer(mcp.NewCodexHandler(tickets))
	defer srv.Close()
	mcpURL := srv.URL + "/mcp/codex/" + handle

	client, err := codexgo.New(codexgo.WithStdioProcess(bin, "app-server"))
	if err != nil {
		t.Fatalf("codex client: %v", err)
	}
	defer client.Close()

	// A working model provider must be supplied per-thread (the shared Codex's own
	// default provider may be unconfigured/unreachable in the test environment).
	baseURL := os.Getenv("OPENAI_BASE_URL")
	apiKey := os.Getenv("OPENAI_API_KEY")
	model := os.Getenv("OPENAI_MODEL")
	if baseURL == "" || apiKey == "" || model == "" {
		t.Skip("set OPENAI_BASE_URL / OPENAI_API_KEY / OPENAI_MODEL to a Responses-API provider")
	}

	runner := codexagent.NewRunner(client, codexagent.NewMemoryThreadStore(), testBinder{tickets: tickets})
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	base := codexagent.TurnRequest{
		TenantID:        "it-tenant",
		SessionID:       "it-session",
		DatasetIDs:      []string{"it-kb"},
		Model:           model,
		ProviderID:      "minimax",
		ProviderBaseURL: baseURL,
		ProviderToken:   apiKey,
		MCPURL:          mcpURL,
		Ticket:          handle,
		Binding:         mcp.CodexScope{TenantID: "it-tenant", DatasetIDs: []string{"it-kb"}},
		TurnTimeout:     3 * time.Minute,
		Instructions:    "Answer concisely.",
	}

	first := base
	first.Messages = []map[string]interface{}{{"role": "user", "content": "Reply with exactly: PONG"}}
	out1, err := runner.RunTurn(ctx, first)
	if err != nil {
		t.Fatalf("first turn: %v", err)
	}
	if out1.ThreadID == "" || !out1.Rebuilt {
		t.Fatalf("first turn should create a thread: %+v", out1)
	}
	t.Logf("turn1 thread=%s rebuilt=%v content=%q", out1.ThreadID, out1.Rebuilt, out1.Content)

	// Second turn: same scope, extended history -> must reuse the thread (resume).
	second := base
	second.Messages = []map[string]interface{}{
		{"role": "user", "content": "Reply with exactly: PONG"},
		{"role": "assistant", "content": out1.Content},
		{"role": "user", "content": "Now reply with exactly: PONG2"},
	}
	out2, err := runner.RunTurn(ctx, second)
	if err != nil {
		t.Fatalf("second turn: %v", err)
	}
	if out2.ThreadID != out1.ThreadID {
		t.Fatalf("second turn should reuse thread %s, got %s", out1.ThreadID, out2.ThreadID)
	}
	if out2.Rebuilt {
		t.Fatal("second turn with unchanged scope must NOT rebuild the thread")
	}
	t.Logf("turn2 thread=%s rebuilt=%v content=%q", out2.ThreadID, out2.Rebuilt, out2.Content)
}
