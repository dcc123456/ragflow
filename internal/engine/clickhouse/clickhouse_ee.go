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
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"ragflow/internal/common"
	"strings"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"go.uber.org/zap"
)

var (
	globalDriver *Driver
	once         sync.Once
)

type Driver struct {
	conn        driver.Conn
	config      *clickhouse.Options
	channel     chan interface{}
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	batchSize   int
	syncEventID int64
}

// GetDriver gets global stats client instance
func GetDriver() *Driver {
	return globalDriver
}

func (d *Driver) CollectModelUsage(modelUsage *common.ModelUsage) error {
	//if modelUsage != nil {
	//	common.Info("CollectModelUsage", zap.Any("modelUsage", modelUsage.String()))
	//}
	select {
	case d.channel <- modelUsage:
		return nil
	default:
		return fmt.Errorf("channel is full, current capacity: %d, backlog: %d", cap(d.channel), len(d.channel))
	}
}

func Init(ctx context.Context, cancel context.CancelFunc, host string, port int, user, password, database string) error {
	var err error
	once.Do(func() {
		address := fmt.Sprintf("%s:%d", host, port)

		globalDriver = &Driver{
			ctx:       ctx,
			cancel:    cancel,
			wg:        sync.WaitGroup{},
			channel:   make(chan interface{}, 128*1024),
			batchSize: 128 * 1024,
		}
		globalDriver.config = &clickhouse.Options{
			Addr: []string{address},
			Auth: clickhouse.Auth{
				Database: database,
				Username: user,
				Password: password,
			},
			DialTimeout:     5 * time.Second,
			MaxOpenConns:    10,
			MaxIdleConns:    5,
			ConnMaxLifetime: time.Hour,
		}

		globalDriver.conn, err = clickhouse.Open(globalDriver.config)
		if err != nil {
			return
		}

		if err = globalDriver.conn.Ping(ctx); err != nil {
			return
		}

		if err = globalDriver.InitDB(); err != nil {
			return
		}

		globalDriver.wg.Add(1)
		go globalDriver.consumer()
	})
	return err
}

func (d *Driver) SaveOperationLog(operationLog *common.OperationLog) error {
	select {
	case d.channel <- operationLog:
		return nil
	default:
		return fmt.Errorf("channel is full, current capacity: %d, backlog: %d", cap(d.channel), len(d.channel))
	}
}

func (d *Driver) Status() (map[string]interface{}, error) {
	stats := d.conn.Stats()
	version, err := d.conn.ServerVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get server version: %w", err)
	}
	return map[string]interface{}{
		"max_open_conns": stats.MaxOpenConns,
		"max_idle_conns": stats.MaxIdleConns,
		"open":           stats.Open,
		"idle":           stats.Idle,
		"server_version": version.String(),
	}, nil
}

func (d *Driver) Ping(ctx context.Context) error {
	return d.conn.Ping(ctx)
}

func Close() error {
	return globalDriver.conn.Close()
}

// consumerIdleInterval is the pause between drain attempts when the channel is
// empty. Without it the consumer spins calling drainAndFlush in a tight loop,
// pegging a CPU core at 100% while nothing is queued.
const consumerIdleInterval = 500 * time.Millisecond

func (d *Driver) consumer() {
	defer d.wg.Done()

	for {
		select {
		case <-d.ctx.Done():
			common.Info("Stopping stats collector consumer...")
			d.drainAndFlush()
			common.Info("Stats collector consumer stopped...")
			return

		default:
			d.drainAndFlush()
			// When the channel is empty drainAndFlush returns immediately; pause so
			// we do not busy-loop at 100% CPU.
			time.Sleep(consumerIdleInterval)
		}
	}
}

func (d *Driver) drainAndFlush() {
	usageBatch, logBatch, ingestBatch, syncBatch := d.drainChannel()

	if len(usageBatch)+len(logBatch)+len(ingestBatch)+len(syncBatch) == 0 {
		return
	}

	d.flushModelUsage(usageBatch)
	d.flushOperationLog(logBatch)
	d.flushIngestionEvents(ingestBatch)
	d.flushSyncEvents(syncBatch)
}

func (d *Driver) drainChannel() ([]*common.ModelUsage, []*common.OperationLog, []*common.IngestionLog, []*SyncEvent) {
	var usageBatch []*common.ModelUsage
	var logBatch []*common.OperationLog
	var ingestBatch []*common.IngestionLog
	var syncBatch []*SyncEvent

	for {
		select {
		case message, ok := <-d.channel:
			if !ok {
				// channel is closed
				return usageBatch, logBatch, ingestBatch, syncBatch
			}
			switch record := message.(type) {
			case *common.ModelUsage:
				usageBatch = append(usageBatch, record)
			case *common.OperationLog:
				logBatch = append(logBatch, record)
			case *common.IngestionLog:
				ingestBatch = append(ingestBatch, record)
			case *SyncEvent:
				syncBatch = append(syncBatch, record)
			default:
				common.Error("Invalid record type", errors.New("invalid type"))
			}

			if len(usageBatch)+len(logBatch)+len(ingestBatch)+len(syncBatch) >= d.batchSize {
				common.Info(fmt.Sprintf("Reach batch size limit, flush batch, batchSize: %d", d.batchSize))
				return usageBatch, logBatch, ingestBatch, syncBatch
			}

		default:
			// Drain channel, flush batch
			if len(usageBatch)+len(logBatch)+len(ingestBatch)+len(syncBatch) > 0 {
				common.Info("Drain channel, flush batch", zap.Int("batchSize", len(usageBatch)+len(logBatch)+len(ingestBatch)+len(syncBatch)))
			}
			return usageBatch, logBatch, ingestBatch, syncBatch
		}
	}
}

func (d *Driver) flushModelUsage(batch []*common.ModelUsage) {
	if len(batch) == 0 {
		return
	}

	batchInsert, err := d.conn.PrepareBatch(d.ctx, `
        INSERT INTO model_usage_fact (
            start_at,
            event_date,
            event_hour,
            user_id,
            user_email,
            tenant_id,
            tenant_email,
            group_id,
            group_name,
            app_id,
            app_name,
            session_id,
            provider_name,
            model_name,
            instance_id,
            api_key,
            request_id,
            type,
            response_time_ms,
            error_message,
            input_tokens,
            output_tokens,
            total_tokens
        )
    `)

	if err != nil {
		common.Error("Prepare batch insert failed", err)
		return
	}

	for _, record := range batch {
		eventDate := record.StartAt
		eventHour := time.Date(
			eventDate.Year(), eventDate.Month(), eventDate.Day(),
			eventDate.Hour(), 0, 0, 0, eventDate.Location(),
		)

		err = batchInsert.Append(
			record.StartAt,
			eventDate,
			eventHour,
			record.UserID,
			record.UserEmail,
			record.TenantID,
			record.TenantEmail,
			record.GroupID,
			record.GroupName,
			record.AppID,
			record.AppName,
			record.SessionID,
			record.ProviderName,
			record.ModelName,
			record.InstanceID,
			record.APIKey,
			record.RequestID,
			record.Type,
			record.ResponseTimeMS,
			record.ErrorMessage,
			record.InputTokens,
			record.OutputTokens,
			record.TotalTokens,
		)
		if err != nil {
			common.Error("Append record to batch failed", err)
		}
	}

	if err = batchInsert.Send(); err != nil {
		common.Error("Batch insert failed", err)
		return
	}

	common.Info("Batch insert success", zap.Int("records", len(batch)))
}

func (d *Driver) flushOperationLog(batch []*common.OperationLog) {
	if len(batch) == 0 {
		return
	}

	batchInsert, err := d.conn.PrepareBatch(d.ctx, `
        INSERT INTO operation_audit_log (
            event_time,
            user_id,
            ip_address,
            operation,
            api_path,
            http_method,
            resource_type,
            resource_id,
            request_params,
            extra_metadata,
            error_code,
            message,
            duration_ms
        )
    `)

	if err != nil {
		common.Error("Prepare batch insert failed", err)
		return
	}

	for _, record := range batch {
		resources, marshalErr := json.Marshal(record.Resources)
		if marshalErr != nil {
			common.Error("Encode audit resources failed", marshalErr)
			continue
		}
		message := ""
		if record.Error != nil {
			message = record.Error.Error()
		}
		resourceType, resourceID := auditResource(record.Resources)
		err = batchInsert.Append(
			record.EventTime,
			record.UserID,
			record.IPAddress,
			record.Operation,
			record.APIPath,
			record.HTTPMethod,
			resourceType,
			resourceID,
			record.RequestParams,
			string(resources),
			record.Code,
			message,
			record.DurationMS,
		)
		if err != nil {
			common.Error("Append record to batch failed", err)
		}
	}

	if err = batchInsert.Send(); err != nil {
		common.Error("Batch insert failed", err)
		return
	}

	common.Info("Batch insert success", zap.Int("records", len(batch)))
}

func auditResource(resources map[string]any) (string, string) {
	for _, resourceType := range []string{"chunk", "document", "session", "version", "agent", "search", "memory", "dataset"} {
		if id, ok := resources[resourceType+"_id"].(string); ok && id != "" {
			return resourceType, id
		}
	}
	return "", ""
}

func (d *Driver) InitDB() error {
	// Every statement returned by getCreateSQLs uses IF NOT EXISTS, so this call
	// is idempotent: on a fresh database it creates everything, and on an
	// upgraded database it converges any analytics table that is missing
	// (e.g. ingestion_events_fact on a deployment that already had
	// model_usage_fact). The previous early-return on model_usage_fact's
	// presence skipped getCreateSQLs entirely, silently dropping ingestion
	// events on every upgraded database.
	common.Info("Start create tables and views (idempotent)...")

	statements := getCreateSQLs()
	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}

		err := d.conn.Exec(d.ctx, stmt)
		if err != nil {
			common.Error("Execute statement failed", err)
			if strings.Contains(stmt, "CREATE TABLE IF NOT EXISTS operation_audit_log") {
				return fmt.Errorf("initialize audit table: %w", err)
			}
			if stmt == createSyncEventsSQL {
				return fmt.Errorf("initialize sync events table: %w", err)
			}
		}
	}

	common.Info("Create tables and views success")
	return nil
}

type UserStatsResult struct {
	StatDate          time.Time `json:"stat_date"`
	UserID            string    `json:"user_id"`
	UserEmail         string    `json:"user_email"`
	GroupID           string    `json:"group_id"`
	GroupName         string    `json:"group_name"`
	AppID             string    `json:"app_id"`
	AppName           string    `json:"app_name"`
	ProviderName      string    `json:"provider_name"`
	ModelName         string    `json:"model_name"`
	RequestCount      uint64    `json:"request_count"`
	TotalInputTokens  uint64    `json:"total_input_tokens"`
	TotalOutputTokens uint64    `json:"total_output_tokens"`
	TotalTokens       uint64    `json:"total_tokens"`
	AvgResponseTime   float64   `json:"avg_response_time_ms"`
	ErrorCount        uint64    `json:"error_count"`
	SuccessRate       float64   `json:"success_rate"`
}

func (d *Driver) UserStats(userID string, startDateStr, endDateStr string, granularity string) ([]UserStatsResult, error) {
	startDate, err := parseStatsDate(startDateStr, "start")
	if err != nil {
		return nil, err
	}
	endDate, err := parseStatsDate(endDateStr, "end")
	if err != nil {
		return nil, err
	}

	var dateFormat string
	switch granularity {
	case "hour":
		dateFormat = "toStartOfHour(start_at)"
	case "month":
		dateFormat = "toStartOfMonth(start_at)"
	default: // day
		dateFormat = "toDate(start_at)"
	}

	query := fmt.Sprintf(`
        SELECT
            %s as stat_date,
            user_id,
            any(user_email) as user_email,
            any(group_id) as group_id,
            any(group_name) as group_name,
            any(app_id) as app_id,
            any(app_name) as app_name,
            provider_name,
            model_name,
            count() as request_count,
            sum(input_tokens) as total_input_tokens,
            sum(output_tokens) as total_output_tokens,
            sum(total_tokens) as total_tokens,
            avg(response_time_ms) as avg_response_time_ms,
            countIf(error_message != '') as error_count,
            (count() - countIf(error_message != '')) / count() as success_rate
        FROM model_usage_fact
        WHERE user_id = ?
            AND event_date >= ?
            AND event_date <= ?
        GROUP BY
            stat_date,
            user_id,
            provider_name,
            model_name
        ORDER BY stat_date DESC, total_tokens DESC
    `, dateFormat)

	rows, err := d.conn.Query(d.ctx, query, userID, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to query user stats failed: %w", err)
	}
	defer rows.Close()

	var results []UserStatsResult
	for rows.Next() {
		var r UserStatsResult
		err = rows.Scan(
			&r.StatDate,
			&r.UserID,
			&r.UserEmail,
			&r.GroupID,
			&r.GroupName,
			&r.AppID,
			&r.AppName,
			&r.ProviderName,
			&r.ModelName,
			&r.RequestCount,
			&r.TotalInputTokens,
			&r.TotalOutputTokens,
			&r.TotalTokens,
			&r.AvgResponseTime,
			&r.ErrorCount,
			&r.SuccessRate,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user stats result: %w", err)
		}
		results = append(results, r)
	}

	return results, nil
}

// UsersStats aggregates model_usage_fact per user across all tenants within
// the date range and returns the top users ranked by total tokens
// (request_count as tiebreak). Start/end dates accept both "2006-01-02" and
// "2006-01-02 15:04:05"; the day boundaries drive the event_date comparison.
func (d *Driver) UsersStats(startDateStr, endDateStr string, top int) ([]UserStatsResult, error) {
	if top <= 0 {
		return nil, fmt.Errorf("top must be positive, got %d", top)
	}
	startDate, err := parseStatsDate(startDateStr, "start")
	if err != nil {
		return nil, err
	}
	endDate, err := parseStatsDate(endDateStr, "end")
	if err != nil {
		return nil, err
	}

	query := `
        SELECT
            user_id,
            any(user_email) as user_email,
            count() as request_count,
            sum(input_tokens) as total_input_tokens,
            sum(output_tokens) as total_output_tokens,
            sum(total_tokens) as total_tokens,
            avg(response_time_ms) as avg_response_time_ms,
            countIf(error_message != '') as error_count,
            (count() - countIf(error_message != '')) / count() as success_rate
        FROM model_usage_fact
        WHERE event_date >= ?
            AND event_date <= ?
        GROUP BY user_id
        ORDER BY total_tokens DESC, request_count DESC
        LIMIT ?`

	rows, err := d.conn.Query(d.ctx, query, startDate, endDate, top)
	if err != nil {
		return nil, fmt.Errorf("failed to query users stats: %w", err)
	}
	defer rows.Close()

	var results []UserStatsResult
	for rows.Next() {
		var r UserStatsResult
		err = rows.Scan(
			&r.UserID,
			&r.UserEmail,
			&r.RequestCount,
			&r.TotalInputTokens,
			&r.TotalOutputTokens,
			&r.TotalTokens,
			&r.AvgResponseTime,
			&r.ErrorCount,
			&r.SuccessRate,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan users stats result: %w", err)
		}
		results = append(results, r)
	}

	return results, nil
}

// parseStatsDate accepts both the date-only and the datetime form so handlers
// can forward their defaults (which include the " 00:00:00"/" 23:59:59"
// suffixes) without extra trimming.
func parseStatsDate(value, name string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02 15:04:05", value); err == nil {
		return t, nil
	}
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid %s date %q: %w", name, value, err)
	}
	return t, nil
}

// StatsSummary totals model_usage_fact across all users and tenants within
// the date range into a single row: request count, token sums, average
// response time, error count and success rate. Start/end dates accept both
// "2006-01-02" and "2006-01-02 15:04:05".
func (d *Driver) StatsSummary(startDateStr, endDateStr string) (*UserStatsResult, error) {
	startDate, err := parseStatsDate(startDateStr, "start")
	if err != nil {
		return nil, err
	}
	endDate, err := parseStatsDate(endDateStr, "end")
	if err != nil {
		return nil, err
	}

	query := `
        SELECT
            count() as request_count,
            sum(input_tokens) as total_input_tokens,
            sum(output_tokens) as total_output_tokens,
            sum(total_tokens) as total_tokens,
            avg(response_time_ms) as avg_response_time_ms,
            countIf(error_message != '') as error_count,
            (count() - countIf(error_message != '')) / count() as success_rate
        FROM model_usage_fact
        WHERE event_date >= ?
            AND event_date <= ?`

	var summary UserStatsResult
	err = d.conn.QueryRow(d.ctx, query, startDate, endDate).Scan(
		&summary.RequestCount,
		&summary.TotalInputTokens,
		&summary.TotalOutputTokens,
		&summary.TotalTokens,
		&summary.AvgResponseTime,
		&summary.ErrorCount,
		&summary.SuccessRate,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query stats summary: %w", err)
	}

	return &summary, nil
}

type OperationLogStats struct {
	EventID       string    `json:"event_id"`
	EventTime     time.Time `json:"event_time"`
	UserID        string    `json:"user_id"`
	UserName      string    `json:"user_name"`
	IPAddress     string    `json:"ip_address"`
	Operation     string    `json:"operation"`
	APIPath       string    `json:"api_path"`
	HTTPMethod    string    `json:"http_method"`
	ResourceType  string    `json:"resource_type"`
	ResourceID    string    `json:"resource_id"`
	ResourceName  string    `json:"resource_name"`
	RequestParams string    `json:"request_params"`
	ErrorCode     uint16    `json:"error_code"`
	Message       string    `json:"message"`
	DurationMS    *uint64   `json:"duration_ms"`
}

func (d *Driver) OperationLogStats(userID string, startTime, endTime time.Time, limit, offset int64) ([]OperationLogStats, error) {
	query := `
    SELECT
        event_id,
        event_time,
        user_id,
        user_name,
        ip_address,
        operation,
        api_path,
        http_method,
        resource_type,
        resource_id,
        resource_name,
        request_params,
        error_code,
        message,
        duration_ms,
    FROM operation_audit_log
    WHERE event_time >= ? AND event_time <= ? AND user_id = ?
    ORDER BY event_time DESC
    LIMIT ? OFFSET ?
    `

	args := []interface{}{startTime, endTime, userID, limit, offset}

	rows, err := d.conn.Query(d.ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query operation log stats failed: %w", err)
	}
	defer rows.Close()

	var results []OperationLogStats
	for rows.Next() {
		var log OperationLogStats
		err = rows.Scan(
			&log.EventID,
			&log.EventTime,
			&log.UserID,
			&log.UserName,
			&log.IPAddress,
			&log.Operation,
			&log.APIPath,
			&log.HTTPMethod,
			&log.ResourceType,
			&log.ResourceID,
			&log.ResourceName,
			&log.RequestParams,
			&log.ErrorCode,
			&log.Message,
			&log.DurationMS,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user stats result: %w", err)
		}
		results = append(results, log)
	}

	return results, nil
}

func (d *Driver) tableExists(tableName string) (bool, error) {
	var count uint64
	err := d.conn.QueryRow(d.ctx, `
        SELECT count()
        FROM system.tables
        WHERE database = currentDatabase()
          AND name = ?
    `, tableName).Scan(&count)

	if err != nil {
		return false, err
	}

	return count > 0, nil
}

func getCreateSQLs() []string {
	return []string{
		`
CREATE TABLE IF NOT EXISTS model_usage_fact
(
    start_at DateTime64(3) CODEC(Delta(8), ZSTD(1)),
    event_date Date DEFAULT toDate(start_at) CODEC(ZSTD(1)),
    event_hour DateTime DEFAULT toStartOfHour(start_at) CODEC(ZSTD(1)),
    user_id LowCardinality(String) CODEC(ZSTD(1)),
    user_email LowCardinality(String) CODEC(ZSTD(1)),
    tenant_id LowCardinality(String) CODEC(ZSTD(1)),
    tenant_email LowCardinality(String) CODEC(ZSTD(1)),
    group_id LowCardinality(String) CODEC(ZSTD(1)),
    group_name LowCardinality(String) CODEC(ZSTD(1)),
    app_id LowCardinality(String) CODEC(ZSTD(1)),
    app_name LowCardinality(String) CODEC(ZSTD(1)),
    session_id String CODEC(ZSTD(1)),
    provider_name LowCardinality(String) CODEC(ZSTD(1)),
    model_name LowCardinality(String) CODEC(ZSTD(1)),
    instance_id String CODEC(ZSTD(1)),
    api_key String CODEC(ZSTD(1)),
    request_id String CODEC(ZSTD(1)),
    type LowCardinality(String) CODEC(ZSTD(1)),
    response_time_ms UInt64 CODEC(ZSTD(1)),
    error_message String CODEC(ZSTD(1)),
    input_tokens UInt64 CODEC(ZSTD(1)),
    output_tokens UInt64 CODEC(ZSTD(1)),
    total_tokens UInt64 CODEC(ZSTD(1)),

    extra_metadata String,

    INDEX idx_user_id user_id TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_tenant_id tenant_id TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_group_id group_id TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(event_date)
ORDER BY (tenant_id, user_id, event_date, start_at)
SETTINGS index_granularity = 8192`,
		`
CREATE TABLE IF NOT EXISTS model_usage_user_daily
(
    stat_date Date,
    tenant_id LowCardinality(String),
    user_id LowCardinality(String),
    user_email LowCardinality(String),
    group_id LowCardinality(String),
    group_name LowCardinality(String),
    app_id LowCardinality(String),
    app_name LowCardinality(String),
    provider_name LowCardinality(String),
    model_name LowCardinality(String),
    request_count UInt64,
    total_input_tokens UInt64,
    total_output_tokens UInt64,
    total_tokens UInt64,
    avg_response_time_ms Float64,
    error_count UInt64,
    success_rate Float64
)
ENGINE = SummingMergeTree()
PARTITION BY toYYYYMM(stat_date)
ORDER BY (tenant_id, stat_date, user_id)
SETTINGS index_granularity = 8192`,
		`
CREATE TABLE IF NOT EXISTS model_usage_model_daily
(
    stat_date Date,
    tenant_id LowCardinality(String),
    provider_name LowCardinality(String),
    model_name LowCardinality(String),
    instance_id String,
    request_count UInt64,
    total_input_tokens UInt64,
    total_output_tokens UInt64,
    total_tokens UInt64,
    avg_response_time_ms Float64,
    unique_users UInt64
)
ENGINE = SummingMergeTree()
PARTITION BY toYYYYMM(stat_date)
ORDER BY (tenant_id, stat_date, provider_name, model_name)
SETTINGS index_granularity = 8192`,
		`
CREATE MATERIALIZED VIEW IF NOT EXISTS mv_model_usage_user_daily
TO model_usage_user_daily
AS SELECT
    toDate(start_at) as stat_date,
    tenant_id,
    user_id,
    any(user_email) as user_email,
    any(group_id) as group_id,
    any(group_name) as group_name,
    any(app_id) as app_id,
    any(app_name) as app_name,
    provider_name,
    model_name,
    count() as request_count,
    sum(input_tokens) as total_input_tokens,
    sum(output_tokens) as total_output_tokens,
    sum(total_tokens) as total_tokens,
    avg(response_time_ms) as avg_response_time_ms,
    countIf(error_message != '') as error_count,
    (count() - countIf(error_message != '')) / count() as success_rate
FROM model_usage_fact
GROUP BY
    toDate(start_at),
    tenant_id,
    user_id,
    provider_name,
    model_name`,
		`
CREATE MATERIALIZED VIEW IF NOT EXISTS mv_model_usage_model_daily
TO model_usage_model_daily
AS SELECT
    toDate(start_at) as stat_date,
    tenant_id,
    provider_name,
    model_name,
    any(instance_id) as instance_id,
    count() as request_count,
    sum(input_tokens) as total_input_tokens,
    sum(output_tokens) as total_output_tokens,
    sum(total_tokens) as total_tokens,
    avg(response_time_ms) as avg_response_time_ms,
    uniq(user_id) as unique_users
FROM model_usage_fact
GROUP BY
    toDate(start_at),
    tenant_id,
    provider_name,
    model_name`,
		`
    CREATE TABLE IF NOT EXISTS operation_audit_log
    (
        event_id UUID MATERIALIZED generateUUIDv7(),
        event_time DateTime64(3) DEFAULT now() CODEC(Delta(8), ZSTD(1)),
        event_date Date DEFAULT toDate(event_time) CODEC(ZSTD(1)),

        user_id LowCardinality(String) CODEC(ZSTD(1)),
        user_name String,
        ip_address String CODEC(ZSTD(1)),

        operation LowCardinality(String) CODEC(ZSTD(1)),
	    api_path LowCardinality(String) CODEC(ZSTD(1)),
        http_method LowCardinality(String) CODEC(ZSTD(1)),
        resource_type String,
        resource_id String,
        resource_name String,

        request_params String,
        error_code UInt16 CODEC(ZSTD(1)),
        message String,
        duration_ms UInt64,

        extra_metadata String,
    )
    ENGINE = MergeTree()
    PARTITION BY event_date
    ORDER BY (event_time, user_id, resource_type)
    TTL event_date + INTERVAL 30 DAY DELETE
    `,
		`
    CREATE TABLE IF NOT EXISTS ingestion_events_fact
    (
        event_at DateTime64(3) CODEC(Delta(8), ZSTD(1)),
        event_date Date DEFAULT toDate(event_at) CODEC(ZSTD(1)),

        tenant_id LowCardinality(String) CODEC(ZSTD(1)),
        dataset_id LowCardinality(String) CODEC(ZSTD(1)),
        doc_id String CODEC(ZSTD(1)),
        user_id LowCardinality(String) CODEC(ZSTD(1)),
        user_name String CODEC(ZSTD(1)),
        tenant_name String CODEC(ZSTD(1)),
        worker LowCardinality(String) CODEC(ZSTD(1)),
        ingestor_name LowCardinality(String) CODEC(ZSTD(1)),
        nats_queue LowCardinality(String) CODEC(ZSTD(1)),
        task_id String CODEC(ZSTD(1)),

        event_type LowCardinality(String) CODEC(ZSTD(1)),
        status LowCardinality(String) CODEC(ZSTD(1)),
        error_info String CODEC(ZSTD(1)),

        filename String CODEC(ZSTD(1)),
        file_type LowCardinality(String) CODEC(ZSTD(1)),
        file_size Int64 CODEC(ZSTD(1)),
        parser_id LowCardinality(String) CODEC(ZSTD(1)),

        upload_time Nullable(DateTime64(3)) CODEC(ZSTD(1)),
        enqueue_time Nullable(DateTime64(3)) CODEC(ZSTD(1)),
        begin_time Nullable(DateTime64(3)) CODEC(ZSTD(1)),
        end_time Nullable(DateTime64(3)) CODEC(ZSTD(1)),

        duration_ms Int64 CODEC(ZSTD(1)),
        wait_duration_s Float64 CODEC(ZSTD(1)),
        parse_duration_s Float64 CODEC(ZSTD(1)),
        progress Float64 CODEC(ZSTD(1)),
        run_count Int32 CODEC(ZSTD(1)),
        code Int32 CODEC(ZSTD(1)),

        operation Nullable(String) CODEC(ZSTD(1)),

        INDEX idx_dataset_id dataset_id TYPE bloom_filter(0.01) GRANULARITY 1,
        INDEX idx_doc_id doc_id TYPE bloom_filter(0.01) GRANULARITY 1,
        INDEX idx_user_id user_id TYPE bloom_filter(0.01) GRANULARITY 1,
        INDEX idx_task_id task_id TYPE bloom_filter(0.01) GRANULARITY 1
    )
    ENGINE = MergeTree
    PARTITION BY toYYYYMM(event_date)
    ORDER BY (tenant_id, event_at, task_id)
    SETTINGS index_granularity = 8192
    `,
		createSyncEventsSQL,
	}
}
