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

package codexagent

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	codexgo "github.com/yuzhichang/codex-app-server-go"

	"ragflow/internal/retrievalaccount"
)

// TestTurnErrorDetailPrefersTurnError pins that a failed turn surfaces the turn's own
// error payload (which carries the real cause, e.g. the provider's HTTP status) rather
// than the bare, often empty, `error` notification that arrives first.
func TestTurnErrorDetailPrefersTurnError(t *testing.T) {
	const detailed = `{"message":"unexpected status 404 Not Found, url: https://api.minimaxi.com/responses"}`
	turn := &codexgo.Turn{Error: json.RawMessage(detailed)}

	if got := turnErrorDetail(turn, errors.New("(code=\"\"): ")); got != detailed {
		t.Fatalf("turn error detail: got %q, want %q", got, detailed)
	}
}

func TestTurnErrorDetailFallsBackToStreamError(t *testing.T) {
	streamErr := errors.New("codexagent: the Codex server reported an error with no detail")

	if got := turnErrorDetail(nil, streamErr); got != streamErr.Error() {
		t.Fatalf("fallback to stream error: got %q, want %q", got, streamErr.Error())
	}
	if got := turnErrorDetail(&codexgo.Turn{}, streamErr); got != streamErr.Error() {
		t.Fatalf("empty turn error falls back: got %q, want %q", got, streamErr.Error())
	}
}

func TestTurnErrorDetailPlaceholder(t *testing.T) {
	if got := turnErrorDetail(nil, nil); got != "no error detail" {
		t.Fatalf("placeholder: got %q, want %q", got, "no error detail")
	}
}

// reasoningItemText backs the completed-item fallback: a provider that does not stream
// reasoning deltas sends one finished reasoning item instead. Its raw reasoning rides
// `content`; `summary` is the model-written summary and is often empty for those
// providers, so content wins and summary is the fallback.
func TestReasoningItemText(t *testing.T) {
	item := func(payload map[string]any) codexgo.Item {
		t.Helper()
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		return codexgo.Item{ID: "r1", Kind: codexgo.ItemKindReasoning, Payload: raw}
	}

	if got := reasoningItemText(item(map[string]any{
		"summary": []string{"a summary"},
		"content": []string{"raw ", "reasoning"},
	})); got != "raw reasoning" {
		t.Fatalf("content must win: got %q", got)
	}
	if got := reasoningItemText(item(map[string]any{"summary": []string{"only summary"}})); got != "only summary" {
		t.Fatalf("summary must be the fallback: got %q", got)
	}
	if got := reasoningItemText(codexgo.Item{Kind: codexgo.ItemKindReasoning}); got != "" {
		t.Fatalf("an empty payload must yield no text: got %q", got)
	}
}

// usageOrNil distinguishes "the server reported no usage" from "a turn that cost zero
// tokens": a caller must not publish an all-zero usage block in the former case.
func TestUsageOrNil(t *testing.T) {
	if usageOrNil(TurnUsage{}) != nil {
		t.Fatal("no model calls must yield nil usage, not a zero block")
	}
	got := usageOrNil(TurnUsage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7, LLMCalls: 1})
	if got == nil || got.TotalTokens != 7 || got.LLMCalls != 1 {
		t.Fatalf("usageOrNil dropped a reported usage: %+v", got)
	}
}

// mcpToolRoundTrip reads the call half of an MCP tool call, which is what makes a
// retrieval step visible: without the tool name and its arguments the thinking stream
// cannot say what the model reached for.
func TestMcpToolRoundTrip(t *testing.T) {
	item := func(payload map[string]any) codexgo.Item {
		t.Helper()
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return codexgo.Item{ID: "c1", Kind: codexgo.ItemKindMCPToolCall, Payload: raw}
	}

	got, ok := mcpToolRoundTrip(item(map[string]any{
		"server":     "ragflow",
		"tool":       "search_bm25_chunks",
		"status":     "completed",
		"arguments":  map[string]any{"queries": []string{"曹操 生卒年", "孙权 生卒年"}},
		"durationMs": 340,
	}))
	if !ok {
		t.Fatal("a well-formed mcpToolCall must parse")
	}
	if got.Tool != "search_bm25_chunks" || got.Status != ToolStatusCompleted || got.DurationMS != 340 {
		t.Fatalf("round trip = %+v", got)
	}
	if got.Kind != RoundTripKindMCP {
		t.Fatalf("kind = %q, want %q", got.Kind, RoundTripKindMCP)
	}
	if got.Args != `{"queries":["曹操 生卒年","孙权 生卒年"]}` {
		t.Fatalf("args must be compact JSON: %q", got.Args)
	}

	// No status yet (the item/started half) reads as running, not as an empty status.
	if running, ok := mcpToolRoundTrip(item(map[string]any{"tool": "grep_chunks"})); !ok || running.Status != ToolStatusRunning {
		t.Fatalf("missing status must default to running: %+v ok=%v", running, ok)
	}
	// An item with no tool name is not a tool call.
	if _, ok := mcpToolRoundTrip(item(map[string]any{"server": "ragflow"})); ok {
		t.Fatal("an item without a tool name must not parse as a tool call")
	}
}

// commandRoundTrip reads a local shell execution — the channel mode 8 now permits for
// arithmetic — so the thinking stream can show the model computing rather than only
// asserting a number. Its status maps the command enum AND the exit code: a server
// "completed" with a non-zero exit is still a failure.
func TestCommandRoundTrip(t *testing.T) {
	item := func(payload map[string]any) codexgo.Item {
		t.Helper()
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return codexgo.Item{ID: "x1", Kind: codexgo.ItemKindCommandExecution, Payload: raw}
	}

	got, ok := commandRoundTrip(item(map[string]any{
		"command":    "python3 -c 'print(1234*5678)'",
		"status":     "completed",
		"exitCode":   0,
		"durationMs": 42,
	}))
	if !ok {
		t.Fatal("a well-formed commandExecution must parse")
	}
	if got.Tool != commandToolName || got.Kind != RoundTripKindCommand || got.Status != ToolStatusCompleted || got.DurationMS != 42 {
		t.Fatalf("round trip = %+v", got)
	}
	if got.ExitCode == nil || *got.ExitCode != 0 {
		t.Fatalf("exit code = %v, want 0", got.ExitCode)
	}

	// A non-zero exit is a failure even when the server reports "completed".
	failed, ok := commandRoundTrip(item(map[string]any{"command": "python3 -c 'boom'", "status": "completed", "exitCode": 1}))
	if !ok || failed.Status != ToolStatusFailed || failed.ExitCode == nil || *failed.ExitCode != 1 {
		t.Fatalf("non-zero exit must read as failed: %+v ok=%v", failed, ok)
	}
	// The sandbox refusing a command (a write outside the profile, or the network) is a
	// failure, not a silent no-op.
	if declined, ok := commandRoundTrip(item(map[string]any{"command": "curl https://example.com", "status": "declined"})); !ok || declined.Status != ToolStatusFailed {
		t.Fatalf("declined must read as failed: %+v ok=%v", declined, ok)
	}
	// The item/started half carries no exit code and reads as running.
	if running, ok := commandRoundTrip(item(map[string]any{"command": "node -e '1+1'", "status": "inProgress"})); !ok || running.Status != ToolStatusRunning || running.ExitCode != nil {
		t.Fatalf("in-progress must read as running: %+v ok=%v", running, ok)
	}
	// A command line is required; an item without one is not an execution.
	if _, ok := commandRoundTrip(item(map[string]any{"status": "completed"})); ok {
		t.Fatal("an item without a command must not parse as an execution")
	}

	// The command line is collapsed to one line and bounded, like tool arguments.
	long, ok := commandRoundTrip(item(map[string]any{"command": "python3 -c \"\n" + strings.Repeat("x", 400) + "\n\"", "status": "inProgress"}))
	if !ok || strings.Contains(long.Args, "\n") || len(long.Args) > 303 {
		t.Fatalf("args must be one bounded line: len=%d", len(long.Args))
	}
}

// collabRoundTrip reads a sub-agent collaboration step (features.multi_agent_v2) so the
// thinking stream shows delegation — otherwise enabling the feature is invisible. The tool
// is namespaced into the "agent:" counts key so it can never pass for a retrieval tool.
func TestCollabRoundTrip(t *testing.T) {
	item := func(payload map[string]any) codexgo.Item {
		t.Helper()
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return codexgo.Item{ID: "a1", Kind: codexgo.ItemKindCollabAgentToolCall, Payload: raw}
	}

	got, ok := collabRoundTrip(item(map[string]any{
		"tool":              "spawnAgent",
		"status":            "completed",
		"prompt":            "find the   founding year",
		"receiverThreadIds": []string{"t1"},
	}))
	if !ok {
		t.Fatal("a well-formed collabAgentToolCall must parse")
	}
	if got.Tool != "agent:spawnAgent" || got.Kind != RoundTripKindAgent || got.Status != ToolStatusCompleted {
		t.Fatalf("round trip = %+v", got)
	}
	if got.Args != "find the founding year" {
		t.Fatalf("args must be one collapsed line: %q", got.Args)
	}

	// wait/interrupt carry no prompt — report the target count instead of an empty line.
	if wait, ok := collabRoundTrip(item(map[string]any{"tool": "wait", "status": "inProgress", "receiverThreadIds": []string{"t1", "t2"}})); !ok || wait.Args != "2 agent(s)" || wait.Status != ToolStatusRunning {
		t.Fatalf("wait = %+v ok=%v", wait, ok)
	}
	// interrupted reads as failed; a missing status reads as running.
	if bad, ok := collabRoundTrip(item(map[string]any{"tool": "sendMessage", "status": "interrupted"})); !ok || bad.Status != ToolStatusFailed {
		t.Fatalf("interrupted = %+v ok=%v", bad, ok)
	}
	if running, ok := collabRoundTrip(item(map[string]any{"tool": "listAgents"})); !ok || running.Status != ToolStatusRunning {
		t.Fatalf("missing status = %+v ok=%v", running, ok)
	}
	// An item with no collaboration tool is not a step.
	if _, ok := collabRoundTrip(item(map[string]any{"status": "completed"})); ok {
		t.Fatal("an item without a tool must not parse")
	}
}

// subAgentRoundTrip reads the sub-agent spawn/complete markers — the only signal that a
// sub-agent actually ran, since `spawn_agent` itself arrives as a plain function call the
// app-server does not surface as a thread item. start and completion are DIFFERENT keys so
// the tally counts agents started, not each agent twice.
func TestSubAgentRoundTrip(t *testing.T) {
	activity := func(payload map[string]any) codexgo.Item {
		t.Helper()
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return codexgo.Item{ID: "s1", Kind: codexgo.ItemKindSubAgentActivity, Payload: raw}
	}

	started, ok := subAgentRoundTrip(activity(map[string]any{"kind": "started", "agentPath": "/root/quantum_core", "model": "MiniMax-M3"}))
	if !ok {
		t.Fatal("a started activity must parse")
	}
	if started.Tool != "agent:spawn" || started.Kind != RoundTripKindAgent || started.Status != ToolStatusRunning || started.Args != "/root/quantum_core" {
		t.Fatalf("started = %+v", started)
	}
	done, ok := subAgentRoundTrip(activity(map[string]any{"kind": "completed", "agentPath": "/root/quantum_core"}))
	if !ok || done.Tool != "agent:done" || done.Status != ToolStatusCompleted {
		t.Fatalf("completed = %+v ok=%v", done, ok)
	}
	if failed, ok := subAgentRoundTrip(activity(map[string]any{"kind": "failed", "agentPath": "/root/x"})); !ok || failed.Status != ToolStatusFailed {
		t.Fatalf("failed = %+v ok=%v", failed, ok)
	}
	// An activity without an agent path names nothing to report.
	if _, ok := subAgentRoundTrip(activity(map[string]any{"kind": "started"})); ok {
		t.Fatal("an activity without an agent path must not parse")
	}
}

// compactArgs keeps the thinking line on one line and bounded, so a long query list
// cannot flood the panel.
func TestCompactArgs(t *testing.T) {
	if got := compactArgs(json.RawMessage(`{"a": 1,  "b":"x"}`)); got != `{"a":1,"b":"x"}` {
		t.Fatalf("compactArgs = %q", got)
	}
	if got := compactArgs(nil); got != "" {
		t.Fatalf("nil args = %q, want empty", got)
	}
	if got := compactArgs(json.RawMessage(`null`)); got != "" {
		t.Fatalf("null args = %q, want empty", got)
	}
	long := json.RawMessage(`{"q":"` + strings.Repeat("x", 400) + `"}`)
	if got := compactArgs(long); len(got) > 303 || !strings.HasSuffix(got, "...") {
		t.Fatalf("long args not truncated: len=%d tail=%q", len(got), got[len(got)-8:])
	}
}

// toolTally is what makes a mode-8 turn report tool_call_counts / tool_call_errors in the
// same shape as the in-process engines: a benchmark reads them without caring which engine
// produced the row. A call is counted once (the item/started half, or the completed half
// when the provider sends no started); an outcome only records failures.
func TestToolTally(t *testing.T) {
	var tt toolTally
	tt.call("search_bm25_chunks")
	tt.call("search_bm25_chunks")
	tt.call("list_chunks")
	tt.call("") // no tool name: not a call
	tt.outcome(RoundTrip{Tool: "search_bm25_chunks", Status: ToolStatusCompleted})
	tt.outcome(RoundTrip{Tool: "list_chunks", Status: ToolStatusFailed})

	if got := tt.counts["search_bm25_chunks"]; got != 2 {
		t.Errorf("search_bm25_chunks = %d, want 2", got)
	}
	if got := tt.counts["list_chunks"]; got != 1 {
		t.Errorf("list_chunks = %d, want 1", got)
	}
	if len(tt.counts) != 2 {
		t.Errorf("counts = %v, want two tools", tt.counts)
	}
	if got := tt.errors["list_chunks"]; got != 1 {
		t.Errorf("errors[list_chunks] = %d, want 1", got)
	}
	if len(tt.errors) != 1 {
		t.Errorf("a completed call must not be an error: %v", tt.errors)
	}
}

// The v2 status is camelCase ("inProgress"), while the narration branches on our own
// constant: without normalization a call would be narrated as a RESULT.
func TestMcpToolRoundTripNormalizesInProgress(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"tool": "grep_chunks", "status": "inProgress"})
	rt, ok := mcpToolRoundTrip(codexgo.Item{ID: "c", Kind: codexgo.ItemKindMCPToolCall, Payload: raw})
	if !ok || rt.Status != ToolStatusRunning {
		t.Fatalf("inProgress must normalize to running: %+v ok=%v", rt, ok)
	}
}

// retrievalAccounting turns the MCP tool results into the same accounting the in-process
// engines ship: documents retrieved vs served, and chunk reads split by depth. The weight
// of a served document is one per CALL (the rendered output repeats a document once per
// chunk, and mode 5's own weight is distinct queries — a caller that only sees the output
// cannot attribute hits to a query, so the call is the unit).
func TestRetrievalAccounting(t *testing.T) {
	var ra retrievalAccounting
	ra.add(retrievalaccount.AccountToolOutput("search_bm25_chunks",
		`<search_results count="2" query="q">`+
			`<chunk rank="1" chunk_id="c1" doc_id="d1" doc_name="5412.md"><match_snippet>s</match_snippet></chunk>`+
			`<chunk rank="2" chunk_id="c2" doc_id="d2" doc_name="5412.md"><match_snippet>t</match_snippet></chunk>`+
			`</search_results>`))
	ra.add(retrievalaccount.AccountToolOutput("list_chunks",
		`<chunks doc_id="d1" fetched="1"><chunk chunk_id="c9" doc_id="d1" doc_name="77.md"><content>x</content></chunk></chunks>`))
	// web_search renders url/title: it must never inflate corpus recall.
	ra.add(retrievalaccount.AccountToolOutput("web_search",
		`<web_results><result url="https://e/x" title="X">s</result></web_results>`))

	var out TurnOutcome
	ra.apply(&out)

	// A locator's doc was SERVED but not RETRIEVED (it was never opened); list_chunks'
	// doc was both.
	if got := out.ServedDocIDs; !reflect.DeepEqual(got, []string{"5412", "77"}) {
		t.Fatalf("ServedDocIDs = %v", got)
	}
	if got := out.RetrievedDocIDs; !reflect.DeepEqual(got, []string{"77"}) {
		t.Fatalf("RetrievedDocIDs = %v (only the full-content tool opened a document)", got)
	}
	// One document served twice inside one call still weighs one call.
	if out.ServedDocIDs[0] != "5412" {
		t.Fatalf("the most-served document must rank first: %v", out.ServedDocIDs)
	}
	if out.ShallowReadChunks != 2 || out.DeepReadChunks != 1 {
		t.Fatalf("reads = deep %d shallow %d, want 1/2", out.DeepReadChunks, out.ShallowReadChunks)
	}
	if !reflect.DeepEqual(out.ShallowReadChunkIDs, []string{"c1", "c2"}) ||
		!reflect.DeepEqual(out.DeepReadChunkIDs, []string{"c9"}) {
		t.Fatalf("ids = deep %v shallow %v", out.DeepReadChunkIDs, out.ShallowReadChunkIDs)
	}
}
