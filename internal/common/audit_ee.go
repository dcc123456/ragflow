//go:build ee

package common

import (
	"errors"
	"path/filepath"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	AuditLogger *zap.Logger
)

func InitAudit(logType string, file FileOutput, serviceName string) error {
	switch logType {
	case "database":
		return initDatabaseAuditLog()
	case "file":
		return initFileAuditLog(file, serviceName)
	default:
		return errors.New("invalid audit log type")
	}
}

func initDatabaseAuditLog() error {
	return nil
}

func initFileAuditLog(file FileOutput, serviceName string) error {
	// JSON encoder: only the message key plus the structured fields supplied
	// at call sites. No timestamp, level, pid, or service name is emitted.
	encoderConfig := zapcore.EncoderConfig{
		MessageKey: "",
		LineEnding: zapcore.DefaultLineEnding,
	}

	// File destination: use the configured path when present, otherwise fall
	// back to "<serviceName>.log" in the working directory.
	var logPath string
	var filename string
	if file.Path != "" {
		filename = file.Path
	} else {
		filename = serviceName + ".log"
	}
	logPath = filepath.Join("logs", filename)

	maxSize := file.MaxSize
	if maxSize <= 0 {
		maxSize = DefaultLogMaxSizeMB
	}
	maxBackups := file.MaxBackups
	if maxBackups <= 0 {
		maxBackups = DefaultLogMaxBackups
	}
	maxAge := file.MaxAge
	if maxAge <= 0 {
		maxAge = DefaultLogMaxAgeDays
	}

	ljLogger := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		MaxAge:     maxAge,
		LocalTime:  true,
	}

	// No level gating: accept every entry.
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		zapcore.AddSync(ljLogger),
		zapcore.DebugLevel,
	)

	AuditLogger = zap.New(core, zap.AddCallerSkip(1))
	return nil
}

func Audit(operationLog *OperationLog, fields ...zap.Field) {
	fields = append(fields,
		zap.Time("time", operationLog.EventTime),
		zap.String("user_id", operationLog.UserID),
		zap.String("user_name", operationLog.UserName),
		zap.String("ip_address", operationLog.IPAddress),
		zap.String("operation", operationLog.Operation),
		zap.String("api_path", operationLog.APIPath),
		zap.String("http_method", operationLog.HTTPMethod),
		zap.String("resource_type", operationLog.ResourceType),
		zap.String("resource_id", operationLog.ResourceID),
		zap.String("resource_name", operationLog.ResourceName),
		zap.String("request_params", operationLog.RequestParams),
		zap.Int("code", operationLog.Code),
		zap.Error(operationLog.Error),
		zap.Int64("duration_ms", operationLog.DurationMS),
	)
	AuditLogger.Info("", fields...)
}
