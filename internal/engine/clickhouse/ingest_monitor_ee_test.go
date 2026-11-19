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
	"strings"
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("bad test time %q: %v", s, err)
	}
	return v
}

// ---- ParseMonitorWindow ----

func TestParseMonitorWindow_ValidRFC3339(t *testing.T) {
	from, to, err := ParseMonitorWindow("2026-11-01T00:00:00Z", "2026-11-19T12:30:00Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !from.Equal(mustTime(t, "2026-11-01T00:00:00Z")) {
		t.Errorf("from = %v", from)
	}
	if !to.Equal(mustTime(t, "2026-11-19T12:30:00Z")) {
		t.Errorf("to = %v", to)
	}
}

func TestParseMonitorWindow_OffsetConvertedToUTC(t *testing.T) {
	from, to, err := ParseMonitorWindow("2026-11-01T08:00:00+08:00", "2026-11-02T08:00:00+08:00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if from.Location() != time.UTC {
		t.Errorf("from location = %v, want UTC", from.Location())
	}
	if !from.Equal(mustTime(t, "2026-11-01T00:00:00Z")) {
		t.Errorf("from = %v, want 2026-11-01T00:00:00Z", from)
	}
	_ = to
}

func TestParseMonitorWindow_RejectsInvalidFrom(t *testing.T) {
	if _, _, err := ParseMonitorWindow("not-a-time", "2026-11-19T00:00:00Z"); err == nil {
		t.Fatal("expected error for invalid from")
	}
}

func TestParseMonitorWindow_RejectsInvalidTo(t *testing.T) {
	if _, _, err := ParseMonitorWindow("2026-11-01T00:00:00Z", "2026-11-19"); err == nil {
		t.Fatal("expected error for invalid to")
	}
}

func TestParseMonitorWindow_RejectsReversedRange(t *testing.T) {
	if _, _, err := ParseMonitorWindow("2026-11-19T00:00:00Z", "2026-11-01T00:00:00Z"); err == nil {
		t.Fatal("expected error when from > to")
	}
}

func TestParseMonitorWindow_AllowsEqualRange(t *testing.T) {
	if _, _, err := ParseMonitorWindow("2026-11-01T00:00:00Z", "2026-11-01T00:00:00Z"); err != nil {
		t.Fatalf("equal range should be valid, got %v", err)
	}
}

func TestParseMonitorWindow_RejectsEmpty(t *testing.T) {
	if _, _, err := ParseMonitorWindow("", ""); err == nil {
		t.Fatal("expected error for empty window")
	}
}

// ---- BuildMonitorWhere ----

func TestBuildMonitorWhere_AllValuesNoFilterClause(t *testing.T) {
	from := mustTime(t, "2026-11-01T00:00:00Z")
	to := mustTime(t, "2026-11-19T00:00:00Z")
	sql, args, err := BuildMonitorWhere(from, to, "admin", "", "All", "All", "All", "All", "All", "All")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, col := range []string{"tenant_id =", "user_id =", "doc_id =", "nats_queue =", "ingestor_name =", "status ="} {
		if strings.Contains(sql, col) {
			t.Errorf("SQL should not contain %q for All values, got: %s", col, sql)
		}
	}
	if !strings.Contains(sql, "event_at >= ?") || !strings.Contains(sql, "event_at <= ?") {
		t.Errorf("SQL missing time bounds: %s", sql)
	}
	// only the 4 time args (2× event_at, 2× event_date)
	if len(args) != 4 {
		t.Errorf("args len = %d, want 4, args=%v", len(args), args)
	}
}

func TestBuildMonitorWhere_SpecificValuesBoundAsArgs(t *testing.T) {
	from := mustTime(t, "2026-11-01T00:00:00Z")
	to := mustTime(t, "2026-11-19T00:00:00Z")
	sql, args, err := BuildMonitorWhere(from, to, "admin", "", "t-1", "u-1", "d-1", "high", "parser-a", "FAILED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, col := range []string{"tenant_id = ?", "user_id = ?", "doc_id = ?", "nats_queue = ?", "ingestor_name = ?", "status = ?"} {
		if !strings.Contains(sql, col) {
			t.Errorf("SQL missing %q: %s", col, sql)
		}
	}
	for _, want := range []string{"t-1", "u-1", "d-1", "high", "parser-a", "FAILED"} {
		if !containsArg(args, want) {
			t.Errorf("args missing %q: %v", want, args)
		}
	}
}

func TestBuildMonitorWhere_InjectionOnlyBoundNotEmbedded(t *testing.T) {
	from := mustTime(t, "2026-11-01T00:00:00Z")
	to := mustTime(t, "2026-11-19T00:00:00Z")
	payload := "x' OR 1=1 -- DROP TABLE ingestion_events_fact"
	sql, args, err := BuildMonitorWhere(from, to, "admin", "", payload, "All", "All", "All", "All", "All")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(strings.ToUpper(sql), "DROP") || strings.Contains(sql, "1=1") {
		t.Errorf("injection leaked into SQL text: %s", sql)
	}
	if !containsArg(args, payload) {
		t.Errorf("payload should be a bound arg, args=%v", args)
	}
}

func TestBuildMonitorWhere_TenantScopeForcesOwnTenant(t *testing.T) {
	from := mustTime(t, "2026-11-01T00:00:00Z")
	to := mustTime(t, "2026-11-19T00:00:00Z")
	// client tries another tenant while scoped to "my-tenant"
	sql, args, err := BuildMonitorWhere(from, to, "tenant", "my-tenant", "other-tenant", "All", "All", "All", "All", "All")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "tenant_id = ?") {
		t.Errorf("tenant scope must add tenant_id filter: %s", sql)
	}
	if !containsArg(args, "my-tenant") {
		t.Errorf("args must contain scoped tenant: %v", args)
	}
	if containsArg(args, "other-tenant") {
		t.Errorf("client tenant must be ignored under tenant scope: %v", args)
	}
}

func TestBuildMonitorWhere_TenantScopeAllStillForcesOwnTenant(t *testing.T) {
	from := mustTime(t, "2026-11-01T00:00:00Z")
	to := mustTime(t, "2026-11-19T00:00:00Z")
	sql, args, err := BuildMonitorWhere(from, to, "tenant", "my-tenant", "All", "All", "All", "All", "All", "All")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "tenant_id = ?") || !containsArg(args, "my-tenant") {
		t.Errorf("tenant scope must override All: sql=%s args=%v", sql, args)
	}
}

func TestBuildMonitorWhere_TenantScopeWithoutScopeTenantErrors(t *testing.T) {
	from := mustTime(t, "2026-11-01T00:00:00Z")
	to := mustTime(t, "2026-11-19T00:00:00Z")
	if _, _, err := BuildMonitorWhere(from, to, "tenant", "", "All", "All", "All", "All", "All", "All"); err == nil {
		t.Fatal("expected error when tenant scope has no scope tenant id")
	}
}

func TestBuildMonitorWhere_UnknownScopeErrors(t *testing.T) {
	from := mustTime(t, "2026-11-01T00:00:00Z")
	to := mustTime(t, "2026-11-19T00:00:00Z")
	if _, _, err := BuildMonitorWhere(from, to, "superadmin", "", "All", "All", "All", "All", "All", "All"); err == nil {
		t.Fatal("expected error for unknown scope")
	}
}

func TestBuildMonitorWhere_EmptyDimensionTreatedAsAll(t *testing.T) {
	from := mustTime(t, "2026-11-01T00:00:00Z")
	to := mustTime(t, "2026-11-19T00:00:00Z")
	sql, args, err := BuildMonitorWhere(from, to, "admin", "", "", "", "", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(sql, "status = ?") {
		t.Errorf("empty status must be treated as All: %s", sql)
	}
	if len(args) != 4 {
		t.Errorf("args len = %d, want 4 (no filter args)", len(args))
	}
}

// ---- whitelists ----

func TestResolveBreakdownColumn_Valid(t *testing.T) {
	cases := map[string]string{
		"status":           "status",
		"tenant":           "tenant_id",
		"user":             "user_id",
		"queue":            "nats_queue",
		"fail_rate_tenant": "tenant_id",
		"fail_rate_user":   "user_id",
	}
	for in, want := range cases {
		got, err := ResolveBreakdownColumn(in)
		if err != nil {
			t.Errorf("ResolveBreakdownColumn(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ResolveBreakdownColumn(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveBreakdownColumn_RejectsInjection(t *testing.T) {
	for _, in := range []string{"tenant_id; DROP TABLE x", "", "status OR 1=1", "TENANT"} {
		if _, err := ResolveBreakdownColumn(in); err == nil {
			t.Errorf("expected error for %q", in)
		}
	}
}

func TestResolveIntervalExpr_Valid(t *testing.T) {
	cases := map[string]string{
		"5m": "toStartOfFiveMinute(event_at)",
		"1h": "toStartOfHour(event_at)",
		"1d": "toStartOfDay(event_at)",
	}
	for in, want := range cases {
		got, err := ResolveIntervalExpr(in)
		if err != nil || got != want {
			t.Errorf("ResolveIntervalExpr(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestResolveIntervalExpr_RejectsInjection(t *testing.T) {
	for _, in := range []string{"1h;DROP", "", "1w", "NOW()"} {
		if _, err := ResolveIntervalExpr(in); err == nil {
			t.Errorf("expected error for %q", in)
		}
	}
}

func TestResolveSortColumn_Valid(t *testing.T) {
	cases := map[string]string{
		"event_at":      "event_at",
		"last_event_at": "event_at",
		"progress":      "progress",
		"status":        "status",
	}
	for in, want := range cases {
		got, err := ResolveSortColumn(in)
		if err != nil || got != want {
			t.Errorf("ResolveSortColumn(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestResolveSortColumn_RejectsInjection(t *testing.T) {
	for _, in := range []string{"event_at; DROP", "", "(SELECT 1)", "EVENT_AT"} {
		if _, err := ResolveSortColumn(in); err == nil {
			t.Errorf("expected error for %q", in)
		}
	}
}

func containsArg(args []any, want string) bool {
	for _, a := range args {
		if s, ok := a.(string); ok && s == want {
			return true
		}
	}
	return false
}
