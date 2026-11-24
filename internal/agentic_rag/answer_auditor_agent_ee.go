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
	"regexp"

	"github.com/cloudwego/eino/adk"
	einocommon "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
)

// answerAuditorTemplateID selects the auditor agent from
// conf/agentic_rag.yaml (tools: [list_chunks]; content holds its audit prompt).
const answerAuditorTemplateID = "answer_auditor"

// answerAuditorMaxIterations bounds the auditor's own ReAct loop: it only
// deep-reads a handful of cited chunks per chain step, so ten rounds are
// generous.
const answerAuditorMaxIterations = 10

// AuditTemperature resolves the temperature the auditor must run at: the
// auditor template's `temperature` when it declares one, else nil - the
// auditor then runs at the MODEL's own default, because the temperature field
// is simply never set (an undeclared auditor is not pinned to any number).
// The caller builds the auditor's model instance with it (an operator knob,
// reloaded from disk like audit_max_pass).
func AuditTemperature() *float64 {
	return TemplateTemperature(answerAuditorTemplateID)
}

// NewAnswerAuditorAgent builds the answer_auditor auditor as a standalone ADK
// ChatModelAgent bound to list_chunks only (deep-read of cited chunks; it has
// no locate tools, so a deliverable's Searched patterns are trusted as
// declared — see the template's step 2b/2d). It is deliberately NOT part of
// the main agent's toolset: the delivery gate drives it directly inside a
// runner-managed SESSION, whose event log keeps one continuous conversation
// across audit passes — the auditor remembers its earlier opinions and the
// chunks it already deep-read, so a re-audit focuses on what changed instead
// of re-reading everything. Every pass hands the gate nothing but its payload;
// the session replays the rest (see run_session.go).
// question pins the audited question into the auditor's system prompt, so
// every audit payload carries just the deliverable (final_message) and the
// question is never re-sent per pass.
// toolDurations and retrievedDocs are the run's shared ledgers (see
// Input.ToolCallDurations / Input.RetrievedDocIDs): the auditor's deep-reads
// land in the same tallies as the explorer's, so per-question usage and
// retrieval-coverage accounting covers the whole turn.
func NewAnswerAuditorAgent(
	ctx context.Context,
	model einocommon.BaseChatModel,
	tenantID string,
	datasetIDs []string,
	question string,
	toolDurations *durationAccumulator,
	retrievedDocs *docIDLedger,
	chunkReads *chunkReadLedger,
	searches *searchLedger,
) (*adk.ChatModelAgent, error) {
	tmpl, err := resolveTemplateFor(answerAuditorTemplateID)
	if err != nil {
		return nil, fmt.Errorf("answer auditor: %w", err)
	}

	// The question handed in is the stage-one RESOLVED question (see
	// extractResolvedQuestion): the caller's question with conversational
	// references resolved, so the clause-coverage checks operate on what the
	// conversation is actually asking, not on a follow-up fragment.
	instruction := tmpl.Content + "\n\n## Resolved question under audit\n\n" + question

	// Wrap the auditor's tools with the SAME timing accumulator the explorer
	// uses, so per-question usage accounting sees auditor deep-reads too
	// (a bare list_chunks in the audit pass used to land in the durations but
	// never in the counts — the two ledgers disagreed).
	// toolsFor injects the conversation's web search tool when it has a
	// provider, so the auditor gets one too — a web-cited line can be
	// corroborated by re-running its query instead of being taken on trust.
	// Without a provider the auditor is built with no such tool and the
	// template's "list_chunks only" wording stays literally true.
	tools := toolsFor(ctx, tmpl, tenantID, datasetIDs, nil)
	if toolDurations != nil || retrievedDocs != nil || chunkReads != nil {
		wrapped := make([]tool.BaseTool, len(tools))
		for i, t := range tools {
			if it, ok := t.(tool.InvokableTool); ok {
				wrapped[i] = &instrumentedTool{
					InvokableTool: it,
					acc:           toolDurations,
					docs:          retrievedDocs,
					chunks:        chunkReads,
				}
			} else {
				wrapped[i] = t
			}
		}
		tools = wrapped
	}
	// The search ledger reader is appended AFTER the instrumentation wrap on
	// purpose: the auditor's reads of the ledger are not retrieval actions
	// and must not record themselves into the facts they report. The ledger
	// is the run's ground truth of what the explorer actually searched -
	// recorded mechanically at tool execution, while the deliverable's
	// Searched lines are the model's end-of-run reconstruction (measurably
	// lossy: one #30 run executed 59 distinct searches and its matrix carried
	// 8). Coverage judgements read the ledger; the deliverable is audited for
	// the claims it makes about its own behavior.
	tools = append(tools, NewReadSearchLedgerTool(searches))

	cfg := &adk.ChatModelAgentConfig{
		Name:          tmpl.ID,
		Description:   tmpl.Description,
		Instruction:   instruction,
		Model:         model,
		MaxIterations: answerAuditorMaxIterations,
		// Same transient-failure retry as the main agent: a 529 overload in
		// the middle of a gate audit pass must not cost the whole pass.
		ModelRetryConfig: agentModelRetryConfig(),
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools:               tools,
				ExecuteSequentially: false,
			},
		},
	}
	agent, err := adk.NewChatModelAgent(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("answer auditor: build agent: %w", err)
	}
	return agent, nil
}

// auditVerdictRe parses the auditor's overall verdict line - the LAST line of
// its md output, exactly `Audit Result: PASS` or `Audit Result: FAIL (M suspects)`.
var auditVerdictRe = regexp.MustCompile(`(?im)^[^\S\n]*Audit Result:\s*(PASS|FAIL)\b`)

// auditSuspectCountRe extracts M from `Audit Result: FAIL (M suspects)`.
var auditSuspectCountRe = regexp.MustCompile(`(?i)Audit Result:\s*FAIL\s*\((\d+)\s+suspects?\)`)

// auditPassed reports whether the auditor's last output concluded
// `Audit Result: PASS`. An empty or malformed verdict counts as NOT passed:
// the gate routes it to a suspects directive and the loop stays bounded.
func auditPassed(verdict string) bool {
	m := auditVerdictRe.FindStringSubmatch(verdict)
	return m != nil && m[1] == "PASS"
}

// auditSuspectCount extracts M from `Audit Result: FAIL (M suspects)`, or 0.
func auditSuspectCount(verdict string) int {
	m := auditSuspectCountRe.FindStringSubmatch(verdict)
	if m == nil {
		return 0
	}
	n := 0
	for _, r := range m[1] {
		n = n*10 + int(r-'0')
	}
	return n
}
