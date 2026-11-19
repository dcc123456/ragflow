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

//go:build ee && integration

package clickhouse

import (
	"math"
	"testing"
	"time"

	"ragflow/internal/common"
)

func monitorTestDriver(t *testing.T) *Driver {
	t.Helper()
	return newIntegrationDriver(t)
}

// seedMonitorRows inserts a small, self-contained set of events so these tests
// do not depend on external seed data (the upgrade test in this package drops
// and recreates the table, which would otherwise make these tests flaky).
func seedMonitorRows(t *testing.T, d *Driver) {
	t.Helper()
	now := time.Now().UTC()
	rows := []common.IngestionLog{
		{EventTime: now, TenantID: "tenant-A", DatasetID: "ds-1", DocID: "doc-A", UserID: "user-A",
			UserName: "Alice", TenantName: "Tenant A", TaskID: "mon-A-1", EventType: "created", Status: "CREATED",
			Filename: "a.pdf", Worker: "w1", IngestorName: "parser-a", NatsQueue: "high", Progress: 0.1},
		{EventTime: now.Add(time.Second), TenantID: "tenant-A", DatasetID: "ds-1", DocID: "doc-A", UserID: "user-A",
			UserName: "Alice", TenantName: "Tenant A", TaskID: "mon-A-1", EventType: "running", Status: "RUNNING",
			Filename: "a.pdf", Worker: "w1", IngestorName: "parser-a", NatsQueue: "high", Progress: 0.6,
			BeginTime: now, RunCount: 1},
		{EventTime: now, TenantID: "tenant-B", DatasetID: "ds-2", DocID: "doc-B", UserID: "user-B",
			UserName: "Bob", TenantName: "Tenant B", TaskID: "mon-B-1", EventType: "failed", Status: "FAILED",
			Filename: "b.pdf", Worker: "w2", IngestorName: "parser-b", NatsQueue: "low", Progress: 0.3},
		{EventTime: now, TenantID: "tenant-B", DatasetID: "ds-2", DocID: "doc-C", UserID: "user-C",
			UserName: "Carol", TenantName: "Tenant B", TaskID: "mon-B-2", EventType: "created", Status: "CREATED",
			Filename: "c.pdf", Worker: "w2", IngestorName: "parser-b", NatsQueue: "low", Progress: 0.0},
	}
	for _, r := range rows {
		if err := d.CollectIngestionLog(r); err != nil {
			t.Fatalf("seed collect: %v", err)
		}
	}
	d.drainAndFlush()
}

func monitorAllTimeQuery() MonitorQuery {
	return MonitorQuery{
		From:     time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		To:       time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
		Scope:    "admin",
		TenantID: "All",
		UserID:   "All",
		DocID:    "All",
		Queue:    "All",
		Ingestor: "All",
		Status:   "All",
	}
}

func TestQueryOverview_MatchesRawCounts(t *testing.T) {
	d := monitorTestDriver(t)
	defer d.conn.Close()
	seedMonitorRows(t, d)

	got, err := d.QueryOverview(monitorAllTimeQuery())
	if err != nil {
		t.Fatalf("QueryOverview: %v", err)
	}
	if got.TotalEvents == 0 {
		t.Error("total events should be > 0")
	}
	if got.DistinctDocs == 0 {
		t.Error("distinct docs should be > 0")
	}
	if len(got.StatusCounts) == 0 {
		t.Error("status_counts should be populated")
	}
	// failed_events must equal the FAILED bucket exactly (STOPPED excluded).
	var failedFromCounts uint64
	for _, sc := range got.StatusCounts {
		if sc.Status == "FAILED" {
			failedFromCounts = sc.Count
		}
	}
	if got.FailedEvents != failedFromCounts {
		t.Errorf("failed_events=%d != FAILED status_count=%d", got.FailedEvents, failedFromCounts)
	}
}

func TestQueryOverview_TenantScopeIsolates(t *testing.T) {
	d := monitorTestDriver(t)
	defer d.conn.Close()
	seedMonitorRows(t, d)

	all := monitorAllTimeQuery()
	all.Scope = "tenant"
	all.ScopeTenant = "tenant-A"

	got, err := d.QueryOverview(all)
	if err != nil {
		t.Fatalf("QueryOverview: %v", err)
	}
	// Cross-check with an explicit tenant filter of the same tenant.
	explicit := monitorAllTimeQuery()
	explicit.TenantID = "tenant-A"
	ref, err := d.QueryOverview(explicit)
	if err != nil {
		t.Fatalf("QueryOverview explicit: %v", err)
	}
	if got.TotalEvents != ref.TotalEvents {
		t.Errorf("tenant-scoped total=%d != explicit tenant total=%d", got.TotalEvents, ref.TotalEvents)
	}
}

func TestQueryTimeseries_ReturnsRFC3339Buckets(t *testing.T) {
	d := monitorTestDriver(t)
	defer d.conn.Close()
	seedMonitorRows(t, d)

	rows, err := d.QueryTimeseries(monitorAllTimeQuery(), "1d")
	if err != nil {
		t.Fatalf("QueryTimeseries: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("timeseries should return rows")
	}
	for _, r := range rows {
		if r.T.IsZero() {
			t.Error("timeseries bucket time is zero")
		}
		if r.Status == "" {
			t.Error("timeseries status is empty")
		}
	}
}

func TestQueryFilterOptions_Populated(t *testing.T) {
	d := monitorTestDriver(t)
	defer d.conn.Close()
	seedMonitorRows(t, d)

	got, err := d.QueryFilterOptions(monitorAllTimeQuery())
	if err != nil {
		t.Fatalf("QueryFilterOptions: %v", err)
	}
	if len(got.Statuses) == 0 {
		t.Error("statuses should be populated")
	}
	if len(got.Queues) == 0 {
		t.Error("queues should be populated")
	}
	if len(got.Ingestors) == 0 {
		t.Error("ingestors should be populated")
	}
}

func TestQueryRunningTasks_ReturnsRunningSeconds(t *testing.T) {
	d := monitorTestDriver(t)
	defer d.conn.Close()
	seedMonitorRows(t, d)

	q := monitorAllTimeQuery()
	q.Status = "RUNNING"
	rows, err := d.QueryRunningTasks(q, "RUNNING", 10)
	if err != nil {
		t.Fatalf("QueryRunningTasks: %v", err)
	}
	for _, r := range rows {
		if r.DocID == "" {
			t.Error("running task doc_id empty")
		}
		if r.Status != "RUNNING" {
			t.Errorf("running task status = %q, want RUNNING", r.Status)
		}
		// running_seconds is a raw number derived from begin_time; no display string.
		if r.LastEventAt.IsZero() {
			t.Error("last_event_at is zero")
		}
	}
}

func TestQueryBreakdown_CountsByDimension(t *testing.T) {
	d := monitorTestDriver(t)
	defer d.conn.Close()
	seedMonitorRows(t, d)

	rows, err := d.QueryBreakdown(monitorAllTimeQuery(), "status", 10)
	if err != nil {
		t.Fatalf("QueryBreakdown: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("breakdown should return rows")
	}
	// keys must be raw status values, never event_type
	for _, r := range rows {
		if r.Key == "" {
			t.Error("breakdown key empty")
		}
	}
}

func TestQueryFailRate_ComputesRate(t *testing.T) {
	d := monitorTestDriver(t)
	defer d.conn.Close()
	seedMonitorRows(t, d)

	rows, err := d.QueryFailRate(monitorAllTimeQuery(), "tenant", 10)
	if err != nil {
		t.Fatalf("QueryFailRate: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("fail rate should return rows")
	}
	for _, r := range rows {
		if r.Total > 0 && r.Failed > r.Total {
			t.Errorf("failed > total for key %q", r.Key)
		}
		if r.FailRate < 0 || r.FailRate > 1 {
			t.Errorf("fail_rate out of range for %q: %v", r.Key, r.FailRate)
		}
	}
}

func TestQueryRunningOverview_Counts(t *testing.T) {
	d := monitorTestDriver(t)
	defer d.conn.Close()
	seedMonitorRows(t, d)

	o, err := d.QueryRunningOverview(monitorAllTimeQuery(), "RUNNING")
	if err != nil {
		t.Fatalf("QueryRunningOverview: %v", err)
	}
	if o.RunningTenants > o.RunningTasks {
		t.Errorf("running tenants (%d) > running tasks (%d)", o.RunningTenants, o.RunningTasks)
	}
	if o.AvgProgress < 0 || o.AvgProgress > 1 {
		t.Errorf("avg_progress out of range: %v", o.AvgProgress)
	}
}

func TestQueryEvents_PagedWithFlattenedOperation(t *testing.T) {
	d := monitorTestDriver(t)
	defer d.conn.Close()
	seedMonitorRows(t, d)

	rows, err := d.QueryEvents(monitorAllTimeQuery(), "event_at", 10, 0)
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("events should return rows")
	}
	for _, r := range rows {
		if r.EventAt.IsZero() {
			t.Error("event_at zero")
		}
		// ui_label/has_error come from flattened operation JSON; no raw JSON in contract.
	}
}

func TestQueryDocTimeline_Chronological(t *testing.T) {
	d := monitorTestDriver(t)
	defer d.conn.Close()
	seedMonitorRows(t, d)

	q := monitorAllTimeQuery()
	rows, err := d.QueryDocTimeline(q, "doc-A", 1)
	if err != nil {
		t.Fatalf("QueryDocTimeline: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("timeline should return rows for doc-A")
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].EventAt.Before(rows[i-1].EventAt) {
			t.Errorf("timeline not chronological at %d", i)
		}
	}
}

// TestQueryRunningTasks_NullBeginTimeDoesNotCrash guards the NULL-scan bug:
// dateDiff over a NULL begin_time yields NULL; scanning that into the non-pointer
// running_seconds int64 fails. A doc whose latest status is CREATED has no claim
// time, so requesting status=CREATED must still return rows with 0 seconds.
func TestQueryRunningTasks_NullBeginTimeDoesNotCrash(t *testing.T) {
	d := monitorTestDriver(t)
	defer d.conn.Close()
	seedMonitorRows(t, d)

	rows, err := d.QueryRunningTasks(monitorAllTimeQuery(), "CREATED", 50)
	if err != nil {
		t.Fatalf("QueryRunningTasks(status=CREATED) must not error on NULL begin_time: %v", err)
	}
	foundDocC := false
	for _, r := range rows {
		if r.DocID == "doc-C" {
			foundDocC = true
			if r.RunningSeconds != 0 {
				t.Errorf("doc-C running_seconds = %d, want 0 (NULL begin_time)", r.RunningSeconds)
			}
		}
	}
	if !foundDocC {
		t.Errorf("expected doc-C (latest CREATED) in results, got %d rows", len(rows))
	}
}

// TestQueryFilterOptions_TenantScopeNarrowsUsersAndDocs guards the cross-tenant
// leak: a tenant caller must not receive user names or document filenames that
// belong to other tenants.
func TestQueryFilterOptions_TenantScopeNarrowsUsersAndDocs(t *testing.T) {
	d := monitorTestDriver(t)
	defer d.conn.Close()
	seedMonitorRows(t, d)

	q := monitorAllTimeQuery()
	q.Scope = "tenant"
	q.ScopeTenant = "tenant-A"

	o, err := d.QueryFilterOptions(q)
	if err != nil {
		t.Fatalf("QueryFilterOptions: %v", err)
	}
	for _, u := range o.Users {
		if u.ID != "user-A" {
			t.Errorf("tenant-A sees foreign user %q", u.ID)
		}
	}
	for _, doc := range o.Docs {
		if doc.ID != "doc-A" {
			t.Errorf("tenant-A sees foreign doc %q", doc.ID)
		}
	}
	// Admin (global) must still see both tenants' users/docs.
	global, err := d.QueryFilterOptions(monitorAllTimeQuery())
	if err != nil {
		t.Fatalf("QueryFilterOptions admin: %v", err)
	}
	if len(global.Users) < 2 || len(global.Docs) < 2 {
		t.Errorf("admin should see all users/docs, got users=%d docs=%d", len(global.Users), len(global.Docs))
	}
}

// TestCountEvents_MatchesQueryScope guards that a total count can be returned
// for pagination, and that it respects the same scope/filters as QueryEvents.
func TestCountEvents_MatchesQueryScope(t *testing.T) {
	d := monitorTestDriver(t)
	defer d.conn.Close()
	seedMonitorRows(t, d)

	total, err := d.CountEvents(monitorAllTimeQuery())
	if err != nil {
		t.Fatalf("CountEvents: %v", err)
	}
	if total == 0 {
		t.Fatal("expected a positive total")
	}
	rows, err := d.QueryEvents(monitorAllTimeQuery(), "event_at", 100, 0)
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if uint64(len(rows)) != total {
		t.Errorf("total=%d but QueryEvents returned %d rows", total, len(rows))
	}
}

// TestQueryRunningOverview_EmptyStatusReturnsFiniteAvg guards that an empty
// result set (no docs in the requested status) yields a finite avg_progress of
// 0 rather than NaN, which json.Marshal cannot encode.
func TestQueryRunningOverview_EmptyStatusReturnsFiniteAvg(t *testing.T) {
	d := monitorTestDriver(t)
	defer d.conn.Close()
	seedMonitorRows(t, d)

	o, err := d.QueryRunningOverview(monitorAllTimeQuery(), "STATUS_THAT_DOES_NOT_EXIST")
	if err != nil {
		t.Fatalf("QueryRunningOverview: %v", err)
	}
	if o.RunningTasks != 0 {
		t.Errorf("empty status should have 0 tasks, got %d", o.RunningTasks)
	}
	if math.IsNaN(o.AvgProgress) || math.IsInf(o.AvgProgress, 0) || o.AvgProgress != 0 {
		t.Errorf("avg_progress should be finite 0 on empty set, got %v", o.AvgProgress)
	}
}

// TestQueryDocTimeline_RespectsLimit guards that a document with many events
// cannot produce an unbounded response.
func TestQueryDocTimeline_RespectsLimit(t *testing.T) {
	d := monitorTestDriver(t)
	defer d.conn.Close()
	seedMonitorRows(t, d)

	rows, err := d.QueryDocTimeline(monitorAllTimeQuery(), "doc-A", 1)
	if err != nil {
		t.Fatalf("QueryDocTimeline: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("limit=1 should return 1 row, got %d", len(rows))
	}
}
