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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"ragflow/internal/retrievalaccount"
	"regexp"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"

	"ragflow/internal/common"
	"ragflow/internal/tokenizer"
)

// # Mode 6 — Iterative Synthesis
//
// A role-decoupled, summary-based deep-search engine: the Planner decides the next
// move and never reads raw documents, the Synthesizer owns the only persistent state
// (`global_summary`), and every Planner round rebuilds its context from
// `(question, summary, ledger delta)` alone. The engine is ported from IterSynth
// (Tencent x Zhejiang University) — see itersynth-research-notes.md for the mechanism
// mapping and for which inspiration each block below implements.
//
// Why a second engine instead of a flag on Run: Run's whole design is one managed
// session replayed by the Runner (agent.go:1205-1209). Bounded context is not a
// tunable of that design, it is its negation — the trajectory never accumulates here.
// Merging the two would mean one function carrying two contradictory context
// contracts, which is exactly the liability AGENTS.md says not to preserve.
//
// The six inspirations this file implements:
//  1. Only the rewritten summary crosses a round boundary (buildPlannerPrompt).
//  2. The search ledger's delta is rendered into the Planner's input, so the model's
//     state and the ground truth are the same source (renderLedgerDelta).
//  3. The state is placed where the decision is made — served-but-unread documents
//     name themselves on the turn that decides what to search (renderServedUnread).
//  4. Quality is checked every round, not only at a terminal delivery gate
//     (judgeSummary).
//  5. One underlying model, two roles — no second weights, only a second prompt.
//  6. The rubric runs training-free as an online gate that buys a rewrite.
//
// Deliberate deviations from IterSynth, and why:
//   - Tool schemas ride the provider's native tool-calling channel, not a JSON
//     snippet inlined into the prompt. IterSynth has to keep its inlined schema in
//     sync with tool_config.yaml by hand (report §1.3); nothing is duplicated here.
//   - The summary must carry RAGFlow's deliverable sections (`## Candidate Matrix`,
//     `## Reasoning Chain`, `Final Answer:`), because the citation payload, the UI
//     and hasFOSStructure all key on them. IterSynth's §1/§2/§4 map onto extra
//     sections around them rather than replacing them.
//   - The summary is an INDEX plus verbatim snippets, not a lossy digest
//     (report §6.1): `chunk_id` anchors stay in it so the Planner can deep-read the
//     original through list_chunks whenever a verbatim quote is required.
//   - Each round's tool output is capped (iterativeToolResponseMaxRunes). Bounded
//     context is the point of this engine; an unbounded tool dump would reintroduce
//     the growth the mode exists to remove.

// iterativeStateEmpty is what a role is shown when no round has run yet. The exact
// sentence matters: it tells the model the absence is a fact about the run rather
// than a missing variable.
const iterativeStateEmpty = "No previous search has been conducted yet."

// iterativeLedgerEmpty is shown when the ledger delta carries no entry — the first
// Planner round, always.
const iterativeLedgerEmpty = "(no retrieval action has run yet)"

// iterativeUnreadEmpty is shown when no document was served-but-unread.
const iterativeUnreadEmpty = "(none)"

// iterativeToolResponseMaxRunes caps the tool output handed to the Synthesizer in
// one round. IterSynth has no such cap because its search tool returns a fixed
// result count; RAGFlow's retrieval tools render full chunk text, and a grant of
// three such results can exceed a whole round's budget — which would make the
// Synthesizer's prompt the one unbounded thing left in a bounded-context engine.
const iterativeToolResponseMaxRunes = 32000

// iterativeLedgerDeltaMaxEntries bounds how much of the search ledger the Planner
// reads in one round. The delta is cumulative-by-cursor, so a long run's tail is
// what matters; the oldest overflow is announced rather than silently dropped.
const iterativeLedgerDeltaMaxEntries = 60

// iterativeServedUnreadLimit is how many served-but-unread leads the Planner is
// shown. Mirroring mode 5's watchdog, the list is a set of leads, not a census.
const iterativeServedUnreadLimit = 5

// iterativePlanner is the model surface the Planner role needs: a chat model that can
// also be bound to tools. It is an interface for one reason — the loop is the
// product, and a fake that records prompts and replays canned role outputs is what
// makes bounded context, the ledger cursor and the rubric gate testable without a
// provider. *models.EinoChatModel satisfies it.
type iterativePlanner interface {
	model.BaseChatModel
	WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error)
}

// iterativeModel is the surface the non-planner roles need: generation only. Those
// roles never see a tool schema — the Synthesizer's action space is a summary and
// nothing else (report §1.5), and the judge's is one JSON object.
type iterativeModel = model.BaseChatModel

// Kinds of work IterativeRoundStat reports. A run's cost decomposes into rounds,
// whose count is the caller's budget, plus the ONE closing commit pass, which is not
// a round — it runs after the loop has already ended. Both ride the same list so the
// parts add up to the turn's total.
const (
	IterativeStatKindRound  = "round"
	IterativeStatKindCommit = "commit"
)

// IterativeRoundStat is one unit of mode-6 work measured end to end: what it cost in
// tokens and wall time, what it spent them on, and what it concluded.
//
// It exists because a single turn-level total cannot answer the question a long run
// actually raises — which round burned the budget, and whether the rubric gate was
// worth its extra call. Token and call counts are DELTAS over the unit, read off the
// run's token sink, so a caller that installed no sink simply sees zeros.
type IterativeRoundStat struct {
	// Kind is IterativeStatKindRound or IterativeStatKindCommit.
	Kind string `json:"kind"`
	// Round is the 1-based research round, 0 for the commit pass.
	Round int `json:"round,omitempty"`
	// ElapsedSeconds is the unit's wall time, model and tool calls included.
	ElapsedSeconds float64 `json:"elapsed_seconds"`
	// PromptTokens / CompletionTokens / TotalTokens / LLMCalls are the unit's share
	// of every LLM call it made — the Planner's turn, the Synthesizer's rewrite, the
	// rubric judge, a rubric rewrite.
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	LLMCalls         int `json:"llm_calls"`
	// ToolCalls is how many retrieval calls the round made, split by tool name. A
	// round that made none is the round that stopped the loop.
	ToolCalls       int            `json:"tool_calls,omitempty"`
	ToolCallsByName map[string]int `json:"tool_calls_by_name,omitempty"`
	// Docs is how many DISTINCT documents the round's tool output carried — the
	// context the Synthesizer saw, not a token count.
	Docs int `json:"docs,omitempty"`
	// SummaryBytes is the standing summary's size after the unit, which is the
	// context every later round pays for.
	SummaryBytes int `json:"summary_bytes,omitempty"`
	// RubricScore is the judge's weighted average, present only when the gate ran
	// (RubricScored). 0 is a legitimate score — the worst one — which is why the
	// two fields travel together.
	RubricScore  float64 `json:"rubric_score,omitempty"`
	RubricScored bool    `json:"rubric_scored,omitempty"`
	// Rewritten records that the round spent a rewrite on a below-threshold score.
	Rewritten bool `json:"rewritten,omitempty"`
	// Stopped records that the Planner declared the research complete, ending the
	// loop after this round.
	Stopped bool `json:"stopped,omitempty"`
	// Error is set when the unit could not finish (a model outage); the run then
	// degrades rather than failing outright.
	Error string `json:"error,omitempty"`
}

// roundUsageMark is the run's accounting as it stood when a unit of work STARTED:
// the token sink's cumulative counters plus a copy of the per-tool call counts. The
// unit's own cost is the difference between its end and this mark.
type roundUsageMark struct {
	prompt, completion, total, calls int
	tools                            map[string]int
}

// markRunUsage snapshots the accounting for one unit of work. A nil sink (no caller
// installed one) yields a zero mark, so the deltas come out as zeros rather than
// panicking.
func markRunUsage(ctx context.Context, toolCounts map[string]int) roundUsageMark {
	m := roundUsageMark{tools: make(map[string]int, len(toolCounts))}
	for name, count := range toolCounts {
		m.tools[name] = count
	}
	if sink := tokenizer.GetRunUsage(ctx); sink != nil {
		m.prompt, m.completion, m.total, m.calls = sink.Snapshot()
	}
	return m
}

// delta fills in the unit's token and tool-call cost against its starting mark.
func (m roundUsageMark) delta(ctx context.Context, toolCounts map[string]int, st IterativeRoundStat) IterativeRoundStat {
	if sink := tokenizer.GetRunUsage(ctx); sink != nil {
		prompt, completion, total, calls := sink.Snapshot()
		st.PromptTokens = prompt - m.prompt
		st.CompletionTokens = completion - m.completion
		st.TotalTokens = total - m.total
		st.LLMCalls = calls - m.calls
	}
	byName := make(map[string]int)
	totalTools := 0
	for name, count := range toolCounts {
		if n := count - m.tools[name]; n > 0 {
			byName[name] = n
			totalTools += n
		}
	}
	st.ToolCalls = totalTools
	if totalTools > 0 {
		st.ToolCallsByName = byName
	}
	return st
}

// IterativeInput carries everything RunIterative needs to spin up one mode-6 turn.
// Ledger fields mirror Input's, and are the same types: the caller (the service
// layer) builds them once and the accounting a benchmark archives is unchanged
// between mode 5 and mode 6.
type IterativeInput struct {
	// Model backs the PLANNER: it is bound to the retrieval tools and is the only
	// instance that ever emits a tool call.
	//
	// Role separation here is prompt separation, not weight separation
	// (inspiration 5) — IterSynth's central claim is that one policy specialized by
	// prompt and action space is enough, and it is measurably cheaper than a second
	// model. The three instances below are therefore the SAME underlying weights on
	// the same failover chain, differing only in sampling and in failover state.
	Model iterativePlanner
	// SynthModel, when non-nil, is the instance the Synthesizer — and the commit
	// and rewrite passes, which are synthesis-shaped — run on. Separate from Model
	// for the failover reason: an instance caches the error of its last full-chain
	// failure for 30s, so sharing hands the rewrite a stale error from whatever
	// call killed the planner. Nil falls back to Model.
	SynthModel iterativeModel
	// JudgeModel, when non-nil, is the model the rubric judge runs on. Same reason
	// as SynthModel, plus sampling: the judge's verdict is machine-parsed and
	// decides whether an extra rewrite is spent. Nil falls back to Model.
	JudgeModel iterativeModel
	// Messages are the conversation history plus the current user message. Only
	// the LAST user message reaches either role: mode 6's context is rebuilt from
	// the question and the summary, so earlier turns are not replayed into the
	// loop (that is mode 5's contract, not this one's).
	Messages []*schema.Message
	// MaxRounds caps the Planner/Synthesizer loop. Zero falls back to the config's
	// max_search_rounds.
	MaxRounds int
	// Stream selects whether the PLANNER streams. True lets its `<think>` reach the
	// user as it is produced — the only window into why the run searched what it
	// searched, since the next round never sees it. False issues one blocking call
	// per round instead. The Synthesizer never streams either way: its output is
	// machine-parsed state, not prose for a reader.
	Stream bool
	// TenantID and DatasetIDs scope the retrieval tools.
	TenantID   string
	DatasetIDs []string
	// Tools are the eino tools the Planner may call. When empty, the tool set is
	// built from the config's `tools` list. Web search is injected from the run
	// context (WithWebSearch), exactly as in Run.
	Tools []tool.BaseTool
	// OnDelta receives the run's streaming text: the Planner's reasoning live, then
	// compact per-round progress lines, then the final deliverable once.
	OnDelta func(contentDelta, thinkingDelta string)
	// OnRound, when non-nil, is called once per completed research round and once
	// for the closing commit pass, in order, from the run's own goroutine. It is how
	// a caller collects IterativeRoundStat without the engine holding a result the
	// caller may not want.
	OnRound func(IterativeRoundStat)
	// ToolCallCounts, ToolCallErrors, ToolErrorSamples, ToolCallDurations,
	// RetrievedDocIDs, ChunkReads, Searches and Serves are the same per-turn
	// accounting Input carries; see Input's field comments for their contracts.
	ToolCallCounts   map[string]int
	ToolCallErrors   map[string]int
	ToolErrorSamples map[string]string

	ToolCallDurations *durationAccumulator
	RetrievedDocIDs   *docIDLedger
	ChunkReads        *chunkReadLedger
	Searches          *searchLedger
	Serves            *servedLedger
}

// summaryTagRe extracts the Synthesizer's state block.
var summaryTagRe = regexp.MustCompile(`(?s)<summary>(.*?)</summary>`)

// thinkTagRe extracts the Planner's reasoning block.
var thinkTagRe = regexp.MustCompile(`(?s)<think>(.*?)</think>`)

// rubricWeights are the synthesizer rubric's dimension weights
// (rubrics/synthesizer_rubric.json). A dimension the judge omits is dropped from
// the average rather than scored 0: a malformed one-entry reply must not read as a
// failed summary and burn a rewrite.
var rubricWeights = map[string]float64{
	"entity_anchor_integrity":                   5,
	"conflict_flagging_required":                3,
	"gold_evidence_salience":                    4,
	"claim_citation_grounding":                  4,
	"subconstraint_coverage_and_disambiguation": 4,
}

// RunIterative executes one mode-6 turn and returns the deliverable it ships,
// streaming the Planner's reasoning and the round progress through in.OnDelta.
//
// The loop is: rebuild the Planner's context from (question, summary, ledger delta)
// → the Planner either calls tools or stops → run the tools → the Synthesizer
// rewrites the summary → the rubric scores it and buys a rewrite when it is weak.
// Nothing else crosses a round boundary.
func RunIterative(ctx context.Context, in IterativeInput) (string, error) {
	if in.Model == nil {
		return "", errNilModel
	}
	cfg, cfgErr := loadIterativeConfig()
	if cfgErr != nil {
		return "", fmt.Errorf("iterative_rag: %w", cfgErr)
	}

	question := strings.TrimSpace(lastUserQuestion(in.Messages))
	if question == "" {
		return "", errors.New("iterative_rag: no user question in the conversation")
	}

	// Shared per-run ledgers: created here so a caller that passes none still gets
	// the accounting, exactly as Run does.
	if in.ToolCallDurations == nil {
		in.ToolCallDurations = NewDurationAccumulator()
	}
	if in.RetrievedDocIDs == nil {
		in.RetrievedDocIDs = NewDocIDLedger()
	}
	if in.ChunkReads == nil {
		in.ChunkReads = NewChunkReadLedger()
	}
	if in.Searches == nil {
		in.Searches = NewSearchLedger()
	}
	if in.Serves == nil {
		in.Serves = NewServedLedger()
	}
	// The serve ledger rides the context: every retrieval tool's serve point records
	// what it put in front of the model, which is what the served-but-unread leads
	// are computed from.
	ctx = withServedLedger(ctx, in.Serves)

	// toolsFor handles both cases: an explicit tool set wins, otherwise the
	// template's list is used; either way the conversation's web search tool is
	// appended when it has a provider.
	tools := toolsFor(ctx, Template{Tools: cfg.Tools}, in.TenantID, in.DatasetIDs, in.Tools)

	// Wrap every invokable tool so a call is timed, logged, counted and recorded in
	// the run's ledgers — the same wrapper mode 5 uses, deliberately shared so the
	// two engines' per-turn accounting is directly comparable.
	watch := newLocateWatch()
	unreadWatch := newServedUnreadWatch(in.Serves, in.RetrievedDocIDs)
	invokables := make(map[string]*instrumentedTool, len(tools))
	infos := make([]*schema.ToolInfo, 0, len(tools))
	for _, t := range tools {
		it, ok := t.(tool.InvokableTool)
		if !ok {
			continue
		}
		info, ierr := it.Info(ctx)
		if ierr != nil || info == nil || info.Name == "" {
			common.WarnCtx(ctx, "iterative_rag: skipping tool without info", zap.Error(ierr))
			continue
		}
		if info.Name == searchSemanticChunksToolName {
			watch.markSemanticAvailable()
		}
		invokables[info.Name] = &instrumentedTool{
			InvokableTool: it,
			acc:           in.ToolCallDurations,
			docs:          in.RetrievedDocIDs,
			chunks:        in.ChunkReads,
			searches:      in.Searches,
			watch:         watch,
			unread:        unreadWatch,
		}
		infos = append(infos, info)
	}
	if len(infos) == 0 {
		common.WarnCtx(ctx, "iterative_rag: no retrieval tool resolved; the Planner cannot search")
	}

	bound, err := in.Model.WithTools(infos)
	if err != nil {
		return "", fmt.Errorf("iterative_rag: bind tools: %w", err)
	}

	rounds := cfg.MaxSearchRounds
	if in.MaxRounds > 0 {
		rounds = in.MaxRounds
	}
	if rounds <= 0 {
		rounds = defaultIterativeRounds
	}

	// The two non-planner roles fall back to the planner's instance rather than
	// failing: a caller that built one model still gets a working run, it just
	// shares failover state across roles.
	synth := in.SynthModel
	if synth == nil {
		synth = in.Model
	}
	judge := in.JudgeModel
	if judge == nil {
		judge = in.Model
	}

	common.InfoCtx(ctx, "iterative_rag: run start",
		zap.Int("max_rounds", rounds),
		zap.Int("tools", len(infos)),
		zap.Bool("rubric", cfg.rubricEnabled()),
		zap.String("question", truncateForLog(question, 200)))

	summary := ""
	// afterSeq is the ledger cursor: the next Planner sees only the actions that ran
	// since its own last turn (inspiration 2).
	afterSeq := 0
	rewrites := 0
	roundsRun := 0
	var runErr error
	stopped := false

	// finishRound closes out one unit of work: it diffs the run's accounting against
	// the mark taken when the unit started, reports the stat through in.OnRound, and
	// logs it. Every exit path of a round goes through here, including the failures —
	// a round that died on a model outage still cost the tokens it burned, and hiding
	// that is exactly what makes a run's total unexplainable.
	finishRound := func(st IterativeRoundStat, mark roundUsageMark, think string) {
		st = mark.delta(ctx, in.ToolCallCounts, st)
		if in.OnRound != nil {
			in.OnRound(st)
		}
		fields := []zap.Field{
			zap.String("kind", st.Kind),
			zap.Int("round", st.Round),
			zap.Float64("elapsed_seconds", st.ElapsedSeconds),
			zap.Int("prompt_tokens", st.PromptTokens),
			zap.Int("completion_tokens", st.CompletionTokens),
			zap.Int("total_tokens", st.TotalTokens),
			zap.Int("llm_calls", st.LLMCalls),
			zap.Int("tool_calls", st.ToolCalls),
			zap.Int("docs", st.Docs),
			zap.Int("summary_bytes", st.SummaryBytes),
			zap.Bool("rubric_scored", st.RubricScored),
			zap.Float64("rubric_score", st.RubricScore),
			zap.Bool("rewritten", st.Rewritten),
			zap.Bool("stopped", st.Stopped),
			zap.String("think", think),
		}
		if st.Error != "" {
			fields = append(fields, zap.String("error", st.Error))
		}
		if st.ToolCallsByName != nil {
			fields = append(fields, zap.Any("tool_calls_by_name", st.ToolCallsByName))
		}
		if st.Kind == IterativeStatKindCommit {
			common.DebugCtx(ctx, "iterative_rag: commit pass complete", fields...)
			return
		}
		common.DebugCtx(ctx, "iterative_rag: round complete", fields...)
	}

	for round := 1; round <= rounds; round++ {
		if err := ctx.Err(); err != nil {
			runErr = err
			break
		}
		roundStart := time.Now()
		mark := markRunUsage(ctx, in.ToolCallCounts)
		roundsRun = round

		prompt := fillTemplate(cfg.Planner.Content, map[string]string{
			"QUESTION":      question,
			"SUMMARY":       displayState(summary),
			"LEDGER_DELTA":  renderLedgerDelta(in.Searches, afterSeq, iterativeLedgerDeltaMaxEntries),
			"SERVED_UNREAD": renderServedUnread(in.Serves, in.RetrievedDocIDs, iterativeServedUnreadLimit),
			"ROUND":         fmt.Sprintf("%d", round),
			"MAX_ROUNDS":    fmt.Sprintf("%d", rounds),
		})

		planMsg, perr := streamPlanner(ctx, bound, prompt, in.Stream, in.OnDelta)
		if perr != nil {
			// The Planner could not run: stop the loop and let the commit pass ship
			// the standing summary. Losing the round is not losing the run — the
			// state carries everything gathered so far.
			common.WarnCtx(ctx, "iterative_rag: planner failed", zap.Int("round", round), zap.Error(perr))
			finishRound(IterativeRoundStat{
				Kind:           IterativeStatKindRound,
				Round:          round,
				ElapsedSeconds: time.Since(roundStart).Seconds(),
				SummaryBytes:   len(summary),
				Error:          perr.Error(),
			}, mark, "")
			runErr = perr
			break
		}

		think := plannerThink(planMsg)
		calls := planMsg.ToolCalls
		if len(calls) == 0 {
			// The Planner declared the research complete. Its own text is not the
			// deliverable — the summary is — but a stop on an EMPTY summary (a
			// conversational turn, or a Planner that answered from nothing) is
			// worth keeping as the commit pass's seed instead of discarding it.
			if strings.TrimSpace(summary) == "" {
				if text := strings.TrimSpace(planMsg.Content); text != "" {
					summary = text
				}
			}
			common.InfoCtx(ctx, "iterative_rag: planner stopped",
				zap.Int("round", round), zap.Int("summary_bytes", len(summary)))
			finishRound(IterativeRoundStat{
				Kind:           IterativeStatKindRound,
				Round:          round,
				ElapsedSeconds: time.Since(roundStart).Seconds(),
				SummaryBytes:   len(summary),
				Stopped:        true,
			}, mark, think)
			stopped = true
			break
		}

		// The cursor is advanced to the count as it stood BEFORE this round's tools
		// ran, so the NEXT Planner's delta is exactly this round's actions. Moving it
		// to the post-tool count instead would skip them: the delta would start one
		// round too late and the Planner would re-run the search it just performed.
		ledgerBefore := in.Searches.Count()
		results, docCount := runIterativeTools(ctx, invokables, calls, in)
		queries := renderToolCalls(calls)
		afterSeq = ledgerBefore

		synthPrompt := fillTemplate(cfg.Synthesizer.Content, map[string]string{
			"QUESTION":      question,
			"SUMMARY":       displayState(summary),
			"THINK":         displayThink(think),
			"QUERIES":       displayQueries(queries),
			"DOC_COUNT":     fmt.Sprintf("%d", docCount),
			"TOOL_RESPONSE": truncateRunes(results, iterativeToolResponseMaxRunes),
		})
		raw, gerr := generateOnce(ctx, synth, synthPrompt)
		if gerr != nil {
			// The retrieval landed in the ledger and the read ledgers; only the
			// rewrite is lost. Keeping the previous summary is lossy but honest, and
			// the next round can re-read what this one surfaced.
			common.WarnCtx(ctx, "iterative_rag: synthesizer failed", zap.Int("round", round), zap.Error(gerr))
			finishRound(IterativeRoundStat{
				Kind:           IterativeStatKindRound,
				Round:          round,
				ElapsedSeconds: time.Since(roundStart).Seconds(),
				Docs:           docCount,
				SummaryBytes:   len(summary),
				Error:          gerr.Error(),
			}, mark, think)
			runErr = gerr
			continue
		}
		next := parseSummaryOutput(ctx, raw, round)
		if next == "" {
			common.WarnCtx(ctx, "iterative_rag: synthesizer returned an empty summary; keeping the standing one",
				zap.Int("round", round))
			finishRound(IterativeRoundStat{
				Kind:           IterativeStatKindRound,
				Round:          round,
				ElapsedSeconds: time.Since(roundStart).Seconds(),
				Docs:           docCount,
				SummaryBytes:   len(summary),
			}, mark, think)
			continue
		}
		summary = next

		outcome := in.maybeRewrite(ctx, cfg, synth, judge, question, &summary, round, rewrites)
		if outcome.rewritten {
			rewrites++
		}
		emit(ctx, in.OnDelta, "", fmt.Sprintf("\n\n[round %d/%d] %s → %s\n\n",
			round, rounds, describeQueries(queries), describeSummary(summary, outcome.findings)))

		finishRound(IterativeRoundStat{
			Kind:           IterativeStatKindRound,
			Round:          round,
			ElapsedSeconds: time.Since(roundStart).Seconds(),
			Docs:           docCount,
			SummaryBytes:   len(summary),
			RubricScore:    outcome.score,
			RubricScored:   outcome.scored,
			Rewritten:      outcome.rewritten,
		}, mark, think)
	}

	final := strings.TrimSpace(summary)
	if final == "" {
		final = iterativeStateEmpty
	}
	// The loop's product is a summary, not necessarily a shippable deliverable: a run
	// that spent its whole budget mid-investigation still has to answer. One commit
	// pass re-renders the summary with a settled answer line, the same job mode 5's
	// finalizeAnswer does — with the summary as the input instead of a truncated
	// evidence digest, which is the whole reason the state was worth maintaining.
	if !hasAnswerLine(final) || !hasFOSStructure(final) {
		commitStart := time.Now()
		mark := markRunUsage(ctx, in.ToolCallCounts)
		committed, cerr := iterativeCommit(ctx, synth, cfg, question, final)
		if cerr != nil {
			common.WarnCtx(ctx, "iterative_rag: commit pass failed", zap.Error(cerr))
		} else if strings.TrimSpace(committed) != "" {
			final = strings.TrimSpace(committed)
		}
		// The commit pass is reported like a round so the run's parts add up; it is
		// not one, hence Round 0.
		stat := IterativeRoundStat{
			Kind:           IterativeStatKindCommit,
			ElapsedSeconds: time.Since(commitStart).Seconds(),
			SummaryBytes:   len(final),
		}
		if cerr != nil {
			stat.Error = cerr.Error()
		}
		finishRound(stat, mark, "")
	}
	// Never ship a turn without an answer line: the citation payload, the label
	// governance and the UI all read the label, and a missing one turns a completed
	// investigation into an unanswerable row.
	if !hasAnswerLine(final) {
		final = strings.TrimSpace(final) +
			"\n\nGuessed Answer: **insufficient evidence** (assumption: the corpus did not establish a value for this question)\n"
	}

	common.InfoCtx(ctx, "iterative_rag: shipping final result",
		zap.Int("final_bytes", len(final)),
		zap.Int("rounds_run", roundsRun),
		zap.Int("searches", in.Searches.Count()),
		zap.Int("rewrites", rewrites),
		zap.Bool("planner_stopped", stopped),
		zap.Bool("run_err", runErr != nil))
	emit(ctx, in.OnDelta, final, "")
	return final, runErr
}

// rubricOutcome is what one round's quality gate concluded: the score it measured,
// whether it measured at all, the judge's findings, and whether a rewrite was spent
// on them. A struct rather than a (score, rewriteCount, findings) tuple because the
// per-round stat reports all four and a tuple's third element would have to be
// re-derived from the second.
type rubricOutcome struct {
	scored    bool
	score     float64
	findings  []string
	rewritten bool
}

// maybeRewrite runs the rubric judge and, when the summary scores below the
// threshold, buys ONE rewrite pass (inspirations 4 and 6).
//
// A judge failure is never fatal: the summary stands unaudited and the run
// continues. A rewrite failure is equally non-fatal — the rejected summary is the
// one that was already standing.
func (in IterativeInput) maybeRewrite(
	ctx context.Context,
	cfg *iterativeConfig,
	synth iterativeModel,
	judge iterativeModel,
	question string,
	summary *string,
	round, rewrites int,
) rubricOutcome {
	if !cfg.rubricEnabled() || summary == nil || strings.TrimSpace(*summary) == "" {
		return rubricOutcome{}
	}
	if cfg.Rubric.EveryRounds > 1 && round%cfg.Rubric.EveryRounds != 0 {
		return rubricOutcome{}
	}
	score, findings, err := judgeSummary(ctx, judge, cfg, question, *summary)
	if err != nil {
		common.WarnCtx(ctx, "iterative_rag: rubric judge unavailable", zap.Int("round", round), zap.Error(err))
		return rubricOutcome{}
	}
	out := rubricOutcome{scored: true, score: score, findings: findings}
	common.InfoCtx(ctx, "iterative_rag: rubric score",
		zap.Int("round", round),
		zap.Float64("score", score),
		zap.Float64("threshold", cfg.Rubric.Threshold),
		zap.Int("findings", len(findings)))
	if score >= cfg.Rubric.Threshold {
		return out
	}
	if rewrites >= cfg.Rubric.MaxRewrites {
		common.WarnCtx(ctx, "iterative_rag: rewrite budget spent; shipping the scored summary",
			zap.Int("max_rewrites", cfg.Rubric.MaxRewrites))
		return out
	}

	prompt := fillTemplate(cfg.Rewrite.Content, map[string]string{
		"QUESTION": question,
		"SUMMARY":  *summary,
		"FINDINGS": displayFindings(findings),
	})
	raw, rerr := generateOnce(ctx, synth, prompt)
	if rerr != nil {
		common.WarnCtx(ctx, "iterative_rag: rubric rewrite failed", zap.Int("round", round), zap.Error(rerr))
		return out
	}
	rewritten := parseSummaryOutput(ctx, raw, round)
	if rewritten == "" {
		return out
	}
	*summary = rewritten
	out.rewritten = true
	common.InfoCtx(ctx, "iterative_rag: summary rewritten after rubric",
		zap.Int("round", round), zap.Int("summary_bytes", len(rewritten)))
	return out
}

// streamPlanner runs one Planner completion, emitting its reasoning live on the
// thinking channel, and returns the merged assistant message (content plus the tool
// calls it decided on).
//
// The Planner streams because its `<think>` IS the only window into why the run
// searched what it searched — the next round never sees it, and neither would the
// user if it were not streamed as it is produced.
func streamPlanner(
	ctx context.Context,
	m model.ToolCallingChatModel,
	prompt string,
	stream bool,
	onDelta func(contentDelta, thinkingDelta string),
) (*schema.Message, error) {
	msgs := []*schema.Message{schema.UserMessage(prompt)}
	if !stream {
		return generatePlanner(ctx, m, msgs, onDelta)
	}
	sr, err := m.Stream(ctx, msgs)
	if err != nil {
		// A provider that cannot stream this request still has to answer it: fall
		// back to a blocking call rather than losing the round.
		common.WarnCtx(ctx, "iterative_rag: planner stream unavailable, falling back to Generate", zap.Error(err))
		return generatePlanner(ctx, m, msgs, onDelta)
	}
	defer sr.Close()

	var chunks []*schema.Message
	for {
		chunk, recvErr := sr.Recv()
		if recvErr != nil {
			if errors.Is(recvErr, io.EOF) {
				break
			}
			return nil, recvErr
		}
		if chunk == nil {
			continue
		}
		emit(ctx, onDelta, "", chunk.ReasoningContent+chunk.Content)
		chunks = append(chunks, chunk)
	}
	merged := mergeStreamedAssistant(chunks)
	if merged == nil {
		return nil, errors.New("iterative_rag: planner produced no output")
	}
	return merged, nil
}

// generatePlanner is the Planner's blocking path: used when the caller asked for no
// streaming, and as the fallback when a provider cannot stream the request.
func generatePlanner(
	ctx context.Context,
	m model.ToolCallingChatModel,
	msgs []*schema.Message,
	onDelta func(contentDelta, thinkingDelta string),
) (*schema.Message, error) {
	resp, err := m.Generate(ctx, msgs)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("iterative_rag: planner returned no message")
	}
	emit(ctx, onDelta, "", resp.ReasoningContent+resp.Content)
	return resp, nil
}

// generateOnce issues one blocking completion and returns its trimmed content. It is
// the Synthesizer/commit/rewrite call: none of those stream, because their output is
// state to be parsed rather than prose for a reader.
func generateOnce(ctx context.Context, m iterativeModel, prompt string) (string, error) {
	resp, err := m.Generate(ctx, []*schema.Message{schema.UserMessage(prompt)})
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", errors.New("iterative_rag: empty model response")
	}
	return strings.TrimSpace(resp.Content), nil
}

// runIterativeTools executes one round's tool calls in order, returning the rendered
// tool-response block and how many DISTINCT documents the round put in front of the
// Synthesizer.
//
// IterSynth's own `doc_count` is permanently 0 — it reads `total_results` from a
// metrics map the search tool never populates (report §3.3) — so the count here is
// derived from what the tools actually rendered instead of from a key nobody sets.
func runIterativeTools(
	ctx context.Context,
	tools map[string]*instrumentedTool,
	calls []schema.ToolCall,
	in IterativeInput,
) (string, int) {
	var b strings.Builder
	docs := make(map[string]struct{})
	for _, tc := range calls {
		name := tc.Function.Name
		if in.ToolCallCounts != nil && name != "" {
			in.ToolCallCounts[name]++
		}
		it, ok := tools[name]
		if !ok {
			fmt.Fprintf(&b, "<tool_error tool=%q severity=\"error\">unknown tool</tool_error>\n", name)
			continue
		}
		out, err := it.InvokableRun(ctx, tc.Function.Arguments)
		if err != nil {
			if in.ToolCallErrors != nil && name != "" {
				in.ToolCallErrors[name]++
			}
			if in.ToolErrorSamples != nil && name != "" {
				if _, seen := in.ToolErrorSamples[name]; !seen {
					in.ToolErrorSamples[name] = err.Error()
				}
			}
			fmt.Fprintf(&b, "<tool_error tool=%q severity=\"error\">%s</tool_error>\n", name, err.Error())
			continue
		}
		if strings.Contains(out, toolErrorMarker) && strings.Contains(out, `severity="error"`) {
			if in.ToolCallErrors != nil && name != "" {
				in.ToolCallErrors[name]++
			}
			if in.ToolErrorSamples != nil && name != "" {
				if _, seen := in.ToolErrorSamples[name]; !seen {
					in.ToolErrorSamples[name] = out
				}
			}
		}
		for _, d := range retrievalaccount.RecordedDocIDs(out) {
			docs[d] = struct{}{}
		}
		fmt.Fprintf(&b, "<tool_result tool=%q>\n%s\n</tool_result>\n", name, out)
	}
	return b.String(), len(docs)
}

// plannerThink returns the Planner's own reasoning for the Synthesizer's
// `Latest Reasoning` block: the native reasoning channel when the provider filled it,
// else the `<think>` block of the content, else the content itself (a Planner that
// wrote prose instead of tags still told the Synthesizer something).
func plannerThink(m *schema.Message) string {
	if m == nil {
		return ""
	}
	if t := strings.TrimSpace(m.ReasoningContent); t != "" {
		return t
	}
	if match := thinkTagRe.FindStringSubmatch(m.Content); match != nil {
		return strings.TrimSpace(match[1])
	}
	return strings.TrimSpace(m.Content)
}

// parseSummaryOutput extracts the Synthesizer's state block. A reply without the
// tag degrades to the whole output — IterSynth's own fallback (report §2.4) — because
// throwing the round's work away is worse than keeping a noisy summary. Unlike
// IterSynth, the degradation is logged: the report flags it as the one place
// contamination can enter every later round, and a warning is what makes it
// attributable after the fact.
func parseSummaryOutput(ctx context.Context, raw string, round int) string {
	if match := summaryTagRe.FindStringSubmatch(raw); match != nil {
		return strings.TrimSpace(match[1])
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	common.WarnCtx(ctx, "iterative_rag: synthesizer output carried no <summary> tag; using it verbatim",
		zap.Int("round", round), zap.Int("bytes", len(trimmed)))
	return trimmed
}

// iterativeCommit re-renders the standing summary with a settled answer line, so a
// run that stopped mid-investigation still ships its evidence instead of a blank.
func iterativeCommit(
	ctx context.Context,
	m iterativeModel,
	cfg *iterativeConfig,
	question, summary string,
) (string, error) {
	prompt := fillTemplate(cfg.Commit.Content, map[string]string{
		"QUESTION": question,
		"SUMMARY":  displayState(summary),
	})
	out, err := generateOnce(ctx, m, prompt)
	if err != nil {
		return "", err
	}
	return parseSummaryOutput(ctx, out, 0), nil
}

// rubricVerdict is the judge's machine-parsed reply.
type rubricVerdict struct {
	Scores   map[string]float64 `json:"scores"`
	Findings []string           `json:"findings"`
}

// judgeSummary scores a standing summary on the synthesizer rubric and returns the
// weighted average plus the judge's findings.
func judgeSummary(
	ctx context.Context,
	judge iterativeModel,
	cfg *iterativeConfig,
	question, summary string,
) (float64, []string, error) {
	prompt := fillTemplate(cfg.Rubric.Content, map[string]string{
		"QUESTION": question,
		"SUMMARY":  summary,
	})
	raw, err := generateOnce(ctx, judge, prompt)
	if err != nil {
		return 0, nil, err
	}
	verdict, err := parseRubricVerdict(raw)
	if err != nil {
		return 0, nil, err
	}
	score, ok := weightedRubricScore(verdict.Scores)
	if !ok {
		return 0, nil, fmt.Errorf("iterative_rag: rubric judge scored no known dimension (%d keys)", len(verdict.Scores))
	}
	return score, verdict.Findings, nil
}

// weightedRubricScore reduces the judge's per-dimension scores to the one number the
// gate compares against its threshold. A dimension the judge omitted is dropped from
// BOTH sides of the ratio rather than counted as 0: a one-entry reply must score on
// what it did judge, or a formatting slip would read as a failed summary and burn a
// rewrite. ok is false when no dimension the rubric knows was scored at all.
func weightedRubricScore(scores map[string]float64) (float64, bool) {
	var weighted, weight float64
	for dim, w := range rubricWeights {
		v, ok := scores[dim]
		if !ok {
			continue
		}
		weighted += v * w
		weight += w
	}
	if weight == 0 {
		return 0, false
	}
	return weighted / weight, true
}

// parseRubricVerdict reads the judge's JSON object out of a reply that may be
// wrapped in prose or a code fence. The first `{` through the last `}` is the
// object: a judge that narrates around its answer is still usable, and the
// alternative — a stricter parse — turns a formatting slip into a lost audit.
func parseRubricVerdict(raw string) (*rubricVerdict, error) {
	start := strings.IndexByte(raw, '{')
	end := strings.LastIndexByte(raw, '}')
	if start < 0 || end <= start {
		return nil, fmt.Errorf("iterative_rag: rubric judge returned no JSON object")
	}
	var v rubricVerdict
	if err := json.Unmarshal([]byte(raw[start:end+1]), &v); err != nil {
		return nil, fmt.Errorf("iterative_rag: parse rubric verdict: %w", err)
	}
	if len(v.Scores) == 0 {
		return nil, errors.New("iterative_rag: rubric verdict carried no scores")
	}
	return &v, nil
}

// renderLedgerDelta renders the search ledger's tail as the Planner's own record of
// what already ran (inspiration 2). The cursor keeps the block small on a long run,
// and an overflow is announced rather than silently dropped — a missing entry would
// read as "this was never searched", which is exactly the misreading the delta
// exists to prevent.
func renderLedgerDelta(ledger *searchLedger, after, limit int) string {
	total := ledger.Count()
	entries := ledger.Snapshot(after, limit)
	if len(entries) == 0 {
		return iterativeLedgerEmpty
	}
	var b strings.Builder
	if omitted := total - after - len(entries); omitted > 0 {
		fmt.Fprintf(&b, "- (%d earlier action(s) omitted; re-run any of them if a result is needed)\n", omitted)
	}
	for _, e := range entries {
		fmt.Fprintf(&b, "- #%d %s %s\n", e.seq, e.tool, e.args)
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderServedUnread names the documents that were served and never deep-read —
// mode 5's servedUnreadDocs, placed where the decision is made rather than in a
// watchdog that appends to a tool result after the fact (inspiration 3).
//
// The count is part of each lead's label already, and it is the DISTINCT-QUERY serve
// count: the same weight mode 5's census uses, and the one that keeps a run's own
// latched document from crowding out the leads it has never opened.
func renderServedUnread(served *servedLedger, deep *docIDLedger, limit int) string {
	docs := servedUnreadDocs(served, deep, limit)
	if len(docs) == 0 {
		return iterativeUnreadEmpty
	}
	lines := make([]string, 0, len(docs))
	for _, lead := range docs {
		lines = append(lines, "- "+lead)
	}
	return strings.Join(lines, "\n")
}

// renderToolCalls renders the calls a Planner round issued, for the Synthesizer's
// `Latest Search Queries` block.
func renderToolCalls(calls []schema.ToolCall) []string {
	out := make([]string, 0, len(calls))
	for _, tc := range calls {
		args := strings.TrimSpace(tc.Function.Arguments)
		if args == "" {
			args = "{}"
		}
		out = append(out, fmt.Sprintf("%s %s", tc.Function.Name, args))
	}
	return out
}

// displayState renders a role's view of the persistent state, substituting the
// first-round placeholder for an empty one.
func displayState(summary string) string {
	if strings.TrimSpace(summary) == "" {
		return iterativeStateEmpty
	}
	return summary
}

// displayThink renders the Synthesizer's view of the Planner's reasoning.
func displayThink(think string) string {
	if strings.TrimSpace(think) == "" {
		return "(the planner recorded no reasoning this round)"
	}
	return think
}

// displayQueries renders the Synthesizer's view of the round's queries.
func displayQueries(queries []string) string {
	if len(queries) == 0 {
		return "(none)"
	}
	return strings.Join(queries, "\n")
}

// displayFindings renders the judge's findings into the rewrite prompt.
func displayFindings(findings []string) string {
	if len(findings) == 0 {
		return "(the judge named no specific defect; tighten the record's grounding, salience and constraint coverage)"
	}
	var b strings.Builder
	for _, f := range findings {
		fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(f))
	}
	return strings.TrimRight(b.String(), "\n")
}

// describeQueries is the compact form of a round's calls for the progress line.
func describeQueries(queries []string) string {
	if len(queries) == 0 {
		return "no query"
	}
	if len(queries) == 1 {
		return truncateForLog(queries[0], 120)
	}
	return fmt.Sprintf("%s (+%d more)", truncateForLog(queries[0], 100), len(queries)-1)
}

// describeSummary is the compact form of a round's outcome for the progress line.
func describeSummary(summary string, findings []string) string {
	if len(findings) > 0 {
		return fmt.Sprintf("summary %d bytes, %d rubric finding(s)", len(summary), len(findings))
	}
	return fmt.Sprintf("summary %d bytes", len(summary))
}

// fillTemplate substitutes `{{NAME}}` placeholders. The templates live in
// conf/iterative_synthesis.yaml and use a deliberately dumb syntax: a research
// prompt is full of braces from its own JSON examples, so anything resembling a
// template language would collide with the content.
func fillTemplate(tmpl string, vars map[string]string) string {
	out := tmpl
	for k, v := range vars {
		out = strings.ReplaceAll(out, "{{"+k+"}}", v)
	}
	return out
}
