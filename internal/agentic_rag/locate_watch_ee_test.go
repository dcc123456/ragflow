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

// TestLocateWatchdogSpeaksOnChurnWithoutReading pins the cadence against an
// explicit expectation table: silence for the first two locate calls, a reminder
// on every third call after that while the run keeps churning. There is no upper
// bound on how often it speaks — the deep read is what silences it, not a counter.
func TestLocateWatchdogSpeaksOnChurnWithoutReading(t *testing.T) {
	w := newLocateWatch()
	w.markSemanticAvailable()

	// Locate call number -> does the watchdog speak? Every 3rd call, uncapped.
	want := map[int]bool{1: false, 2: false, 3: true, 4: false, 5: false, 6: true,
		7: false, 8: false, 9: true, 10: false, 11: false, 12: true}
	spoken := 0
	for call := 1; call <= 12; call++ {
		tool := grepChunksToolName
		if call == 3 {
			// The tally spans tools: it counts locate calls, not grep calls.
			tool = searchBm25ChunksToolName
		}
		got := w.observe(tool)
		if (got != "") != want[call] {
			t.Errorf("locate call %d: spoke=%v, want %v (got %q)", call, got != "", want[call], got)
		}
		if got != "" {
			spoken++
			if !strings.Contains(got, searchSemanticChunksToolName) {
				t.Errorf("the reminder must name the meaning-based leg, got %q", got)
			}
			if !strings.Contains(got, "CATEGORY") || !strings.Contains(got, "MEANING") {
				t.Errorf("the reminder must carry the reason and the category fallback, got %q", got)
			}
		}
	}
	if w.nudges != 4 || spoken != 4 {
		t.Errorf("nudges = %d (spoken %d), want 4 at calls 3,6,9,12", w.nudges, spoken)
	}
}

// TestLocateWatchdogDeepReadResets: a document read is progress, so the churn
// count starts over — a search-then-read pattern is never nudged, and one read
// buys exactly three more searches.
func TestLocateWatchdogDeepReadResets(t *testing.T) {
	w := newLocateWatch()
	w.markSemanticAvailable()
	for round := 0; round < 6; round++ {
		for i := 0; i < 2; i++ {
			if q := w.observe(grepChunksToolName); q != "" {
				t.Fatalf("round %d search %d: a search-then-read pattern must never be nudged, got %q", round, i, q)
			}
		}
		w.observe(listChunksToolName) // progress: the tally goes back to zero
	}
	if w.sinceRead != 0 {
		t.Fatalf("sinceRead = %d after a deep read, want 0", w.sinceRead)
	}

	// Three searches after the read: the third is the reminder point again.
	w.observe(grepChunksToolName)
	w.observe(grepChunksToolName)
	if q := w.observe(grepChunksToolName); q == "" {
		t.Error("the third locate call after a read is the reminder point")
	}
}

// TestLocateWatchdogScope: it only speaks about a tool the run holds, and one use
// of the bridge — even an unproductive one — ends the reminders for good. The
// point is getting the leg tried, not policing its results.
func TestLocateWatchdogScope(t *testing.T) {
	absent := newLocateWatch()
	for i := 0; i < 9; i++ {
		if got := absent.observe(grepChunksToolName); got != "" {
			t.Fatalf("must not advertise an absent tool: %q", got)
		}
	}

	used := newLocateWatch()
	used.markSemanticAvailable()
	used.observe(searchSemanticChunksToolName)
	for i := 0; i < 9; i++ {
		if got := used.observe(grepChunksToolName); got != "" {
			t.Fatalf("a used bridge must end the reminders: %q", got)
		}
	}

	// A bridge call is progress too: it resets the churn count.
	resetByBridge := newLocateWatch()
	resetByBridge.markSemanticAvailable()
	resetByBridge.observe(grepChunksToolName)
	resetByBridge.observe(searchSemanticChunksToolName)
	for i := 0; i < 2; i++ {
		if got := resetByBridge.observe(grepChunksToolName); got != "" {
			t.Errorf("the bridge call should have reset the count (search %d), got %q", i+1, got)
		}
	}
	if got := resetByBridge.observe(grepChunksToolName); got != "" {
		t.Errorf("once the bridge has been used the watchdog stays silent for the rest of the run, got %q", got)
	}
}
