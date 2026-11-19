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
	"strings"
	"testing"
	"time"

	chdriver "github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// sqlRecordingConn captures issued SQL so a unit test can pin query shape
// without a live ClickHouse instance.
type sqlRecordingConn struct {
	queries []string
}

func (*sqlRecordingConn) Contributors() []string                            { return nil }
func (*sqlRecordingConn) ServerVersion() (*chdriver.ServerVersion, error)   { return nil, nil }
func (*sqlRecordingConn) Select(context.Context, any, string, ...any) error { return nil }
func (c *sqlRecordingConn) Query(_ context.Context, query string, _ ...any) (chdriver.Rows, error) {
	c.queries = append(c.queries, query)
	return &sqlEmptyRows{}, nil
}
func (*sqlRecordingConn) QueryRow(context.Context, string, ...any) chdriver.Row {
	return &sqlEmptyRow{}
}
func (*sqlRecordingConn) PrepareBatch(context.Context, string, ...chdriver.PrepareBatchOption) (chdriver.Batch, error) {
	return nil, nil
}
func (*sqlRecordingConn) Exec(context.Context, string, ...any) error              { return nil }
func (*sqlRecordingConn) AsyncInsert(context.Context, string, bool, ...any) error { return nil }
func (*sqlRecordingConn) Ping(context.Context) error                              { return nil }
func (*sqlRecordingConn) Stats() chdriver.Stats                                   { return chdriver.Stats{} }
func (*sqlRecordingConn) Close() error                                            { return nil }

type sqlEmptyRows struct{}

func (*sqlEmptyRows) Next() bool                         { return false }
func (*sqlEmptyRows) Scan(...any) error                  { return nil }
func (*sqlEmptyRows) ScanStruct(any) error               { return nil }
func (*sqlEmptyRows) ColumnTypes() []chdriver.ColumnType { return nil }
func (*sqlEmptyRows) Totals(...any) error                { return nil }
func (*sqlEmptyRows) Columns() []string                  { return nil }
func (*sqlEmptyRows) Close() error                       { return nil }
func (*sqlEmptyRows) Err() error                         { return nil }
func (*sqlEmptyRows) HasData() bool                      { return false }

type sqlEmptyRow struct{}

func (*sqlEmptyRow) Err() error           { return nil }
func (*sqlEmptyRow) Scan(...any) error    { return nil }
func (*sqlEmptyRow) ScanStruct(any) error { return nil }

// TestQueryFailRate_UsesSingleWhitelistedAggregate pins that fail-rate is
// aggregated in one SQL statement with a whitelisted column and bound limit,
// instead of two scans plus in-Go sorting.
func TestQueryFailRate_UsesSingleWhitelistedAggregate(t *testing.T) {
	conn := &sqlRecordingConn{}
	d := &Driver{conn: conn, ctx: context.Background()}
	q := MonitorQuery{
		From:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		To:       time.Date(2026, 1, 19, 0, 0, 0, 0, time.UTC),
		Scope:    monitorScopeAdmin,
		TenantID: "All", UserID: "All", DocID: "All", Queue: "All", Ingestor: "All", Status: "All",
	}

	if _, err := d.QueryFailRate(q, "tenant", 10); err != nil {
		t.Fatalf("QueryFailRate: %v", err)
	}
	if len(conn.queries) != 1 {
		t.Fatalf("expected a single SQL statement, got %d: %v", len(conn.queries), conn.queries)
	}
	sql := conn.queries[0]
	for _, fragment := range []string{"countIf", "fail_rate", "ORDER BY fail_rate DESC", "LIMIT ?"} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("SQL missing %q: %s", fragment, sql)
		}
	}
}
