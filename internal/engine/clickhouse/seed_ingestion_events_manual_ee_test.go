//go:build ee && manual

package clickhouse

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/observability"
)

// TestSeedIngestionEvents emits 1000 ingestion lifecycle events through the REAL
// system capture path (observability.BuildIngestLog -> common.LogIngest -> the
// EE ClickHouse sink) so the Grafana dashboard has realistic, system-produced
// data instead of hand-written SQL rows. EventTime is backdated so events spread
// across the last 30 days, and the channel is drained periodically because
// CollectIngestionLog is non-blocking (a full 64-slot buffer drops events).
func TestSeedIngestionEvents(t *testing.T) {
	d := newIntegrationDriver(t)
	defer d.conn.Close()
	defer common.SetIngestLogSink(nil)
	ctx := context.Background()

	if err := d.conn.Exec(ctx, "TRUNCATE TABLE ingestion_events_fact"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	common.SetIngestLogSink(d.CollectIngestionLog)

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
	exts := []string{".pdf", ".docx", ".txt", ".md", ".csv", ".xlsx"}
	parsers := []string{"pdf_parser", "docx_parser", "text_parser", "md_parser", "csv_parser", "excel_parser"}
	r := rand.New(rand.NewSource(42))

	for i := 0; i < 1000; i++ {
		idx := r.Intn(6)
		typ, status := events[idx].typ, events[idx].status
		tID := fmt.Sprintf("tenant-%d", 1+r.Intn(12))
		uID := fmt.Sprintf("user-%d", 1+r.Intn(60))
		dID := fmt.Sprintf("doc-%d", 1+r.Intn(250))
		kbID := fmt.Sprintf("kb-%d", 1+r.Intn(12))
		taskID := fmt.Sprintf("seed-task-%06d", i)

		base := time.Now().Add(-time.Duration(r.Intn(30*86400)) * time.Second)
		upload := base.Add(-time.Duration(r.Intn(6*3600)) * time.Second)
		enqueue := upload.Add(time.Duration(r.Intn(1800)) * time.Second)

		var begin *time.Time
		var dur float64
		var prog float64
		var errInfo string
		if idx >= 2 { // running/completed/failed/stopped have a worker-claim begin
			b := enqueue.Add(time.Duration(r.Intn(300)) * time.Second)
			begin = &b
		}
		if idx >= 3 { // completed/failed/stopped are terminal
			dur = float64(30 + r.Intn(1200))
			prog = 1.0
		} else if idx == 2 { // running is in progress
			prog = float64(r.Intn(90)+5) / 100.0
		}
		if idx == 4 { // failed carries an error reason
			errInfo = "parse timeout / model unavailable"
		}

		ext := exts[r.Intn(len(exts))]
		name := fmt.Sprintf("doc-%d", 1+r.Intn(250))
		ftype := strings.TrimPrefix(ext, ".")
		doc := &entity.Document{
			Name:       &name,
			Suffix:     ext,
			Type:       ftype,
			Size:       int64(100000 + r.Intn(9000000)),
			ParserID:   parsers[r.Intn(len(parsers))],
			CreateDate: &upload,
		}
		if begin != nil {
			doc.ProcessBeginAt = begin
		}
		if idx >= 3 {
			doc.ProcessDuration = dur
		}
		rc := 1
		run := &entity.PipelineOperationLog{
			RunCount:   &rc,
			TenantID:   tID,
			CreateDate: &enqueue,
			Progress:   prog,
		}
		if begin != nil {
			run.ProcessBeginAt = begin
		}
		if idx >= 3 {
			run.ProcessDuration = dur
		}
		task := &entity.IngestionTask{ID: taskID, DocumentID: dID, DatasetID: kbID, UserID: uID}

		log := observability.BuildIngestLog(ctx, task, doc, run, typ, status, errInfo)
		log.EventTime = base // backdate so events spread over the retention window
		common.LogIngest(&log)

		if i%50 == 0 {
			d.drainAndFlush()
		}
	}
	d.drainAndFlush()

	var cnt uint64
	if err := d.conn.QueryRow(ctx, "SELECT count() FROM ingestion_events_fact").Scan(&cnt); err != nil {
		t.Fatalf("count: %v", err)
	}
	t.Logf("seeded %d events via the real capture path", cnt)
	if cnt < 1000 {
		t.Errorf("expected >=1000 events, got %d", cnt)
	}
}
