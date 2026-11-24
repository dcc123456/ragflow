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
	"strings"
	"testing"
)

// The served-unread watchdog turns the lead-consumption duty from a line in
// the system prompt into a line on the tool result: documents served
// repeatedly but never deep-read are named where the model is looking when it
// decides what to query next. The reminder repeats on a cadence while the
// pile stands - #40 showed a run opening unrelated documents by the dozen
// while the named leads sat unread, so only the pile shrinking below the
// threshold counts as progress and silences the watchdog.
func TestServedUnreadWatch(t *testing.T) {
	served := NewServedLedger()
	deep := NewDocIDLedger()
	w := newServedUnreadWatch(served, deep)
	if w == nil {
		t.Fatal("a watchdog over live ledgers must exist")
	}

	// A document one query returned is a neighbour, not a pattern: silent
	// even after a search.
	served.Add("q-one", "10509")
	if got := w.observe(grepChunksToolName); got != "" {
		t.Fatalf("one query must stay silent, got %q", got)
	}

	// Three documents several DIFFERENT queries returned and nobody opened:
	// the watchdog speaks on the cadence, naming the stems and the deep-read
	// tool.
	for _, d := range []string{"13204", "16606", "10509"} {
		served.Add("q1", d)
		served.Add("q2", d)
		served.Add("q3", d)
	}
	if got := w.observe(searchBm25ChunksToolName); got != "" {
		t.Fatalf("the first search of the cadence must stay silent, got %q", got)
	}
	if got := w.observe(grepChunksToolName); got != "" {
		t.Fatalf("the second search of the cadence must stay silent, got %q", got)
	}
	got := w.observe(searchBm25ChunksToolName)
	if got == "" {
		t.Fatal("the third search must trigger the reminder")
	}
	for _, want := range []string{"10509", "13204", "list_chunks"} {
		if !strings.Contains(got, want) {
			t.Fatalf("reminder must name the leads and the deep-read tool, got %q", got)
		}
	}

	// A deep read of an UNRELATED document is not progress: the pile stands,
	// and the reminder returns on the next cadence tick.
	deep.Add("99999")
	if got := w.observe(listChunksToolName); got != "" {
		t.Fatalf("a deep read must be silent in itself, got %q", got)
	}
	if got := w.observe(grepChunksToolName); got != "" {
		t.Fatalf("cadence restart 1 must be silent, got %q", got)
	}
	if got := w.observe(grepChunksToolName); got != "" {
		t.Fatalf("cadence restart 2 must be silent, got %q", got)
	}
	if got := w.observe(searchBm25ChunksToolName); got == "" || !strings.Contains(got, "10509") {
		t.Fatalf("an unrelated read must not silence the watchdog, got %q", got)
	}

	// Opening one named lead shrinks the pile below the threshold: silence,
	// and the cadence restarts from zero.
	deep.Add("13204")
	if got := w.observe(listChunksToolName); got != "" {
		t.Fatalf("a deep read must be silent in itself, got %q", got)
	}
	for i := 0; i < 5; i++ {
		if got := w.observe(grepChunksToolName); got != "" {
			t.Fatalf("a consumed pile must stay silent (call %d), got %q", i+1, got)
		}
	}

	// A fresh pile above the threshold is named again on the cadence.
	for _, d := range []string{"17061", "17062", "1756"} {
		served.Add("q1", d)
		served.Add("q2", d)
		served.Add("q3", d)
	}
	for i := 0; i < 2; i++ {
		if got := w.observe(searchBm25ChunksToolName); got != "" {
			t.Fatalf("fresh-pile cadence %d must be silent, got %q", i+1, got)
		}
	}
	if got := w.observe(searchSemanticChunksToolName); got == "" || !strings.Contains(got, "17061") {
		t.Fatalf("a fresh pile must be named, got %q", got)
	}

	// A nil watchdog (no ledgers) is permanently silent and the observe path
	// must not panic on non-search tools.
	var nilWatch *servedUnreadWatch
	if got := nilWatch.observe(grepChunksToolName); got != "" {
		t.Fatalf("nil watchdog must stay silent, got %q", got)
	}
	if got := w.observe(thinkToolName); got != "" {
		t.Fatalf("a non-search tool must not trigger the watchdog, got %q", got)
	}
}
