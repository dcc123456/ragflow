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
	"strings"
	"testing"
	"time"

	codexgo "github.com/yuzhichang/codex-app-server-go"
)

// TestCodexSandboxMountAndNetworkCapability answers a design question empirically: can
// Codex itself mount an S3 bucket (e.g. by running mount-s3 / geesefs / s3fs from PATH and
// being told to do so in its system prompt), rather than having the pod pre-mount it?
//
// Three things must all hold for "let the model mount it" to work, and each is a separate
// mechanism:
//  1. the mount syscall must be permitted (bubblewrap drops CAP_SYS_ADMIN in its namespace)
//  2. /dev/fuse must exist in the sandbox (bubblewrap assembles its own minimal /dev)
//  3. the network must be reachable (the read-only profile is network-isolated, seccomp
//     denies socket syscalls)
//
// All probes run under the PRODUCTION posture: SandboxReadOnly + ApprovalOnRequest, via
// command/exec. Results are logged, not asserted — the point is to record what the sandbox
// actually permits, which a different codex version could change.
//
//	RAGFLOW_CODEX_IT=1 CODEX_WS_URL=ws://127.0.0.1:8790 \
//	  OPENAI_BASE_URL=... OPENAI_API_KEY=... OPENAI_MODEL=... \
//	  bash build.sh --test-integration -run TestCodexSandboxMountAndNetworkCapability ./internal/codexagent/...
func TestCodexSandboxMountAndNetworkCapability(t *testing.T) {
	env := requireCodexProbeEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	client := dialCodexWS(t, ctx, env.Endpoint)
	defer client.Close()

	readOnly := json.RawMessage(`{"type":"readOnly"}`)

	probes := []struct{ label, cmd string }{
		{"tooling-on-PATH", `for b in mount-s3 geesefs s3fs rclone fusermount fusermount3; do printf '%s -> ' "$b"; command -v "$b" || echo MISSING; done`},
		{"fuse-device", `test -e /dev/fuse && echo "/dev/fuse PRESENT" || echo "/dev/fuse ABSENT"; ls -l /dev/fuse 2>&1 | head -1`},
		{"capabilities", `grep -E '^(CapEff|CapPrm|NoNewPrivs|Seccomp)' /proc/self/status`},
		{"mount-syscall", `mount -t tmpfs none /mnt 2>&1; echo "mount-exit=$?"`},
		{"network-tcp", `timeout 3 bash -c 'echo > /dev/tcp/127.0.0.1/9000' 2>&1; echo "tcp-exit=$?"`},
		{"network-dns", `timeout 3 getent hosts localhost 2>&1; echo "getent-exit=$?"`},
		{"writable-tmp", `touch /tmp/.ragflow-sandbox-probe 2>&1; echo "touch-exit=$?"; rm -f /tmp/.ragflow-sandbox-probe`},
	}

	for _, p := range probes {
		p := p
		t.Run(p.label, func(t *testing.T) {
			res, err := client.CommandExec(ctx, codexgo.CommandExecRequest{
				Command:       []string{"/bin/bash", "-lc", p.cmd},
				SandboxPolicy: readOnly,
				TimeoutMs:     20000,
			})
			if err != nil {
				t.Fatalf("command/exec: %v", err)
			}
			t.Logf("cmd: %s", p.cmd)
			t.Logf("exit=%d", res.ExitCode)
			if out := trimSttyNoise(res.Stdout); out != "" {
				t.Logf("stdout:\n%s", out)
			}
			if errOut := trimSttyNoise(res.Stderr); errOut != "" {
				t.Logf("stderr:\n%s", errOut)
			}
		})
	}
}

// trimSttyNoise drops the harmless "stty: 'standard input': Inappropriate ioctl" lines the
// unified-exec startup emits for a non-tty, so the real output stays readable.
func trimSttyNoise(s string) string {
	out := make([]string, 0, 8)
	for _, line := range strings.Split(s, "\n") {
		if line == "" || strings.HasPrefix(line, "stty:") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
