//go:build ee && integration

package clickhouse

import (
	"os"
	"slices"
	"testing"
	"time"
)

func TestSyncEventTimeline(t *testing.T) {
	if os.Getenv("CLICKHOUSE_TEST_DATABASE") == "" {
		t.Skip("set CLICKHOUSE_TEST_DATABASE to an isolated ClickHouse database")
	}
	d := newIntegrationDriver(t)
	t.Cleanup(func() { _ = d.conn.Close() })
	ctx := t.Context()
	taskID := "sync-test-" + time.Now().Format("20060102150405.000000000")
	for i, message := range []string{"queued", "started", "batch indexed", "completed"} {
		if err := d.CollectSyncEvent(SyncEvent{
			TaskID: taskID, EventType: i + 1, Component: "sync", Message: message,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.CollectSyncEvent(SyncEvent{TaskID: taskID + "-other", Message: "other task"}); err != nil {
		t.Fatal(err)
	}
	d.drainAndFlush()

	checkPage := func(limit int, after, before *int, hasBefore, hasAfter bool, messages ...string) []SyncEvent {
		t.Helper()
		page, err := d.ListSyncEvents(ctx, taskID, limit, after, before)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, event := range page.Events {
			got = append(got, event.Message)
			if event.ID <= 0 || event.ID > (1<<53)-1 || event.CreatedAt.IsZero() || event.Component != "sync" || event.EventType == 0 {
				t.Fatalf("invalid timeline event: %+v", event)
			}
		}
		if !slices.Equal(got, messages) || page.HasMoreBefore != hasBefore || page.HasMoreAfter != hasAfter {
			t.Fatalf("events: %v, before: %t, after: %t; want %v, %t, %t", got, page.HasMoreBefore, page.HasMoreAfter, messages, hasBefore, hasAfter)
		}
		return page.Events
	}

	latest := checkPage(2, nil, nil, true, false, "batch indexed", "completed")
	older := checkPage(2, nil, &latest[0].ID, false, true, "queued", "started")
	checkPage(1, &older[0].ID, nil, true, true, "started")
	checkPage(1, &latest[1].ID, nil, true, false)
	checkPage(1, nil, &older[0].ID, false, true)
}
