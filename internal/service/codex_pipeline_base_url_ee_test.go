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

//go:build ee

package service

import (
	"ragflow/internal/codexagent"
	"strings"
	"testing"
)

// TestNormalizeResponsesBaseURL pins that a version-less provider base gains "/v1" for the
// Codex Responses endpoint, while a base that already names a version is untouched.
func TestNormalizeResponsesBaseURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"minimax appends v1", "https://api.minimaxi.com", "https://api.minimaxi.com/v1"},
		{"trailing slash", "https://api.minimaxi.com/", "https://api.minimaxi.com/v1"},
		{"openai kept", "https://api.openai.com/v1", "https://api.openai.com/v1"},
		{"versioned kept", "https://api.siliconflow.cn/v1", "https://api.siliconflow.cn/v1"},
		{"beta version kept", "https://generativelanguage.googleapis.com/v1beta", "https://generativelanguage.googleapis.com/v1beta"},
		{"query preserved", "https://x.example.com/openai/deployments/d?api-version=2024-01-01", "https://x.example.com/openai/deployments/d/v1?api-version=2024-01-01"},
		{"empty stays empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeResponsesBaseURL(tc.in); got != tc.want {
				t.Fatalf("normalizeResponsesBaseURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestCodexCitationContractDefersCapabilityAndScopesTheShell pins the two properties the
// citation contract must have now that web search is a PER-TURN decision while the contract
// is baked into a thread at create (and not reloaded by resume):
//
//   - it is generic about the RETRIEVAL toolset — the MCP tool list is the single source of
//     truth for whether web search exists, so the prompt must not claim either way, and must
//     stay identical across a web-search toggle (otherwise prompt and tool list are two
//     copies of one fact that can disagree);
//   - it scopes the shell: LOCAL computation is wanted (mode 8 runs a read-only, network-
//     isolated sandbox so a short script resolves arithmetic exactly), but the shell has no
//     network, so it must never be used as a path to the web — otherwise the model either
//     refuses to compute, or burns the turn on `curl`.
func TestCodexCitationContractDefersCapabilityAndScopesTheShell(t *testing.T) {
	quoted := codexRootContract(true)

	// Capability comes from the tool list, not the prompt: conditional everywhere.
	if !strings.Contains(quoted, "authoritative") || !strings.Contains(quoted, "tool list") {
		t.Errorf("contract does not defer capability to the MCP tool list:\n%s", quoted)
	}
	if !strings.Contains(quoted, "when this conversation enables web search") {
		t.Errorf("contract does not present web search conditionally:\n%s", quoted)
	}
	if strings.Contains(quoted, "You have a web search tool") {
		t.Errorf("contract asserts the web capability instead of deferring to the tool list:\n%s", quoted)
	}
	// Local computation is permitted...
	for _, must := range []string{"LOCAL computation", "python3", "arithmetic"} {
		if !strings.Contains(quoted, must) {
			t.Errorf("contract does not permit local computation (%q missing):\n%s", must, quoted)
		}
	}
	// ...but the shell is never a path to the web, and asking to be let out is not a path
	// either. Naming the request channel matters: a q11 run read "no network access" as a
	// condition to be lifted and passed `sandbox_permissions: require_escalated`.
	for _, must := range []string{
		"NO network access", "curl/wget", "urllib", "fetch", "sandbox_permissions", "require_escalated",
	} {
		if !strings.Contains(quoted, must) {
			t.Errorf("contract does not forbid the shell as a web path (%q missing):\n%s", must, quoted)
		}
	}

	// Both provenance forms are described, the web one conditionally on the tool.
	if !strings.Contains(quoted, "(doc: <doc_name>") || !strings.Contains(quoted, "(source: web") {
		t.Errorf("contract omits a provenance form:\n%s", quoted)
	}
	if !strings.Contains(quoted, "returned by the web search tool") {
		t.Errorf("the web form is not tied to the web tool:\n%s", quoted)
	}

	// Quote off still scopes the shell and defers capability; it only drops the format.
	plain := codexRootContract(false)
	if !strings.Contains(plain, "NO network access") || !strings.Contains(plain, "curl/wget") {
		t.Errorf("quote-off contract dropped the shell scope:\n%s", plain)
	}
	if strings.Contains(plain, "(doc: <doc_name>") {
		t.Errorf("quote-off contract should not prescribe a provenance format:\n%s", plain)
	}

	// The root contract carries no prose-shape instruction: Codex's own agentic prompt
	// already answers in prose, and the line is exactly the root-oriented phrasing that
	// mis-fits a sub-agent.
	if strings.Contains(quoted, "Answer the user in prose") {
		t.Errorf("contract still carries the redundant prose instruction:\n%s", quoted)
	}
}

// TestCodexSubagentContractIsScoped pins what a delegated sub-agent gets: the retrieval-tools
// authority line, the shell/network policy, and the provenance format — and nothing that is the
// root's end-user framing. The shell policy is deliberately INCLUDED: it was once root-only on
// the theory that the sandbox enforces the boundary by itself, but a child has the same shell,
// the same sandbox and the same `require_escalated` request channel, and a q11 run showed the
// boundary is askable-around.
func TestCodexSubagentContractIsScoped(t *testing.T) {
	quoted := codexSubagentContract(true)

	// Kept: tool authority + the shell/network policy + the provenance format.
	for _, must := range []string{
		"authoritative", "tool list", "NO network access", "require_escalated",
		"(doc: <doc_name>", "(source: web",
	} {
		if !strings.Contains(quoted, must) {
			t.Errorf("sub-agent contract dropped a required clause (%q missing):\n%s", must, quoted)
		}
	}
	// Dropped: worker framing Codex's own role prompt already provides, and the root's
	// answer-shaping fallback.
	for _, mustNot := range []string{"delegated subtask", "say plainly what they do not cover"} {
		if strings.Contains(quoted, mustNot) {
			t.Errorf("sub-agent contract still carries root/role framing (%q):\n%s", mustNot, quoted)
		}
	}

	// Quote off drops the format but keeps the tool-authority line.
	plain := codexSubagentContract(false)
	if !strings.Contains(plain, "authoritative") {
		t.Errorf("quote-off sub-agent contract dropped the tool-authority line:\n%s", plain)
	}
	if strings.Contains(plain, "(doc: <doc_name>") {
		t.Errorf("quote-off sub-agent contract should not prescribe a provenance format:\n%s", plain)
	}
}

// narrationChunk frames a tool line so it never runs into the reasoning text around it:
// exactly one newline on each side. A stream that is mid-line gets a leading newline; a
// fresh line (or a fresh think block) does not, or the panel would open with a blank line.
func TestNarrationChunkFraming(t *testing.T) {
	cases := []struct {
		name string
		tail byte
		line string
		want string
	}{
		{"mid-line", 'x', "grep_chunks {}", "\ngrep_chunks {}\n"},
		{"at a line start", '\n', "grep_chunks {}", "grep_chunks {}\n"},
		{"fresh block", 0, "grep_chunks {}", "grep_chunks {}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := narrationChunk(tc.tail, tc.line); got != tc.want {
				t.Fatalf("narrationChunk(%q, %q) = %q, want %q", tc.tail, tc.line, got, tc.want)
			}
		})
	}
}

// The narration carries no "[MCP] " prefix: the tool name is the first token.
func TestToolNarrationHasNoPrefix(t *testing.T) {
	cases := map[string]codexagent.RoundTrip{
		"search_bm25_chunks {}":                {Tool: "search_bm25_chunks", Status: codexagent.ToolStatusRunning, Args: "{}"},
		"search_bm25_chunks returned in 886ms": {Tool: "search_bm25_chunks", Status: codexagent.ToolStatusCompleted, DurationMS: 886},
		"grep_chunks failed in 12ms":           {Tool: "grep_chunks", Status: codexagent.ToolStatusFailed, DurationMS: 12},
		"list_chunks":                          {Tool: "list_chunks", Status: codexagent.ToolStatusRunning},
	}
	for want, rt := range cases {
		if got := toolNarration(rt); got != want {
			t.Fatalf("toolNarration(%+v) = %q, want %q", rt, got, want)
		}
		if strings.Contains(toolNarration(rt), "[MCP]") {
			t.Fatalf("narration still carries the [MCP] prefix: %q", toolNarration(rt))
		}
	}
}

// commandNarration renders a local shell execution distinctly from a tool call: the command
// line plus its exit status, so a user watching the panel sees the model compute rather than
// only assert a number.
func TestCommandNarration(t *testing.T) {
	exit0, exit1 := 0, 1
	cases := []struct {
		name string
		rt   codexagent.RoundTrip
		want string
	}{
		{
			name: "completed",
			rt:   codexagent.RoundTrip{Tool: "shell", Kind: codexagent.RoundTripKindCommand, Args: "python3 -c 'print(1)'", Status: codexagent.ToolStatusCompleted, DurationMS: 42, ExitCode: &exit0},
			want: "shell: python3 -c 'print(1)' (exit 0, 42ms)",
		},
		{
			name: "non-zero exit",
			rt:   codexagent.RoundTrip{Tool: "shell", Kind: codexagent.RoundTripKindCommand, Args: "node -e boom", Status: codexagent.ToolStatusFailed, DurationMS: 7, ExitCode: &exit1},
			want: "shell: node -e boom (exit 1, 7ms)",
		},
		{
			name: "declined without an exit code still reads as failed",
			rt:   codexagent.RoundTrip{Tool: "shell", Kind: codexagent.RoundTripKindCommand, Args: "curl x", Status: codexagent.ToolStatusFailed, DurationMS: 3},
			want: "shell: curl x (failed, 3ms)",
		},
		{
			name: "running shows the command alone",
			rt:   codexagent.RoundTrip{Tool: "shell", Kind: codexagent.RoundTripKindCommand, Args: "python3 -c '1+1'", Status: codexagent.ToolStatusRunning},
			want: "shell: python3 -c '1+1'",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := commandNarration(tc.rt); got != tc.want {
				t.Fatalf("commandNarration(%+v) = %q, want %q", tc.rt, got, tc.want)
			}
		})
	}
}

// agentNarration renders a sub-agent collaboration step (features.multi_agent_v2) so
// delegation is visible in the panel; the namespaced tool key reads un-namespaced.
func TestAgentNarration(t *testing.T) {
	cases := []struct {
		name string
		rt   codexagent.RoundTrip
		want string
	}{
		{
			name: "running shows the delegated task",
			rt:   codexagent.RoundTrip{Tool: "agent:spawnAgent", Kind: codexagent.RoundTripKindAgent, Args: "find the year", Status: codexagent.ToolStatusRunning},
			want: "agent: spawnAgent find the year",
		},
		{
			name: "completed appends the status",
			rt:   codexagent.RoundTrip{Tool: "agent:spawnAgent", Kind: codexagent.RoundTripKindAgent, Args: "find the year", Status: codexagent.ToolStatusCompleted},
			want: "agent: spawnAgent find the year (completed)",
		},
		{
			name: "no args shows the tool alone",
			rt:   codexagent.RoundTrip{Tool: "agent:listAgents", Kind: codexagent.RoundTripKindAgent, Status: codexagent.ToolStatusRunning},
			want: "agent: listAgents",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := agentNarration(tc.rt); got != tc.want {
				t.Fatalf("agentNarration(%+v) = %q, want %q", tc.rt, got, tc.want)
			}
		})
	}
}
