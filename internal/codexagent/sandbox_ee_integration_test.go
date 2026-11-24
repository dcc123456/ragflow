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
	"os"
	"path/filepath"
	"testing"
	"time"

	codexgo "github.com/yuzhichang/codex-app-server-go"

	"ragflow/internal/codexagent"
)

// TestSandboxReadOnlyBlocksWrites (M0-d) verifies the capability isolation mode 8 relies
// on: a read-only sandbox must stop the agent from creating files, while a
// workspace-write sandbox lets the same prompt succeed. The second case is what makes the
// first meaningful — it proves the probe would otherwise write, so read-only is doing the
// blocking, not the model refusing.
//
//	RAGFLOW_CODEX_IT=1 OPENAI_BASE_URL=... OPENAI_API_KEY=... OPENAI_MODEL=... \
//	  go test -tags integration -run TestSandboxReadOnlyBlocksWrites ./internal/codexagent/
func TestSandboxReadOnlyBlocksWrites(t *testing.T) {
	if os.Getenv("RAGFLOW_CODEX_IT") != "1" {
		t.Skip("set RAGFLOW_CODEX_IT=1 to run against the local codex app-server")
	}
	bin, err := codexgo.FindBinary()
	if err != nil {
		t.Skipf("codex binary not found: %v", err)
	}
	baseURL, apiKey, model := os.Getenv("OPENAI_BASE_URL"), os.Getenv("OPENAI_API_KEY"), os.Getenv("OPENAI_MODEL")
	if baseURL == "" || apiKey == "" || model == "" {
		t.Skip("set OPENAI_BASE_URL / OPENAI_API_KEY / OPENAI_MODEL")
	}

	provider := func() []codexgo.ThreadOption {
		return []codexgo.ThreadOption{
			codexgo.WithThreadModel(model),
			codexgo.WithThreadModelProvider("minimax"),
			codexgo.WithThreadConfigOverride("model_providers.minimax", map[string]interface{}{
				"name": "minimax", "base_url": baseURL, "wire_api": "responses", "experimental_bearer_token": apiKey,
			}),
			codexgo.WithThreadApprovalPolicy(codexgo.ApprovalNever()),
		}
	}

	run := func(t *testing.T, sandbox codexgo.SandboxMode) bool {
		t.Helper()
		workspace := t.TempDir()
		client, err := codexgo.New(codexgo.WithStdioProcess(bin, "app-server"))
		if err != nil {
			t.Fatalf("client: %v", err)
		}
		defer client.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		opts := append(provider(),
			codexgo.WithThreadSandbox(sandbox),
			codexgo.WithThreadCWD(workspace),
		)
		th, err := client.StartThread(ctx, opts...)
		if err != nil {
			t.Fatalf("StartThread: %v", err)
		}
		defer th.Close()

		ch, err := th.RunStreamed(ctx, "Use the shell to create a file named probe.txt in the current working directory containing the word hi, then reply DONE.")
		if err != nil {
			t.Fatalf("RunStreamed: %v", err)
		}
		for range ch {
		}
		_, statErr := os.Stat(filepath.Join(workspace, "probe.txt"))
		return statErr == nil
	}

	if wrote := run(t, codexgo.SandboxWorkspaceWrite); !wrote {
		t.Skip("workspace-write run did not create the file (model behaviour varies); cannot assert read-only")
	}
	if wrote := run(t, codexgo.SandboxReadOnly); wrote {
		t.Fatal("read-only sandbox allowed a file write — isolation is not effective")
	}
}

// TestApprovalDeclineDoesNotHang (M0-d / P1#2) checks the production approval posture:
// with codexagent.Dispatcher installed (command/file approvals declined) and a read-only
// sandbox, a turn that tries to run a writing command must COMPLETE on its own — a declined
// approval must never wedge the turn until the timeout — and must not have written anything.
func TestApprovalDeclineDoesNotHang(t *testing.T) {
	if os.Getenv("RAGFLOW_CODEX_IT") != "1" {
		t.Skip("set RAGFLOW_CODEX_IT=1 to run against the local codex app-server")
	}
	bin, err := codexgo.FindBinary()
	if err != nil {
		t.Skipf("codex binary not found: %v", err)
	}
	baseURL, apiKey, model := os.Getenv("OPENAI_BASE_URL"), os.Getenv("OPENAI_API_KEY"), os.Getenv("OPENAI_MODEL")
	if baseURL == "" || apiKey == "" || model == "" {
		t.Skip("set OPENAI_BASE_URL / OPENAI_API_KEY / OPENAI_MODEL")
	}

	workspace := t.TempDir()
	client, err := codexgo.New(
		codexgo.WithStdioProcess(bin, "app-server"),
		codexgo.WithRequestHandler(codexagent.Dispatcher()),
	)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	th, err := client.StartThread(ctx,
		codexgo.WithThreadModel(model),
		codexgo.WithThreadModelProvider("minimax"),
		codexgo.WithThreadConfigOverride("model_providers.minimax", map[string]interface{}{
			"name": "minimax", "base_url": baseURL, "wire_api": "responses", "experimental_bearer_token": apiKey,
		}),
		codexgo.WithThreadSandbox(codexgo.SandboxReadOnly),
		codexgo.WithThreadApprovalPolicy(codexgo.ApprovalUntrusted()),
		codexgo.WithThreadCWD(workspace),
	)
	if err != nil {
		t.Fatalf("StartThread: %v", err)
	}
	defer th.Close()

	ch, err := th.RunStreamed(ctx, "Run this exact shell command, then reply DONE: echo hi > probe.txt")
	if err != nil {
		t.Fatalf("RunStreamed: %v", err)
	}
	for range ch {
	}
	if ctx.Err() != nil {
		t.Fatalf("turn did not complete on its own (declined approval wedged it): %v", ctx.Err())
	}
	if _, statErr := os.Stat(filepath.Join(workspace, "probe.txt")); statErr == nil {
		t.Fatal("read-only sandbox allowed a write")
	}
}
