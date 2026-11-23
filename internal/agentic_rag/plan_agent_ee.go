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
	"ragflow/internal/entity/models"

	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/session"
	einocommon "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"

	"ragflow/internal/common"
)

// plannerTemplateID selects the planner agent from conf/agentic_rag.yaml
// (tools: [check_decomposition]; content holds the planner prompt with the
// worked examples).
const plannerTemplateID = "planner"

// PlannerTemperature resolves the temperature the plan stage's planner runs
// at: the planner template's declared value, else nil - the planner then
// samples at the MODEL's own default (the same contract as the auditors:
// an undeclared temperature means the field is never set, never a number of
// ours). Exported: the chat pipeline builds the planner's model instance.
func PlannerTemperature() *float64 {
	return TemplateTemperature(plannerTemplateID)
}

// PlanAuditorTemplateID selects the independent plan auditor: a separate
// agent that holds the question and the plan and audits the plan against the
// question clause by clause. The planner auditing its own plan is structurally
// blind to the defects it introduces - #36's self-review pruned four
// discriminating clauses and its own clause mapping went blind with them -
// while the one round the delivery-style grading was applied handed q875 its
// first win. Exported: the chat pipeline builds the auditor's model instance.
const PlanAuditorTemplateID = "plan_auditor"

// PlanProducerTemplateID selects the planer-explorer-auditor producer: the one
// template whose prompt declares the two-section run input (`## Resolved
// question` + `## The question decomposition`) and whose deliverable the
// delivery gate audits. A run naming it goes through the plan stage; every other
// template is the plain-explorer shape and carries no plan (see Run). The chat
// pipeline's producer id (service.gateTemplateID) is this same value.
const PlanProducerTemplateID = "planer-explorer-auditor"

// noPlanMarker is what the planner agent returns for a
// purely conversational message - nothing that needs facts, so there is no
// plan to pin and the explorer answers it per its own Intent rule.
const noPlanMarker = "NO-DECOMPOSITION"

// planStageMaxIterations bounds the planner's own ReAct loop: it writes
// one plan, calls the checker once per revision and emits the plan once the
// checker is clean. Twenty leaves room for a duplicate call and two repair
// rounds - twelve was measured to be too tight (q1005, smoke #16: the planner
// called the checker twice per iteration and ran out of iterations with a
// clean plan in hand, so the stage returned nothing).
const planStageMaxIterations = 20

// planStageMaxRepairRounds caps the Go-side repair loop: after the agent
// returns a plan, checkDecomposition is run HERE (deterministic, free - the
// adoption of the in-agent checker cannot be taken on trust), and a plan that
// still carries findings is sent back for repair at most this many times.
// An exhausted budget pins the last plan unverified: availability over
// perfection - the explorer works the plan as written and the delivery gate
// sees the outcome.
const planStageMaxRepairRounds = 2

// planStageAuditRounds caps the audit-driven repair loop that follows a
// mechanically clean plan: each round is one plan-auditor turn plus one
// planner repair turn, and both are the cheapest turns in the pipeline - no
// retrieval, no chunk reads, a few KB of prompt - while the plan is the
// single input the explorer, the auditor and the delivery gate all work
// from. Eight audits beat one shipped plan that drops clauses: the budget is
// deliberately generous because the stage's tokens cost a fraction of the
// explorer's.
const planStageAuditRounds = 8

// planHeadRe finds the first block heading in the agent's final
// message: the plan is everything from there down (the agent may fence the
// plan or prefix one short line; the trailing fence is trimmed).
var planHeadRe = regexp.MustCompile(`(?m)^### Sub-question`)

// NewPlannerAgent builds the stage-one agent: same construction
// as the answer auditor (a standalone ChatModelAgent over its own template),
// with one difference - its toolset holds ONLY the checker, because the one
// thing this stage must never do is retrieve.
func NewPlannerAgent(
	ctx context.Context,
	model einocommon.BaseChatModel,
	tenantID string,
	datasetIDs []string,
	question string,
	toolDurations *durationAccumulator,
) (*adk.ChatModelAgent, error) {
	tmpl, err := resolveTemplateFor(plannerTemplateID)
	if err != nil {
		return nil, fmt.Errorf("plan stage: %w", err)
	}
	// Same tool wrapping as the auditor: the checker's calls land in the run's
	// shared duration ledger, so per-question cost accounting covers this
	// stage too (the COUNT comes from consumeAgentEvents' tally instead).
	tools := toolsFor(ctx, tmpl, tenantID, datasetIDs, nil)
	if toolDurations != nil {
		wrapped := make([]tool.BaseTool, len(tools))
		for i, t := range tools {
			if it, ok := t.(tool.InvokableTool); ok {
				wrapped[i] = &instrumentedTool{InvokableTool: it, acc: toolDurations}
			} else {
				wrapped[i] = t
			}
		}
		tools = wrapped
	}
	cfg := &adk.ChatModelAgentConfig{
		Name:        tmpl.ID,
		Description: tmpl.Description,
		// Same pinning as the auditor: the question lives in the instruction,
		// so every turn's input is just the directive.
		Instruction:      tmpl.Content + "\n\n## The question to decompose\n\n" + question,
		Model:            model,
		MaxIterations:    planStageMaxIterations,
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
		return nil, fmt.Errorf("plan stage: build agent: %w", err)
	}
	return agent, nil
}

// runPlanStage runs stage one end to end. It returns the plan text to
// pin into the explorer's context, or "" with conversational=true when the
// stage ruled the message purely conversational - the caller then skips the
// explorer-auditor pipeline entirely and answers directly. It also returns ""
// (conversational=false) when there is a plan to pin but the stage failed or
// returned nothing readable: the explorer then works unpinned, exactly as it
// did before this stage existed (availability over perfection).
//
// The agent's own checker calls are only half the verification: after every
// agent round the plan is re-checked HERE with the same deterministic
// checkDecomposition the tool wraps, because two measured runs (#11/#12) showed
// an in-prompt call contract the model simply never exercised. A plan that
// still carries findings after the repair budget is pinned anyway, with a
// warning in the log - the explorer is told the plan may be imperfect.
// resolvedQuestionHeader is the line a plan opens with when the pinned
// question needed resolving against the conversation's earlier turns (see the
// "The resolved question" section of the planner template).
// Everything downstream — the explorer's input, the auditor's pinned
// question — reads THIS line as the question, never the raw fragment.
const resolvedQuestionHeader = "## Resolved question"

// extractResolvedQuestion returns the self-contained question the plan opens
// with, or "" when the plan carries no such line (single-turn runs restate
// the pinned question verbatim, and a planner that skipped the line leaves
// the caller falling back to the raw last user message).
func extractResolvedQuestion(plan string) string {
	i := strings.Index(plan, resolvedQuestionHeader)
	if i < 0 {
		return ""
	}
	rest := strings.TrimPrefix(plan[i+len(resolvedQuestionHeader):], ":")
	if nl := strings.IndexAny(rest, "\r\n"); nl >= 0 {
		rest = rest[:nl]
	}
	return strings.TrimSpace(rest)
}

// runPlanStage derives its inputs from the conversation itself: the
// question is the last user message, and the turns before it are the context
// a follow-up's references resolve against (priorTurns). No caller-side
// splitting of in.Messages — the stage owns the reading of its input.
func runPlanStage(ctx context.Context, in Input) (string, bool, error) {
	question := lastUserQuestion(in.Messages)
	prior := priorTurns(in.Messages)
	if strings.TrimSpace(question) == "" {
		return "", false, nil
	}
	agent, err := NewPlannerAgent(ctx, planStageModelFor(in), in.TenantID, in.DatasetIDs, question, in.ToolCallDurations)
	if err != nil {
		common.WarnCtx(ctx, "agentic_rag: plan stage planner unavailable", zap.Error(err))
		return "", false, nil
	}
	// The stage owns a private conversation: one planner session per Run, no
	// history reuse (the explorer's session semantics do not apply here).
	store := session.NewInMemoryStore[adk.Message](nil)
	conv := newConversation("plan-stage", store)

	directive := "Decompose the question pinned in your instructions."
	plan := ""
	for round := 0; round <= planStageMaxRepairRounds+planStageAuditRounds; round++ {
		input := []adk.Message{schema.UserMessage(directive)}
		if round == 0 && len(prior) > 0 {
			// A multi-turn conversation: the turns before the current question
			// ride along with the first directive, so the planner can resolve
			// the references the pinned fragment makes (it sees no other
			// copy of the history — its session starts empty). Repair and
			// review rounds run on the directive alone: the context is
			// already in the session.
			//
			// An empty message trips some provider APIs (the same reason
			// turnMessages guards its synthetic assistant line), so blank
			// history entries are dropped rather than replayed.
			input = make([]adk.Message, 0, len(prior)+1)
			for _, m := range prior {
				if m != nil && strings.TrimSpace(m.Content) != "" {
					input = append(input, m)
				}
			}
			input = append(input, schema.UserMessage(directive))
		}
		iter := conv.runner(ctx, agent, false).Run(ctx, input)
		final, _, err := consumeAgentEvents(ctx, iter, func(string, string) {}, in.ToolCallCounts, nil, nil)
		if err != nil {
			conv.discardFailedTurn(ctx, conv.head(ctx))
			if plan != "" {
				// A mid-stage failure (e.g. the model exhausting its
				// iteration cap while polishing the draft against the
				// checker) must not erase the stage's work: the last
				// readable draft still goes through the audit loop, where
				// the plan auditor and the repair path do the converging.
				common.WarnCtx(ctx, "agentic_rag: plan stage run failed - falling back to the last readable draft", zap.Error(err))
				break
			}
			common.WarnCtx(ctx, "agentic_rag: plan stage run failed", zap.Error(err))
			// The death rides back as the return error: the upper layer
			// (Run) checks it and makes the run's own error self-describing
			// ("...: 529 overloaded"), instead of the row reading
			// "returned no plan" with the cause buried here (#44's 22 rows).
			return "", false, err
		}
		plan = extractPlan(final)
		if plan == "" {
			if strings.Contains(final, noPlanMarker) {
				common.InfoCtx(ctx, "agentic_rag: plan stage skipped (conversational message)")
				return "", true, nil
			}
			common.WarnCtx(ctx, "agentic_rag: plan stage returned no readable plan")
			return "", false, nil
		}
		findings := checkDecomposition(plan)
		if len(findings) == 0 {
			// Mechanically clean is not yet audited: the independent plan
			// auditor holds the resolved question and walks it clause by
			// clause, so a clause the planner dropped or pruned surfaces as
			// UNMAPPED - the defect class the self-review was structurally
			// blind to (#36: the self-review itself pruned c7-c10 and its own
			// mapping went blind with them). Findings ride back to the
			// planner as the next directive; the loop budget is generous
			// because plan-stage turns are the cheapest in the pipeline.
			auditedQuestion := extractResolvedQuestion(plan)
			if auditedQuestion == "" {
				auditedQuestion = question
			}
			audited := runPlanAuditor(ctx, in, plan, auditedQuestion, question)
			if len(audited) == 0 {
				common.InfoCtx(ctx, "agentic_rag: plan stage verified",
					zap.Int("rounds", round+1), zap.Int("plan_bytes", len(plan)))
				return plan, false, nil
			}
			common.WarnCtx(ctx, "agentic_rag: plan audit reported findings",
				zap.Int("round", round+1), zap.Strings("findings", audited))
			directive = "The independent plan auditor reports:\n- " + strings.Join(audited, "\n- ") +
				"\n\nFix every finding and return ONLY the corrected plan."
			continue
		}
		common.WarnCtx(ctx, "agentic_rag: plan stage failed the mechanical check",
			zap.Int("round", round+1), zap.Strings("findings", findings))
		directive = "check_decomposition reports:\n- " + strings.Join(findings, "\n- ") +
			"\n\nFix every finding and return ONLY the corrected plan."
	}
	common.WarnCtx(ctx, "agentic_rag: plan stage audit budget exhausted - keeping the last mechanically valid plan")
	return plan, false, nil
}

// NewPlanAuditorAgent builds the independent plan auditor: a separate agent
// that holds the resolved question and the plan and audits the plan against
// the question clause by clause (see the plan_auditor template). It is a
// different agent from the planner on purpose - the planner auditing its own
// plan is structurally blind to the defects it introduces: #36's self-review
// pruned four discriminating clauses (c7-c10) and its own clause mapping went
// blind with them, so the plan shipped without the question's second half.
// It owns the checker so its mechanical claims are grounded in the same tool
// the planner used, and it never rewrites - findings only.
func NewPlanAuditorAgent(
	ctx context.Context,
	model einocommon.BaseChatModel,
	tenantID string,
	datasetIDs []string,
	resolved string,
	original string,
	plan string,
	toolDurations *durationAccumulator,
) (*adk.ChatModelAgent, error) {
	tmpl, err := resolveTemplateFor(PlanAuditorTemplateID)
	if err != nil {
		return nil, fmt.Errorf("plan auditor: %w", err)
	}
	tools := toolsFor(ctx, tmpl, tenantID, datasetIDs, nil)
	if toolDurations != nil {
		wrapped := make([]tool.BaseTool, len(tools))
		for i, t := range tools {
			if it, ok := t.(tool.InvokableTool); ok {
				wrapped[i] = &instrumentedTool{InvokableTool: it, acc: toolDurations}
			} else {
				wrapped[i] = t
			}
		}
		tools = wrapped
	}
	cfg := &adk.ChatModelAgentConfig{
		Name:        tmpl.ID,
		Description: tmpl.Description,
		// All three artifacts pinned: the caller's ORIGINAL question (the
		// completeness witness - a constraint tracing to it while the
		// resolved line carries nothing means the RESOLUTION dropped a
		// clause), the resolved question the mapping walks, and the plan the
		// mapping walks it against.
		Instruction:      tmpl.Content + "\n\n## The caller's original question\n\n" + original + "\n\n## Resolved question under audit\n\n" + resolved + "\n\n## The plan under audit\n\n" + plan,
		Model:            model,
		MaxIterations:    planAuditMaxIterations,
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
		return nil, fmt.Errorf("plan auditor: build agent: %w", err)
	}
	return agent, nil
}

// planAuditMaxIterations bounds the plan auditor's own ReAct loop:
// it reads the two pinned artifacts, optionally re-runs the checker to ground
// a mechanical claim, and emits the mapping and findings. Four leaves room
// for a duplicate call and a re-check.
const planAuditMaxIterations = 4

// planAuditFindingsRe extracts one finding line from the plan auditor's
// output: `- UNMAPPED clause: ...` / `- suspect: ...`. `pass` items and the
// Audit Result line are shaped out of the pattern.
var planAuditFindingsRe = regexp.MustCompile(`(?m)^\s*-\s+(UNMAPPED clause:.*|INVENTED constraint:.*|suspect: .*)$`)

// planAuditVerdictRe reads the auditor's own verdict line - the authoritative
// signal the template requires it to end with (`Audit Result: PASS|FAIL`).
// The findings bullets are the detail; the verdict is the contract.
var planAuditVerdictRe = regexp.MustCompile(`(?mi)^\s*\**\s*Audit Result:\s*\**\s*(PASS|FAIL)\b`)

// planAuditCensusRe matches one entity-census line from the plan auditor's
// fixed output: `entity: <what the question calls it> -> ?variable` (the
// variable that carries it) or `-> UNBOUND` (no variable carries it). The
// census is the over-merge audit trail: a prose judgment the pipeline can
// parse, so an unbound entity cannot hide inside a PASS verdict.
var planAuditCensusRe = regexp.MustCompile(`(?im)^\s*[-*]?\s*` + "`" + `?\**entity:` + "`" + `?\s*(.+?)\s*->\s*(\?[A-Za-z0-9_]+|UNBOUND)\b`)

// planAuditSelfRefutingRe matches an UNMAPPED finding line that retracts
// itself: the auditor's own annotation says a constraint covers the clause
// (`- UNMAPPED clause: "X" - no block carries it — not present, c2 covers it`).
// The line asserts opposite things in its two halves; a PASS verdict over
// such lines is the auditor walking its mapping aloud.
var planAuditSelfRefutingRe = regexp.MustCompile(`(?i)unmapped clause.*not present.*covers`)

// planAuditPassPrefixes are the openings a Findings bullet may use to record
// a check that held rather than a defect.
var planAuditPassPrefixes = []string{"pass", "`pass`", "no ", "none "}

// extractPlanAuditFindings returns the auditor's finding lines: the old
// fixed-prefix sweep plus every non-pass bullet of its Findings section. The
// section's wording is the auditor's own (it has flagged drift as
// `c4 paraphrase drift: ...` and structural defects as `Block 7 ...violates
// ...`), so the parse is structural - bullets of the Findings section that do
// not open with a pass marker - rather than prefix-coupled. The second return
// reports whether a Findings section was found at all.
func extractPlanAuditFindings(final string) ([]string, bool) {
	var findings []string
	for _, m := range planAuditFindingsRe.FindAllStringSubmatch(final, -1) {
		findings = append(findings, m[1])
	}
	head := strings.LastIndex(strings.ToLower(final), "findings")
	tail := strings.Index(final, "Audit Result")
	if head < 0 || tail < 0 || tail < head {
		return findings, false
	}
	for _, line := range strings.Split(final[head:tail], "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- ") && !strings.HasPrefix(trimmed, "* ") {
			continue
		}
		t := strings.TrimSpace(strings.TrimLeft(trimmed, "-* "))
		lower := strings.ToLower(t)
		isPass := false
		for _, p := range planAuditPassPrefixes {
			if strings.HasPrefix(lower, p) {
				isPass = true
				break
			}
		}
		if !isPass {
			dup := false
			for _, f := range findings {
				if f == t {
					dup = true
					break
				}
			}
			if !dup {
				findings = append(findings, t)
			}
		}
	}
	return findings, true
}

// planAuditModelFor is the model the independent plan auditor runs on: its
// own instance when the caller built one (pinned sampling, separate failover
// state), else the producer's. Named rule, same shape as auditModelFor.
func planAuditModelFor(in Input) *models.EinoChatModel {
	if in.PlanAuditModel != nil {
		return in.PlanAuditModel
	}
	return in.Model
}

// planStageModelFor is the model the plan stage's planner runs on: a
// dedicated instance when the caller built one (the planner template's
// declared temperature is pinned there), else the producer's.
func planStageModelFor(in Input) *models.EinoChatModel {
	if in.PlanStageModel != nil {
		return in.PlanStageModel
	}
	return in.Model
}

// runPlanAuditor audits the plan with the independent plan auditor and
// returns its findings - empty when the audit passes. An audit outage must
// not cost the run its plan: on error or unreadable output the fallback is
// the plan as written (nil findings), the same contract as every other
// audit-failure path in the pipeline.
func runPlanAuditor(ctx context.Context, in Input, plan, resolved, original string) []string {
	agent, err := NewPlanAuditorAgent(ctx, planAuditModelFor(in), in.TenantID, in.DatasetIDs, resolved, original, plan, in.ToolCallDurations)
	if err != nil {
		common.WarnCtx(ctx, "agentic_rag: plan auditor unavailable", zap.Error(err))
		return nil
	}
	// A private conversation per audit: the auditor sees the two pinned
	// artifacts and nothing of the planner's session.
	store := session.NewInMemoryStore[adk.Message](nil)
	conv := newConversation("plan-audit", store)
	iter := conv.runner(ctx, agent, false).Run(ctx, []adk.Message{schema.UserMessage("Audit the plan pinned in your instructions.")})
	final, _, err := consumeAgentEvents(ctx, iter, func(string, string) {}, in.ToolCallCounts, nil, nil)
	if err != nil {
		conv.discardFailedTurn(ctx, conv.head(ctx))
		common.WarnCtx(ctx, "agentic_rag: plan auditor run failed", zap.Error(err))
		return nil
	}
	// The auditor's full output - mapping, grounding, findings - is the only
	// record of WHY a plan was sent back, and it is a few KB at most: it
	// lands in the debug log WHOLE, never truncated - a cut mapping is an
	// unreadable mapping.
	common.DebugCtx(ctx, "agentic_rag: plan auditor output",
		zap.Int("output_bytes", len(final)),
		zap.String("output", final))
	var findings []string
	verdict := ""
	if m := planAuditVerdictRe.FindStringSubmatch(final); m != nil {
		verdict = m[1]
	}
	if section, _ := extractPlanAuditFindings(final); len(section) > 0 {
		findings = section
	}
	if verdict == "PASS" && len(findings) > 0 {
		// A PASS verdict over self-refuting UNMAPPED lines (each line's own
		// annotation says a constraint covers the clause) is the auditor
		// walking its mapping aloud, not reporting defects: the retracted
		// lines are dropped, and only findings that survive the retraction
		// keep the round alive.
		kept := make([]string, 0, len(findings))
		for _, f := range findings {
			if planAuditSelfRefutingRe.MatchString(f) {
				continue
			}
			kept = append(kept, f)
		}
		if len(kept) < len(findings) {
			common.WarnCtx(ctx, "agentic_rag: plan audit verdict PASS over self-refuting UNMAPPED lines - dropping the retracted findings",
				zap.Int("dropped", len(findings)-len(kept)), zap.Int("kept", len(kept)))
			findings = kept
		}
	}
	// The entity census is the over-merge audit trail: every UNBOUND line is a
	// confession that no variable carries an entity the question distinguishes,
	// and it is a finding regardless of the verdict - #35 r3 shipped a plan
	// whose school entity had no variable because the PASS verdict outranked
	// the census.
	for _, m := range planAuditCensusRe.FindAllStringSubmatch(final, -1) {
		if strings.EqualFold(m[2], "UNBOUND") {
			findings = append(findings, fmt.Sprintf("entity census: %s is UNBOUND - no variable carries it; give it its own block and variable, or split the plan", m[1]))
			common.WarnCtx(ctx, "agentic_rag: plan audit census names an unbound entity",
				zap.String("entity", m[1]))
		}
	}
	switch {
	case len(findings) > 0 && verdict == "PASS":
		// The verdict line says PASS while the findings list names defects:
		// a self-contradicting audit. The findings are the concrete claims -
		// honor them, loudly, so the wording drift of the verdict line does
		// not silently downgrade a reported defect.
		common.WarnCtx(ctx, "agentic_rag: plan audit verdict says PASS but findings are listed - honoring the findings",
			zap.Int("findings", len(findings)))
	case len(findings) == 0 && verdict == "FAIL":
		// The audit failed the plan but wrote its findings in a shape the
		// parser cannot read. Swallowing the verdict would ship a plan the
		// auditor itself rejected - send the repair path a synthetic finding
		// instead (the planner re-examines; the auditor re-runs on the
		// revised plan).
		common.WarnCtx(ctx, "agentic_rag: plan audit verdict FAIL without machine-readable findings - sending the plan back")
		findings = []string{"the audit returned FAIL without finding lines in a machine-readable shape - " +
			"re-examine the plan against the question clause by clause and fix every wording that adds, " +
			"weakens or drops a restriction, then restate the plan"}
	case len(findings) == 0:
		common.InfoCtx(ctx, "agentic_rag: plan audit passed - no findings",
			zap.String("verdict", verdict))
	}
	return findings
}

// runConversationalReply answers a purely conversational message WITHOUT the
// explorer-auditor pipeline: no ReAct loop, no retrieval tools, no delivery
// gate - the plan stage has already ruled that nothing here needs
// facts. One direct generation over the caller's own history produces the
// plain-prose reply; the same emit convention as Run ships it.
func runConversationalReply(ctx context.Context, in Input) (string, error) {
	instruction := "You are RAGFlow. The last message is purely conversational - a greeting, thanks or farewell, nothing that needs facts or retrieval. Reply in plain prose, briefly and naturally. Do not use tools, and do not produce a Candidate Matrix, evidence lines or a Final Answer label."
	msgs := make([]*schema.Message, 0, len(in.Messages)+1)
	msgs = append(msgs, schema.SystemMessage(instruction))
	msgs = append(msgs, in.Messages...)
	res, err := in.Model.Generate(ctx, msgs)
	if err != nil {
		return "", fmt.Errorf("conversational reply: %w", err)
	}
	reply := ""
	if res != nil {
		reply = res.Content
	}
	emit(ctx, in.OnDelta, reply, "")
	return reply, nil
}

// extractPlan returns the plan portion of the agent's final message: everything
// from the first `### Sub-question` heading down, with a trailing code fence
// trimmed. Empty when the message carries no plan at all.
func extractPlan(final string) string {
	i := strings.Index(final, "### Sub-question")
	if i < 0 {
		return ""
	}
	// A `## Resolved question` line written before the first block is part of
	// the plan: it is what every downstream consumer reads as the question
	// (see extractResolvedQuestion). Prose without the header stays excluded.
	if j := strings.LastIndex(final[:i], resolvedQuestionHeader); j >= 0 {
		i = j
	}
	plan := strings.TrimSpace(final[i:])
	plan = strings.TrimSuffix(plan, "```")
	return strings.TrimSpace(plan)
}
