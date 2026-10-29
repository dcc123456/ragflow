// Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build ee

package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"
	"ragflow/internal/service"
	"ragflow/internal/service/dataset"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type adminIngestionLogTestResponse struct {
	Code    common.ErrorCode `json:"code"`
	Data    map[string]any   `json:"data"`
	Message string           `json:"message"`
}

func setupAdminIngestionLogTest(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	db := testutil.SetupTestDB(t, &entity.PipelineOperationLog{}, &entity.IngestionTaskLog{},
		&entity.User{}, &entity.Document{}, &entity.IngestionTask{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterEERouter(router.Group("/api/v1/admin"), NewRouter(NewHandler(NewService())))
	return router, db
}

func requestAdminIngestionLog(t *testing.T, router http.Handler, path string) adminIngestionLogTestResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s; want HTTP 200", recorder.Code, recorder.Body.String())
	}
	var response adminIngestionLogTestResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, recorder.Body.String())
	}
	return response
}

func TestGetAdminIngestionMessagesReturnsOnlyRunScopedMessages(t *testing.T) {
	router, db := setupAdminIngestionLogTest(t)
	runCount := 3
	run := entity.PipelineOperationLog{
		ID: "historical-run", DocumentID: "deleted-document", RunCount: &runCount,
		TenantID: "deleted-tenant", KbID: "deleted-dataset", ParserID: "naive",
		DocumentName: "stored.pdf", DocumentSuffix: ".pdf", DocumentType: "pdf",
		SourceFrom: "local", TaskType: string(entity.PipelineTaskTypeParse), OperationStatus: "DONE",
		DSL: entity.JSONMap{"components": "stored"},
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2026, time.October, 20, 10, 0, 0, 0, time.UTC)
	events := []entity.IngestionTaskLog{
		{ID: 1, PipelineLogID: &run.ID},
		{ID: 2, PipelineLogID: testutil.StrPtr("other-run")},
		{ID: 3, PipelineLogID: &run.ID},
		{ID: 4},
		{ID: 5, PipelineLogID: &run.ID},
	}
	for i := range events {
		events[i].TaskID = "reused-task"
		events[i].Checkpoint = entity.JSONMap{}
		events[i].EventType = dao.EventTypeMessage
		events[i].Component = "Parser"
		events[i].Phase = 2
		events[i].Message = "detail\nsecond line"
		events[i].CreateDate = &createdAt
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatal(err)
	}

	response := requestAdminIngestionLog(t, router, "/api/v1/admin/ingestion/logs/historical-run/messages?limit=2")
	if response.Code != common.CodeSuccess || response.Message != "success" {
		t.Fatalf("response = %+v, want success", response)
	}
	want := dataset.IngestionMessagesResponse{
		RunCount: 3, OldestID: 3, NewestID: 5, HasMoreBefore: true, Terminal: true,
		Items: []service.IngestionEventItem{
			{ID: 3, TS: &createdAt, EventType: dao.EventTypeMessage, Component: "Parser", Phase: 2, Message: "detail\nsecond line"},
			{ID: 5, TS: &createdAt, EventType: dao.EventTypeMessage, Component: "Parser", Phase: 2, Message: "detail\nsecond line"},
		},
	}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var expected map[string]any
	if err := json.Unmarshal(raw, &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(response.Data, expected) {
		t.Fatalf("data = %v, want exactly the Dataset message response %s", response.Data, raw)
	}
}

func TestGetAdminIngestionLogReturnsFileDetails(t *testing.T) {
	router, db := setupAdminIngestionLogTest(t)
	uploadedAt := time.Date(2026, time.October, 16, 16, 16, 44, 123456789, time.FixedZone("UTC+8", 8*60*60))
	startedAt := uploadedAt.Add(10 * time.Second)
	otherStartedAt := startedAt.Add(time.Hour)
	runCount := 3
	run := entity.PipelineOperationLog{
		ID: "completed-run", DocumentID: "document", RunCount: &runCount,
		TenantID: "tenant", KbID: "dataset", ParserID: "naive",
		DocumentName: "stored.pdf", DocumentSuffix: "pdf", DocumentType: "pdf", SourceFrom: "local",
		OperationStatus: "DONE", ProcessBeginAt: &startedAt, ProcessDuration: 1.196,
	}
	doc := entity.Document{
		ID: run.DocumentID, KbID: run.KbID, ParserID: "naive", ParserConfig: entity.JSONMap{},
		CreatedBy: "creator", Name: testutil.StrPtr("current.txt"), Suffix: "txt", Type: "doc",
		Size: 16384, ChunkNum: 13, ProcessBeginAt: &otherStartedAt, ProcessDuration: 999,
		BaseModel: entity.BaseModel{CreateDate: &uploadedAt},
	}
	for _, record := range []any{
		&entity.User{ID: "creator", Nickname: "uploader", Email: "uploader@example.com"},
		&entity.User{ID: "owner", Nickname: "task-owner", Email: "owner@example.com"},
		&doc, &run,
		&entity.IngestionTask{ID: "task", UserID: "owner", DocumentID: doc.ID, DatasetID: doc.KbID,
			Status: common.COMPLETED, PipelineLogID: &run.ID},
	} {
		if err := db.Create(record).Error; err != nil {
			t.Fatal(err)
		}
	}
	want := map[string]any{
		"id": run.ID, "document_id": doc.ID, "run_count": float64(runCount),
		"name": "current.txt", "suffix": "txt", "nickname": "uploader",
		"create_date": uploadedAt.Format(time.RFC3339Nano), "size": float64(16384), "chunk_count": float64(13),
		"process_begin_at": startedAt.Format(time.RFC3339Nano), "process_duration": 1.196, "operation_status": "3",
	}
	response := requestAdminIngestionLog(t, router, "/api/v1/admin/ingestion/logs/"+run.ID)
	if response.Code != common.CodeSuccess || !reflect.DeepEqual(response.Data, want) {
		t.Fatalf("response = %+v, want exactly %v", response, want)
	}

	// Deleted supplemental records must not make the run itself unreadable.
	if err := db.Delete(&entity.User{ID: "creator"}).Error; err != nil {
		t.Fatal(err)
	}
	want["nickname"] = ""
	response = requestAdminIngestionLog(t, router, "/api/v1/admin/ingestion/logs/"+run.ID)
	if response.Code != common.CodeSuccess || !reflect.DeepEqual(response.Data, want) {
		t.Fatalf("without uploader: response = %+v, want exactly %v", response, want)
	}
	if err := db.Delete(&doc).Error; err != nil {
		t.Fatal(err)
	}
	want["name"], want["suffix"] = run.DocumentName, run.DocumentSuffix
	for _, key := range []string{"create_date", "size", "chunk_count"} {
		want[key] = nil
	}
	response = requestAdminIngestionLog(t, router, "/api/v1/admin/ingestion/logs/"+run.ID)
	if response.Code != common.CodeSuccess || !reflect.DeepEqual(response.Data, want) {
		t.Fatalf("without document: response = %+v, want exactly %v", response, want)
	}
}

func TestGetAdminIngestionLogUsesOnlyCurrentRunningDocumentTiming(t *testing.T) {
	router, db := setupAdminIngestionLogTest(t)
	docStartedAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	runStartedAt := docStartedAt.Add(-time.Minute)
	for _, tt := range []struct {
		id, status, boundRun, taskStatus string
		live                             bool
	}{
		{"running", "1", "running", common.RUNNING, true},
		{"superseded", "1", "replacement", common.RUNNING, false},
		{"queued", "5", "queued", common.SCHEDULED, false},
	} {
		t.Run(tt.id, func(t *testing.T) {
			run := entity.PipelineOperationLog{
				ID: tt.id, DocumentID: tt.id, TenantID: "tenant", KbID: "dataset", ParserID: "naive",
				DocumentName: "file.txt", DocumentSuffix: "txt", DocumentType: "doc", SourceFrom: "local",
				OperationStatus: tt.status, ProcessBeginAt: &runStartedAt, ProcessDuration: 1.25,
			}
			doc := entity.Document{ID: tt.id, KbID: run.KbID, ParserID: "naive", ParserConfig: entity.JSONMap{},
				CreatedBy: "creator", Type: "doc", Suffix: "txt", ProcessBeginAt: &docStartedAt, ProcessDuration: 999}
			task := entity.IngestionTask{ID: tt.id, UserID: "owner", DocumentID: tt.id, DatasetID: run.KbID,
				Status: tt.taskStatus, PipelineLogID: &tt.boundRun}
			for _, record := range []any{&run, &doc, &task} {
				if err := db.Create(record).Error; err != nil {
					t.Fatal(err)
				}
			}
			response := requestAdminIngestionLog(t, router, "/api/v1/admin/ingestion/logs/"+run.ID)
			if response.Code != common.CodeSuccess {
				t.Fatalf("response = %+v, want success", response)
			}
			wantStartedAt, wantDuration := runStartedAt, 1.25
			if tt.live {
				wantStartedAt = docStartedAt
				duration, ok := response.Data["process_duration"].(float64)
				if !ok || duration < 60 || duration > time.Since(docStartedAt).Seconds()+1 {
					t.Fatalf("duration = %v, want live elapsed time", response.Data["process_duration"])
				}
				wantDuration = duration
			}
			if response.Data["process_begin_at"] != wantStartedAt.Format(time.RFC3339Nano) ||
				response.Data["process_duration"] != wantDuration {
				t.Fatalf("timing = %v, %v; want %v, %v", response.Data["process_begin_at"],
					response.Data["process_duration"], wantStartedAt, wantDuration)
			}
		})
	}
}

func TestAdminIngestionLogEndpointsReturnBusinessErrors(t *testing.T) {
	router, _ := setupAdminIngestionLogTest(t)
	for _, tt := range []struct {
		path string
		code common.ErrorCode
	}{
		{"/messages?limit=0", common.CodeArgumentError},
		{"/messages?limit=501", common.CodeArgumentError},
		{"/messages?after_id=bad", common.CodeArgumentError},
		{"/messages?before_id=-1", common.CodeArgumentError},
		{"/messages?after_id=1&before_id=2", common.CodeArgumentError},
		{"/messages", common.CodeDataError},
		{"", common.CodeDataError},
	} {
		t.Run(tt.path, func(t *testing.T) {
			response := requestAdminIngestionLog(t, router, "/api/v1/admin/ingestion/logs/missing-run"+tt.path)
			if response.Code != tt.code || response.Message == "" || response.Data != nil {
				t.Fatalf("response = %+v, want business code %d with no data", response, tt.code)
			}
		})
	}
}
