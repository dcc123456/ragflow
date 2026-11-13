//go:build ee

package handler

import (
	"encoding/json/v2"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
	"ragflow/internal/permission/testsupport"
	"ragflow/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMemoryCreationAudit(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&entity.Memory{}); err != nil {
		t.Fatal(err)
	}
	if err := testsupport.PrepareEnterprisePermissionFixture(db, "tenant", "actor"); err != nil {
		t.Fatal(err)
	}
	previousDB := dao.DB
	dao.DB = db
	t.Cleanup(func() { dao.DB = previousDB })

	var events []*common.OperationLog
	common.SetAuditLogSink(func(event *common.OperationLog) error {
		events = append(events, event)
		return nil
	})
	t.Cleanup(func() { common.SetAuditLogSink(nil) })
	handler := NewMemoryHandler(service.NewMemoryService(db, nil))
	router := gin.New()
	router.POST("/api/v1/memories", func(c *gin.Context) {
		c.Set("user", &entity.User{ID: "actor"})
		c.Set("user_id", "actor")
		c.Set("tenant_id", "tenant")
		c.Request = c.Request.WithContext(permission.WithIdentity(c.Request.Context(), "actor", "tenant"))
		handler.CreateMemory(c)
	})
	request := httptest.NewRequest("POST", "/api/v1/memories", strings.NewReader(`{"name":"private-memory-name","memory_type":["raw"],"embd_id":"private-embedding","llm_id":"private-chat","tenant_embd_id":"private-embedding-id","tenant_llm_id":"private-chat-id"}`))
	request.Header.Set("Content-Type", "application/json")
	writer := httptest.NewRecorder()
	router.ServeHTTP(writer, request)
	var response struct {
		Code common.ErrorCode             `json:"code"`
		Data service.CreateMemoryResponse `json:"data"`
	}
	if err := json.Unmarshal(writer.Body.Bytes(), &response); err != nil || response.Code != common.CodeSuccess || response.Data.ID == "" {
		t.Fatalf("creation failed: %v %s", err, writer.Body.String())
	}
	if len(events) != 1 {
		t.Fatalf("expected one creation event, got %d", len(events))
	}
	event := events[0]
	if event.Operation != "create_memory" || event.Code != int(common.CodeSuccess) || event.UserID != "actor" || event.Resources["tenant_id"] != "tenant" || event.Resources["memory_id"] != response.Data.ID || event.Resources["outcome"] != "response_success" {
		t.Fatalf("incorrect creation event: %+v", event)
	}
	encoded, err := json.Marshal(event)
	if err != nil || strings.Contains(string(encoded), "private-") || event.RequestParams != "" || event.Error != nil {
		t.Fatalf("unsafe creation event: %s err=%v", encoded, err)
	}
}

func TestMemoryHandlersAuditValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &MemoryHandler{}
	for _, test := range []struct {
		name, method, route, url, body, action string
		handle                                 gin.HandlerFunc
		memoryID                               string
		messageID                              any
		memoryIDs                              []string
	}{
		{name: "create", method: "POST", route: "/memories", url: "/memories", body: `{"name":"secret"}`, action: "create_memory", handle: handler.CreateMemory},
		{name: "update", method: "PUT", route: "/memories/:memory_id", url: "/memories/memory-one", body: `{`, action: "update_memory", handle: handler.UpdateMemory, memoryID: "memory-one"},
		{name: "delete missing id", method: "DELETE", route: "/memories", url: "/memories", action: "delete_memory", handle: handler.DeleteMemory},
		{name: "add normalized ids", method: "POST", route: "/messages", url: "/messages", body: `{"memory_id":[" memory-one,memory-two "," memory-one "],"agent_id":"secret","session_id":"secret","user_input":"secret","agent_response":"secret"}`, action: "request_memory_message_add", handle: handler.AddMessage, memoryIDs: []string{"memory-one", "memory-two", "memory-one"}},
		{name: "add single id", method: "POST", route: "/messages", url: "/messages", body: `{"memory_id":" memory-one, ","agent_id":"secret","session_id":"secret","user_input":"secret","agent_response":"secret"}`, action: "request_memory_message_add", handle: handler.AddMessage, memoryID: "memory-one", memoryIDs: []string{"memory-one"}},
		{name: "add malformed", method: "POST", route: "/messages", url: "/messages", body: `{"memory_id":[3]}`, action: "request_memory_message_add", handle: handler.AddMessage},
		{name: "status canonical id", method: "PUT", route: "/messages/:memory_message", url: "/messages/memory-one:+0007", body: `{"status":"secret"}`, action: "update_memory_message_status", handle: handler.UpdateMessage, memoryID: "memory-one", messageID: int64(7)},
		{name: "forget malformed id", method: "DELETE", route: "/messages/:memory_message", url: "/messages/memory-one:-1", action: "forget_memory_message", handle: handler.ForgetMessage},
	} {
		t.Run(test.name, func(t *testing.T) {
			var events []*common.OperationLog
			common.SetAuditLogSink(func(event *common.OperationLog) error { events = append(events, event); return nil })
			t.Cleanup(func() { common.SetAuditLogSink(nil) })
			router := gin.New()
			router.Handle(test.method, test.route, func(c *gin.Context) {
				c.Set("user", &entity.User{ID: "actor"})
				c.Set("user_id", "actor")
				c.Set("tenant_id", "tenant")
				c.Set("auth_via_api_token", true)
				c.Request = c.Request.WithContext(permission.WithIdentity(c.Request.Context(), "actor", "tenant"))
				test.handle(c)
			})
			request := httptest.NewRequest(test.method, test.url, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			writer := httptest.NewRecorder()
			router.ServeHTTP(writer, request)
			if len(events) != 1 {
				t.Fatalf("events=%d response=%s", len(events), writer.Body.String())
			}
			event := events[0]
			if event.Operation != test.action || event.UserID != "actor" || event.Resources["tenant_id"] != "tenant" || event.Resources["outcome"] != "response_error" || event.Code == 0 || event.EventTime.IsZero() {
				t.Fatalf("event=%+v", event)
			}
			if event.Resources["memory_id"] != nil && event.Resources["memory_id"] != test.memoryID {
				t.Fatalf("memory ID=%v", event.Resources)
			}
			if test.memoryID != "" && event.Resources["memory_id"] != test.memoryID {
				t.Fatalf("missing memory ID=%v", event.Resources)
			}
			if test.messageID != nil && event.Resources["message_id"] != test.messageID {
				t.Fatalf("message ID=%v", event.Resources)
			}
			if test.memoryIDs != nil && !reflect.DeepEqual(event.Resources["memory_ids"], test.memoryIDs) {
				t.Fatalf("memory IDs=%v", event.Resources)
			}
			encoded, err := json.Marshal(event)
			if err != nil || strings.Contains(string(encoded), "secret") || event.RequestParams != "" || event.Error != nil {
				t.Fatalf("unsafe event=%s err=%v", encoded, err)
			}
		})
	}
}

func TestMemoryReadsDoNotAudit(t *testing.T) {
	handler := &MemoryHandler{}
	var events int
	common.SetAuditLogSink(func(*common.OperationLog) error { events++; return nil })
	t.Cleanup(func() { common.SetAuditLogSink(nil) })
	for _, test := range []struct {
		path   string
		handle gin.HandlerFunc
	}{
		{"/messages?limit=invalid", handler.GetMessages},
		{"/messages/search?similarity_threshold=invalid", handler.SearchMessage},
	} {
		writer := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(writer)
		c.Set("user_id", "actor")
		c.Request = httptest.NewRequest("GET", test.path, nil)
		test.handle(c)
	}
	if events != 0 {
		t.Fatalf("read actions emitted %d events", events)
	}
}
