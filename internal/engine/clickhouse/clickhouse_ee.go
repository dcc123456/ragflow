//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

package clickhouse

import (
	"context"
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
	conn      driver.Conn
	config    *clickhouse.Options
	channel   chan *common.ModelUsage
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	batchSize int
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
			channel:   make(chan *common.ModelUsage, 128*1024),
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
		}
	}
}

func (d *Driver) drainAndFlush() {
	batch := d.drainChannel()

	if len(batch) == 0 {
		return
	}

	d.flush(batch)
}

func (d *Driver) drainChannel() []*common.ModelUsage {
	var batch []*common.ModelUsage

	for {
		select {
		case record, ok := <-d.channel:
			if !ok {
				// channel is closed
				return batch
			}
			batch = append(batch, record)

			if len(batch) >= d.batchSize {
				common.Info("Reach batch size limit, flush batch")
				return batch
			}

		default:
			// Drain channel, flush batch
			if len(batch) > 0 {
				common.Info("Drain channel, flush batch", zap.Int("batchSize", len(batch)))
			}
			return batch
		}
	}
}

func (d *Driver) flush(batch []*common.ModelUsage) {
	if len(batch) == 0 {
		return
	}

	batchInsert, err := d.conn.PrepareBatch(d.ctx, `
        INSERT INTO model_usage_fact (
            start_at, event_date, event_hour,
            user_id, user_email,
            tenant_id, tenant_email,
            group_id, group_name,
            app_id, app_name,
            session_id,
            provider_name, model_name,
            instance_id, api_key,
            request_id, type, response_time_ms, error_message,
            input_tokens, output_tokens, total_tokens
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

func (d *Driver) InitDB() error {
	tableName := "model_usage_fact"
	// 1. Check if table exists
	exists, err := d.tableExists(tableName)
	if err != nil {
		return fmt.Errorf("check table exists failed: %w", err)
	}

	if exists {
		common.Info(fmt.Sprintf("Table '%s' already exists, skip create", tableName))
		return nil
	}

	// 2. Create table and view
	common.Info("Start create tables and views...")

	statements := getCreateSQLs()
	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}

		err = d.conn.Exec(d.ctx, stmt)
		if err != nil {
			common.Error("Execute statement failed", err)
		}
	}

	common.Info("Create tables and views success")
	return nil
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
	}
}
