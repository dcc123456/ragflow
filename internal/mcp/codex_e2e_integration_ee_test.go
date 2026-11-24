//go:build integration && ee

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

package mcp_test

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	codexgo "github.com/yuzhichang/codex-app-server-go"
	"gorm.io/gorm"

	"ragflow/internal/agent/runtime"
	"ragflow/internal/codexagent"
	"ragflow/internal/mcp"
)

const (
	cannedChunkID = "chunk-e2e-0001"
	cannedDocID   = "doc-e2e-0001"
)

// memKV is the in-test mcp.KV so the bridge needs no Kvrocks.
type memKV struct {
	mu   sync.Mutex
	data map[string]string
}

func newMemKV() *memKV { return &memKV{data: map[string]string{}} }

func (k *memKV) Get(_ context.Context, key string) (string, bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.data[key]
	return v, ok, nil
}

func (k *memKV) Set(_ context.Context, key, value string, _ time.Duration) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.data[key] = value
	return nil
}

func (k *memKV) SetNX(_ context.Context, key, value string, _ time.Duration) (bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, ok := k.data[key]; ok {
		return false, nil
	}
	k.data[key] = value
	return true, nil
}

func (k *memKV) Del(_ context.Context, keys ...string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, key := range keys {
		delete(k.data, key)
	}
	return nil
}

func (k *memKV) Expire(_ context.Context, _ string, _ time.Duration) error { return nil }

func (k *memKV) Keys(_ context.Context, prefix string) ([]string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	var out []string
	for key := range k.data {
		if strings.HasPrefix(key, prefix) {
			out = append(out, key)
		}
	}
	return out, nil
}

type fakeRetrieval struct{ calls atomic.Int32 }

func (f *fakeRetrieval) Search(_ context.Context, _ *gorm.DB, _ runtime.RetrievalRequest) ([]runtime.RetrievalChunk, error) {
	f.calls.Add(1)
	return []runtime.RetrievalChunk{{
		ID: cannedChunkID, Content: "hello from the knowledge base",
		DocumentID: cannedDocID, DocumentName: "e2e.md", DatasetID: "kb-e2e", PageNum: 1, Score: 0.91,
	}}, nil
}

// TestCodexCallsBridgeTool (M1 E2E) drives codex to call the bridge's retrieval tool and
// asserts the bridge actually served a hit. Config is env-driven while investigating
// codex's MCP tool-call approval.
func TestCodexCallsBridgeTool(t *testing.T) {
	if os.Getenv("RAGFLOW_CODEX_IT") != "1" {
		t.Skip("set RAGFLOW_CODEX_IT=1")
	}
	bin, err := codexgo.FindBinary()
	if err != nil {
		t.Skipf("codex binary not found: %v", err)
	}
	baseURL, apiKey, model := os.Getenv("OPENAI_BASE_URL"), os.Getenv("OPENAI_API_KEY"), os.Getenv("OPENAI_MODEL")
	if baseURL == "" || apiKey == "" || model == "" {
		t.Skip("set OPENAI_BASE_URL / OPENAI_API_KEY / OPENAI_MODEL")
	}

	retrieval := &fakeRetrieval{}
	runtime.SetRetrievalService(retrieval)
	defer runtime.SetRetrievalService(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	// A handle with a live per-turn binding: the bridge authorizes retrieval only while
	// the turn is bound.
	tickets := mcp.NewCodexTickets(newMemKV())
	handle, err := tickets.HandleForSession(ctx, "e2e-tenant", "e2e-session")
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if err := tickets.Bind(ctx, handle, mcp.CodexScope{TenantID: "e2e-tenant", DatasetIDs: []string{"kb-e2e"}}); err != nil {
		t.Fatalf("bind refused: %v", err)
	}
	srv := httptest.NewServer(mcp.NewCodexHandler(tickets))
	defer srv.Close()

	// codexagent.Dispatcher installs the elicitation handler that accepts MCP tool calls;
	// without it codex declines them (see internal/codexagent/approval.go).
	client, err := codexgo.New(
		codexgo.WithStdioProcess(bin, "app-server"),
		codexgo.WithRequestHandler(codexagent.Dispatcher()),
	)
	if err != nil {
		t.Fatalf("codex client: %v", err)
	}
	defer client.Close()

	opts := []codexgo.ThreadOption{
		codexgo.WithThreadModel(model),
		codexgo.WithThreadModelProvider("minimax"),
		codexgo.WithThreadConfigOverride("model_providers.minimax", map[string]interface{}{
			"name": "minimax", "base_url": baseURL, "wire_api": "responses", "experimental_bearer_token": apiKey,
		}),
		codexgo.WithThreadConfigOverride("mcp_servers.ragflow", map[string]interface{}{
			"url": srv.URL + "/mcp/codex/" + handle,
		}),
		codexgo.WithThreadSandbox(codexgo.SandboxReadOnly),
		codexgo.WithThreadApprovalPolicy(codexgo.ApprovalUntrusted()),
	}

	th, err := client.StartThread(ctx, opts...)
	if err != nil {
		t.Fatalf("StartThread: %v", err)
	}
	defer th.Close()

	prompt := "Use the MCP tool `search_semantic_chunks` from the `ragflow` server with the " +
		"argument queries=[\"hello\"]. Then reply with only the chunk_id value that tool returned."
	ch, err := th.RunStreamed(ctx, prompt)
	if err != nil {
		t.Fatalf("RunStreamed: %v", err)
	}
	answer := ""
	for ev := range ch {
		switch e := ev.Raw.(type) {
		case codexgo.ItemAgentMessageDeltaEvent:
			answer += e.Text
		case codexgo.TurnCompletedEvent:
			if answer == "" && e.Turn != nil {
				answer = (&codexgo.TurnResult{Turn: *e.Turn}).FinalAgentText()
			}
		case codexgo.ErrorEvent:
			t.Fatalf("turn error: code=%q msg=%q", e.Code, e.Message)
		}
	}
	t.Logf("answer=%q retrieval_calls=%d", answer, retrieval.calls.Load())
	if retrieval.calls.Load() < 1 {
		t.Fatalf("codex did not reach the retrieval backend: calls=%d", retrieval.calls.Load())
	}
}
