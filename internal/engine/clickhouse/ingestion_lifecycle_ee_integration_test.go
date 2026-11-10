//go:build ee && integration

package clickhouse

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/observability"
)

// TestIngestionLifecycleCollectsAllEvents drives the real shared emission path
// (observability.BuildIngestLog -> common.LogIngest, the EE seam) for every
// ingestion lifecycle event type and verifies each lands in
// ingestion_events_fact with the user/tenant/doc/status dimensions populated.
//
// This is the functional gate for PR #1139: it confirms the EE sink actually
// records created/scheduled/running/completed/failed/stopped events against a
// live ClickHouse, not just that the DDL/INSERT plumbing compiles.
func TestIngestionLifecycleCollectsAllEvents(t *testing.T) {
	d := newIntegrationDriver(t)
	defer d.conn.Close()
	defer common.SetIngestLogSink(nil)

	common.SetIngestLogSink(d.CollectIngestionLog)

	now := time.Now().UTC()
	open := now.Add(-30 * time.Minute)
	docBegin := now.Add(-20 * time.Minute)

	task := &entity.IngestionTask{
		ID:         "lifecycle-task-001",
		DocumentID: "doc-lifecycle-001",
		DatasetID:  "kb-lifecycle-001",
		UserID:     "user-lifecycle-001",
	}
	name := "report.pdf"
	doc := &entity.Document{
		Name:           &name,
		Suffix:         ".pdf",
		Type:           "pdf",
		Size:           4096,
		ParserID:       "pdf_parser",
		CreateDate:     &now,
		ProcessBeginAt: &docBegin,
	}
	runCount := 1
	run := &entity.PipelineOperationLog{
		RunCount:        &runCount,
		TenantID:        "tenant-lifecycle-001",
		CreateDate:      &open,
		ProcessBeginAt:  &docBegin,
		ProcessDuration: 20 * 60,
		Progress:        1.0,
	}

	taskID := "lifecycle-" + now.Format("20060102150405.000000000")

	events := []struct {
		typ    string
		status string
	}{
		{"created", "CREATED"},
		{"scheduled", "SCHEDULED"},
		{"running", "RUNNING"},
		{"completed", "COMPLETED"},
		{"failed", "FAILED"},
		{"stopped", "STOPPED"},
	}

	ctx := context.Background()
	for _, ev := range events {
		log := observability.BuildIngestLog(task, doc, run, ev.typ, ev.status, "", "", "")
		log.TaskID = taskID
		common.LogIngest(&log)
	}

	d.drainAndFlush()

	rows, err := d.conn.Query(ctx, `
		SELECT event_type, status, tenant_id, user_id, doc_id, dataset_id, task_id
		FROM ingestion_events_fact
		WHERE task_id = ?
		ORDER BY event_type
	`, taskID)
	if err != nil {
		t.Fatalf("query events: %v", err)
	}
	defer rows.Close()

	got := map[string]string{}
	for rows.Next() {
		var (
			etype, status, tenant, user, docID, ds, tid string
		)
		if err := rows.Scan(&etype, &status, &tenant, &user, &docID, &ds, &tid); err != nil {
			t.Fatalf("scan row: %v", err)
		}
		got[etype] = status
		if tenant != "tenant-lifecycle-001" {
			t.Errorf("event %s: tenant_id=%q, want tenant-lifecycle-001", etype, tenant)
		}
		if user != "user-lifecycle-001" {
			t.Errorf("event %s: user_id=%q, want user-lifecycle-001", etype, user)
		}
		if docID != "doc-lifecycle-001" {
			t.Errorf("event %s: doc_id=%q, want doc-lifecycle-001", etype, docID)
		}
		if ds != "kb-lifecycle-001" {
			t.Errorf("event %s: dataset_id=%q, want kb-lifecycle-001", etype, ds)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows iteration: %v", err)
	}

	for _, ev := range events {
		if got[ev.typ] != ev.status {
			t.Errorf("event_type %q: expected status %q, got %q (collected=%v)",
				ev.typ, ev.status, got[ev.typ], got[ev.typ] != "")
		}
	}

	var endTS *time.Time
	if err := d.conn.QueryRow(ctx, `
		SELECT end_time FROM ingestion_events_fact
		WHERE task_id = ? AND event_type = 'completed'
	`, taskID).Scan(&endTS); err != nil {
		t.Fatalf("query completed end_time: %v", err)
	}
	if endTS == nil {
		t.Error("completed event: end_time must be non-null (derived from ProcessBeginAt+ProcessDuration)")
	}

	// Step 2 gate: the EE LogIngest enrichment must land derived UI dimensions
	// (event_label / terminal / has_error) in the Operation JSON column. Verify
	// the failed and completed events carry the expected derived values.
	type opExpect struct {
		eventType string
		label     string
		terminal  bool
		hasError  bool
	}
	wantOps := []opExpect{
		{"failed", "Failed", true, true},
		{"completed", "Completed", true, false},
		{"created", "Created", false, false},
	}
	for _, want := range wantOps {
		var op *string
		if err := d.conn.QueryRow(ctx, `
			SELECT operation FROM ingestion_events_fact
			WHERE task_id = ? AND event_type = ?
		`, taskID, want.eventType).Scan(&op); err != nil {
			t.Fatalf("query operation for %s: %v", want.eventType, err)
		}
		if op == nil || *op == "" {
			t.Fatalf("event %s: operation JSON must be non-empty after EE enrichment", want.eventType)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(*op), &m); err != nil {
			t.Fatalf("event %s: operation JSON unmarshal: %v (raw=%s)", want.eventType, err, *op)
		}
		if m["event_label"] != want.label {
			t.Errorf("event %s: operation.event_label=%v, want %q", want.eventType, m["event_label"], want.label)
		}
		if m["terminal"] != want.terminal {
			t.Errorf("event %s: operation.terminal=%v, want %v", want.eventType, m["terminal"], want.terminal)
		}
		if m["has_error"] != want.hasError {
			t.Errorf("event %s: operation.has_error=%v, want %v", want.eventType, m["has_error"], want.hasError)
		}
	}
}
