//go:build ee

package clickhouse

import (
	"context"
	"fmt"
	"slices"
	"time"

	"ragflow/internal/common"
)

// SyncEvent is one progress entry in a sync or prune task's timeline.
type SyncEvent struct {
	ID        int       `json:"id"`
	TaskID    string    `json:"-"`
	CreatedAt time.Time `json:"ts"`
	EventType int       `json:"event_type"`
	Component string    `json:"component"`
	Phase     int       `json:"phase"`
	Message   string    `json:"message"`
}

// SyncEventPage contains events in ascending cursor order.
type SyncEventPage struct {
	Events        []SyncEvent
	HasMoreBefore bool
	HasMoreAfter  bool
}

// CollectSyncEvent queues an event without blocking the syncer.
func (d *Driver) CollectSyncEvent(event SyncEvent) error {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	select {
	case d.channel <- &event:
		return nil
	default:
		return fmt.Errorf("channel is full, current capacity: %d, backlog: %d", cap(d.channel), len(d.channel))
	}
}

const createSyncEventsSQL = `CREATE TABLE IF NOT EXISTS sync_events_fact
(
    task_id String,
    event_id Int64,
    event_at DateTime64(3, 'UTC'),
    event_type Int32,
    component LowCardinality(String),
    phase Int32,
    message String CODEC(ZSTD(1))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(event_at)
ORDER BY (task_id, event_id)
TTL toDateTime(event_at) + INTERVAL 30 DAY DELETE`

func (d *Driver) flushSyncEvents(events []*SyncEvent) {
	if len(events) == 0 {
		return
	}
	batch, err := d.conn.PrepareBatch(d.ctx, `INSERT INTO sync_events_fact
		(task_id, event_id, event_at, event_type, component, phase, message)`)
	if err != nil {
		common.Error("prepare sync event batch", err)
		return
	}
	defer func() { _ = batch.Close() }()
	for _, event := range events {
		// The consumer assigns cursors in flush order within the task's worker. Microseconds fit JS numbers.
		d.syncEventID = max(time.Now().UnixMicro(), d.syncEventID+1)
		if err := batch.Append(event.TaskID, d.syncEventID, event.CreatedAt, int32(event.EventType), event.Component, int32(event.Phase), event.Message); err != nil {
			common.Error("append sync event to batch", err)
			return
		}
	}
	if err := batch.Send(); err != nil {
		common.Error("write sync event batch", err)
	}
}

// ListSyncEvents returns the latest page or moves strictly past the supplied cursor.
func (d *Driver) ListSyncEvents(ctx context.Context, taskID string, limit int, afterID, beforeID *int) (*SyncEventPage, error) {
	if limit < 1 || limit > 500 || (afterID != nil && beforeID != nil) {
		return nil, fmt.Errorf("limit must be between 1 and 500 and cursors are mutually exclusive")
	}
	const selectEvents = `SELECT event_id, event_at, event_type, component, phase, message FROM sync_events_fact WHERE task_id = ?`
	query := selectEvents + ` ORDER BY event_id DESC LIMIT ?`
	args := []any{taskID, limit + 1}
	switch {
	case afterID != nil:
		query = selectEvents + ` AND event_id > ? ORDER BY event_id ASC LIMIT ?`
		args = []any{taskID, *afterID, limit + 1}
	case beforeID != nil:
		query = selectEvents + ` AND event_id < ? ORDER BY event_id DESC LIMIT ?`
		args = []any{taskID, *beforeID, limit + 1}
	}
	rows, err := d.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	page := &SyncEventPage{Events: make([]SyncEvent, 0, limit)}
	for rows.Next() {
		event := SyncEvent{TaskID: taskID}
		var id int64
		var eventType, phase int32
		if err := rows.Scan(&id, &event.CreatedAt, &eventType, &event.Component, &phase, &event.Message); err != nil {
			return nil, err
		}
		event.ID, event.EventType, event.Phase = int(id), int(eventType), int(phase)
		page.Events = append(page.Events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	more := len(page.Events) > limit
	if more {
		page.Events = page.Events[:limit]
	}
	const selectAdjacent = `SELECT count() FROM (SELECT 1 FROM sync_events_fact WHERE task_id = ? AND `
	var comparison string
	var cursor int
	var opposite *bool
	if afterID != nil {
		page.HasMoreAfter = more
		opposite = &page.HasMoreBefore
		comparison, cursor = `event_id <= ?`, *afterID
		if len(page.Events) > 0 {
			comparison, cursor = `event_id < ?`, page.Events[0].ID
		}
	} else {
		page.HasMoreBefore = more
		slices.Reverse(page.Events)
		if beforeID == nil {
			return page, nil
		}
		opposite = &page.HasMoreAfter
		comparison, cursor = `event_id >= ?`, *beforeID
		if len(page.Events) > 0 {
			comparison, cursor = `event_id > ?`, page.Events[len(page.Events)-1].ID
		}
	}
	var count uint64
	if err := d.conn.QueryRow(ctx, selectAdjacent+comparison+` LIMIT 1)`, taskID, cursor).Scan(&count); err != nil {
		return nil, err
	}
	*opposite = count > 0
	return page, nil
}
