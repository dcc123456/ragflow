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
	"strings"
	"testing"
	"time"

	codexgo "github.com/yuzhichang/codex-app-server-go"
)

// TestThreadWithNonexistentCWD answers one question: what does the shared Codex
// app-server do with a thread cwd that does not exist ON THE APP-SERVER HOST?
//
// It is a probe, not a regression test: only the deterministic halves (thread/start
// acceptance, and whether a shell command runs at all) are stable — the model's prose is
// not. Read the logged payloads, not the pass/fail line.
//
// This is the production shape that was removed. RAGFlow dials the shared server over
// WebSocket (codexagent.Dial) while the work dir was built with os.MkdirTemp — a path on
// the RAGFLOW host. Outside a shared-filesystem deployment the two hosts differ, so that
// path was meaningless to Codex. The probes keep exercising it so the reason for the
// removal stays reproducible.
//
//	RAGFLOW_CODEX_IT=1 CODEX_WS_URL=ws://127.0.0.1:8790 \
//	  OPENAI_BASE_URL=... OPENAI_API_KEY=... OPENAI_MODEL=... \
//	  bash build.sh --test-integration -run TestThreadWithNonexistentCWD ./internal/codexagent/...
func TestThreadWithNonexistentCWD(t *testing.T) {
	env := requireCodexProbeEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	client := dialCodexWS(t, ctx, env.Endpoint)
	defer client.Close()

	// Control #1: the fs/* RPCs act on the APP-SERVER host (SDK fs.go:10-13). If /tmp is
	// not visible through them, the absence probe below proves nothing about Codex.
	if _, err := client.FSGetMetadata(ctx, codexgo.FsGetMetadataParams{Path: codexgo.AbsolutePathBuf("/tmp")}); err != nil {
		t.Fatalf("control: FSGetMetadata(/tmp) against the codex host failed: %v", err)
	}
	t.Log("control: FSGetMetadata(/tmp) succeeded -> fs/* reads the codex host filesystem")

	// Control #2: the probe path really is absent on that host.
	absent := fmt.Sprintf("/tmp/ragflow-nonexistent-cwd-%d", time.Now().UnixNano())
	if _, err := client.FSGetMetadata(ctx, codexgo.FsGetMetadataParams{Path: codexgo.AbsolutePathBuf(absent)}); err != nil {
		t.Logf("baseline: FSGetMetadata(%q) -> %v (absent, as intended)", absent, err)
	} else {
		t.Fatalf("probe path unexpectedly exists on the codex host: %q", absent)
	}

	t.Run("nonexistent_cwd", func(t *testing.T) {
		probeThreadCWD(t, ctx, client, env, absent)
	})
	t.Run("existing_cwd_control", func(t *testing.T) {
		probeThreadCWD(t, ctx, client, env, "/tmp")
	})
	// The post-removal state: no WithThreadCWD at all. This case is what makes deleting
	// the work-dir mechanism safe to justify — Codex falls back to the app-server's own
	// cwd, which exists, so the shell still runs (unlike the non-existent case above).
	t.Run("no_cwd", func(t *testing.T) {
		probeThreadCWD(t, ctx, client, env, "")
	})
}

// probeThreadCWD starts a read-only thread at cwd ("" omits WithThreadCWD), runs one turn
// that forces a shell command, and logs what the server recorded for that command. The
// commandExecution payload carries the real working directory, exit code and output —
// far more reliable than the model's prose.
func probeThreadCWD(t *testing.T, ctx context.Context, client *codexgo.Client, env codexProbeEnv, cwd string) {
	t.Helper()
	label := cwd
	if label == "" {
		label = "(none)"
	}

	th, err := client.StartThread(ctx, probeThreadOpts(env, cwd)...)
	if err != nil {
		t.Logf("VERDICT: thread/start REJECTED cwd=%s -> %v", label, err)
		return
	}
	t.Logf("thread/start ACCEPTED cwd=%s (thread=%s) -> no cwd validation at start", label, th.ID())

	// Close and delete only AFTER the turn: Close releases the client's semaphore slot and
	// thread/delete removes the server-side thread, either of which would strand the turn
	// with "thread not found".
	defer func() {
		th.Close()
		if derr := deleteThread(ctx, client, th.ID()); derr != nil {
			t.Logf("cleanup: thread/delete %s: %v", th.ID(), derr)
		}
	}()

	events, turnID, err := th.RunStreamedTurn(ctx,
		"Use the shell tool to run exactly `pwd`. Then reply with ONLY the exact path it printed.")
	if err != nil {
		t.Logf("turn did not start: %v", err)
		return
	}
	t.Logf("turn=%s", turnID)

	var message strings.Builder
	commands := 0
	for ev := range events {
		// ev.Raw carries the event as a VALUE (SDK events.go derefEventValue), matching
		// the runner's own switch in run.go — a pointer switch never matches here.
		switch v := ev.Raw.(type) {
		case codexgo.ItemCompletedEvent:
			if v.Item != nil && v.Item.Kind == codexgo.ItemKindCommandExecution {
				commands++
				dumpItem(t, "item/completed commandExecution", v.Item.Payload)
			}
		case codexgo.ItemAgentMessageDeltaEvent:
			message.WriteString(v.Text)
		case codexgo.TurnCompletedEvent:
			t.Logf("turn/completed status=%s", v.Status)
		}
	}
	if commands == 0 {
		t.Logf("NOTE: no commandExecution item was observed; the model may not have used the shell, so the cwd it would have used is unproven")
	} else {
		t.Logf("VERDICT: cwd=%s requested; see the commandExecution payload(s) above for what the shell actually used", label)
	}
}
