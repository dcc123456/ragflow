//go:build ee

package handler

import (
	"encoding/json/v2"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
	"ragflow/internal/permission/testsupport"
	"ragflow/internal/service"
)

func searchAuditRouter(handler gin.HandlerFunc, method, path string, anonymous bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if !anonymous {
			c.Set("user", &entity.User{ID: "actor"})
			c.Set("user_id", "actor")
			c.Set("tenant_id", "tenant")
		}
		span := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{1}})
		ctx := permission.WithIdentity(c.Request.Context(), "actor", "tenant")
		c.Request = c.Request.WithContext(trace.ContextWithSpanContext(ctx, span))
		c.Next()
	})
	router.Handle(method, path, handler)
	return router
}

func searchAuditRequest(router *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	writer := httptest.NewRecorder()
	router.ServeHTTP(writer, request)
	return writer
}

func TestSearchHandlerAuditValidationAndExclusions(t *testing.T) {
	search := NewSearchHandler(nil, nil)
	bot := NewSearchBotHandler(nil, nil, nil, nil)
	datasets := &DatasetsHandler{}
	chat := &ChatHandler{}
	for _, test := range []struct {
		name, method, path, url, body, action, id string
		handler                                   gin.HandlerFunc
		anonymous                                 bool
	}{
		{name: "create rejected", method: "POST", path: "/api/v1/searches", body: `{"name":" "}`, action: "create_search", handler: search.CreateSearch},
		{name: "update malformed", method: "PUT", path: "/api/v1/searches/:search_id", url: "/api/v1/searches/search-one", body: `{"name":"private-content",`, action: "update_search", id: "search-one", handler: search.UpdateSearch},
		{name: "delete missing identifier", method: "DELETE", path: "/api/v1/searches", action: "delete_search", handler: search.DeleteSearch},
		{name: "completion malformed", method: "POST", path: "/api/v1/searches/:search_id/completions", url: "/api/v1/searches/search-one/completions", body: `{"question":`, action: "request_search_answer", id: "search-one", handler: search.Completion},
		{name: "ask parsed", method: "POST", path: "/api/v1/searchbots/ask", body: `{"search_id":"search-one","question":"private-content","kb_ids":[""]}`, action: "request_search_answer", id: "search-one", handler: bot.Ask},
		{name: "ask malformed", method: "POST", path: "/api/v1/searchbots/ask", body: `{"question":`, action: "request_search_answer", handler: bot.Ask},
		{name: "bot retrieval", method: "POST", path: "/api/v1/searchbots/retrieval", body: `{"search_id":"search-one","question":"private-content","kb_ids":["dataset"],"top_k":0}`, action: "request_search_query", id: "search-one", handler: bot.RetrievalTest},
		{name: "bot mindmap", method: "POST", path: "/api/v1/searchbots/mindmap", body: `{"search_id":"search-one","question":"private-content","kb_ids":[""]}`, action: "request_search_mindmap", id: "search-one", handler: bot.MindMap},
		{name: "retrieval alias", method: "POST", path: "/api/v1/retrieval", body: `{"search_id":"search-one","question":" ","dataset_ids":["dataset"],"top_k":1}`, action: "request_search_query", id: "search-one", handler: datasets.SearchDatasets},
		{name: "frontend retrieval", method: "POST", path: "/api/v1/datasets/search", body: `{"search_id":"search-one","question":" ","dataset_ids":["dataset"],"top_k":1}`, action: "request_search_query", id: "search-one", handler: datasets.SearchDatasets},
		{name: "single dataset retrieval", method: "POST", path: "/api/v1/datasets/:dataset_id/search", url: "/api/v1/datasets/dataset-one/search", body: `{"search_id":"search-one","question":" ","top_k":1}`, action: "request_search_query", id: "search-one", handler: datasets.SearchDataset},
		{name: "chat mindmap", method: "POST", path: "/api/v1/chat/mindmap", body: `{"search_id":"search-one","question":"private-content","kb_ids":["dataset"]}`, action: "request_search_mindmap", id: "search-one", handler: chat.MindMap},
		{name: "generic retrieval excluded", method: "POST", path: "/api/v1/retrieval", body: `{"question":" ","dataset_ids":["dataset"],"top_k":1}`, handler: datasets.SearchDatasets},
		{name: "generic frontend retrieval excluded", method: "POST", path: "/api/v1/datasets/search", body: `{"question":" ","dataset_ids":["dataset"],"top_k":1}`, handler: datasets.SearchDatasets},
		{name: "generic single dataset excluded", method: "POST", path: "/api/v1/datasets/:dataset_id/search", url: "/api/v1/datasets/dataset-one/search", body: `{"question":" ","top_k":1}`, handler: datasets.SearchDataset},
		{name: "generic mindmap excluded", method: "POST", path: "/api/v1/chat/mindmap", body: `{"search_id":"  ","question":" ","kb_ids":["dataset"]}`, handler: chat.MindMap},
		{name: "shared malformed unclassified", method: "POST", path: "/api/v1/retrieval", body: `{"search_id":"search-one",`, handler: datasets.SearchDatasets},
		{name: "automatic related questions excluded", method: "POST", path: "/api/v1/searchbots/related-questions", body: `{}`, handler: bot.Handle},
		{name: "read excluded", method: "GET", path: "/api/v1/searches", handler: search.GetSearch},
		{name: "anonymous excluded", method: "POST", path: "/api/v1/searches", body: `{}`, handler: search.CreateSearch, anonymous: true},
		{name: "large parsed body retains identifier", method: "POST", path: "/api/v1/datasets/search", body: `{"search_id":"search-one","question":"` + strings.Repeat("private-content", 6000) + `","dataset_ids":[],"top_k":1}`, action: "request_search_query", id: "search-one", handler: datasets.SearchDatasets},
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
					t.Fatalf("excluded request emitted %d events", len(events))
				}
				return
			}
			if len(events) != 1 {
				t.Fatalf("got %d events; response %s", len(events), writer.Body.String())
			}
			event := events[0]
			if event.Operation != test.action || event.UserID != "actor" || event.Resources["tenant_id"] != "tenant" || event.Resources["outcome"] != "response_error" || event.Resources["event_kind"] != "action_request" || event.Code == 0 || event.EventTime.IsZero() || event.APIPath != test.path || event.Resources["http_status"] != writer.Code {
				t.Fatalf("incorrect event: %+v", event)
			}
			if test.id != "" && event.Resources["search_id"] != test.id {
				t.Fatalf("search identifier missing: %+v", event.Resources)
			}
			if test.name == "single dataset retrieval" && event.Resources["dataset_id"] != "dataset-one" {
				t.Fatalf("dataset identifier missing: %+v", event.Resources)
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

func TestSearchHandlerAuditCRUD(t *testing.T) {
	db := setupSearchHandlerTestDB(t)
	if err := testsupport.PrepareEnterprisePermissionFixture(db, "tenant", "actor"); err != nil {
		t.Fatal(err)
	}
	handler := NewSearchHandler(service.NewSearchService(), service.NewUserService())
	var events []*common.OperationLog
	common.SetAuditLogSink(func(event *common.OperationLog) error { events = append(events, event); return nil })
	t.Cleanup(func() { common.SetAuditLogSink(nil) })
	writer := searchAuditRequest(searchAuditRouter(handler.CreateSearch, "POST", "/api/v1/searches", false), "POST", "/api/v1/searches", `{"name":"private-content"}`)
	var response struct {
		Code common.ErrorCode             `json:"code"`
		Data service.CreateSearchResponse `json:"data"`
	}
	if err := json.Unmarshal(writer.Body.Bytes(), &response); err != nil || response.Code != common.CodeSuccess || response.Data.SearchID == "" {
		t.Fatalf("create failed: %v %s", err, writer.Body.String())
	}
	id := response.Data.SearchID
	searchAuditRequest(searchAuditRouter(handler.UpdateSearch, "PUT", "/api/v1/searches/:search_id", false), "PUT", "/api/v1/searches/"+id, `{"name":"renamed-private-content","search_config":{}}`)
	searchAuditRequest(searchAuditRouter(handler.DeleteSearch, "DELETE", "/api/v1/searches/:search_id", false), "DELETE", "/api/v1/searches/"+id, "")
	if len(events) != 3 {
		t.Fatalf("expected 3 CRUD events, got %d", len(events))
	}
	for index, action := range []string{"create_search", "update_search", "delete_search"} {
		event := events[index]
		if event.Operation != action || event.Code != 0 || event.Resources["outcome"] != "response_success" || event.Resources["search_id"] != id {
			t.Fatalf("CRUD event incorrect: %+v", event)
		}
	}
}

type searchAuditStreamRecorder struct {
	*httptest.ResponseRecorder
	beforeWrite func()
	closed      chan bool
}

func (w *searchAuditStreamRecorder) CloseNotify() <-chan bool { return w.closed }

func (w *searchAuditStreamRecorder) Write(data []byte) (int, error) {
	w.beforeWrite()
	return w.ResponseRecorder.Write(data)
}

func TestSearchAskAuditEmittedBeforeStream(t *testing.T) {
	var events []*common.OperationLog
	common.SetAuditLogSink(func(event *common.OperationLog) error { events = append(events, event); return nil })
	t.Cleanup(func() { common.SetAuditLogSink(nil) })
	handler := NewSearchBotHandler(nil, nil, nil, nil)
	writer := &searchAuditStreamRecorder{
		ResponseRecorder: httptest.NewRecorder(), closed: make(chan bool),
		beforeWrite: func() {
			if len(events) != 1 || events[0].Resources["outcome"] != "unknown" {
				t.Fatalf("initiation must be emitted before the first stream frame: %+v", events)
			}
		},
	}
	request := httptest.NewRequest("POST", "/api/v1/searchbots/ask", strings.NewReader(`{"question":"private-content","kb_ids":["dataset"]}`))
	request.Header.Set("Content-Type", "application/json")
	searchAuditRouter(handler.Ask, "POST", "/api/v1/searchbots/ask", false).ServeHTTP(writer, request)
	if !strings.Contains(writer.Body.String(), "chat model not configured") || len(events) != 1 {
		t.Fatalf("expected one initiation event and SSE rejection: %d %s", len(events), writer.Body.String())
	}
	if events[0].Resources["outcome"] != "unknown" || events[0].Operation != "request_search_answer" {
		t.Fatalf("stream initiation incorrectly marked completed: %+v", events[0])
	}
}
