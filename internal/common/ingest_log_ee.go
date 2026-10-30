//go:build ee

package common

import "go.uber.org/zap"

func InitIngestLog(logType string, file FileOutput, serviceName string) error {
	return nil
}

func LogIngest(fields ...zap.Field) {

}
