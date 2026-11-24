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
	"regexp"
	"sort"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// checkDecompositionToolName is the deterministic structural check of a DRAFT
// decomposition - the block plan, written BEFORE any search, so its input holds
// only the block titles and their Op / Binds / From / Constraints header lines.
// No `Searched`, `Tested`, `Eliminated`, `Retained`, `Derived` or `Failure` line
// exists at that moment and this tool deliberately checks none of them.
//
// Why it is code and not one more audit opinion: across the archived runs the
// auditor diagnosed these shape defects only partially (in one run a reserved
// name bound by two blocks went unreported while thirty field-integrity findings
// crowded the same verdict), and no run ever repaired one (q875 spent six audit
// rounds on a shape
// defect and shipped anyway). Every check below is a membership test over header
// text, so it holds whatever the wording - the property the gate's own note
// demands of anything that carries an opinion.
//
// The findings reuse the auditor's own fail strings verbatim, so a finding here
// is a finding there; where the auditor's string carries no locator, the locator
// is appended in parentheses so the audited prefix is still a substring of it.
const checkDecompositionToolName = "check_decomposition"

// The plan's size budget, in one pair of constants: AT the budget the plan
// draws an advisory HINT (the planner decides how deep to split), PAST it the
// plan FAILS - a decomposition this wide is not a plan but the question
// restated, and no checker finding inside it is actionable. The default is 10;
// raise it only with a measured reason.
const (
	decompositionMaxBlocks              = 10
	decompositionMaxConstraintsPerBlock = 10
)

const checkDecompositionToolDescription = `Mechanically checks a DRAFT decomposition (the block plan, written before any search) and returns the structural findings it contains. It reads the plan's header fields only - variables, edges, constraint numbers - and every check below is a membership or counting test over them, so it holds whatever the wording.

The checks:
1. Block shape: every block carries, directly under its ` + "`### Sub-question N:`" + ` heading, a ` + "`slot:`" + ` tag from the closed list (name / literal / number / quantity / date / set), a ` + "`- Op:`" + ` line, a ` + "`- Binds: ?variable`" + ` line, and a ` + "`- Constraints:`" + ` line of ` + "`c<k> = <claim>`" + ` definitions separated by ";" - a missing field, an unknown slot type, or text trailing the last definition on the Constraints line is a finding.
2. Variables: names are unique plan-wide; ` + "`?answer`" + ` is bound by exactly ONE block, the one that fills the asked slot.
3. Edges: every ` + "`From:`" + ` variable is bound somewhere; each entry carries its ` + "`(block N)`" + ` pointer and the pointer names the block that binds it; a "follow" block must have a bound input.
4. Graph (a STRICT DAG): ` + "`From: none`" + ` marks a root, and a root whose variable no other block consumes is a second, disconnected question; the graph must be acyclic, and every block must reach the ` + "`?answer`" + ` block - each block's value consumed, transitively, by the block that binds ` + "`?answer`" + ` - so an orphan, a dead-end side branch, or a block consuming ` + "`?answer`" + ` itself is a finding.
5. Boundary: only a ` + "`?variable`" + ` crosses a block boundary - a block number ("block 1") or a prose reference ("the above") inside a title or a constraint is a finding.
6. Titles: every title names a ` + "`?variable`" + ` (its own block's, or one it consumes), and the upstream variables the title references are EXACTLY the ones its constraints reference - a variable the title carries but no constraint tests is asserted without being verified, and a variable the constraints test but the title omits is a dependency hidden from the reader; a title with no variable at all names no anchor.
7. Constraint numbers: the ` + "`c<k>`" + ` numbers are a partition - a number defined by two blocks, or reused for a different claim, is a finding.
8. Constraint form: every constraint uses its own block's variable plus zero or more of the variables its ` + "`From:`" + ` declares - nothing else - so the claim is a proposition one chunk can confirm or refute on its own.
9. ` + "`From:`" + ` faithfulness: the declared set is EXACTLY the upstream variables the constraints use - an undeclared reference and an unused declaration are both findings.

Two sizes are HINTS on the output, never findings - invitations to keep editing the plan rather than verdicts: a plan of ten or more blocks hints that some of them could share one block (merge them), and a block of ten or more constraints hints that part of them settles its own variable with its own anchors (split it off). Two sizes are HARD CAPS and fail as findings: a plan of more than ten blocks, and a block of more than ten constraints. What is deliberately NOT checked, because it is a judgment about wording rather than a mechanical fact: whether a sub-question is EQUIVALENT to its constraint set, whether a title names a real anchor of the question, whether a value inlines another block's candidate - you make those when you draft the plan, and the auditor re-checks them on the delivery.

Call this IMMEDIATELY after writing the decomposition and BEFORE any retrieval: at that moment the plan is only these header lines, and a defect in it costs a whole run - a name bound twice voids every later reference to it, and a title that names no variable leaves every later reader guessing what the block holds.

Input:
- plan: the drafted decomposition exactly as it will appear in the matrix, with no evidence lines (no Searched/Tested/Eliminated/Retained/Derived/Failure).

It reports the same fail strings the answer auditor uses. Fix every finding, adjust for the hints you choose to act on, and call it again; the output NEVER comes back empty - the plan is ready for retrieval only when it starts with ` + "`OK`" + ` (an ` + "`OK`" + ` that carries planning hint(s) still passes: hints are suggestions, findings are not present).`

type checkDecompositionArgs struct {
	Plan string `json:"plan"`
}

// CheckDecompositionTool is a stateless, deterministic parser of the block plan.
type CheckDecompositionTool struct{}

func NewCheckDecompositionTool() *CheckDecompositionTool { return &CheckDecompositionTool{} }

// Info returns the tool's metadata for the chat model.
func (t *CheckDecompositionTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: checkDecompositionToolName,
		Desc: checkDecompositionToolDescription,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"plan": {
				Type: schema.String, Required: true,
				Desc: "The drafted decomposition: every `### Sub-question N:` title plus its `- Op:`, `- Binds:`, `- From:` and `- Constraints:` lines. No evidence lines.",
			},
		}),
	}, nil
}

// InvokableRun parses the plan and reports its structural findings; errors are
// converted to model-readable text by guardedToolRun, like every other tool.
func (t *CheckDecompositionTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	return guardedToolRun(ctx, checkDecompositionToolName, t.invokableRun, argumentsInJSON)
}

func (t *CheckDecompositionTool) invokableRun(_ context.Context, argumentsInJSON string) (string, error) {
	var args checkDecompositionArgs
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("check_decomposition: parse arguments: %w", err)
	}
	if strings.TrimSpace(args.Plan) == "" {
		return "", fmt.Errorf("check_decomposition: plan must be a non-empty string")
	}
	blocks, findings := checkDecompositionBlocks(args.Plan)
	hints := decompositionSizeHints(blocks)
	if len(findings) == 0 {
		if len(hints) == 0 {
			return "OK - no structural finding: the plan is one DAG, every block reaches the ?answer block, every title carries the variable its block binds, and the constraint numbers are a partition. Start retrieving.", nil
		}
		out := "OK - no structural finding, with planning hint(s) you may act on or ignore:\n"
		for _, n := range hints {
			out += "- " + n + "\n"
		}
		return out, nil
	}
	if len(findings) > 24 {
		findings = append(findings[:24], fmt.Sprintf("... and %d more finding(s)", len(findings)-24))
	}
	var b strings.Builder
	b.WriteString("The plan is NOT ready for retrieval. Fix every finding below (they use the auditor's own fail strings) and call this tool again:\n")
	for i, f := range findings {
		fmt.Fprintf(&b, "%d. %s\n", i+1, f)
	}
	for _, n := range hints {
		b.WriteString("- " + n + "\n")
	}
	return b.String(), nil
}

// ── the check itself ────────────────────────────────────────────────────────

// decompositionBlock is one `### Sub-question N:` block reduced to its header
// facts. Evidence lines are ignored on purpose: the draft has none yet.
type decompositionBlock struct {
	number         string
	title          string
	op             string
	binds          string
	hasOp          bool
	slot           string
	hasBinds       bool
	hasFrom        bool
	hasConstraints bool
	trailingField  bool
	fromNone       bool
	fromEntries    []decompositionEdge
	constraintDefs []decompositionConstraint
}

// decompositionEdge is one `From:` entry: the variable it consumes, the block
// it points at (when the entry carries a pointer) and whether it carries one.
type decompositionEdge struct {
	varName    string
	block      string
	hasPointer bool
}

type decompositionConstraint struct {
	id       string // c<k>
	claim    string // the claim text as written, for the candidate-inlining test
	claimKey string // whitespace-collapsed lower-case claim, for the partition-vs-restart test
}

var (
	decompositionHeadRe    = regexp.MustCompile(`(?m)^[^\S\n]*#{2,6}[^\S\n]*Sub-question[^\S\n]*(\d+)[^\S\n]*:?[^\S\n]*(.*)$`)
	decompositionOpRe      = regexp.MustCompile(`(?m)^[^\S\n]*-[^\S\n]*Op:[^\S\n]*(.*)$`)
	decompositionBindsRe   = regexp.MustCompile(`(?m)^[^\S\n]*-[^\S\n]*Binds:[^\S\n]*(\?[A-Za-z0-9_]+)`)
	decompositionFromRe    = regexp.MustCompile(`(?m)^[^\S\n]*-[^\S\n]*From:[^\S\n]*(.*)$`)
	decompositionConRe     = regexp.MustCompile(`(?m)^[^\S\n]*-[^\S\n]*Constraints:[^\S\n]*(.*)$`)
	decompositionCdefRe    = regexp.MustCompile(`\b(c\d+)\s*=\s*([^;]*)`)
	decompositionFromEntRe = regexp.MustCompile(`(\?[A-Za-z0-9_]+)([^,;]*)`)
	decompositionPointerRe = regexp.MustCompile(`\(\s*block[^\S\n]*(\d+)\s*\)`)
	decompositionVarRe     = regexp.MustCompile(`\?[A-Za-z0-9_]+`)
	// A cross-block reference written as a block number or as prose: the entry
	// fields are exempt (`From: ?school (block 1)` legitimately carries one), so
	// only titles and constraint text are scanned.
	decompositionProseRefRe = regexp.MustCompile(`(?i)\bblock[^\S\n]*\d+\b|\bthe above\b|\bthe previous block\b|\bthat block\b|\bthe earlier block\b`)
	decompositionSlotRe     = regexp.MustCompile(`(?i)slot:[^\S\n]*([A-Za-z_]+)`)
)

// checkDecomposition returns the plan's findings, sorted for a stable report.
func checkDecomposition(plan string) []string {
	_, findings := checkDecompositionBlocks(plan)
	return findings
}

// checkDecompositionBlocks parses the plan ONCE and runs every check over the
// parsed blocks, returning them so callers that also need the size hints (the
// hint thresholds read the same parse) never parse the plan a second time.
func checkDecompositionBlocks(plan string) ([]*decompositionBlock, []string) {
	// Markdown bold is stripped before any prefix match: a run that wrote
	// `- **Op:**` emptied every header-derived metric it had, so tolerating the
	// emphasis is a precondition of reading a plan at all.
	plain := strings.ReplaceAll(plan, "*", "")
	blocks, findings := decompositionParse(plain)
	if len(blocks) == 0 {
		return nil, []string{"schema integrity: the plan declares no `### Sub-question N:` block - write the decomposition before retrieving"}
	}
	if len(blocks) > decompositionMaxBlocks {
		findings = append(findings, fmt.Sprintf("schema integrity: the plan carries %d blocks - more than the cap of %d; merge the blocks that settle the same variable", len(blocks), decompositionMaxBlocks))
	}

	// The plan must open with the resolved question (see "The resolved
	// question" in the planner template): that line is the
	// question every downstream consumer reads - the explorer is seeded with
	// it, the auditor pins it, and the review's clause-coverage mapping
	// enumerates ITS clauses - so a plan without it is unauditable for
	// coverage. Checked on the bold-stripped text so a **bold** header
	// passes; position matters: the line belongs in the preamble, before the
	// first block heading.
	if i := strings.Index(plain, "### Sub-question"); i >= 0 && !strings.Contains(plain[:i], "## Resolved question") {
		findings = append(findings, "schema integrity: the plan does not open with a `## Resolved question: <the question>` line - the clause-coverage review walks THAT question")
	}

	// (a) header shape, then the variable bindings: names are unique
	// question-wide and `?answer` is bound by exactly one block.
	byVar := map[string][]*decompositionBlock{}
	var answerBlocks []*decompositionBlock
	for _, b := range blocks {
		if !b.hasBinds || b.binds == "" {
			findings = append(findings, fmt.Sprintf("schema integrity: block header is missing (Binds, block %s)", b.number))
			continue
		}
		// The registry is keyed CASE-INSENSITIVELY: variable names are unique
		// plan-wide regardless of case (?PersonA and ?personA are the same
		// variable), and every consumer looks the name up lower-cased - a
		// mixed-case bind (?personA) registered raw was invisible to every
		// consumer, which read as "consumed variable is never bound" and made
		// every block unreachable.
		byVar[strings.ToLower(b.binds)] = append(byVar[strings.ToLower(b.binds)], b)
		if strings.EqualFold(b.binds, "?answer") {
			answerBlocks = append(answerBlocks, b)
		}
	}
	varNames := sortedKeys(byVar)
	for _, v := range varNames {
		if len(byVar[v]) < 2 {
			continue
		}
		nums := blockNumbers(byVar[v])
		findings = append(findings, fmt.Sprintf("schema integrity: variable is bound twice (%s, blocks %s)", v, strings.Join(nums, ", ")))
	}
	if len(answerBlocks) == 0 {
		findings = append(findings, "schema integrity: no block binds ?answer")
	}

	// (b) `From:` entries: the variable must be bound somewhere, the pointer must
	// be present and must name the block that binds it, and `follow` must be fed.
	for _, b := range blocks {
		if !b.hasFrom {
			continue
		}
		if b.fromNone && b.number != blocks[0].number {
			// A root after the first block is lawful only when something consumes
			// its variable - reported in (c) below.
		}
		if strings.EqualFold(b.op, "follow") && len(b.fromEntries) == 0 {
			findings = append(findings, fmt.Sprintf("schema integrity: follow op has no bound input (block %s)", b.number))
		}
		for _, e := range b.fromEntries {
			binders := byVar[strings.ToLower(e.varName)]
			if strings.HasPrefix(e.varName, "?") && len(binders) == 0 {
				findings = append(findings, fmt.Sprintf("schema integrity: consumed variable is never bound (%s, block %s)", e.varName, b.number))
				continue
			}
			if !e.hasPointer {
				findings = append(findings, fmt.Sprintf("schema integrity: from entry has no block pointer (%s, block %s)", e.varName, b.number))
				continue
			}
			matched := false
			for _, p := range binders {
				if p.number == e.block {
					matched = true
				}
			}
			if !matched && len(binders) > 0 {
				findings = append(findings, fmt.Sprintf("schema integrity: dependency pointer does not match the binding block (%s, block %s)", e.varName, b.number))
			}
		}
	}

	// (c) `From: none` marks a ROOT; a root whose variable nobody consumes is a
	// second, disconnected question, and every block must reach the ?answer block.
	for _, b := range blocks {
		if !b.hasFrom || !b.fromNone || b.binds == "" {
			continue
		}
		consumers := 0
		for _, other := range blocks {
			if other == b {
				continue
			}
			for _, e := range other.fromEntries {
				if strings.EqualFold(e.varName, b.binds) {
					consumers++
				}
			}
		}
		// The `?answer` block is exempt: a one-block plan (or a flat question
		// whose naming block comes last) is exactly that block, and it is not a
		// second question just because nothing consumes the value it names.
		if consumers == 0 && !strings.EqualFold(b.binds, "?answer") {
			findings = append(findings, fmt.Sprintf("schema integrity: root block's variable is never consumed (block %s) - connect it in its consumer's `From:` or delete the block", b.number))
		}
	}
	if len(answerBlocks) == 1 {
		findings = append(findings, decompositionReach(blocks, byVar, answerBlocks[0])...)
	}

	for _, b := range blocks {
		if b.trailingField {
			findings = append(findings, fmt.Sprintf("schema integrity: constraints line carries a trailing field - the chain is stated by From (block %s)", b.number))
		}
	}

	// (d) only a VARIABLE crosses a block boundary.
	for _, b := range blocks {
		if decompositionProseRefRe.MatchString(b.title) {
			findings = append(findings, fmt.Sprintf("schema integrity: block number or prose is used across a block boundary (title, block %s) - write the variable instead", b.number))
		}
		for _, c := range b.constraintDefs {
			if decompositionProseRefRe.MatchString(c.claim) {
				findings = append(findings, fmt.Sprintf("schema integrity: block number or prose is used across a block boundary (%s, block %s) - write the variable instead", c.id, b.number))
			}
		}
	}

	// (e) a title is a STABLE plan: it names a `?variable` - its own block's or one
	// it consumes - never a value a search returned. Whether it names a real anchor
	// of the question is a judgment about wording: the writer makes it when drafting
	// and the auditor re-checks it on the delivery.
	for _, b := range blocks {
		findings = append(findings, decompositionTitle(b)...)
	}

	// (j) the title's referenced variables are EXACTLY the ones its constraints
	// reference (own variable exempt): the title is the plan a reader and the next
	// block see, so a variable it carries but no constraint tests is a dependency
	// asserted without being verified, and a variable the constraints test but the
	// title omits hides that dependency from the reader. Together with (i) this
	// makes the title's variable set a faithful summary of the block's real
	// upstream set.
	for _, b := range blocks {
		if b.binds == "" || len(b.constraintDefs) == 0 {
			continue
		}
		own := strings.ToLower(b.binds)
		titleRefs := map[string]bool{}
		for _, v := range decompositionVarRe.FindAllString(b.title, -1) {
			if low := strings.ToLower(v); low != own {
				titleRefs[low] = true
			}
		}
		usedRefs := map[string]bool{}
		for _, c := range b.constraintDefs {
			for _, v := range decompositionVarRe.FindAllString(c.claim, -1) {
				if low := strings.ToLower(v); low != own {
					usedRefs[low] = true
				}
			}
		}
		for _, v := range sortedKeys(titleRefs) {
			if !usedRefs[v] {
				findings = append(findings, fmt.Sprintf("schema integrity: title references a variable its constraints never use (%s, block %s)", v, b.number))
			}
		}
		for _, v := range sortedKeys(usedRefs) {
			if !titleRefs[v] {
				findings = append(findings, fmt.Sprintf("schema integrity: constraints reference a variable the title does not carry (%s, block %s)", v, b.number))
			}
		}
	}

	// (f) the constraint numbers are a PARTITION, and a number that carries a
	// DIFFERENT claim in a second block is a restarted numbering.
	defined := map[string][]*decompositionBlock{}
	for _, b := range blocks {
		if len(b.constraintDefs) == 0 {
			findings = append(findings, fmt.Sprintf("schema integrity: block defines no constraint (block %s)", b.number))
		}
		if len(b.constraintDefs) > decompositionMaxConstraintsPerBlock {
			findings = append(findings, fmt.Sprintf("schema integrity: block %s carries %d constraints - more than the cap of %d; split the block that settles its own variable with its own anchors", b.number, len(b.constraintDefs), decompositionMaxConstraintsPerBlock))
		}
		for _, c := range b.constraintDefs {
			defined[c.id] = append(defined[c.id], b)
		}
	}
	ids := sortedKeys(defined)
	for _, id := range ids {
		owners := defined[id]
		if len(owners) < 2 {
			continue
		}
		claims := map[string]bool{}
		for _, o := range owners {
			for _, c := range o.constraintDefs {
				if c.id == id {
					claims[c.claimKey] = true
				}
			}
		}
		if len(claims) > 1 {
			findings = append(findings, fmt.Sprintf("schema integrity: constraint number is reused for a different claim (%s)", id))
			continue
		}
		findings = append(findings, fmt.Sprintf("schema integrity: constraint is defined by two blocks (%s)", id))
	}

	// (h) every constraint names the variable its own block binds: with the
	// variable as the SUBJECT a constraint is a proposition one chunk can confirm
	// or refute on its own, which is what the deep read and the auditor then test
	// - the terse noun phrases this replaces ("founded in 1966", "same first and
	// last name") leave the reader to infer what they attach to, and a snippet
	// that "does not support the clue" is usually that inference failing.
	for _, b := range blocks {
		if b.binds == "" {
			continue
		}
		for _, c := range b.constraintDefs {
			if !strings.Contains(strings.ToLower(c.claim), strings.ToLower(b.binds)) {
				findings = append(findings, fmt.Sprintf("schema integrity: constraint does not name the variable this block binds (%s, block %s)", c.id, b.number))
			}
		}
	}

	// (i) the `From:` set IS the set of upstream variables the constraints use:
	// a constraint may reference its own block's variable and the variables it
	// declared - nothing else - and every declared entry must be used by some
	// constraint. The two directions together make `From:` a faithful record of
	// the block's upstream variable set instead of a decorative edge list.
	for _, b := range blocks {
		if b.binds == "" {
			continue
		}
		upstream := map[string]bool{}
		for _, e := range b.fromEntries {
			upstream[strings.ToLower(e.varName)] = true
		}
		used := map[string]bool{}
		for _, c := range b.constraintDefs {
			for _, v := range decompositionVarRe.FindAllString(c.claim, -1) {
				low := strings.ToLower(v)
				if strings.EqualFold(low, b.binds) {
					continue
				}
				if !upstream[low] {
					findings = append(findings, fmt.Sprintf("schema integrity: constraint references a variable this block neither binds nor consumes (%s, %s, block %s)", low, c.id, b.number))
					continue
				}
				used[low] = true
			}
		}
		for _, e := range b.fromEntries {
			if !used[strings.ToLower(e.varName)] {
				findings = append(findings, fmt.Sprintf("schema integrity: from entry is unused by this block's constraints (%s, block %s)", e.varName, b.number))
			}
		}
	}

	sort.Strings(findings)
	return blocks, findings
}

// decompositionParse reads the block headings and their header fields.
func decompositionParse(plain string) ([]*decompositionBlock, []string) {
	heads := decompositionHeadRe.FindAllStringSubmatchIndex(plain, -1)
	blocks := make([]*decompositionBlock, 0, len(heads))
	findings := []string{}
	for i, h := range heads {
		start := h[0]
		end := len(plain)
		if i+1 < len(heads) {
			end = heads[i+1][0]
		}
		body := plain[start:end]
		b := &decompositionBlock{number: plain[h[2]:h[3]], title: strings.TrimSpace(plain[h[4]:h[5]])}
		if m := decompositionSlotRe.FindStringSubmatch(b.title); m != nil {
			b.slot = strings.ToLower(m[1])
		}
		if m := decompositionOpRe.FindStringSubmatch(body); m != nil {
			b.op = strings.TrimSpace(m[1])
			b.hasOp = b.op != ""
		}
		if m := decompositionBindsRe.FindStringSubmatch(body); m != nil {
			b.hasBinds = true
			b.binds = m[1]
		}
		if m := decompositionFromRe.FindStringSubmatch(body); m != nil {
			b.hasFrom = true
			raw := strings.TrimSpace(m[1])
			lower := strings.ToLower(raw)
			if lower == "" || strings.HasPrefix(lower, "none") {
				b.fromNone = true
			} else {
				for _, e := range decompositionFromEntRe.FindAllStringSubmatch(raw, -1) {
					entry := decompositionEdge{varName: e[1]}
					if p := decompositionPointerRe.FindStringSubmatch(e[2]); p != nil {
						entry.hasPointer = true
						entry.block = p[1]
					}
					b.fromEntries = append(b.fromEntries, entry)
				}
			}
		}
		if m := decompositionConRe.FindStringSubmatch(body); m != nil {
			b.hasConstraints = true
			// Every `;`-separated segment must be a numbered definition: text
			// trailing the last constraint is a field the line does not own.
			for _, seg := range strings.Split(m[1], ";") {
				if strings.TrimSpace(seg) != "" && !decompositionCdefRe.MatchString(seg) {
					b.trailingField = true
				}
			}
			for _, cd := range decompositionCdefRe.FindAllStringSubmatch(m[1], -1) {
				b.constraintDefs = append(b.constraintDefs, decompositionConstraint{
					id:       cd[1],
					claim:    strings.TrimSpace(cd[2]),
					claimKey: strings.ToLower(strings.Join(strings.Fields(cd[2]), " ")),
				})
			}
		}
		blocks = append(blocks, b)
	}
	for _, b := range blocks {
		if !b.hasOp {
			findings = append(findings, fmt.Sprintf("schema integrity: op is missing or unknown (block %s)", b.number))
		}
		if !b.hasConstraints {
			findings = append(findings, fmt.Sprintf("schema integrity: constraints line is missing (block %s)", b.number))
		}
		switch {
		case b.slot == "":
			findings = append(findings, fmt.Sprintf("schema integrity: slot is missing (block %s)", b.number))
		case !decompositionSlots[b.slot]:
			findings = append(findings, fmt.Sprintf("schema integrity: slot type unknown (%s, block %s)", b.slot, b.number))
		}
	}
	return blocks, findings
}

// decompositionReach reports the graph defects: a cycle, and a block that
// cannot reach the ?answer block.
//
// The traversal is the plan's own `consumed by` relation - a `From:` entry points
// FROM the consumer TO the block that binds the variable - so the edges built
// here run producer -> consumer and the answer block is where every chain must
// land. The plan must be a STRICT DAG: acyclic, no orphans, and every block's
// value consumed, transitively, by the block that binds `?answer` - so the
// reachability test walks the `From:` edges upstream from the answer block and
// requires every block to be in that closure. A block nothing consumes, and a
// block that consumes `?answer` itself, both fail it (an undirected test once
// tolerated the second shape; the strict rule does not - computing the answer
// from the answer is a cycle in intent).
func decompositionReach(blocks []*decompositionBlock, byVar map[string][]*decompositionBlock, answer *decompositionBlock) []string {
	findings := []string{}
	edges := map[string][]string{}    // producer -> consumer, for the cycle walk
	upstream := map[string][]string{} // consumer -> producer, for the reachability walk
	link := func(producer, consumer string) {
		edges[producer] = append(edges[producer], consumer)
		upstream[consumer] = append(upstream[consumer], producer)
	}
	for _, c := range blocks {
		for _, e := range c.fromEntries {
			for _, p := range byVar[strings.ToLower(e.varName)] {
				if p == c {
					findings = append(findings, fmt.Sprintf("schema integrity: decomposition has a cycle (block %s consumes its own variable %s)", c.number, e.varName))
					continue
				}
				link(p.number, c.number)
			}
		}
	}
	color := map[string]int{} // 0 white, 1 grey, 2 black
	var walk func(n string, path []string)
	walk = func(n string, path []string) {
		switch color[n] {
		case 1:
			findings = append(findings, fmt.Sprintf("schema integrity: decomposition has a cycle (%s)", strings.Join(append(path, n), " -> ")))
			return
		case 2:
			return
		}
		color[n] = 1
		for _, m := range edges[n] {
			walk(m, append(path, n))
		}
		color[n] = 2
	}
	for _, b := range blocks {
		walk(b.number, nil)
	}
	seen := map[string]bool{answer.number: true}
	queue := []string{answer.number}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, m := range upstream[n] {
			if !seen[m] {
				seen[m] = true
				queue = append(queue, m)
			}
		}
	}
	for _, b := range blocks {
		if !seen[b.number] {
			findings = append(findings, fmt.Sprintf("schema integrity: block does not reach the answer block (block %s) - declare its `From` edge or delete the block", b.number))
		}
	}
	return findings
}

// decompositionTitle applies the one mechanical fact a title carries: it is a
// STABLE plan, so it names a `?variable` - its own block's or one it consumes -
// never a value a search returned. Whether the title names a real anchor of the
// question, and whether it states what the constraints carry, are judgments
// about wording: the writer makes them when drafting and the auditor re-checks
// them on the delivery. An earlier version tried to decide them here - exact
// token match against the question, 5-character stem sharing, generic-word and
// calendar-gloss lists, weighted coverage scores, a CJK guard over the lot -
// and every layer was either vacuous on common vocabulary or false-positive on
// lawful paraphrase.
func decompositionTitle(b *decompositionBlock) []string {
	title := b.title
	if i := strings.Index(title, "slot:"); i > 0 {
		title = title[:i]
	}
	title = strings.TrimRight(strings.TrimSpace(title), "-— ")
	if b.binds == "" || decompositionVarRe.MatchString(title) {
		return nil
	}
	return []string{fmt.Sprintf("suspect: sub-question is not self-contained - it names no anchor (block %s)", b.number)}
}

// decompositionSlots is the closed shape list a title's `slot:` tag draws from.
var decompositionSlots = map[string]bool{
	"name": true, "literal": true, "number": true, "quantity": true, "date": true, "set": true,
}

// decompositionSizeHints reports the blocks whose constraint load suggests a
// further split. At decompositionMaxConstraintsPerBlock on one block the
// planner is hinted that part of them may settle its own variable with its
// own anchors, and a deeper DAG is worth considering - but the call is the
// planner's, so this is a HINT on the tool's output and never a finding: it
// cannot turn an OK into a rejection. It takes the ALREADY-PARSED blocks (see
// checkDecompositionBlocks): the hint thresholds are evaluated over the same
// single parse as the findings, never over a second parse of the plan.
func decompositionSizeHints(blocks []*decompositionBlock) []string {
	hints := []string{}
	// The plan-level hint is the mirror of the block-level one: at
	// decompositionMaxBlocks the plan may have been split finer than its
	// evidence supports.
	if len(blocks) >= decompositionMaxBlocks {
		hints = append(hints, fmt.Sprintf("hint: the plan carries %d blocks - at %d or more, consider whether some of them settle variables that could share one block, and the plan has been split too fine", len(blocks), decompositionMaxBlocks))
	}
	for _, b := range blocks {
		if len(b.constraintDefs) >= decompositionMaxConstraintsPerBlock {
			hints = append(hints, fmt.Sprintf("hint: block %s carries %d constraints - at %d or more, consider whether part of them settles their own variable with their own anchors, and deserves a block of their own", b.number, len(b.constraintDefs), decompositionMaxConstraintsPerBlock))
		}
	}
	return hints
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) < len(out[j])
		}
		return out[i] < out[j]
	})
	return out
}

func blockNumbers(blocks []*decompositionBlock) []string {
	nums := make([]string, 0, len(blocks))
	for _, b := range blocks {
		nums = append(nums, b.number)
	}
	sort.Strings(nums)
	return nums
}
