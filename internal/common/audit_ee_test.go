//go:build ee

package common

import (
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestAuditDatabaseSink(t *testing.T) {
	previousLogger, previousSink := AuditLogger, auditLogSink
	t.Cleanup(func() { AuditLogger, auditLogSink = previousLogger, previousSink })
	if err := InitAudit("database", FileOutput{}, "test"); err != nil {
		t.Fatal(err)
	}
	var events []*OperationLog
	SetAuditLogSink(func(event *OperationLog) error {
		events = append(events, event)
		return nil
	})
	event := &OperationLog{UserID: "user", Operation: "create_dataset"}
	Audit(event)
	Audit(nil)
	if len(events) != 1 || events[0] != event {
		t.Fatalf("expected one unchanged event, got %#v", events)
	}
}

func TestAuditFileSink(t *testing.T) {
	previousLogger, previousSink := AuditLogger, auditLogSink
	t.Cleanup(func() { AuditLogger, auditLogSink = previousLogger, previousSink })
	core, observed := observer.New(zap.InfoLevel)
	AuditLogger, auditLogSink = zap.New(core), nil
	Audit(&OperationLog{UserID: "user", Operation: "update_dataset"})
	entries := observed.All()
	if len(entries) != 1 || entries[0].ContextMap()["operation"] != "update_dataset" {
		t.Fatalf("expected one file logger event, got %#v", entries)
	}
}
