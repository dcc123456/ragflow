//go:build ee

package handler

import (
	"encoding/json/v2"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/permission/testsupport"
	"ragflow/internal/service"
)

func TestChatManagementAuditValidationAndExclusions(t *testing.T) {
	chat := &ChatHandler{}
	session := &ChatSessionHandler{}
	for _, test := range []struct {
		name, method, path, url, body, action string
		handler                               gin.HandlerFunc
		anonymous                             bool
	}{
		{"create malformed", "POST", "/api/v1/chats", "/api/v1/chats", `{"name":"private-content",`, "create_chat", chat.Create, false},
		{"put malformed", "PUT", "/api/v1/chats/:chat_id", "/api/v1/chats/chat-one", `{"name":"private-content",`, "update_chat", chat.UpdateChat, false},
		{"patch malformed", "PATCH", "/api/v1/chats/:chat_id", "/api/v1/chats/chat-one", `{"name":"private-content",`, "update_chat", chat.PatchChat, false},
		{"delete missing identifier", "DELETE", "/api/v1/chats", "/api/v1/chats", "", "delete_chat", chat.DeleteChat, false},
		{"bulk delete malformed", "DELETE", "/api/v1/chats", "/api/v1/chats", `{"ids":`, "delete_chats", chat.BulkDeleteChats, false},
		{"create session malformed", "POST", "/api/v1/chats/:chat_id/sessions", "/api/v1/chats/chat-one/sessions", `{"name":"private-content",`, "create_chat_session", session.CreateSession, false},
		{"update session malformed", "PATCH", "/api/v1/chats/:chat_id/sessions/:session_id", "/api/v1/chats/chat-one/sessions/session-one", `{"name":"private-content",`, "update_chat_session", session.UpdateSession, false},
		{"delete sessions malformed", "DELETE", "/api/v1/chats/:chat_id/sessions", "/api/v1/chats/chat-one/sessions", `{"ids":`, "delete_chat_sessions", session.DeleteSessions, false},
		{"read excluded", "GET", "/api/v1/chats", "/api/v1/chats", "", "", chat.GetChat, false},
		{"feedback malformed", "PUT", "/api/v1/chats/:chat_id/sessions/:session_id/messages/:msg_id/feedback", "/api/v1/chats/chat-one/sessions/session-one/messages/message-one/feedback", `{`, "update_chat_message_feedback", session.UpdateMessageFeedback, false},
		{"anonymous excluded", "POST", "/api/v1/chats", "/api/v1/chats", `{`, "", chat.Create, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var events []*common.OperationLog
			common.SetAuditLogSink(func(event *common.OperationLog) error { events = append(events, event); return nil })
			t.Cleanup(func() { common.SetAuditLogSink(nil) })
			writer := searchAuditRequest(searchAuditRouter(test.handler, test.method, test.path, test.anonymous), test.method, test.url, test.body)
			if test.action == "" {
				if len(events) != 0 {
					t.Fatalf("excluded request emitted %d events", len(events))
				}
				return
			}
			if len(events) != 1 {
				t.Fatalf("expected one event, got %d: %s", len(events), writer.Body.String())
			}
			event := events[0]
			if event.Operation != test.action || event.UserID != "actor" || event.Resources["tenant_id"] != "tenant" ||
				event.Resources["outcome"] != "response_error" || event.Resources["event_kind"] != "action_request" ||
				event.Code == 0 || event.APIPath != test.path || event.HTTPMethod != test.method ||
				event.Resources["http_status"] != writer.Code || event.Resources["trace_id"] == "" || event.EventTime.IsZero() {
				t.Fatalf("incorrect event: %+v", event)
			}
			if strings.Contains(test.path, ":chat_id") && event.Resources["chat_id"] != "chat-one" {
				t.Fatalf("missing chat identifier: %+v", event.Resources)
			}
			if strings.Contains(test.path, ":session_id") && event.Resources["session_id"] != "session-one" {
				t.Fatalf("missing session identifier: %+v", event.Resources)
			}
			encoded, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{"private-content", "origin", "initiator"} {
				if strings.Contains(string(encoded), secret) {
					t.Fatalf("private request information captured: %s", encoded)
				}
			}
			if event.RequestParams != "" || event.Error != nil {
				t.Fatalf("request payload or error captured: %+v", event)
			}
		})
	}
}

func TestChatManagementAuditCRUDAndBatches(t *testing.T) {
	db := setupChatHandlerTestDB(t)
	if err := db.AutoMigrate(&entity.ChatSession{}, &entity.ConversationMessage{}); err != nil {
		t.Fatal(err)
	}
	if err := testsupport.PrepareEnterprisePermissionFixture(db, "tenant", "actor"); err != nil {
		t.Fatal(err)
	}
	chat := NewChatHandler(service.NewChatService(), service.NewUserService())
	session := NewChatSessionHandler(service.NewChatSessionService(), nil)
	var events []*common.OperationLog
	common.SetAuditLogSink(func(event *common.OperationLog) error { events = append(events, event); return nil })
	t.Cleanup(func() { common.SetAuditLogSink(nil) })
	run := func(method, path, url, body, action string, handler gin.HandlerFunc) (*common.OperationLog, *httptest.ResponseRecorder) {
		t.Helper()
		before := len(events)
		writer := searchAuditRequest(searchAuditRouter(handler, method, path, false), method, url, body)
		var response struct {
			Code common.ErrorCode `json:"code"`
		}
		if err := json.Unmarshal(writer.Body.Bytes(), &response); err != nil || response.Code != common.CodeSuccess {
			t.Fatalf("%s %s failed: %v %s", method, url, err, writer.Body.String())
		}
		if len(events) != before+1 {
			t.Fatalf("expected one event for %s, got %d: %s", action, len(events)-before, writer.Body.String())
		}
		event := events[before]
		if event.Operation != action || event.Code != 0 || event.Resources["outcome"] != "response_success" {
			t.Fatalf("incorrect successful event: %+v", event)
		}
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "private-content") || event.RequestParams != "" || event.Error != nil {
			t.Fatalf("private request or response information captured: %s", encoded)
		}
		return event, writer
	}

	event, writer := run("POST", "/api/v1/chats", "/api/v1/chats", `{"name":"private-content","llm_id":""}`, "create_chat", chat.Create)
	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(writer.Body.Bytes(), &created); err != nil || created.Data.ID == "" {
		t.Fatalf("missing created chat identifier: %v %s", err, writer.Body.String())
	}
	chatID := created.Data.ID
	if event.Resources["chat_id"] != chatID {
		t.Fatalf("generated chat identifier missing: %+v", event.Resources)
	}
	for _, update := range []struct {
		method  string
		handler gin.HandlerFunc
	}{{"PUT", chat.UpdateChat}, {"PATCH", chat.PatchChat}} {
		event, _ = run(update.method, "/api/v1/chats/:chat_id", "/api/v1/chats/"+chatID, `{"name":"renamed-private-content"}`, "update_chat", update.handler)
		if event.Resources["chat_id"] != chatID {
			t.Fatalf("updated chat identifier missing: %+v", event.Resources)
		}
	}
	event, writer = run("POST", "/api/v1/chats/:chat_id/sessions", "/api/v1/chats/"+chatID+"/sessions", `{"name":"session-private-content"}`, "create_chat_session", session.CreateSession)
	if err := json.Unmarshal(writer.Body.Bytes(), &created); err != nil || created.Data.ID == "" {
		t.Fatalf("missing created session identifier: %v %s", err, writer.Body.String())
	}
	sessionID := created.Data.ID
	if event.Resources["chat_id"] != chatID || event.Resources["session_id"] != sessionID {
		t.Fatalf("generated session identifiers missing: %+v", event.Resources)
	}
	for _, body := range []string{`{"name":"renamed-private-content"}`, `{}`} {
		event, _ = run("PATCH", "/api/v1/chats/:chat_id/sessions/:session_id", "/api/v1/chats/"+chatID+"/sessions/"+sessionID, body, "update_chat_session", session.UpdateSession)
		if event.Resources["chat_id"] != chatID || event.Resources["session_id"] != sessionID {
			t.Fatalf("updated session identifiers missing: %+v", event.Resources)
		}
	}
	event, _ = run("DELETE", "/api/v1/chats/:chat_id/sessions", "/api/v1/chats/"+chatID+"/sessions", `{}`, "delete_chat_sessions", session.DeleteSessions)
	if event.Resources["delete_all"] != false {
		t.Fatalf("no-op session deletion metadata incorrect: %+v", event.Resources)
	}
	event, writer = run("DELETE", "/api/v1/chats/:chat_id/sessions", "/api/v1/chats/"+chatID+"/sessions", `{"ids":["`+sessionID+`","missing",7,"",null,{"content":"private-content"}]}`, "delete_chat_sessions", session.DeleteSessions)
	if !strings.Contains(writer.Body.String(), `"success_count":1`) ||
		!reflect.DeepEqual(event.Resources["session_ids"], []string{sessionID, "missing"}) || event.Resources["delete_all"] != false {
		t.Fatalf("partial session deletion metadata incorrect: %+v %s", event.Resources, writer.Body.String())
	}
	event, _ = run("DELETE", "/api/v1/chats/:chat_id/sessions", "/api/v1/chats/"+chatID+"/sessions", `{"delete_all":true}`, "delete_chat_sessions", session.DeleteSessions)
	if event.Resources["delete_all"] != true || event.Resources["chat_id"] != chatID {
		t.Fatalf("delete-all session metadata incorrect: %+v", event.Resources)
	}
	event, _ = run("DELETE", "/api/v1/chats/:chat_id", "/api/v1/chats/"+chatID, "", "delete_chat", chat.DeleteChat)
	if event.Resources["chat_id"] != chatID {
		t.Fatalf("deleted chat identifier missing: %+v", event.Resources)
	}

	_, writer = run("POST", "/api/v1/chats", "/api/v1/chats", `{"name":"batch-private-content","llm_id":""}`, "create_chat", chat.Create)
	if err := json.Unmarshal(writer.Body.Bytes(), &created); err != nil || created.Data.ID == "" {
		t.Fatalf("missing batch chat identifier: %v %s", err, writer.Body.String())
	}
	batchID := created.Data.ID
	event, writer = run("DELETE", "/api/v1/chats", "/api/v1/chats", `{"ids":["`+batchID+`","missing","`+batchID+`"]}`, "delete_chats", chat.BulkDeleteChats)
	if !strings.Contains(writer.Body.String(), `"success_count":1`) ||
		!reflect.DeepEqual(event.Resources["chat_ids"], []string{batchID, "missing", batchID}) || event.Resources["delete_all"] != false {
		t.Fatalf("partial chat deletion metadata incorrect: %+v %s", event.Resources, writer.Body.String())
	}
	run("DELETE", "/api/v1/chats", "/api/v1/chats", "", "delete_chats", chat.BulkDeleteChats)
	event, _ = run("DELETE", "/api/v1/chats", "/api/v1/chats", `{"delete_all":true}`, "delete_chats", chat.BulkDeleteChats)
	if event.Resources["delete_all"] != true {
		t.Fatalf("delete-all chat metadata incorrect: %+v", event.Resources)
	}

	createChatHandlerTestChat(t, db, "foreign-chat", "foreign-tenant")
	before := len(events)
	writer = searchAuditRequest(searchAuditRouter(chat.DeleteChat, "DELETE", "/api/v1/chats/:chat_id", false), "DELETE", "/api/v1/chats/foreign-chat", "")
	if len(events) != before+1 || events[before].Resources["outcome"] != "response_error" ||
		events[before].Resources["chat_id"] != "foreign-chat" || events[before].Code != int(common.CodeNotFound) {
		t.Fatalf("permission denial event incorrect: %+v %s", events[before:], writer.Body.String())
	}
	before = len(events)
	searchAuditRequest(searchAuditRouter(session.DeleteSessionMessage, "DELETE", "/api/v1/chats/:chat_id/sessions/:session_id/messages/:msg_id", false), "DELETE", "/api/v1/chats/foreign-chat/sessions/session-one/messages/message-one", "")
	if len(events) != before+1 || events[before].Operation != "delete_chat_message" ||
		events[before].Resources["outcome"] != "response_error" || events[before].Resources["chat_id"] != "foreign-chat" ||
		events[before].Resources["session_id"] != "session-one" || events[before].Resources["message_id"] != "message-one" ||
		events[before].Code != int(common.CodeNotFound) {
		t.Fatalf("message deletion event incorrect: %+v", events[before:])
	}
}
