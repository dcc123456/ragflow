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
	"fmt"
	"regexp"
	"strings"
)

// Survivors chain integrity: the mechanical half of the "look ahead, then
// come back" discipline. Only the ORDER half is enforced here. An earlier
// version also refused an answer filler that traced to no upstream Survivors
// set ("entered from outside the run"); #42 measured that as a false-positive
// generator - a property-type answer (the institution where a person died)
// legitimately names a value that is a PROPERTY of the upstream entity and
// never a member of any Survivors set, so the check refused correct
// deliverables ten passes in a row and burned the audit budget. The
// chain-outside-answer defect that motivated it is caught by the order rule
// itself (the measured case had the downstream filled over an empty
// upstream), so the provenance half is gone.
//
// A Survivors line is a structural artifact - a variable, a set - and the
// plan's `From` edges are structural facts the stage-one check already
// verified. That makes chain order machine-checkable in a way evidence
// quality is not: a downstream variable standing on an empty upstream slot is
// a fact about two sets, not a reading of prose, so the gate enforces it
// itself instead of asking the auditor to notice (the auditor's contract
// grades survivor opinions advisory, and #38-#40 measured the result: a
// deliverable whose ?answer read "Michael Jordan" while the ?attendee slot it
// depended on read empty). Refusing here, before the audit, buys the repair
// turn the discipline needs: the producer must go back and establish the
// empty upstream block, exactly what the doctrine's chain-order rule demands.
//
// Tolerance is deliberate: a deliverable with no Survivors line, or a plan
// that parses to no blocks, yields no findings - this check adds a refusal
// reason, it does not become a new way for a malformed run to fail.

// survivorsLineRe finds a Survivors tail line. The brace body is extracted
// separately so a bolded variant (`**Survivors:**`) matches the same way -
// the plan parser already strips bold for the same reason.
var survivorsLineRe = regexp.MustCompile(`(?m)^[^\S\n]*-[^\S\n]*Survivors:[^\S\n]*\{(.*)\}[^\S\n]*$`)

// survivorsEntryRe splits the brace body into `?var: [fillers]` entries.
var survivorsEntryRe = regexp.MustCompile(`\?([A-Za-z0-9_]+)\s*:\s*\[([^\]]*)\]`)

// parseSurvivorsLine returns the LAST Survivors line's variable sets (the
// lines are cumulative, so the last one is the run's final word), or nil when
// the deliverable carries none.
func parseSurvivorsLine(final string) map[string][]string {
	var body string
	for _, m := range survivorsLineRe.FindAllStringSubmatch(final, -1) {
		body = m[1]
	}
	if body == "" {
		return nil
	}
	sets := map[string][]string{}
	for _, e := range survivorsEntryRe.FindAllStringSubmatch(body, -1) {
		name := e[1]
		var fillers []string
		for _, f := range strings.Split(e[2], ",") {
			if f = strings.TrimSpace(f); f != "" {
				fillers = append(fillers, f)
			}
		}
		sets[name] = fillers
	}
	return sets
}

// survivorsChainBreaks returns the chain-order violations between the
// deliverable's final Survivors line and the plan's From edges: a variable
// filled while one of its declared upstream dependencies reads empty, and an
// answer filler that traces to no upstream set at all. Both are set facts, so
// both are reported as mechanical findings.
func survivorsChainBreaks(final string, blocks []*decompositionBlock) []string {
	sets := parseSurvivorsLine(final)
	if len(sets) == 0 || len(blocks) == 0 {
		return nil
	}
	empty := func(v string) bool { return len(sets[v]) == 0 }

	var breaks []string
	seen := map[string]struct{}{}
	add := func(b string) {
		if _, dup := seen[b]; !dup {
			seen[b] = struct{}{}
			breaks = append(breaks, b)
		}
	}

	// 1) Chain order: a variable is filled while a From dependency it is
	// built on still reads empty on the same (cumulative) line. The plan
	// parser keeps `binds` as the single bound variable (with its `?`) and
	// each From edge's varName likewise, so the keys are normalized here.
	trimVar := func(v string) string { return strings.TrimPrefix(strings.TrimSpace(v), "?") }
	for _, b := range blocks {
		v := trimVar(b.binds)
		if v == "" || empty(v) {
			continue
		}
		for _, e := range b.fromEntries {
			u := trimVar(e.varName)
			if u == "" || !empty(u) {
				continue
			}
			upstream := fmt.Sprintf("?%s", u)
			if e.hasPointer && e.block != "" {
				upstream = fmt.Sprintf("?%s (block %s)", u, e.block)
			}
			add(fmt.Sprintf("?%s is filled while its upstream %s is empty - the chain order is broken", v, upstream))
		}
	}

	return breaks
}

// survivorsChainDirective is the repair directive for a chain-broken
// deliverable: go back and establish the empty upstream blocks first; a
// downstream value standing on an empty slot is a guess, not a finding.
func survivorsChainDirective(breaks []string) string {
	return "Your Survivors lines break the chain order: " + strings.Join(breaks, "; ") + ". " +
		"Fill a downstream variable only when every variable its block's `From` declares is already filled on " +
		"the same Survivors line. The repair is to COME BACK: re-open the empty upstream block - re-sweep its " +
		"relation with fresh wording, read the served-but-unread documents, add candidates to its ledger - and " +
		"establish it first. Then re-render the COMPLETE deliverable (`## Candidate Matrix`, `## Reasoning " +
		"Chain`, and the Final/Guessed Answer line) in this same turn."
}
