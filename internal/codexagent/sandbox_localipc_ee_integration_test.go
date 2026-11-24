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
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	codexgo "github.com/yuzhichang/codex-app-server-go"
)

// TestCodexSandboxLocalIPC answers one question that decides a whole design branch: **can a
// process inside Codex's sandbox reach a local daemon?**
//
// The sibling probe (sandbox_capability_integration_test.go) measured that AF_INET socket()
// fails with EPERM — the sandbox cannot open a TCP connection, so Codex cannot call an HTTP
// MCP bridge or any local HTTP service. That rules out "ship a CLI on PATH that talks HTTP".
// What it did NOT measure is whether the seccomp filter blocks socket() wholesale or only
// for INET domains. That distinction matters:
//
//   - If AF_UNIX also fails, the ONLY channel from Codex to RAGFlow is the MCP protocol
//     over the connection the app-server itself holds. No local CLI/socket path exists.
//   - If AF_UNIX works, a host-local daemon reachable over a unix socket IS viable — so a
//     CLI (or a unix-socket MCP bridge) could serve the navigable tree after all.
//
// Part 2 makes the test decisive rather than theoretical: this test process — which shares
// the host filesystem with the app-server — creates a REAL unix socket listener, and the
// sandbox is asked to connect to it. Success proves end-to-end local IPC; failure with the
// socket file visible proves the block is on the socket syscall, not on file visibility.
//
// Log-only, never asserted: this records what the sandbox permits today (a codex version or
// seccomp profile change could move it).
//
//	RAGFLOW_CODEX_IT=1 CODEX_WS_URL=ws://127.0.0.1:8790 \
//	  OPENAI_BASE_URL=... OPENAI_API_KEY=... OPENAI_MODEL=... \
//	  bash build.sh --test-integration -run TestCodexSandboxLocalIPC ./internal/codexagent/...
func TestCodexSandboxLocalIPC(t *testing.T) {
	env := requireCodexProbeEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	client := dialCodexWS(t, ctx, env.Endpoint)
	defer client.Close()

	// A real AF_UNIX listener on the host. $HOME is provably the host's real home inside the
	// sandbox (the read-barrier probe read a canary there), so the sandbox can see this path.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home dir: %v", err)
	}
	sockPath := filepath.Join(home, fmt.Sprintf("ragflow-afunix-%d.sock", time.Now().UnixNano()))
	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix %s: %v", sockPath, err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(sockPath)
	}()
	go func() {
		for {
			conn, aerr := listener.Accept()
			if aerr != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 16)
				n, _ := c.Read(buf)
				_, _ = c.Write([]byte("PONG:" + string(buf[:n])))
			}(conn)
		}
	}()
	t.Logf("host-side unix listener at %s", sockPath)

	probes := []struct{ label, cmd string }{
		{"tooling", `for b in python3 node socat nc ncat curl; do printf '%s -> ' "$b"; command -v "$b" || echo MISSING; done`},
		{"af-inet-socket", `python3 -c 'import socket; s=socket.socket(socket.AF_INET, socket.SOCK_STREAM); print("AF_INET-OK")' 2>&1 || echo "AF_INET-FAILED(exit=$?)"`},
		{"af-unix-socket", `python3 -c 'import socket; s=socket.socket(socket.AF_UNIX, socket.SOCK_STREAM); print("AF_UNIX-OK")' 2>&1 || echo "AF_UNIX-FAILED(exit=$?)"`},
		{"af-unix-socketpair", `python3 -c 'import socket; a,b=socket.socketpair(socket.AF_UNIX, socket.SOCK_STREAM); a.send(b"x"); print("socketpair-OK", b.recv(1))' 2>&1 || echo "SOCKETPAIR-FAILED(exit=$?)"`},
		{
			"socket-file-visible",
			fmt.Sprintf(`ls -l '%s' 2>&1 || echo "NOT VISIBLE"`, sockPath),
		},
		{
			"af-unix-connect-to-host-daemon",
			fmt.Sprintf(`python3 -c 'import socket,sys
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.settimeout(5)
try:
    s.connect(sys.argv[1])
    s.sendall(b"PING")
    print("CONNECTED reply=", s.recv(32))
except Exception as e:
    print("CONNECT-FAILED:", type(e).__name__, e)
' '%s' 2>&1 || echo "PY-RUN-FAILED(exit=$?)"`, sockPath),
		},
		{"http-to-local-listener", `curl -s -m 3 -o /dev/null -w 'http_code=%{http_code}\n' http://127.0.0.1:8790/ 2>&1; echo "curl-exit=$?"`},
	}

	readOnly := json.RawMessage(`{"type":"readOnly"}`)
	for _, p := range probes {
		p := p
		t.Run(p.label, func(t *testing.T) {
			res, cerr := client.CommandExec(ctx, codexgo.CommandExecRequest{
				Command:       []string{"/bin/bash", "-lc", p.cmd},
				SandboxPolicy: readOnly,
				TimeoutMs:     25000,
			})
			if cerr != nil {
				t.Fatalf("command/exec: %v", cerr)
			}
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
