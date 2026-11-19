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
)

// ---- ResolveMonitorScope: the auth decision (R4) ----

func TestResolveMonitorScope_AdminGlobal(t *testing.T) {
	scope, scopeTenant, overridden, warning := ResolveMonitorScope(true, "own-tenant", "other-tenant")
	if scope != "admin" {
		t.Errorf("scope = %q, want admin", scope)
	}
	if scopeTenant != "" {
		t.Errorf("admin scopeTenant should be empty, got %q", scopeTenant)
	}
	if overridden {
		t.Error("admin must not be treated as overridden")
	}
	if warning != "" {
		t.Errorf("admin should not produce tenant warning, got %q", warning)
	}
}

func TestResolveMonitorScope_TenantOwnTenant(t *testing.T) {
	scope, scopeTenant, overridden, warning := ResolveMonitorScope(false, "own-tenant", "own-tenant")
	if scope != "tenant" || scopeTenant != "own-tenant" {
		t.Errorf("got scope=%q tenant=%q", scope, scopeTenant)
	}
	if overridden || warning != "" {
		t.Errorf("own tenant must not warn, overridden=%v warning=%q", overridden, warning)
	}
}

func TestResolveMonitorScope_TenantOtherTenantOverridden(t *testing.T) {
	scope, scopeTenant, overridden, warning := ResolveMonitorScope(false, "own-tenant", "other-tenant")
	if scope != "tenant" || scopeTenant != "own-tenant" {
		t.Errorf("got scope=%q tenant=%q", scope, scopeTenant)
	}
	if !overridden {
		t.Error("cross-tenant client value must be overridden")
	}
	if warning == "" {
		t.Error("override must emit a structured warning for audit")
	}
	if !strings.Contains(warning, "other-tenant") || !strings.Contains(warning, "own-tenant") {
		t.Errorf("warning must contain both requested and effective tenant: %q", warning)
	}
}

func TestResolveMonitorScope_TenantAllOverridden(t *testing.T) {
	_, scopeTenant, overridden, warning := ResolveMonitorScope(false, "own-tenant", "All")
	if scopeTenant != "own-tenant" || !overridden || warning == "" {
		t.Errorf("All must be overridden to own tenant: tenant=%q overridden=%v warning=%q", scopeTenant, overridden, warning)
	}
}

func TestResolveMonitorScope_TenantEmptyClientUsesOwn(t *testing.T) {
	_, scopeTenant, overridden, warning := ResolveMonitorScope(false, "own-tenant", "")
	if scopeTenant != "own-tenant" || overridden || warning != "" {
		t.Errorf("empty client tenant should silently use own: tenant=%q overridden=%v warning=%q", scopeTenant, overridden, warning)
	}
}

func TestResolveMonitorScope_TenantWithNoIdentity(t *testing.T) {
	scope, scopeTenant, _, _ := ResolveMonitorScope(false, "", "All")
	if scope != "tenant" || scopeTenant != "" {
		t.Errorf("tenant with no identity must not fabricate a tenant: scope=%q tenant=%q", scope, scopeTenant)
	}
	// BuildMonitorWhere is the final guard: an empty scoped tenant errors.
	from := mustTime(t, "2026-11-01T00:00:00Z")
	to := mustTime(t, "2026-11-19T00:00:00Z")
	if _, _, err := BuildMonitorWhere(from, to, scope, scopeTenant, "All", "All", "All", "All", "All", "All"); err == nil {
		t.Fatal("expected BuildMonitorWhere to reject empty scope tenant")
	}
}

// ---- Query SQL builders (pure, injection-safe) ----

func TestBuildOverviewSQL_UsesWhereAndNoEventType(t *testing.T) {
	sql, err := buildOverviewSQL("(where)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"count()", "uniqExact(doc_id)", "countIf(status='FAILED')", "(where)"} {
		if !strings.Contains(sql, want) {
			t.Errorf("overview SQL missing %q: %s", want, sql)
		}
	}
	if strings.Contains(sql, "event_type") {
		t.Errorf("overview SQL must not reference removed event_type: %s", sql)
	}
}

func TestBuildTimeseriesSQL_UsesResolvedInterval(t *testing.T) {
	sql, err := buildTimeseriesSQL("(where)", "toStartOfHour(event_at)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "toStartOfHour(event_at) AS t") {
		t.Errorf("timeseries SQL missing interval expression: %s", sql)
	}
	if !strings.Contains(sql, "status") || !strings.Contains(sql, "count() AS cnt") {
		t.Errorf("timeseries must group by status with cnt: %s", sql)
	}
}

func TestBuildBreakdownSQL_UsesResolvedColumn(t *testing.T) {
	sql, args, err := buildBreakdownSQL("(where)", "tenant_id", 12)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "tenant_id") || !strings.Contains(sql, "GROUP BY tenant_id") {
		t.Errorf("breakdown SQL wrong: %s", sql)
	}
	if len(args) != 1 || args[0] != 12 {
		t.Errorf("limit must be a bound arg, got %v", args)
	}
}

func TestBuildRunningTaskSQL_UsesLatestStatusPerDoc(t *testing.T) {
	sql, err := buildRunningTaskSQL("(where)", "event_at")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"GROUP BY doc_id", "argMax(status, event_at)", "running_seconds"} {
		if !strings.Contains(sql, want) {
			t.Errorf("running task SQL missing %q: %s", want, sql)
		}
	}
	// started_at must be derived from begin_time, not formatted in SQL to a display string
	if strings.Contains(sql, "concat(") {
		t.Errorf("running task SQL must not format display strings: %s", sql)
	}
}
