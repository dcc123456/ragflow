//go:build ee

package clickhouse

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ragflow/internal/common"

	"go.uber.org/zap"
)

// ingestionColumns is the exact column order of the INSERT into
// ingestion_events_fact. ingestionRow returns values in this same order, so the
// two must stay in lockstep. event_date is intentionally absent: it is a
// DEFAULT-derived column (toDate(event_at)) and must not be inserted explicitly.
var ingestionColumns = []string{
	"event_at",
	"tenant_id",
	"dataset_id",
	"doc_id",
	"user_id",
	"user_name",
	"tenant_name",
	"worker",
	"ingestor_name",
	"nats_queue",
	"task_id",
	"event_type",
	"status",
	"error_info",
	"filename",
	"file_type",
	"file_size",
	"parser_id",
	"upload_time",
	"enqueue_time",
	"begin_time",
	"end_time",
	"duration_ms",
	"wait_duration_s",
	"parse_duration_s",
	"progress",
	"run_count",
	"code",
	"operation",
}

// ingestionInsertSQL is generated from ingestionColumns so the INSERT column
// list has a single source of truth. event_date is a DEFAULT-derived column
// (toDate(event_at)) and is intentionally not inserted.
var ingestionInsertSQL = fmt.Sprintf(
	"INSERT INTO ingestion_events_fact (%s)",
	strings.Join(ingestionColumns, ", "),
)

// toNullableTime maps a Go time to a ClickHouse Nullable(DateTime64(3)) value:
// the zero time becomes nil (NULL), any other instant is passed through as UTC.
func toNullableTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

// operationJSON serializes the extensible Operation map to a JSON string for the
// Nullable(String) column. An empty map becomes nil (NULL). The field is a
// reserved, forward-compatible extension point: nothing in the codebase assigns
// it yet, so it lands as NULL until a future event populates it.
func operationJSON(m map[string]any) interface{} {
	if len(m) == 0 {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		common.Error("marshal ingestion operation", err)
		return nil
	}
	return string(b)
}

// ingestionRow renders one ingestion event as the ordered column tuple expected
// by the INSERT statement. It is a pure function so the mapping can be unit
// tested without a live ClickHouse connection.
func ingestionRow(log *common.IngestionLog) []interface{} {
	return []interface{}{
		log.EventTime.UTC(),
		log.TenantID,
		log.DatasetID,
		log.DocID,
		log.UserID,
		log.UserName,
		log.TenantName,
		log.Worker,
		log.IngestorName,
		log.NatsQueue,
		log.TaskID,
		log.EventType,
		log.Status,
		log.ErrorInfo,
		log.Filename,
		log.FileType,
		log.FileSize,
		log.ParserID,
		toNullableTime(log.UploadTime),
		toNullableTime(log.EnqueueTime),
		toNullableTime(log.BeginTime),
		toNullableTime(log.EndTime),
		log.DurationMS,
		log.WaitDurationS,
		log.ParseDurationS,
		log.Progress,
		int32(log.RunCount),
		int32(log.Code),
		operationJSON(log.Operation),
	}
}

// CollectIngestionLog enqueues one ingestion event onto the driver channel. It
// never blocks: if the buffered channel is full it returns an error and the
// event is dropped, protecting the ingestion pipeline from a slow/dead
// ClickHouse. The same non-blocking contract as CollectModelUsage.
func (d *Driver) CollectIngestionLog(log common.IngestionLog) error {
	select {
	case d.channel <- &log:
		return nil
	default:
		return fmt.Errorf("channel is full, current capacity: %d, backlog: %d", cap(d.channel), len(d.channel))
	}
}

// RegisterIngestLog wires the common ingest-log seam to this driver so that every
// event emitted through common.LogIngest is collected into ClickHouse.
func (d *Driver) RegisterIngestLog() {
	common.SetIngestLogSink(d.CollectIngestionLog)
}

// flushIngestionEvents batch-inserts a drained batch of ingestion events.
func (d *Driver) flushIngestionEvents(batch []*common.IngestionLog) {
	if len(batch) == 0 {
		return
	}

	batchInsert, err := d.conn.PrepareBatch(d.ctx, ingestionInsertSQL)

	if err != nil {
		common.Error("Prepare ingestion batch insert failed", err)
		return
	}

	for _, record := range batch {
		err = batchInsert.Append(ingestionRow(record)...)
		if err != nil {
			common.Error("Append ingestion record to batch failed", err)
			// Abort the whole batch: a failed Append leaves the batch in an
			// inconsistent state, so sending it would write a partial/garbled
			// batch. Drop the batch rather than risk a half-insert.
			return
		}
	}

	if err = batchInsert.Send(); err != nil {
		common.Error("Ingestion batch insert failed", err)
		return
	}

	common.Info("Ingestion events batch insert success", zap.Int("records", len(batch)))
}
