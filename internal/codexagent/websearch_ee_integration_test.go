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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	codexgo "github.com/yuzhichang/codex-app-server-go"
)

// TestWebSearchOverrideIsHonored (mode 8) proves the per-thread override that closes
// Codex's provider-hosted web search is a real config key, not a silently-ignored one.
// The app-server must ACCEPT `web_search = "disabled"` and REJECT an out-of-enum value;
// the rejection is what makes the acceptance meaningful — without it, a typo'd key would
// pass too and the server-side tool would stay on (and keep being billed by the provider).
//
//	RAGFLOW_CODEX_IT=1 go test -tags integration -run TestWebSearchOverrideIsHonored ./internal/codexagent/
func TestWebSearchOverrideIsHonored(t *testing.T) {
	if os.Getenv("RAGFLOW_CODEX_IT") != "1" {
		t.Skip("set RAGFLOW_CODEX_IT=1 to run against the local codex app-server")
	}
	bin, err := codexgo.FindBinary()
	if err != nil {
		t.Skipf("codex binary not found: %v", err)
	}

	client, err := codexgo.New(codexgo.WithStdioProcess(bin, "app-server"))
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// A dummy provider so thread/start mirrors production's option shape without needing
	// credentials: the thread is never run, so the provider is never contacted.
	base := []codexgo.ThreadOption{
		codexgo.WithThreadModel("dummy-model"),
		codexgo.WithThreadModelProvider("dummy"),
		codexgo.WithThreadConfigOverride("model_providers.dummy", map[string]interface{}{
			"name": "dummy", "base_url": "http://127.0.0.1:1/v1", "wire_api": "responses",
		}),
		codexgo.WithThreadSandbox(codexgo.SandboxReadOnly),
		codexgo.WithThreadApprovalPolicy(codexgo.ApprovalUntrusted()),
	}

	// Control: an out-of-enum value must be rejected. If it is accepted, the config key is
	// not being validated and the "disabled" assertion below would be worthless.
	if th, err := client.StartThread(ctx, append(base,
		codexgo.WithThreadConfigOverride("web_search", "not-a-mode"))...); err == nil {
		th.Close()
		t.Fatal("app-server accepted an invalid web_search mode; the override may be ignored")
	}

	// The production setting must be accepted.
	th, err := client.StartThread(ctx, append(base,
		codexgo.WithThreadConfigOverride("web_search", "disabled"))...)
	if err != nil {
		t.Fatalf("app-server rejected web_search=disabled: %v", err)
	}
	th.Close()
}

// TestWebSearchOverrideRemovesHostedTool is the end-to-end counterpart: it points the
// thread's provider at a local mock and inspects the ACTUAL outbound model request, so it
// proves the disabled override removes the hosted `{"type":"web_search"}` tool from the
// wire — not merely that the key parses. The control run (no override) must contain it,
// which is what keeps the assertion honest. No provider is contacted and no tokens are
// spent: the mock records the request and replies with an error.
//
//	RAGFLOW_CODEX_IT=1 go test -tags integration -run TestWebSearchOverrideRemovesHostedTool ./internal/codexagent/
func TestWebSearchOverrideRemovesHostedTool(t *testing.T) {
	if os.Getenv("RAGFLOW_CODEX_IT") != "1" {
		t.Skip("set RAGFLOW_CODEX_IT=1 to run against the local codex app-server")
	}
	bin, err := codexgo.FindBinary()
	if err != nil {
		t.Skipf("codex binary not found: %v", err)
	}

	// The mock captures the first /responses request body Codex sends and fails the call:
	// the tool list is in the request, so the turn failing afterwards is irrelevant.
	var (
		mu     sync.Mutex
		bodies []string
	)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if body, rerr := io.ReadAll(io.LimitReader(r.Body, 1<<20)); rerr == nil {
			mu.Lock()
			bodies = append(bodies, string(body))
			mu.Unlock()
		}
		http.Error(w, `{"error":{"message":"mock provider"}}`, http.StatusInternalServerError)
	}))
	defer mock.Close()

	runTurn := func(t *testing.T, extra ...codexgo.ThreadOption) string {
		t.Helper()
		client, err := codexgo.New(codexgo.WithStdioProcess(bin, "app-server"))
		if err != nil {
			t.Fatalf("client: %v", err)
		}
		defer client.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		opts := []codexgo.ThreadOption{
			codexgo.WithThreadModel("dummy-model"),
			codexgo.WithThreadModelProvider("dummy"),
			codexgo.WithThreadConfigOverride("model_providers.dummy", map[string]interface{}{
				"name": "dummy", "base_url": mock.URL + "/v1", "wire_api": "responses",
				"experimental_bearer_token": "unused",
				"request_max_retries":       0,
				"stream_max_retries":        0,
			}),
			codexgo.WithThreadSandbox(codexgo.SandboxReadOnly),
			codexgo.WithThreadApprovalPolicy(codexgo.ApprovalUntrusted()),
		}
		opts = append(opts, extra...)

		th, err := client.StartThread(ctx, opts...)
		if err != nil {
			t.Fatalf("StartThread: %v", err)
		}
		defer th.Close()
		ch, err := th.RunStreamed(ctx, "hello")
		if err == nil {
			for range ch { // drain; the turn is expected to fail at the mock
			}
		}
		mu.Lock()
		defer mu.Unlock()
		if len(bodies) == 0 {
			t.Fatal("no model request reached the mock")
		}
		return bodies[len(bodies)-1]
	}

	hasWebSearch := func(body string) bool {
		var req struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		if err := json.Unmarshal([]byte(body), &req); err != nil {
			t.Fatalf("model request is not JSON: %v\n%s", err, body)
		}
		for _, tool := range req.Tools {
			if tool["type"] == "web_search" {
				return true
			}
		}
		return false
	}

	// Control first (also fills `bodies[0]`): without the override the hosted tool is sent.
	if !hasWebSearch(runTurn(t)) {
		t.Skip("provider capability did not advertise web_search; cannot assert the override removes it")
	}
	// Production setting: the hosted tool must be gone from the wire.
	if body := runTurn(t, codexgo.WithThreadConfigOverride("web_search", "disabled")); hasWebSearch(body) {
		t.Fatalf("web_search=disabled still produced a hosted web_search tool:\n%s",
			strings.TrimSpace(body))
	}
}

// TestResumeDoesNotReloadHostedToolPolicy pins the reason the hosted-tools policy is part
// of the thread reuse key: thread/resume does NOT reload it. A thread created without the
// override keeps the provider's `web_search` tool even when resumed WITH
// `web_search = "disabled"`, so RAGFlow must REBUILD such a thread rather than resume it
// (RunTurn folds codexagent.HostedToolsFingerprint into the scope fingerprint for that).
// Same mock technique as above: no provider is contacted.
//
//	RAGFLOW_CODEX_IT=1 go test -tags integration -run TestResumeDoesNotReloadHostedToolPolicy ./internal/codexagent/
func TestResumeDoesNotReloadHostedToolPolicy(t *testing.T) {
	if os.Getenv("RAGFLOW_CODEX_IT") != "1" {
		t.Skip("set RAGFLOW_CODEX_IT=1 to run against the local codex app-server")
	}
	bin, err := codexgo.FindBinary()
	if err != nil {
		t.Skipf("codex binary not found: %v", err)
	}

	var (
		mu     sync.Mutex
		bodies []string
	)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b, rerr := io.ReadAll(io.LimitReader(r.Body, 1<<20)); rerr == nil {
			mu.Lock()
			bodies = append(bodies, string(b))
			mu.Unlock()
		}
		http.Error(w, `{"error":{"message":"mock provider"}}`, http.StatusInternalServerError)
	}))
	defer mock.Close()

	client, err := codexgo.New(codexgo.WithStdioProcess(bin, "app-server"))
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	base := []codexgo.ThreadOption{
		codexgo.WithThreadModel("dummy-model"),
		codexgo.WithThreadModelProvider("dummy"),
		codexgo.WithThreadConfigOverride("model_providers.dummy", map[string]interface{}{
			"name": "dummy", "base_url": mock.URL + "/v1", "wire_api": "responses",
			"request_max_retries": 0, "stream_max_retries": 0,
		}),
		codexgo.WithThreadSandbox(codexgo.SandboxReadOnly),
		codexgo.WithThreadApprovalPolicy(codexgo.ApprovalUntrusted()),
	}
	lastHasWebSearch := func() bool {
		mu.Lock()
		defer mu.Unlock()
		if len(bodies) == 0 {
			t.Fatal("no model request reached the mock")
		}
		var req struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		if err := json.Unmarshal([]byte(bodies[len(bodies)-1]), &req); err != nil {
			t.Fatalf("model request is not JSON: %v", err)
		}
		for _, tool := range req.Tools {
			if tool["type"] == "web_search" {
				return true
			}
		}
		return false
	}
	run := func(th *codexgo.SessionThread) {
		t.Helper()
		ch, rerr := th.RunStreamed(ctx, "hello")
		if rerr == nil {
			for range ch { // drain; the turn fails at the mock
			}
		}
	}

	// A thread created WITHOUT the override carries the hosted tool.
	th, err := client.StartThread(ctx, base...)
	if err != nil {
		t.Fatalf("StartThread: %v", err)
	}
	defer th.Close()
	run(th)
	if !lastHasWebSearch() {
		t.Skip("provider capability did not advertise web_search; cannot assert resume behaviour")
	}

	// Resuming that thread WITH the override must NOT remove it — this is the measured
	// Codex behaviour the rebuild-on-policy-change exists to work around. If this ever
	// starts failing (i.e. resume DOES reload), the fingerprint component can be dropped.
	th2, err := client.ResumeThread(ctx, th.ID(), append(base,
		codexgo.WithThreadConfigOverride("web_search", "disabled"))...)
	if err != nil {
		t.Fatalf("ResumeThread: %v", err)
	}
	defer th2.Close()
	run(th2)
	if !lastHasWebSearch() {
		t.Skip("resume now reloads the hosted-tool policy; the reuse-key component is no longer required")
	}
}
