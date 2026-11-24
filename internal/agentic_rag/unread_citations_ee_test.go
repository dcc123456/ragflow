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
	"reflect"
	"strings"
	"testing"
)

// deliveredWithCites renders a minimal FOS deliverable citing the given chunk ids,
// one per matrix line - the shape ExtractCitedChunkIDs reads.
func deliveredWithCites(ids ...string) string {
	var b strings.Builder
	b.WriteString("## Candidate Matrix\n### Sub-question 1: who is it\n")
	for _, id := range ids {
		b.WriteString("- Tested: X (doc: 1.md, doc_id: d1, chunk_id: " + id + ", snippet: \"x\")\n")
	}
	b.WriteString("## Reasoning Chain\n- Clue: x (doc: 1.md, doc_id: d1, chunk_id: " + ids[0] + ", snippet: \"x\")\n")
	b.WriteString("Final Answer: **X**\n")
	return b.String()
}

func TestUnreadCitations(t *testing.T) {
	reads := NewChunkReadLedger()
	reads.AddDeepIDs("c1", "c2")
	reads.AddShallowIDs("c3")

	cases := []struct {
		name  string
		final string
		reads *chunkReadLedger
		want  []string
	}{
		{
			// Every cited id was served, deep or shallow: the deliverable is grounded
			// in what the run actually read and the gate has nothing to say.
			name: "all cited chunks were read", final: deliveredWithCites("c1", "c2", "c3"),
			reads: reads, want: nil,
		},
		{
			// A triage snippet IS a read: the ledger keeps deep and shallow apart for
			// measurement, not to make snippet-cited lines fail here.
			name: "shallow read counts as read", final: deliveredWithCites("c3"), reads: reads, want: nil,
		},
		{
			name: "unread id is returned, sorted", final: deliveredWithCites("cx", "c1", "cy"),
			reads: reads, want: []string{"cx", "cy"},
		},
		{
			// No testimony without a record: a nil or empty ledger says the run's reads
			// were never accounted for, not that the citations are fabricated.
			name: "nil ledger yields nothing", final: deliveredWithCites("c1", "cx"), reads: nil, want: nil,
		},
		{
			name: "empty ledger yields nothing", final: deliveredWithCites("c1", "cx"),
			reads: NewChunkReadLedger(), want: nil,
		},
		{
			name: "no citations yields nothing", final: "## Reasoning Chain\n- Clue: x\nFinal Answer: **X**\n",
			reads: reads, want: nil,
		},
		{
			// A web-sourced line carries url/title instead of a chunk_id by design, so
			// it never reaches this check.
			name:  "web-sourced line is not a citation",
			final: "## Candidate Matrix\n- Tested: X (source: web, url: https://e.com, title: T, snippet: \"x\")\nFinal Answer: **X**\n",
			reads: reads, want: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := unreadCitations(c.final, c.reads); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("unreadCitations() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestUnreadCitationDirective(t *testing.T) {
	got := unreadCitationDirective([]string{"cx", "cy"})
	for _, want := range []string{"cx", "cy", "list_chunks", "doc_id", "COMPLETE deliverable"} {
		if !strings.Contains(got, want) {
			t.Fatalf("directive does not mention %q:\n%s", want, got)
		}
	}
}

func TestCountUnreadCitation(t *testing.T) {
	var nilAudit *GateAuditRecord
	countUnreadCitation(nilAudit) // nil-safe
	audit := &GateAuditRecord{}
	countUnreadCitation(audit)
	countUnreadCitation(audit)
	if audit.UnreadCitations != 2 {
		t.Fatalf("UnreadCitations = %d, want 2", audit.UnreadCitations)
	}
	// The counter is recorded apart from the other refusal counts so a benchmark can
	// tell "cited what I never read" from "no deliverable at all".
	if audit.Rejections != 0 || audit.CitationGroundings != 0 {
		t.Fatalf("unread citations leaked into other counters: %+v", audit)
	}
}
