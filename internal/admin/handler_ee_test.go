//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

//go:build ee

package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"
	servicepkg "ragflow/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type adminIngestionTaskTestResponse struct {
	Code    common.ErrorCode                `json:"code"`
	Data    []servicepkg.AdminIngestionTask `json:"data"`
	Total   int64                           `json:"total"`
	Message string                          `json:"message"`
}

func setupAdminIngestionTaskDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.SetupTestDB(t,
		&entity.IngestionTask{},
		&entity.User{}, &entity.Document{}, &entity.Knowledgebase{}, &entity.UserCanvas{},
	)
	t.Cleanup(testutil.ReplaceDBForTest(t, db))
	return db
}

func adminIngestionTaskTestRouter(handler *Handler) *gin.Engine {
	engine := gin.New()
	RegisterEERouter(engine.Group("/api/v1/admin"), NewRouter(handler))
	return engine
}

func TestAdminIngestionTasksUsesQueryFiltersAndPaginationWithoutBody(t *testing.T) {
	db := setupAdminIngestionTaskDB(t)
	for _, user := range []entity.User{
		{ID: "user-a", Nickname: "Alice", Email: "alice@example.com"},
		{ID: "user-b", Nickname: "Bob", Email: "bob@example.com"},
	} {
		if err := db.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, dataset := range []entity.Knowledgebase{
		{ID: "dataset-a", Name: "Legal Guides", TenantID: "user-a", EmbdID: "embedding"},
		{ID: "dataset-b", Name: "Other Dataset", TenantID: "user-b", EmbdID: "embedding"},
	} {
		if err := db.Create(&dataset).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i, fixture := range []struct {
		id, user, dataset, status string
	}{
		{"task-a", "user-a", "dataset-a", common.RUNNING},
		{"task-b", "user-a", "dataset-a", common.RUNNING},
		{"task-c", "user-b", "dataset-b", common.RUNNING},
		{"task-d", "user-a", "dataset-a", common.FAILED},
	} {
		name := "Contract " + fixture.id
		doc := entity.Document{
			ID: "doc-" + fixture.id, KbID: fixture.dataset, ParserID: "naive", Type: "txt",
			CreatedBy: fixture.user, SourceType: "local", Suffix: "txt", Name: &name, ParserConfig: entity.JSONMap{},
			PipelineID: testutil.StrPtr("pipeline-" + fixture.id),
		}
		if err := db.Create(&doc).Error; err != nil {
			t.Fatal(err)
		}
		created := int64(i+1) * 1000
		updated := int64(4-i) * 1000
		createdDate := time.Date(2026, time.January, i+1, 8, 0, 0, 0, time.UTC)
		updatedDate := createdDate.Add(time.Hour)
		runID := "run-" + fixture.id
		if err := db.Create(&entity.UserCanvas{ID: "pipeline-" + fixture.id, UserID: fixture.user, Title: testutil.StrPtr("Current pipeline")}).Error; err != nil {
			t.Fatal(err)
		}
		task := entity.IngestionTask{
			ID: fixture.id, UserID: fixture.user, DocumentID: doc.ID, DatasetID: fixture.dataset,
			Status: fixture.status, PipelineLogID: &runID, BaseModel: entity.BaseModel{
				CreateTime: &created, UpdateTime: &updated, CreateDate: &createdDate, UpdateDate: &updatedDate,
			},
		}
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
	}

	engine := adminIngestionTaskTestRouter(&Handler{service: &Service{ingestionTaskSvc: servicepkg.NewIngestionTaskService()}})
	query := url.Values{
		"user_name": {"alice@example.com"}, "dataset_id": {"dataset-a"},
		"dataset_name": {"Guid"}, "document_name": {"Contract"}, "status": {"running"}, "page": {"2"}, "page_size": {"1"},
		"order_by": {"create_time"}, "desc": {"false"},
		"pageSize": {"99"}, "_": {"1", "2"}, "trace_id": {"trace-1"}, "user_email": {"bob@example.com"},
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/ingestion/tasks?"+query.Encode(), nil))
	if response.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, body = %s", response.Code, response.Body.String())
	}
	var result adminIngestionTaskTestResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Code != common.CodeSuccess || result.Total != 2 {
		t.Fatalf("response = %s, want filtered total 2", response.Body.String())
	}
	if len(result.Data) != 1 || result.Data[0].ID != "task-b" {
		t.Fatalf("response = %s, want task-b", response.Body.String())
	}
	var raw struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{
		"id": "task-b", "pipeline_log_id": "run-task-b", "pipeline_id": "pipeline-task-b", "pipeline_name": "Current pipeline",
		"user_id": "user-a", "nickname": "Alice", "email": "alice@example.com",
		"document_id": "doc-task-b", "document_name": "Contract task-b", "dataset_id": "dataset-a", "dataset_name": "Legal Guides",
		"status": common.RUNNING, "create_date": "2026-01-02T08:00:00Z", "update_date": "2026-01-02T09:00:00Z",
	}
	if !reflect.DeepEqual(raw.Data, []map[string]interface{}{want}) {
		t.Fatalf("response data = %+v, want %+v", raw.Data, want)
	}
	if err := db.Model(&entity.IngestionTask{}).Where("id = ?", "task-b").
		UpdateColumns(map[string]interface{}{"create_date": nil, "update_date": nil}).Error; err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/ingestion/tasks?"+query.Encode(), nil))
	if response.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, body = %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	want["create_date"], want["update_date"] = nil, nil
	if !reflect.DeepEqual(raw.Data, []map[string]interface{}{want}) {
		t.Fatalf("nullable dates must remain null despite populated timestamps: data=%+v, want %+v", raw.Data, want)
	}
	for _, query := range []string{"document_name=task-b", "user_name=alice%40example.com&status=RUNNING&document_name=task-b"} {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/ingestion/tasks?"+query, nil))
		var result adminIngestionTaskTestResponse
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusOK || result.Code != common.CodeSuccess || result.Total != 1 ||
			len(result.Data) != 1 || result.Data[0].ID != "task-b" {
			t.Fatalf("query %s: HTTP status = %d, body = %s, want task-b", query, response.Code, response.Body.String())
		}
	}
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/ingestion/tasks?dataset_id=dataset-b&user_name=alice%40example.com", nil))
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || result.Total != 0 || len(result.Data) != 0 {
		t.Fatalf("dataset and user filters must combine with AND: HTTP %d, body = %s", response.Code, response.Body.String())
	}
	for _, test := range []struct {
		query, wantID string
		wantCount     int
	}{
		{"page_size=1", "task-a", 1},
		{"page_size=1&desc=false", "task-d", 1},
		{"page_size=1&order_by=create_time", "task-d", 1},
		{"page_size=1&order_by=create_time&desc=false", "task-a", 1},
		{"page_size=1&order_by=update_time", "task-a", 1},
		{"page_size=1&order_by=update_time&desc=false", "task-d", 1},
		{"page_size=1&order_by=%20UPDATE_TIME%20&desc=%20false%20", "task-d", 1},
		{"pageSize=1&user_email=bob%40example.com&pipeline_name=unknown&_=1&_=2&trace_id=trace-1", "task-a", 4},
	} {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/ingestion/tasks?"+test.query, nil))
		var result adminIngestionTaskTestResponse
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusOK || result.Code != common.CodeSuccess || result.Total != 4 ||
			len(result.Data) != test.wantCount || result.Data[0].ID != test.wantID {
			t.Fatalf("query %s: HTTP %d, body = %s, want %d tasks starting with %s", test.query, response.Code, response.Body.String(), test.wantCount, test.wantID)
		}
	}
}

func TestAdminIngestionTasksReturnsEmptyDisplayFieldsForMissingMetadata(t *testing.T) {
	db := setupAdminIngestionTaskDB(t)
	if err := db.Create(&entity.IngestionTask{
		ID: "task-1", UserID: "missing-user", DocumentID: "missing-document", DatasetID: "missing-dataset", Status: common.CREATED,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.IngestionTask{}).Where("id = ?", "task-1").
		UpdateColumns(map[string]interface{}{"create_date": nil, "update_date": nil}).Error; err != nil {
		t.Fatal(err)
	}
	engine := adminIngestionTaskTestRouter(&Handler{service: &Service{ingestionTaskSvc: servicepkg.NewIngestionTaskService()}})
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/ingestion/tasks", nil))
	var result struct {
		Code  common.ErrorCode         `json:"code"`
		Data  []map[string]interface{} `json:"data"`
		Total int64                    `json:"total"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{
		"id": "task-1", "pipeline_log_id": nil, "pipeline_id": nil, "pipeline_name": "",
		"user_id": "missing-user", "nickname": "", "email": "",
		"document_id": "missing-document", "document_name": "", "dataset_id": "missing-dataset", "dataset_name": "",
		"status": common.CREATED, "create_date": nil, "update_date": nil,
	}
	if response.Code != http.StatusOK || result.Code != common.CodeSuccess || result.Total != 1 ||
		!reflect.DeepEqual(result.Data, []map[string]interface{}{want}) {
		t.Fatalf("HTTP status = %d, body = %s, want the task with empty display fields and nullable IDs and dates", response.Code, response.Body.String())
	}
}

func TestAdminIngestionTasksRejectsInvalidQuery(t *testing.T) {
	engine := adminIngestionTaskTestRouter(&Handler{service: &Service{ingestionTaskSvc: servicepkg.NewIngestionTaskService()}})
	for _, query := range []string{
		"page=0", "page=", "page=abc", "page=1000001", "page=9223372036854775807", "page_size=0", "page_size=101",
		"desc=", "desc=sideways", "status=UNKNOWN", "page=1&page=2", "dataset_id=one&dataset_id=two",
		"desc=true&desc=false", "status=RUNNING&status=FAILED", "page=abc&trace_id=trace-1",
		"order_by=", "order_by=document_name", "order_by=process_begin_at", "order_by=create_time%20DESC", "order_by=create_time&order_by=update_time",
	} {
		t.Run(query, func(t *testing.T) {
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/ingestion/tasks?"+query, nil))
			var result adminIngestionTaskTestResponse
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusOK || result.Code != common.CodeBadRequest || result.Message == "" {
				t.Fatalf("HTTP status = %d, body = %s, want HTTP 200 with bad-request code", response.Code, response.Body.String())
			}
		})
	}
}

func TestAdminIngestionTasksReturnsEmptyList(t *testing.T) {
	setupAdminIngestionTaskDB(t)
	engine := adminIngestionTaskTestRouter(&Handler{service: &Service{ingestionTaskSvc: servicepkg.NewIngestionTaskService()}})
	for _, query := range []string{"", "?page=1000000&page_size=100"} {
		t.Run(query, func(t *testing.T) {
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/ingestion/tasks"+query, nil))
			var result adminIngestionTaskTestResponse
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusOK || result.Code != common.CodeSuccess || result.Total != 0 ||
				result.Data == nil || len(result.Data) != 0 {
				t.Fatalf("HTTP status = %d, body = %s, want an empty array and total 0", response.Code, response.Body.String())
			}
		})
	}
}

func TestAdminIngestionTasksReturnsDatabaseError(t *testing.T) {
	db := setupAdminIngestionTaskDB(t)
	if err := db.Migrator().DropTable(&entity.IngestionTask{}); err != nil {
		t.Fatal(err)
	}
	engine := adminIngestionTaskTestRouter(&Handler{service: &Service{ingestionTaskSvc: servicepkg.NewIngestionTaskService()}})
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/ingestion/tasks", nil))
	var result adminIngestionTaskTestResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || result.Code != common.CodeServerError || result.Message == "" {
		t.Fatalf("HTTP status = %d, body = %s, want HTTP 200 with server-error code", response.Code, response.Body.String())
	}
}

func TestAdminIngestionTasksRouteCanUseAdminAuthentication(t *testing.T) {
	handler := &Handler{}
	engine := gin.New()
	protected := engine.Group("/api/v1/admin", handler.AuthMiddleware())
	RegisterEERouter(protected, NewRouter(handler))
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/ingestion/tasks", nil))
	if !strings.Contains(response.Body.String(), "Missing authorization header") {
		t.Fatalf("response = %s, want administrator authentication before task lookup", response.Body.String())
	}
}
