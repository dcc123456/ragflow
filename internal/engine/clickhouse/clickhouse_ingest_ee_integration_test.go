//go:build ee && (integration || manual)

package clickhouse

import (
	"cmp"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"ragflow/internal/common"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// newIntegrationDriver connects to a real ClickHouse named by CLICKHOUSE_TEST_ADDR
// (host:port) and ensures the ingestion_events_fact table exists. It returns a
// Driver whose flush path is exercised end-to-end.
func newIntegrationDriver(t *testing.T) *Driver {
	t.Helper()
	addr := os.Getenv("CLICKHOUSE_TEST_ADDR")
	if addr == "" {
		t.Skip("set CLICKHOUSE_TEST_ADDR=host:port (and optionally CLICKHOUSE_TEST_AUTH=user:pass) to run ingestion ClickHouse e2e")
	}

	user, pass := "default", ""
	if a := os.Getenv("CLICKHOUSE_TEST_AUTH"); a != "" {
		if parts := strings.SplitN(a, ":", 2); len(parts) == 2 {
			user, pass = parts[0], parts[1]
		}
	}

	ctx := context.Background()
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{addr},
		Auth: clickhouse.Auth{Database: cmp.Or(os.Getenv("CLICKHOUSE_TEST_DATABASE"), "default"), Username: user, Password: pass},
	})
	if err != nil {
		t.Fatalf("open clickhouse: %v", err)
	}
	if err = conn.Ping(ctx); err != nil {
		t.Fatalf("ping clickhouse: %v", err)
	}
	d := &Driver{
		conn:      conn,
		channel:   make(chan interface{}, 64),
		batchSize: 64,
		ctx:       ctx,
	}
	if err = d.InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	return d
}

// TestIngestionLogEndToEnd sends one event with NULL times and verifies it lands
// in ingestion_events_fact with correct scalar columns and NULL-able time columns.
func TestIngestionLogEndToEnd(t *testing.T) {
	d := newIntegrationDriver(t)
	defer d.conn.Close()

	taskID := "e2e-" + time.Now().Format("20060102150405.000000000")
	now := time.Now().UTC()
	log := common.IngestionLog{
		EventTime: now,
		TenantID:  "ten1",
		DatasetID: "ds1",
		DocID:     "doc1",
		UserID:    "u1",
		TaskID:    taskID,
		EventType: "created",
		Status:    "CREATED",
		Filename:  "a.pdf",
		FileType:  "pdf",
		FileSize:  2048,
		ParserID:  "p1",
		RunCount:  1,
		// UploadTime/EnqueueTime/BeginTime/EndTime intentionally zero -> NULL
	}
	if err := d.CollectIngestionLog(log); err != nil {
		t.Fatalf("collect: %v", err)
	}
	d.drainAndFlush()

	row := d.conn.QueryRow(d.ctx, `
		SELECT tenant_id, task_id, file_size, event_type, upload_time, begin_time, operation
		FROM ingestion_events_fact
		WHERE task_id = ?
	`, taskID)
	var (
		tenant, gotTask, ftype, opSQL string
		fileSize                      int64
		uploadTime, beginTime         *time.Time
	)
	if err := row.Scan(&tenant, &gotTask, &fileSize, &ftype, &uploadTime, &beginTime, &opSQL); err != nil {
		t.Fatalf("query row: %v", err)
	}
	if tenant != "ten1" || gotTask != taskID {
		t.Fatalf("scalar mismatch: tenant=%q task=%q", tenant, gotTask)
	}
	if fileSize != 2048 {
		t.Fatalf("file_size mismatch: %d", fileSize)
	}
	if ftype != "created" {
		t.Fatalf("event_type mismatch: %q", ftype)
	}
	if uploadTime != nil {
		t.Fatalf("upload_time must be NULL for zero time, got %v", uploadTime)
	}
	if beginTime != nil {
		t.Fatalf("begin_time must be NULL for zero time, got %v", beginTime)
	}
	if opSQL != "" {
		t.Fatalf("operation must be empty/NULL when unset, got %q", opSQL)
	}
}
