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
	"strings"
	"testing"
	"time"

	"ragflow/internal/common"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// captureProcessLog swaps the global logger for an observer capturing INFO
// entries, restoring the original on cleanup.
func captureProcessLog(t *testing.T) *observer.ObservedLogs {
	t.Helper()
	prev := common.Logger
	t.Cleanup(func() { common.Logger = prev })
	core, logs := observer.New(zapcore.InfoLevel)
	common.Logger = zap.New(core)
	return logs
}

func heartbeatGroup() ioProgressGroup {
	return ioProgressGroup{
		component: "DeepDOC",
		operation: "OCRDetect",
		count:     1,
		initial:   true,
		details:   map[string]struct{}{"file_type=pdf": {}},
	}
}

// Core fix: an installed ProgressMessageCallback is the DB-persisting path.
// report() must not invoke it; the heartbeat must instead land in the
// process log only.
func TestReportDoesNotPersistProgressMessage(t *testing.T) {
	logs := captureProcessLog(t)

	var persisted []string
	ctx := WithProgressMessageCallback(context.Background(), func(component, message string) {
		persisted = append(persisted, component+": "+message)
	})

	tr := &ioProgressTracker{}
	tr.report(ctx, []ioProgressGroup{heartbeatGroup()})

	if len(persisted) != 0 {
		t.Fatalf("report() persisted %d progress message(s) via callback: %v", len(persisted), persisted)
	}
	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 process-log entry, got %d: %v", len(entries), entries)
	}
	if got, want := entries[0].Message, "DeepDOC: OCRDetect | file_type=pdf | active=1"; got != want {
		t.Fatalf("process-log message = %q, want %q", got, want)
	}
}

// The periodic heartbeat (initial=false) carries an elapsed suffix and must
// still be visible in the process log while never touching the DB path.
func TestReportEmitsHeartbeatToProcessLog(t *testing.T) {
	logs := captureProcessLog(t)
	ctx := WithProgressMessageCallback(context.Background(), func(_, _ string) {
		t.Fatal("heartbeat must not reach the progress-message (DB) callback")
	})

	g := heartbeatGroup()
	g.operation = "TSR"
	g.initial = false
	g.count = 2
	g.started = time.Now().Add(-41 * time.Second)

	tr := &ioProgressTracker{}
	tr.report(ctx, []ioProgressGroup{g})

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 process-log entry, got %d: %v", len(entries), entries)
	}
	msg := entries[0].Message
	for _, want := range []string{"DeepDOC: TSR", "active=2", "elapsed="} {
		if !strings.Contains(msg, want) {
			t.Errorf("process-log message %q missing %q", msg, want)
		}
	}
}

// Distinguishing condition: the initial message has no elapsed suffix while
// the recurring heartbeat does.
func TestReportInitialVsHeartbeatFormat(t *testing.T) {
	cases := []struct {
		name        string
		initial     bool
		wantElapsed bool
	}{
		{name: "initial", initial: true, wantElapsed: false},
		{name: "heartbeat", initial: false, wantElapsed: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureProcessLog(t)
			g := heartbeatGroup()
			g.initial = tc.initial
			g.started = time.Now().Add(-15 * time.Second)

			tr := &ioProgressTracker{}
			tr.report(context.Background(), []ioProgressGroup{g})

			entries := logs.All()
			if len(entries) != 1 {
				t.Fatalf("expected 1 log entry, got %d", len(entries))
			}
			hasElapsed := strings.Contains(entries[0].Message, "elapsed=")
			if hasElapsed != tc.wantElapsed {
				t.Fatalf("message %q: elapsed present = %v, want %v", entries[0].Message, hasElapsed, tc.wantElapsed)
			}
		})
	}
}

// Corner case: many details are deduped, sorted, and capped with a "+N more"
// marker in the process-log message.
func TestReportJoinsAndLimitsDetails(t *testing.T) {
	logs := captureProcessLog(t)
	g := heartbeatGroup()
	g.operation = "TSR"
	g.details = map[string]struct{}{
		"d": {}, "b": {}, "a": {}, "c": {},
	}

	tr := &ioProgressTracker{}
	tr.report(context.Background(), []ioProgressGroup{g})

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(entries))
	}
	if got, want := entries[0].Message, "DeepDOC: TSR | a;b;c;+1 more | active=1"; got != want {
		t.Fatalf("process-log message = %q, want %q", got, want)
	}
}

// Corner case: no groups produce no output on either path.
func TestReportEmptyGroups(t *testing.T) {
	logs := captureProcessLog(t)
	ctx := WithProgressMessageCallback(context.Background(), func(_, _ string) {
		t.Fatal("unexpected progress-message callback for empty groups")
	})
	tr := &ioProgressTracker{}
	tr.report(ctx, nil)
	if got := len(logs.All()); got != 0 {
		t.Fatalf("expected 0 log entries, got %d", got)
	}
}

// Guard against an over-broad fix: ReportProgressMessage itself must still
// reach an installed callback, because short-operation callers rely on it.
func TestReportProgressMessageChannelIntact(t *testing.T) {
	var got []string
	ctx := WithProgressMessageCallback(context.Background(), func(component, message string) {
		got = append(got, component+": "+message)
	})
	ReportProgressMessage(ctx, "Indexer", "indexed 3 chunks")
	if len(got) != 1 || got[0] != "Indexer: indexed 3 chunks" {
		t.Fatalf("ReportProgressMessage did not reach callback: %v", got)
	}
}
