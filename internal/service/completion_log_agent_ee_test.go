//go:build ee

package service

import (
	"github.com/redis/go-redis/v9"
	"ragflow/internal/agent/canvas"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"strings"
	"testing"
	"time"
)

func TestRunAgentCompletionLog(t *testing.T) {
	testDB := setupServiceTestDB(t)
	if err := testDB.AutoMigrate(
		&entity.CompletionLog{},
		&entity.UserCanvas{},
		&entity.UserCanvasVersion{},
		&entity.APIToken{},
		&entity.API4Conversation{},
		&entity.TenantModelProvider{},
		&entity.TenantModelInstance{},
		&entity.TenantModel{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	orig := dao.DB
	dao.DB = testDB
	t.Cleanup(func() { dao.DB = orig })

	dsl := map[string]any{
		"globals": map[string]any{"sys.files": []any{"stale file"}},
		"components": map[string]any{
			"begin_0": map[string]any{
				"obj": map[string]any{
					"component_name": "Begin",
					"params":         map[string]any{},
				},
				"downstream": []any{"message_0"},
			},
			"message_0": map[string]any{
				"obj": map[string]any{
					"component_name": "Message",
					"params":         map[string]any{"text": "hello {{sys.query}}"},
				},
				"upstream": []any{"begin_0"},
			},
		},
		"path": []any{"begin_0", "message_0"},
	}
	makeCanvasWithDSL(t, "canvas-hello", "user-1", "tenant-1", "v-hello", dsl)

	tracker, mr := newRunTrackerForTest(t, 30*24*time.Hour)
	cpClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = cpClient.Close() })
	cp := canvas.NewKvrocksCheckPointStoreWithClient(cpClient, 30*24*time.Hour)
	svc := NewAgentServiceWithOptions(cp, nil, tracker)
	events, err := svc.RunAgent(
		WithAgentSessionID(serviceTestContext(t, "user-1", "tenant-1"), "session-hello"),
		"canvas-hello",
		"session-hello",
		"", // draft DSL
		"world", nil)
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	messages, waiting, errs, _, done := drainAgentEvents(t, events)
	if len(errs) > 0 {
		t.Fatalf("unexpected error events: %+v", errs)
	}
	if len(waiting) > 0 {
		t.Fatalf("unexpected waiting_for_user events: %+v", waiting)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message event, got %d", len(messages))
	}
	if !strings.Contains(messages[0].Content, "hello world") {
		t.Errorf("Content = %q, want substring %q", messages[0].Content, "hello world")
	}
	if !done {
		t.Error("missing terminator done event")
	}
	var log entity.CompletionLog
	if err := testDB.Take(&log).Error; err != nil {
		t.Fatal(err)
	}
	if log.UserMessage != "world" || log.AssistantResponse != "hello world" || log.ResourceID != "canvas-hello" || log.SessionID != "session-hello" || log.Status != "completed" {
		t.Fatalf("completion log = %+v", log)
	}
}
