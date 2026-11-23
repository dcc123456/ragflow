//go:build ee

package service

import (
	"context"
	"encoding/json/v2"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"ragflow/internal/agent/canvas"
	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
)

type completionLog struct {
	db  *gorm.DB
	row entity.CompletionLog
}

func startCompletionLog(ctx context.Context, kind, resourceID, sessionID, question string) *completionLog {
	if dao.DB == nil {
		return nil
	}
	log := &completionLog{
		// Content belongs only in the table, including when a database write fails.
		db: dao.DB.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}),
		row: entity.CompletionLog{
			ID: common.GenerateToken(), UserID: permission.UserIDFromContext(ctx), TenantID: permission.TenantIDFromContext(ctx),
			ResourceType: kind, ResourceID: resourceID, SessionID: sessionID, UserMessage: question,
			Status: "running", CreatedAt: time.Now(),
		},
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := log.db.WithContext(saveCtx).Create(&log.row).Error; err != nil {
		common.Warn("completion log insert failed", zap.String("log_id", log.row.ID))
		return nil
	}
	return log
}

func (log *completionLog) finish(ctx context.Context, response, status, sessionID string) {
	if log == nil {
		return
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if status == "incomplete" && ctx.Err() != nil {
		status = "cancelled"
	}
	if err := log.db.WithContext(saveCtx).Model(&entity.CompletionLog{}).Where("id = ?", log.row.ID).Updates(map[string]any{
		"assistant_response": response, "status": status, "session_id": sessionID, "finished_at": time.Now(),
	}).Error; err != nil {
		common.Warn("completion log update failed", zap.String("log_id", log.row.ID))
	}
}

func logChatCompletion(ctx context.Context, chat *entity.Chat, messages []map[string]any, stream bool, in <-chan AsyncChatResult) <-chan AsyncChatResult {
	if dao.DB == nil || len(messages) == 0 {
		return in
	}
	question, err := NormalizeOpenAIMessageContent(messages[len(messages)-1]["content"])
	if err != nil {
		return in
	}
	sessionID := common.SessionIDFromContext(ctx)
	log := startCompletionLog(permission.WithTenantID(ctx, chat.TenantID), "chat", chat.ID, sessionID, question)
	if log == nil {
		return in
	}
	out := make(chan AsyncChatResult)
	go func() {
		defer close(out)
		var answer strings.Builder
		status := "incomplete"
		for result := range in {
			if result.Final {
				status = "completed"
				if result.Answer != "" && (!stream || result.AnswerIsAuthoritative || answer.Len() == 0) {
					answer.Reset()
					answer.WriteString(result.Answer)
				}
				if strings.Contains(result.Answer, "**ERROR**") {
					if stream && !result.AnswerIsAuthoritative && answer.String() != result.Answer {
						answer.WriteString(result.Answer)
					}
					status = "failed"
				}
			} else {
				if result.StartToThink {
					answer.WriteString("<think>")
				}
				if result.EndToThink {
					answer.WriteString("</think>")
				}
				answer.WriteString(result.Reasoning)
				answer.WriteString(result.Answer)
			}
			select {
			case out <- result:
			case <-ctx.Done():
			}
		}
		log.finish(ctx, answer.String(), status, sessionID)
	}()
	return out
}

func logAgentCompletion(ctx context.Context, agentID, sessionID string, userInput any, in <-chan canvas.RunEvent) <-chan canvas.RunEvent {
	question := agentRunQuery(userInput)
	if question == nil {
		question = userInput
	}
	log := startCompletionLog(ctx, "agent", agentID, sessionID, stringifyAgentUserInput(question))
	if log == nil {
		return in
	}
	out := make(chan canvas.RunEvent)
	go func() {
		defer close(out)
		var answer strings.Builder
		status := "incomplete"
		for event := range in {
			if event.SessionID != "" {
				sessionID = event.SessionID
			}
			switch event.Type {
			case "message":
				var message canvas.MessageEvent
				if err := json.Unmarshal([]byte(event.Data), &message); err == nil {
					if message.StartToThink {
						answer.WriteString("<think>")
					}
					if message.EndToThink {
						answer.WriteString("</think>")
					}
					answer.WriteString(message.Content)
				}
			case "workflow_finished":
				status = "completed"
			case "waiting_for_user":
				status = "waiting"
			case "error":
				status = "failed"
				var payload canvas.ErrorEvent
				if err := json.Unmarshal([]byte(event.Data), &payload); err == nil {
					if payload.Kind == canvas.RunErrorKindInternal {
						answer.WriteString(canvas.InternalRunErrorMessage)
					} else {
						answer.WriteString(payload.Message)
					}
				}
			case "cancelled":
				status = "cancelled"
			}
			select {
			case out <- event:
			case <-ctx.Done():
			}
		}
		log.finish(ctx, answer.String(), status, sessionID)
	}()
	return out
}
