//go:build ee

package common

import (
	"time"

	"go.uber.org/zap"
)

type IngestionLog struct {
	EventTime  time.Time `json:"event_time"`
	TenantID   string    `json:"tenant_id"`
	DatasetID  string    `json:"dataset_id"`
	DocID      string    `json:"doc_id"`
	UserID     string    `json:"user_id"`
	UserName   string    `json:"user_name,omitempty"`
	TenantName string    `json:"tenant_name,omitempty"`
	Worker     string    `json:"worker"`
	TaskID     string    `json:"task_id"`
	EventType  string    `json:"event_type"` // created|scheduled|running|completed|failed|stopped
	Status     string    `json:"status"`
	// ErrorInfo is the terminal reason as a plain string (not an error
	// interface) so the event serializes cleanly and portably to external
	// dashboards / ClickHouse.
	ErrorInfo      string    `json:"error_info,omitempty"`
	Filename       string    `json:"filename"`
	FileType       string    `json:"file_type"`
	FileSize       int64     `json:"file_size"`
	ParserID       string    `json:"parser_id"`
	UploadTime     time.Time `json:"upload_time"`
	EnqueueTime    time.Time `json:"enqueue_time"`
	BeginTime      time.Time `json:"begin_time"`
	EndTime        time.Time `json:"end_time"`
	DurationMS     int64     `json:"duration_ms"`
	WaitDurationS  float64   `json:"wait_duration_s"`
	ParseDurationS float64   `json:"parse_duration_s"`
	Progress       float64   `json:"progress"`
	RunCount       int       `json:"run_count"`
	// Code is a machine-readable category for grouping in dashboards. It
	// defaults to 0 when the caller does not set it.
	Code      int            `json:"code"`
	Operation map[string]any `json:"operation,omitempty"`
}

func InitIngestLog(logType string, file FileOutput, serviceName string) error {
	return nil
}

// SyncIngestLog satisfies the shared shutdown hook; the EE sink has no local buffer.
func SyncIngestLog() {}

func LogIngest(_ *IngestionLog, fields ...zap.Field) {

}

func LogIngestEvent(log IngestionLog) {
}
