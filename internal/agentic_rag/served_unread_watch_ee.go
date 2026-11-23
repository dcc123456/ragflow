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
	"strings"
	"sync"
)

// Served-unread watchdog: a document the retrieval keeps surfacing and the run
// never opens is a lead going to waste.
//
// The evidence for the mechanism is measured, not stylistic, and it is the same
// evidence that built the locate watchdog: the lead-consumption duty written
// into the system prompt produced zero read_search_ledger calls across three
// measured runs (#39 - 0 calls in every run, while one run burned seventeen
// semantic relation searches and still never opened the document the searches
// kept returning). A rule the model reads once at the top of a 30k-character
// prompt competes with everything else in it; a line attached to the tool
// result it is looking at does not.
//
// What it counts is leads, not activity: the tally is the served documents
// that at least servedUnreadWatchMinServes DIFFERENT QUERIES returned and that
// have never reached a deep read (the unit is the query, never the chunk - see
// servedLedger for what counting chunks did to the ranking). A run that opens
// one of them has consumed the
// lead and hears nothing (and the reminder re-arms for the next batch); a run
// that keeps enumerating new candidates while the same documents pile up
// unheard hears the same short reminder naming them. One reminder per
// deep-read epoch - the reset is the cap.
const (
	// servedUnreadWatchMinServes is how many different QUERIES must have
	// returned a document before it counts as a repeatedly-surfaced candidate
	// rather than the neighbour of one search. Matches the gate's
	// served-unread census so the mid-run nudge and the end-of-run payload
	// describe the same set.
	servedUnreadWatchMinServes = 2

	// servedUnreadWatchMinUnread is how many unread repeatedly-served
	// documents must be on the pile before the watchdog speaks - below that
	// the pile is not a pattern yet.
	servedUnreadWatchMinUnread = 3

	// servedUnreadWatchMaxNamed caps how many stems the reminder names; the
	// ledger tool holds the full list.
	servedUnreadWatchMaxNamed = 5

	// servedUnreadWatchEvery is how many search calls pass between reminders
	// while the pile stands: #40's first firing re-armed on ANY deep read -
	// the model opened unrelated documents and never heard of the named
	// leads again. Progress here is the pile shrinking, not a read happening,
	// so the reminder repeats on this cadence until the pile drops below the
	// threshold.
	servedUnreadWatchEvery = 3
)

// servedUnreadWatch is the per-run watchdog behind the reminder. One instance
// is created per Run over that run's served ledger and deep-read ledger, and
// shared by every tool wrapper; tools execute concurrently within a round, so
// the state is mutex-guarded.
type servedUnreadWatch struct {
	mu     sync.Mutex
	served *servedLedger
	deep   *docIDLedger
	// searchesSince counts search calls since the last reminder (or since
	// the pile last dropped below the threshold). It is the reminder's
	// cadence: while the pile stands, the model hears it every Nth search;
	// a shrinking pile is progress and silences the watchdog.
	searchesSince int
}

// newServedUnreadWatch returns the watchdog for one run. Nil ledgers (probe
// paths that never search) yield a permanently silent watchdog.
func newServedUnreadWatch(served *servedLedger, deep *docIDLedger) *servedUnreadWatch {
	if served == nil || deep == nil {
		return nil
	}
	return &servedUnreadWatch{served: served, deep: deep}
}

// unreadRepeatedlyServed returns the documents served at least
// servedUnreadWatchMinServes times and never deep-read, most-served first,
// capped at servedUnreadWatchMaxNamed.
func (w *servedUnreadWatch) unreadRepeatedlyServed() []string {
	deep := map[string]struct{}{}
	for _, d := range w.deep.Snapshot() {
		deep[d] = struct{}{}
	}
	out := []string{}
	for _, d := range w.served.Docs() {
		if _, read := deep[d]; read {
			continue
		}
		if w.served.Serves(d) < servedUnreadWatchMinServes {
			break // Docs() is most-served first; the rest are quieter still
		}
		out = append(out, fmt.Sprintf("%s (x%d)", d, w.served.Serves(d)))
		if len(out) == servedUnreadWatchMaxNamed {
			break
		}
	}
	return out
}

// observe records one tool invocation and returns the notice to append to that
// tool's result, or "" when the watchdog stays silent.
//
// Silence is the default: a deep read re-arms the watchdog and says nothing, a
// tool that is not a search cannot grow the pile, and an epoch already
// reminded stays reminded until a lead is consumed.
func (w *servedUnreadWatch) observe(tool string) string {
	if w == nil {
		return ""
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	// A deep read does not by itself reset anything: #40 showed a run
	// opening unrelated documents by the dozen while the named leads sat
	// unread. Only the pile shrinking below the threshold is progress.
	if _, searching := locateWatchToolNames[tool]; !searching {
		return ""
	}
	w.searchesSince++
	unread := w.unreadRepeatedlyServed()
	if len(unread) < servedUnreadWatchMinUnread {
		// The pile is consumed (or not yet formed): silence, and the
		// cadence restarts when a fresh pile forms.
		w.searchesSince = 0
		return ""
	}
	if w.searchesSince%servedUnreadWatchEvery != 0 {
		return ""
	}
	w.searchesSince = 0
	return "\n<served-unread-notice> The search results keep surfacing documents you have never opened: " +
		strings.Join(unread, ", ") +
		". These are leads - the corpus keeps returning them because they keep matching what you asked for. " +
		"Before you enumerate more candidates, re-anchor, or declare a constraint unverifiable, open these " +
		"documents with list_chunks; a surfaced lead that goes unread is how a correct anchor is lost while " +
		"wrong ones are enumerated." +
		"\n</served-unread-notice>"
}
