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

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/engine/clickhouse"
	"ragflow/internal/entity"

	"github.com/gin-gonic/gin"
)

// fakeIngestMonitorQuerier records the query it was called with.
type fakeIngestMonitorQuerier struct {
	lastQuery   clickhouse.MonitorQuery
	overview    *clickhouse.MonitorOverview
	filters     *clickhouse.FilterOptions
	timeseries  []clickhouse.TimeseriesRow
	running     []clickhouse.RunningTask
	breakdown   []clickhouse.BreakdownRow
	runOverview *clickhouse.RunningOverview
	events      []clickhouse.EventRow
	eventsTotal uint64
	timeline    []clickhouse.TimelineRow
	err         error
}

func (f *fakeIngestMonitorQuerier) QueryOverview(q clickhouse.MonitorQuery) (*clickhouse.MonitorOverview, error) {
	f.lastQuery = q
	return f.overview, f.err
}
func (f *fakeIngestMonitorQuerier) QueryTimeseries(q clickhouse.MonitorQuery, interval string) ([]clickhouse.TimeseriesRow, error) {
	f.lastQuery = q
	return f.timeseries, f.err
}
func (f *fakeIngestMonitorQuerier) QueryFilterOptions(q clickhouse.MonitorQuery) (*clickhouse.FilterOptions, error) {
	f.lastQuery = q
	return f.filters, f.err
}
func (f *fakeIngestMonitorQuerier) QueryRunningTasks(q clickhouse.MonitorQuery, status string, limit int) ([]clickhouse.RunningTask, error) {
	f.lastQuery = q
	return f.running, f.err
}
func (f *fakeIngestMonitorQuerier) QueryBreakdown(q clickhouse.MonitorQuery, by string, limit int) ([]clickhouse.BreakdownRow, error) {
	f.lastQuery = q
	return f.breakdown, f.err
}
func (f *fakeIngestMonitorQuerier) QueryFailRate(q clickhouse.MonitorQuery, by string, limit int) ([]clickhouse.BreakdownRow, error) {
	f.lastQuery = q
	return f.breakdown, f.err
}
func (f *fakeIngestMonitorQuerier) QueryRunningOverview(q clickhouse.MonitorQuery, status string) (*clickhouse.RunningOverview, error) {
	f.lastQuery = q
	return f.runOverview, f.err
}
func (f *fakeIngestMonitorQuerier) QueryEvents(q clickhouse.MonitorQuery, sortBy string, limit, offset int) ([]clickhouse.EventRow, error) {
	f.lastQuery = q
	return f.events, f.err
}
func (f *fakeIngestMonitorQuerier) CountEvents(q clickhouse.MonitorQuery) (uint64, error) {
	f.lastQuery = q
	return f.eventsTotal, f.err
}
func (f *fakeIngestMonitorQuerier) QueryDocTimeline(q clickhouse.MonitorQuery, docID string, limit int) ([]clickhouse.TimelineRow, error) {
	f.lastQuery = q
	return f.timeline, f.err
}

func newMonitorCtx(user *entity.User, tenantID string, query string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Set("user", user)
	c.Set("tenant_id", tenantID)
	url := "/"
	if query != "" {
		url = "/?" + query
	}
	c.Request = httptest.NewRequest(http.MethodGet, url, nil)
	return c, rec
}

func decodeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, rec.Body.String())
	}
	return m
}

func TestIngestMonitorOverview_TenantScopedIgnoresClientTenant(t *testing.T) {
	super := false
	user := &entity.User{ID: "u-1", IsSuperuser: &super}
	fake := &fakeIngestMonitorQuerier{overview: &clickhouse.MonitorOverview{TotalEvents: 42}}
	h := NewIngestMonitorHandlerWith(fake)

	q := "from=2026-11-01T00:00:00Z&to=2026-11-19T00:00:00Z&tenant_id=other-tenant"
	c, rec := newMonitorCtx(user, "my-tenant", q)
	h.Overview(c)

	if fake.lastQuery.Scope != "tenant" {
		t.Errorf("scope = %q, want tenant", fake.lastQuery.Scope)
	}
	if fake.lastQuery.ScopeTenant != "my-tenant" {
		t.Errorf("scopeTenant = %q, want my-tenant", fake.lastQuery.ScopeTenant)
	}
	if fake.lastQuery.TenantID != "other-tenant" {
		t.Errorf("client tenant should be preserved in query for override logic, got %q", fake.lastQuery.TenantID)
	}
	env := decodeEnvelope(t, rec)
	if env["code"] != float64(common.CodeSuccess) {
		t.Errorf("code = %v, body=%s", env["code"], rec.Body.String())
	}
}

func TestIngestMonitorOverview_AdminGlobal(t *testing.T) {
	super := true
	user := &entity.User{ID: "u-1", IsSuperuser: &super}
	fake := &fakeIngestMonitorQuerier{overview: &clickhouse.MonitorOverview{TotalEvents: 100}}
	h := NewIngestMonitorHandlerWith(fake)

	q := "from=2026-11-01T00:00:00Z&to=2026-11-19T00:00:00Z&tenant_id=All"
	c, _ := newMonitorCtx(user, "my-tenant", q)
	h.Overview(c)

	if fake.lastQuery.Scope != "admin" {
		t.Errorf("scope = %q, want admin", fake.lastQuery.Scope)
	}
	if fake.lastQuery.ScopeTenant != "" {
		t.Errorf("admin scopeTenant should be empty, got %q", fake.lastQuery.ScopeTenant)
	}
}

func TestIngestMonitorOverview_InvalidTimeReturnsError(t *testing.T) {
	super := false
	user := &entity.User{ID: "u-1", IsSuperuser: &super}
	fake := &fakeIngestMonitorQuerier{overview: &clickhouse.MonitorOverview{}}
	h := NewIngestMonitorHandlerWith(fake)

	q := "from=bad&to=2026-11-19T00:00:00Z"
	c, rec := newMonitorCtx(user, "my-tenant", q)
	h.Overview(c)

	env := decodeEnvelope(t, rec)
	if env["code"] == float64(common.CodeSuccess) {
		t.Errorf("expected error code for invalid time, body=%s", rec.Body.String())
	}
}

func TestIngestMonitorFilters_TenantScopePassedToQuerier(t *testing.T) {
	super := false
	user := &entity.User{ID: "u-1", IsSuperuser: &super}
	fake := &fakeIngestMonitorQuerier{filters: &clickhouse.FilterOptions{
		Tenants: []clickhouse.NameID{{ID: "my-tenant"}},
	}}
	h := NewIngestMonitorHandlerWith(fake)

	c, rec := newMonitorCtx(user, "my-tenant", "")
	h.Filters(c)

	// The handler must resolve the caller's identity into a tenant scope and
	// pass it to the querier (the store then narrows tenants/users/docs).
	if fake.lastQuery.Scope != "tenant" || fake.lastQuery.ScopeTenant != "my-tenant" {
		t.Errorf("scope not passed to querier: scope=%q tenant=%q", fake.lastQuery.Scope, fake.lastQuery.ScopeTenant)
	}
	env := decodeEnvelope(t, rec)
	if env["code"] != float64(common.CodeSuccess) {
		t.Errorf("code=%v body=%s", env["code"], rec.Body.String())
	}
}

func TestIngestMonitorFilters_TenantCrossTenantRequestStillScoped(t *testing.T) {
	super := false
	user := &entity.User{ID: "u-1", IsSuperuser: &super}
	fake := &fakeIngestMonitorQuerier{filters: &clickhouse.FilterOptions{}}
	h := NewIngestMonitorHandlerWith(fake)

	// A tenant asking for another tenant must still be scoped to its own.
	c, _ := newMonitorCtx(user, "my-tenant", "tenant_id=other-tenant")
	h.Filters(c)

	if fake.lastQuery.Scope != "tenant" || fake.lastQuery.ScopeTenant != "my-tenant" {
		t.Errorf("cross-tenant request not scoped down: scope=%q tenant=%q", fake.lastQuery.Scope, fake.lastQuery.ScopeTenant)
	}
}

func TestIngestMonitorFilters_AdminSeesAllTenants(t *testing.T) {
	super := true
	user := &entity.User{ID: "u-1", IsSuperuser: &super}
	fake := &fakeIngestMonitorQuerier{filters: &clickhouse.FilterOptions{
		Tenants: []clickhouse.NameID{{ID: "t-1"}, {ID: "t-2"}},
	}}
	h := NewIngestMonitorHandlerWith(fake)

	c, rec := newMonitorCtx(user, "t-1", "")
	h.Filters(c)

	if fake.lastQuery.Scope != "admin" || fake.lastQuery.ScopeTenant != "" {
		t.Errorf("admin scope not passed: scope=%q tenant=%q", fake.lastQuery.Scope, fake.lastQuery.ScopeTenant)
	}
	env := decodeEnvelope(t, rec)
	data := env["data"].(map[string]any)
	tenants := data["tenants"].([]any)
	if len(tenants) != 2 {
		t.Fatalf("admin should see all tenants, got %d", len(tenants))
	}
}
func TestIngestMonitorTimeseries_BadIntervalReturnsError(t *testing.T) {
	super := false
	user := &entity.User{ID: "u-1", IsSuperuser: &super}
	fake := &fakeIngestMonitorQuerier{}
	h := NewIngestMonitorHandlerWith(fake)

	q := "from=2026-11-01T00:00:00Z&to=2026-11-19T00:00:00Z&interval=1h%3BDROP"
	c, rec := newMonitorCtx(user, "my-tenant", q)
	h.Timeseries(c)

	env := decodeEnvelope(t, rec)
	if env["code"] == float64(common.CodeSuccess) {
		t.Errorf("expected error for injected interval, body=%s", rec.Body.String())
	}
}

func TestIngestMonitorRunningTasks_ScopedToTenant(t *testing.T) {
	super := false
	user := &entity.User{ID: "u-1", IsSuperuser: &super}
	fake := &fakeIngestMonitorQuerier{running: []clickhouse.RunningTask{}}
	h := NewIngestMonitorHandlerWith(fake)

	q := "from=2026-11-01T00:00:00Z&to=2026-11-19T00:00:00Z&status=RUNNING"
	c, rec := newMonitorCtx(user, "my-tenant", q)
	h.RunningTasks(c)

	if fake.lastQuery.Scope != "tenant" || fake.lastQuery.ScopeTenant != "my-tenant" {
		t.Errorf("running tasks not scoped: scope=%q tenant=%q", fake.lastQuery.Scope, fake.lastQuery.ScopeTenant)
	}
	env := decodeEnvelope(t, rec)
	if env["code"] != float64(common.CodeSuccess) {
		t.Errorf("code=%v body=%s", env["code"], rec.Body.String())
	}
}

func TestIngestMonitorQueryFromContext_DefaultWindow(t *testing.T) {
	// ensure a missing from/to yields an error rather than an unbounded scan
	super := false
	user := &entity.User{ID: "u-1", IsSuperuser: &super}
	fake := &fakeIngestMonitorQuerier{}
	h := NewIngestMonitorHandlerWith(fake)

	c, rec := newMonitorCtx(user, "my-tenant", "")
	h.Overview(c)

	env := decodeEnvelope(t, rec)
	if env["code"] == float64(common.CodeSuccess) {
		t.Errorf("missing time window must error, body=%s", rec.Body.String())
	}
}

var _ = time.Time{}

func TestIngestMonitorBreakdown_BadByReturnsError(t *testing.T) {
	super := false
	user := &entity.User{ID: "u-1", IsSuperuser: &super}
	fake := &fakeIngestMonitorQuerier{}
	h := NewIngestMonitorHandlerWith(fake)
	q := "from=2026-11-01T00:00:00Z&to=2026-11-19T00:00:00Z&by=tenant%3BDROP"
	c, rec := newMonitorCtx(user, "my-tenant", q)
	h.Breakdown(c)
	env := decodeEnvelope(t, rec)
	if env["code"] == float64(common.CodeSuccess) {
		t.Errorf("expected error for injected by, body=%s", rec.Body.String())
	}
}

func TestIngestMonitorBreakdown_ScopedToTenant(t *testing.T) {
	super := false
	user := &entity.User{ID: "u-1", IsSuperuser: &super}
	fake := &fakeIngestMonitorQuerier{breakdown: []clickhouse.BreakdownRow{}}
	h := NewIngestMonitorHandlerWith(fake)
	q := "from=2026-11-01T00:00:00Z&to=2026-11-19T00:00:00Z&by=status"
	c, _ := newMonitorCtx(user, "my-tenant", q)
	h.Breakdown(c)
	if fake.lastQuery.Scope != "tenant" || fake.lastQuery.ScopeTenant != "my-tenant" {
		t.Errorf("breakdown not scoped: %q %q", fake.lastQuery.Scope, fake.lastQuery.ScopeTenant)
	}
}

func TestIngestMonitorRunningOverview_Scoped(t *testing.T) {
	super := false
	user := &entity.User{ID: "u-1", IsSuperuser: &super}
	fake := &fakeIngestMonitorQuerier{runOverview: &clickhouse.RunningOverview{RunningTasks: 5}}
	h := NewIngestMonitorHandlerWith(fake)
	q := "from=2026-11-01T00:00:00Z&to=2026-11-19T00:00:00Z"
	c, rec := newMonitorCtx(user, "my-tenant", q)
	h.RunningOverview(c)
	if fake.lastQuery.Scope != "tenant" || fake.lastQuery.ScopeTenant != "my-tenant" {
		t.Errorf("running overview not scoped: %q %q", fake.lastQuery.Scope, fake.lastQuery.ScopeTenant)
	}
	env := decodeEnvelope(t, rec)
	if env["code"] != float64(common.CodeSuccess) {
		t.Errorf("code=%v body=%s", env["code"], rec.Body.String())
	}
}

func TestIngestMonitorEvents_Scoped(t *testing.T) {
	super := false
	user := &entity.User{ID: "u-1", IsSuperuser: &super}
	fake := &fakeIngestMonitorQuerier{events: []clickhouse.EventRow{}}
	h := NewIngestMonitorHandlerWith(fake)
	q := "from=2026-11-01T00:00:00Z&to=2026-11-19T00:00:00Z&sort_by=event_at"
	c, rec := newMonitorCtx(user, "my-tenant", q)
	h.Events(c)
	if fake.lastQuery.Scope != "tenant" || fake.lastQuery.ScopeTenant != "my-tenant" {
		t.Errorf("events not scoped: %q %q", fake.lastQuery.Scope, fake.lastQuery.ScopeTenant)
	}
	env := decodeEnvelope(t, rec)
	if env["code"] != float64(common.CodeSuccess) {
		t.Errorf("code=%v body=%s", env["code"], rec.Body.String())
	}
}

func TestIngestMonitorEvents_BadSortReturnsError(t *testing.T) {
	super := false
	user := &entity.User{ID: "u-1", IsSuperuser: &super}
	fake := &fakeIngestMonitorQuerier{}
	h := NewIngestMonitorHandlerWith(fake)
	q := "from=2026-11-01T00:00:00Z&to=2026-11-19T00:00:00Z&sort_by=event_at%3BDROP"
	c, rec := newMonitorCtx(user, "my-tenant", q)
	h.Events(c)
	env := decodeEnvelope(t, rec)
	if env["code"] == float64(common.CodeSuccess) {
		t.Errorf("expected error for injected sort, body=%s", rec.Body.String())
	}
}

func TestIngestMonitorTimeline_RequiresDocID(t *testing.T) {
	super := false
	user := &entity.User{ID: "u-1", IsSuperuser: &super}
	fake := &fakeIngestMonitorQuerier{}
	h := NewIngestMonitorHandlerWith(fake)
	q := "from=2026-11-01T00:00:00Z&to=2026-11-19T00:00:00Z"
	c, rec := newMonitorCtx(user, "my-tenant", q)
	h.Timeline(c)
	env := decodeEnvelope(t, rec)
	if env["code"] == float64(common.CodeSuccess) {
		t.Errorf("timeline without doc_id must error, body=%s", rec.Body.String())
	}
}

func TestIngestMonitorEvents_ResponseIncludesTotal(t *testing.T) {
	super := false
	user := &entity.User{ID: "u-1", IsSuperuser: &super}
	fake := &fakeIngestMonitorQuerier{
		events:      []clickhouse.EventRow{},
		eventsTotal: 137,
	}
	h := NewIngestMonitorHandlerWith(fake)
	q := "from=2026-11-01T00:00:00Z&to=2026-11-19T00:00:00Z"
	c, rec := newMonitorCtx(user, "my-tenant", q)
	h.Events(c)
	env := decodeEnvelope(t, rec)
	data := env["data"].(map[string]any)
	if _, ok := data["total"]; !ok {
		t.Fatalf("events response must include total for pagination: %s", rec.Body.String())
	}
	if data["total"] != float64(137) {
		t.Errorf("total = %v, want 137", data["total"])
	}
}

func TestResolveScopeWithAudit_CrossTenantWarning(t *testing.T) {
	user := &entity.User{ID: "u-1"}
	c, _ := newMonitorCtx(user, "my-tenant", "")
	_, _, warning := resolveScopeWithAudit(c, false, "my-tenant", "other-tenant")
	if warning == "" {
		t.Fatal("cross-tenant request must produce an audit warning")
	}
}
