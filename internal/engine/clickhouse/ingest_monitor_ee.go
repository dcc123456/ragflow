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
	"errors"
	"fmt"
	"strings"
	"time"
)

// ParseMonitorWindow parses RFC3339 from/to bounds and normalises them to UTC.
// An empty bound, malformed value, or reversed range is rejected.
func ParseMonitorWindow(from, to string) (time.Time, time.Time, error) {
	if strings.TrimSpace(from) == "" || strings.TrimSpace(to) == "" {
		return time.Time{}, time.Time{}, errors.New("from and to are required")
	}
	fromT, err := time.Parse(time.RFC3339, from)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid from: %w", err)
	}
	toT, err := time.Parse(time.RFC3339, to)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid to: %w", err)
	}
	if fromT.After(toT) {
		return time.Time{}, time.Time{}, errors.New("from must not be after to")
	}
	if toT.Sub(fromT) > 90*24*time.Hour {
		return time.Time{}, time.Time{}, errors.New("window must not exceed 90 days")
	}
	return fromT.UTC(), toT.UTC(), nil
}

const (
	monitorScopeAdmin  = "admin"
	monitorScopeTenant = "tenant"
)

// normaliseFilterValue maps "All" and empty strings to the sentinel "All".
func normaliseFilterValue(v string) string {
	if v == "" || v == "All" {
		return "All"
	}
	return v
}

// BuildMonitorWhere builds the WHERE clause (without the WHERE keyword) for
// ingestion_events_fact queries. Every caller-supplied value is bound with a
// `?` placeholder — no value is ever concatenated into the SQL text.
//
// It enforces two invariants:
//   - "All"/empty dimension values produce no predicate (no filtering).
//   - under tenant scope, tenant_id is ALWAYS filtered to scopeTenantID,
//     ignoring any client-supplied tenant (which is why tenant scope requires
//     a non-empty scopeTenantID).
//
// args are returned in placeholder order: from, to, fromDate, toDate, then the
// scoped tenant (when applicable), then each set dimension.
func BuildMonitorWhere(from, to time.Time, scope, scopeTenantID, tenantID, userID, docID, queue, ingestor, status string) (string, []any, error) {
	clauses := []string{
		"event_at >= ?",
		"event_at <= ?",
		"event_date >= ?",
		"event_date <= ?",
	}
	fromUTC, toUTC := from.UTC(), to.UTC()
	args := []any{fromUTC, toUTC,
		time.Date(fromUTC.Year(), fromUTC.Month(), fromUTC.Day(), 0, 0, 0, 0, time.UTC),
		time.Date(toUTC.Year(), toUTC.Month(), toUTC.Day(), 0, 0, 0, 0, time.UTC),
	}

	switch scope {
	case monitorScopeAdmin:
		// no forced tenant predicate
	case monitorScopeTenant:
		if strings.TrimSpace(scopeTenantID) == "" {
			return "", nil, errors.New("tenant scope requires a scope tenant id")
		}
		clauses = append(clauses, "tenant_id = ?")
		args = append(args, scopeTenantID)
	default:
		return "", nil, fmt.Errorf("unknown monitor scope %q", scope)
	}

	dims := []struct {
		column string
		value  string
	}{
		{"tenant_id", normaliseFilterValue(tenantID)},
		{"user_id", normaliseFilterValue(userID)},
		{"doc_id", normaliseFilterValue(docID)},
		{"nats_queue", normaliseFilterValue(queue)},
		{"ingestor_name", normaliseFilterValue(ingestor)},
		{"status", normaliseFilterValue(status)},
	}
	// Under tenant scope the tenant dimension is already forced above; skip the
	// client-supplied tenant entirely.
	for _, d := range dims {
		if scope == monitorScopeTenant && d.column == "tenant_id" {
			continue
		}
		if d.value == "All" {
			continue
		}
		clauses = append(clauses, d.column+" = ?")
		args = append(args, d.value)
	}

	return strings.Join(clauses, " AND "), args, nil
}

var monitorBreakdownColumns = map[string]string{
	"status":           "status",
	"tenant":           "tenant_id",
	"user":             "user_id",
	"queue":            "nats_queue",
	"fail_rate_tenant": "tenant_id",
	"fail_rate_user":   "user_id",
}

// ResolveBreakdownColumn maps a logical breakdown key to a whitelisted column.
func ResolveBreakdownColumn(by string) (string, error) {
	col, ok := monitorBreakdownColumns[by]
	if !ok {
		return "", fmt.Errorf("unsupported breakdown %q", by)
	}
	return col, nil
}

var monitorIntervalExprs = map[string]string{
	"5m": "toStartOfFiveMinute(event_at)",
	"1h": "toStartOfHour(event_at)",
	"1d": "toStartOfDay(event_at)",
}

// ResolveIntervalExpr maps a logical interval to a whitelisted ClickHouse
// time-bucketing expression.
func ResolveIntervalExpr(interval string) (string, error) {
	expr, ok := monitorIntervalExprs[interval]
	if !ok {
		return "", fmt.Errorf("unsupported interval %q", interval)
	}
	return expr, nil
}

var monitorSortColumns = map[string]string{
	"event_at":      "event_at",
	"last_event_at": "event_at",
	"progress":      "progress",
	"status":        "status",
}

// ResolveSortColumn maps a logical sort key to a whitelisted column.
func ResolveSortColumn(sortBy string) (string, error) {
	col, ok := monitorSortColumns[sortBy]
	if !ok {
		return "", fmt.Errorf("unsupported sort key %q", sortBy)
	}
	return col, nil
}

// ResolveMonitorScope makes the admin/tenant authorisation decision.
//
//   - admin: global scope, no tenant predicate, client tenant is irrelevant.
//   - tenant: scope is forced to identityTenant. A client-supplied tenant that
//     differs is silently overridden and reported via overridden+warning so the
//     handler can emit a structured audit warning (it never returns 403, which
//     would confirm the existence of another tenant).
//
// An empty client tenant means "not specified" and uses the identity tenant
// without a warning. An explicit "All" is an attempt to view global data and is
// treated as an override (scoped down + audit warning), since a tenant must
// never see other tenants' data even when they ask for everything.
func ResolveMonitorScope(isAdmin bool, identityTenant, clientTenant string) (scope, scopeTenant string, overridden bool, warning string) {
	if isAdmin {
		return monitorScopeAdmin, "", false, ""
	}
	client := strings.TrimSpace(clientTenant)
	if client == "" || client == identityTenant {
		return monitorScopeTenant, identityTenant, false, ""
	}
	return monitorScopeTenant, identityTenant, true,
		fmt.Sprintf("tenant scope override: client requested tenant %q but effective tenant is %q", client, identityTenant)
}

func buildOverviewSQL(where string) (string, error) {
	return `SELECT count(), uniqExact(doc_id), countIf(status='FAILED'), ` +
		`uniqExact(tenant_id), uniqExact(user_id) ` +
		`FROM ingestion_events_fact WHERE ` + where, nil
}

func buildTimeseriesSQL(where, intervalExpr string) (string, error) {
	if strings.TrimSpace(intervalExpr) == "" {
		return "", errors.New("interval expression is required")
	}
	return `SELECT ` + intervalExpr + ` AS t, status, count() AS cnt ` +
		`FROM ingestion_events_fact WHERE ` + where +
		` GROUP BY t, status ORDER BY t`, nil
}

func buildBreakdownSQL(where, column string, limit int) (string, []any, error) {
	if strings.TrimSpace(column) == "" {
		return "", nil, errors.New("breakdown column is required")
	}
	sql := `SELECT ` + column + `, count() AS cnt FROM ingestion_events_fact WHERE ` + where +
		` GROUP BY ` + column + ` ORDER BY cnt DESC LIMIT ?`
	return sql, []any{limit}, nil
}

func buildRunningTaskSQL(where, sortColumn string) (string, error) {
	return buildRunningTaskSQLInner(where, sortColumn, false)
}

// buildRunningTaskSQLInner selects one row per doc with its latest status. The
// status filter is applied in an OUTER WHERE on the aliased latest_status so it
// filters on the doc's current status (not any historic event), matching the
// dashboard's HAVING argMax(status, event_at).
func buildRunningTaskSQLInner(where, sortColumn string, filterStatus bool) (string, error) {
	if strings.TrimSpace(sortColumn) == "" {
		return "", errors.New("sort column is required")
	}
	inner := `SELECT doc_id, ` +
		`argMax(tenant_id, event_at) AS tenant_id, ` +
		`argMax(user_id, event_at) AS user_id, ` +
		`argMax(tenant_name, event_at) AS tenant_name, ` +
		`argMax(user_name, event_at) AS user_name, ` +
		`argMax(nats_queue, event_at) AS queue, ` +
		`argMax(worker, event_at) AS worker, ` +
		`argMax(ingestor_name, event_at) AS ingestor, ` +
		`argMax(progress, event_at) AS progress, ` +
		`argMax(begin_time, event_at) AS started_at, ` +
		`dateDiff('second', argMax(begin_time, event_at), now()) AS running_seconds, ` +
		`max(event_at) AS last_event_at, ` +
		`argMax(status, event_at) AS latest_status ` +
		`FROM ingestion_events_fact WHERE ` + where + ` GROUP BY doc_id`
	outerWhere := ""
	if filterStatus {
		outerWhere = " WHERE latest_status = ?"
	}
	return `SELECT doc_id, tenant_id, user_id, tenant_name, user_name, queue, worker, ` +
		`ingestor, progress, started_at, running_seconds, last_event_at, latest_status AS status ` +
		`FROM (` + inner + `)` + outerWhere +
		` ORDER BY ` + sortColumn + ` DESC LIMIT ?`, nil
}

// ---- DTOs: display-agnostic data contracts shared with the frontend ----

// StatusCount is a raw status distribution row (no derived "failed" bucket).
type StatusCount struct {
	Status string `json:"status"`
	Count  uint64 `json:"count"`
}

// MonitorOverview is the aggregate header stats.
type MonitorOverview struct {
	TotalEvents  uint64        `json:"total_events"`
	DistinctDocs uint64        `json:"distinct_docs"`
	FailedEvents uint64        `json:"failed_events"`
	Tenants      uint64        `json:"tenants"`
	Users        uint64        `json:"users"`
	StatusCounts []StatusCount `json:"status_counts"`
}

// TimeseriesRow is a single (bucket, status, count) observation. No series/points wrapper.
type TimeseriesRow struct {
	T      time.Time `json:"t"`
	Status string    `json:"status"`
	Count  uint64    `json:"count"`
}

// NameID is a neutral data-dictionary entry (id + name, no "value/label").
type NameID struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// FilterOptions holds all dropdown candidate values.
type FilterOptions struct {
	Tenants   []NameID `json:"tenants"`
	Users     []NameID `json:"users"`
	Docs      []NameID `json:"docs"`
	Queues    []string `json:"queues"`
	Ingestors []string `json:"ingestors"`
	Statuses  []string `json:"statuses"`
}

// RunningTask is a task row in the running monitor. Durations are raw seconds,
// timestamps are RFC3339 — no preformatted display strings.
type RunningTask struct {
	DocID          string     `json:"doc_id"`
	TenantID       string     `json:"tenant_id"`
	TenantName     string     `json:"tenant_name"`
	UserID         string     `json:"user_id"`
	UserName       string     `json:"user_name"`
	Queue          string     `json:"queue"`
	Worker         string     `json:"worker"`
	Ingestor       string     `json:"ingestor"`
	Progress       float64    `json:"progress"`
	StartedAt      *time.Time `json:"started_at"`
	LastEventAt    time.Time  `json:"last_event_at"`
	RunningSeconds int64      `json:"running_seconds"`
	Status         string     `json:"status"`
}

// MonitorQuery carries all normalised inputs for a monitoring query.
type MonitorQuery struct {
	From, To    time.Time
	Scope       string
	ScopeTenant string
	TenantID    string
	UserID      string
	DocID       string
	Queue       string
	Ingestor    string
	Status      string
}

// where builds the WHERE clause and args for this query.
func (q MonitorQuery) where() (string, []any, error) {
	return BuildMonitorWhere(q.From, q.To, q.Scope, q.ScopeTenant, q.TenantID, q.UserID, q.DocID, q.Queue, q.Ingestor, q.Status)
}

// ---- driver query methods ----

// QueryOverview returns the six aggregate header stats plus the raw status
// distribution. The status distribution lets the client derive its own buckets
// instead of being locked into the server's "failed" definition.
func (d *Driver) QueryOverview(q MonitorQuery) (*MonitorOverview, error) {
	where, args, err := q.where()
	if err != nil {
		return nil, err
	}
	sql, err := buildOverviewSQL(where)
	if err != nil {
		return nil, err
	}
	row := d.conn.QueryRow(d.ctx, sql, args...)
	var o MonitorOverview
	if err := row.Scan(&o.TotalEvents, &o.DistinctDocs, &o.FailedEvents, &o.Tenants, &o.Users); err != nil {
		return nil, fmt.Errorf("scan overview: %w", err)
	}

	statusSQL := `SELECT status, count() AS cnt FROM ingestion_events_fact WHERE ` + where +
		` GROUP BY status ORDER BY cnt DESC`
	rows, err := d.conn.Query(d.ctx, statusSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("query status counts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var sc StatusCount
		if err := rows.Scan(&sc.Status, &sc.Count); err != nil {
			return nil, fmt.Errorf("scan status count: %w", err)
		}
		o.StatusCounts = append(o.StatusCounts, sc)
	}
	return &o, nil
}

// QueryTimeseries returns (bucket, status, count) rows. interval is a logical
// key resolved through a whitelist; no caller text is interpolated.
func (d *Driver) QueryTimeseries(q MonitorQuery, interval string) ([]TimeseriesRow, error) {
	expr, err := ResolveIntervalExpr(interval)
	if err != nil {
		return nil, err
	}
	where, args, err := q.where()
	if err != nil {
		return nil, err
	}
	sql, err := buildTimeseriesSQL(where, expr)
	if err != nil {
		return nil, err
	}
	rows, err := d.conn.Query(d.ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query timeseries: %w", err)
	}
	defer rows.Close()
	var out []TimeseriesRow
	for rows.Next() {
		var r TimeseriesRow
		if err := rows.Scan(&r.T, &r.Status, &r.Count); err != nil {
			return nil, fmt.Errorf("scan timeseries: %w", err)
		}
		out = append(out, r)
	}
	return out, nil
}

// QueryFilterOptions returns dropdown candidates. For a tenant-scoped caller,
// tenants/users/docs are narrowed to that tenant so names and filenames cannot
// leak across tenants; queue/ingestor/status remain global enumerations.
func (d *Driver) QueryFilterOptions(q MonitorQuery) (*FilterOptions, error) {
	var o FilterOptions
	// Tenant-scoped callers must only see dictionary entries from their own
	// tenant, otherwise user names and document filenames leak across tenants.
	tenantWhere := ""
	var tenantArgs []any
	if q.Scope == monitorScopeTenant {
		if strings.TrimSpace(q.ScopeTenant) == "" {
			return nil, errors.New("tenant scope requires a scope tenant id")
		}
		tenantWhere = " WHERE tenant_id = ?"
		tenantArgs = []any{q.ScopeTenant}
	}

	nameQueries := []struct {
		sql    string
		args   []any
		target *[]NameID
	}{
		{`SELECT tenant_id, any(tenant_name) FROM ingestion_events_fact` + tenantWhere + ` GROUP BY tenant_id ORDER BY tenant_id`, tenantArgs, &o.Tenants},
		{`SELECT user_id, any(user_name) FROM ingestion_events_fact` + tenantWhere + ` GROUP BY user_id ORDER BY user_id`, tenantArgs, &o.Users},
		{`SELECT doc_id, any(filename) FROM ingestion_events_fact` + tenantWhere + ` GROUP BY doc_id ORDER BY doc_id LIMIT 1000`, tenantArgs, &o.Docs},
	}
	for _, nq := range nameQueries {
		rows, err := d.conn.Query(d.ctx, nq.sql, nq.args...)
		if err != nil {
			return nil, fmt.Errorf("query filter options: %w", err)
		}
		for rows.Next() {
			var n NameID
			if err := rows.Scan(&n.ID, &n.Name); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan filter option: %w", err)
			}
			*nq.target = append(*nq.target, n)
		}
		rows.Close()
	}
	strQueries := []struct {
		sql    string
		args   []any
		target *[]string
	}{
		{`SELECT DISTINCT nats_queue FROM ingestion_events_fact` + tenantWhere + ` ORDER BY nats_queue`, tenantArgs, &o.Queues},
		{`SELECT DISTINCT ingestor_name FROM ingestion_events_fact` + tenantWhere + ` ORDER BY ingestor_name`, tenantArgs, &o.Ingestors},
		{`SELECT DISTINCT status FROM ingestion_events_fact` + tenantWhere + ` ORDER BY status`, tenantArgs, &o.Statuses},
	}
	for _, sq := range strQueries {
		rows, err := d.conn.Query(d.ctx, sq.sql, sq.args...)
		if err != nil {
			return nil, fmt.Errorf("query filter options: %w", err)
		}
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan filter option: %w", err)
			}
			*sq.target = append(*sq.target, v)
		}
		rows.Close()
	}
	return &o, nil
}

// QueryRunningTasks returns one row per doc with its latest status, scoped by
// the requested status (typically RUNNING). sortColumn is whitelisted.
func (d *Driver) QueryRunningTasks(q MonitorQuery, status string, limit int) ([]RunningTask, error) {
	// Running tasks are always ordered by the outer projection's last_event_at.
	const sortCol = "last_event_at"
	innerQ := q
	innerQ.Status = "All" // status is applied to each doc's LATEST status in the outer WHERE
	where, args, err := innerQ.where()
	if err != nil {
		return nil, err
	}
	filterStatus := strings.TrimSpace(status) != "" && status != "All"
	sql, err := buildRunningTaskSQLInner(where, sortCol, filterStatus)
	if err != nil {
		return nil, err
	}
	if filterStatus {
		args = append(args, status)
	}
	args = append(args, limit)
	rows, err := d.conn.Query(d.ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query running tasks: %w", err)
	}
	defer rows.Close()
	var out []RunningTask
	for rows.Next() {
		var r RunningTask
		var startedAt *time.Time
		if err := rows.Scan(&r.DocID, &r.TenantID, &r.UserID, &r.TenantName, &r.UserName,
			&r.Queue, &r.Worker, &r.Ingestor, &r.Progress, &startedAt, &r.RunningSeconds,
			&r.LastEventAt, &r.Status); err != nil {
			return nil, fmt.Errorf("scan running task: %w", err)
		}
		r.StartedAt = startedAt
		out = append(out, r)
	}
	return out, nil
}

// BreakdownRow is a generic grouped-count row. The group key is carried in a
// neutral field; fail-rate queries additionally fill failed/total/fail_rate.
type BreakdownRow struct {
	Key      string  `json:"key"`
	Count    uint64  `json:"count"`
	Failed   uint64  `json:"failed"`
	Total    uint64  `json:"total"`
	FailRate float64 `json:"fail_rate"`
}

// RunningOverview holds the aggregate header stats of the running monitor.
type RunningOverview struct {
	RunningTasks   uint64  `json:"running_tasks"`
	RunningTenants uint64  `json:"running_tenants"`
	RunningQueues  uint64  `json:"running_queues"`
	AvgProgress    float64 `json:"avg_progress"`
}

// ---- Batch 7: breakdown / fail-rate / running-overview ----

// QueryBreakdown returns grouped counts for a logical dimension (status/tenant/user/queue).
func (d *Driver) QueryBreakdown(q MonitorQuery, by string, limit int) ([]BreakdownRow, error) {
	col, err := ResolveBreakdownColumn(by)
	if err != nil {
		return nil, err
	}
	where, args, err := q.where()
	if err != nil {
		return nil, err
	}
	sql, sqlArgs, err := buildBreakdownSQL(where, col, limit)
	if err != nil {
		return nil, err
	}
	args = append(args, sqlArgs...)
	rows, err := d.conn.Query(d.ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query breakdown: %w", err)
	}
	defer rows.Close()
	var out []BreakdownRow
	for rows.Next() {
		var r BreakdownRow
		if err := rows.Scan(&r.Key, &r.Count); err != nil {
			return nil, fmt.Errorf("scan breakdown: %w", err)
		}
		out = append(out, r)
	}
	return out, nil
}

// QueryFailRate returns per-group failure rate with raw failed/total counts.
// A single whitelisted aggregate computes failed/total/fail_rate in ClickHouse
// and applies the ordering and limit there, avoiding two scans plus in-Go sort.
func (d *Driver) QueryFailRate(q MonitorQuery, by string, limit int) ([]BreakdownRow, error) {
	col, err := ResolveBreakdownColumn(by)
	if err != nil {
		return nil, err
	}
	where, args, err := q.where()
	if err != nil {
		return nil, err
	}
	sql := `SELECT ` + col + `,
		countIf(status='FAILED') AS failed,
		count() AS total,
		if(count() > 0, countIf(status='FAILED') / count(), 0) AS fail_rate
		FROM ingestion_events_fact WHERE ` + where +
		` GROUP BY ` + col + ` ORDER BY fail_rate DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.conn.Query(d.ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query fail rate: %w", err)
	}
	defer rows.Close()
	var out []BreakdownRow
	for rows.Next() {
		var r BreakdownRow
		if err := rows.Scan(&r.Key, &r.Failed, &r.Total, &r.FailRate); err != nil {
			return nil, fmt.Errorf("scan fail rate: %w", err)
		}
		out = append(out, r)
	}
	return out, nil
}

// QueryRunningOverview returns aggregate stats of docs currently in the given status.
func (d *Driver) QueryRunningOverview(q MonitorQuery, status string) (*RunningOverview, error) {
	innerQ := q
	innerQ.Status = "All"
	where, args, err := innerQ.where()
	if err != nil {
		return nil, err
	}
	cte := `(SELECT doc_id, argMax(tenant_id, event_at) AS tenant_id, ` +
		`argMax(nats_queue, event_at) AS queue, argMax(progress, event_at) AS progress, ` +
		`argMax(status, event_at) AS latest_status FROM ingestion_events_fact WHERE ` + where +
		` GROUP BY doc_id)`
	filter := ""
	filterArgs := []any{}
	if strings.TrimSpace(status) != "" && status != "All" {
		filter = " WHERE latest_status = ?"
		filterArgs = append(filterArgs, status)
	}
	sql := `SELECT count(), uniqExact(tenant_id), uniqExact(queue), if(isNaN(avg(progress)), 0, avg(progress)) FROM ` + cte + filter
	allArgs := append(append([]any{}, args...), filterArgs...)
	var o RunningOverview
	if err := d.conn.QueryRow(d.ctx, sql, allArgs...).Scan(
		&o.RunningTasks, &o.RunningTenants, &o.RunningQueues, &o.AvgProgress); err != nil {
		return nil, fmt.Errorf("scan running overview: %w", err)
	}
	return &o, nil
}

// EventRow is one ingestion event in the paged detail table. operation JSON is
// flattened into ui_label / has_error so the client never parses raw JSON.
type EventRow struct {
	EventAt    time.Time `json:"event_at"`
	Status     string    `json:"status"`
	TenantID   string    `json:"tenant_id"`
	TenantName string    `json:"tenant_name"`
	UserID     string    `json:"user_id"`
	UserName   string    `json:"user_name"`
	DocID      string    `json:"doc_id"`
	DatasetID  string    `json:"dataset_id"`
	Filename   string    `json:"filename"`
	UILabel    string    `json:"ui_label"`
	HasError   bool      `json:"has_error"`
}

// TimelineRow is one event in a document's lifecycle timeline.
type TimelineRow struct {
	EventAt    time.Time `json:"event_at"`
	Status     string    `json:"status"`
	Worker     string    `json:"worker"`
	Progress   float64   `json:"progress"`
	DurationMS int64     `json:"duration_ms"`
	ErrorInfo  string    `json:"error_info"`
	UILabel    string    `json:"ui_label"`
	Terminal   bool      `json:"terminal"`
}

// ---- Batch 8: events detail + doc timeline ----

// QueryEvents returns a page of ingestion events. operation JSON is flattened
// into ui_label/has_error via JSONExtract so the data contract carries plain
// fields, never a raw JSON blob.
func (d *Driver) QueryEvents(q MonitorQuery, sortBy string, limit, offset int) ([]EventRow, error) {
	col, err := ResolveSortColumn(sortBy)
	if err != nil {
		return nil, err
	}
	where, args, err := q.where()
	if err != nil {
		return nil, err
	}
	sql := `SELECT event_at, status, tenant_id, tenant_name, user_id, user_name, doc_id, dataset_id, filename, ` +
		`ifNull(JSONExtractString(operation, 'event_label'), '') AS ui_label, ` +
		`JSONExtractBool(operation, 'has_error') AS has_error ` +
		`FROM ingestion_events_fact WHERE ` + where +
		` ORDER BY ` + col + ` DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := d.conn.Query(d.ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()
	var out []EventRow
	for rows.Next() {
		var r EventRow
		if err := rows.Scan(&r.EventAt, &r.Status, &r.TenantID, &r.TenantName, &r.UserID,
			&r.UserName, &r.DocID, &r.DatasetID, &r.Filename, &r.UILabel, &r.HasError); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		out = append(out, r)
	}
	return out, nil
}

// QueryDocTimeline returns one document's lifecycle events in chronological
// order. Like the source dashboard, it intentionally applies NO time filter —
// it is keyed only by doc_id plus the caller's dimension filters.
func (d *Driver) QueryDocTimeline(q MonitorQuery, docID string, limit int) ([]TimelineRow, error) {
	// Build dimension filters without the time window.
	where, args, err := BuildMonitorWhere(
		time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		q.Scope, q.ScopeTenant, q.TenantID, q.UserID, q.DocID, q.Queue, q.Ingestor, q.Status)
	if err != nil {
		return nil, err
	}
	// Force the requested doc regardless of the (possibly All) doc filter.
	sql := `SELECT event_at, status, worker, progress, duration_ms, error_info, ` +
		`ifNull(JSONExtractString(operation, 'event_label'), '') AS ui_label, ` +
		`JSONExtractBool(operation, 'terminal') AS terminal ` +
		`FROM ingestion_events_fact WHERE (` + where + `) AND doc_id = ? ORDER BY event_at LIMIT ?`
	args = append(args, docID, limit)
	rows, err := d.conn.Query(d.ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query doc timeline: %w", err)
	}
	defer rows.Close()
	var out []TimelineRow
	for rows.Next() {
		var r TimelineRow
		if err := rows.Scan(&r.EventAt, &r.Status, &r.Worker, &r.Progress, &r.DurationMS,
			&r.ErrorInfo, &r.UILabel, &r.Terminal); err != nil {
			return nil, fmt.Errorf("scan timeline: %w", err)
		}
		out = append(out, r)
	}
	return out, nil
}

// CountEvents returns the number of events matching the query scope/filters.
// It powers pagination totals for the events endpoint.
func (d *Driver) CountEvents(q MonitorQuery) (uint64, error) {
	where, args, err := q.where()
	if err != nil {
		return 0, err
	}
	sql := `SELECT count() FROM ingestion_events_fact WHERE ` + where
	var total uint64
	if err := d.conn.QueryRow(d.ctx, sql, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("count events: %w", err)
	}
	return total, nil
}
