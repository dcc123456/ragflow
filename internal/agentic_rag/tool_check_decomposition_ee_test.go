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
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// lawfulPlan is the shape the checks must accept: a root naming the question's
// own anchor, a chain edge with its pointer, and a partition of numbers.
const lawfulPlan = `## Resolved question: A school with 3 founders was named after the birthplace of one of them, and two individuals from different industries attended it and share the same first and last name. What is the shared name?

### Sub-question 1: ?school is the school with 3 founders whose name is the birthplace of one of them — slot: name — kind: institution
- Op: lookup
- Binds: ?school
- From: none
- Constraints: c1 = ?school has 3 founders; c2 = ?school's name is the birthplace of one of those 3 founders

### Sub-question 2: the shared first and last name of two individuals from different industries at ?school — slot: name — kind: person_name
- Op: follow
- Binds: ?answer
- From: ?school (block 1)
- Constraints: c3 = ?answer is the shared first and last name of the two individuals who attended ?school; c4 = ?answer is shared by two individuals of different industries
`

func TestCheckDecompositionAcceptsLawfulPlan(t *testing.T) {
	if got := checkDecomposition(lawfulPlan); len(got) != 0 {
		t.Fatalf("lawful plan reported findings: %v", got)
	}
}

// A plan written with markdown emphasis must read exactly like the same plan
// without it: one archived run wrote `- **Op:**` and every header-derived metric
// it had went to zero.
func TestCheckDecompositionToleratesBoldMarkup(t *testing.T) {
	bold := strings.NewReplacer(
		"- Op:", "- **Op:**",
		"- Binds:", "- **Binds:**",
		"- From:", "- **From:**",
		"- Constraints:", "- **Constraints:**",
	).Replace(lawfulPlan)
	if got := checkDecomposition(bold); len(got) != 0 {
		t.Fatalf("bold plan reported findings: %v", got)
	}
}

func TestCheckDecompositionFindings(t *testing.T) {
	cases := []struct {
		name    string
		plan    string
		want    string // a substring of the expected finding
		notWant string
	}{
		{
			name: "answer bound twice",
			plan: strings.Replace(lawfulPlan, "- Binds: ?school", "- Binds: ?answer", 1),
			want: "schema integrity: variable is bound twice (?answer",
		},
		{
			name: "title names no anchor",
			plan: strings.Replace(lawfulPlan, "?school is the school with 3 founders whose name is the birthplace of one of them", "the school of the person", 1),
			want: "suspect: sub-question is not self-contained - it names no anchor (block 1)",
		},
		{
			name: "block number crosses a boundary",
			plan: strings.Replace(lawfulPlan, "c4 = ?answer is shared by two individuals of different industries", "c4 = the school of block 1", 1),
			want: "schema integrity: block number or prose is used across a block boundary",
		},
		{
			name: "root whose variable nobody consumes",
			plan: strings.Replace(lawfulPlan, "- From: ?school (block 1)", "- From: none", 1),
			want: "schema integrity: root block's variable is never consumed (block 1)",
		},
		{
			name:    "lawful plan has no orphan finding",
			plan:    lawfulPlan,
			notWant: "never consumed",
		},
		{
			name: "constraint number restarts",
			plan: strings.Replace(lawfulPlan, "c3 = ?answer is the shared first and last name of the two individuals who attended ?school", "c1 = ?answer is the shared first and last name of the two individuals", 1),
			want: "schema integrity: constraint number is reused for a different claim (c1)",
		},
		{
			name: "pointer mismatch",
			plan: strings.Replace(lawfulPlan, "- From: ?school (block 1)", "- From: ?school (block 2)", 1),
			want: "schema integrity: dependency pointer does not match the binding block (?school",
		},
		{
			name: "from entry without pointer",
			plan: strings.Replace(lawfulPlan, "- From: ?school (block 1)", "- From: ?school", 1),
			want: "schema integrity: from entry has no block pointer (?school",
		},
		{
			name: "follow without a bound input",
			plan: strings.Replace(lawfulPlan, "- From: ?school (block 1)", "- From: none", 1),
			want: "schema integrity: follow op has no bound input (block 2)",
		},
		{
			name: "constraints line missing",
			plan: strings.Replace(lawfulPlan, "- Constraints: c3 = ?answer is the shared first and last name of the two individuals who attended ?school; c4 = ?answer is shared by two individuals of different industries\n", "", 1),
			want: "schema integrity: constraints line is missing (block 2)",
		},
		{
			name: "constraint references an undeclared variable",
			plan: strings.Replace(lawfulPlan, "c3 = ?answer is the shared first and last name of the two individuals who attended ?school", "c3 = ?answer is the shared first and last name of the two individuals who attended ?other", 1),
			want: "schema integrity: constraint references a variable this block neither binds nor consumes (?other",
		},
		{
			name: "from entry unused by the constraints",
			plan: strings.Replace(lawfulPlan, "c3 = ?answer is the shared first and last name of the two individuals who attended ?school", "c3 = ?answer is the shared first and last name of the two individuals", 1),
			want: "schema integrity: from entry is unused by this block's constraints (?school",
		},
		{
			name: "title references a variable its constraints never use",
			plan: strings.Replace(lawfulPlan, "at ?school — slot", "at ?school and ?rival — slot", 1),
			want: "schema integrity: title references a variable its constraints never use (?rival, block 2)",
		},
		{
			name: "constraints reference a variable the title does not carry",
			plan: strings.Replace(lawfulPlan, "c4 = ?answer is shared by two individuals of different industries", "c4 = ?answer is shared by two individuals of different industries; c5 = ?answer once competed against ?rival", 1),
			want: "schema integrity: constraints reference a variable the title does not carry (?rival, block 2)",
		},
		{
			name: "a block that consumes ?answer cannot reach it",
			plan: lawfulPlan + "\n\n### Sub-question 3: ?audit is the record citing ?answer — slot: name — kind: text\n- Op: lookup\n- Binds: ?audit\n- From: ?answer (block 2)\n- Constraints: c5 = ?audit cites ?answer\n",
			want: "schema integrity: block does not reach the answer block (block 3)",
		},
		{
			name: "an orphan block cannot reach the answer",
			plan: lawfulPlan + "\n\n### Sub-question 3: ?weather is the weather on the day this record was signed — slot: name — kind: text\n- Op: lookup\n- Binds: ?weather\n- From: none\n- Constraints: c5 = ?weather is the weather on the signing day\n",
			want: "schema integrity: block does not reach the answer block (block 3)",
		},
		{
			name:    "lawful plan uses every from entry",
			plan:    lawfulPlan,
			notWant: "unused by this block's constraints",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := checkDecomposition(tc.plan)
			joined := strings.Join(got, "\n")
			if tc.want != "" && !strings.Contains(joined, tc.want) {
				t.Fatalf("want a finding containing %q, got %v", tc.want, got)
			}
			if tc.notWant != "" && strings.Contains(joined, tc.notWant) {
				t.Fatalf("did not want %q, got %v", tc.notWant, got)
			}
		})
	}
}

// exampleQuestion is the decomposition example the section ships. It is not one
// of the benchmark's 830 questions (checked), and the entities the walkthrough
// resolves are written as placeholders for the same reason every other example
// is: a measured delivery once shipped an example's answer as its own.
const exampleQuestion = "Which player of a team in the 2004-05 season, who was born in 90s? This team is founded in 1966 and is an East German football team."

// The positive example, WITH the question it decomposes (exampleQuestion above).
// The team is resolved from its own constraints first, the second block keeps
// ?team as a variable, and ?answer is bound once, by the block that fills the
// asked slot. This is the draft the tool must pass before a single query runs.
const examplePlanGood = `## Resolved question: Which East German football team was founded in 1966, and where did it play its home games in its first season?

### Sub-question 1: ?team is the East German football team founded in 1966 — slot: name — kind: organization
- Op: lookup
- Binds: ?team
- From: none
- Constraints: c1 = ?team was founded in 1966; c2 = ?team is an East German football team

### Sub-question 2: ?answer is the set of players ?team fielded in the 2004-05 season who were born in the 1990s — slot: set — kind: person
- Op: follow
- Binds: ?answer
- From: ?team (block 1)
- Constraints: c3 = ?answer is the set of players ?team fielded in the 2004-05 season; c4 = ?answer are all born in the 1990s
`

// The shipped DAG example, WITH the question it decomposes - the question is what
// the titles' anchors are checked against, so the two travel together:
// "The winner and the runner-up of a European championship whose final was
// decided on penalties both came from the same city; which city was it? The
// championship was held in 1996."
const exampleQuestionDAG = "The winner and the runner-up of a European championship whose final was decided on penalties both came from the same city; which city was it? The championship was held in 1996."

// ?tournament forks into the winner and the runner-up, which merge into the city.
const examplePlanDAG = `## Resolved question: Which European championship was held in 1996, decided on penalties, and who won it?

### Sub-question 1: ?tournament is the European championship held in 1996 whose final was decided on penalties — slot: name — kind: organization
- Op: lookup
- Binds: ?tournament
- From: none
- Constraints: c1 = ?tournament was held in 1996; c2 = ?tournament is a European championship; c3 = ?tournament had a final decided on penalties

### Sub-question 2: ?winner is the winner of ?tournament — slot: name — kind: person
- Op: follow
- Binds: ?winner
- From: ?tournament (block 1)
- Constraints: c4 = ?winner won ?tournament

### Sub-question 3: ?runner_up is the runner-up of ?tournament — slot: name — kind: person
- Op: follow
- Binds: ?runner_up
- From: ?tournament (block 1)
- Constraints: c5 = ?runner_up lost the final of ?tournament

### Sub-question 4: ?answer is the city both ?winner and ?runner_up were born in — slot: name — kind: place
- Op: filter
- Binds: ?answer
- From: ?winner (block 2), ?runner_up (block 3)
- Constraints: c6 = ?answer is the birthplace of ?winner; c7 = ?answer is the birthplace of ?runner_up
`

// The contrastive example: the same question, decomposed the three ways the
// section names. "Dynamo" stands for the value block 1 returned.
const examplePlanBad = `### Sub-question 1: the East German football team founded in 1966 — slot: name — kind: organization
- Op: lookup
- Binds: ?answer
- From: none
- Constraints: c1 = ?answer was founded in 1966; c2 = ?answer is an East German football team

### Sub-question 2: the player of Dynamo in the 2004-05 season born in the 1990s — slot: set — kind: person
- Op: follow
- Binds: ?answer
- From: ?answer (block 1)
- Constraints: c3 = ?answer played for Dynamo in the 2004-05 season; c4 = ?answer was born in the 1990s
`

func TestCheckDecompositionAcceptsShippedExample(t *testing.T) {
	if got := checkDecomposition(examplePlanGood); len(got) != 0 {
		t.Fatalf("the shipped positive example reported findings: %v", got)
	}
}

func TestCheckDecompositionAcceptsShippedDAGExample(t *testing.T) {
	if got := checkDecomposition(examplePlanDAG); len(got) != 0 {
		t.Fatalf("the shipped DAG example reported findings: %v", got)
	}
}

// TestPromptShipsExamplesThatPass guards the pair at its source: the examples in
// conf/agentic_rag.yaml must each carry the question they decompose - the
// reader needs it to see what the variables stand for - and the plan must be
// one this checker accepts. Editing the prompt can therefore never ship an
// example the pipeline would reject.
func TestPromptShipsExamplesThatPass(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "conf", "agentic_rag.yaml"))
	if err != nil {
		t.Skipf("prompt not available here: %v", err)
	}
	planPartRe := regexp.MustCompile("`([^`]*)`")
	questionRe := regexp.MustCompile(`question: "([^"]*)"`)
	for _, tag := range []string{"Example (two blocks", "Example (four blocks"} {
		line := ""
		for _, l := range strings.Split(string(raw), "\n") {
			if strings.Contains(l, tag) {
				line = l
				break
			}
		}
		if line == "" {
			t.Fatalf("the prompt no longer ships %q", tag)
		}
		qm := questionRe.FindStringSubmatch(line)
		if qm == nil {
			t.Fatalf("%s carries no question - the titles' anchors cannot be checked without it", tag)
		}
		var parts []string
		for _, m := range planPartRe.FindAllStringSubmatch(line, -1) {
			p := strings.TrimSpace(m[1])
			if strings.HasPrefix(p, "## Resolved question") || strings.HasPrefix(p, "### Sub-question") || strings.HasPrefix(p, "- ") {
				parts = append(parts, p)
			}
		}
		if len(parts) < 5 {
			t.Fatalf("%s: extracted only %d plan lines from the prompt", tag, len(parts))
		}
		if got := checkDecomposition(strings.Join(parts, "\n")); len(got) != 0 {
			t.Fatalf("%s ships a plan the checker rejects: %v", tag, got)
		}
	}
}

// A constraint that never names its own block's variable leaves the reader to
// infer what it attaches to - the defect the sentence form removes.
func TestCheckDecompositionFlagsConstraintWithoutItsVariable(t *testing.T) {
	plan := strings.Replace(examplePlanGood, "c1 = ?team was founded in 1966", "c1 = founded in 1966", 1)
	got := strings.Join(checkDecomposition(plan), "\n")
	if !strings.Contains(got, "schema integrity: constraint does not name the variable this block binds (c1, block 1)") {
		t.Fatalf("missed the unnamed-variable finding; got:\n%s", got)
	}
}

// Equivalence between a sub-question and its constraint set is a judgment
// about wording - the writer's and the auditor's, not the checker's: a plan
// whose constraint shares no terms with its title but is structurally perfect
// gets no finding and no hint.
func TestCheckDecompositionIgnoresWording(t *testing.T) {
	plan := strings.Replace(lawfulPlan,
		"c3 = ?answer is the shared first and last name of the two individuals who attended ?school",
		"c3 = ?answer is the outcome of ?school's rule that the question words differently", 1)
	if got := checkDecomposition(plan); len(got) != 0 {
		t.Fatalf("wording is not the checker's business, yet it reported: %v", got)
	}
}

// A block carrying five or more constraints gets a PLANNING NOTE on the tool's
// output - it never turns an OK into a rejection, because how deep to split is
// the planner's call, not the checker's.
func TestCheckDecompositionNotesHeavyBlocks(t *testing.T) {
	tool := NewCheckDecompositionTool()
	args, _ := json.Marshal(checkDecompositionArgs{Plan: lawfulPlan})
	out, err := tool.invokableRun(context.Background(), string(args))
	if err != nil {
		t.Fatalf("invokableRun: %v", err)
	}
	if strings.Contains(out, "planning hint") {
		t.Fatalf("a two-block plan should carry no size hint, got:\n%s", out)
	}
	parts := []string{
		"c1 = ?school has 3 founders",
		"c2 = ?school's name is the birthplace of one of those 3 founders",
	}
	for i := 3; i <= decompositionMaxConstraintsPerBlock; i++ {
		parts = append(parts, fmt.Sprintf("c%d = ?school holds its %d-th additional property", i, i))
	}
	heavy := strings.Replace(lawfulPlan,
		"- Constraints: c1 = ?school has 3 founders; c2 = ?school's name is the birthplace of one of those 3 founders",
		"- Constraints: "+strings.Join(parts, "; "), 1)
	args, _ = json.Marshal(checkDecompositionArgs{Plan: heavy})
	out, err = tool.invokableRun(context.Background(), string(args))
	if err != nil {
		t.Fatalf("invokableRun: %v", err)
	}
	if !strings.Contains(out, fmt.Sprintf("block 1 carries %d constraints", decompositionMaxConstraintsPerBlock)) {
		t.Fatalf("missed the size hint; got:\n%s", out)
	}
}

// Every remaining check is script-agnostic - variables, edges and constraint
// numbers are ASCII tokens over any prose - so a CJK plan is judged by the same
// rules (a constraint that never names its own variable is still a finding,
// whatever the script).
func TestCheckDecompositionCJKPlan(t *testing.T) {
	plan := `## Resolved question: 那所由三位创始人创立并以其中一位创始人出生地命名的学校是什么？

### Sub-question 1: ?school 是那所由三位创始人创立并以其中一位创始人出生地命名的学校 — slot: name — kind: institution
- Op: lookup
- Binds: ?school
- From: none
- Constraints: c1 = ?school 拥有三位创始人; c2 = ?school 以其中一位创始人的出生地命名

### Sub-question 2: ?answer 是这两个人共有的名字，其中一人曾就读于 ?school — slot: name — kind: person_name
- Op: follow
- Binds: ?answer
- From: ?school (block 1)
- Constraints: c3 = ?answer 是这两个人共有的名字，其中一人曾就读于 ?school
`
	if got := checkDecomposition(plan); len(got) != 0 {
		t.Fatalf("a CJK plan must not be flagged: %v", got)
	}
	bad := strings.Replace(plan, "c3 = ?answer 是这两个人共有的名字，其中一人曾就读于 ?school", "c3 = 这两个人共有的名字，其中一人曾就读于该学校", 1)
	joined := strings.Join(checkDecomposition(bad), "\n")
	if !strings.Contains(joined, "constraint does not name the variable this block binds (c3, block 2)") {
		t.Fatalf("missed the CJK unnamed-variable finding; got %v", joined)
	}
}

// The size caps are FINDINGS, unlike the five-or-more hints: a plan of more
// than ten blocks, or a block with more than ten constraints, is the question
// restated rather than decomposed.
func TestCheckDecompositionPlanBlockCap(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= decompositionMaxBlocks+1; i++ {
		binds := fmt.Sprintf("?v%d", i)
		cons := fmt.Sprintf("c%d = %s is the %d-th clue of the question that ?v1 anchors", i, binds, i)
		prev := ""
		if i > 1 {
			prev = "- From: ?v1 (block 1)\n"
		}
		if i == 2 {
			binds = "?answer"
			cons = fmt.Sprintf("c%d = ?answer is the answer that ?v1 anchors", i)
		}
		fmt.Fprintf(&b, "### Sub-question %d: %s is the %d-th clue of the question — slot: name — kind: person\n- Op: lookup\n- Binds: %s\n%s- Constraints: %s\n\n", i, binds, i, binds, prev, cons)
	}
	got := strings.Join(checkDecomposition(b.String()), "\n")
	if !strings.Contains(got, fmt.Sprintf("schema integrity: the plan carries %d blocks - more than the cap of %d", decompositionMaxBlocks+1, decompositionMaxBlocks)) {
		t.Fatalf("missed the block-cap finding; got:\n%s", got)
	}
}

func TestCheckDecompositionConstraintCap(t *testing.T) {
	cons := make([]string, 0, decompositionMaxConstraintsPerBlock+1)
	for i := 1; i <= decompositionMaxConstraintsPerBlock+1; i++ {
		cons = append(cons, fmt.Sprintf("c%d = ?answer is the %d-th clause of the question", i, i))
	}
	plan := "### Sub-question 1: ?answer is the one the question asks for — slot: name — kind: person\n- Op: lookup\n- Binds: ?answer\n- From: none\n- Constraints: " + strings.Join(cons, "; ") + "\n"
	got := strings.Join(checkDecomposition(plan), "\n")
	if !strings.Contains(got, fmt.Sprintf("schema integrity: block 1 carries %d constraints - more than the cap of %d", decompositionMaxConstraintsPerBlock+1, decompositionMaxConstraintsPerBlock)) {
		t.Fatalf("missed the constraint-cap finding; got:\n%s", got)
	}
}

// The plan-level mirror: ten or more blocks gets the split-too-fine hint.
func TestCheckDecompositionNotesFinePlans(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= decompositionMaxBlocks; i++ {
		prev := ""
		if i > 1 {
			prev = fmt.Sprintf("- From: ?v%d (block %d)\n", i-1, i-1)
		}
		fmt.Fprintf(&b, "### Sub-question %d: ?v%d is the %d-th clue of the question - skip - skip — slot: name — kind: person\n- Op: lookup\n- Binds: ?v%d\n%s- Constraints: c%d = ?v%d is the %d-th clue of the question\n\n",
			i, i, i, i, prev, i, i, i)
	}
	tool := NewCheckDecompositionTool()
	args, _ := json.Marshal(checkDecompositionArgs{Plan: b.String()})
	out, err := tool.invokableRun(context.Background(), string(args))
	if err != nil {
		t.Fatalf("invokableRun: %v", err)
	}
	if !strings.Contains(out, fmt.Sprintf("the plan carries %d blocks", decompositionMaxBlocks)) {
		t.Fatalf("missed the plan-size hint; got:\n%s", out)
	}
}

// TestCheckDecompositionRequiresResolvedQuestion pins the preamble rule: the
// plan opens with the resolved question - the clause-coverage review walks
// THAT text, and every downstream consumer reads it as the question.
func TestCheckDecompositionRequiresResolvedQuestion(t *testing.T) {
	if got := checkDecomposition(lawfulPlan); len(got) != 0 {
		t.Fatalf("a plan with the resolved-question preamble must pass, got %v", got)
	}
	headerless := strings.Replace(lawfulPlan, "## Resolved question: ", "", 1)
	got := strings.Join(checkDecomposition(headerless), "\n")
	want := "schema integrity: the plan does not open with a `## Resolved question: <the question>` line"
	if !strings.Contains(got, want) {
		t.Fatalf("a plan without the preamble must report %q, got:\n%s", want, got)
	}
	// Bold markup on the header is tolerated (the checker strips emphasis).
	bold := strings.Replace(lawfulPlan, "## Resolved question:", "**## Resolved question:**", 1)
	if got := checkDecomposition(bold); len(got) != 0 {
		t.Fatalf("a bolded resolved-question header must pass, got %v", got)
	}
}

func TestCheckDecompositionFlagsShippedCounterExample(t *testing.T) {
	got := strings.Join(checkDecomposition(examplePlanBad), "\n")
	// The Dynamo inlining - a value a search returned, frozen into the plan - is
	// a judgment about wording: the auditor owns it now. What stays mechanical is
	// the reserved name bound twice and the titles carrying no variable at all.
	for _, want := range []string{
		"schema integrity: variable is bound twice (?answer, blocks 1, 2)",
		"suspect: sub-question is not self-contained - it names no anchor (block 1)",
		"suspect: sub-question is not self-contained - it names no anchor (block 2)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("counter-example missed %q; got:\n%s", want, got)
		}
	}
}

// TestProbeArchivedDecompositions replays the checker over archived runs. It is
// skipped unless DECOMP_PROBE_DIR names a directory of answers.jsonl files, so
// the offline replay needs no absolute path in the repository.
func TestProbeArchivedDecompositions(t *testing.T) {
	dir := os.Getenv("DECOMP_PROBE_DIR")
	if dir == "" {
		t.Skip("set DECOMP_PROBE_DIR to a directory of answers.jsonl files to replay archived decompositions")
	}
	entries, err := filepath.Glob(filepath.Join(dir, "*", "answers.jsonl"))
	if err != nil || len(entries) == 0 {
		t.Skipf("no answers.jsonl under %s", dir)
	}
	thinkRe := regexp.MustCompile(`(?s)<think>.*?</think>`)
	for _, path := range entries {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var row struct {
				QuestionID string `json:"question_id"`
				Question   string `json:"question"`
				Answer     string `json:"ragflow_answer"`
			}
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				continue
			}
			plan := thinkRe.ReplaceAllString(row.Answer, "")
			findings := checkDecomposition(plan)
			t.Logf("%s q%s: %d finding(s) %v", filepath.Base(filepath.Dir(path)), row.QuestionID, len(findings), findings)
		}
	}
}

// TestBrowseCompPlusDecompositionPlans is the POSITIVE case over real questions:
// browsecompplus_decomposition.json carries three BrowseComp-Plus questions
// (25, 875, 1005) with the dataset's own wording, the decomposition an archived
// run shipped, and the normalised plan each question should be decomposed into.
// The normalised plan must pass - a checker that rejects the shape a real
// question calls for is worse than no checker - and the archived plan's own
// findings are logged for the record rather than asserted, so this fixture
// cannot pass by accident and cannot rot silently either.
func TestBrowseCompPlusDecompositionPlans(t *testing.T) {
	raw, err := os.ReadFile("browsecompplus_decomposition.json")
	if err != nil {
		t.Skipf("fixture not available here: %v", err)
	}
	var fixture struct {
		Notice    string `json:"note"`
		Questions []struct {
			QuestionID    string   `json:"question_id"`
			Question      string   `json:"question"`
			GoldAnswer    string   `json:"gold_answer"`
			ExpectedDocID []string `json:"expected_doc_ids"`
			Observed      struct {
				Run           string `json:"run"`
				JudgedCorrect bool   `json:"judged_correct"`
				Blocks        []struct {
					Number      any      `json:"number"`
					Title       string   `json:"title"`
					Op          string   `json:"op"`
					Binds       string   `json:"binds"`
					From        []string `json:"from"`
					Constraints []string `json:"constraints"`
				} `json:"blocks"`
			} `json:"observed"`
			Plan     string `json:"plan"`
			PlanNote string `json:"plan_note"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("fixture is not valid JSON: %v", err)
	}
	if len(fixture.Questions) != 3 {
		t.Fatalf("expected the three fixtures (25, 875, 1005), got %d", len(fixture.Questions))
	}
	for _, q := range fixture.Questions {
		if strings.TrimSpace(q.Question) == "" || strings.TrimSpace(q.Plan) == "" {
			t.Fatalf("q%s is incomplete: question or plan is empty", q.QuestionID)
		}
		if strings.TrimSpace(q.PlanNote) == "" {
			t.Fatalf("q%s: the normalised plan must say what it changed", q.QuestionID)
		}
		// The archived plans predate the resolved-question rule; the fixture
		// prepends the line the current doctrine requires so the schema check
		// under test is not satisfied vacuously by an anachronism.
		plan := "## Resolved question: " + strings.TrimSpace(q.Question) + "\n\n" + q.Plan
		if got := checkDecomposition(plan); len(got) != 0 {
			t.Fatalf("q%s: the normalised plan must pass, got %v", q.QuestionID, got)
		}
		observed := make([]string, 0, len(q.Observed.Blocks))
		for _, b := range q.Observed.Blocks {
			// The archived title is the shipped heading verbatim, so only add one
			// when the fixture stored the bare text.
			head := fmt.Sprintf("### Sub-question %v: %s", b.Number, b.Title)
			if strings.HasPrefix(strings.TrimSpace(b.Title), "#") {
				head = strings.TrimSpace(b.Title)
			}
			observed = append(observed, strings.Join([]string{
				head,
				"- Op: " + b.Op,
				"- Binds: " + b.Binds,
				"- From: " + strings.Join(b.From, ", "),
				"- Constraints: " + strings.Join(b.Constraints, "; "),
			}, "\n"))
		}
		archived := checkDecomposition(strings.Join(observed, "\n\n"))
		t.Logf("q%s (gold %q, %s, judged=%v, %d observed block(s)): the archived plan has %d finding(s) %v; the normalised plan has 0",
			q.QuestionID, q.GoldAnswer, q.Observed.Run, q.Observed.JudgedCorrect, len(q.Observed.Blocks),
			len(archived), archived)
	}
}
