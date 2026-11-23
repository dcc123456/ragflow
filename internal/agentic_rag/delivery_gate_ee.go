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
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// The gate reads LABELS, not answers. `Final Answer` / `Guessed Answer` are the
// contract's FIXED vocabulary — the one part of a deliverable a pattern can match
// without betting on the model's phrasing — and one pattern serves both, because
// everything around the words varies (heading marks, blockquote, bullets, emphasis,
// backticks, quotes, ASCII and full-width colons, any whitespace) and is therefore
// stripped by class instead of enumerated.
//
// What the value SAYS is answer_auditor's judgement: which text is the value, whether
// it is grounded in the evidence, whether a rival is a real tie, whether a line merely
// cites a document. The auditor reads the deliverable and the cited chunks with a
// model, and its contract already carries every one of those defects. The gate used to
// duplicate four of them as text heuristics, and each was a bet on phrasing that lost:
// the value reader alone needed five patterns as five shapes appeared in real runs
// (value on the next line, whole-line bold, separately bolded label, notes citing
// documents in parentheses, CJK punctuation), and a miss read a well-formed delivery as
// value-less — skipping the checks built on it and buying a repair turn (measured on
// q775 and twice on q283).
// The contract's label is the phrase `Final Answer` / `Guessed Answer`, and the
// gate asks exactly ONE question about a deliverable: does that phrase occur?
// Nothing else about the text is read - not the emphasis markers, not the
// parentheses, not the colons, not a `tie` clause - so the pattern is the phrase
// itself, anchored to nothing. The `\b` after `answer` keeps the plural
// (`Final Answers`, in prose) from reading as a label; everything past the phrase
// belongs to the auditor.
var answerLineLabelRe = regexp.MustCompile(`(?i)(final|guessed)\s+answer\b`)

// answerLabel returns the label the deliverable claims — "final", "guessed" — or ""
// when it carries neither. The first labelled line wins.
func answerLabel(final string) string {
	if m := answerLineLabelRe.FindStringSubmatch(final); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

// hasAnswerLine reports whether the deliverable carries an answer label at all.
func hasAnswerLine(final string) bool { return answerLabel(final) != "" }

// answerLineCount counts lines carrying an answer label. The finalize synthesis must
// produce exactly ONE: zero means the label was lost, two-plus means the model
// re-rendered the whole deliverable inside an answer line.
func answerLineCount(s string) int {
	return len(answerLineLabelRe.FindAllString(s, -1))
}

// fosSectionRe matches the structural headings of the FOS deliverable.
var fosSectionRe = regexp.MustCompile(`(?im)^[^\S\n]*#{1,4}[^\S\n]*(?:Candidate Matrix|Reasoning Chain)\b`)

// hasFOSStructure reports whether the reply carries the deliverable's
// structural headings. Format presence again — it decides whether an
// audit-failed deliverable is worth shipping verbatim (it still carries
// corpus-grounded structure) versus pure narration that only finalizeAnswer
// can turn into an answer (the q55-class A-gate stays intact).
func hasFOSStructure(final string) bool {
	return fosSectionRe.MatchString(final)
}

// demoteFinalAnswerLabel rewrites the deliverable's answer label from
// `Final Answer` to `Guessed Answer` when the delivery gate shipped WITHOUT a
// concluding PASS audit.
//
// `Final Answer` is a claim that every discriminating constraint is
// corpus-verified, so it may only survive an audit that passed. Measured on the
// 525-question browsecomp run, 29 questions shipped a WRONG answer under that
// label while 76% of them had never passed their last audit - the gate stopped
// on a stall, a budget or the clock, and the over-claim rode along. Demotion is
// label-only: the value (what the judge scores) is untouched, only the
// confidence claim is corrected.
//
// EVERY label occurrence is rewritten, not just the first: a deliverable
// carries the label twice (heading + value line), and demoting one left
// `## Final Answer` sitting above `**Guessed Answer: X**` — the corrected value
// under the stronger claim (4/46 rows of the browsecomp retry batch). The
// assumption note is appended once, on the line that actually carries the value.
func demoteFinalAnswerLabel(final, reason string) (string, bool) {
	lines := strings.Split(final, "\n")
	changed := false
	assumptionLine := -1
	for i, line := range lines {
		loc := answerLineLabelRe.FindStringSubmatchIndex(line)
		if loc == nil {
			continue
		}
		// Where the assumption belongs: the line whose label is followed by a
		// value (`Final Answer: **X**`), not the bare `## Final Answer` heading.
		// Read from the ORIGINAL line - the rewrite shifts the offsets.
		// loc[1] is the end of the whole match (after the word `answer`), not the end
		// of the label: what makes a line the VALUE's line is having text after
		// "answer", and the heading `## Final Answer` has none.
		if assumptionLine < 0 && strings.TrimSpace(line[loc[1]:]) != "" {
			assumptionLine = i
		}
		if !strings.EqualFold(line[loc[2]:loc[3]], "final") {
			continue
		}
		lines[i] = line[:loc[2]] + "Guessed" + line[loc[3]:]
		changed = true
	}
	if !changed {
		return final, false
	}
	if assumptionLine < 0 {
		// Heading-only deliverable: the note rides the first label line.
		for i, line := range lines {
			if answerLineLabelRe.MatchString(line) {
				assumptionLine = i
				break
			}
		}
	}
	if assumptionLine >= 0 && !strings.Contains(strings.ToLower(lines[assumptionLine]), "assumption:") {
		lines[assumptionLine] = strings.TrimRight(lines[assumptionLine], " \t\r") + " (assumption: " + reason + ")"
	}
	return strings.Join(lines, "\n"), true
}

// reconcileAnswerLabels makes a deliverable agree with itself on the answer
// label. Demotion normalizes the gate's own rewrite, but the MODEL also mixes
// the shapes on its own (`## Final Answer` heading above a `**Guessed Answer:
// X**` value line), and the label can arrive from a synthesized or recovered
// deliverable that never went through demotion at all.
//
// The conservative label wins: a Guessed value under a Final heading overstates
// verification — precisely the claim lever 1 exists to police — so the heading
// is rewritten to match the value. The value itself is never touched.
func reconcileAnswerLabels(final string) string {
	lines := strings.Split(final, "\n")
	guessed := false
	for _, line := range lines {
		if loc := answerLineLabelRe.FindStringSubmatchIndex(line); loc != nil && strings.EqualFold(line[loc[2]:loc[3]], "guessed") {
			guessed = true
			break
		}
	}
	if !guessed {
		return final // no conservative claim to align to
	}
	changed := false
	for i, line := range lines {
		loc := answerLineLabelRe.FindStringSubmatchIndex(line)
		if loc == nil || !strings.EqualFold(line[loc[2]:loc[3]], "final") {
			continue
		}
		lines[i] = line[:loc[2]] + "Guessed" + line[loc[3]:]
		changed = true
	}
	if !changed {
		return final
	}
	return strings.Join(lines, "\n")
}

// groundingWindowSlack is how many words a sliding match may span beyond the
// value's own length: the slack absorbs the words a source inserts between the
// value's tokens (a title, an epithet, the middle name the VALUE omits).
const groundingWindowSlack = 4

// valueTokensAppearNearby reports whether tokens occur in words in order, inside
// one window, with at most one skipped NON-FINAL token and the final one matched.
//
// It is a forward pass over the window with the state (matched, skipped) rather
// than a greedy walk, because the two admissible moves conflict: at "Jacqueline
// Cantrelle" the token `cantrelle` must be held for the head while `georgette`
// (the value's own middle token) is the one skipped. A greedy matcher that skips
// the value's token on the first mismatch eats the head and rejects the q784 gold.
func valueTokensAppearNearby(words, tokens []string) bool {
	last := len(tokens) - 1
	span := last + groundingWindowSlack
	reach := make([][2]bool, len(tokens)+1)
	for start := 0; start < len(words); start++ {
		if words[start] != tokens[0] {
			continue
		}
		end := start + span + 1
		if end > len(words) {
			end = len(words)
		}
		for i := range reach {
			reach[i] = [2]bool{}
		}
		reach[1][0] = true
		for j := start + 1; j < end; j++ {
			// Skipping a value's own token consumes NO haystack word, so it has to
			// be closed over BEFORE the word at this position is matched: otherwise
			// the match of `cantrelle` is only ever tried while the state still
			// expects `georgette`, and the two moves that q784 needs (omit the
			// middle token, then match the head) can never combine.
			for i := 0; i < last; i++ {
				if reach[i][0] {
					reach[i+1][1] = true
				}
			}
			next := make([][2]bool, len(tokens)+1)
			for i := 0; i <= len(tokens); i++ {
				for skipped := 0; skipped < 2; skipped++ {
					if !reach[i][skipped] {
						continue
					}
					// The corpus may insert words the value does not carry.
					next[i][skipped] = true
					if i < len(tokens) && words[j] == tokens[i] {
						next[i+1][skipped] = true
					}
				}
			}
			reach = next
			if reach[len(tokens)][0] || reach[len(tokens)][1] {
				return true
			}
		}
	}
	return false
}

// locateToolNames are the tools whose calls the Candidate Matrix's `Searched:`
// lines are supposed to record.
var locateToolNames = []string{"grep_chunks", "search_bm25_chunks", "search_semantic_chunks", "search_chunks"}

// searchedLineRe matches a matrix line that records a locate call, capturing the
// tool name it credits.
var searchedLineRe = regexp.MustCompile(`(?im)Searched:\s*(grep_chunks|search_bm25_chunks|search_semantic_chunks|search_chunks)`)

// searchedLineCount counts the matrix lines that credit a locate call. The
// re-anchor round (see auditReanchorDirective) is the ONE place the pipeline
// enforces the anchor demand: the continuation must carry more `Searched:` lines
// than the deliverable it replaces, which is a reading of the RECORD's own
// membership - never of the value - so it stays on the gate's side of the line.
func searchedLineCount(matrix string) int {
	return len(searchedLineRe.FindAllString(matrix, -1))
}

// unrecordedLocateTools returns the locate tools the run actually called but the
// deliverable never credits on a `Searched:` line, sorted.
//
// It exists for DIAGNOSIS, and deliberately not as a gate rejection: measured on
// 703 scored rows, 69% used some locate tool whose call the matrix never names
// (grep_chunks in 362 of them), so rejecting runs on it would reject two out of
// three deliverables over bookkeeping. The cost of that silence is real, though:
// on #71 the ONE call that surfaced the answer's own document was a
// search_semantic_chunks query the matrix omitted, so nothing in the archived row
// showed the pure-vector leg had done the work — the log had to be read to see
// it. The warning makes that visible without changing what the model is asked to
// do; whether to enforce the schema is a separate, evidence-gated decision.
func unrecordedLocateTools(matrix string, toolCallCounts map[string]int) []string {
	recorded := map[string]struct{}{}
	for _, m := range searchedLineRe.FindAllStringSubmatch(matrix, -1) {
		recorded[strings.ToLower(m[1])] = struct{}{}
	}
	out := make([]string, 0, len(locateToolNames))
	for _, name := range locateToolNames {
		if toolCallCounts[name] <= 0 {
			continue
		}
		if _, ok := recorded[name]; ok {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// auditVerdictExcerptMax bounds how much of one audit verdict is archived. The
// excerpt only has to be enough to see WHICH claims were contested and why; the
// full text of ten rounds would bloat every benchmark row.
const auditVerdictExcerptMax = 400

var (
	// auditOpinionRe matches the auditor's per-item opinion sub-lines. The
	// auditor echoes the whole deliverable back and hangs one `- audit: <opinion>`
	// under every audited line, so the deliverable's own text is 95% of the
	// verdict string and none of it is a finding.
	auditOpinionRe = regexp.MustCompile(`(?m)^\s*-\s*audit:\s*(.+?)\s*$`)
	// auditResultLineRe matches the overall verdict line that closes the audit.
	auditResultLineRe = regexp.MustCompile(`(?m)^\s*Audit Result:.*$`)

	// advisoryOpinionRe is the pipeline's OWN grading of an audit opinion,
	// not the auditor's: these opinions are completeness or form - they never
	// make a shipped line false - so they are reported but never block the
	// delivery. #36 proved the auditor cannot be trusted with the grading: it
	// tagged one advisory finding across thirteen audit rounds and left
	// `constraint hits not consumed` - the very class the rule names -
	// untagged, which the untagged-means-BLOCKING default then turned into a
	// refusal (q1005 ended on M=14 across six rounds), while the one round it
	// did grade advisory handed q875 its first win. The pipeline decides - and
	// since 2026-10-03 it also HONORS the auditor's own `[ADVISORY]` tag: a run
	// that had served the gold document, cited it and shipped the correct value
	// still lost its answer line to ONE finding the auditor had explicitly
	// graded advisory, purely because that class ("the matrix's Searched lines
	// omit N of the M searches the ledger records") was not on the pipeline's
	// list below. The list stays - it catches the classes the auditor leaves
	// untagged - and the tag is now read as well, because a finding its own
	// author grades as form must not block the answer.
	advisoryOpinionRe = regexp.MustCompile(`(?i)` +
		`\[ADVISORY\]` +
		`|constraint hits not consumed` +
		`|derived line is missing` +
		`|survivors` +
		`|plan[- ]mirroring` +
		`|clue anchor never queried`)
)

// auditFindings distils one audit verdict into its findings: the non-pass
// opinions and the overall verdict line, ` | `-joined, with the echoed
// deliverable dropped.
//
// The auditor answers by echoing the producer's message back with one
// `- audit: <opinion>` sub-line under every audited line, so the deliverable is
// ~95% of the verdict string and none of it is a finding — a plain truncation of
// the verdict records the DELIVERABLE again, with the finding past the cut. A
// passing item is skipped too: it says nothing a reader would not assume from the
// round's suspect count.
// auditOpinions returns the auditor's non-pass opinions - the findings - as a
// list. Empty opinions and `pass` are skipped, so the list is exactly what the
// auditor objected to.
func auditOpinions(verdict string) []string {
	out := make([]string, 0, 4)
	for _, m := range auditOpinionRe.FindAllStringSubmatch(verdict, -1) {
		opinion := strings.TrimSpace(m[1])
		if opinion == "" || strings.EqualFold(opinion, "pass") {
			continue
		}
		out = append(out, opinion)
	}
	return out
}

// auditBlockingOpinions returns the opinions the PIPELINE grades BLOCKING:
// every finding the advisory pattern does not match. Advisory findings stay
// reported - they are never discarded, they simply do not block.
func auditBlockingOpinions(verdict string) []string {
	out := make([]string, 0, 4)
	for _, opinion := range auditOpinions(verdict) {
		if advisoryOpinionRe.MatchString(opinion) {
			continue
		}
		out = append(out, opinion)
	}
	return out
}

// auditShipsOnAdvisory reports whether a FAIL verdict becomes a shipment
// because every finding the auditor raised is advisory by rule. The auditor
// keeps the last word on BLOCKING defects; the pipeline only refuses to let a
// completeness or form finding cost the run the answer it earned.
func auditShipsOnAdvisory(verdict string) bool {
	if auditPassed(verdict) {
		return false
	}
	opinions := auditOpinions(verdict)
	return len(opinions) > 0 && len(auditBlockingOpinions(verdict)) == 0
}

func auditFindings(verdict string) string {
	findings := make([]string, 0, 4)
	for _, m := range auditOpinionRe.FindAllStringSubmatch(verdict, -1) {
		opinion := strings.TrimSpace(m[1])
		if opinion == "" || strings.EqualFold(opinion, "pass") {
			continue
		}
		findings = append(findings, opinion)
	}
	if overall := strings.TrimSpace(auditResultLineRe.FindString(verdict)); overall != "" {
		findings = append(findings, overall)
	}
	if len(findings) == 0 {
		// Every opinion passed, or the shape was not recognised — say so rather
		// than return an empty string a reader would take for "no issues".
		return "audit produced no parseable opinion"
	}
	return strings.Join(findings, " | ")
}

// recordAuditVerdict archives one audit's findings, in the same order as
// Suspects, so the counts and the auditor's own words stay in step (entry i of
// AuditVerdicts is the verdict that produced entry i of Suspects).
//
// Without this a failure shows a curve (5→3→1→0) but no reason, and the only way
// to explain the run is to reverse-engineer the shipped deliverable — which is how
// the #221 analysis twice landed on the wrong cause.
func recordAuditVerdict(audit *GateAuditRecord, verdict string) {
	if audit == nil || strings.TrimSpace(verdict) == "" {
		return
	}
	excerpt := auditFindings(verdict)
	if runes := []rune(excerpt); len(runes) > auditVerdictExcerptMax {
		excerpt = string(runes[:auditVerdictExcerptMax]) + "…"
	}
	audit.AuditVerdicts = append(audit.AuditVerdicts, excerpt)
}

// auditPayload is the ONE JSON object answer_auditor audits: the producer's
// complete FINAL message, verbatim, plus the gate's own reading of its LABEL.
// The question under audit is pinned into the auditor's system prompt at
// construction time and never re-sent.
//
// The gate's mechanical suspicions used to ride here as `gate_prechecks`. They
// are gone. The one reading the gate still has mechanically is "the deliverable
// carries no answer label", which the auditor reads off final_message itself
// (its contract fails a missing answer) — and every other reading the gate ever
// carried there was a bet on phrasing that lost: a missed pattern read a
// well-formed deliverable as value-less, skipped the checks built on it and
// bought a whole repair turn (q775, twice on q283). What survives is the fact
// the gate establishes without reading content and the auditor cannot
// reconstruct from the text it is handed: the label that text was shipped
// under.
type auditPayload struct {
	FinalMessage string `json:"final_message"`
	// GateAnswerLabel is the gate's label reading ("final"/"guessed"), sent as evidence
	// for the label-consistency rules the auditor owns: a declared tie must ship as
	// `Guessed Answer`, and a `Final Answer` that contradicts its own evidence is a
	// defect. The gate does not read the value, so it states the one thing it knows.
	GateAnswerLabel string `json:"gate_answer_label,omitempty"`
	// ServedUnreadDocs lists the documents the retrieval tools served but the
	// chain never deep-read - the evidence a failing chain never looked at.
	// When the deliverable declares an entity-unresolved failure while this
	// list is non-empty, the failure is premature: the audit must suspect it
	// and demand the re-sweep.
	ServedUnreadDocs []string `json:"served_unread_docs,omitempty"`
	// SearchActions is the size of the run's search ledger (Input.Searches) at
	// audit time - the nudge that tells the auditor a read_search_ledger call
	// has ground truth to check coverage judgements against. The count rides
	// the payload because the auditor cannot know from the deliverable alone
	// that its Searched lines underreport; the full entries stay behind the
	// tool so the payload never grows with them.
	SearchActions int `json:"search_actions,omitempty"`
}

// unreadCitations returns the chunk ids the deliverable cites that the run never
// received, sorted - the difference between what the matrix claims to have read
// and what the tools actually put in front of the model.
//
// It is the ONE citation question the gate answers without reading content, which
// is why it can be enforced mechanically where the removed value prechecks could
// not: a cited id is either in the run's chunk-read ledger or it is not - no
// phrasing to bet on, no well-formed deliverable to misread (the failure mode that
// retired the gate's text heuristics, see answerLineLabelRe). The ledger holds
// every chunk the four corpus retrieval tools rendered, deep reads and triage
// snippets alike, so an id outside it was never served: it is a fabricated or
// mis-transcribed identifier, and the snippet beside it cannot be verbatim from it.
//
// A nil ledger, or one that recorded nothing at all, yields nil: a run whose reads
// were never accounted for cannot testify about what it read, and refusing
// deliverables on the absence of a record would punish the archive, not the answer.
func unreadCitations(final string, reads *chunkReadLedger) []string {
	cited := ExtractCitedChunkIDs(final)
	if len(cited) == 0 {
		return nil
	}
	deep, shallow := reads.ChunkIDs()
	if len(deep)+len(shallow) == 0 {
		return nil
	}
	served := make(map[string]struct{}, len(deep)+len(shallow))
	for _, id := range deep {
		served[id] = struct{}{}
	}
	for _, id := range shallow {
		served[id] = struct{}{}
	}
	var out []string
	for _, id := range cited {
		if _, ok := served[id]; !ok {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// unreadCitationDirective is the repair directive for a deliverable that cites
// chunks the run never read: read them, or drop the lines. It says BOTH, because
// from the gate's side a mis-typed id and a remembered one are the same defect -
// the producer is the one who can tell which fix applies.
func unreadCitationDirective(unread []string) string {
	return "Your deliverable CITES chunk ids this run never received: " + strings.Join(unread, ", ") + ". " +
		"A cited chunk must be one the tools actually put in front of you (a list_chunks deep read, or a locate " +
		"result's snippet window); an id you never received cannot support a line, and the snippet beside it " +
		"cannot be verbatim from it. Fix each one: either read that chunk now - `list_chunks` with its doc_id " +
		"and `anchor_chunk_ids=[<the id>]` - and correct the line's doc/doc_id/chunk_id/snippet against what " +
		"comes back, or replace that line with evidence you did read. Then re-render the COMPLETE deliverable " +
		"(`## Candidate Matrix`, `## Reasoning Chain`, and the Final/Guessed Answer line) in this same turn."
}

// buildAuditPayload serializes the deliverable into answer_auditor's JSON - the
// shape the audit contract advertises. It is a verbatim passthrough, not an
// extraction: the auditor reads the FINAL message's own md structure (##
// Candidate Matrix, ## Reasoning Chain, the Final/Guessed Answer line) and echoes
// it back with audit opinions, so the gate must not reshape or truncate it.
func buildAuditPayload(final string, searches *searchLedger, unreadDocs []string) string {
	b, err := json.Marshal(auditPayload{FinalMessage: final, GateAnswerLabel: answerLabel(final), SearchActions: searches.Count(), ServedUnreadDocs: unreadDocs})
	if err != nil {
		// json.Marshal of plain strings cannot fail.
		return ""
	}
	return string(b)
}
