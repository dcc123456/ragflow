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
//  See the specific language governing permissions and limitations under
//  the License.

//go:build ee

package agentic_rag

import (
	"context"
	"strings"
	"testing"
)

// TestSearchLedgerAppendOnly pins the ledger's contract: append-only with
// execution-order sequence numbers (entry 1 is the run's actual first search,
// what the candidate-free opening rule is judged against), verbatim argument
// storage, nil-safety, and cursor reads that return the delta after a seq.
func TestSearchLedgerAppendOnly(t *testing.T) {
	l := NewSearchLedger()
	if l.Count() != 0 {
		t.Fatalf("fresh ledger Count = %d, want 0", l.Count())
	}
	l.Add("search_bm25_chunks", `{"queries": ["a"]}`)
	l.Add("grep_chunks", `{"query": "a.*b|b.*a"}`)
	l.Add("search_bm25_chunks", `{"queries": ["a"]}`) // deliberate repeat: a repeated query is behavioral signal

	if l.Count() != 3 {
		t.Fatalf("Count = %d, want 3 (append-only: duplicates are kept)", l.Count())
	}
	full := l.Snapshot(0, 0)
	if len(full) != 3 || full[0].seq != 1 || full[0].tool != "search_bm25_chunks" {
		t.Fatalf("full snapshot = %+v, want 3 entries starting at seq 1", full)
	}
	if !strings.Contains(full[0].args, `"a"`) {
		t.Fatalf("args must be stored verbatim, got %q", full[0].args)
	}
	delta := l.Snapshot(1, 0)
	if len(delta) != 2 || delta[0].seq != 2 {
		t.Fatalf("delta after seq 1 = %+v, want the two later entries", delta)
	}
	capped := l.Snapshot(0, 1)
	if len(capped) != 1 || capped[0].seq != 1 {
		t.Fatalf("capped snapshot = %+v, want only entry 1", capped)
	}

	// The read_search_ledger tool renders one entry per line and reports the
	// delta the same way - the auditor reads both shapes.
	tool := NewReadSearchLedgerTool(l)
	out, err := tool.InvokableRun(context.Background(), `{"after": 2}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if !strings.Contains(out, "3. search_bm25_chunks") {
		t.Fatalf("delta render missing entry 3: %q", out)
	}
	if strings.Contains(out, "1. search_bm25_chunks") {
		t.Fatalf("delta render must not repeat entries at or before the cursor: %q", out)
	}

	// A nil ledger never panics and reads as unavailable - the audit can
	// proceed without coverage facts, it just cannot assert them.
	var nilLedger *searchLedger
	nilLedger.Add("search_bm25_chunks", `{"queries": ["x"]}`)
	if nilLedger.Count() != 0 {
		t.Fatal("nil ledger must ignore Add")
	}
	nilTool := NewReadSearchLedgerTool(nil)
	out, err = nilTool.InvokableRun(context.Background(), `{}`)
	if err != nil || !strings.Contains(out, "no search ledger") {
		t.Fatalf("nil ledger read = %q, err = %v; want the unavailable notice", out, err)
	}
}
