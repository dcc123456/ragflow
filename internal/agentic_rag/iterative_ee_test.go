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

package agentic_rag

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"ragflow/internal/tokenizer"
)

// ---------------------------------------------------------------------------
// Fakes: mode 6 drives its models through narrow interfaces so the loop — bounded
// context, the ledger cursor, the rubric gate — is testable with no provider.
// ---------------------------------------------------------------------------

// fakeIterativeModel replays two independent queues: one for the Planner (whose
// output is streamed) and one for everything else (Synthesizer, commit, judge,
// rewrite), which is generated. Recording the prompts is what lets a test assert
// WHAT each role was shown — the property this engine is built around.
type fakeIterativeModel struct {
	mu          sync.Mutex
	planner     []*schema.Message
	generated   []string
	streamCalls []string
	genCalls    []string
	// promptPerCall / completionPerCall, when set, are recorded into the run's token
	// sink on EVERY call, the way a real model driver records provider-reported
	// usage. It is what lets a test assert per-round token DELTAS instead of the run
	// total they merely sum to.
	promptPerCall, completionPerCall int
}

// recordUsage mirrors the driver's one side effect the engine's accounting depends on.
func (f *fakeIterativeModel) recordUsage(ctx context.Context) {
	if f.promptPerCall == 0 && f.completionPerCall == 0 {
		return
	}
	tokenizer.RecordRunTokenUsage(ctx, f.promptPerCall, f.completionPerCall,
		f.promptPerCall+f.completionPerCall)
}

func (f *fakeIterativeModel) Generate(ctx context.Context, msgs []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordUsage(ctx)
	f.genCalls = append(f.genCalls, firstContent(msgs))
	if len(f.generated) == 0 {
		return nil, fmt.Errorf("fake: no queued generate reply")
	}
	out := f.generated[0]
	f.generated = f.generated[1:]
	return schema.AssistantMessage(out, nil), nil
}

func (f *fakeIterativeModel) Stream(ctx context.Context, msgs []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordUsage(ctx)
	f.streamCalls = append(f.streamCalls, firstContent(msgs))
	if len(f.planner) == 0 {
		return nil, fmt.Errorf("fake: no queued planner reply")
	}
	out := f.planner[0]
	f.planner = f.planner[1:]
	return schema.StreamReaderFromArray([]*schema.Message{out}), nil
}

// WithTools returns the same instance: the queues are shared so a test controls the
// exact call order, and the binding itself is not what is under test here.
func (f *fakeIterativeModel) WithTools(_ []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return f, nil
}

func (f *fakeIterativeModel) prompts() (streams, generates []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.streamCalls...), append([]string(nil), f.genCalls...)
}

func firstContent(msgs []*schema.Message) string {
	if len(msgs) == 0 || msgs[0] == nil {
		return ""
	}
	return msgs[0].Content
}

// fakeRetrievalTool is a corpus tool with a canned payload. Its output carries
// doc_name / chunk_id attributes so the shared ledger instrumentation records it
// exactly as it records a real tool's.
type fakeRetrievalTool struct {
	name  string
	out   string
	calls []string
}

func (t *fakeRetrievalTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name, Desc: "fake retrieval tool"}, nil
}

func (t *fakeRetrievalTool) InvokableRun(_ context.Context, args string, _ ...tool.Option) (string, error) {
	t.calls = append(t.calls, args)
	return t.out, nil
}

// ---------------------------------------------------------------------------
// Config fixtures
// ---------------------------------------------------------------------------

// writeIterativeConfig writes a config file and points the loader at it. Distinct
// temp paths make the loader reload without any explicit cache reset (it keys on
// the path), and t.Setenv keeps the switch scoped to the test.
func writeIterativeConfig(t *testing.T, yaml string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "iterative_synthesis.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	t.Setenv("ITERATIVE_SYNTHESIS_CONFIG", path)
}

// iterativeYAML builds a runnable config whose prompts are labelled, so a test can
// tell which role was shown what without depending on the shipped wording.
//
// rubricExtra supplies the `rubric:` block's OWN keys (enabled / threshold / ...);
// the scoring prompt is always present, because a config the engine cannot run at all
// is a different test (see TestIterativeConfigRejectsMissingRolePrompt).
func iterativeYAML(t *testing.T, maxRounds int, rubricExtra string) string {
	t.Helper()
	return fmt.Sprintf(`max_search_rounds: %d
tools: []
rubric:
%s  content: |
    J|{{QUESTION}}|{{SUMMARY}}
planner:
  content: |
    P|{{QUESTION}}|{{SUMMARY}}|{{LEDGER_DELTA}}|{{SERVED_UNREAD}}|{{ROUND}}/{{MAX_ROUNDS}}
synthesizer:
  content: |
    S|{{QUESTION}}|{{SUMMARY}}|{{THINK}}|{{QUERIES}}|{{DOC_COUNT}}|{{TOOL_RESPONSE}}
commit:
  content: |
    C|{{QUESTION}}|{{SUMMARY}}
rewrite:
  content: |
    R|{{SUMMARY}}|{{FINDINGS}}
`, maxRounds, rubricExtra)
}

// ---------------------------------------------------------------------------
// Config loading
// ---------------------------------------------------------------------------

// TestShippedIterativeConfigLoads guards the wiring against the SHIPPED file: every
// placeholder the engine substitutes must exist in the template that receives it.
// A typo would otherwise ship as a literal `{{TOOL_RESPONSE}}` in the prompt, which
// no other test would notice.
func TestShippedIterativeConfigLoads(t *testing.T) {
	t.Setenv("ITERATIVE_SYNTHESIS_CONFIG", filepath.Join("..", "..", "conf", "iterative_synthesis.yaml"))

	cfg, err := loadIterativeConfig()
	if err != nil {
		t.Fatalf("load shipped config: %v", err)
	}
	if cfg.MaxSearchRounds != defaultIterativeRounds {
		t.Errorf("max_search_rounds = %d, want %d", cfg.MaxSearchRounds, defaultIterativeRounds)
	}
	if len(cfg.Tools) == 0 {
		t.Error("the shipped config must name the Planner's toolset")
	}
	if !cfg.rubricEnabled() {
		t.Error("the shipped config enables the rubric gate (inspiration 6); disabled would be an opt-out")
	}
	if cfg.Rubric.Threshold <= 0 || cfg.Rubric.MaxRewrites <= 0 {
		t.Errorf("rubric threshold/max_rewrites must be positive, got %v/%d", cfg.Rubric.Threshold, cfg.Rubric.MaxRewrites)
	}
	if cfg.Rubric.Temperature == nil || *cfg.Rubric.Temperature != 0 {
		t.Errorf("the judge's sampling must be pinned at 0 (machine-parsed verdict), got %v", cfg.Rubric.Temperature)
	}
	for _, tc := range []struct {
		role         string
		content      string
		placeholders []string
	}{
		{"planner", cfg.Planner.Content,
			[]string{"{{QUESTION}}", "{{SUMMARY}}", "{{LEDGER_DELTA}}", "{{SERVED_UNREAD}}", "{{ROUND}}", "{{MAX_ROUNDS}}"}},
		{"synthesizer", cfg.Synthesizer.Content,
			[]string{"{{QUESTION}}", "{{SUMMARY}}", "{{THINK}}", "{{QUERIES}}", "{{DOC_COUNT}}", "{{TOOL_RESPONSE}}", "<summary>"}},
		{"commit", cfg.Commit.Content, []string{"{{QUESTION}}", "{{SUMMARY}}", "Final Answer"}},
		{"rubric", cfg.Rubric.Content, []string{"{{QUESTION}}", "{{SUMMARY}}", "entity_anchor_integrity", "\"scores\""}},
		{"rewrite", cfg.Rewrite.Content, []string{"{{QUESTION}}", "{{SUMMARY}}", "{{FINDINGS}}", "<summary>"}},
	} {
		if strings.TrimSpace(tc.content) == "" {
			t.Errorf("%s: role prompt is empty", tc.role)
			continue
		}
		for _, ph := range tc.placeholders {
			if !strings.Contains(tc.content, ph) {
				t.Errorf("%s: prompt is missing %q", tc.role, ph)
			}
		}
	}
	// The summary must keep RAGFlow's deliverable sections: the citation payload,
	// the UI and hasFOSStructure all key on them, so the Synthesizer's contract has
	// to name them.
	for _, want := range []string{"## Candidate Matrix", "## Reasoning Chain", "chunk_id:"} {
		if !strings.Contains(cfg.Synthesizer.Content, want) {
			t.Errorf("the synthesizer's summary contract must carry %q", want)
		}
	}
}

func TestIterativeConfigRejectsMissingRolePrompt(t *testing.T) {
	for _, tc := range []struct {
		name string
		yaml string
	}{
		{"missing planner", "synthesizer:\n  content: S\ncommit:\n  content: C\n"},
		{"missing synthesizer", "planner:\n  content: P\ncommit:\n  content: C\n"},
		{"missing commit", "planner:\n  content: P\nsynthesizer:\n  content: S\n"},
		{"missing rubric prompt while the gate is on", "planner:\n  content: P\nsynthesizer:\n  content: S\ncommit:\n  content: C\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeIterativeConfig(t, tc.yaml)
			if _, err := loadIterativeConfig(); err == nil {
				t.Fatal("a config the engine cannot run must fail loudly")
			}
		})
	}
}

// TestIterativeRubricDisabledNeedsNoJudgePrompt pins the opt-out: a disabled gate is
// never invoked, so its prompt is not required — but the roles that ARE invoked
// always are.
func TestIterativeRubricDisabledNeedsNoJudgePrompt(t *testing.T) {
	writeIterativeConfig(t, `max_search_rounds: 2
rubric:
  enabled: false
planner:
  content: P
synthesizer:
  content: S
commit:
  content: C
`)
	cfg, err := loadIterativeConfig()
	if err != nil {
		t.Fatalf("a disabled gate must not require a judge prompt: %v", err)
	}
	if cfg.rubricEnabled() {
		t.Error("enabled: false must disable the gate")
	}
	if cfg.MaxSearchRounds != 2 {
		t.Errorf("max_search_rounds = %d, want the file's 2", cfg.MaxSearchRounds)
	}
}

// TestIterativeRubricDefaults pins "absent means on with shipped budgets": the gate
// is this mode's training-free benefit, so silence must not turn it off.
func TestIterativeRubricDefaults(t *testing.T) {
	writeIterativeConfig(t, "planner:\n  content: P\nsynthesizer:\n  content: S\ncommit:\n  content: C\nrubric:\n  content: J\nrewrite:\n  content: R\n")
	cfg, err := loadIterativeConfig()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.rubricEnabled() {
		t.Error("an omitted rubric block means enabled")
	}
	if cfg.MaxSearchRounds != defaultIterativeRounds {
		t.Errorf("max_search_rounds = %d, want the default %d", cfg.MaxSearchRounds, defaultIterativeRounds)
	}
	if cfg.Rubric.EveryRounds != 1 || cfg.Rubric.Threshold != 3.0 || cfg.Rubric.MaxRewrites != 5 {
		t.Errorf("rubric defaults = every %d / threshold %v / max %d, want 1 / 3.0 / 5",
			cfg.Rubric.EveryRounds, cfg.Rubric.Threshold, cfg.Rubric.MaxRewrites)
	}
}

func TestIterativeTemperaturesFollowTheConfig(t *testing.T) {
	writeIterativeConfig(t, `planner:
  temperature: 0.5
  content: P
synthesizer:
  temperature: 0.2
  content: S
commit:
  content: C
rubric:
  temperature: 0
  content: J
rewrite:
  content: R
`)
	temps := IterativeTemperatures()
	for _, tc := range []struct {
		role string
		got  *float64
		want float64
	}{
		{"planner", temps.Planner, 0.5},
		{"synthesizer", temps.Synthesizer, 0.2},
		{"rubric", temps.Rubric, 0},
	} {
		if tc.got == nil || *tc.got != tc.want {
			t.Errorf("%s temperature = %v, want %v", tc.role, tc.got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Pure helpers
// ---------------------------------------------------------------------------

func TestFillTemplate(t *testing.T) {
	got := fillTemplate("A {{X}} B {{Y}} {{X}}", map[string]string{"X": "1", "Y": "2"})
	if got != "A 1 B 2 1" {
		t.Errorf("fillTemplate = %q", got)
	}
	// An unknown placeholder is left verbatim rather than emptied: a template that
	// names something the engine does not provide should be visible, not silent.
	if got := fillTemplate("keep {{NOPE}}", map[string]string{"X": "1"}); got != "keep {{NOPE}}" {
		t.Errorf("unknown placeholders must survive, got %q", got)
	}
}

func TestParseSummaryOutput(t *testing.T) {
	ctx := t.Context()
	if got := parseSummaryOutput(ctx, "<summary>  STATE  </summary>", 1); got != "STATE" {
		t.Errorf("tagged output = %q, want STATE", got)
	}
	// IterSynth's fallback: no tag means the whole reply becomes the state, because
	// dropping the round's work is worse than keeping a noisy summary.
	if got := parseSummaryOutput(ctx, "## Candidate Matrix\nFinal Answer: **1**", 1); !strings.Contains(got, "Final Answer") {
		t.Errorf("untagged output must fall back to verbatim, got %q", got)
	}
	if got := parseSummaryOutput(ctx, "   ", 1); got != "" {
		t.Errorf("blank output = %q, want empty", got)
	}
}

func TestParseRubricVerdict(t *testing.T) {
	// A judge that fences its JSON is still usable.
	ok := "Some prose first.\n```json\n{\"scores\":{\"entity_anchor_integrity\":5},\"findings\":[\"f1\"]}\n```\n"
	v, err := parseRubricVerdict(ok)
	if err != nil {
		t.Fatalf("fenced JSON must parse: %v", err)
	}
	if v.Scores["entity_anchor_integrity"] != 5 || len(v.Findings) != 1 {
		t.Errorf("verdict = %+v", v)
	}
	for _, tc := range []struct{ name, raw string }{
		{"no object", "I could not score this."},
		{"empty scores", `{"scores":{},"findings":[]}`},
		{"malformed", `{"scores":`},
	} {
		if _, err := parseRubricVerdict(tc.raw); err == nil {
			t.Errorf("%s: must fail, not score", tc.name)
		}
	}
}

// TestWeightedRubricScore pins the reduction and its two guards: omitted dimensions
// leave the ratio alone (a partial reply must not read as a failure), and a verdict
// scoring nothing the rubric knows is not a score at all.
func TestWeightedRubricScore(t *testing.T) {
	full := map[string]float64{
		"entity_anchor_integrity":                   5,
		"conflict_flagging_required":                5,
		"gold_evidence_salience":                    5,
		"claim_citation_grounding":                  5,
		"subconstraint_coverage_and_disambiguation": 5,
	}
	if got, ok := weightedRubricScore(full); !ok || got != 5 {
		t.Errorf("all-5 = (%v, %v), want (5, true)", got, ok)
	}
	if got, ok := weightedRubricScore(map[string]float64{"entity_anchor_integrity": 1}); !ok || got != 1 {
		t.Errorf("a single scored dimension = (%v, %v), want (1, true)", got, ok)
	}
	if _, ok := weightedRubricScore(map[string]float64{"not_a_dimension": 5}); ok {
		t.Error("a verdict that scored no known dimension must not produce a score")
	}
	// Weights matter: a 5 on the weight-5 dimension outranks a 5 on a weight-3 one.
	anchorOnly := map[string]float64{"entity_anchor_integrity": 5, "conflict_flagging_required": 1}
	weighted, ok := weightedRubricScore(anchorOnly)
	if !ok || weighted != (5*5+1*3)/8.0 {
		t.Errorf("weighted score = (%v, %v), want (%v, true)", weighted, ok, (5*5+1*3)/8.0)
	}
}

func TestRenderLedgerDelta(t *testing.T) {
	if got := renderLedgerDelta(nil, 0, 10); got != iterativeLedgerEmpty {
		t.Errorf("nil ledger = %q, want the empty placeholder", got)
	}
	ledger := NewSearchLedger()
	if got := renderLedgerDelta(ledger, 0, 10); got != iterativeLedgerEmpty {
		t.Errorf("empty ledger = %q, want the empty placeholder", got)
	}
	ledger.Add("grep_chunks", `{"query":"alpha"}`)
	ledger.Add("search_chunks", `{"query":"beta"}`)

	// The cursor is the whole point: the second Planner must see the action the first
	// one ran, and must still be told about it after one more round.
	got := renderLedgerDelta(ledger, 0, 10)
	if !strings.Contains(got, "#1 grep_chunks") || !strings.Contains(got, "#2 search_chunks") {
		t.Errorf("after=0 must render the whole ledger, got %q", got)
	}
	if got := renderLedgerDelta(ledger, 1, 10); strings.Contains(got, "#1 ") || !strings.Contains(got, "#2 ") {
		t.Errorf("after=1 must render only entry 2, got %q", got)
	}
	if got := renderLedgerDelta(ledger, 2, 10); got != iterativeLedgerEmpty {
		t.Errorf("after=2 = %q, want the empty placeholder", got)
	}
	// An overflow is announced, never silent: a dropped entry would read as "this
	// was never searched", which is the misreading the delta exists to prevent.
	got = renderLedgerDelta(ledger, 0, 1)
	if !strings.Contains(got, "omitted") {
		t.Errorf("a capped delta must say how much it omitted, got %q", got)
	}
}

func TestRenderServedUnread(t *testing.T) {
	if got := renderServedUnread(nil, nil, 5); got != iterativeUnreadEmpty {
		t.Errorf("no ledgers = %q, want the empty placeholder", got)
	}
	served := NewServedLedger()
	deep := NewDocIDLedger()
	// Two DISTINCT queries served doc-a, one served doc-b; doc-b was then deep-read.
	served.Add("q1", "doc-a")
	served.Add("q2", "doc-a")
	served.Add("q3", "doc-b")
	deep.Add("doc-b")

	got := renderServedUnread(served, deep, 5)
	if !strings.Contains(got, "doc-a (x2)") {
		t.Errorf("served-unread must name doc-a with its distinct-query serve count, got %q", got)
	}
	if strings.Contains(got, "doc-b") {
		t.Errorf("a deep-read document is not a lead, got %q", got)
	}
}

func TestPlannerThink(t *testing.T) {
	// The native reasoning channel wins.
	if got := plannerThink(&schema.Message{ReasoningContent: "native", Content: "<think>tagged</think>"}); got != "native" {
		t.Errorf("reasoning channel = %q, want native", got)
	}
	if got := plannerThink(&schema.Message{Content: "before <think>tagged</think> after"}); got != "tagged" {
		t.Errorf("think tag = %q, want tagged", got)
	}
	// Prose with no tags still told the Synthesizer something.
	if got := plannerThink(&schema.Message{Content: "just prose"}); got != "just prose" {
		t.Errorf("untagged prose = %q, want it kept", got)
	}
	if got := plannerThink(nil); got != "" {
		t.Errorf("nil message = %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// The loop
// ---------------------------------------------------------------------------

// toolCall builds a Planner tool call with an explicit stream Index: the stream merge
// treats a nil Index as 0, so two calls without one would collapse into the first.
func toolCall(idx int, name, args string) schema.ToolCall {
	i := idx
	return schema.ToolCall{
		Index:    &i,
		ID:       fmt.Sprintf("call-%d", idx),
		Type:     "function",
		Function: schema.FunctionCall{Name: name, Arguments: args},
	}
}

// TestRunIterativeRebuildsBoundedContext is the mode's central contract (inspiration
// 1): the second Planner round is rebuilt from `(question, summary, ledger delta)`
// and must therefore contain the summary while containing NEITHER the previous
// round's raw tool output NOR the previous Planner's reasoning. The Synthesizer, by
// contrast, is the one role that sees the raw output — exactly once.
func TestRunIterativeRebuildsBoundedContext(t *testing.T) {
	writeIterativeConfig(t, iterativeYAML(t, 3, "  enabled: false\n"))
	grepTool := &fakeRetrievalTool{
		name: "grep_chunks",
		out:  `<chunks><chunk chunk_id="c1" doc_name="alpha.md">RAWTOOLOUTPUT</chunk></chunks>`,
	}

	round1 := schema.AssistantMessage("<think>ROUND1THINK</think>", []schema.ToolCall{toolCall(0, "grep_chunks", `{"query":"alpha"}`)})
	round2 := schema.AssistantMessage("research complete", nil)
	fake := &fakeIterativeModel{
		planner:   []*schema.Message{round1, round2},
		generated: []string{"<summary>## Candidate Matrix\n- Tested: x\n## Reasoning Chain\n- step\nSUMSUMMARY\nFinal Answer: **42**</summary>"},
	}

	final, err := RunIterative(t.Context(), IterativeInput{
		Model:      fake,
		Stream:     true,
		Messages:   []*schema.Message{schema.UserMessage("QUESTION")},
		Tools:      []tool.BaseTool{grepTool},
		TenantID:   "t1",
		DatasetIDs: []string{"kb1"},
	})
	if err != nil {
		t.Fatalf("RunIterative: %v", err)
	}
	if !strings.Contains(final, "Final Answer: **42**") {
		t.Errorf("final = %q, want the summary's answer line", final)
	}
	if len(grepTool.calls) != 1 || grepTool.calls[0] != `{"query":"alpha"}` {
		t.Errorf("tool calls = %v, want the Planner's one call", grepTool.calls)
	}

	streams, generates := fake.prompts()
	if len(streams) != 2 {
		t.Fatalf("planner rounds = %d, want 2", len(streams))
	}
	if !strings.Contains(streams[0], "QUESTION") || !strings.Contains(streams[0], iterativeStateEmpty) {
		t.Errorf("round 1 must show the question and the empty-state placeholder:\n%s", streams[0])
	}
	// Round 2: the summary is in, the raw output and the previous think are not.
	for _, want := range []string{"QUESTION", "SUMSUMMARY"} {
		if !strings.Contains(streams[1], want) {
			t.Errorf("round 2 prompt must carry %q:\n%s", want, streams[1])
		}
	}
	for _, gone := range []string{"RAWTOOLOUTPUT", "ROUND1THINK"} {
		if strings.Contains(streams[1], gone) {
			t.Errorf("round 2 prompt must NOT carry %q — the context is rebuilt, not replayed:\n%s", gone, streams[1])
		}
	}
	// The ledger delta reaches the Planner that decides what to search next.
	if !strings.Contains(streams[1], "grep_chunks") || !strings.Contains(streams[1], `{"query":"alpha"}`) {
		t.Errorf("round 2 must be told what round 1 already searched:\n%s", streams[1])
	}

	if len(generates) != 1 {
		t.Fatalf("synthesizer calls = %d, want 1", len(generates))
	}
	for _, want := range []string{"RAWTOOLOUTPUT", "ROUND1THINK", "QUESTION"} {
		if !strings.Contains(generates[0], want) {
			t.Errorf("the synthesizer's prompt must carry %q:\n%s", want, generates[0])
		}
	}
	// The document count is derived from the rendered output, not read from a
	// metrics key nobody sets (IterSynth's own doc_count bug, report §3.3).
	if !strings.Contains(generates[0], "|1|") {
		t.Errorf("the synthesizer must be told 1 document was rendered:\n%s", generates[0])
	}
}

// TestRunIterativeLedgerDeltaIsPerRound pins the cursor arithmetic: each Planner sees
// the actions run SINCE ITS OWN LAST TURN, so a long run's delta stays small without
// hiding what the previous round did.
func TestRunIterativeLedgerDeltaIsPerRound(t *testing.T) {
	writeIterativeConfig(t, iterativeYAML(t, 3, "  enabled: false\n"))
	grepTool := &fakeRetrievalTool{name: "grep_chunks", out: `<chunks><chunk chunk_id="c1" doc_name="a.md">x</chunk></chunks>`}
	fake := &fakeIterativeModel{
		planner: []*schema.Message{
			schema.AssistantMessage("", []schema.ToolCall{toolCall(0, "grep_chunks", `{"query":"FIRST"}`)}),
			schema.AssistantMessage("", []schema.ToolCall{toolCall(0, "grep_chunks", `{"query":"SECOND"}`)}),
			schema.AssistantMessage("stop", nil),
		},
		generated: []string{
			"<summary>one\nFinal Answer: **1**</summary>",
			"<summary>two\nFinal Answer: **2**</summary>",
		},
	}
	if _, err := RunIterative(t.Context(), IterativeInput{
		Model:    fake,
		Stream:   true,
		Messages: []*schema.Message{schema.UserMessage("Q")},
		Tools:    []tool.BaseTool{grepTool},
	}); err != nil {
		t.Fatalf("RunIterative: %v", err)
	}

	streams, _ := fake.prompts()
	if len(streams) != 3 {
		t.Fatalf("planner rounds = %d, want 3", len(streams))
	}
	if strings.Contains(streams[0], "FIRST") {
		t.Error("round 1 must see an empty delta — nothing has run yet")
	}
	if !strings.Contains(streams[1], "FIRST") || strings.Contains(streams[1], "SECOND") {
		t.Errorf("round 2 must see round 1's action and only that:\n%s", streams[1])
	}
	if !strings.Contains(streams[2], "SECOND") {
		t.Errorf("round 3 must see round 2's action:\n%s", streams[2])
	}
}

// TestRunIterativeRubricBuysOneRewrite pins the online gate (inspirations 4 and 6):
// a below-threshold score buys exactly one rewrite, the rewrite gets the judge's
// findings, and it is the REWRITTEN text that ships.
func TestRunIterativeRubricBuysOneRewrite(t *testing.T) {
	writeIterativeConfig(t, iterativeYAML(t, 3, "  enabled: true\n  threshold: 3.0\n  every_rounds: 1\n  max_rewrites: 5\n"))
	low := `{"scores":{"entity_anchor_integrity":1,"conflict_flagging_required":1,"gold_evidence_salience":1,"claim_citation_grounding":1,"subconstraint_coverage_and_disambiguation":1},"findings":["citation is unverifiable"]}`
	fake := &fakeIterativeModel{
		planner: []*schema.Message{
			schema.AssistantMessage("", []schema.ToolCall{toolCall(0, "grep_chunks", `{"query":"alpha"}`)}),
			schema.AssistantMessage("stop", nil),
		},
		generated: []string{
			"<summary>## Candidate Matrix\n- Tested: x (chunk_id: c1)\n## Reasoning Chain\n- step (chunk_id: c1)\nDRAFT\nFinal Answer: **draft**</summary>", // synthesizer
			low, // judge
			"<summary>## Candidate Matrix\n- Tested: x (chunk_id: c1)\n## Reasoning Chain\n- step (chunk_id: c1)\nREWRITTEN\nFinal Answer: **final**</summary>", // rewrite
		},
	}
	grepTool := &fakeRetrievalTool{name: "grep_chunks", out: `<chunks><chunk chunk_id="c1" doc_name="a.md">x</chunk></chunks>`}
	final, err := RunIterative(t.Context(), IterativeInput{
		Model:    fake,
		Stream:   true,
		Messages: []*schema.Message{schema.UserMessage("Q")},
		Tools:    []tool.BaseTool{grepTool},
	})
	if err != nil {
		t.Fatalf("RunIterative: %v", err)
	}
	if !strings.Contains(final, "REWRITTEN") {
		t.Errorf("final = %q, want the rewritten summary to ship", final)
	}
	_, generates := fake.prompts()
	if len(generates) != 3 {
		t.Fatalf("generate calls = %d, want synth + judge + rewrite", len(generates))
	}
	if !strings.Contains(generates[1], "J|") {
		t.Errorf("call 2 must be the judge:\n%s", generates[1])
	}
	if !strings.Contains(generates[2], "citation is unverifiable") {
		t.Errorf("the rewrite must receive the judge's findings:\n%s", generates[2])
	}
}

// TestRunIterativeRubricPassesWithoutRewrite pins the other side of the gate: a
// summary at or above the threshold ships untouched, so the extra model call is the
// only cost. Without this, "always rewrite" would pass the test above.
func TestRunIterativeRubricPassesWithoutRewrite(t *testing.T) {
	writeIterativeConfig(t, iterativeYAML(t, 3, "  enabled: true\n  threshold: 3.0\n"))
	high := `{"scores":{"entity_anchor_integrity":5,"conflict_flagging_required":5,"gold_evidence_salience":5,"claim_citation_grounding":5,"subconstraint_coverage_and_disambiguation":5},"findings":[]}`
	fake := &fakeIterativeModel{
		planner: []*schema.Message{
			schema.AssistantMessage("", []schema.ToolCall{toolCall(0, "grep_chunks", `{"query":"a"}`)}),
			schema.AssistantMessage("stop", nil),
		},
		generated: []string{
			"<summary>## Candidate Matrix\n- Tested: x (chunk_id: c1)\n## Reasoning Chain\n- step (chunk_id: c1)\nDRAFT\nFinal Answer: **draft**</summary>",
			high,
		},
	}
	grepTool := &fakeRetrievalTool{name: "grep_chunks", out: `<chunks><chunk chunk_id="c1" doc_name="a.md">x</chunk></chunks>`}
	final, err := RunIterative(t.Context(), IterativeInput{
		Model:    fake,
		Stream:   true,
		Messages: []*schema.Message{schema.UserMessage("Q")},
		Tools:    []tool.BaseTool{grepTool},
	})
	if err != nil {
		t.Fatalf("RunIterative: %v", err)
	}
	if !strings.Contains(final, "DRAFT") {
		t.Errorf("a passing summary must ship untouched, got %q", final)
	}
	_, generates := fake.prompts()
	if len(generates) != 2 {
		t.Fatalf("generate calls = %d, want synth + judge only", len(generates))
	}
}

// TestRunIterativeCommitsWhenBudgetIsSpent: the loop's product is a summary, and a run
// that spent every round still has to answer. The commit pass re-renders the standing
// summary instead of discarding the evidence gathered.
func TestRunIterativeCommitsWhenBudgetIsSpent(t *testing.T) {
	writeIterativeConfig(t, iterativeYAML(t, 1, "  enabled: false\n"))
	fake := &fakeIterativeModel{
		planner: []*schema.Message{
			schema.AssistantMessage("", []schema.ToolCall{toolCall(0, "grep_chunks", `{"query":"a"}`)}),
		},
		generated: []string{
			"<summary>## Candidate Matrix\n- Tested: x (chunk_id: c1)\nNO ANSWER YET</summary>",                                                          // synthesizer
			"<summary>## Candidate Matrix\n- Tested: x (chunk_id: c1)\n## Reasoning Chain\n- step (chunk_id: c1)\nFinal Answer: **committed**</summary>", // commit
		},
	}
	grepTool := &fakeRetrievalTool{name: "grep_chunks", out: `<chunks><chunk chunk_id="c1" doc_name="a.md">x</chunk></chunks>`}
	final, err := RunIterative(t.Context(), IterativeInput{
		Model:    fake,
		Stream:   true,
		Messages: []*schema.Message{schema.UserMessage("Q")},
		Tools:    []tool.BaseTool{grepTool},
	})
	if err != nil {
		t.Fatalf("RunIterative: %v", err)
	}
	if !strings.Contains(final, "Final Answer: **committed**") {
		t.Errorf("final = %q, want the commit pass's answer", final)
	}
	_, generates := fake.prompts()
	if len(generates) != 2 || !strings.Contains(generates[1], "C|") {
		t.Fatalf("the commit pass must run when the budget is spent, generates=%v", generates)
	}
}

// TestRunIterativeCommitsWhenStructureIsLost pins the second half of the commit
// condition: an answer line without the deliverable's sections is not shippable, since
// the citation payload and the UI both key on `## Candidate Matrix` / `## Reasoning
// Chain`.
func TestRunIterativeCommitsWhenStructureIsLost(t *testing.T) {
	writeIterativeConfig(t, iterativeYAML(t, 1, "  enabled: false\n"))
	fake := &fakeIterativeModel{
		planner: []*schema.Message{
			schema.AssistantMessage("", []schema.ToolCall{toolCall(0, "grep_chunks", `{"query":"a"}`)}),
		},
		generated: []string{
			"<summary>Final Answer: **bare**</summary>",
			"<summary>## Candidate Matrix\n- Tested: x\n## Reasoning Chain\n- step\nFinal Answer: **structured**</summary>",
		},
	}
	grepTool := &fakeRetrievalTool{name: "grep_chunks", out: `<chunks><chunk chunk_id="c1" doc_name="a.md">x</chunk></chunks>`}
	final, err := RunIterative(t.Context(), IterativeInput{
		Model:    fake,
		Stream:   true,
		Messages: []*schema.Message{schema.UserMessage("Q")},
		Tools:    []tool.BaseTool{grepTool},
	})
	if err != nil {
		t.Fatalf("RunIterative: %v", err)
	}
	if !hasFOSStructure(final) || !strings.Contains(final, "structured") {
		t.Errorf("final = %q, want the structured commit result", final)
	}
}

// TestRunIterativeNeverShipsWithoutAnAnswerLine: if even the commit pass fails, the
// turn still carries a label — the UI, the citation payload and label governance all
// read it, and its absence turns a finished investigation into an unanswerable row.
func TestRunIterativeNeverShipsWithoutAnAnswerLine(t *testing.T) {
	writeIterativeConfig(t, iterativeYAML(t, 1, "  enabled: false\n"))
	fake := &fakeIterativeModel{
		planner: []*schema.Message{
			schema.AssistantMessage("", []schema.ToolCall{toolCall(0, "grep_chunks", `{"query":"a"}`)}),
		},
		generated: []string{
			"<summary>## Candidate Matrix\n- nothing</summary>",
			"still no answer here", // the commit pass fails to comply
		},
	}
	grepTool := &fakeRetrievalTool{name: "grep_chunks", out: `<chunks><chunk chunk_id="c1" doc_name="a.md">x</chunk></chunks>`}
	final, err := RunIterative(t.Context(), IterativeInput{
		Model:    fake,
		Stream:   true,
		Messages: []*schema.Message{schema.UserMessage("Q")},
		Tools:    []tool.BaseTool{grepTool},
	})
	if err != nil {
		t.Fatalf("RunIterative: %v", err)
	}
	if !hasAnswerLine(final) {
		t.Errorf("final must always carry an answer label, got %q", final)
	}
}

// TestRunIterativeStopOnEmptySummaryKeepsThePlannersText: a Planner that stops on
// round 1 answered from nothing, which is what a purely conversational turn looks
// like. The text is kept as the commit pass's seed rather than discarded, so the turn
// ships something instead of the empty-state placeholder.
func TestRunIterativeStopOnEmptySummaryKeepsThePlannersText(t *testing.T) {
	writeIterativeConfig(t, iterativeYAML(t, 3, "  enabled: false\n"))
	fake := &fakeIterativeModel{
		planner: []*schema.Message{schema.AssistantMessage("Hello, how can I help?", nil)},
		generated: []string{
			"<summary>## Candidate Matrix\n## Reasoning Chain\nFinal Answer: **greeting**</summary>",
		},
	}
	final, err := RunIterative(t.Context(), IterativeInput{
		Model:    fake,
		Stream:   true,
		Messages: []*schema.Message{schema.UserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("RunIterative: %v", err)
	}
	if !strings.Contains(final, "greeting") {
		t.Errorf("final = %q, want the commit pass's rendering", final)
	}
	_, generates := fake.prompts()
	if len(generates) != 1 || !strings.Contains(generates[0], "Hello, how can I help?") {
		t.Errorf("the Planner's text must seed the commit pass, generates=%v", generates)
	}
}

// TestRunIterativePlannerFailureStillShipsTheStandingSummary: a model outage mid-run
// must not lose the state the Synthesizer already built.
func TestRunIterativePlannerFailureStillShipsTheStandingSummary(t *testing.T) {
	writeIterativeConfig(t, iterativeYAML(t, 3, "  enabled: false\n"))
	// One planner round, then the queue runs dry and the stream errors.
	fake := &fakeIterativeModel{
		planner: []*schema.Message{
			schema.AssistantMessage("", []schema.ToolCall{toolCall(0, "grep_chunks", `{"query":"a"}`)}),
		},
		generated: []string{
			"<summary>## Candidate Matrix\n## Reasoning Chain\nFinal Answer: **kept**</summary>",
		},
	}
	grepTool := &fakeRetrievalTool{name: "grep_chunks", out: `<chunks><chunk chunk_id="c1" doc_name="a.md">x</chunk></chunks>`}
	final, err := RunIterative(t.Context(), IterativeInput{
		Model:    fake,
		Stream:   true,
		Messages: []*schema.Message{schema.UserMessage("Q")},
		Tools:    []tool.BaseTool{grepTool},
	})
	if err == nil {
		t.Error("a planner outage must surface as a run error (degraded, not silent)")
	}
	if !strings.Contains(final, "Final Answer: **kept**") {
		t.Errorf("final = %q, want the standing summary", final)
	}
}

func TestRunIterativeRejectsBadInput(t *testing.T) {
	t.Run("no model", func(t *testing.T) {
		if _, err := RunIterative(t.Context(), IterativeInput{}); err == nil {
			t.Error("a nil model must fail")
		}
	})
	t.Run("no question", func(t *testing.T) {
		writeIterativeConfig(t, iterativeYAML(t, 3, "  enabled: false\n"))
		_, err := RunIterative(t.Context(), IterativeInput{
			Model:    &fakeIterativeModel{},
			Messages: []*schema.Message{schema.AssistantMessage("no user turn", nil)},
		})
		if err == nil || !strings.Contains(err.Error(), "no user question") {
			t.Errorf("err = %v, want a missing-question error", err)
		}
	})
	t.Run("missing config", func(t *testing.T) {
		t.Setenv("ITERATIVE_SYNTHESIS_CONFIG", filepath.Join(t.TempDir(), "does-not-exist.yaml"))
		_, err := RunIterative(t.Context(), IterativeInput{
			Model:    &fakeIterativeModel{},
			Messages: []*schema.Message{schema.UserMessage("Q")},
		})
		if err == nil || !strings.Contains(err.Error(), "read") {
			t.Errorf("err = %v, want a config read error", err)
		}
	})
}

// TestRunIterativeNonStreamingPlannerUsesGenerate pins what Stream selects: false
// issues one blocking Planner call per round (no live reasoning), true streams it.
// Without this, the field would be accepted and ignored.
func TestRunIterativeNonStreamingPlannerUsesGenerate(t *testing.T) {
	writeIterativeConfig(t, iterativeYAML(t, 3, "  enabled: false\n"))
	// A generated Planner reply carries its tool call directly: the blocking path
	// returns the message as-is, with no stream merge to assemble it from.
	seq := &sequenceModel{
		plan:    schema.AssistantMessage("<think>PLANNERTHINK</think>", []schema.ToolCall{toolCall(0, "grep_chunks", `{"query":"a"}`)}),
		summary: "<summary>## Candidate Matrix\n- Tested: x\n## Reasoning Chain\n- step\nFinal Answer: **7**</summary>",
	}
	grepTool := &fakeRetrievalTool{name: "grep_chunks", out: `<chunks><chunk chunk_id="c1" doc_name="a.md">x</chunk></chunks>`}
	final, err := RunIterative(t.Context(), IterativeInput{
		Model:    seq,
		Messages: []*schema.Message{schema.UserMessage("Q")},
		Tools:    []tool.BaseTool{grepTool},
	})
	if err != nil {
		t.Fatalf("RunIterative: %v", err)
	}
	if len(seq.streamCalls) != 0 {
		t.Errorf("Stream=false must not open a stream, got %v", seq.streamCalls)
	}
	if !strings.Contains(final, "Final Answer: **7**") {
		t.Errorf("final = %q", final)
	}
}

// sequenceModel answers the Planner with a canned tool-call message and every other
// role with a canned summary — the shape the non-streaming path drives.
type sequenceModel struct {
	mu          sync.Mutex
	plan        *schema.Message
	summary     string
	plannerRuns int
	streamCalls []string
}

func (m *sequenceModel) Generate(_ context.Context, msgs []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// The Planner's prompt is the only one carrying the ledger placeholder.
	if strings.HasPrefix(firstContent(msgs), "P|") {
		m.plannerRuns++
		return m.plan, nil
	}
	return schema.AssistantMessage(m.summary, nil), nil
}

func (m *sequenceModel) Stream(_ context.Context, msgs []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.streamCalls = append(m.streamCalls, firstContent(msgs))
	return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("", nil)}), nil
}

func (m *sequenceModel) WithTools(_ []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}
