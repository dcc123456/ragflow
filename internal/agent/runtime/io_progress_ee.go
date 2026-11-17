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

package runtime

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"ragflow/internal/common"
)

const ioProgressInterval = 15 * time.Second

type ioProgressKey struct{}

type trackedIOCall struct {
	component string
	operation string
	details   string
}

type ioProgressTracker struct {
	mu           sync.Mutex
	nextID       uint64
	active       map[uint64]trackedIOCall
	activeGroups map[string]int
	stageStarted map[string]time.Time
	batches      map[string]*ioProgressBatch
	initialSent  map[string]bool
	lastReported map[string]time.Time
}

type ioProgressBatch struct {
	expectedCalls int
	startedCalls  int
	peakActive    int
	depth         int
}

type ioProgressGroup struct {
	component string
	operation string
	count     int
	started   time.Time
	details   map[string]struct{}
	initial   bool
}

// WithIOProgress starts a task-scoped heartbeat for long-running I/O calls.
// Existing trackers are reused so nested pipeline components share counts.
func WithIOProgress(ctx context.Context) (context.Context, func()) {
	if ctx == nil {
		return ctx, func() {}
	}
	if _, ok := ctx.Value(ioProgressKey{}).(*ioProgressTracker); ok {
		return ctx, func() {}
	}

	tracker := &ioProgressTracker{
		active:       make(map[uint64]trackedIOCall),
		activeGroups: make(map[string]int),
		stageStarted: make(map[string]time.Time),
		batches:      make(map[string]*ioProgressBatch),
		initialSent:  make(map[string]bool),
		lastReported: make(map[string]time.Time),
	}
	reportCtx, cancel := context.WithCancel(ctx)
	reportCtx = context.WithValue(reportCtx, ioProgressKey{}, tracker)
	var finishOnce sync.Once
	go tracker.run(reportCtx)
	return reportCtx, func() {
		finishOnce.Do(func() {
			cancel()
		})
	}
}

// BeginIOProgressBatch delays a group's initial message until its expected
// number of calls have started. If fewer calls start, the batch's peak is
// reported when the batch ends.
func BeginIOProgressBatch(ctx context.Context, component, operation string, expected int) func() {
	if ctx == nil || expected <= 0 {
		return func() {}
	}
	tracker, ok := ctx.Value(ioProgressKey{}).(*ioProgressTracker)
	if !ok || tracker == nil {
		return func() {}
	}
	key := component + "\x00" + operation
	tracker.mu.Lock()
	if tracker.initialSent[key] {
		tracker.mu.Unlock()
		return func() {}
	}
	batch := tracker.batches[key]
	if batch == nil {
		batch = &ioProgressBatch{
			expectedCalls: expected,
			depth:         1,
		}
		tracker.batches[key] = batch
	} else {
		batch.depth++
		if expected > batch.expectedCalls {
			batch.expectedCalls = expected
		}
	}
	tracker.mu.Unlock()

	var finishOnce sync.Once
	return func() {
		finishOnce.Do(func() {
			var report *ioProgressGroup
			tracker.mu.Lock()
			if tracker.batches[key] == batch {
				batch.depth--
				if batch.depth == 0 {
					delete(tracker.batches, key)
					if !tracker.initialSent[key] && batch.startedCalls > 0 {
						group := tracker.groupLocked(component, operation)
						group.initial = true
						if group.count == 0 {
							group.count = batch.peakActive
						}
						report = &group
						tracker.initialSent[key] = true
						if tracker.activeGroups[key] > 0 {
							tracker.lastReported[key] = time.Now()
						}
					}
					if tracker.activeGroups[key] == 0 {
						delete(tracker.activeGroups, key)
						delete(tracker.lastReported, key)
					}
				}
			}
			tracker.mu.Unlock()
			if report != nil {
				tracker.report(ctx, []ioProgressGroup{*report})
			}
		})
	}
}

// TrackIO reports the first active call for a component/operation unless that
// group is inside a logical batch, then tracks calls through their completion.
// Long-running calls report grouped active counts every 15 seconds.
func TrackIO(ctx context.Context, component, operation, details string) func() {
	if ctx == nil {
		return func() {}
	}
	tracker, ok := ctx.Value(ioProgressKey{}).(*ioProgressTracker)
	if !ok || tracker == nil {
		return func() {}
	}
	details = strings.TrimSpace(details)
	finish, report := tracker.begin(component, operation, details)
	if report != nil {
		tracker.report(ctx, []ioProgressGroup{*report})
	}
	return finish
}

// ReportIOProgress emits one INFO progress message for short operations that
// complete before a heartbeat is due, such as index searches and writes.
func ReportIOProgress(ctx context.Context, component, message string) {
	if ctx != nil {
		ReportProgressMessage(ctx, component, message)
	}
}

func (t *ioProgressTracker) begin(component, operation, details string) (func(), *ioProgressGroup) {
	key := component + "\x00" + operation
	now := time.Now()
	t.mu.Lock()
	t.nextID++
	id := t.nextID
	t.active[id] = trackedIOCall{
		component: component,
		operation: operation,
		details:   strings.TrimSpace(details),
	}
	active := t.activeGroups[key]
	if active == 0 {
		if _, exists := t.stageStarted[key]; !exists {
			t.stageStarted[key] = now
		}
	}
	active++
	t.activeGroups[key] = active
	var report *ioProgressGroup
	if batch := t.batches[key]; batch != nil {
		batch.startedCalls++
		if active > batch.peakActive {
			batch.peakActive = active
		}
		if !t.initialSent[key] && batch.startedCalls >= batch.expectedCalls {
			t.initialSent[key] = true
			t.lastReported[key] = now
			group := t.groupLocked(component, operation)
			group.initial = true
			report = &group
		} else if t.initialSent[key] && t.lastReported[key].IsZero() {
			t.lastReported[key] = now
		}
	} else if active == 1 {
		t.lastReported[key] = now
		if !t.initialSent[key] {
			t.initialSent[key] = true
			group := t.groupLocked(component, operation)
			group.initial = true
			report = &group
		}
	}
	t.mu.Unlock()

	var finishOnce sync.Once
	finish := func() {
		finishOnce.Do(func() {
			t.mu.Lock()
			delete(t.active, id)
			t.activeGroups[key]--
			if t.activeGroups[key] == 0 {
				if t.batches[key] == nil {
					delete(t.activeGroups, key)
					delete(t.lastReported, key)
				}
			}
			t.mu.Unlock()
		})
	}
	return finish, report
}

func (t *ioProgressTracker) run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			t.report(ctx, t.snapshot(now))
		}
	}
}

func (t *ioProgressTracker) snapshot(now time.Time) []ioProgressGroup {
	t.mu.Lock()
	defer t.mu.Unlock()

	groups := make(map[string]*ioProgressGroup)
	for _, call := range t.active {
		key := call.component + "\x00" + call.operation
		group := groups[key]
		if group == nil {
			group = &ioProgressGroup{
				component: call.component,
				operation: call.operation,
				started:   t.stageStarted[key],
				details:   make(map[string]struct{}),
			}
			groups[key] = group
		}
		group.count++
		if call.details != "" {
			group.details[call.details] = struct{}{}
		}
	}

	result := make([]ioProgressGroup, 0, len(groups))
	for key, group := range groups {
		if t.batches[key] != nil && !t.initialSent[key] {
			continue
		}
		lastReported := t.lastReported[key]
		if lastReported.IsZero() || now.Sub(lastReported) < ioProgressInterval {
			continue
		}
		t.lastReported[key] = now
		result = append(result, *group)
	}
	for key := range t.lastReported {
		if _, active := groups[key]; !active {
			delete(t.lastReported, key)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].component == result[j].component {
			return result[i].operation < result[j].operation
		}
		return result[i].component < result[j].component
	})
	return result
}

func (t *ioProgressTracker) groupLocked(component, operation string) ioProgressGroup {
	group := ioProgressGroup{
		component: component,
		operation: operation,
		count:     t.activeGroups[component+"\x00"+operation],
		started:   t.stageStarted[component+"\x00"+operation],
		details:   make(map[string]struct{}),
	}
	for _, call := range t.active {
		if call.component != component || call.operation != operation {
			continue
		}
		if call.details != "" {
			group.details[call.details] = struct{}{}
		}
	}
	return group
}

func (t *ioProgressTracker) report(_ context.Context, groups []ioProgressGroup) {
	for _, group := range groups {
		message := group.operation
		if details := strings.Join(groupDetails(group.details), ";"); details != "" {
			message += " | " + details
		}
		if group.initial {
			message += fmt.Sprintf(" | active=%d", group.count)
		} else {
			message += fmt.Sprintf(" | active=%d | elapsed=%s", group.count, time.Since(group.started).Round(time.Second))
		}
		// Heartbeats are high-frequency transient observability. Write them to
		// the process log only: ReportProgressMessage routes through the
		// ProgressMessageCallback, which persists every message to the task log
		// table and grows without bound for long-running parses.
		common.Info(group.component + ": " + message)
	}
}

func groupDetails(details map[string]struct{}) []string {
	if len(details) == 0 {
		return nil
	}
	values := make([]string, 0, len(details))
	for value := range details {
		values = append(values, value)
	}
	sort.Strings(values)
	if len(values) > 3 {
		return append(values[:3], fmt.Sprintf("+%d more", len(values)-3))
	}
	return values
}
