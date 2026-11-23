//go:build ee

package service

import (
	"context"
	"encoding/json/v2"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"ragflow/internal/agent/canvas"
	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
)

func completionLogTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&entity.CompletionLog{}); err != nil {
		t.Fatal(err)
	}
	previous := dao.DB
	dao.DB = db
	t.Cleanup(func() { dao.DB = previous })
	return db
}

func TestChatCompletionLog(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "nonstream", true: "stream"}[stream], func(t *testing.T) {
			db := completionLogTestDB(t)
			ctx := common.WithSessionID(permission.WithIdentity(t.Context(), "user", "tenant"), "session")
			messages := []map[string]any{{"role": "user", "content": "old question"}, {"role": "assistant", "content": "old answer"}, {"role": "user", "content": "你好"}}
			in := make(chan AsyncChatResult, 3)
			if stream {
				in <- AsyncChatResult{Answer: "draft", Reasoning: "private reasoning"}
			}
			in <- AsyncChatResult{Answer: "final answer", Final: true, AnswerIsAuthoritative: true}
			close(in)
			out := logChatCompletion(ctx, &entity.Chat{ID: "chat", TenantID: "tenant"}, messages, stream, in)
			count := 0
			for range out {
				count++
			}
			wantCount := 1
			if stream {
				wantCount = 2
			}
			if count != wantCount {
				t.Fatalf("events = %d", count)
			}
			var rows []entity.CompletionLog
			if err := db.Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 {
				t.Fatalf("rows = %d", len(rows))
			}
			row := rows[0]
			if row.UserMessage != "你好" || row.AssistantResponse != "final answer" || row.UserID != "user" || row.TenantID != "tenant" || row.ResourceID != "chat" || row.ResourceType != "chat" || row.SessionID != "session" || row.Status != "completed" || row.FinishedAt == nil {
				t.Fatalf("row = %+v", row)
			}
		})
	}
}

func TestAgentCompletionLogCancellation(t *testing.T) {
	db := completionLogTestDB(t)
	ctx, cancel := context.WithCancel(permission.WithIdentity(t.Context(), "user", "tenant"))
	in := make(chan canvas.RunEvent, 2)
	in <- canvas.RunEvent{Type: "message", Data: `{"content":"partial", "thinking":"private"}`, SessionID: "session"}
	out := logAgentCompletion(ctx, "agent", "", "question", in)
	<-out
	cancel()
	close(in)
	for range out {
	}
	var row entity.CompletionLog
	if err := db.Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.UserMessage != "question" || row.AssistantResponse != "partial" || row.ResourceType != "agent" || row.SessionID != "session" || row.Status != "cancelled" {
		t.Fatalf("row = %+v", row)
	}
}

func TestAgentCompletionLog(t *testing.T) {
	db := completionLogTestDB(t)
	in := make(chan canvas.RunEvent, 4)
	in <- canvas.RunEvent{Type: "node_finished", Data: `{"outputs":{"content":"internal"}}`}
	in <- canvas.RunEvent{Type: "message", Data: `{"content":"hello "}`}
	in <- canvas.RunEvent{Type: "message", Data: `{"content":"world"}`}
	in <- canvas.RunEvent{Type: "workflow_finished"}
	close(in)
	for range logAgentCompletion(permission.WithIdentity(t.Context(), "user", "tenant"), "agent", "session", map[string]any{"query": "question"}, in) {
	}
	var row entity.CompletionLog
	if err := db.Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.UserMessage != "question" || row.AssistantResponse != "hello world" || row.Status != "completed" {
		t.Fatalf("row = %+v", row)
	}
}

func TestCompletionLogTerminalStatusSurvivesRequestCancellation(t *testing.T) {
	for _, status := range []string{"completed", "failed", "waiting"} {
		t.Run(status, func(t *testing.T) {
			db := completionLogTestDB(t)
			ctx, cancel := context.WithCancel(t.Context())
			log := startCompletionLog(ctx, "chat", "chat", "session", "question")
			cancel()
			log.finish(ctx, "response", status, "session")
			var row entity.CompletionLog
			if err := db.Take(&row).Error; err != nil {
				t.Fatal(err)
			}
			if row.Status != status || row.AssistantResponse != "response" {
				t.Fatalf("row = %+v", row)
			}
		})
	}
}

func TestCompletionLogWriteFailurePreservesEvents(t *testing.T) {
	db := completionLogTestDB(t)
	if err := db.Migrator().DropTable(&entity.CompletionLog{}); err != nil {
		t.Fatal(err)
	}
	in := make(chan AsyncChatResult, 1)
	in <- AsyncChatResult{Answer: "answer", Final: true}
	close(in)
	out := logChatCompletion(t.Context(), &entity.Chat{ID: "chat"}, []map[string]any{{"role": "user", "content": "question"}}, true, in)
	if out != in {
		t.Fatal("failed logging changed the result channel")
	}
	if result := <-out; result.Answer != "answer" {
		t.Fatalf("result = %+v", result)
	}
}

func TestAsyncChatCreatesCompletionLog(t *testing.T) {
	db := completionLogTestDB(t)
	ctx := permission.WithIdentity(t.Context(), "user", "tenant")
	results, err := NewChatPipelineService().AsyncChat(ctx, &entity.Chat{ID: "chat", TenantID: "tenant"}, []map[string]any{{"role": "user", "content": "question"}}, false, map[string]any{"store_history_messages": false})
	if err != nil {
		t.Fatal(err)
	}
	for range results {
	}
	var row entity.CompletionLog
	if err := db.Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	// An unconfigured model fails inside the real pipeline without any external calls.
	if row.UserMessage != "question" || row.Status != "failed" || row.AssistantResponse == "" {
		t.Fatalf("row = %+v", row)
	}
}

func TestAgentCompletionLogThinkingAndError(t *testing.T) {
	db := completionLogTestDB(t)
	in := make(chan canvas.RunEvent, 8)
	for _, message := range buildAgentMessageEvents("answer", "thinking", nil) {
		data, err := json.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		in <- canvas.RunEvent{Type: "message", Data: string(data)}
	}
	in <- canvas.RunEvent{Type: "error", Data: `{"message":"private storage details", "kind":"internal"}`}
	close(in)
	for range logAgentCompletion(t.Context(), "agent", "session", "question", in) {
	}
	var row entity.CompletionLog
	if err := db.Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	want := "<think>thinking</think>answer" + canvas.InternalRunErrorMessage
	if row.AssistantResponse != want || row.Status != "failed" {
		t.Fatalf("row = %+v", row)
	}
}

func TestChatCompletionLogPartialThinking(t *testing.T) {
	db := completionLogTestDB(t)
	ctx, cancel := context.WithCancel(t.Context())
	in := make(chan AsyncChatResult, 2)
	in <- AsyncChatResult{StartToThink: true, Reasoning: "thinking"}
	in <- AsyncChatResult{EndToThink: true, Answer: "partial"}
	out := logChatCompletion(ctx, &entity.Chat{ID: "chat"}, []map[string]any{{"role": "user", "content": "question"}}, true, in)
	<-out
	<-out
	cancel()
	close(in)
	for range out {
	}
	var row entity.CompletionLog
	if err := db.Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.AssistantResponse != "<think>thinking</think>partial" || row.Status != "cancelled" {
		t.Fatalf("row = %+v", row)
	}
}

func TestChatCompletionLogPartialFailure(t *testing.T) {
	db := completionLogTestDB(t)
	in := make(chan AsyncChatResult, 2)
	in <- AsyncChatResult{Answer: "partial"}
	in <- AsyncChatResult{Answer: "**ERROR**: model failed", Final: true}
	close(in)
	for range logChatCompletion(t.Context(), &entity.Chat{ID: "chat"}, []map[string]any{{"role": "user", "content": "question"}}, true, in) {
	}
	var row entity.CompletionLog
	if err := db.Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.AssistantResponse != "partial**ERROR**: model failed" || row.Status != "failed" {
		t.Fatalf("row = %+v", row)
	}
}
