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

// Shared harness for the codex sandbox probes. These probes measure what the shared
// Codex app-server actually does — they are diagnostics, not assertions, because the
// model's willingness to run a shell command varies between runs; only the parts that
// are deterministic (thread/start acceptance, command/exec, fs/* RPCs) could be
// asserted on.
//
// They MUST connect over WebSocket. codexgo.WithStdioProcess spawns codex as a local
// child process that shares RAGFlow's filesystem, which hides exactly the cross-host
// behaviour these probes exist to measure (see the package's production dialer,
// codexagent.Dial, and internal/service/codex_pipeline.go).
package codexagent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	codexgo "github.com/yuzhichang/codex-app-server-go"

	"ragflow/internal/codexagent"
)

// codexProbeEnv holds the connection and model inputs every probe needs. Probes skip
// (never fail) when they are absent, so the default unit run stays self-contained.
type codexProbeEnv struct {
	Endpoint string
	BaseURL  string
	APIKey   string
	Model    string
}

func requireCodexProbeEnv(t *testing.T) codexProbeEnv {
	t.Helper()
	if os.Getenv("RAGFLOW_CODEX_IT") != "1" {
		t.Skip("set RAGFLOW_CODEX_IT=1 to run against a codex app-server")
	}
	env := codexProbeEnv{
		Endpoint: os.Getenv("CODEX_WS_URL"),
		BaseURL:  os.Getenv("OPENAI_BASE_URL"),
		APIKey:   os.Getenv("OPENAI_API_KEY"),
		Model:    os.Getenv("OPENAI_MODEL"),
	}
	if env.Endpoint == "" {
		env.Endpoint = "ws://127.0.0.1:8790"
	}
	if env.BaseURL == "" || env.APIKey == "" || env.Model == "" {
		t.Skip("set OPENAI_BASE_URL / OPENAI_API_KEY / OPENAI_MODEL")
	}
	return env
}

// dialCodexWS connects over WebSocket, matching the production transport
// (codexagent.Dial from internal/service/codex_pipeline.go). CODEX_WS_TOKEN is optional
// and only needed when the app-server requires a bearer token.
func dialCodexWS(t *testing.T, ctx context.Context, endpoint string) *codexgo.Client {
	t.Helper()
	opts := make([]codexgo.Option, 0, 4)
	if tok := os.Getenv("CODEX_WS_TOKEN"); tok != "" {
		opts = append(opts, codexgo.WithWSBearerToken(tok))
	}
	opts = append(opts,
		codexgo.WithReconnectingWSTransport(ctx, endpoint),
		codexgo.WithAutoReconnect(),
		codexgo.WithRequestHandler(codexagent.Dispatcher()),
	)
	client, err := codexgo.New(opts...)
	if err != nil {
		t.Fatalf("dial %s: %v", endpoint, err)
	}
	return client
}

// probeThreadOpts builds the mode-8 thread posture: read-only sandbox, on-request
// approvals, and a per-thread provider (the shared server's own default may be
// unconfigured here). An EMPTY cwd omits WithThreadCWD — the state RAGFlow is in once
// the work-dir mechanism is removed.
func probeThreadOpts(env codexProbeEnv, cwd string) []codexgo.ThreadOption {
	opts := []codexgo.ThreadOption{
		codexgo.WithThreadModel(env.Model),
		codexgo.WithThreadModelProvider("minimax"),
		codexgo.WithThreadConfigOverride("model_providers.minimax", map[string]interface{}{
			"name": "minimax", "base_url": env.BaseURL, "wire_api": "responses", "experimental_bearer_token": env.APIKey,
		}),
		codexgo.WithThreadSandbox(codexgo.SandboxReadOnly),
		codexgo.WithThreadApprovalPolicy(codexgo.ApprovalOnRequest()),
	}
	if cwd != "" {
		opts = append(opts, codexgo.WithThreadCWD(cwd))
	}
	return opts
}

// deleteThread removes a probe thread from the shared server so repeated runs do not
// accumulate state. Callers defer it AFTER the turn: deleting first strands the turn
// with "thread not found".
func deleteThread(ctx context.Context, client *codexgo.Client, threadID string) error {
	dctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return client.ThreadDelete(dctx, codexgo.ThreadDeleteRequest{ThreadID: threadID})
}

// dumpItem logs every field of an item payload (command, cwd, status, exit code,
// aggregated output, ...) without hard-coding field names that could drift.
func dumpItem(t *testing.T, label string, payload json.RawMessage) {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(payload, &m); err != nil {
		t.Logf("%s: (not an object) %s", label, truncate(string(payload), 600))
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, truncate(string(m[k]), 400)))
	}
	t.Logf("%s: %s", label, strings.Join(parts, " | "))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}
