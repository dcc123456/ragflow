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
	"bufio"
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

// TestCodexSandboxSeesHostMounts answers the Go/No-Go question for exposing a dataset
// bucket as a POSIX filesystem: **is a host mount visible inside Codex's bubblewrap
// sandbox?**
//
// The FS design rests on a real mount existing in the Codex pod, and Codex runs the shell
// under a bubblewrap mount namespace. If bwrap binds the root NON-recursively, a submount
// (the entire point of a FUSE/NFS export) would appear as the bare mountpoint directory
// instead of the mounted filesystem, and the design is dead. Nothing in the SDK exposes
// this, so it has to be measured.
//
// Two parts:
//
//  1. A calibration CONTROL that must be invisible: a canary in the /dev/shm tmpfs.
//     bubblewrap assembles its own minimal /dev, so the host sees that file while the
//     sandbox does not. Logged only, never asserted — its job is to show the probe CAN
//     detect an invisible mount, which is what makes part 2 meaningful.
//
//  2. The AUTHORITATIVE subject: $CODEX_PROBE_MOUNT, a real mount at a normal path (e.g. a
//     FUSE export of the dataset). Its visibility is asserted. Skipped when unset, because
//     only the caller knows which mount matters.
//
// For each part the probe plants a canary, confirms the Codex HOST sees it (fs/getMetadata
// executes on the app-server host), then tries to read it from inside the sandbox through
// two channels: a deterministic command/exec under an explicit readOnly policy (asserted),
// and a model-driven turn (logged; the model may decline to run a command).
//
// Measured 2026-10-16 against codex 0.162.0, a sshfs FUSE mount at /tmp/ragflow-fuse-mnt:
// control invisible (as expected), FUSE mount READ from inside the sandbox (exit=0, both
// channels) -> bubblewrap propagates host submounts. Reproduce with:
//
//	sshfs localhost:/tmp/ragflow-fuse-src /tmp/ragflow-fuse-mnt -o BatchMode=yes
//	RAGFLOW_CODEX_IT=1 CODEX_WS_URL=ws://127.0.0.1:8790 \
//	  CODEX_PROBE_MOUNT=/tmp/ragflow-fuse-mnt \
//	  OPENAI_BASE_URL=... OPENAI_API_KEY=... OPENAI_MODEL=... \
//	  bash build.sh --test-integration -run TestCodexSandboxSeesHostMounts ./internal/codexagent/...
func TestCodexSandboxSeesHostMounts(t *testing.T) {
	env := requireCodexProbeEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	client := dialCodexWS(t, ctx, env.Endpoint)
	defer client.Close()

	// Part 1 — control, logged only. bubblewrap replaces /dev, so this file is reachable on
	// the host but not inside the sandbox. Seeing it anyway would mean bwrap passes /dev
	// through, which is worth knowing but does not invalidate part 2.
	t.Run("control_dev_shm", func(t *testing.T) {
		probeMountVisibility(t, ctx, client, env, "/dev/shm", false)
	})

	// Part 2 — the authoritative subject.
	mountpoint := strings.TrimSpace(os.Getenv("CODEX_PROBE_MOUNT"))
	if mountpoint == "" {
		t.Log("CODEX_PROBE_MOUNT is unset; the control above ran but the authoritative mount check did not. Set it to a real host mount (FUSE/NFS export of the dataset) to validate the design.")
		return
	}
	t.Run("mount_"+strings.ReplaceAll(strings.Trim(mountpoint, "/"), "/", "_"), func(t *testing.T) {
		probeMountVisibility(t, ctx, client, env, mountpoint, true)
	})
}

// probeMountVisibility plants a canary in mountpoint and reports whether the sandbox can
// read it. When assert is true an invisible mount is a test failure (the design would not
// work); when false the outcome is only logged, which is what a calibration control wants.
func probeMountVisibility(t *testing.T, ctx context.Context, client *codexgo.Client, env codexProbeEnv, mountpoint string, assert bool) {
	t.Helper()

	if !isDistinctMount(t, mountpoint) {
		t.Skipf("%s is not a distinct mount on this host, so the canary would be visible anyway and the result would prove nothing", mountpoint)
	}

	canary := filepath.Join(mountpoint, fmt.Sprintf(".ragflow-mount-probe-%d.txt", time.Now().UnixNano()))
	content := fmt.Sprintf("RAGFLOW-MOUNT-PROBE-%d", time.Now().UnixNano())
	if werr := os.WriteFile(canary, []byte(content), 0o644); werr != nil {
		t.Skipf("cannot plant a canary in %s: %v", mountpoint, werr)
	}
	defer func() { _ = os.Remove(canary) }()
	t.Logf("%s is a distinct mount; canary = %s", mountpoint, canary)

	// Host-side control: without it, a sandbox miss is indistinguishable from "the
	// app-server cannot see the file either".
	if _, err := client.FSGetMetadata(ctx, codexgo.FsGetMetadataParams{Path: codexgo.AbsolutePathBuf(canary)}); err != nil {
		t.Fatalf("host-side control: fs/getMetadata(%s) failed, so a sandbox miss would be uninterpretable: %v", canary, err)
	}
	t.Logf("host-side control: the codex host sees the canary at %s", canary)

	// Channel 1 (deterministic): read it from inside the sandbox.
	res, err := client.CommandExec(ctx, codexgo.CommandExecRequest{
		Command:       []string{"/bin/bash", "-lc", fmt.Sprintf("cat %q", canary)},
		SandboxPolicy: json.RawMessage(`{"type":"readOnly"}`),
		TimeoutMs:     30000,
	})
	if err != nil {
		t.Fatalf("command/exec failed: %v", err)
	}
	visible := strings.Contains(res.Stdout, content)
	switch {
	case visible:
		t.Logf("sandbox read the canary through %s (exit=%d) -> submount IS visible inside bubblewrap", mountpoint, res.ExitCode)
	case assert:
		t.Errorf("submount NOT visible: the host sees %s but a readOnly command/exec did not (exit=%d stdout=%q stderr=%q) — a mount at this path would not reach Codex",
			canary, res.ExitCode, truncate(strings.TrimSpace(res.Stdout), 200), truncate(strings.TrimSpace(res.Stderr), 200))
	default:
		t.Logf("sandbox did NOT see the canary in %s (exit=%d) -> expected for the control: bubblewrap assembles its own /dev, so the probe CAN detect an invisible mount",
			mountpoint, res.ExitCode)
	}

	// Channel 2 (the model-driven path mode 8 actually uses; logged only).
	th, err := client.StartThread(ctx, probeThreadOpts(env, "")...)
	if err != nil {
		t.Logf("model channel: thread/start failed: %v", err)
		return
	}
	defer func() {
		th.Close()
		if derr := deleteThread(ctx, client, th.ID()); derr != nil {
			t.Logf("cleanup: thread/delete %s: %v", th.ID(), derr)
		}
	}()
	events, _, err := th.RunStreamedTurn(ctx, fmt.Sprintf(
		"Use the shell tool to run exactly this command:\n\ncat %s\n\nThen reply with ONLY the file's raw contents, nothing else.", canary))
	if err != nil {
		t.Logf("model channel: turn failed to start: %v", err)
		return
	}
	seen := false
	for ev := range events {
		if v, ok := ev.Raw.(codexgo.ItemCompletedEvent); ok && v.Item != nil && v.Item.Kind == codexgo.ItemKindCommandExecution {
			if strings.Contains(string(v.Item.Payload), content) {
				seen = true
			}
		}
	}
	if seen {
		t.Logf("model channel: the shell read the canary through %s", mountpoint)
	} else {
		t.Logf("model channel: inconclusive — the canary was not observed; the model may not have run the command")
	}
}

// isDistinctMount reports whether path is a mount point on THIS host, by looking it up in
// /proc/self/mountinfo. The probe process and the Codex app-server share this mount
// namespace in the dev setup; a deployment with the app-server elsewhere should run the
// probe from the app-server's host.
func isDistinctMount(t *testing.T, path string) bool {
	t.Helper()
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		t.Fatalf("open /proc/self/mountinfo: %v", err)
	}
	defer f.Close()

	want := filepath.Clean(path)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		// mountinfo: ... <mount point> <mount options> ... — the mount point is field 5
		// (1-based), octal-escaped for space/tab/newline/backslash.
		fields := strings.Fields(scanner.Text())
		if len(fields) < 5 {
			continue
		}
		if unescapeMountInfo(fields[4]) == want {
			return true
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan /proc/self/mountinfo: %v", err)
	}
	return false
}

func unescapeMountInfo(s string) string {
	r := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return r.Replace(s)
}
