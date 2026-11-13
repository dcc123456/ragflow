//go:build ee

package handler

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"ragflow/internal/agent/canvas"
	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
	"ragflow/internal/service"

	"github.com/gin-gonic/gin"
)

func serveAgentAuditRequest(t *testing.T, ctx context.Context, method, route, url, body string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("user", &entity.User{ID: "user-1"})
		c.Set("user_id", "user-1")
		c.Set("tenant_id", permission.TenantIDFromContext(ctx))
	})
	engine.Handle(method, route, handler)
	request := httptest.NewRequest(method, url, strings.NewReader(body)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	writer := httptest.NewRecorder()
	engine.ServeHTTP(writer, request)
	return writer
}

func captureAgentAudit(t *testing.T) *[]*common.OperationLog {
	t.Helper()
	var events []*common.OperationLog
	common.SetAuditLogSink(func(event *common.OperationLog) error { events = append(events, event); return nil })
	t.Cleanup(func() { common.SetAuditLogSink(nil) })
	return &events
}

func assertAgentAudit(t *testing.T, events []*common.OperationLog, action string, want map[string]any) {
	t.Helper()
	if len(events) != 1 {
		t.Fatalf("expected one audit event, got %d", len(events))
	}
	event := events[0]
	if event.Operation != action || event.UserID != "user-1" || event.EventTime.IsZero() {
		t.Fatalf("incorrect event: %+v", event)
	}
	for key, value := range want {
		if !reflect.DeepEqual(event.Resources[key], value) {
			t.Fatalf("%s: got %#v, want %#v", key, event.Resources[key], value)
		}
	}
	encoded, err := json.Marshal(event)
	if err != nil || strings.Contains(string(encoded), "secret") || event.RequestParams != "" || event.Error != nil {
		t.Fatalf("unsafe event: %s %v", encoded, err)
	}
}

func TestAgentActionHandlersAuditRejectedRequests(t *testing.T) {
	fixture, _, _ := setupGinContextWithUserAndDB(t, "POST", "/")
	h := NewAgentHandler(t.Context(), service.NewAgentService(), nil)
	h.chatRunner = &stubChatRunner{err: errors.New("secret")}
	bot := &BotHandler{botService: &stubBotService{}}
	for _, test := range []struct {
		method, route, action string
		handler               gin.HandlerFunc
	}{
		{"POST", "/api/v1/agents", "create_agent", h.CreateAgent},
		{"PUT", "/api/v1/agents/:canvas_id", "update_agent", h.UpdateAgent},
		{"DELETE", "/api/v1/agents/:canvas_id", "delete_agent", h.DeleteAgent},
		{"POST", "/api/v1/agents/:canvas_id/run", "request_agent_run", h.RunAgent},
		{"POST", "/api/v1/agents/:canvas_id/publish", "publish_agent", h.PublishAgent},
		{"PUT", "/api/v1/agents/:canvas_id/tags", "update_agent_tags", h.UpdateAgentTags},
		{"POST", "/api/v1/agents/:canvas_id/reset", "reset_agent", h.ResetAgent},
		{"POST", "/api/v1/agents/:canvas_id/components/:component_id/debug", "request_agent_component_debug", h.DebugComponent},
		{"PATCH", "/api/v1/agents/:canvas_id/versions/:version_id", "update_agent_version", h.PatchVersion},
		{"DELETE", "/api/v1/agents/:canvas_id/versions/:version_id", "delete_agent_version", h.DeleteVersion},
		{"POST", "/api/v1/agents/:canvas_id/versions/:version_id/apply", "apply_agent_version", h.ApplyVersion},
		{"POST", "/api/v1/agents/:canvas_id/sessions", "create_agent_session", h.CreateAgentSession},
		{"DELETE", "/api/v1/agents/:canvas_id/sessions", "delete_agent_sessions", h.DeleteAgentSession},
		{"DELETE", "/api/v1/agents/:canvas_id/sessions/:session_id", "delete_agent_session", h.DeleteAgentSession},
		{"POST", "/api/v1/tasks/:session_id/cancel", "request_agent_run_cancel", h.CancelSessionRun},
		{"POST", "/api/v1/agents/:canvas_id/upload", "upload_agent_files", h.UploadAgentFile},
		{"POST", "/api/v1/agents/chat/completions", "request_agent_chat_completion", h.AgentChatCompletions},
		{"POST", "/api/v1/agentbots/:agent_id/completions", "request_agentbot_completion", bot.AgentbotCompletion},
		{"POST", "/api/v1/agents/:canvas_id/webhook/credentials", "create_agent_webhook_credential", (&AgentHandler{}).CreateWebhookCredential},
		{"DELETE", "/api/v1/agents/:canvas_id/webhook/credentials", "revoke_agent_webhook_credential", (&AgentHandler{webhookCredentials: &fakeAgentWebhookCredentialManager{}}).RevokeWebhookCredential},
		{"POST", "/api/v1/agents/test-db-connection", "test_agent_db_connection", h.TestDBConnection},
		{"POST", "/api/v1/agents/:canvas_id/webhook/test", "request_agent_webhook_test", (&AgentHandler{}).Webhook},
		{"GET", "/api/v1/agents/:canvas_id/webhook/test", "request_agent_webhook_test", (&AgentHandler{}).Webhook},
		{"PUT", "/api/v1/agents/:canvas_id/webhook/test", "request_agent_webhook_test", (&AgentHandler{}).Webhook},
		{"PATCH", "/api/v1/agents/:canvas_id/webhook/test", "request_agent_webhook_test", (&AgentHandler{}).Webhook},
		{"DELETE", "/api/v1/agents/:canvas_id/webhook/test", "request_agent_webhook_test", (&AgentHandler{}).Webhook},
		{"HEAD", "/api/v1/agents/:canvas_id/webhook/test", "request_agent_webhook_test", (&AgentHandler{}).Webhook},
	} {
		t.Run(test.method+" "+test.route, func(t *testing.T) {
			events := captureAgentAudit(t)
			url := strings.NewReplacer(":canvas_id", "missing", ":agent_id", "missing", ":session_id", "session", ":version_id", "version", ":component_id", "component").Replace(test.route)
			serveAgentAuditRequest(t, fixture.Request.Context(), test.method, test.route, url, `{"password":"secret",`, test.handler)
			assertAgentAudit(t, *events, test.action, map[string]any{"tenant_id": "user-1"})
			if (*events)[0].APIPath != test.route || (*events)[0].HTTPMethod != test.method {
				t.Fatalf("incorrect request identity: %+v", (*events)[0])
			}
		})
	}
}

func TestAgentAuditCreatedResourcesAndSaveFlags(t *testing.T) {
	fixture, _, db := setupGinContextWithUserAndDB(t, "POST", "/")
	seedAgentCreationContext(t, fixture, db)
	h := NewAgentHandler(t.Context(), service.NewAgentService(), nil)
	events := captureAgentAudit(t)
	writer := serveAgentAuditRequest(t, fixture.Request.Context(), "POST", "/api/v1/agents", "/api/v1/agents", `{"title":"Agent","dsl":{"graph":{"nodes":[],"edges":[]},"prompt":"secret"}}`, h.CreateAgent)
	var created struct {
		Code int               `json:"code"`
		Data entity.UserCanvas `json:"data"`
	}
	if err := json.Unmarshal(writer.Body.Bytes(), &created); err != nil || created.Code != 0 || created.Data.ID == "" {
		t.Fatalf("create response: %s %v", writer.Body.String(), err)
	}
	id := created.Data.ID
	assertAgentAudit(t, *events, "create_agent", map[string]any{"agent_id": id, "outcome": "response_success"})
	*events = nil
	writer = serveAgentAuditRequest(t, fixture.Request.Context(), "PUT", "/api/v1/agents/:canvas_id", "/api/v1/agents/"+id, `{"autosave":true,"release":"false","dsl":{"graph":{"nodes":[],"edges":[]},"prompt":"secret"}}`, h.UpdateAgent)
	assertAgentAudit(t, *events, "update_agent", map[string]any{"agent_id": id, "release_requested": true, "autosave": true, "outcome": "response_success"})
	*events = nil
	writer = serveAgentAuditRequest(t, fixture.Request.Context(), "POST", "/api/v1/agents/:canvas_id/publish", "/api/v1/agents/"+id+"/publish", `{}`, h.PublishAgent)
	var published struct {
		Code int                      `json:"code"`
		Data entity.UserCanvasVersion `json:"data"`
	}
	if err := json.Unmarshal(writer.Body.Bytes(), &published); err != nil || published.Code != 0 || published.Data.ID == "" {
		t.Fatalf("publish response: %s %v", writer.Body.String(), err)
	}
	assertAgentAudit(t, *events, "publish_agent", map[string]any{"agent_id": id, "version_id": published.Data.ID, "outcome": "response_success"})
	*events = nil
	writer = serveAgentAuditRequest(t, fixture.Request.Context(), "POST", "/api/v1/agents/:canvas_id/sessions", "/api/v1/agents/"+id+"/sessions", `{}`, h.CreateAgentSession)
	var session struct {
		Code int                     `json:"code"`
		Data entity.API4Conversation `json:"data"`
	}
	if err := json.Unmarshal(writer.Body.Bytes(), &session); err != nil || session.Code != 0 || session.Data.ID == "" {
		t.Fatalf("session response: %s %v", writer.Body.String(), err)
	}
	assertAgentAudit(t, *events, "create_agent_session", map[string]any{"agent_id": id, "session_id": session.Data.ID, "outcome": "response_success"})
}

func TestAgentAuditSessionDeleteSelectors(t *testing.T) {
	for _, test := range []struct {
		name, query, body string
		ids               []string
		all               bool
	}{
		{"query", "?ids=first,%20second&delete_all=true", "", []string{"first", "second"}, true},
		{"body overrides query", "?ids=first&delete_all=true", `{"ids":["second"]}`, []string{"second"}, false},
		{"empty body overrides query", "?ids=first&delete_all=true", `{}`, nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, _, _ := setupGinContextWithUserAndDB(t, "DELETE", "/")
			h := NewAgentHandler(t.Context(), service.NewAgentService(), nil)
			events := captureAgentAudit(t)
			serveAgentAuditRequest(t, fixture.Request.Context(), "DELETE", "/api/v1/agents/:canvas_id/sessions", "/api/v1/agents/missing/sessions"+test.query, test.body, h.DeleteAgentSession)
			assertAgentAudit(t, *events, "delete_agent_sessions", map[string]any{"session_ids": test.ids, "delete_all": test.all, "outcome": "response_error"})
		})
	}
}

type agentAuditStreamWriter struct {
	gin.ResponseWriter
	t      *testing.T
	events *[]*common.OperationLog
}

func (w agentAuditStreamWriter) Write(body []byte) (int, error) {
	if len(*w.events) != 1 || (*w.events)[0].Resources["outcome"] != "unknown" {
		w.t.Fatalf("run request was not audited before first frame: %+v", *w.events)
	}
	return w.ResponseWriter.Write(body)
}

func TestAgentAuditEmitsBeforeStreaming(t *testing.T) {
	for _, mode := range []string{"run", "chat", "openai", "agentbot"} {
		t.Run(mode, func(t *testing.T) {
			fixture, _, _ := setupGinContextWithUserAndDB(t, "POST", "/")
			events := captureAgentAudit(t)
			runner := &stubChatRunner{events: []canvas.RunEvent{{Type: "done"}}}
			h := &AgentHandler{chatRunner: runner}
			method, route, url, body, action := "POST", "/api/v1/agents/:canvas_id/run", "/api/v1/agents/agent/run?version=version", `{"user_input":"secret"}`, "request_agent_run"
			handle := h.RunAgent
			if mode == "chat" || mode == "openai" {
				route, url, action = "/api/v1/agents/chat/completions", "/api/v1/agents/chat/completions", "request_agent_chat_completion"
				body = `{"agent_id":"agent","stream":true,"query":"secret"}`
				if mode == "openai" {
					body = `{"agent_id":"agent","stream":true,"openai-compatible":true,"messages":[{"role":"user","content":"secret"}]}`
				}
				handle = h.AgentChatCompletions
			}
			if mode == "agentbot" {
				route, url, action = "/api/v1/agentbots/:agent_id/completions", "/api/v1/agentbots/agent/completions", "request_agentbot_completion"
				body = `{"session_id":"session","query":"secret"}`
				handle = (&BotHandler{botService: &stubBotService{agentbotCompleteFn: func(context.Context, string, service.AgentbotCompletionRequest) (<-chan canvas.RunEvent, common.ErrorCode, error) {
					ch := make(chan canvas.RunEvent)
					close(ch)
					return ch, common.CodeSuccess, nil
				}}}).AgentbotCompletion
			}
			serveAgentAuditRequest(t, fixture.Request.Context(), method, route, url, body, func(c *gin.Context) {
				c.Writer = agentAuditStreamWriter{ResponseWriter: c.Writer, t: t, events: events}
				handle(c)
			})
			assertAgentAudit(t, *events, action, map[string]any{"agent_id": "agent", "outcome": "unknown"})
			if mode != "agentbot" && ((*events)[0].Resources["session_id"] != runner.sessionID || runner.sessionID == "") {
				t.Fatalf("missing generated session: %+v", (*events)[0].Resources)
			}
		})
	}
}

func TestAgentAuditExcludesReadsAutomationAndAnonymousRequests(t *testing.T) {
	for _, test := range []struct {
		method, path string
		handle       gin.HandlerFunc
	}{
		{"GET", "/api/v1/agents/prompts", (&AgentHandler{}).Prompts},
		{"POST", "/api/v1/agents/agent/webhook", (&AgentHandler{}).Webhook},
	} {
		t.Run(test.path, func(t *testing.T) {
			events := captureAgentAudit(t)
			serveAgentAuditRequest(t, permission.WithIdentity(t.Context(), "user-1", "user-1"), test.method, test.path, test.path, "", test.handle)
			if len(*events) != 0 {
				t.Fatalf("unexpected audit events: %+v", *events)
			}
		})
	}
	events := captureAgentAudit(t)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("PUT", "/api/v1/agents/agent", io.NopCloser(strings.NewReader(`{`)))
	(&AgentHandler{}).UpdateAgent(c)
	if len(*events) != 0 {
		t.Fatalf("anonymous request was audited: %+v", *events)
	}
}
