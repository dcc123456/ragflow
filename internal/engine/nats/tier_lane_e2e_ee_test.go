//
//  Copyright (c) 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//
//  This file is part of the RAGFlow Enterprise Edition and is licensed,
//  not sold, under the RAGFlow Enterprise Commercial License Agreement.
//  Unauthorized copying, modification, distribution, or disclosure of this
//  file, via any medium, is strictly prohibited.
//
//  Portions of this file may incorporate code from RAGFlow Community Edition,
//  which is licensed under the Apache License, Version 2.0.
//
//  For commercial licensing inquiries: sales@infiniflow.ai
//

//go:build ee

package nats

import (
	"context"
	"testing"
	"time"

	"ragflow/internal/common"
)

// collectTierTaskIDs drains at most max tasks from the given ingestor tier and
// returns the task IDs it actually received. A generous deadline is used
// because lower-priority lanes each get a 1s fetch budget, so draining a low
// ingestor's three lanes can legitimately take a few seconds.
func collectTierTaskIDs(t *testing.T, queue *NatsEngine, tier common.Tier, max int) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	handles, err := queue.PullMessagesForTier(ctx, tier, max)
	if err != nil {
		t.Fatalf("PullMessagesForTier(%q): %v", tier, err)
	}
	ids := make([]string, 0, len(handles))
	for _, h := range handles {
		ids = append(ids, h.GetMessage().TaskID)
	}
	return ids
}

// hasID reports whether id is present in ids.
func hasID(ids []string, id string) bool {
	for _, got := range ids {
		if got == id {
			return true
		}
	}
	return false
}

// seedThreeLanes publishes one distinctly-named task to each of the three
// lanes so a tier-restricted pull can be checked against exactly what it is
// allowed to see.
func seedThreeLanes(t *testing.T, queue *NatsEngine) {
	t.Helper()
	publishTier(t, queue, common.TaskSubject, "task-low")
	publishTier(t, queue, common.TaskSubjectMedium, "task-medium")
	publishTier(t, queue, common.TaskSubjectHigh, "task-high")
}

// TestTierLaneE2EHighOnlyIsolatesLowAndMedium is the core rule-3 adversarial
// fixture: all three lanes are seeded, but a HIGH ingestor must return only
// the high task. A wrong implementation that drains lower lanes (e.g. ignores
// the tier and uses the whole lane set) would pick up medium/low and fail.
func TestTierLaneE2EHighOnlyIsolatesLowAndMedium(t *testing.T) {
	queue := newTaskQueue(t)
	seedThreeLanes(t, queue)

	ids := collectTierTaskIDs(t, queue, common.TierHigh, 5)

	if len(ids) != 1 || !hasID(ids, "task-high") {
		t.Fatalf("high ingestor got %v, want exactly [task-high]", ids)
	}
	if hasID(ids, "task-medium") || hasID(ids, "task-low") {
		t.Fatalf("high ingestor must NOT consume medium/low lanes; got %v", ids)
	}
}

// TestTierLaneE2EMediumExcludesLow is rule 2: a MEDIUM ingestor drains high +
// medium but never the low lane. Seeding all three lanes makes a wrong
// implementation that collapses medium to the full set (including low) fail.
func TestTierLaneE2EMediumExcludesLow(t *testing.T) {
	queue := newTaskQueue(t)
	seedThreeLanes(t, queue)

	ids := collectTierTaskIDs(t, queue, common.TierMedium, 5)

	if !hasID(ids, "task-high") || !hasID(ids, "task-medium") {
		t.Fatalf("medium ingestor must drain both high and medium lanes; got %v", ids)
	}
	if hasID(ids, "task-low") {
		t.Fatalf("medium ingestor must NOT consume the low lane; got %v", ids)
	}
}

// TestTierLaneE2ELowIncludesAllLanes is rule 1: a LOW ingestor drains all
// three lanes. Seeding only a low-lane task is the adversarial fixture that
// fails if the default lane were accidentally dropped from the low tier's
// allowed set (a mapping that only covered high+medium would return nothing).
func TestTierLaneE2ELowIncludesAllLanes(t *testing.T) {
	queue := newTaskQueue(t)
	seedThreeLanes(t, queue)

	ids := collectTierTaskIDs(t, queue, common.TierDefault, 5)

	for _, want := range []string{"task-high", "task-medium", "task-low"} {
		if !hasID(ids, want) {
			t.Fatalf("low ingestor must drain all lanes; missing %q in %v", want, ids)
		}
	}
}

// TestTierLaneE2EPriorityOrder checks that within a single tier-restricted
// drain, higher lanes are consumed before lower ones. Seeding reverse order
// (low first, high last) ensures FIFO would give the wrong answer; the high
// task must still come back first.
func TestTierLaneE2EPriorityOrder(t *testing.T) {
	queue := newTaskQueue(t)
	publishTier(t, queue, common.TaskSubject, "task-low")
	publishTier(t, queue, common.TaskSubjectMedium, "task-medium")
	publishTier(t, queue, common.TaskSubjectHigh, "task-high")

	ids := collectTierTaskIDs(t, queue, common.TierDefault, 1)

	if len(ids) != 1 || ids[0] != "task-high" {
		t.Fatalf("first drained task = %v, want [task-high] (higher lane wins regardless of publish order)", ids)
	}
}
