//go:build ee

package common

import (
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"
)

func TestOperationLogResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name    string
		code    ErrorCode
		outcome string
	}{
		{name: "success", code: CodeSuccess, outcome: "response_success"},
		{name: "failure", code: CodeDataError, outcome: "response_error"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var events []*OperationLog
			previousLogger, previousSink := AuditLogger, auditLogSink
			SetAuditLogSink(func(event *OperationLog) error {
				// Emission consumes the context entry before calling the destination.
				events = append(events, event)
				return nil
			})
			t.Cleanup(func() { AuditLogger, auditLogSink = previousLogger, previousSink })
			engine := gin.New()
			span := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{1}})
			engine.POST("/datasets/:dataset_id/documents/:document_id", func(c *gin.Context) {
				c.Set("user_id", "actor")
				c.Set("tenant_id", "tenant")
				c.Request = c.Request.WithContext(trace.ContextWithSpanContext(c.Request.Context(), span))
				StartOperationLog(c, "update_document")
				StartOperationLog(c, "nested_operation")
				defer FinishOperationLog(c)
				SetOperationResource(c, "chunk_id", "chunk-one")
				Respond(c, 200, test.code, map[string]any{"content": "secret"}, "secret")
				value, exists := c.Get(operationLogKey)
				if !exists || value != nil {
					t.Fatalf("operation not consumed: %v %v", value, exists)
				}
				SetOperationResource(c, "content", "late-secret")
				StartOperationLog(c, "late_operation")
				FinishOperationLog(c)
			})
			writer := httptest.NewRecorder()
			engine.ServeHTTP(writer, httptest.NewRequest("POST", "/datasets/dataset-one/documents/document-one", strings.NewReader(`{"api_key":"secret"}`)))
			if len(events) != 1 {
				t.Fatalf("expected one event, got %d", len(events))
			}
			event := events[0]
			if event.UserID != "actor" || event.Operation != "update_document" || event.Code != int(test.code) || event.APIPath != "/datasets/:dataset_id/documents/:document_id" || event.HTTPMethod != "POST" || event.EventTime.IsZero() || event.IPAddress == "" || event.DurationMS < 0 {
				t.Fatalf("wrong event: %+v", event)
			}
			want := map[string]any{"dataset_id": "dataset-one", "document_id": "document-one", "chunk_id": "chunk-one", "event_kind": "action_request", "outcome": test.outcome, "tenant_id": "tenant", "http_status": 200, "trace_id": span.TraceID().String()}
			if !reflect.DeepEqual(event.Resources, want) {
				t.Fatalf("wrong resources: %v", event.Resources)
			}
			if event.RequestParams != "" || event.Error != nil {
				t.Fatalf("payload leaked: %+v", event)
			}
			if !strings.Contains(writer.Body.String(), `"content":"secret"`) {
				t.Fatalf("response changed: %s", writer.Body.String())
			}
		})
	}
}

func TestOperationLogRawFallback(t *testing.T) {
	previousLogger, previousSink := AuditLogger, auditLogSink
	var events []*OperationLog
	SetAuditLogSink(func(event *OperationLog) error { events = append(events, event); return nil })
	t.Cleanup(func() { AuditLogger, auditLogSink = previousLogger, previousSink })
	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest("POST", "/stream", nil)
	c.Set("user_id", "actor")
	StartOperationLog(c, "request_run")
	c.Data(200, "text/event-stream", []byte("data: secret\n\n"))
	FinishOperationLog(c)
	FinishOperationLog(c)
	if len(events) != 1 || events[0].Resources["outcome"] != "unknown" || events[0].Operation != "request_run" {
		t.Fatalf("unexpected events: %+v", events)
	}
}

func TestOperationLogMissingInvalidAndAnonymous(t *testing.T) {
	previousLogger, previousSink := AuditLogger, auditLogSink
	var events []*OperationLog
	SetAuditLogSink(func(event *OperationLog) error { events = append(events, event); return nil })
	t.Cleanup(func() { AuditLogger, auditLogSink = previousLogger, previousSink })
	for _, test := range []struct {
		name                      string
		value                     any
		set, start, authenticated bool
	}{
		{name: "absent", authenticated: true},
		{name: "wrong type", value: "secret", set: true, authenticated: true},
		{name: "typed nil", value: (*OperationLog)(nil), set: true, authenticated: true},
		{name: "anonymous", start: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/", nil)
			if test.authenticated {
				c.Set("user_id", "actor")
			}
			if test.set {
				c.Set(operationLogKey, test.value)
			}
			if test.start {
				StartOperationLog(c, "create_resource")
			}
			SetOperationResource(c, "resource_id", "one")
			RespondError(c, CodeDataError, "secret")
			FinishOperationLog(c)
		})
	}
	if len(events) != 0 {
		t.Fatalf("unexpected events: %+v", events)
	}
}

func TestOperationLogBeforeResponseDoesNotGuessHTTPStatus(t *testing.T) {
	previousLogger, previousSink := AuditLogger, auditLogSink
	var captured *OperationLog
	SetAuditLogSink(func(event *OperationLog) error { captured = event; return nil })
	t.Cleanup(func() { AuditLogger, auditLogSink = previousLogger, previousSink })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/webhook", nil)
	c.Set("user_id", "actor")
	StartOperationLog(c, "request_webhook")
	FinishOperationLog(c)
	c.JSON(202, gin.H{"accepted": true})
	FinishOperationLog(c)
	if captured == nil || captured.Resources["outcome"] != "unknown" {
		t.Fatalf("unexpected event: %+v", captured)
	}
	if _, exists := captured.Resources["http_status"]; exists {
		t.Fatalf("guessed response status before writing: %v", captured.Resources)
	}
}
