//go:build ee

package handler

import (
	"context"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
	"ragflow/internal/service/dataset"
	"ragflow/internal/service/document"

	"github.com/gin-gonic/gin"
)

func TestDatasetOperationHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	datasets := &DatasetsHandler{}
	documents := &DocumentHandler{}
	chunks := &ChunkHandler{}
	files := &FileHandler{}
	for _, test := range []struct {
		name    string
		handler gin.HandlerFunc
		action  string
		outcome string
	}{
		{"create dataset", datasets.CreateDataset, "create_dataset", "response_error"},
		{"update dataset", datasets.UpdateDataset, "update_dataset", "response_error"},
		{"delete datasets", datasets.DeleteDatasets, "delete_dataset", "response_error"},
		{"delete documents", documents.DeleteDocuments, "delete_dataset_documents", "response_error"},
		{"start parsing", documents.StartIngestionTask, "start_ingestion_task", "response_error"},
		{"stop parsing", documents.StopParseDocuments, "stop_dataset_parsing", "response_error"},
		{"stop tasks", documents.StopIngestionTasks, "stop_dataset_ingestion_tasks", "response_error"},
		{"remove tasks", documents.RemoveIngestionTasks, "remove_dataset_ingestion_tasks", "response_error"},
		{"ingest", documents.Ingest, "request_dataset_ingestion", "response_error"},
		{"link files", files.LinkToDatasets, "link_files_to_datasets", "response_error"},
		{"parse chunks", chunks.Parse, "start_dataset_chunk_parsing", "response_error"},
		{"delete document raw response", documents.DeleteDocument, "delete_dataset_document", "unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var events []*common.OperationLog
			common.SetAuditLogSink(func(event *common.OperationLog) error {
				events = append(events, event)
				return nil
			})
			t.Cleanup(func() { common.SetAuditLogSink(nil) })
			engine := gin.New()
			engine.POST("/action", func(c *gin.Context) {
				c.Set("user", &entity.User{ID: "actor"})
				c.Set("user_id", "actor")
				c.Set("tenant_id", "tenant")
				c.Request = c.Request.WithContext(permission.WithIdentity(c.Request.Context(), "actor", "tenant"))
				test.handler(c)
			})
			writer := httptest.NewRecorder()
			request := httptest.NewRequest("POST", "/action", strings.NewReader(`{"content":"sensitive",`))
			request.Header.Set("Content-Type", "application/json")
			engine.ServeHTTP(writer, request)
			if len(events) != 1 {
				t.Fatalf("expected one event, got %d", len(events))
			}
			event := events[0]
			if event.Operation != test.action || event.UserID != "actor" || event.Resources["tenant_id"] != "tenant" || event.Resources["outcome"] != test.outcome {
				t.Fatalf("wrong event: %+v", event)
			}
			if event.Error != nil || event.RequestParams != "" || strings.Contains(event.Operation, "sensitive") {
				t.Fatalf("unsafe event: %+v", event)
			}
		})
	}
}

func TestDatasetOperationParserConfig(t *testing.T) {
	for _, code := range []common.ErrorCode{common.CodeSuccess, common.CodeConflict} {
		t.Run(code.Message(), func(t *testing.T) {
			var events []*common.OperationLog
			common.SetAuditLogSink(func(event *common.OperationLog) error { events = append(events, event); return nil })
			t.Cleanup(func() { common.SetAuditLogSink(nil) })
			svc := &applyParserConfigService{code: code}
			if code != common.CodeSuccess {
				svc.err = errors.New("sensitive configuration")
			}
			h := NewDocumentHandler(svc, nil, nil)
			engine := gin.New()
			engine.POST("/datasets/:dataset_id/apply", func(c *gin.Context) {
				c.Set("user", &entity.User{ID: "user-1"})
				c.Set("user_id", "user-1")
				c.Set("tenant_id", "tenant")
				c.Request = c.Request.WithContext(permission.WithIdentity(c.Request.Context(), "user-1", "tenant"))
				h.ApplyDatasetParserConfig(c)
			})
			engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/datasets/kb-1/apply", nil))
			if !svc.called || len(events) != 1 {
				t.Fatalf("called=%v events=%d", svc.called, len(events))
			}
			event := events[0]
			outcome := "response_success"
			if code != common.CodeSuccess {
				outcome = "response_error"
			}
			if event.Resources["dataset_id"] != "kb-1" || event.Resources["outcome"] != outcome || event.Code != int(code) || event.Error != nil {
				t.Fatalf("wrong event: %+v", event)
			}
		})
	}
}

func TestDatasetOperationParsedIdentifiers(t *testing.T) {
	var events []*common.OperationLog
	common.SetAuditLogSink(func(event *common.OperationLog) error { events = append(events, event); return nil })
	t.Cleanup(func() { common.SetAuditLogSink(nil) })
	called := false
	svc := &mockChunkSvc{switchChunksFn: func(_ context.Context, datasetID, documentID string, available int, ids []string) error {
		called = true
		if datasetID != "dataset" || documentID != "document" || available != 0 || !reflect.DeepEqual(ids, []string{"chunk"}) {
			t.Fatalf("wrong service input %s %s %d %v", datasetID, documentID, available, ids)
		}
		return nil
	}}
	h := &ChunkHandler{chunkService: svc}
	engine := gin.New()
	engine.PATCH("/datasets/:dataset_id/documents/:document_id/chunks", func(c *gin.Context) {
		c.Set("user_id", "actor")
		c.Set("tenant_id", "tenant")
		h.SwitchChunks(c)
	})
	request := httptest.NewRequest("PATCH", "/datasets/dataset/documents/document/chunks", strings.NewReader(`{"chunk_ids":["chunk"],"available":false,"content":"sensitive"}`))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(httptest.NewRecorder(), request)
	if !called || len(events) != 1 {
		t.Fatalf("called=%v events=%d", called, len(events))
	}
	want := map[string]any{"dataset_id": "dataset", "document_id": "document", "chunk_ids": []string{"chunk"}, "event_kind": "action_request", "outcome": "response_success", "tenant_id": "tenant", "http_status": 200}
	if !reflect.DeepEqual(events[0].Resources, want) {
		t.Fatalf("wrong identifiers: %v", events[0].Resources)
	}
}

// Every Dataset mutation entry point must emit one authenticated action record,
// including early returns for invalid requests or missing resources.
func TestDatasetOperationRouteInventory(t *testing.T) {
	_, _, db := setupGinContextWithUserAndDB(t, "POST", "/")
	if err := db.AutoMigrate(&entity.Knowledgebase{}, &entity.Document{}); err != nil {
		t.Fatal(err)
	}
	datasetService := dataset.NewDatasetService()
	datasets := &DatasetsHandler{}
	documents := NewDocumentHandler(document.NewDocumentService(), datasetService, nil)
	chunks := &ChunkHandler{}
	artifacts := NewDatasetArtifactHandler(nil, datasetService, nil)
	commits := &FileCommitHandler{}
	files := &FileHandler{}
	for _, test := range []struct {
		route   string
		handler gin.HandlerFunc
		action  string
	}{
		{"POST /api/v1/datasets", datasets.CreateDataset, "create_dataset"},
		{"PUT /api/v1/datasets/:dataset_id", datasets.UpdateDataset, "update_dataset"},
		{"DELETE /api/v1/datasets", datasets.DeleteDatasets, "delete_dataset"},
		{"PUT /api/v1/datasets/:dataset_id/metadata/config", datasets.UpdateMetadataConfig, "update_dataset_metadata_config"},
		{"POST /api/v1/datasets/:dataset_id/documents", documents.UploadDocuments, "upload_dataset_documents"},
		{"PATCH /api/v1/datasets/:dataset_id/documents/:document_id", documents.UpdateDatasetDocument, "update_dataset_document"},
		{"DELETE /api/v1/datasets/:dataset_id/documents", documents.DeleteDocuments, "delete_dataset_documents"},
		{"POST /api/v1/datasets/:dataset_id/documents/batch-update-status", documents.BatchUpdateDocumentStatus, "update_dataset_document_status"},
		{"POST /api/v1/datasets/:dataset_id/documents/apply-parser-config", documents.ApplyDatasetParserConfig, "apply_dataset_parser_config"},
		{"POST /api/v1/datasets/:dataset_id/documents/parse", documents.StartIngestionTask, "start_ingestion_task"},
		{"POST /api/v1/datasets/:dataset_id/documents/stop", documents.StopParseDocuments, "stop_dataset_parsing"},
		{"PUT /api/v1/datasets/ingestion/tasks", documents.StopIngestionTasks, "stop_dataset_ingestion_tasks"},
		{"DELETE /api/v1/datasets/ingestion/tasks", documents.RemoveIngestionTasks, "remove_dataset_ingestion_tasks"},
		{"POST /api/v1/datasets/:dataset_id/chunks", chunks.Parse, "start_dataset_chunk_parsing"},
		{"DELETE /api/v1/datasets/:dataset_id/chunks", chunks.StopParsing, "stop_dataset_chunk_parsing"},
		{"POST /api/v1/datasets/:dataset_id/documents/:document_id/chunks", chunks.AddChunk, "add_dataset_chunk"},
		{"PATCH /api/v1/datasets/:dataset_id/documents/:document_id/chunks", chunks.SwitchChunks, "switch_dataset_chunks"},
		{"PATCH /api/v1/datasets/:dataset_id/documents/:document_id/chunks/:chunk_id", chunks.UpdateChunk, "update_dataset_chunk"},
		{"DELETE /api/v1/datasets/:dataset_id/documents/:document_id/chunks", chunks.RemoveChunks, "delete_dataset_chunks"},
		{"PUT /api/v1/datasets/:dataset_id/documents/:document_id/metadata/config", datasets.UpdateDocumentMetadataConfig, "update_dataset_document_metadata_config"},
		{"POST /api/v1/datasets/:dataset_id/metadata/update", documents.MetadataBatchUpdate, "update_dataset_document_metadata"},
		{"PATCH /api/v1/datasets/:dataset_id/documents/metadatas", documents.UpdateDocumentMetadatas, "update_dataset_document_metadata"},
		{"PUT /api/v1/datasets/:dataset_id/artifacts/:page_type/*slug", artifacts.UpdateArtifact, "update_dataset_artifact"},
		{"DELETE /api/v1/datasets/:dataset_id/skills", artifacts.DeleteSkills, "delete_dataset_skills"},
		{"DELETE /api/v1/datasets/:dataset_id/skills/:skill_kwd", artifacts.DeleteSkill, "delete_dataset_skill"},
		{"DELETE /api/v1/datasets/:dataset_id/documents/:document_id/structure/graph", artifacts.DeleteDocumentGraph, "delete_dataset_document_graph"},
		{"POST /api/v1/datasets/:dataset_id/commits", commits.CreateCommit, "create_dataset_commit"},
		{"POST /api/v1/documents/upload", documents.UploadInfo, "upload_dataset_document_files"},
		{"POST /api/v1/documents/ingest", documents.Ingest, "request_dataset_ingestion"},
		{"PUT /api/v1/documents/:id", documents.UpdateDocument, "update_dataset_document"},
		{"DELETE /api/v1/documents/:id", documents.DeleteDocument, "delete_dataset_document"},
		{"POST /api/v1/files/link-to-datasets", files.LinkToDatasets, "link_files_to_datasets"},
	} {
		t.Run(test.route, func(t *testing.T) {
			var events []*common.OperationLog
			common.SetAuditLogSink(func(event *common.OperationLog) error {
				events = append(events, event)
				return nil
			})
			t.Cleanup(func() { common.SetAuditLogSink(nil) })

			method, route, _ := strings.Cut(test.route, " ")
			path := route
			wantResources := map[string]any{}
			for _, segment := range strings.Split(route, "/") {
				if !strings.HasPrefix(segment, ":") && !strings.HasPrefix(segment, "*") {
					continue
				}
				key := segment[1:]
				value := "missing-" + key
				path = strings.Replace(path, segment, value, 1)
				if key == "id" {
					key = "document_id"
				}
				if segment[0] == '*' {
					value = "/" + value
				}
				wantResources[key] = value
			}
			engine := gin.New()
			engine.Handle(method, route, func(c *gin.Context) {
				c.Set("user", &entity.User{ID: "actor"})
				c.Set("user_id", "actor")
				c.Set("tenant_id", "tenant")
				c.Request = c.Request.WithContext(permission.WithIdentity(c.Request.Context(), "actor", "tenant"))
				test.handler(c)
			})
			request := httptest.NewRequest(method, path, strings.NewReader(`{"content":"sensitive",`))
			request.Header.Set("Content-Type", "application/json")
			engine.ServeHTTP(httptest.NewRecorder(), request)

			if len(events) != 1 {
				t.Fatalf("expected one event, got %d", len(events))
			}
			event := events[0]
			if event.Operation != test.action || event.APIPath != route || event.HTTPMethod != method {
				t.Fatalf("wrong action or route: %+v", event)
			}
			if event.UserID != "actor" || event.Resources["tenant_id"] != "tenant" || event.Resources["event_kind"] != "action_request" {
				t.Fatalf("wrong actor or tenant: %+v", event)
			}
			for key, want := range wantResources {
				if event.Resources[key] != want {
					t.Errorf("resource %s = %v, want %v", key, event.Resources[key], want)
				}
			}
			if event.Error != nil || event.RequestParams != "" {
				t.Fatalf("unsafe event: %+v", event)
			}
		})
	}
}
