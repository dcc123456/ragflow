//go:build ee

package agentic_rag

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// testAuditMaxPass is the audit budget the gate tests hand to
// runDeliveryGate. Production reads the same knob from the template
// (Template.AuditMaxPass, `audit_max_pass` in conf/agentic_rag.yaml); the
// tests pin it so a budget edit in the yaml cannot silently change what the
// gate tests assert.
const testAuditMaxPass = 15

func TestAuditVerdictParsing(t *testing.T) {
	cases := []struct {
		verdict  string
		passed   bool
		suspects int
	}{
		{verdict: "Audit Result: PASS", passed: true, suspects: 0},
		{verdict: "line above\nAudit Result: FAIL (2 suspects)", passed: false, suspects: 2},
		{verdict: "Audit Result: FAIL (12 suspects)", passed: false, suspects: 12},
		{verdict: "Audit Result: FAIL (0 suspects)", passed: false, suspects: 0},
		{verdict: "", passed: false, suspects: 0},
		{verdict: "audit result: fail (1 suspect)", passed: false, suspects: 1},
	}
	for _, tc := range cases {
		if got := auditPassed(tc.verdict); got != tc.passed {
			t.Errorf("auditPassed(%q) = %v, want %v", tc.verdict, got, tc.passed)
		}
		if got := auditSuspectCount(tc.verdict); got != tc.suspects {
			t.Errorf("auditSuspectCount(%q) = %d, want %d", tc.verdict, got, tc.suspects)
		}
	}
}

// The FOS-structure sentinel distinguishes an audit-failed DELIVERABLE
// (worth shipping verbatim) from pure narration (only synthesis can help).
func TestHasFOSStructure(t *testing.T) {
	cases := []struct {
		final string
		want  bool
	}{
		{final: "## Candidate Matrix\n### Sub-question 1: x\n- Retained: Bob\n## Reasoning Chain\n- Clue: x\n", want: true},
		{final: "### Candidate Matrix only, no chain\n", want: true},
		{final: "prose about the search, then:\n## Reasoning Chain heading\n", want: true},
		{final: "Let me re-render the deliverable. Please audit it.\n", want: false},
		{final: "", want: false},
	}
	for _, tc := range cases {
		if got := hasFOSStructure(tc.final); got != tc.want {
			t.Errorf("hasFOSStructure(%q) = %v, want %v", tc.final, got, tc.want)
		}
	}
}

func TestBuildAuditPayload(t *testing.T) {
	final := "## Candidate Matrix\n" +
		"### Sub-question 1: who signed\n" +
		"- Retained: Bob\n" +
		"## Reasoning Chain\n" +
		"- Clue: Bob signed in 1897 (doc: a.md, doc_id: d1, chunk_id: c1, snippet: \"...\")\n" +
		"Final Answer: **1897**\n"

	p := buildAuditPayload(final, nil, nil)
	var decoded auditPayload
	if err := json.Unmarshal([]byte(p), &decoded); err != nil {
		t.Fatalf("buildAuditPayload produced invalid JSON: %v\n%s", err, p)
	}
	// Verbatim passthrough: the auditor reads and echoes the FINAL message's
	// own md structure, so the gate must not reshape it.
	if decoded.FinalMessage != final {
		t.Errorf("final_message not verbatim:\n got %q\nwant %q", decoded.FinalMessage, final)
	}
	// The label is the ONE thing the gate still states about the deliverable:
	// everything else about the text is the auditor's judgement.
	if decoded.GateAnswerLabel != "final" {
		t.Errorf("gate_answer_label = %q, want %q", decoded.GateAnswerLabel, "final")
	}
	// The payload's KEY SET is the contract. A field added here without the
	// auditor's prompt learning about it is exactly the drift that left
	// gate_prechecks documented as evidence the gate could no longer send, so
	// pin the shape: the deliverable, and the label.
	keys := payloadKeys(t, p)
	if len(keys) != 2 || !keys["final_message"] || !keys["gate_answer_label"] {
		t.Errorf("payload fields = %v, want final_message + gate_answer_label only", keys)
	}
	// A deliverable with no label states that by OMITTING the label, not by
	// carrying a separate "there is no answer line" field: the auditor reads
	// the answer's absence off final_message itself.
	bare := buildAuditPayload("just prose, with no answer line at all", nil, nil)
	if keys := payloadKeys(t, bare); len(keys) != 1 || !keys["final_message"] {
		t.Errorf("an unlabelled deliverable must carry final_message alone, got %v", keys)
	}
}

// payloadKeys decodes a payload's top-level field names.
func payloadKeys(t *testing.T, payload string) map[string]bool {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &raw); err != nil {
		t.Fatalf("payload is not a JSON object: %v\n%s", err, payload)
	}
	keys := make(map[string]bool, len(raw))
	for key := range raw {
		keys[key] = true
	}
	return keys
}

func TestBuildAuditPayloadEmptyFinal(t *testing.T) {
	p := buildAuditPayload("", nil, nil)
	var decoded auditPayload
	if err := json.Unmarshal([]byte(p), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	// A narration/empty final passes through verbatim; the auditor (not the
	// gate) flags it as schema integrity.
	if decoded.FinalMessage != "" {
		t.Errorf("unexpected payload %+v", decoded)
	}
}

// fakeAuditorAgent stands in for the answer_auditor agent: it records the
// message list each Run received and emits one assistant verdict. Satisfying
// adk.Agent lets the gate drive it through a real Runner, so these exercises
// cover the session machinery too — including the history the Runner replays.
type fakeAuditorAgent struct {
	gotMsgs  []*schema.Message
	priorLen int
	runs     int
	verdict  string
	// verdictSeq, when set, supplies the verdict per run (1-based, cycling) so
	// a test can script a suspect count that moves between passes. Empty means
	// the fixed verdict is used.
	verdictSeq []string
	// emitted records every verdict handed out, for diagnosing a loop that
	// stopped on an unexpected pass.
	emitted    []string
	toolResult string
}

func (f *fakeAuditorAgent) Name(context.Context) string { return "answer_auditor" }
func (f *fakeAuditorAgent) Description(context.Context) string {
	return "scripted stand-in for the auditor"
}

func (f *fakeAuditorAgent) Run(_ context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	f.runs++
	f.gotMsgs = input.Messages
	f.priorLen = len(input.Messages) - 1 // minus the payload user message
	// Resolved on the calling goroutine: reading f.runs from inside the
	// goroutine below races with the NEXT Run's increment, which made a
	// scripted suspect sequence skip entries and stall detection fire at
	// inconsistent passes (observed flip-flopping 19 ↔ 14 audits).
	verdict := f.verdictAt(f.runs)
	f.emitted = append(f.emitted, verdict)
	toolResult := f.toolResult
	go func() {
		if toolResult != "" {
			gen.Send(assistantMsgEvent("deep-read", []schema.ToolCall{
				{ID: "c1", Function: schema.FunctionCall{Name: "list_chunks", Arguments: "{}"}},
			}))
			gen.Send(&adk.AgentEvent{Output: &adk.AgentOutput{
				MessageOutput: &adk.TypedMessageVariant[*schema.Message]{
					Role: schema.Tool,
					Message: &schema.Message{
						Role: schema.Tool, ToolCallID: "c1", Content: toolResult,
					},
				},
			}})
		}
		gen.Send(assistantMsgEvent(verdict, nil))
		gen.Close()
	}()
	return iter
}

// verdictAt returns the verdict for the n-th run (1-based). A non-empty
// verdictSeq CYCLES, so a test can script a repeating suspect count without
// spelling out a ceiling-length slice.
func (f *fakeAuditorAgent) verdictAt(n int) string {
	if len(f.verdictSeq) == 0 {
		return f.verdict
	}
	return f.verdictSeq[(n-1)%len(f.verdictSeq)]
}

func TestGateRunAuditReturnsVerdict(t *testing.T) {
	fake := &fakeAuditorAgent{verdict: "## Reasoning Chain\n- Clue: x\n  - audit: pass\nFinal Answer: **1**\n  - audit: pass\nAudit Result: PASS"}

	verdict, err := gateRunAudit(context.Background(), fake, newTestConversation(),
		"Final Answer: **1897**", nil, nil, nil, NewSearchLedger(), nil)
	if err != nil {
		t.Fatalf("gateRunAudit error: %v", err)
	}
	if len(fake.gotMsgs) != 1 || fake.gotMsgs[0].Role != schema.User {
		t.Fatalf("first audit must hand over nothing but its payload, got %v", fake.gotMsgs)
	}
	if !strings.Contains(fake.gotMsgs[0].Content, `"final_message":"Final Answer: **1897**"`) {
		t.Errorf("payload missing the deliverable: %s", fake.gotMsgs[0].Content)
	}
	if !auditPassed(verdict) {
		t.Errorf("verdict = %q, want a PASS verdict", verdict)
	}
}

// flakyExplorer fails its first `failures` Runs with a tool error, then
// answers normally — the shape of an Elasticsearch outage mid-repair.
type flakyExplorer struct {
	failures int
	runs     int
	msgs     []*schema.Message
	// reply, when set, replaces the default continuation. The re-anchor round's
	// enforcement reads the continuation's `Searched:` lines, so a test that wants
	// that round ADOPTED has to script a reply that carries one. replies is the
	// per-attempt sequence (attempt i uses replies[i]); both fall back to the
	// default continuation.
	reply   string
	replies []string
}

func (f *flakyExplorer) Name(context.Context) string { return "flaky-explorer" }
func (f *flakyExplorer) Description(context.Context) string {
	return "scripted stand-in for the explorer"
}

func (f *flakyExplorer) Run(_ context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	f.runs++
	f.msgs = input.Messages
	failing := f.runs <= f.failures
	go func() {
		if failing {
			gen.Send(&adk.AgentEvent{Err: errors.New("Elasticsearch query failed: connection refused")})
			gen.Close()
			return
		}
		reply := ""
		switch {
		case len(f.replies) >= f.runs:
			reply = f.replies[f.runs-1]
		case f.reply != "":
			reply = f.reply
		default:
			reply = "## Reasoning Chain\n- Clue: x\nFinal Answer: **1897**"
		}
		gen.Send(assistantMsgEvent(reply, nil))
		gen.Close()
	}()
	return iter
}

// A repair turn that keeps aborting on a tool error is retried IN PLACE — the
// audit is NOT re-run, because the deliverable under repair never changed and
// re-auditing it would only reproduce the verdict already in hand.
func TestRunDeliveryGateRetriesToolFailureWithoutReAuditing(t *testing.T) {
	auditor := &fakeAuditorAgent{verdict: "Audit Result: FAIL (1 suspects)"}
	explorer := &flakyExplorer{failures: 99} // every repair turn aborts
	in := deliveryGateInput{
		explorer:     explorer,
		auditor:      auditor,
		baseMessages: []*schema.Message{schema.UserMessage("q?")},
		final:        "old final",
		auditMaxPass: testAuditMaxPass,
	}

	final, audited, _, _ := runDeliveryGate(context.Background(), in)
	if auditor.runs != 1 {
		t.Errorf("audits = %d, want 1 - a failed repair must not trigger a re-audit", auditor.runs)
	}
	if explorer.runs != gateNoProgressLimit {
		t.Errorf("repair attempts = %d, want gateNoProgressLimit=%d", explorer.runs, gateNoProgressLimit)
	}
	if final != "old final" || audited != "old final" {
		t.Errorf("final = %q / audited = %q, want the standing final preserved", final, audited)
	}
	// The retry directive must name the failure so the model can avoid the call.
	if !strings.Contains(explorer.msgs[len(explorer.msgs)-1].Content, "TOOL FAILURE NOTICE") {
		t.Errorf("retry directive does not name the failure: %s", explorer.msgs[len(explorer.msgs)-1].Content)
	}
}

// A template with no audit budget ships unaudited: the gate is not entered at
// all, so neither the auditor nor a repair turn ever runs. This is the shape
// `audit_max_pass` absent (or 0) takes, and it is what keeps the auditor
// template itself from auditing its own audits.
func TestRunDeliveryGateSkipsAuditWithoutBudget(t *testing.T) {
	auditor := &fakeAuditorAgent{verdict: "Audit Result: FAIL (1 suspects)"}
	explorer := &flakyExplorer{}
	in := deliveryGateInput{
		explorer:     explorer,
		auditor:      auditor,
		baseMessages: []*schema.Message{schema.UserMessage("q?")},
		final:        "old final",
		auditMaxPass: 0,
	}

	final, audited, _, _ := runDeliveryGate(context.Background(), in)
	if auditor.runs != 0 || explorer.runs != 0 {
		t.Errorf("audits = %d / repair turns = %d, want 0 each - no budget means no gate", auditor.runs, explorer.runs)
	}
	if final != "old final" {
		t.Errorf("final = %q, want the standing final untouched", final)
	}
	if audited != "" {
		t.Errorf("auditedFinal = %q, want empty (nothing was audited, so there is no fallback candidate)", audited)
	}
}

// The budget is the ceiling: a template that names 3 gets 3 audits, however
// promising the repair curve still looks. Same falling-count script as the
// full-budget test, so only the ceiling differs.
func TestRunDeliveryGateStopsAtTemplateBudget(t *testing.T) {
	falling := make([]string, 0, testAuditMaxPass+1)
	for n := 20; n >= 1; n-- {
		falling = append(falling, fmt.Sprintf("Audit Result: FAIL (%d suspects)", n))
	}
	auditor := &fakeAuditorAgent{verdictSeq: falling}
	in := deliveryGateInput{
		explorer:     &flakyExplorer{},
		auditor:      auditor,
		baseMessages: []*schema.Message{schema.UserMessage("q?")},
		final:        "old final",
		auditMaxPass: 3,
	}

	_, _, _, _ = runDeliveryGate(context.Background(), in)
	if auditor.runs != 3 {
		t.Errorf("audits = %d, want 3 - the template's budget, not a hardcoded ceiling", auditor.runs)
	}
}

// A repair that DOES advance the deliverable is re-audited — that is the only
// case where a fresh audit says something new. The count falls on every pass
// here, so the stall check never fires and the ceiling is what stops the loop.
func TestRunDeliveryGateReAuditsAfterAdoptingRepair(t *testing.T) {
	// A strictly falling count (20, 19, 18, ...): the newest verdict is always
	// below both of its predecessors.
	falling := make([]string, 0, testAuditMaxPass+1)
	for n := 20; n >= 1; n-- {
		falling = append(falling, fmt.Sprintf("Audit Result: FAIL (%d suspects)", n))
	}
	auditor := &fakeAuditorAgent{verdictSeq: falling}
	explorer := &flakyExplorer{} // every repair returns a deliverable-shaped final
	in := deliveryGateInput{
		explorer:     explorer,
		auditor:      auditor,
		baseMessages: []*schema.Message{schema.UserMessage("q?")},
		final:        "old final",
		auditMaxPass: testAuditMaxPass,
	}

	final, _, _, _ := runDeliveryGate(context.Background(), in)
	if auditor.runs != testAuditMaxPass {
		t.Errorf("audits = %d, want auditMaxPass=%d (each adopted repair deserves a re-audit)",
			auditor.runs, testAuditMaxPass)
	}
	if !hasAnswerLine(final) {
		t.Errorf("final = %q, want the adopted repair deliverable", final)
	}
}

// A count that never falls is the plain stall: the newest verdict matches both
// of its predecessors, so the third observation ends the NORMAL repair sequence.
// q99 sat on 4 suspects for 16 passes, q481 on 5 for 12, q371 on 9 for 9 — the
// ceiling used to let each spend 20 passes to establish what three had already
// shown.
//
// A stall now buys ONE re-anchor round instead of ending outright, and this fake
// never adds the `Searched:` line that round requires, so every attempt of it is
// rejected (gateNoProgressLimit of them) and it never reaches the auditor: the
// audit count stays at the stall window and the deliverable in hand ships.
// TestRunDeliveryGateReanchorRoundAdoptsANewAnchor pins the adopted path.
func TestRunDeliveryGateStopsWhenSuspectsStall(t *testing.T) {
	auditor := &fakeAuditorAgent{verdict: "Audit Result: FAIL (5 suspects)"}
	explorer := &flakyExplorer{} // every repair is adopted; the count never moves
	in := deliveryGateInput{
		explorer:     explorer,
		auditor:      auditor,
		baseMessages: []*schema.Message{schema.UserMessage("q?")},
		final:        "old final",
		auditMaxPass: testAuditMaxPass,
	}

	final, _, _, _ := runDeliveryGate(context.Background(), in)
	if auditor.runs != gateStallWindow {
		t.Errorf("audits = %d, want gateStallWindow=%d - identical verdicts must not buy more passes",
			auditor.runs, gateStallWindow)
	}
	// gateStallWindow-1 adopted repairs, then the re-anchor round's rejections: a
	// continuation that re-renders the same matrix with no new `Searched:` line is
	// discarded, and that round ends on gateNoProgressLimit.
	if want := gateStallWindow - 1 + gateNoProgressLimit; explorer.runs != want {
		t.Errorf("repair turns = %d, want %d (gateStallWindow-1 adopted repairs, then gateNoProgressLimit rejected re-anchor attempts)",
			explorer.runs, want)
	}
	// A stall ships the deliverable in hand - the same one the loop would have
	// ended on after burning the full ceiling.
	if !hasAnswerLine(final) {
		t.Errorf("final = %q, want the repaired deliverable shipped", final)
	}
}

// A low suspect count gets NO extra slack. An earlier rule gave counts of two
// or fewer 15 passes, earned by q667 (a 13-pass stall at 2 suspects that then
// cleared and scored 4) - but once the candidate-matrix audit was removed, runs
// began freezing at 1 suspect instead, and that slack only bought 17-20 passes
// of a repair going nowhere. The trend test is flat: three observations, same
// rule whatever the count.
func TestRunDeliveryGateGivesLowSuspectsNoExtraSlack(t *testing.T) {
	auditor := &fakeAuditorAgent{verdict: "Audit Result: FAIL (2 suspects)"}
	explorer := &flakyExplorer{}
	in := deliveryGateInput{
		explorer:     explorer,
		auditor:      auditor,
		baseMessages: []*schema.Message{schema.UserMessage("q?")},
		final:        "old final",
		auditMaxPass: testAuditMaxPass,
	}

	final, _, _, _ := runDeliveryGate(context.Background(), in)
	if auditor.runs != gateStallWindow {
		t.Errorf("audits = %d, want gateStallWindow=%d - low counts are cut as fast as high ones",
			auditor.runs, gateStallWindow)
	}
	if !hasAnswerLine(final) {
		t.Errorf("final = %q, want the repaired deliverable shipped", final)
	}
}

// Regression against the observed frames runs that motivated the split. These
// are the real per-pass suspect counts: q99 and q481 froze and never cleared
// (both scored 0 and 4 respectively on the deliverable they already held).
// q667 is the one the flat rule gives up: it broke a 13-pass stall at 2
// suspects and passed, and this cut takes it at pass 7. That is the accepted
// price of the change — five stalls of that shape were observed and only one
// ever broke, while the slack that protected them cost 112 passes over 13
// questions once runs started freezing at 1 suspect.
func TestRunDeliveryGateStallOnObservedFramesSequences(t *testing.T) {
	cases := []struct {
		name string
		// suspects holds the observed per-pass count; 0 marks a pass that PASSED.
		suspects []int
		wantRuns int
	}{
		{
			name:     "q99 bm25: 15→11→6→5 then frozen at 4 (ended FAIL, scored 0)",
			suspects: []int{15, 11, 6, 5, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4},
			wantRuns: 7, // cut at the third identical 4-suspect verdict
		},
		{
			name:     "q481 reasoning: 16→5 then frozen at 5 (ended FAIL, scored 4)",
			suspects: []int{16, 5, 5, 5, 5, 5, 5, 5, 6, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5},
			wantRuns: 4, // 16 then three identical 5s
		},
		{
			name:     "q667 bm25: 2-suspect stall of 13, then cleared (scored 4)",
			suspects: []int{5, 6, 3, 3, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 1, 0},
			wantRuns: 7, // cut at the third 2-suspect verdict — this run is the
			// flat rule's known cost: left alone it cleared on pass 18.
		},
		{
			name:     "q617 reasoning: frozen at 1 suspect (ended FAIL, scored 0)",
			suspects: []int{4, 3, 2, 2, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
			wantRuns: 7, // falls to 1 and freezes there; the third 1 ends it
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seq := make([]string, 0, len(tc.suspects))
			for _, n := range tc.suspects {
				if n == 0 {
					seq = append(seq, "Audit Result: PASS")
					continue
				}
				seq = append(seq, fmt.Sprintf("Audit Result: FAIL (%d suspects)", n))
			}
			auditor := &fakeAuditorAgent{verdictSeq: seq}
			explorer := &flakyExplorer{}
			in := deliveryGateInput{
				explorer:     explorer,
				auditor:      auditor,
				baseMessages: []*schema.Message{schema.UserMessage("q?")},
				final:        "old final",
				auditMaxPass: testAuditMaxPass,
			}

			_, _, _, _ = runDeliveryGate(context.Background(), in)
			if auditor.runs != tc.wantRuns {
				t.Errorf("audits = %d, want %d (repair turns = %d)\nverdicts: %v",
					auditor.runs, tc.wantRuns, explorer.runs, auditor.emitted)
			}
		})
	}
}

// The window holds the last three observations, and a count that climbs back
// counts as stalled even though no two adjacent verdicts agree: f1 < f2 < f3
// means the repair is making the deliverable worse. An earlier rule that
// needed three EQUAL counts let this run on.
func TestRunDeliveryGateStopsWhenSuspectsClimbBack(t *testing.T) {
	auditor := &fakeAuditorAgent{verdictSeq: []string{
		"Audit Result: FAIL (9 suspects)", // f1
		"Audit Result: FAIL (7 suspects)", // f2 — falling, keep going
		"Audit Result: FAIL (6 suspects)", // f3 — still falling, keep going
		"Audit Result: FAIL (7 suspects)", // window [7,6,7]: 7 >= 7 and >= 6 → stop
	}}
	explorer := &flakyExplorer{}
	in := deliveryGateInput{
		explorer:     explorer,
		auditor:      auditor,
		baseMessages: []*schema.Message{schema.UserMessage("q?")},
		final:        "old final",
		auditMaxPass: testAuditMaxPass,
	}

	final, _, _, _ := runDeliveryGate(context.Background(), in)
	if auditor.runs != 4 {
		t.Errorf("audits = %d, want 4 - a count climbing back is not progress", auditor.runs)
	}
	if want := 3 + gateNoProgressLimit; explorer.runs != want {
		t.Errorf("repair turns = %d, want %d (3 adopted repairs, then the re-anchor round's rejected attempts)", explorer.runs, want)
	}
	if !hasAnswerLine(final) {
		t.Errorf("final = %q, want the repaired deliverable shipped", final)
	}
}

// TestServedUnreadDocsRanksAndLabelsTheCensus pins the gate's served-unread
// census: most-served first, each lead labelled with the count the retrieval
// served it, deep-read documents excluded, capped at the caller's limit, and a
// document below the lead threshold ENDS the walk because Docs() is ranked.
//
// The label is what the audit opinion and the log report, and the cap is why
// the re-anchor round asks for a longer list than the auditor: on the smoke53
// run q1093's two answer-bearing documents had been served twice each and ranked
// below ten louder leads, so the auditor's ten never mentioned them.
//
// The weight is the DISTINCT QUERY count, never the chunk count: measured
// 2026-10-05, a latched document reached 35 chunk-serves while every unread lead
// stood at 2-4, so the census's whole top five was that one document. A repeat
// of the SAME query is one search (pinned below), which is what stops a run from
// pumping its own anchor to the top of the list of leads it has not considered.
func TestServedUnreadDocsRanksAndLabelsTheCensus(t *testing.T) {
	served := NewServedLedger()
	served.Add("q1", "b")
	served.Add("q2", "b")
	served.Add("q1", "a")
	served.Add("q2", "a")
	served.Add("q1", "c")
	served.Add("q2", "c")
	served.Add("q3", "c")
	served.Add("q3", "c") // same query again: still ONE search
	deep := NewDocIDLedger()
	deep.Add("c") // opened: no longer a lead

	want := "[a (x2) b (x2)]"
	if got := fmt.Sprintf("%v", servedUnreadDocs(served, deep, servedUnreadAuditorMax)); got != want {
		t.Fatalf("census = %s, want %s (most-served first, ties by id, deep reads excluded)", got, want)
	}
	if got := fmt.Sprintf("%v", servedUnreadDocs(served, deep, 1)); got != "[a (x2)]" {
		t.Fatalf("capped census = %s, want [a (x2)]", got)
	}
	served.Add("q4", "d") // one query: below the lead threshold
	if got := fmt.Sprintf("%v", servedUnreadDocs(served, deep, servedUnreadAuditorMax)); got != want {
		t.Fatalf("census after a single serve = %s, want %s", got, want)
	}
	if got := fmt.Sprintf("%v", servedUnreadDocs(nil, deep, servedUnreadAuditorMax)); got != "[]" {
		t.Fatalf("census with no ledger = %s, want []", got)
	}
}

// The re-anchor round is ADOPTED when the continuation really adds a
// `Searched:` line, and then it buys one more audit: the stalled count gets a
// verdict from a deliverable whose search actually moved, and a second stall
// after that ships what stands. This is the round the smoke47 re-run's two
// failures never got — q1093's wrong anchor (a series the auditor named in round
// three) ended the loop there, while four served-and-never-read documents of the
// RIGHT series sat untouched, and q351 stalled on a category that seven served
// documents carried.
func TestRunDeliveryGateReanchorRoundAdoptsANewAnchor(t *testing.T) {
	oneSearch := "## Candidate Matrix\n- Searched: search_bm25_chunks(\"the held candidate\")\n## Reasoning Chain\n- Clue: x\nFinal Answer: **1897**"
	twoSearches := oneSearch + "\n- Searched: grep_chunks(\"the other roster|the year list\")"
	auditor := &fakeAuditorAgent{verdict: "Audit Result: FAIL (5 suspects)"}
	explorer := &flakyExplorer{replies: []string{oneSearch, oneSearch, twoSearches}}
	in := deliveryGateInput{
		explorer:     explorer,
		auditor:      auditor,
		baseMessages: []*schema.Message{schema.UserMessage("q?")},
		final:        "old final",
		auditMaxPass: testAuditMaxPass,
	}

	final, _, _, _ := runDeliveryGate(context.Background(), in)
	if want := gateStallWindow + 1; auditor.runs != want {
		t.Errorf("audits = %d, want %d (the stall window, then the re-anchor round, then the second stall)",
			auditor.runs, want)
	}
	if want := gateStallWindow; explorer.runs != want {
		t.Errorf("repair turns = %d, want %d (every re-anchor attempt was adopted on its first try)", explorer.runs, want)
	}
	if !strings.Contains(final, "the other roster") {
		t.Errorf("final = %q, want the re-anchored deliverable shipped", final)
	}
}

// Narration continuations are discarded in place too: an unchanged deliverable
// must not buy itself another audit.
func TestRunDeliveryGateRetriesNarrationWithoutReAuditing(t *testing.T) {
	auditor := &fakeAuditorAgent{verdict: "Audit Result: FAIL (1 suspects)"}
	explorer := &narratingExplorer{}
	in := deliveryGateInput{
		explorer:     explorer,
		auditor:      auditor,
		baseMessages: []*schema.Message{schema.UserMessage("q?")},
		final:        "old final",
		auditMaxPass: testAuditMaxPass,
	}

	final, _, _, _ := runDeliveryGate(context.Background(), in)
	if auditor.runs != 1 {
		t.Errorf("audits = %d, want 1 - a rejected continuation must not trigger a re-audit", auditor.runs)
	}
	if explorer.runs != gateNoProgressLimit {
		t.Errorf("repair attempts = %d, want gateNoProgressLimit=%d", explorer.runs, gateNoProgressLimit)
	}
	if final != "old final" {
		t.Errorf("final = %q, want narration never clobbering the standing final", final)
	}
	if !strings.Contains(explorer.msgs[len(explorer.msgs)-1].Content, "REJECTION NOTICE") {
		t.Errorf("retry directive does not carry the rejection reason")
	}
}

// narratingExplorer always answers with process talk and no FOS answer line.
type narratingExplorer struct {
	runs int
	msgs []*schema.Message
}

func (f *narratingExplorer) Name(context.Context) string { return "narrating-explorer" }
func (f *narratingExplorer) Description(context.Context) string {
	return "scripted stand-in for the explorer"
}

func (f *narratingExplorer) Run(_ context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	f.runs++
	f.msgs = input.Messages
	go func() {
		gen.Send(assistantMsgEvent("Let me re-render the deliverable now.", nil))
		gen.Close()
	}()
	return iter
}

// A run whose shared wall-clock budget has expired must stop auditing and
// repairing immediately: on a dead context every attempt can only fail, and
// the in-place retry allowance would burn on instant failures (q268).
func TestRunDeliveryGateStopsOnExpiredBudget(t *testing.T) {
	auditor := &fakeAuditorAgent{verdict: "Audit Result: FAIL (1 suspects)"}
	explorer := &flakyExplorer{failures: 99}
	in := deliveryGateInput{
		explorer:     explorer,
		auditor:      auditor,
		baseMessages: []*schema.Message{schema.UserMessage("q?")},
		final:        "old final",
		auditMaxPass: testAuditMaxPass,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // budget gone

	final, audited, _, _ := runDeliveryGate(ctx, in)
	if auditor.runs != 0 {
		t.Errorf("audits = %d, want 0 - an expired budget must skip auditing", auditor.runs)
	}
	if explorer.runs != 0 {
		t.Errorf("repair attempts = %d, want 0 - an expired budget must skip repairs", explorer.runs)
	}
	if final != "old final" || audited != "old final" {
		t.Errorf("final = %q / audited = %q, want the standing final shipped as-is", final, audited)
	}
}

// The synthesis fallback must not inherit the run's expired deadline — it
// exists precisely for the case where that deadline ran out.
func TestFinalizeAnswerOutlivesExpiredRunContext(t *testing.T) {
	dead, cancel := context.WithCancel(context.Background())
	cancel()

	// Must survive the run's expired budget — that is the whole point.
	detached, detachedCancel := context.WithTimeout(context.WithoutCancel(dead), finalizeTimeout)
	defer detachedCancel()
	if err := detached.Err(); err != nil {
		t.Fatalf("finalize context should be alive after the run context expired: %v", err)
	}
	if _, ok := detached.Deadline(); !ok {
		t.Error("finalize context must carry its own bounded budget")
	}
	// Cancellation is deliberately NOT propagated (a deadline expiry and a
	// client hang-up are indistinguishable downstream), so the bounded budget
	// is what prevents unbounded work.
	parent, parentCancel := context.WithCancel(context.Background())
	detached2, cancel2 := context.WithTimeout(context.WithoutCancel(parent), finalizeTimeout)
	defer cancel2()
	parentCancel()
	if err := detached2.Err(); err != nil {
		t.Errorf("finalize context must not inherit cancellation: %v", err)
	}
	// Values must still reach the synthesis call.
	key := struct{}{}
	typed := context.WithValue(context.Background(), key, "v")
	if got := context.WithoutCancel(typed).Value(key); got != "v" {
		t.Errorf("value lost through WithoutCancel: %v", got)
	}
}

// A bare answer line is not a deliverable: adopting it would discard the
// structured (if failing) final and buy a re-audit that only repeats the
// coverage defect. It must be rejected like narration (q1093).
type bareAnswerExplorer struct {
	runs int
	msgs []*schema.Message
}

func (f *bareAnswerExplorer) Name(context.Context) string { return "bare-answer-explorer" }
func (f *bareAnswerExplorer) Description(context.Context) string {
	return "scripted stand-in for the explorer"
}

func (f *bareAnswerExplorer) Run(_ context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	f.runs++
	f.msgs = input.Messages
	go func() {
		gen.Send(assistantMsgEvent("Guessed Answer: **I do not have sufficient evidence.**", nil))
		gen.Close()
	}()
	return iter
}

func TestRunDeliveryGateRejectsBareAnswerLine(t *testing.T) {
	auditor := &fakeAuditorAgent{verdict: "Audit Result: FAIL (1 suspects)"}
	explorer := &bareAnswerExplorer{}
	in := deliveryGateInput{
		explorer:     explorer,
		auditor:      auditor,
		baseMessages: []*schema.Message{schema.UserMessage("q?")},
		final:        "## Candidate Matrix\n- Retained: Bob\nFinal Answer: **Bob**",
		auditMaxPass: testAuditMaxPass,
	}

	final, _, _, _ := runDeliveryGate(context.Background(), in)
	if auditor.runs != 1 {
		t.Errorf("audits = %d, want 1 - a bare answer line advances nothing", auditor.runs)
	}
	if explorer.runs != gateNoProgressLimit {
		t.Errorf("repair attempts = %d, want gateNoProgressLimit=%d", explorer.runs, gateNoProgressLimit)
	}
	if !hasAnswerLine(final) || !hasFOSStructure(final) {
		t.Errorf("final = %q, want the structured standing final preserved", final)
	}
	if !strings.Contains(explorer.msgs[len(explorer.msgs)-1].Content, "REJECTION NOTICE") {
		t.Error("retry directive does not carry the rejection reason")
	}
}

func TestGateRunAuditNilAuditorErrors(t *testing.T) {
	// Must return an error (not panic) when the auditor is unavailable.
	if _, err := gateRunAudit(context.Background(), nil, newTestConversation(),
		"Final Answer: **1897**", nil, nil, nil, NewSearchLedger(), nil); err == nil {
		t.Fatal("gateRunAudit with a nil auditor should error")
	}
}

// TestAdoptableContinuation pins the relaxation that lets the AUDITOR decide about
// a deliverable missing its answer line. Requiring the value here rejected the
// continuation three times and gave up (q490/q784/q872 ended that way, never
// audited); requiring the SECTIONS is what keeps a bare answer line from throwing
// away a structured deliverable (q1093 burned 16 passes on that shape).
func TestAdoptableContinuation(t *testing.T) {
	matrixOnly := "## Candidate Matrix\n- Tested: X (chunk_id: c1)\n"
	if !adoptableContinuation(matrixOnly) {
		t.Error("a matrix without its answer line is auditable — the auditor names the missing answer")
	}
	bare := "Guessed Answer: **X** (assumption: I do not have sufficient evidence)"
	if adoptableContinuation(bare) {
		t.Error("an answer line alone must not replace a structured deliverable (q1093)")
	}
	if adoptableContinuation("I have all the evidence I need. Let me now render the deliverable.") {
		t.Error("narration is not a deliverable")
	}
	if adoptableContinuation("") {
		t.Error("an empty continuation is not a deliverable")
	}
}

// TestLoggableSwitch is retired: every debug log now carries its fields WHOLE
// (no switch, no cap) - a truncated field once hid the exact evidence a
// forensic needed. truncateForLog survives only for model-facing text, where
// the cap is context hygiene.

// TestAnswerLabelShapeZoo pins the whole family of answer-LINE shapes against the one
// thing the gate reads: the label. The old reader was a ladder of per-shape patterns
// and every new shape cost a gate misreading before it got its own — the shapes below
// include ones no run has produced yet, because format drift is the expected case.
// The gate does not care what follows the label; the auditor reads that.
func TestAnswerLabelShapeZoo(t *testing.T) {
	labelled := []struct{ name, final, want string }{
		{"plain", "Final Answer: **X**", "final"},
		{"with note", "Guessed Answer: **X** (assumption: y)", "guessed"},
		{"heading, blank line, value", "## Final Answer\n\n**X**", "final"},
		{"heading, value on the next line", "## Final Answer\n**X**", "final"},
		{"whole line bold", "**Final Answer: X**", "final"},
		{"label bolded separately", "**Final Answer**: **X**", "final"},
		{"space before the colon", "**Final Answer** : **X**", "final"},
		{"blockquote and doubled space", "> **Guessed  Answer**: **X**", "guessed"},
		{"list bullet", "- Final Answer: **X**", "final"},
		{"full-width colon", "Final Answer：**X**", "final"},
		{"un-bolded value", "Final Answer: X", "final"},
		{"CRLF line ending", "Final Answer: **X**\r", "final"},
		{"note citing a document", "Final Answer: **X** (assumption: per (82489.md) it holds)", "final"},
		{"whole-bold with a tie note", "**Guessed Answer: X** (tie: \"Y\" — both fit)", "guessed"},
		{"CJK value", "Final Answer: **关羽**（含\"过五关\"）", "final"},
		{"backticked label", "`Final Answer: **X**`", "final"},
		{"indented, mid-document", "text above\n\n   Final Answer: **X**", "final"},
	}
	for _, tc := range labelled {
		t.Run(tc.name, func(t *testing.T) {
			if got := answerLabel(tc.final); got != tc.want {
				t.Errorf("answerLabel(%q) = %q, want %q", tc.final, got, tc.want)
			}
			if !hasAnswerLine(tc.final) {
				t.Errorf("shape not seen as an answer line: %q", tc.final)
			}
		})
	}
	// What is NOT an answer: text without the phrase at all.
	for _, text := range []string{"", "A prose paragraph without any label.", "the final_answer is computed elsewhere"} {
		if got := answerLabel(text); got != "" {
			t.Errorf("answerLabel(%q) = %q, want empty", text, got)
		}
		if hasAnswerLine(text) {
			t.Errorf("hasAnswerLine(%q) = true, want false", text)
		}
	}
	// A MENTION in prose DOES read as the label, and that is deliberate: telling a
	// mention from a claim needs the sense of the sentence, which is the auditor's
	// reading, not the gate's. What keeps a narration-only run from shipping is not
	// this test but the deliverable's structure (hasFOSStructure) and the answer-less
	// continuation rejection.
	if !hasAnswerLine("I will give a final answer once the evidence is in.") {
		t.Error("the phrase's presence is all the gate reads, mentions included")
	}
	// The bare-heading shape still counts as the label's presence, for the same reason:
	// whether an answer LINE is complete is not something this reader decides.
	if !hasAnswerLine("## Candidate Matrix\n...\n## Final Answer") {
		t.Error("a bare heading contains the phrase")
	}
}

// TestAnswerLineCountLabelOnly pins the synthesis guard's input: it counts LABEL
// lines, which is all the guard needs to tell a single answer line from a lost one or
// from a deliverable re-rendered inside an answer line.
func TestAnswerLineCountLabelOnly(t *testing.T) {
	cases := []struct {
		name, final string
		want        int
	}{
		{"sane synthesis", "## Candidate Matrix\n...\nFinal Answer: **1897**", 1},
		{"two answer lines", "Final Answer: **1897**\nFinal Answer: **1900**", 2},
		{"heading plus value line", "## Final Answer\n**1897**", 1},
		{"lost label", "## Candidate Matrix\n...\nthe answer is 1897", 0},
		{"empty", "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := answerLineCount(tc.final); got != tc.want {
				t.Errorf("answerLineCount(%q) = %d, want %d", tc.final, got, tc.want)
			}
		})
	}
}

// TestCollectGatePrechecksLabelOnly is gone with the prechecks: the gate's one
// mechanical reading, "the deliverable carries no answer label", is one the
// auditor makes itself off final_message, and the payload no longer has a field
// to carry it. TestBuildAuditPayload pins the payload's key set instead.

// TestAuditShipsOnAdvisory pins the pipeline's grading of audit findings: an
// auditor that leaves a completeness finding untagged still gets overruled,
// while a BLOCKING finding keeps the refusal. #36: the auditor graded ONE
// advisory finding across thirteen rounds, and the round it did grade handed
// q875 its first win - so the grading cannot be the auditor's call. Since
// 2026-10-03 the auditor's EXPLICIT `[ADVISORY]` tag is honored as well: a
// class off the pipeline's list ("Searched lines omit N of the M searches the
// ledger records") blocked smoke49, and the run lost the answer line it had
// earned over one finding its own author had graded as form.
func TestAuditShipsOnAdvisory(t *testing.T) {
	advisory := "## Candidate Matrix\n### Sub-question 1: the school\n- Retained: X\n  - audit: suspect: constraint hits not consumed - 95776.md, 35876.md\nAudit Result: FAIL (1 suspects)"
	blocking := "## Candidate Matrix\n### Sub-question 1: the school\n- Retained: X\n  - audit: suspect: elimination ground contradicted by cited chunk - the c5 evidence does not state it\nAudit Result: FAIL (1 suspects)"
	mixed := "## Candidate Matrix\n### Sub-question 1: the school\n- Retained: X\n  - audit: suspect: derived line is missing (block 1)\n- Derived: y\n  - audit: suspect: the supported clues never derive the answer\nAudit Result: FAIL (2 suspects)"
	passed := "## Candidate Matrix\n- Retained: X\n  - audit: pass\nAudit Result: PASS"
	taggedAdvisory := "## Candidate Matrix\n- Searched: grep_chunks(\"x\")\n  - audit: suspect: [ADVISORY] the matrix's Searched lines omit 17 of the 52 searches the ledger records, hiding behavior from every later check\nAudit Result: FAIL (1 suspects)"

	if !auditShipsOnAdvisory(advisory) {
		t.Fatalf("a completeness finding must ship: blocking=%v", auditBlockingOpinions(advisory))
	}
	if !auditShipsOnAdvisory(taggedAdvisory) {
		t.Fatalf("an auditor-tagged [ADVISORY] finding must ship: blocking=%v", auditBlockingOpinions(taggedAdvisory))
	}
	if auditShipsOnAdvisory(blocking) {
		t.Fatalf("a contradicted ground must NOT ship")
	}
	if auditShipsOnAdvisory(mixed) {
		t.Fatalf("one blocking finding among advisory ones must NOT ship: blocking=%v", auditBlockingOpinions(mixed))
	}
	if auditShipsOnAdvisory(passed) {
		t.Fatalf("an already-passing verdict is not an advisory waiver")
	}
	if n := len(auditOpinions(advisory)); n != 1 {
		t.Fatalf("auditOpinions = %d, want 1", n)
	}
	if got := strings.Join(auditBlockingOpinions(mixed), " | "); !strings.Contains(got, "never derive the answer") {
		t.Fatalf("the blocking opinion must survive, got %q", got)
	}
}
