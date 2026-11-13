//
//  Copyright 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//

//go:build ee

package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	servicepkg "ragflow/internal/service"
)

// promoteTaskFakePublisher records publishes so the test can assert the lane
// without a live NATS server.
type promoteTaskFakePublisher struct {
	calls []string
	msgs  []common.TaskMessage
}

func (f *promoteTaskFakePublisher) PublishTaskMessage(subject string, msg common.TaskMessage) error {
	f.calls = append(f.calls, subject)
	f.msgs = append(f.msgs, msg)
	return nil
}

func TestAdminPromoteIngestionTask_PublishesToHigh(t *testing.T) {
	db := setupAdminIngestionTaskDB(t)
	if err := db.Create(&entity.IngestionTask{
		ID: "t1", UserID: "u1", DocumentID: "d1", DatasetID: "ds1", Status: common.CREATED,
	}).Error; err != nil {
		t.Fatal(err)
	}
	pub := &promoteTaskFakePublisher{}
	svc := servicepkg.NewIngestionTaskService()
	svc.SetTaskPublisher(pub)
	engine := adminIngestionTaskTestRouter(&Handler{service: &Service{ingestionTaskSvc: svc}})

	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/api/v1/admin/ingestion/tasks/t1/promote", nil))
	if resp.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var body struct {
		Code common.ErrorCode `json:"code"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != common.CodeSuccess {
		t.Fatalf("code = %v, want success, body = %s", body.Code, resp.Body.String())
	}
	if len(pub.calls) != 1 || pub.calls[0] != common.TaskSubjectHigh {
		t.Fatalf("want exactly one high-lane publish, got %v", pub.calls)
	}
}

// R1 (adversarial): the system-admin route is not scoped to a tenant, so a task
// that would land on the free/low lane through resolveTier is still promoted to
// the high lane. The happy path above already exercises "any tenant's task";
// this asserts a distinct task id also goes high.
func TestAdminPromoteIngestionTask_PromotesAnyTenantTask(t *testing.T) {
	db := setupAdminIngestionTaskDB(t)
	if err := db.Create(&entity.IngestionTask{
		ID: "other-tenant-task", UserID: "u2", DocumentID: "d2", DatasetID: "ds2", Status: common.CREATED,
	}).Error; err != nil {
		t.Fatal(err)
	}
	pub := &promoteTaskFakePublisher{}
	svc := servicepkg.NewIngestionTaskService()
	svc.SetTaskPublisher(pub)
	engine := adminIngestionTaskTestRouter(&Handler{service: &Service{ingestionTaskSvc: svc}})

	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/api/v1/admin/ingestion/tasks/other-tenant-task/promote", nil))
	if resp.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var body struct {
		Code common.ErrorCode `json:"code"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != common.CodeSuccess {
		t.Fatalf("code = %v, want success", body.Code)
	}
	if len(pub.calls) != 1 || pub.calls[0] != common.TaskSubjectHigh {
		t.Fatalf("want one high-lane publish, got %v", pub.calls)
	}
}

// R3: a RUNNING task is rejected; the publisher is not called.
func TestAdminPromoteIngestionTask_RunningRejected(t *testing.T) {
	db := setupAdminIngestionTaskDB(t)
	if err := db.Create(&entity.IngestionTask{
		ID: "running-task", UserID: "u3", DocumentID: "d3", DatasetID: "ds3", Status: common.RUNNING,
	}).Error; err != nil {
		t.Fatal(err)
	}
	pub := &promoteTaskFakePublisher{}
	svc := servicepkg.NewIngestionTaskService()
	svc.SetTaskPublisher(pub)
	engine := adminIngestionTaskTestRouter(&Handler{service: &Service{ingestionTaskSvc: svc}})

	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/api/v1/admin/ingestion/tasks/running-task/promote", nil))
	if resp.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var body struct {
		Code common.ErrorCode `json:"code"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != common.CodeConflict {
		t.Fatalf("code = %v, want CodeConflict (409)", body.Code)
	}
	if len(pub.calls) != 0 {
		t.Fatalf("publisher called %d times on RUNNING task, want 0", len(pub.calls))
	}
}

// R4: a missing task returns CodeNotFound and publishes nothing.
func TestAdminPromoteIngestionTask_NotFound(t *testing.T) {
	setupAdminIngestionTaskDB(t)
	pub := &promoteTaskFakePublisher{}
	svc := servicepkg.NewIngestionTaskService()
	svc.SetTaskPublisher(pub)
	engine := adminIngestionTaskTestRouter(&Handler{service: &Service{ingestionTaskSvc: svc}})

	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/api/v1/admin/ingestion/tasks/ghost/promote", nil))
	if resp.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var body struct {
		Code common.ErrorCode `json:"code"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != common.CodeNotFound {
		t.Fatalf("code = %v, want CodeNotFound (404)", body.Code)
	}
	if len(pub.calls) != 0 {
		t.Fatalf("publisher called %d times for missing task, want 0", len(pub.calls))
	}
}

// R1 extension: a terminal (FAILED) task may be re-prioritized for a re-run.
func TestAdminPromoteIngestionTask_TerminalRepromotable(t *testing.T) {
	db := setupAdminIngestionTaskDB(t)
	if err := db.Create(&entity.IngestionTask{
		ID: "failed-task", UserID: "u4", DocumentID: "d4", DatasetID: "ds4", Status: common.FAILED,
	}).Error; err != nil {
		t.Fatal(err)
	}
	pub := &promoteTaskFakePublisher{}
	svc := servicepkg.NewIngestionTaskService()
	svc.SetTaskPublisher(pub)
	engine := adminIngestionTaskTestRouter(&Handler{service: &Service{ingestionTaskSvc: svc}})

	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/api/v1/admin/ingestion/tasks/failed-task/promote", nil))
	if resp.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var body struct {
		Code common.ErrorCode `json:"code"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != common.CodeSuccess {
		t.Fatalf("code = %v, want success", body.Code)
	}
	if len(pub.calls) != 1 || pub.calls[0] != common.TaskSubjectHigh {
		t.Fatalf("want one high-lane publish, got %v", pub.calls)
	}
}
