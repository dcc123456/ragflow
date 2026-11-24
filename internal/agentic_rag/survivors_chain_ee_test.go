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
	"errors"
	"strings"
	"testing"
)

// The model-failure classifier is what lets the upper layer attribute a
// turn's death to the provider: the shapes are the failover layer's own
// verdicts and the provider's overload response, both measured in #44.
func TestModelFailureClassification(t *testing.T) {
	yes := []string{
		"[NodeRunError] exceeds max retries: last error: models: EinoChatModel.Generate(MiniMax-M3): API request failed with status 529",
		"models: eino generate failed on every model in the chain",
		"models: eino generate short-circuited by failover cooldown",
		`API request failed with status 529: {"type":"error","error":{"type":"overloaded_error"}}`,
		"[NodeRunError] models: EinoChatModel.Generate(MiniMax-M3): minimax API error: insufficient balance",
	}
	for _, s := range yes {
		if !modelFailure(errors.New(s)) {
			t.Fatalf("must classify as model failure: %q", s[:60])
		}
	}
	no := []string{
		"plan stage returned no readable plan",
		"delivery gate refused the deliverable — Survivors chain order broken",
		"[NodeRunError] run node[ChatModel] pre processor fail: exceeds max iterations",
		"[GraphRunError] context has been canceled: context deadline exceeded",
		"",
	}
	for _, s := range no {
		if modelFailure(errors.New(s)) {
			t.Fatalf("must NOT classify as model failure: %q", s[:60])
		}
	}
	if modelFailure(nil) {
		t.Fatal("nil error is not a model failure")
	}
	// The cause names the provider in the shape the row's error carries.
	if got := modelCause(errors.New(`exceeds max retries: last error: status 529: {"error":{"type":"overloaded_error"}}`)); got != "provider overloaded (HTTP 529)" {
		t.Fatalf("cause = %q", got)
	}
	if got := modelCause(errors.New("plan stage returned no readable plan")); got != "" {
		t.Fatalf("a content failure has no model cause, got %q", got)
	}
}

// chainPlan is a four-block chain: school -> attendee -> twin -> answer, each
// consuming the previous variable - the shape the Survivors line must respect.
const chainPlan = `### Sub-question 1: ?school is the school with 3 founders whose name is the birthplace of one of them — slot: name — kind: institution
- Op: lookup
- Binds: ?school
- From: none
- Constraints: c1 = ?school has 3 founders

### Sub-question 2: ?attendee is an individual who attended ?school — slot: name — kind: person
- Op: follow
- Binds: ?attendee
- From: ?school (block 1)
- Constraints: c2 = ?attendee attended ?school

### Sub-question 3: ?twin is the individual from a different industry sharing ?attendee's first and last name — slot: name — kind: person
- Op: follow
- Binds: ?twin
- From: ?attendee (block 2)
- Constraints: c3 = ?twin shares ?attendee's first and last name

### Sub-question 4: ?answer is the shared first and last name — slot: name — kind: name
- Op: filter
- Binds: ?answer
- From: ?twin (block 3), ?attendee (block 2)
- Constraints: c4 = ?answer is the shared first and last name
`

// The measured bad line (#38 r1): the twin and answer slots filled while the
// attendee slot they stand on read empty. Both downstream fills are chain
// breaks; the answer filler itself traces to the twin set, so the provenance
// check adds nothing here.
func TestSurvivorsChainBreaksOnEmptyUpstream(t *testing.T) {
	final := "## Candidate Matrix\n" +
		"### Sub-question 4: ?answer is the shared first and last name\n" +
		"- Survivors: {?school: [Benenden School], ?attendee: [], ?twin: [Michael Jordan (NBA), Michael Jordan (UK)], ?answer: [Michael Jordan]}"
	blocks, _ := checkDecompositionBlocks(chainPlan)
	got := survivorsChainBreaks(final, blocks)
	if len(got) != 2 {
		t.Fatalf("want 2 chain breaks (twin, answer over empty attendee), got %d: %q", len(got), got)
	}
	joined := strings.Join(got, " | ")
	if !strings.Contains(joined, "?twin") || !strings.Contains(joined, "?attendee") {
		t.Fatalf("breaks must name the filled variable and its empty upstream, got %q", joined)
	}
}

// A chain built in order is silent: every non-empty variable's upstream sets
// are filled, and the answer filler traces to an upstream set.
func TestSurvivorsChainAcceptsOrderedFill(t *testing.T) {
	final := "## Candidate Matrix\n" +
		"- Survivors: {?school: [St Sithians], ?attendee: [Kevin Anderson], ?twin: [Kevin B. Anderson], ?answer: [Kevin Anderson]}"
	blocks, _ := checkDecompositionBlocks(chainPlan)
	if got := survivorsChainBreaks(final, blocks); len(got) != 0 {
		t.Fatalf("an ordered fill must yield no findings, got %q", got)
	}
}

// A property-type answer (the institution where a person died) is a PROPERTY
// of the upstream entity and never a member of any Survivors set - the
// removed provenance check refused such correct deliverables ten passes in a
// row (#42), so the order-only rule must leave it silent. The fill is in
// chain order and every upstream slot is filled.
func TestSurvivorsChainToleratesPropertyAnswer(t *testing.T) {
	final := "## Candidate Matrix\n" +
		"- Survivors: {?person: [George Carlin], ?answer: [St. John's Health Center]}"
	propertyPlan := `### Sub-question 1: ?person is the individual — slot: name — kind: person
- Op: lookup
- Binds: ?person
- From: none
- Constraints: c1 = ?person is the individual

### Sub-question 2: ?answer is the institution where ?person died — slot: name — kind: institution
- Op: follow
- Binds: ?answer
- From: ?person (block 1)
- Constraints: c2 = ?answer is where ?person died
`
	blocks, _ := checkDecompositionBlocks(propertyPlan)
	if got := survivorsChainBreaks(final, blocks); len(got) != 0 {
		t.Fatalf("a property-type answer in chain order must not be flagged, got %q", got)
	}
}

// Tolerance: no Survivors line, no plan blocks, and a bolded Survivors
// variant must all behave - the check adds a refusal reason, not a new way
// for a malformed run to fail.
func TestSurvivorsChainToleratesMalformedInput(t *testing.T) {
	blocks, _ := checkDecompositionBlocks(chainPlan)
	if got := survivorsChainBreaks("## Candidate Matrix\nno survivors here", blocks); got != nil {
		t.Fatalf("a deliverable without a Survivors line must be skipped, got %q", got)
	}
	if got := survivorsChainBreaks("- Survivors: {?school: [x]}", nil); got != nil {
		t.Fatalf("no plan blocks must be skipped, got %q", got)
	}
	// Bold markup on the Survivors line parses like the plain form.
	bold := "## Candidate Matrix\n" +
		"- **Survivors:** {?school: [St Sithians], ?attendee: [Kevin Anderson], ?twin: [Kevin B. Anderson], ?answer: [Kevin Anderson]}"
	if got := survivorsChainBreaks(bold, blocks); len(got) != 0 {
		t.Fatalf("a bolded Survivors line must parse, got %q", got)
	}
}
