//go:build ee

package common

import (
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"
)

const operationLogKey = "ragflow.operationLog"

// StartOperationLog starts one action record owned by the current HTTP handler.
func StartOperationLog(c *gin.Context, action string) {
	if _, exists := c.Get(operationLogKey); exists {
		return
	}
	resources := map[string]any{"event_kind": "action_request", "outcome": "unknown"}
	for _, key := range []string{"dataset_id", "document_id", "chunk_id", "search_id", "memory_id", "agent_id", "session_id", "version_id", "component_id", "page_type", "slug", "skill_kwd"} {
		if id := c.Param(key); id != "" {
			resources[key] = id
		}
	}
	if id := c.Param("canvas_id"); id != "" {
		resources["agent_id"] = id
	}
	c.Set(operationLogKey, &OperationLog{
		EventTime: time.Now(), IPAddress: c.ClientIP(), Operation: action,
		APIPath: c.FullPath(), HTTPMethod: c.Request.Method, Resources: resources,
	})
}

// SetOperationResource adds an explicitly selected resource identifier or action detail.
func SetOperationResource(c *gin.Context, key string, value any) {
	if operation := operationLogFromContext(c); operation != nil {
		operation.Resources[key] = value
	}
}

func operationLogFromContext(c *gin.Context) *OperationLog {
	value, _ := c.Get(operationLogKey)
	operation, _ := value.(*OperationLog)
	return operation
}

func auditResponse(c *gin.Context, code ErrorCode) {
	if operation := operationLogFromContext(c); operation != nil {
		operation.Code = int(code)
		operation.Resources["outcome"] = "response_error"
		if code == CodeSuccess {
			operation.Resources["outcome"] = "response_success"
		}
		FinishOperationLog(c)
	}
}

// FinishOperationLog emits the remaining record; raw and streaming responses keep an unknown outcome.
func FinishOperationLog(c *gin.Context) {
	operation := operationLogFromContext(c)
	if operation == nil {
		return
	}
	c.Set(operationLogKey, nil)
	if c.GetString("user_id") == "" {
		return
	}
	operation.UserID = c.GetString("user_id")
	operation.Resources["tenant_id"] = c.GetString("tenant_id")
	if c.Writer.Written() {
		operation.Resources["http_status"] = c.Writer.Status()
	}
	if span := trace.SpanContextFromContext(c.Request.Context()); span.IsValid() {
		operation.Resources["trace_id"] = span.TraceID().String()
	}
	operation.DurationMS = time.Since(operation.EventTime).Milliseconds()
	Audit(operation)
}
