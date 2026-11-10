//
//  Copyright (c) 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//
//  This file is part of the RAGFlow Enterprise Edition and is licensed,
//  not sold, under the RAGFlow Enterprise Commercial License Agreement.
//  Unauthorized copying, modification, distribution, or disclosure of this
//  file, via any medium, is strictly prohibited.
//
//  Portions of this file may incorporate code from RAGFlow Community Edition,
//  which is licensed under the Apache License, Version 2.0.
//
//  For commercial licensing inquiries: sales@infiniflow.ai
//

//go:build ee

package common

import (
	"go.uber.org/zap"
)

// ingestLogSink is the EE ingest-log destination. It is nil by default, which
// makes LogIngest a no-op; the ClickHouse collector installs itself via
// SetIngestLogSink during server startup. The sink type returns an error so the
// collector can signal backpressure (a full channel) without failing the caller.
var ingestLogSink func(IngestionLog) error

// SetIngestLogSink installs the EE ingest-log destination (the ClickHouse
// collector). Passing nil restores the default no-op behavior. It is safe to call
// once at startup before any events are emitted.
func SetIngestLogSink(sink func(IngestionLog) error) {
	ingestLogSink = sink
}

func InitIngestLog(logType string, file FileOutput, serviceName string) error {
	return nil
}

// SyncIngestLog satisfies the shared shutdown hook; the EE sink has no local buffer.
func SyncIngestLog() {}

// LogIngest forwards one structured ingestion lifecycle event to the installed
// EE sink (the ClickHouse collector). With no sink registered it is a no-op, so a
// missing or failed ClickHouse connection can never break the ingestion
// pipeline. The signature matches the OSS stub so shared callers compile under
// both build tags.
func LogIngest(log *IngestionLog, fields ...zap.Field) {
	if ingestLogSink == nil {
		return
	}
	// Copy the Operation map before enriching it with EE-only dashboard
	// dimensions. Operation is a reference type, so writing derived keys onto
	// log.Operation would mutate the caller's struct; a future caller that reuses
	// one IngestionLog across multiple LogIngest emits would then accumulate
	// stale derived keys. Copying decouples this shared seam from caller
	// lifetime. The sink receives the enriched copy via a value pass, so the
	// caller's struct is never touched.
	op := make(map[string]any, len(log.Operation)+5)
	for k, v := range log.Operation {
		op[k] = v
	}
	// EE-only dashboard dimensions. These live in the flexible Operation JSON
	// column so the shared schema / OSS files are untouched: the post-merge UI
	// joins by user_id / tenant_id and renders these derived labels.
	op["event_label"] = ingestEventLabel(log.EventType)
	op["terminal"] = ingestIsTerminal(log.EventType)
	op["has_error"] = log.Status == "FAILED" || log.ErrorInfo != ""
	if log.TenantName != "" {
		op["tenant_name"] = log.TenantName
	}
	if log.UserName != "" {
		op["user_name"] = log.UserName
	}
	out := *log
	out.Operation = op
	_ = ingestLogSink(out)
}

// ingestEventLabel returns a human-readable label for an ingestion event type.
func ingestEventLabel(eventType string) string {
	switch eventType {
	case "created":
		return "Created"
	case "scheduled":
		return "Scheduled"
	case "running":
		return "Running"
	case "completed":
		return "Completed"
	case "failed":
		return "Failed"
	case "stopped":
		return "Stopped"
	default:
		return eventType
	}
}

// ingestIsTerminal reports whether an event type ends a document's lifecycle.
func ingestIsTerminal(eventType string) bool {
	switch eventType {
	case "completed", "failed", "stopped":
		return true
	default:
		return false
	}
}
