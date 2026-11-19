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
	"strconv"
	"strings"

	"ragflow/internal/common"
	"ragflow/internal/engine/clickhouse"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// IngestMonitorQuerier is the read side of the ingestion monitoring store.
// It is an interface so handlers can be unit-tested without a live ClickHouse.
type IngestMonitorQuerier interface {
	QueryOverview(q clickhouse.MonitorQuery) (*clickhouse.MonitorOverview, error)
	QueryTimeseries(q clickhouse.MonitorQuery, interval string) ([]clickhouse.TimeseriesRow, error)
	QueryFilterOptions(q clickhouse.MonitorQuery) (*clickhouse.FilterOptions, error)
	QueryRunningTasks(q clickhouse.MonitorQuery, status string, limit int) ([]clickhouse.RunningTask, error)
	QueryBreakdown(q clickhouse.MonitorQuery, by string, limit int) ([]clickhouse.BreakdownRow, error)
	QueryFailRate(q clickhouse.MonitorQuery, by string, limit int) ([]clickhouse.BreakdownRow, error)
	QueryRunningOverview(q clickhouse.MonitorQuery, status string) (*clickhouse.RunningOverview, error)
	QueryEvents(q clickhouse.MonitorQuery, sortBy string, limit, offset int) ([]clickhouse.EventRow, error)
	CountEvents(q clickhouse.MonitorQuery) (uint64, error)
	QueryDocTimeline(q clickhouse.MonitorQuery, docID string, limit int) ([]clickhouse.TimelineRow, error)
}

type IngestMonitorHandler struct {
	querier IngestMonitorQuerier
}

// NewIngestMonitorHandler wires the real ClickHouse driver.
func NewIngestMonitorHandler() *IngestMonitorHandler {
	return &IngestMonitorHandler{querier: clickhouse.GetDriver()}
}

// NewIngestMonitorHandlerWith wires an injected querier (tests / alternate stores).
func NewIngestMonitorHandlerWith(q IngestMonitorQuerier) *IngestMonitorHandler {
	return &IngestMonitorHandler{querier: q}
}

// monitorIdentity returns (isAdmin, identityTenant). Superusers are global
// admins; everyone else is scoped to the tenant carried in the auth context.
func monitorIdentity(c *gin.Context) (bool, string) {
	user, code, _ := GetUser(c)
	if code != common.CodeSuccess || user == nil {
		return false, ""
	}
	isAdmin := user.IsSuperuser != nil && *user.IsSuperuser
	tenantID, _ := c.Get("tenant_id")
	tenant, _ := tenantID.(string)
	return isAdmin, tenant
}

// resolveScopeWithAudit resolves the admin/tenant scope and emits the audit
// warning when a tenant client requested a foreign tenant. The warning is
// returned so the decision remains observable/testable.
func resolveScopeWithAudit(c *gin.Context, isAdmin bool, identityTenant, clientTenant string) (scope, scopeTenant, warning string) {
	scope, scopeTenant, overridden, warning := clickhouse.ResolveMonitorScope(isAdmin, identityTenant, clientTenant)
	if overridden {
		common.WarnCtx(c.Request.Context(), warning,
			zap.String("actor_user_id", userIDOf(c)),
			zap.String("requested_tenant", clientTenant),
			zap.String("effective_tenant", scopeTenant),
		)
	}
	return scope, scopeTenant, warning
}

// buildMonitorQuery parses all shared filters and applies the admin/tenant
// authorisation decision. The client tenant is preserved in q.TenantID (for
// override detection); the effective scope is carried in q.Scope/ScopeTenant.
func buildMonitorQuery(c *gin.Context) (clickhouse.MonitorQuery, bool, error) {
	from, to, err := clickhouse.ParseMonitorWindow(c.Query("from"), c.Query("to"))
	if err != nil {
		return clickhouse.MonitorQuery{}, false, err
	}
	isAdmin, identityTenant := monitorIdentity(c)
	clientTenant := c.Query("tenant_id")
	scope, scopeTenant, _ := resolveScopeWithAudit(c, isAdmin, identityTenant, clientTenant)
	q := clickhouse.MonitorQuery{
		From: from, To: to,
		Scope: scope, ScopeTenant: scopeTenant,
		TenantID: clientTenant,
		UserID:   c.Query("user_id"),
		DocID:    c.Query("doc_id"),
		Queue:    c.Query("queue"),
		Ingestor: c.Query("ingestor"),
		Status:   c.Query("status"),
	}
	if q.UserID == "" {
		q.UserID = "All"
	}
	if q.DocID == "" {
		q.DocID = "All"
	}
	if q.Queue == "" {
		q.Queue = "All"
	}
	if q.Ingestor == "" {
		q.Ingestor = "All"
	}
	if q.Status == "" {
		q.Status = "All"
	}
	if q.TenantID == "" {
		q.TenantID = "All"
	}
	return q, isAdmin, nil
}

func userIDOf(c *gin.Context) string {
	user, _, _ := GetUser(c)
	if user == nil {
		return ""
	}
	return user.ID
}

func (h *IngestMonitorHandler) Overview(c *gin.Context) {
	q, _, err := buildMonitorQuery(c)
	if err != nil {
		common.RespondError(c, common.CodeParamError, err.Error())
		return
	}
	o, err := h.querier.QueryOverview(q)
	if err != nil {
		common.RespondError(c, common.CodeServerError, err.Error())
		return
	}
	common.RespondSuccess(c, o, "")
}

func (h *IngestMonitorHandler) Timeseries(c *gin.Context) {
	q, _, err := buildMonitorQuery(c)
	if err != nil {
		common.RespondError(c, common.CodeParamError, err.Error())
		return
	}
	interval := c.Query("interval")
	if interval == "" {
		interval = "1h"
	}
	// Validate the whitelist at the edge; never pass caller text to the store unchecked.
	if _, err := clickhouse.ResolveIntervalExpr(interval); err != nil {
		common.RespondError(c, common.CodeParamError, err.Error())
		return
	}
	rows, err := h.querier.QueryTimeseries(q, interval)
	if err != nil {
		common.RespondError(c, common.CodeServerError, err.Error())
		return
	}
	common.RespondSuccess(c, rows, "")
}

func (h *IngestMonitorHandler) Filters(c *gin.Context) {
	isAdmin, identityTenant := monitorIdentity(c)
	clientTenant := c.Query("tenant_id")
	scope, scopeTenant, _ := resolveScopeWithAudit(c, isAdmin, identityTenant, clientTenant)
	q := clickhouse.MonitorQuery{Scope: scope, ScopeTenant: scopeTenant}

	o, err := h.querier.QueryFilterOptions(q)
	if err != nil {
		common.RespondError(c, common.CodeServerError, err.Error())
		return
	}
	common.RespondSuccess(c, o, "")
}

func (h *IngestMonitorHandler) RunningTasks(c *gin.Context) {
	q, _, err := buildMonitorQuery(c)
	if err != nil {
		common.RespondError(c, common.CodeParamError, err.Error())
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	status := c.Query("status")
	if status == "" || strings.TrimSpace(status) == "" {
		status = "RUNNING"
	}
	rows, err := h.querier.QueryRunningTasks(q, status, limit)
	if err != nil {
		common.RespondError(c, common.CodeServerError, err.Error())
		return
	}
	common.RespondSuccess(c, rows, "")
}

func (h *IngestMonitorHandler) Breakdown(c *gin.Context) {
	q, _, err := buildMonitorQuery(c)
	if err != nil {
		common.RespondError(c, common.CodeParamError, err.Error())
		return
	}
	by := c.Query("by")
	if by == "" {
		by = "status"
	}
	if _, err := clickhouse.ResolveBreakdownColumn(by); err != nil {
		common.RespondError(c, common.CodeParamError, err.Error())
		return
	}
	limit := monitorLimit(c.Query("limit"), 12)
	rows, err := h.querier.QueryBreakdown(q, by, limit)
	if err != nil {
		common.RespondError(c, common.CodeServerError, err.Error())
		return
	}
	byCol, _ := clickhouse.ResolveBreakdownColumn(by)
	common.RespondSuccess(c, map[string]any{"by": byCol, "items": rows}, "")
}

func (h *IngestMonitorHandler) FailRate(c *gin.Context) {
	q, _, err := buildMonitorQuery(c)
	if err != nil {
		common.RespondError(c, common.CodeParamError, err.Error())
		return
	}
	by := c.Query("by")
	if by == "" {
		by = "tenant"
	}
	if by != "tenant" && by != "user" {
		common.RespondError(c, common.CodeParamError, "by must be tenant or user")
		return
	}
	limit := monitorLimit(c.Query("limit"), 12)
	rows, err := h.querier.QueryFailRate(q, by, limit)
	if err != nil {
		common.RespondError(c, common.CodeServerError, err.Error())
		return
	}
	common.RespondSuccess(c, map[string]any{"by": by, "items": rows}, "")
}

func (h *IngestMonitorHandler) RunningOverview(c *gin.Context) {
	q, _, err := buildMonitorQuery(c)
	if err != nil {
		common.RespondError(c, common.CodeParamError, err.Error())
		return
	}
	status := c.Query("status")
	if status == "" {
		status = "RUNNING"
	}
	o, err := h.querier.QueryRunningOverview(q, status)
	if err != nil {
		common.RespondError(c, common.CodeServerError, err.Error())
		return
	}
	common.RespondSuccess(c, o, "")
}

func monitorLimit(raw string, def int) int {
	n, _ := strconv.Atoi(raw)
	if n <= 0 || n > 200 {
		return def
	}
	return n
}

func (h *IngestMonitorHandler) Events(c *gin.Context) {
	q, _, err := buildMonitorQuery(c)
	if err != nil {
		common.RespondError(c, common.CodeParamError, err.Error())
		return
	}
	sortBy := c.Query("sort_by")
	if sortBy == "" {
		sortBy = "event_at"
	}
	if _, err := clickhouse.ResolveSortColumn(sortBy); err != nil {
		common.RespondError(c, common.CodeParamError, err.Error())
		return
	}
	limit := monitorLimit(c.Query("limit"), 100)
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * limit
	rows, err := h.querier.QueryEvents(q, sortBy, limit, offset)
	if err != nil {
		common.RespondError(c, common.CodeServerError, err.Error())
		return
	}
	total, err := h.querier.CountEvents(q)
	if err != nil {
		common.RespondError(c, common.CodeServerError, err.Error())
		return
	}
	common.RespondSuccess(c, map[string]any{"total": total, "page": page, "page_size": limit, "items": rows}, "")
}

func (h *IngestMonitorHandler) Timeline(c *gin.Context) {
	q, _, err := buildMonitorQuery(c)
	if err != nil {
		common.RespondError(c, common.CodeParamError, err.Error())
		return
	}
	docID := strings.TrimSpace(c.Query("doc_id"))
	if docID == "" {
		common.RespondError(c, common.CodeParamError, "doc_id is required")
		return
	}
	rows, err := h.querier.QueryDocTimeline(q, docID, monitorLimit(c.Query("limit"), 500))
	if err != nil {
		common.RespondError(c, common.CodeServerError, err.Error())
		return
	}
	common.RespondSuccess(c, map[string]any{"doc_id": docID, "items": rows}, "")
}
