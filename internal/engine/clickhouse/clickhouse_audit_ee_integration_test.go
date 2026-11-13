//go:build ee && integration

package clickhouse

import (
	"encoding/json/v2"
	"errors"
	"testing"
	"time"

	"ragflow/internal/common"
)

func TestAuditDatabaseEndToEnd(t *testing.T) {
	d := newIntegrationDriver(t)
	t.Cleanup(func() { _ = d.conn.Close() })
	userID := "audit-test-" + time.Now().Format("20060102150405.000000000")
	t.Cleanup(func() {
		if err := d.conn.Exec(d.ctx, "ALTER TABLE operation_audit_log DELETE WHERE user_id = ? SETTINGS mutations_sync = 1", userID); err != nil {
			t.Errorf("clean audit events: %v", err)
		}
	})
	if err := common.InitAudit("database", common.FileOutput{}, "test"); err != nil {
		t.Fatal(err)
	}
	common.SetAuditLogSink(d.SaveOperationLog)
	defer common.SetAuditLogSink(nil)

	for _, eventError := range []error{nil, errors.New("rejected")} {
		common.Audit(&common.OperationLog{
			EventTime: time.Now(), UserID: userID, Operation: "update_dataset",
			Resources: map[string]any{"dataset_id": "dataset-test", "tenant_id": "tenant-test"},
			Error:     eventError,
		})
	}
	d.drainAndFlush()
	rows, err := d.conn.Query(d.ctx, "SELECT message, extra_metadata, resource_type, resource_id FROM operation_audit_log WHERE user_id = ? ORDER BY message", userID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var messages []string
	for rows.Next() {
		var message, metadata, resourceType, resourceID string
		if err := rows.Scan(&message, &metadata, &resourceType, &resourceID); err != nil {
			t.Fatal(err)
		}
		if resourceType != "dataset" || resourceID != "dataset-test" {
			t.Fatalf("lost resource columns: type=%q id=%q", resourceType, resourceID)
		}
		var resources map[string]string
		if err := json.Unmarshal([]byte(metadata), &resources); err != nil {
			t.Fatal(err)
		}
		if resources["dataset_id"] != "dataset-test" || resources["tenant_id"] != "tenant-test" {
			t.Fatalf("lost resource context: %s", metadata)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0] != "" || messages[1] != "rejected" {
		t.Fatalf("expected exactly one success and failure event, got %v", messages)
	}
}
