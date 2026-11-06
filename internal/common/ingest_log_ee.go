//go:build ee

package common

import (
	"go.uber.org/zap"
)

func InitIngestLog(logType string, file FileOutput, serviceName string) error {
	return nil
}

// SyncIngestLog satisfies the shared shutdown hook; the EE sink has no local buffer.
func SyncIngestLog() {}

func LogIngest(_ *IngestionLog, fields ...zap.Field) {

}

func LogIngestEvent(log IngestionLog) {
}
