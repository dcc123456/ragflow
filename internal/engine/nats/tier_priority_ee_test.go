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

package nats

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"ragflow/internal/common"

	"github.com/nats-io/nats.go/jetstream"
)

// publishTier publishes one task to a specific tier subject. Every tier
// subject is a child of the legacy "tasks.>" filter, so this works against the
// pre-refactor single stream too (which is what makes these RED tests compile
// and fail on a real priority assertion rather than on a missing symbol).
func publishTier(t *testing.T, queue *NatsEngine, subject, taskID string) {
	t.Helper()
	payload, err := json.Marshal(common.TaskMessage{TaskID: taskID, TaskType: common.TaskTypeIngestionTask})
	if err != nil {
		t.Fatalf("marshal task: %v", err)
	}
	if err := queue.PublishTask(subject, payload); err != nil {
		t.Fatalf("publish to %s: %v", subject, err)
	}
}

// TestPullMessagePrefersHighTier is the core priority RED. Publish low, then
// medium, then high; a priority-aware consumer must return the HIGH task first,
// while the legacy single FIFO consumer returns low (publish order).
func TestPullMessagePrefersHighTier(t *testing.T) {
	queue := newTaskQueue(t)
	publishTier(t, queue, common.TaskSubject, "low-1")
	publishTier(t, queue, common.TaskSubjectMedium, "medium-1")
	publishTier(t, queue, common.TaskSubjectHigh, "high-1")

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	handle, err := queue.PullMessage(ctx)
	if err != nil {
		t.Fatalf("PullMessage: %v", err)
	}
	if handle == nil {
		t.Fatal("PullMessage returned nil, want the high-tier task")
	}
	if got := handle.GetMessage().TaskID; got != "high-1" {
		t.Fatalf("priority pull returned %q, want %q (high must beat medium/low)", got, "high-1")
	}
}

// TestPullMessagePrefersMediumOverLow: with no high task, medium must win over
// low. Guards against a two-tier or medium==low implementation.
func TestPullMessagePrefersMediumOverLow(t *testing.T) {
	queue := newTaskQueue(t)
	publishTier(t, queue, common.TaskSubject, "low-1")
	publishTier(t, queue, common.TaskSubjectMedium, "medium-1")

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	handle, err := queue.PullMessage(ctx)
	if err != nil {
		t.Fatalf("PullMessage: %v", err)
	}
	if handle == nil || handle.GetMessage().TaskID != "medium-1" {
		t.Fatalf("priority pull = %v, want medium-1", handle)
	}
}

// TestPullMessageReturnsLowWhenOnlyLowQueued (adversarial C3): a low-only queue
// must still deliver the low task, not return nil and not reorder. This passes
// today and must keep passing after the priority refactor.
func TestPullMessageReturnsLowWhenOnlyLowQueued(t *testing.T) {
	queue := newTaskQueue(t)
	publishTier(t, queue, common.TaskSubject, "low-only")

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	handle, err := queue.PullMessage(ctx)
	if err != nil {
		t.Fatalf("PullMessage: %v", err)
	}
	if handle == nil || handle.GetMessage().TaskID != "low-only" {
		t.Fatalf("low-only pull = %v, want low-only", handle)
	}
}

// TestPullMessageAllTiersEmptyReturnsNil (adversarial C2): with nothing queued,
// PullMessage must return (nil, nil), not an error and not a fabricated handle.
func TestPullMessageAllTiersEmptyReturnsNil(t *testing.T) {
	queue := newTaskQueue(t)
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	handle, err := queue.PullMessage(ctx)
	if err != nil {
		t.Fatalf("empty pull error = %v, want nil", err)
	}
	if handle != nil {
		t.Fatalf("empty pull handle = %v, want nil", handle)
	}
}

// TestIngestionStreamsArePhysicallySeparated (adversarial B3-prereq): the three
// lanes must be three independent JetStream streams, not one "tasks.>" stream.
// The pre-refactor engine has a single RAGFLOW_TASKS stream, so this fails.
func TestIngestionStreamsArePhysicallySeparated(t *testing.T) {
	host, port := newEmbeddedNatsServer(t)
	queue := NewNatsEngine(host, port)
	if err := queue.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for _, name := range []string{"RAGFLOW_TASKS_HIGH", "RAGFLOW_TASKS_MEDIUM", "RAGFLOW_TASKS"} {
		stream, err := queue.jetStream.Stream(ctx, name)
		if err != nil {
			t.Fatalf("tier stream %s missing: %v (three lanes must be separate streams)", name, err)
		}
		info, err := stream.Info(ctx)
		if err != nil {
			t.Fatalf("stream %s info: %v", name, err)
		}
		if info.Config.Discard != jetstream.DiscardNew {
			t.Fatalf("stream %s discard = %v, want DiscardNew (fail loudly)", name, info.Config.Discard)
		}
	}
}
