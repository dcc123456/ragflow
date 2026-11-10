//go:build ee && integration

package clickhouse

import (
	"testing"
	"time"

	"ragflow/internal/common"
)

// TestIngestionLogNamesEndToEnd sends one event carrying the resolved display
// names and verifies user_name and tenant_name actually land in
// ingestion_events_fact through the real CollectIngestionLog ->
// flushIngestionEvents -> ingestionRow -> INSERT path (the exact production code
// exercised by the API/ingestor servers).
func TestIngestionLogNamesEndToEnd(t *testing.T) {
	d := newIntegrationDriver(t)
	defer d.conn.Close()

	taskID := "e2e-names-" + time.Now().Format("20060102150405.000000000")
	now := time.Now().UTC()
	log := common.IngestionLog{
		EventTime:  now,
		TenantID:   "ten-names-1",
		DatasetID:  "ds-names-1",
		DocID:      "doc-names-1",
		UserID:     "u-names-1",
		UserName:   "alice",
		TenantName: "Acme Corp",
		TaskID:     taskID,
		EventType:  "completed",
		Status:     "COMPLETED",
		Filename:   "b.pdf",
		FileType:   "pdf",
		FileSize:   4096,
		ParserID:   "p1",
		RunCount:   1,
	}
	if err := d.CollectIngestionLog(log); err != nil {
		t.Fatalf("collect: %v", err)
	}
	d.drainAndFlush()

	row := d.conn.QueryRow(d.ctx, `
		SELECT user_name, tenant_name, user_id, tenant_id
		FROM ingestion_events_fact
		WHERE task_id = ?
	`, taskID)
	var (
		userName, tenantName, userID, tenantID string
	)
	if err := row.Scan(&userName, &tenantName, &userID, &tenantID); err != nil {
		t.Fatalf("query row: %v", err)
	}
	if userName != "alice" {
		t.Fatalf("user_name = %q, want alice", userName)
	}
	if tenantName != "Acme Corp" {
		t.Fatalf("tenant_name = %q, want Acme Corp", tenantName)
	}
	if userID != "u-names-1" || tenantID != "ten-names-1" {
		t.Fatalf("id mismatch: userID=%q tenantID=%q", userID, tenantID)
	}
}
