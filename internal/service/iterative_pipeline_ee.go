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
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"ragflow/internal/agentic_rag"
	"ragflow/internal/common"
	"ragflow/internal/entity"
	modelModule "ragflow/internal/entity/models"
	"ragflow/internal/tokenizer"
)

// iterativeSynthesisTimeout is the total wall-clock budget for one mode-6 run,
// shared by every model and tool it calls — tools carry no per-call limits of their
// own.
//
// The budget mirrors agenticRag's smartReasoningTimeout, and for the same reason:
// the two HTTP entrypoints pass Request.Context(), which carries no deadline, so
// without an explicit budget here a client that keeps its connection open could let
// the loop burn CPU indefinitely. Mode 6's per-round cost is lower than mode 5's —
// a Planner turn plus a Synthesizer turn plus an optional rubric pass, against a
// ReAct turn plus audit passes — but its round count is comparable, so the same
// 30 minutes is the honest starting point rather than a smaller guess that would
// kill long runs mid-investigation.
var iterativeSynthesisTimeout = 30 * time.Minute

// iterativeSynthesis runs one mode-6 (Iterative Synthesis) turn: the role-decoupled,
// summary-based loop in internal/agentic_rag.
//
// It mirrors agenticRag's channel contract: yields AsyncChatResult deltas (reasoning
// while the Planner works, then the finished deliverable) over a buffered channel
// consumed by the same callers (ChatCompletions / OpenAIChatCompletions), and ships
// the identical per-turn accounting on the final result so a benchmark can compare
// mode 5 and mode 6 rows field by field.
func (s *ChatPipelineService) iterativeSynthesis(
	ctx context.Context,
	chat *entity.Chat,
	messages []map[string]interface{},
	stream bool,
	kwargs map[string]interface{},
	useWebSearch bool,
	quote bool,
) (<-chan AsyncChatResult, error) {
	out := make(chan AsyncChatResult, 16)

	go func() {
		defer close(out)

		// The chain is the dialog's own model plus the failover members the dialog
		// itself configures — see agenticModelChain. Mode 6 adds no second model
		// here: the roles below are the SAME chain at different sampling, because
		// role separation is prompt separation (inspiration 5).
		modelChain, err := s.agenticModelChain(ctx, chat)
		if err != nil {
			common.ErrorCtx(ctx, "iterative_synthesis: resolve chat model", err)
			out <- AsyncChatResult{Answer: fmt.Sprintf("**ERROR**: %s", err.Error()), Final: true}
			return
		}

		// The dialog's LLM setting with per-request overrides, exactly like the
		// regular AsyncChat path — otherwise temperature / top_p / max_tokens
		// silently no-op for this engine.
		chatCfg := BuildChatConfig(chat, kwargs)
		temps := agentic_rag.IterativeTemperatures()

		// Three instances over one chain. Each pinned role gets its own because a
		// failover instance caches the error of its last full-chain failure for 30s:
		// sharing hands the rewrite, or the judge, a stale error from whatever call
		// killed the Planner. A failure to build a non-planner instance degrades to
		// the producer's rather than failing the turn — the run is still correct, it
		// just shares failover state.
		plannerModel, pErr := modelModule.NewFailoverEinoChatModel(
			modelChain, chatConfigWithTemperature(chatCfg, temps.Planner))
		if pErr != nil {
			common.ErrorCtx(ctx, "iterative_synthesis: build planner model", pErr)
			out <- AsyncChatResult{Answer: fmt.Sprintf("**ERROR**: %s", pErr.Error()), Final: true}
			return
		}
		synthModel := plannerModel
		if m, sErr := modelModule.NewFailoverEinoChatModel(
			modelChain, chatConfigWithTemperature(chatCfg, temps.Synthesizer)); sErr != nil {
			common.WarnCtx(ctx, "iterative_synthesis: build synthesizer model", zap.Error(sErr))
		} else {
			synthModel = m
		}
		judgeModel := plannerModel
		if m, jErr := modelModule.NewFailoverEinoChatModel(
			modelChain, chatConfigWithTemperature(chatCfg, temps.Rubric)); jErr != nil {
			common.WarnCtx(ctx, "iterative_synthesis: build rubric judge model", zap.Error(jErr))
		} else {
			judgeModel = m
		}

		// Attachments are resolved during conversion; the model type decides
		// whether images are carried (see convertMessagesToEino).
		modelType := "chat"
		if info := modelChain[0].Info(); info != nil && entity.ModelTypeFromStrings(info.ModelTypes).Has(entity.ModelTypeImage2Text) {
			modelType = "image2text"
		}
		msgs := s.convertMessagesToEino(ctx, modelType, messages)

		// The dataset scope is resolved from the chat's KBs, exactly as agenticRag
		// resolves it, so the Planner's corpus tools search the right datasets.
		datasetIDs := make([]string, 0, len(chat.KBIDs))
		for _, raw := range chat.KBIDs {
			if id, ok := raw.(string); ok && id != "" {
				datasetIDs = append(datasetIDs, id)
			}
		}
		// The tenant scope is the chat's OWNING tenant, not the requesting user:
		// index names are built from the tenant id, so passing the member user's id
		// would query the wrong index and return stable empty results.

		// Web search is a capability of the conversation, not of the engine: it is
		// injected through the run context so an absent capability is never
		// advertised to the Planner.
		var webSearch agentic_rag.WebSearchFunc
		if useWebSearch {
			// webSearchFunc resolves the conversation's provider itself and returns
			// nil when the conversation carries none.
			webSearch = s.webSearchFunc(chat.PromptConfig)
		}
		common.InfoCtx(ctx, "iterative_synthesis: web search", zap.Bool("enabled", webSearch != nil))

		// Give the whole run a fixed total budget (see iterativeSynthesisTimeout).
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, iterativeSynthesisTimeout)
		defer cancel()

		maxRounds := 0
		if v, ok := kwargs["max_iterations"]; ok {
			switch n := v.(type) {
			case int:
				maxRounds = n
			case float64:
				maxRounds = int(n)
			}
		}

		// thinking tracks whether the Planner's reasoning stream is open so the
		// StartToThink marker is emitted once and EndToThink fires on the first
		// non-thinking delta after it — the same framing every SSE consumer already
		// understands from the other agentic paths.
		thinking := false
		var final string

		// Per-question token accounting: every LLM call inside the loop accumulates
		// into this sink, so the mode's real cost is reportable next to mode 5's.
		runCtx := tokenizer.WithRunUsage(ctx)
		registry := agentic_rag.NewEvidenceRegistry()
		runCtx = agentic_rag.WithEvidenceRegistry(runCtx, registry)
		runCtx = agentic_rag.WithWebSearch(runCtx, webSearch)

		toolCounts := make(map[string]int)
		toolErrors := make(map[string]int)
		toolErrorSamples := make(map[string]string)
		toolDurations := agentic_rag.NewDurationAccumulator()
		retrievedDocs := agentic_rag.NewDocIDLedger()
		servedDocs := agentic_rag.NewServedLedger()
		chunkReads := agentic_rag.NewChunkReadLedger()
		// The engine reports each unit of work as it finishes; the run's own
		// goroutine is the only writer, so no lock is needed.
		var rounds []agentic_rag.IterativeRoundStat

		runStart := time.Now()
		final, err = agentic_rag.RunIterative(runCtx, agentic_rag.IterativeInput{
			Model:      plannerModel,
			SynthModel: synthModel,
			JudgeModel: judgeModel,
			Messages:   msgs,
			MaxRounds:  maxRounds,
			Stream:     stream,
			TenantID:   chat.TenantID,
			DatasetIDs: datasetIDs,
			OnRound: func(stat agentic_rag.IterativeRoundStat) {
				rounds = append(rounds, stat)
			},

			ToolCallCounts:    toolCounts,
			ToolCallErrors:    toolErrors,
			ToolErrorSamples:  toolErrorSamples,
			ToolCallDurations: toolDurations,
			RetrievedDocIDs:   retrievedDocs,
			Serves:            servedDocs,
			ChunkReads:        chunkReads,

			OnDelta: func(contentDelta, thinkingDelta string) {
				startToThink, endToThink := false, false
				if thinkingDelta != "" {
					if !thinking {
						startToThink = true
						thinking = true
					}
				} else if thinking {
					endToThink = true
					thinking = false
				}
				// Markers travel on their own chunks: the frontend appends
				// '<think>' / '</think>' AFTER a chunk's answer text, so a marker
				// riding on a content chunk would strand that text on the wrong side
				// of the think section.
				if startToThink {
					out <- AsyncChatResult{Final: false, StartToThink: true}
				}
				if contentDelta != "" || thinkingDelta != "" {
					out <- AsyncChatResult{Answer: contentDelta, Reasoning: thinkingDelta, Final: false}
				}
				if endToThink {
					out <- AsyncChatResult{Final: false, EndToThink: true}
				}
			},
		})
		elapsed := time.Since(runStart)

		var turnUsage *TurnUsage
		if sink := tokenizer.GetRunUsage(runCtx); sink != nil {
			pt, ct, tt, calls := sink.Snapshot()
			turnUsage = &TurnUsage{
				PromptTokens:     pt,
				CompletionTokens: ct,
				TotalTokens:      tt,
				LLMCalls:         calls,
				LLMTurns:         sink.CallSnapshot(),
			}
			fields := []zap.Field{
				zap.String("chat_id", chat.ID),
				zap.String("mode", string(engineIterative)),
				zap.String("question", lastUserQuestion(messages)),
				zap.Int("calls", calls),
				zap.Int("prompt_tokens", pt),
				zap.Int("completion_tokens", ct),
				zap.Int("total_tokens", tt),
				zap.Float64("elapsed_seconds", elapsed.Seconds()),
				zap.Int("rounds_run", len(rounds)),
				zap.Bool("error", err != nil),
			}
			for name, count := range toolCounts {
				fields = append(fields, zap.Int("tool_"+name, count))
			}
			for name, d := range toolDurations.Snapshot() {
				fields = append(fields, zap.Float64("tool_"+name+"_ms", float64(d.Milliseconds())))
			}
			common.InfoCtx(ctx, "iterative_synthesis: question usage", fields...)
		}

		if err != nil {
			common.ErrorCtx(ctx, "iterative_synthesis: run", err)
			if final == "" {
				final = fmt.Sprintf("**ERROR**: %s", err.Error())
			}
		}
		// The final result must stay free of think markers: the streaming consumer
		// skips any result carrying EndToThink before it checks Final, which would
		// drop the terminating event and its reference payload.
		if thinking {
			out <- AsyncChatResult{Reference: map[string]interface{}{}, Final: false, EndToThink: true}
			thinking = false
		}

		// The deliverable cites its provenance explicitly (`chunk_id: <id>` on every
		// summary line), so the citation payload ports here without the naive
		// pipeline's two probabilistic pillars — see buildAgenticReference.
		reference := map[string]interface{}{}
		if quote {
			reference, final = s.buildAgenticReference(ctx, chat.TenantID, datasetIDs, final, registry)
		}

		deepRead, shallowRead := chunkReads.Snapshot()
		deepReadIDs, shallowReadIDs := chunkReads.ChunkIDs()
		common.InfoCtx(ctx, "iterative_synthesis: shipping final result",
			zap.Int("final_bytes", len(final)),
			zap.Bool("run_err", err != nil))
		out <- AsyncChatResult{
			Answer:              final,
			Reference:           reference,
			Final:               true,
			ToolCallCounts:      toolCounts,
			ToolCallErrors:      toolErrors,
			ToolErrorSamples:    toolErrorSamples,
			RetrievedDocIDs:     retrievedDocs.Snapshot(),
			ServedDocIDs:        servedDocs.Docs(),
			Rounds:              rounds,
			Usage:               turnUsage,
			ElapsedSeconds:      elapsed.Seconds(),
			DeepReadChunks:      deepRead,
			ShallowReadChunks:   shallowRead,
			DeepReadChunkIDs:    deepReadIDs,
			ShallowReadChunkIDs: shallowReadIDs,
		}
	}()

	return out, nil
}
