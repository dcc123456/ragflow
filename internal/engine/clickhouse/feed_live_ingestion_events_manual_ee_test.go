//go:build ee && manual

package clickhouse

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"testing"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/observability"
)

// TestFeedLiveIngestionEvents is a local observation harness (NOT part of the
// PR functional gate). It drives the REAL capture path
// (observability.BuildIngestLog -> common.LogIngest -> the EE ClickHouse sink)
// at a steady rate so the Grafana dashboard can be watched updating live.
//
// It does NOT truncate the table, so events accumulate on top of existing rows.
// Each event gets EventTime = time.Now() (real-time), and tenant/user/doc IDs
// are randomized every tick, so the timeseries and aggregation panels change
// dynamically as new data lands.
//
// Tunables via env:
//
//	FEED_TOTAL      total events to emit        (default 600 -> 1 hour @10/min)
//	FEED_PERIOD_SEC seconds between emissions    (default 6   -> 10/min)
//	FEED_TENANTS    tenant id pool size          (default 12)
//	FEED_USERS      user id pool size            (default 60)
//	FEED_DOCS       doc id pool size             (default 200)
func TestFeedLiveIngestionEvents(t *testing.T) {
	d := newIntegrationDriver(t)
	defer d.conn.Close()
	defer common.SetIngestLogSink(nil)
	ctx := context.Background()

	common.SetIngestLogSink(d.CollectIngestionLog)

	total := atoiEnv(t, "FEED_TOTAL", 600)
	period := time.Duration(atoiEnv(t, "FEED_PERIOD_SEC", 6)) * time.Second
	nTenant := atoiEnv(t, "FEED_TENANTS", 12)
	nUser := atoiEnv(t, "FEED_USERS", 60)
	nDoc := atoiEnv(t, "FEED_DOCS", 200)

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
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	t.Logf("feed start: total=%d period=%s tenants=%d users=%d docs=%d", total, period, nTenant, nUser, nDoc)

	for i := 0; i < total; i++ {
		idx := r.Intn(6)
		typ, status := events[idx].typ, events[idx].status
		tID := fmt.Sprintf("tenant-%d", 1+r.Intn(nTenant))
		uID := fmt.Sprintf("user-%d", 1+r.Intn(nUser))
		dID := fmt.Sprintf("doc-%d", 1+r.Intn(nDoc))
		kbID := fmt.Sprintf("kb-%d", 1+r.Intn(nTenant))
		taskID := fmt.Sprintf("live-task-%06d", i)

		base := time.Now().UTC()
		upload := base.Add(-time.Duration(r.Intn(30)) * time.Second)
		enqueue := upload.Add(time.Duration(r.Intn(10)) * time.Second)

		var begin *time.Time
		var dur float64
		var prog float64
		var errInfo string
		if idx >= 2 {
			b := enqueue.Add(time.Duration(r.Intn(20)) * time.Second)
			begin = &b
		}
		if idx >= 3 {
			dur = float64(30 + r.Intn(1200))
			prog = 1.0
		} else if idx == 2 {
			prog = float64(r.Intn(90)+5) / 100.0
		}
		if idx == 4 {
			errInfo = "parse timeout / model unavailable"
		}

		ext := exts[r.Intn(len(exts))]
		name := fmt.Sprintf("doc-%d%s", 1+r.Intn(nDoc), ext)
		ftype := ext[1:]
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
		log.EventTime = base
		common.LogIngest(&log)

		d.drainAndFlush()

		if (i+1)%10 == 0 || i == 0 {
			var cnt uint64
			if err := d.conn.QueryRow(ctx, "SELECT count() FROM ingestion_events_fact").Scan(&cnt); err == nil {
				t.Logf("emitted %d/%d  total_rows=%d  last=(%s %s %s/%s/%s)",
					i+1, total, cnt, typ, status, tID, uID, dID)
			}
		}

		if i < total-1 {
			time.Sleep(period)
		}
	}

	var cnt uint64
	if err := d.conn.QueryRow(ctx, "SELECT count() FROM ingestion_events_fact").Scan(&cnt); err != nil {
		t.Fatalf("final count: %v", err)
	}
	t.Logf("feed complete: %d events emitted via the real capture path, total_rows=%d", total, cnt)
}

func atoiEnv(t *testing.T, key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
		t.Logf("env %s invalid (%q), using default %d", key, v, def)
	}
	return def
}
