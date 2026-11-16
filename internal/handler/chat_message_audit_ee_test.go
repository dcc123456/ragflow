//go:build ee

package handler

import (
	"encoding/json/v2"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
	"ragflow/internal/permission/testsupport"
	"ragflow/internal/service"
)

func TestChatMessageAuditValidationAndExclusions(t *testing.T) {
	db := setupChatHandlerTestDB(t)
	if err := testsupport.PrepareEnterprisePermissionFixture(db, "tenant", "actor"); err != nil {
		t.Fatal(err)
	}
	chat := NewChatSessionHandler(service.NewChatSessionService(), nil)
	openai := NewOpenAIChatHandler(nil)
	bot := NewBotHandler(nil)
	for _, test := range []struct {
		name, method, path, url, body, action, chatID, sessionID, messageID string
		handler                                                             gin.HandlerFunc
		anonymous                                                           bool
	}{
		{name: "chat malformed", method: "POST", path: "/api/v1/chat/completions", body: `{"question":`, action: "request_chat_completion", handler: chat.ChatCompletions},
		{name: "openai malformed", method: "POST", path: "/api/v1/openai/:chat_id/chat/completions", url: "/api/v1/openai/chat-one/chat/completions", body: `{"messages":`, action: "request_chat_completion", chatID: "chat-one", handler: openai.OpenAIChatCompletions},
		{name: "chatbot malformed", method: "POST", path: "/api/v1/chatbots/:dialog_id/completions", url: "/api/v1/chatbots/chat-one/completions", body: `{"question":`, action: "request_chatbot_completion", chatID: "chat-one", handler: bot.ChatbotCompletion},
		{name: "delete rejected", method: "DELETE", path: "/api/v1/chats/:chat_id/sessions/:session_id/messages/:msg_id", url: "/api/v1/chats/absent/sessions/session-one/messages/message-one", action: "delete_chat_message", chatID: "absent", sessionID: "session-one", messageID: "message-one", handler: chat.DeleteSessionMessage},
		{name: "feedback malformed", method: "PUT", path: "/api/v1/chats/:chat_id/sessions/:session_id/messages/:msg_id/feedback", url: "/api/v1/chats/chat-one/sessions/session-one/messages/message-one/feedback", body: `{"feedback":"private-content",`, action: "update_chat_message_feedback", chatID: "chat-one", sessionID: "session-one", messageID: "message-one", handler: chat.UpdateMessageFeedback},
		{name: "conversation alias", method: "POST", path: "/api/v1/chat/completions", body: `{"chat_id":"chat-one","conversation_id":"session-one","question":"private-content","messages":[{"role":"user","content":"private-history"}],"max_tokens":0}`, action: "request_chat_completion", chatID: "chat-one", sessionID: "session-one", handler: chat.ChatCompletions},
		{name: "session precedence", method: "POST", path: "/api/v1/chat/completions", body: `{"chat_id":"chat-one","session_id":"session-one","conversation_id":"ignored","question":"private-content","max_tokens":0}`, action: "request_chat_completion", chatID: "chat-one", sessionID: "session-one", handler: chat.ChatCompletions},
		{name: "anonymous excluded", method: "POST", path: "/api/v1/chat/completions", body: `{"question":`, handler: chat.ChatCompletions, anonymous: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var events []*common.OperationLog
			common.SetAuditLogSink(func(event *common.OperationLog) error { events = append(events, event); return nil })
			t.Cleanup(func() { common.SetAuditLogSink(nil) })
			url := test.url
			if url == "" {
				url = test.path
			}
			writer := searchAuditRequest(searchAuditRouter(test.handler, test.method, test.path, test.anonymous), test.method, url, test.body)
			if test.action == "" {
				if len(events) != 0 {
					t.Fatalf("anonymous request emitted %d events", len(events))
				}
				return
			}
			if len(events) != 1 {
				t.Fatalf("got %d events; response %s", len(events), writer.Body.String())
			}
			event := events[0]
			if event.Operation != test.action || event.UserID != "actor" || event.Resources["tenant_id"] != "tenant" || event.Resources["outcome"] != "response_error" || event.Resources["event_kind"] != "action_request" || event.Code == 0 || event.EventTime.IsZero() || event.APIPath != test.path || event.HTTPMethod != test.method || event.Resources["http_status"] != writer.Code {
				t.Fatalf("incorrect event: %+v", event)
			}
			for key, value := range map[string]string{"chat_id": test.chatID, "session_id": test.sessionID, "message_id": test.messageID} {
				if value != "" && event.Resources[key] != value {
					t.Fatalf("missing %s: %+v", key, event.Resources)
				}
			}
			assertChatMessageAuditPrivacy(t, event)
		})
	}
}

func assertChatMessageAuditPrivacy(t *testing.T, event *common.OperationLog) {
	t.Helper()
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-content", "private-history", "private-feedback", "origin", "initiator"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("private request information captured: %s", encoded)
		}
	}
	if event.RequestParams != "" || event.Error != nil {
		t.Fatalf("request payload or error captured: %+v", event)
	}
}

func TestChatMessageAuditMutations(t *testing.T) {
	db := setupChatHandlerTestDB(t)
	if err := db.AutoMigrate(&entity.ChatSession{}, &entity.ConversationMessage{}); err != nil {
		t.Fatal(err)
	}
	if err := testsupport.PrepareEnterprisePermissionFixture(db, "tenant", "actor"); err != nil {
		t.Fatal(err)
	}
	createChatHandlerTestChat(t, db, "chat-one", "tenant")
	if err := permission.NewDatabaseChecker(db).GrantResourceOwner(permission.WithIdentity(t.Context(), "actor", "tenant"), permission.ResourceRef{Kind: permission.ResourceKindChat, ID: "chat-one"}); err != nil {
		t.Fatal(err)
	}
	if err := dao.NewChatSessionDAO().Create(t.Context(), db, &entity.ChatSession{
		ID: "session-one", DialogID: "chat-one", UserID: sptr("actor"),
		Message: []byte(`[{"id":"message-one","role":"user","content":"private-content"},{"id":"message-one","role":"assistant","content":"private-history"}]`),
	}); err != nil {
		t.Fatal(err)
	}
	handler := NewChatSessionHandler(service.NewChatSessionService(), nil)
	var events []*common.OperationLog
	common.SetAuditLogSink(func(event *common.OperationLog) error { events = append(events, event); return nil })
	t.Cleanup(func() { common.SetAuditLogSink(nil) })
	path := "/api/v1/chats/:chat_id/sessions/:session_id/messages/:msg_id"
	url := "/api/v1/chats/chat-one/sessions/session-one/messages/message-one"
	for index, test := range []struct {
		method, body, action string
		handler              gin.HandlerFunc
	}{
		{method: "PUT", body: `{"thumbup":false,"feedback":"private-feedback"}`, action: "update_chat_message_feedback", handler: handler.UpdateMessageFeedback},
		{method: "DELETE", action: "delete_chat_message", handler: handler.DeleteSessionMessage},
	} {
		requestPath, requestURL := path, url
		if test.method == "PUT" {
			requestPath += "/feedback"
			requestURL += "/feedback"
		}
		writer := searchAuditRequest(searchAuditRouter(test.handler, test.method, requestPath, false), test.method, requestURL, test.body)
		var response struct {
			Code common.ErrorCode `json:"code"`
			Data struct {
				Messages []map[string]any `json:"messages"`
			} `json:"data"`
		}
		if err := json.Unmarshal(writer.Body.Bytes(), &response); err != nil || response.Code != common.CodeSuccess {
			t.Fatalf("mutation failed: %v %s", err, writer.Body.String())
		}
		if test.method == "PUT" && (len(response.Data.Messages) != 2 || response.Data.Messages[1]["feedback"] != "private-feedback") {
			t.Fatalf("feedback was not applied: %s", writer.Body.String())
		}
		if test.method == "DELETE" && len(response.Data.Messages) != 0 {
			t.Fatalf("message pair was not deleted: %s", writer.Body.String())
		}
		if len(events) != index+1 {
			t.Fatalf("expected one event per mutation, got %d", len(events))
		}
		event := events[index]
		if event.Operation != test.action || event.Code != 0 || event.APIPath != requestPath || event.HTTPMethod != test.method || event.Resources["outcome"] != "response_success" || event.Resources["chat_id"] != "chat-one" || event.Resources["session_id"] != "session-one" || event.Resources["message_id"] != "message-one" {
			t.Fatalf("incorrect mutation event: %+v", event)
		}
		assertChatMessageAuditPrivacy(t, event)
	}
}

func TestChatMessageAuditEmittedBeforeStream(t *testing.T) {
	db := setupChatHandlerTestDB(t)
	if err := testsupport.PrepareEnterprisePermissionFixture(db, "tenant", "actor"); err != nil {
		t.Fatal(err)
	}
	var events []*common.OperationLog
	common.SetAuditLogSink(func(event *common.OperationLog) error { events = append(events, event); return nil })
	t.Cleanup(func() { common.SetAuditLogSink(nil) })
	writer := &searchAuditStreamRecorder{
		ResponseRecorder: httptest.NewRecorder(), closed: make(chan bool),
		beforeWrite: func() {
			if len(events) != 1 || events[0].Resources["outcome"] != "unknown" {
				t.Fatalf("initiation must be emitted before the first stream frame: %+v", events)
			}
		},
	}
	request := httptest.NewRequest("POST", "/api/v1/chat/completions", strings.NewReader(`{"chat_id":"absent","session_id":"session-one","question":"private-content","stream":true}`))
	request.Header.Set("Content-Type", "application/json")
	handler := NewChatSessionHandler(service.NewChatSessionService(), nil)
	searchAuditRouter(handler.ChatCompletions, "POST", "/api/v1/chat/completions", false).ServeHTTP(writer, request)
	if writer.Body.Len() == 0 || len(events) != 1 {
		t.Fatalf("expected one initiation event and SSE rejection: %d %s", len(events), writer.Body.String())
	}
	event := events[0]
	if event.Operation != "request_chat_completion" || event.Resources["outcome"] != "unknown" || event.Resources["chat_id"] != "absent" || event.Resources["session_id"] != "session-one" {
		t.Fatalf("incorrect stream initiation event: %+v", event)
	}
	assertChatMessageAuditPrivacy(t, event)
}

func TestChatMessageAuditChatbotEmittedBeforeStream(t *testing.T) {
	db := setupChatHandlerTestDB(t)
	if err := db.AutoMigrate(&entity.API4Conversation{}, &entity.API4ConversationMessage{}); err != nil {
		t.Fatal(err)
	}
	if err := testsupport.PrepareEnterprisePermissionFixture(db, "tenant", "actor"); err != nil {
		t.Fatal(err)
	}
	createChatHandlerTestChat(t, db, "chat-one", "tenant")
	if err := db.Model(&entity.Chat{}).Where("id = ?", "chat-one").Update("prompt_config", entity.JSONMap{"prologue": "private-content"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := permission.NewDatabaseChecker(db).GrantResourceOwner(permission.WithIdentity(t.Context(), "actor", "tenant"), permission.ResourceRef{Kind: permission.ResourceKindChat, ID: "chat-one"}); err != nil {
		t.Fatal(err)
	}
	var events []*common.OperationLog
	common.SetAuditLogSink(func(event *common.OperationLog) error { events = append(events, event); return nil })
	t.Cleanup(func() { common.SetAuditLogSink(nil) })
	writer := &searchAuditStreamRecorder{
		ResponseRecorder: httptest.NewRecorder(), closed: make(chan bool),
		beforeWrite: func() {
			if len(events) != 1 || events[0].Resources["outcome"] != "unknown" {
				t.Fatalf("initiation must be emitted before the first stream frame: %+v", events)
			}
		},
	}
	request := httptest.NewRequest("POST", "/api/v1/chatbots/chat-one/completions", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	handler := NewBotHandler(service.NewBotService(nil))
	searchAuditRouter(handler.ChatbotCompletion, "POST", "/api/v1/chatbots/:dialog_id/completions", false).ServeHTTP(writer, request)
	if !strings.Contains(writer.Body.String(), "private-content") || !strings.Contains(writer.Body.String(), "[DONE]") || len(events) != 1 {
		t.Fatalf("expected one initiation event and opening frames: %d %s", len(events), writer.Body.String())
	}
	event := events[0]
	if event.Operation != "request_chatbot_completion" || event.Resources["outcome"] != "unknown" || event.Resources["chat_id"] != "chat-one" {
		t.Fatalf("incorrect chatbot stream initiation event: %+v", event)
	}
	assertChatMessageAuditPrivacy(t, event)
}
