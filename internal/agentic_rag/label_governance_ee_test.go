//go:build ee

package agentic_rag

import (
	"ragflow/internal/retrievalaccount"
	"strings"
	"testing"
)

func TestCountChunkElementsStillWorks(t *testing.T) {
	if n := retrievalaccount.CountChunkElements("<chunks><chunk a=\"1\">x</chunk></chunks>"); n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
}

// TestUnrecordedLocateTools pins the diagnostic: a locate tool the run called
// must be credited on a `Searched:` line, and the check exists because an
// omitted call is invisible in the archived row (on #71 the decisive
// pure-vector call was omitted). Tools never called are not reported.
func TestUnrecordedLocateTools(t *testing.T) {
	matrix := "### Sub-question 1\n" +
		"- `Searched: search_bm25_chunks(\"Ballast Bank Wexford\") -> top: doc 15985.md`\n" +
		"- `Tested: Ballast Bank - all clues supported`\n" +
		"Final Answer: **Ballast Bank**"
	counts := map[string]int{
		"grep_chunks":            8,
		"search_bm25_chunks":     11,
		"search_semantic_chunks": 1,
		"list_chunks":            17,
	}
	got := unrecordedLocateTools(matrix, counts)
	if len(got) != 2 || got[0] != "grep_chunks" || got[1] != "search_semantic_chunks" {
		t.Fatalf("unrecordedLocateTools = %v, want [grep_chunks search_semantic_chunks]", got)
	}
	// A deep read is not a locate call, and a tool that was never called cannot
	// be missing.
	if got := unrecordedLocateTools(matrix, map[string]int{"list_chunks": 3}); len(got) != 0 {
		t.Errorf("unrecordedLocateTools with no locate calls = %v, want none", got)
	}
	if got := unrecordedLocateTools("", map[string]int{"search_chunks": 2}); len(got) != 1 || got[0] != "search_chunks" {
		t.Errorf("a deliverable with no Searched lines = %v, want [search_chunks]", got)
	}
}

// TestRecordAuditVerdict pins the audit record: one FINDING per round, in step
// with Suspects, with the echoed deliverable dropped (it is 95% of the verdict
// string and none of it is a finding), trimmed, and safe on every path —
// including a run whose gate never produced a verdict (nil record).
func TestRecordAuditVerdict(t *testing.T) {
	// The shape the auditor actually returns: the producer's message echoed back
	// with one `- audit: <opinion>` sub-line per audited line, then the overall
	// verdict. Verbatim from the answer_auditor contract in conf/agentic_rag.yaml.
	verdict := strings.Join([]string{
		"## Candidate Matrix",
		"### Sub-question 1: who signed the treaty",
		`- Retained: Bob (doc: A Title, doc_id: d1, chunk_id: c1, snippet: "...")`,
		`  - audit: field integrity: field doc is incorrect (list_chunks doc_name says "66090.md", the line claims "A Title")`,
		"## Reasoning Chain",
		"- Clue: Bob signed in 1897 (doc: 66090.md, doc_id: d1, chunk_id: c1, snippet: \"...\")",
		"  - audit: pass",
		"Final Answer: **1897**",
		"  - audit: suspect: the supported clues never derive 1897",
		"Audit Result: FAIL (2 suspects)",
	}, "\n")

	rec := &GateAuditRecord{Suspects: []int{2}}
	recordAuditVerdict(rec, verdict)
	if len(rec.AuditVerdicts) != 1 {
		t.Fatalf("verdicts = %d, want 1", len(rec.AuditVerdicts))
	}
	got := rec.AuditVerdicts[0]
	for _, want := range []string{"field integrity: field doc is incorrect", "suspect: the supported clues never derive 1897", "Audit Result: FAIL (2 suspects)"} {
		if !strings.Contains(got, want) {
			t.Errorf("excerpt must keep %q, got %q", want, got)
		}
	}
	if strings.Contains(got, "Candidate Matrix") || strings.Contains(got, "doc_id: d1") {
		t.Errorf("the echoed deliverable must not be archived, got %q", got)
	}
	if strings.Contains(got, "audit: pass") {
		t.Errorf("a passing item is not a finding, got %q", got)
	}

	// Entry i of AuditVerdicts must belong to entry i of Suspects — including a
	// PASS round, whose findings are empty by construction.
	rec.Suspects = append(rec.Suspects, 0)
	recordAuditVerdict(rec, "## Candidate Matrix\n- Retained: Bob\n  - audit: pass\nAudit Result: PASS")
	if len(rec.AuditVerdicts) != len(rec.Suspects) {
		t.Fatalf("verdicts %d and suspects %d must stay in step", len(rec.AuditVerdicts), len(rec.Suspects))
	}
	if last := rec.AuditVerdicts[len(rec.AuditVerdicts)-1]; !strings.Contains(last, "Audit Result: PASS") {
		t.Errorf("a PASS round must still record its verdict line, got %q", last)
	}

	// An empty verdict records nothing, and a nil record is not a panic.
	before := len(rec.AuditVerdicts)
	recordAuditVerdict(rec, "   ")
	recordAuditVerdict(nil, "Audit Result: PASS")
	if len(rec.AuditVerdicts) != before {
		t.Error("a blank verdict must not add an entry")
	}

	// Truncation bound, on rune boundaries.
	recordAuditVerdict(rec, "  - audit: suspect: "+strings.Repeat("é", auditVerdictExcerptMax+50))
	last := rec.AuditVerdicts[len(rec.AuditVerdicts)-1]
	if n := len([]rune(last)); n != auditVerdictExcerptMax+1 {
		t.Errorf("excerpt is %d runes, want %d (bound plus the ellipsis)", n, auditVerdictExcerptMax+1)
	}
}

// TestAuditFindings pins the distillation BOTH consumers use (the log line and
// the archived record): the echoed deliverable never reaches either, a PASS round
// reduces to its verdict line, and an unrecognised shape says so rather than
// returning "" — which a reader would take for "no issues".
func TestAuditFindings(t *testing.T) {
	got := auditFindings("## Candidate Matrix\n- Retained: Bob\n  - audit: pass\nAudit Result: PASS")
	if strings.Contains(got, "Bob") || strings.Contains(got, "Candidate Matrix") {
		t.Errorf("the echoed deliverable must be dropped, got %q", got)
	}
	if got != "Audit Result: PASS" {
		t.Errorf("a PASS round distils to its verdict line, got %q", got)
	}
	if got := auditFindings("## Candidate Matrix\n- Retained: Bob\n"); got != "audit produced no parseable opinion" {
		t.Errorf("an unrecognised shape must say so, got %q", got)
	}
}

// TestAuditRepairDirective pins the instruction the gate hands the producer after
// an audit FAIL. Two rules ride in it, and both cost a whole pass when they are
// missing:
//
//   - Lever 3 (unchanged): a repair after TWO failures must open with new
//     retrieval on a different anchor — re-rendering the same matrix leaves the
//     suspect count flat or climbing.
//   - (c) Repair the RECORD, not the conclusion: q221 shipped "Opium: A Portrait
//     of the Heavenly Demon" under `Final Answer` right after the auditor pushed
//     it off a weakness-based elimination of the gold, i.e. a PASS bought by
//     swapping the answer. The directive must forbid that swap by name, and send
//     a grounded-but-unrefutable rival to a declared TIE instead.
func TestAuditRepairDirective(t *testing.T) {
	early := auditRepairDirective(2, "Audit Result: FAIL (2 suspects)\n  - audit: suspect: competing slot-filler never tested", []int{2})
	if strings.Contains(early, "ANCHOR CHANGE REQUIRED") {
		t.Error("the anchor demand must not fire on the first failure — it would forbid a repair before one was tried")
	}
	for _, want := range []string{
		"REPAIR THE RECORD, NOT THE CONCLUSION",
		// (d) A rejected argument restated is not a repair: v2 on q221 spent
		// three passes re-arguing one naming-level inference and flatlined.
		"FIX THE EVIDENCE, NOT THE WORDING",
		"a `(tie: ...)` clause per rival on the answer line",
		"eliminated for ABSENCE",
		"(2 suspect item(s))",
		// The verdict rides along verbatim: the producer repairs the lines the
		// auditor named, not a paraphrase of them.
		"competing slot-filler never tested",
	} {
		if !strings.Contains(early, want) {
			t.Errorf("repair directive must carry %q", want)
		}
	}

	stalled := auditRepairDirective(3, "Audit Result: FAIL (3 suspects)", []int{5, 4, 6})
	if !strings.Contains(stalled, "ANCHOR CHANGE REQUIRED") {
		t.Error("a repair after two failures must demand new evidence on a different anchor")
	}
	if !strings.Contains(stalled, "5 -> 4 -> 6") {
		t.Errorf("the directive must show the suspect trend it is reacting to, got:\n%s", stalled)
	}
}
