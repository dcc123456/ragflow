//go:build ee

package clickhouse

import (
	"strings"
	"testing"
	"time"

	"ragflow/internal/common"
)

// newTestDriver builds a Driver with a caller-owned channel and no real
// ClickHouse connection. Unit tests exercise the channel/draining/mapping logic
// without a server; flushIngestionEvents is covered by the integration test.
func newTestDriver(ch chan interface{}) *Driver {
	return &Driver{
		channel:   ch,
		batchSize: 1024,
	}
}

// colIndex returns the position of a column in ingestionColumns.
func colIndex(name string) int {
	for i, c := range ingestionColumns {
		if c == name {
			return i
		}
	}
	panic("column " + name + " not present in ingestionColumns")
}

func TestIngestionColumnsCount(t *testing.T) {
	if len(ingestionColumns) != 29 {
		t.Fatalf("expected 29 ingest columns, got %d", len(ingestionColumns))
	}
	if ingestionColumns[0] != "event_at" {
		t.Fatalf("first column must be event_at, got %q", ingestionColumns[0])
	}
	if ingestionColumns[len(ingestionColumns)-1] != "operation" {
		t.Fatalf("last column must be operation, got %q", ingestionColumns[len(ingestionColumns)-1])
	}
}

func TestIngestionRowMapping(t *testing.T) {
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	log := &common.IngestionLog{
		EventTime:      now,
		TenantID:       "t1",
		DatasetID:      "d1",
		DocID:          "doc1",
		UserID:         "u1",
		UserName:       "alice",
		Worker:         "w1",
		TaskID:         "task1",
		EventType:      "completed",
		Status:         "COMPLETED",
		ErrorInfo:      "",
		Filename:       "a.pdf",
		FileType:       "pdf",
		FileSize:       1234,
		ParserID:       "p1",
		UploadTime:     now,
		EnqueueTime:    now,
		BeginTime:      now,
		EndTime:        now,
		DurationMS:     500,
		WaitDurationS:  1.5,
		ParseDurationS: 2.5,
		Progress:       1.0,
		RunCount:       2,
		Code:           0,
		Operation:      nil,
	}
	row := ingestionRow(log)
	if len(row) != len(ingestionColumns) {
		t.Fatalf("row length %d must equal column count %d", len(row), len(ingestionColumns))
	}
	if !row[colIndex("event_at")].(time.Time).Equal(now) {
		t.Fatalf("event_at mismatch: %v", row[colIndex("event_at")])
	}
	if row[colIndex("tenant_id")].(string) != "t1" {
		t.Fatalf("tenant_id mismatch: %v", row[colIndex("tenant_id")])
	}
	if row[colIndex("task_id")].(string) != "task1" {
		t.Fatalf("task_id mismatch: %v", row[colIndex("task_id")])
	}
	if row[colIndex("file_size")].(int64) != 1234 {
		t.Fatalf("file_size mismatch: %v", row[colIndex("file_size")])
	}
	if row[colIndex("run_count")].(int32) != 2 {
		t.Fatalf("run_count mismatch: %v", row[colIndex("run_count")])
	}
	if row[colIndex("code")].(int32) != 0 {
		t.Fatalf("code mismatch: %v", row[colIndex("code")])
	}
	// operation unset -> NULL (nil) in the row
	if row[colIndex("operation")] != nil {
		t.Fatalf("operation must be nil when unset, got %v", row[colIndex("operation")])
	}
}

func TestIngestionRowNullableTimes(t *testing.T) {
	log := &common.IngestionLog{TaskID: "zero-times"}
	row := ingestionRow(log)
	for _, name := range []string{"upload_time", "enqueue_time", "begin_time", "end_time"} {
		if row[colIndex(name)] != nil {
			t.Fatalf("%s should be NULL (nil) for zero time, got %v", name, row[colIndex(name)])
		}
	}
}

func TestIngestionRowOperationJSON(t *testing.T) {
	log := &common.IngestionLog{
		TaskID:    "op",
		Operation: map[string]any{"retry": float64(2), "reason": "timeout"},
	}
	row := ingestionRow(log)
	raw, ok := row[colIndex("operation")].(string)
	if !ok || raw == "" {
		t.Fatalf("operation should serialize to a JSON string, got %#v", row[colIndex("operation")])
	}
	if !strings.Contains(raw, "timeout") {
		t.Fatalf("operation JSON missing content: %s", raw)
	}
}

func TestCollectIngestionLogSuccess(t *testing.T) {
	d := newTestDriver(make(chan interface{}, 4))
	if err := d.CollectIngestionLog(common.IngestionLog{TaskID: "t1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	select {
	case v := <-d.channel:
		ptr, ok := v.(*common.IngestionLog)
		if !ok {
			t.Fatalf("channel should carry *common.IngestionLog, got %T", v)
		}
		if ptr.TaskID != "t1" {
			t.Fatalf("carried wrong task id: %q", ptr.TaskID)
		}
	default:
		t.Fatal("collector did not enqueue the log")
	}
}

func TestCollectIngestionLogChannelFull(t *testing.T) {
	d := newTestDriver(make(chan interface{}, 1))
	d.channel <- "blocker" // fill the only slot
	if err := d.CollectIngestionLog(common.IngestionLog{TaskID: "t1"}); err == nil {
		t.Fatal("expected channel-full error when the buffer is full")
	} else if !strings.Contains(err.Error(), "channel is full") {
		t.Fatalf("error should mention channel full, got %q", err.Error())
	}
}

func TestRegisterIngestLogWiresSink(t *testing.T) {
	d := newTestDriver(make(chan interface{}, 4))
	d.RegisterIngestLog()
	defer common.SetIngestLogSink(nil)

	common.LogIngest(&common.IngestionLog{TaskID: "wired"})

	select {
	case v := <-d.channel:
		ptr, ok := v.(*common.IngestionLog)
		if !ok || ptr.TaskID != "wired" {
			t.Fatalf("sink did not deliver the log to the channel: %#v", v)
		}
	default:
		t.Fatal("registered sink did not deliver the log to the channel")
	}
}

func TestDrainChannelCollectsIngestionLogs(t *testing.T) {
	ch := make(chan interface{}, 8)
	ch <- &common.IngestionLog{TaskID: "x"}
	ch <- &common.ModelUsage{TenantID: "t"}
	d := newTestDriver(ch)

	usg, opLog, ing, _ := d.drainChannel()
	if len(ing) != 1 || ing[0].TaskID != "x" {
		t.Fatalf("expected exactly 1 ingestion log drained, got %#v", ing)
	}
	if len(usg) != 1 {
		t.Fatalf("expected 1 model usage drained, got %d", len(usg))
	}
	if len(opLog) != 0 {
		t.Fatalf("expected 0 operation logs, got %d", len(opLog))
	}
}

func TestCreateSQLsIncludesIngestionFact(t *testing.T) {
	var found bool
	for _, stmt := range getCreateSQLs() {
		if strings.Contains(stmt, "CREATE TABLE IF NOT EXISTS ingestion_events_fact") {
			found = true
		}
	}
	if !found {
		t.Fatal("getCreateSQLs must include the ingestion_events_fact DDL")
	}
}

// tenant_name must sit immediately after user_name in the column order so the
// INSERT and ingestionRow stay aligned with the DDL.
func TestIngestionColumnsIncludesTenantNameAfterUserName(t *testing.T) {
	ui := colIndex("user_name")
	ti := colIndex("tenant_name")
	if ti != ui+1 {
		t.Fatalf("tenant_name must follow user_name; got user_name@%d, tenant_name@%d", ui, ti)
	}
}

func TestIngestionRowMapsTenantName(t *testing.T) {
	log := &common.IngestionLog{
		TaskID:     "tn",
		UserID:     "u1",
		UserName:   "alice",
		TenantID:   "t1",
		TenantName: "Acme Corp",
	}
	row := ingestionRow(log)
	if len(row) != len(ingestionColumns) {
		t.Fatalf("row length %d must equal column count %d", len(row), len(ingestionColumns))
	}
	if row[colIndex("tenant_name")].(string) != "Acme Corp" {
		t.Fatalf("tenant_name mapping mismatch: %v", row[colIndex("tenant_name")])
	}
	if row[colIndex("user_name")].(string) != "alice" {
		t.Fatalf("user_name mapping mismatch: %v", row[colIndex("user_name")])
	}
}

func TestCreateSQLsIncludesTenantName(t *testing.T) {
	var found bool
	for _, stmt := range getCreateSQLs() {
		if strings.Contains(stmt, "CREATE TABLE IF NOT EXISTS ingestion_events_fact") &&
			strings.Contains(stmt, "tenant_name") {
			found = true
		}
	}
	if !found {
		t.Fatal("ingestion_events_fact DDL must declare a tenant_name column")
	}
}

// TestCreateSQLsIncludesIdentityRoutingColumns guards that the ingestion_events_fact
// DDL declares the two routing identity columns (ingestor_name, nats_queue). On an
// upgraded deployment the operator drops the stale table once before redeploy so that
// InitDB's CREATE TABLE IF NOT EXISTS rebuilds it from this DDL with every current column.
func TestCreateSQLsIncludesIdentityRoutingColumns(t *testing.T) {
	var ddl string
	for _, stmt := range getCreateSQLs() {
		if strings.Contains(stmt, "CREATE TABLE IF NOT EXISTS ingestion_events_fact") {
			ddl = stmt
		}
	}
	if ddl == "" {
		t.Fatal("ingestion_events_fact DDL not found in getCreateSQLs")
	}
	if !strings.Contains(ddl, "ingestor_name") {
		t.Fatal("ingestion_events_fact DDL must declare an ingestor_name column")
	}
	if !strings.Contains(ddl, "nats_queue") {
		t.Fatal("ingestion_events_fact DDL must declare a nats_queue column")
	}
}
