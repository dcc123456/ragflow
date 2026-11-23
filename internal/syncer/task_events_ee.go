//go:build ee

package syncer

import (
	"ragflow/internal/common"
	"ragflow/internal/engine/clickhouse"

	"go.uber.org/zap"
)

var collectTaskEvent = func(event clickhouse.SyncEvent) error {
	if driver := clickhouse.GetDriver(); driver != nil {
		return driver.CollectSyncEvent(event)
	}
	return nil
}

func recordTaskEvent(taskID string, eventType int, component, message string) {
	if err := collectTaskEvent(clickhouse.SyncEvent{
		TaskID: taskID, EventType: eventType, Component: component, Message: message,
	}); err != nil {
		common.Warn("Sync progress event could not be queued", zap.String("task_id", taskID), zap.Error(err))
	}
}
