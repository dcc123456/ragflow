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

package clickhouse

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// These tests pin the public data contract shared with the frontend. The
// contract is display-agnostic: RFC3339 timestamps, raw numbers, no formatted
// duration strings, no chart-library-specific series/points wrappers.

// TestBreakdownRow_ZeroValuesArePresent guards the data contract: zero counts
// and a 0.0 fail rate are legitimate values and must not be dropped by omitempty.
func TestBreakdownRow_ZeroValuesArePresent(t *testing.T) {
	dto := BreakdownRow{Key: "tenant-A", Count: 0, Failed: 0, Total: 0, FailRate: 0.0}
	b, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, key := range []string{`"count"`, `"failed"`, `"total"`, `"fail_rate"`} {
		if !strings.Contains(s, key) {
			t.Errorf("zero-value field %s must be present: %s", key, s)
		}
	}
}

func TestOverviewDTO_Shape(t *testing.T) {
	dto := MonitorOverview{
		TotalEvents:  100,
		DistinctDocs: 20,
		FailedEvents: 5,
		Tenants:      3,
		Users:        7,
		StatusCounts: []StatusCount{{Status: "RUNNING", Count: 40}, {Status: "FAILED", Count: 5}},
	}
	b, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, key := range []string{`"total_events"`, `"distinct_docs"`, `"failed_events"`, `"tenants"`, `"users"`, `"status_counts"`} {
		if !strings.Contains(s, key) {
			t.Errorf("overview JSON missing %s: %s", key, s)
		}
	}
	if strings.Contains(s, "event_types") {
		t.Errorf("overview must not expose removed event_types: %s", s)
	}
}

func TestTimeseriesDTO_Shape(t *testing.T) {
	ts := mustTime(t, "2026-11-19T10:00:00Z")
	dto := []TimeseriesRow{{T: ts, Status: "RUNNING", Count: 12}}
	b, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	// RFC3339 timestamp as a string, not epoch millis
	if !strings.Contains(s, "2026-11-19T10:00:00Z") {
		t.Errorf("timeseries time must be RFC3339 string: %s", s)
	}
	for _, key := range []string{`"t"`, `"status"`, `"count"`} {
		if !strings.Contains(s, key) {
			t.Errorf("timeseries row missing %s: %s", key, s)
		}
	}
	for _, banned := range []string{`"series"`, `"points"`} {
		if strings.Contains(s, banned) {
			t.Errorf("timeseries must not use chart-specific %s: %s", banned, s)
		}
	}
}

func TestRunningTaskDTO_NoFormattedDuration(t *testing.T) {
	dto := RunningTask{
		DocID:          "d-1",
		TenantID:       "t-1",
		TenantName:     "Acme",
		UserID:         "u-1",
		UserName:       "Alice",
		Queue:          "high",
		Worker:         "w-1",
		Ingestor:       "parser-a",
		Progress:       0.63,
		StartedAt:      &[]time.Time{mustTime(t, "2026-11-19T08:00:00Z")}[0],
		LastEventAt:    mustTime(t, "2026-11-19T10:15:00Z"),
		RunningSeconds: 8100,
		Status:         "RUNNING",
	}
	b, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, key := range []string{`"running_seconds"`, `"started_at"`, `"last_event_at"`, `"progress"`, `"status"`} {
		if !strings.Contains(s, key) {
			t.Errorf("running task missing %s: %s", key, s)
		}
	}
	if strings.Contains(s, "2h") || strings.Contains(s, "running_duration") {
		t.Errorf("running task must not carry a formatted duration string: %s", s)
	}
}

func TestFilterOptionsDTO_Shape(t *testing.T) {
	dto := FilterOptions{
		Tenants:   []NameID{{ID: "t-1", Name: "Acme"}},
		Users:     []NameID{{ID: "u-1", Name: "Alice"}},
		Docs:      []NameID{{ID: "d-1", Name: "report.pdf"}},
		Queues:    []string{"high", "low"},
		Ingestors: []string{"parser-a"},
		Statuses:  []string{"CREATED", "RUNNING", "COMPLETED", "FAILED", "STOPPED"},
	}
	b, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, key := range []string{`"tenants"`, `"users"`, `"docs"`, `"queues"`, `"ingestors"`, `"statuses"`} {
		if !strings.Contains(s, key) {
			t.Errorf("filters missing %s: %s", key, s)
		}
	}
	if strings.Contains(s, "event_types") {
		t.Errorf("filters must not expose removed event_types: %s", s)
	}
}
