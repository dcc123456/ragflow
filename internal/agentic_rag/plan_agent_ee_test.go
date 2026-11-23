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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The two-stage Run lives or dies with the wiring: the plan stage's
// template must exist, own ONLY the checker (the one thing the stage must
// never do is retrieve), carry the conversational escape marker, and the
// explorer must have lost the checker from ITS toolset - a prompt that says
// "do not call check_decomposition" next to a toolset that offers it is the
// contradiction this stage exists to remove.
func TestQuestionDecompositionStageWiring(t *testing.T) {
	// The wiring test reads the REPO conf (not a fixture): it asserts the real
	// deployment shape, so it loads the same file the server does, through the
	// same AGENTIC_RAG_CONFIG + cache-reset dance the config tests use.
	raw, err := os.ReadFile(filepath.Join("..", "..", "conf", "agentic_rag.yaml"))
	if err != nil {
		t.Skipf("prompt not available here: %v", err)
	}
	path := filepath.Join(t.TempDir(), "agentic_rag.yaml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	t.Setenv("AGENTIC_RAG_CONFIG", path)
	configMu.Lock()
	cachedFile = nil
	configMu.Unlock()
	t.Cleanup(func() {
		configMu.Lock()
		cachedFile = nil
		configMu.Unlock()
	})
	tmpl, err := resolveTemplateFor(plannerTemplateID)
	if err != nil {
		t.Fatalf("the planner template is missing: %v", err)
	}
	if len(tmpl.Tools) != 1 || tmpl.Tools[0] != "check_decomposition" {
		t.Fatalf("the plan stage must own exactly the checker, got %v", tmpl.Tools)
	}
	if !strings.Contains(tmpl.Content, noPlanMarker) {
		t.Fatalf("the planner prompt must carry the conversational escape marker %q", noPlanMarker)
	}
	if !strings.Contains(tmpl.Content, "Example (two blocks") ||
		!strings.Contains(tmpl.Content, "Example (four blocks") {
		t.Fatalf("the worked examples must travel with the writer, not the consumer")
	}
	explorer, err := resolveTemplateFor("planer-explorer-auditor")
	if err != nil {
		t.Fatalf("smart-reasoning missing: %v", err)
	}
	for _, name := range explorer.Tools {
		if name == "check_decomposition" {
			t.Fatalf("the explorer still holds the checker - the two-stage split is not real")
		}
	}
	if !strings.Contains(explorer.Content, "## The question decomposition") ||
		!strings.Contains(explorer.Content, "never re-decompose") {
		t.Fatalf("the explorer prompt must consume the pinned plan, not promise to write one")
	}
}

// extractPlan takes everything from the first block heading down — plus a
// leading `## Resolved question` line, which is part of the plan because every
// downstream consumer reads it as the question. Prose or a code fence before
// the plan is the agent's business, the plan itself must be clean for
// checkDecomposition, and a message with no heading yields nothing.
func TestExtractPlan(t *testing.T) {
	plan := "### Sub-question 1: ?x is the one — slot: name\n- Binds: ?x\n"
	for name, in := range map[string]string{
		"bare":           plan,
		"fenced":         "```\n" + plan + "```\n",
		"prose-prefixed": "Here is the plan.\n" + plan,
	} {
		if got := extractPlan(in); !strings.HasPrefix(got, "### Sub-question 1") || !strings.Contains(got, "- Binds: ?x") {
			t.Fatalf("%s: extractPlan mangled the plan: %q", name, got)
		}
	}
	if got := extractPlan("no plan here, just prose"); got != "" {
		t.Fatalf("a plan-less message must extract to nothing, got %q", got)
	}
	resolved := "## Resolved question: the spouse of the painter from turn one\n\n" + plan
	got := extractPlan(resolved)
	if !strings.HasPrefix(got, "## Resolved question") || !strings.Contains(got, "- Binds: ?x") {
		t.Fatalf("extractPlan must keep the resolved-question header: %q", got)
	}
	if q := extractResolvedQuestion(got); q != "the spouse of the painter from turn one" {
		t.Fatalf("extractResolvedQuestion = %q", q)
	}
	if q := extractResolvedQuestion(plan); q != "" {
		t.Fatalf("a plan without the header must resolve to nothing, got %q", q)
	}
}

// The plan auditor's verdict line is the authoritative signal, and its
// findings may be worded outside the fixed `suspect:` prefix (the auditor has
// flagged drift as `c4 paraphrase drift: ...` and structure as `Block 7
// ...violates ...`). The parse must catch those, keep `pass` bullets out, and
// read the verdict - a FAIL swallowed as "no findings" ships a plan the
// auditor itself rejected.
func TestPlanAuditFindingsParsing(t *testing.T) {
	verdictShaped := "## Findings\n\n" +
		"- INVENTED: c22 concatenates every attribute into one conjunctive constraint\n" +
		"- Block 7 violates the fewer-blocks rule: it adds no new attribute\n" +
		"- c4 paraphrase drift: \"a lot of\" for \"many\"\n" +
		"- pass (all clauses mapped)\n" +
		"- pass (no INVENTED constraints)\n\n" +
		"Audit Result: FAIL (6 suspects)"
	findings, ok := extractPlanAuditFindings(verdictShaped)
	if !ok {
		t.Fatal("a Findings section must be detected")
	}
	if len(findings) != 3 {
		t.Fatalf("want the 3 defect bullets, got %d: %q", len(findings), findings)
	}
	if m := planAuditVerdictRe.FindStringSubmatch(verdictShaped); m == nil || m[1] != "FAIL" {
		t.Fatalf("verdict not read as FAIL: %q", m)
	}
	passShaped := "## Findings\n\n" +
		"- pass (every question clause carried)\n" +
		"- No UNMAPPED clause.\n" +
		"- No INVENTED constraint.\n\n" +
		"Audit Result: PASS"
	findings, _ = extractPlanAuditFindings(passShaped)
	if len(findings) != 0 {
		t.Fatalf("a passing audit must yield no findings, got %q", findings)
	}
	if m := planAuditVerdictRe.FindStringSubmatch(passShaped); m == nil || m[1] != "PASS" {
		t.Fatalf("verdict not read as PASS: %q", m)
	}
	// The old fixed-prefix sweep still lands through the section parse.
	fixed := "Findings\n- suspect: title mirror defect in block 3\n\nAudit Result: FAIL"
	findings, _ = extractPlanAuditFindings(fixed)
	if len(findings) != 1 || !strings.Contains(findings[0], "title mirror") {
		t.Fatalf("fixed-prefix finding lost: %q", findings)
	}
	// A self-refuting UNMAPPED line (the annotation says a constraint covers
	// the clause) is recognized, so a PASS verdict over such lines is not
	// read as defects.
	retracted := "- UNMAPPED clause: \"born in the first decade\" - no block carries it — not present, c1 covers it"
	if !planAuditSelfRefutingRe.MatchString(retracted) {
		t.Fatalf("self-refuting UNMAPPED line not recognized: %q", retracted)
	}
	if planAuditSelfRefutingRe.MatchString("- UNMAPPED clause: \"born in the first decade\" - no block carries it") {
		t.Fatal("a genuine UNMAPPED line must not match the retraction pattern")
	}
	// The entity census lines parse: entity -> variable, and entity -> UNBOUND
	// (the over-merge confession).
	census := "**Entity census**\n" +
		"- `entity: the two individuals -> ?person1, ?person2`\n" +
		"- `entity: the school -> UNBOUND`\n"
	found := planAuditCensusRe.FindAllStringSubmatch(census, -1)
	if len(found) != 2 {
		t.Fatalf("want 2 census lines, got %d: %q", len(found), found)
	}
	if !strings.EqualFold(found[0][2], "?person1") || !strings.EqualFold(found[1][2], "UNBOUND") {
		t.Fatalf("census parse wrong: %q", found)
	}
}
