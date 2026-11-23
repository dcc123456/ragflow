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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	codexgo "github.com/yuzhichang/codex-app-server-go"
)

// TestReadOnlySandboxCWDisNotAReadBarrier tests a security assumption, not a feature.
//
// Mode 8 used to treat a per-turn WithThreadCWD into an empty private directory as
// isolation ("running in the shared Codex's default cwd would expose that host's tree").
// The sibling probe (cwd_missing_integration_test.go) showed cwd also fails to constrain
// the shell when it does not exist. This probe asks the remaining question: under the
// read-only sandbox, does cwd restrict what the shell can READ, or is it only a starting
// directory?
//
// It is a probe, not a regression test: the model-driven half depends on the model
// choosing to run the command. Read the logged payloads, not the pass/fail line.
//
// Method: plant a canary OUTSIDE the thread's cwd, point the thread at an empty cwd, and
// see whether the shell can read the canary. Three channels are exercised:
//   - a deterministic command/exec carrying an explicit readOnly SandboxPolicy,
//   - the model-driven path mode 8 actually uses, and
//   - a readability-only check of an operator-private file.
//
// Operator-private files are probed for READABILITY ONLY (a boolean and a byte count). No
// credential or config content is ever printed, asserted on, or logged.
//
//	RAGFLOW_CODEX_IT=1 CODEX_WS_URL=ws://127.0.0.1:8790 \
//	  OPENAI_BASE_URL=... OPENAI_API_KEY=... OPENAI_MODEL=... \
//	  bash build.sh --test-integration -run TestReadOnlySandboxCWDisNotAReadBarrier ./internal/codexagent/...
func TestReadOnlySandboxCWDisNotAReadBarrier(t *testing.T) {
	env := requireCodexProbeEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	client := dialCodexWS(t, ctx, env.Endpoint)
	defer client.Close()

	// A canary in the operator's HOME — precisely what cwd isolation was meant to keep out
	// of reach — plus a universally readable host file as a second, write-free data point.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home dir: %v", err)
	}
	canary := filepath.Join(home, fmt.Sprintf("ragflow-canary-%d.txt", time.Now().UnixNano()))
	canaryContent := fmt.Sprintf("RAGFLOW-CANARY-%d", time.Now().UnixNano())
	if werr := os.WriteFile(canary, []byte(canaryContent), 0o644); werr != nil {
		t.Fatalf("plant canary: %v", werr)
	}
	defer func() { _ = os.Remove(canary) }()

	// The thread's cwd: an empty private dir, the shape the removed newWorkDir built.
	cwd := t.TempDir()
	t.Logf("canary (OUTSIDE cwd) = %s", canary)
	t.Logf("thread cwd (empty)   = %s", cwd)

	readOnly := json.RawMessage(`{"type":"readOnly"}`)

	t.Run("command_exec_readOnly", func(t *testing.T) {
		// Deterministic: no model in the loop, explicit readOnly policy, cwd = empty dir.
		assertReadable(t, ctx, client, cwd, readOnly, fmt.Sprintf("cat %q", canary), "canary", canaryContent)
		assertReadable(t, ctx, client, cwd, readOnly, "head -c 60 /etc/passwd", "host /etc/passwd", "root:")
	})

	t.Run("model_driven", func(t *testing.T) {
		th, err := client.StartThread(ctx, probeThreadOpts(env, cwd)...)
		if err != nil {
			t.Fatalf("thread/start: %v", err)
		}
		t.Logf("thread/start ACCEPTED cwd=%q (thread=%s)", cwd, th.ID())
		defer func() {
			th.Close()
			if derr := deleteThread(ctx, client, th.ID()); derr != nil {
				t.Logf("cleanup: thread/delete %s: %v", th.ID(), derr)
			}
		}()

		events, turnID, err := th.RunStreamedTurn(ctx, fmt.Sprintf(
			"Use the shell tool to run exactly this command:\n\ncat %s\n\n"+
				"Then reply with ONLY the file's raw contents, nothing else.", canary))
		if err != nil {
			t.Fatalf("turn: %v", err)
		}
		t.Logf("turn=%s", turnID)

		var message strings.Builder
		readFromShell := false
		for ev := range events {
			switch v := ev.Raw.(type) {
			case codexgo.ItemCompletedEvent:
				if v.Item != nil && v.Item.Kind == codexgo.ItemKindCommandExecution {
					dumpItem(t, "item/completed commandExecution", v.Item.Payload)
					if strings.Contains(string(v.Item.Payload), canaryContent) {
						readFromShell = true
					}
				}
			case codexgo.ItemAgentMessageDeltaEvent:
				message.WriteString(v.Text)
			}
		}

		if readFromShell || strings.Contains(message.String(), canaryContent) {
			t.Logf("VERDICT: the model-driven shell READ the canary at %s while cwd was %s -> cwd is NOT a read barrier", canary, cwd)
		} else {
			t.Logf("INCONCLUSIVE: the canary content was not observed; the model may not have run the command. See the command payload above.")
		}
	})

	t.Run("operator_private_readability", func(t *testing.T) {
		// READABILITY ONLY: report a boolean and a byte count. Never the content.
		res, err := client.CommandExec(ctx, codexgo.CommandExecRequest{
			Command: []string{"/bin/bash", "-lc",
				`f="$HOME/.codex/config.toml"; if [ -r "$f" ]; then echo "READABLE bytes=$(wc -c < "$f")"; else echo "NOT_READABLE"; fi`},
			CWD:           cwd,
			SandboxPolicy: readOnly,
			TimeoutMs:     30000,
		})
		if err != nil {
			t.Logf("command/exec failed: %v", err)
			return
		}
		t.Logf("operator-private probe: exit=%d stdout=%q stderr=%q", res.ExitCode, strings.TrimSpace(res.Stdout), strings.TrimSpace(res.Stderr))
	})
}

// assertReadable runs one command under the readOnly policy from cwd and reports whether
// stdout carried want (proving a path OUTSIDE cwd was read).
func assertReadable(t *testing.T, ctx context.Context, client *codexgo.Client, cwd string, policy json.RawMessage, command, label, want string) {
	t.Helper()
	res, err := client.CommandExec(ctx, codexgo.CommandExecRequest{
		Command:       []string{"/bin/bash", "-lc", command},
		CWD:           cwd,
		SandboxPolicy: policy,
		TimeoutMs:     30000,
	})
	if err != nil {
		t.Logf("[%s] command/exec error: %v", label, err)
		return
	}
	got := res.Stdout
	if strings.Contains(got, want) {
		t.Logf("[%s] READ OUTSIDE cwd: exit=%d stdout=%q -> cwd is NOT a read barrier", label, res.ExitCode, truncate(strings.TrimSpace(got), 200))
		return
	}
	t.Logf("[%s] not read: exit=%d stdout=%q stderr=%q", label, res.ExitCode, truncate(strings.TrimSpace(got), 200), truncate(strings.TrimSpace(res.Stderr), 200))
}
