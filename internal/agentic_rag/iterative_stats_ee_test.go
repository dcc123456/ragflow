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
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"ragflow/internal/tokenizer"
)

// TestRunIterativeReportsPerRoundStats pins the per-round breakdown: each stat's
// tokens are a DELTA over that round (not the run total they sum to), its tool calls
// are the ones that round made, and the rubric verdict rides the round whose summary
// it scored.
func TestRunIterativeReportsPerRoundStats(t *testing.T) {
	writeIterativeConfig(t, iterativeYAML(t, 3, "  enabled: true\n  threshold: 3.0\n"))
	high := `{"scores":{"entity_anchor_integrity":5,"conflict_flagging_required":5,"gold_evidence_salience":5,"claim_citation_grounding":5,"subconstraint_coverage_and_disambiguation":5},"findings":[]}`
	fake := &fakeIterativeModel{
		promptPerCall:     100,
		completionPerCall: 10,
		planner: []*schema.Message{
			schema.AssistantMessage("", []schema.ToolCall{toolCall(0, "grep_chunks", `{"query":"a"}`)}),
			schema.AssistantMessage("done", nil),
		},
		generated: []string{
			"<summary>## Candidate Matrix\n- Tested: x (chunk_id: c1)\n## Reasoning Chain\n- step (chunk_id: c1)\nFinal Answer: **5**</summary>",
			high,
		},
	}
	grepTool := &fakeRetrievalTool{name: "grep_chunks", out: `<chunks><chunk chunk_id="c1" doc_name="a.md">x</chunk></chunks>`}

	ctx := tokenizer.WithRunUsage(t.Context())
	var stats []IterativeRoundStat
	if _, err := RunIterative(ctx, IterativeInput{
		Model:          fake,
		Stream:         true,
		Messages:       []*schema.Message{schema.UserMessage("Q")},
		Tools:          []tool.BaseTool{grepTool},
		ToolCallCounts: map[string]int{},
		OnRound:        func(st IterativeRoundStat) { stats = append(stats, st) },
	}); err != nil {
		t.Fatalf("RunIterative: %v", err)
	}

	if len(stats) != 2 {
		t.Fatalf("stats = %d entries, want one per round (no commit pass here): %+v", len(stats), stats)
	}
	r1, r2 := stats[0], stats[1]

	// Round 1: planner + synthesizer + judge = 3 calls, one tool call, one document.
	if r1.Kind != IterativeStatKindRound || r1.Round != 1 {
		t.Errorf("stat 1 = kind %q round %d, want round 1", r1.Kind, r1.Round)
	}
	if r1.LLMCalls != 3 || r1.PromptTokens != 300 || r1.CompletionTokens != 30 || r1.TotalTokens != 330 {
		t.Errorf("round 1 usage = %d calls / %d/%d/%d tokens, want 3 calls / 300/30/330",
			r1.LLMCalls, r1.PromptTokens, r1.CompletionTokens, r1.TotalTokens)
	}
	if r1.ToolCalls != 1 || r1.ToolCallsByName["grep_chunks"] != 1 {
		t.Errorf("round 1 tools = %d %v, want 1 grep_chunks", r1.ToolCalls, r1.ToolCallsByName)
	}
	if r1.Docs != 1 || r1.SummaryBytes == 0 {
		t.Errorf("round 1 docs/summary = %d/%d, want 1 doc and a non-empty summary", r1.Docs, r1.SummaryBytes)
	}
	if !r1.RubricScored || r1.RubricScore != 5 || r1.Rewritten || r1.Stopped {
		t.Errorf("round 1 rubric = scored %v score %v rewritten %v stopped %v, want scored 5 / no rewrite / no stop",
			r1.RubricScored, r1.RubricScore, r1.Rewritten, r1.Stopped)
	}
	if r1.ElapsedSeconds <= 0 {
		t.Error("round 1 must report a wall time")
	}

	// Round 2: the Planner stopped, so ONE call and no tool work. Its summary was
	// never re-scored — the loop breaks before the gate.
	if r2.Round != 2 || r2.LLMCalls != 1 || r2.PromptTokens != 100 {
		t.Errorf("round 2 = round %d / %d calls / %d prompt tokens, want 2 / 1 / 100",
			r2.Round, r2.LLMCalls, r2.PromptTokens)
	}
	if !r2.Stopped || r2.ToolCalls != 0 || r2.RubricScored {
		t.Errorf("round 2 = stopped %v tools %d rubric-scored %v, want stopped / 0 / unscored",
			r2.Stopped, r2.ToolCalls, r2.RubricScored)
	}

	// The parts must add up to the run's own total: this is the property that makes a
	// per-round breakdown trustworthy rather than decorative.
	total := 0
	for _, st := range stats {
		total += st.TotalTokens
	}
	_, _, sinkTotal, sinkCalls := tokenizer.GetRunUsage(ctx).Snapshot()
	if total != sinkTotal {
		t.Errorf("per-round tokens sum to %d, sink reports %d", total, sinkTotal)
	}
	if calls := r1.LLMCalls + r2.LLMCalls; calls != sinkCalls {
		t.Errorf("per-round calls sum to %d, sink reports %d", calls, sinkCalls)
	}
}

// TestRunIterativeReportsRewriteAndCommitStats covers the other two units of work: a
// rewrite bought by the rubric is charged to the round that spent it, and the closing
// commit pass is reported as its own entry (Kind=commit, Round=0) so a run's parts
// still add up when the loop ended mid-investigation.
func TestRunIterativeReportsRewriteAndCommitStats(t *testing.T) {
	writeIterativeConfig(t, iterativeYAML(t, 1, "  enabled: true\n  threshold: 3.0\n"))
	low := `{"scores":{"entity_anchor_integrity":1,"conflict_flagging_required":1,"gold_evidence_salience":1,"claim_citation_grounding":1,"subconstraint_coverage_and_disambiguation":1},"findings":["no citation"]}`
	fake := &fakeIterativeModel{
		promptPerCall:     50,
		completionPerCall: 5,
		planner: []*schema.Message{
			schema.AssistantMessage("", []schema.ToolCall{toolCall(0, "grep_chunks", `{"query":"a"}`)}),
		},
		generated: []string{
			"<summary>DRAFT, no answer line</summary>", // synthesizer
			low, // judge
			"<summary>REWRITTEN, still no answer line</summary>",                                                           // rewrite
			"<summary>## Candidate Matrix\n- Tested: x\n## Reasoning Chain\n- step\nFinal Answer: **committed**</summary>", // commit
		},
	}
	grepTool := &fakeRetrievalTool{name: "grep_chunks", out: `<chunks><chunk chunk_id="c1" doc_name="a.md">x</chunk></chunks>`}

	ctx := tokenizer.WithRunUsage(t.Context())
	var stats []IterativeRoundStat
	if _, err := RunIterative(ctx, IterativeInput{
		Model:          fake,
		Stream:         true,
		Messages:       []*schema.Message{schema.UserMessage("Q")},
		Tools:          []tool.BaseTool{grepTool},
		ToolCallCounts: map[string]int{},
		OnRound:        func(st IterativeRoundStat) { stats = append(stats, st) },
	}); err != nil {
		t.Fatalf("RunIterative: %v", err)
	}

	if len(stats) != 2 {
		t.Fatalf("stats = %d entries, want the round plus the commit pass: %+v", len(stats), stats)
	}
	round, commit := stats[0], stats[1]
	// planner + synthesizer + judge + rewrite = 4 calls, all charged to round 1.
	if round.LLMCalls != 4 || round.PromptTokens != 200 {
		t.Errorf("round 1 = %d calls / %d prompt tokens, want 4 / 200", round.LLMCalls, round.PromptTokens)
	}
	if !round.RubricScored || round.RubricScore != 1 || !round.Rewritten {
		t.Errorf("round 1 rubric = scored %v score %v rewritten %v, want scored 1 / rewritten",
			round.RubricScored, round.RubricScore, round.Rewritten)
	}
	if commit.Kind != IterativeStatKindCommit || commit.Round != 0 {
		t.Errorf("commit stat = kind %q round %d, want kind commit / round 0", commit.Kind, commit.Round)
	}
	if commit.LLMCalls != 1 || commit.PromptTokens != 50 {
		t.Errorf("commit = %d calls / %d prompt tokens, want 1 / 50", commit.LLMCalls, commit.PromptTokens)
	}
	if commit.ToolCalls != 0 {
		t.Errorf("the commit pass makes no tool calls, got %d", commit.ToolCalls)
	}
	if commit.SummaryBytes == 0 {
		t.Error("the commit stat must report the size of what it shipped")
	}

	total := 0
	for _, st := range stats {
		total += st.TotalTokens
	}
	_, _, sinkTotal, _ := tokenizer.GetRunUsage(ctx).Snapshot()
	if total != sinkTotal {
		t.Errorf("per-round tokens sum to %d, sink reports %d", total, sinkTotal)
	}
}

// TestRunIterativeReportsFailedRoundStats: a round that dies on a model outage still
// cost every token it burned, and a stat that hid it would make the run's total
// unexplainable.
func TestRunIterativeReportsFailedRoundStats(t *testing.T) {
	writeIterativeConfig(t, iterativeYAML(t, 3, "  enabled: false\n"))
	fake := &fakeIterativeModel{
		promptPerCall:     70,
		completionPerCall: 7,
		planner: []*schema.Message{
			schema.AssistantMessage("", []schema.ToolCall{toolCall(0, "grep_chunks", `{"query":"a"}`)}),
		},
		generated: []string{
			"<summary>## Candidate Matrix\n## Reasoning Chain\nFinal Answer: **1**</summary>",
		},
	}
	grepTool := &fakeRetrievalTool{name: "grep_chunks", out: `<chunks><chunk chunk_id="c1" doc_name="a.md">x</chunk></chunks>`}

	ctx := tokenizer.WithRunUsage(t.Context())
	var stats []IterativeRoundStat
	if _, err := RunIterative(ctx, IterativeInput{
		Model:          fake,
		Stream:         true,
		Messages:       []*schema.Message{schema.UserMessage("Q")},
		Tools:          []tool.BaseTool{grepTool},
		ToolCallCounts: map[string]int{},
		OnRound:        func(st IterativeRoundStat) { stats = append(stats, st) },
	}); err == nil {
		t.Error("the outage must still surface as a run error")
	}
	if len(stats) < 2 {
		t.Fatalf("stats = %d entries, want a stat per round including the failed one: %+v", len(stats), stats)
	}
	failed := stats[len(stats)-1]
	if failed.Round != 2 || failed.Error == "" {
		t.Errorf("the second round must be reported with its error, got %+v", failed)
	}
	if failed.LLMCalls == 0 {
		t.Error("the failed round must still report the calls it made")
	}
}

// TestRunIterativeStatsWithoutTokenSink: a caller that installed no sink gets zeros
// rather than a panic or a bogus negative delta.
func TestRunIterativeStatsWithoutTokenSink(t *testing.T) {
	writeIterativeConfig(t, iterativeYAML(t, 1, "  enabled: false\n"))
	fake := &fakeIterativeModel{
		promptPerCall: 100,
		planner: []*schema.Message{
			schema.AssistantMessage("done", nil),
		},
	}
	var stats []IterativeRoundStat
	if _, err := RunIterative(t.Context(), IterativeInput{
		Model:    fake,
		Stream:   true,
		Messages: []*schema.Message{schema.UserMessage("Q")},
		OnRound:  func(st IterativeRoundStat) { stats = append(stats, st) },
	}); err != nil {
		t.Fatalf("RunIterative: %v", err)
	}
	if len(stats) == 0 {
		t.Fatal("a run with no sink must still report its rounds")
	}
	for _, st := range stats {
		if st.TotalTokens != 0 || st.LLMCalls != 0 {
			t.Errorf("no sink must mean zero tokens, got %+v", st)
		}
	}
}
