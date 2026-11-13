//
//  Copyright 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//

//go:build ee

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
	"ragflow/internal/permission/testsupport"
	servicepkg "ragflow/internal/service"
)

// tenantPromoteFakePublisher records publishes so the test can assert the lane
// without a live NATS server.
type tenantPromoteFakePublisher struct {
	calls []string
	msgs  []common.TaskMessage
}

func (f *tenantPromoteFakePublisher) PublishTaskMessage(subject string, msg common.TaskMessage) error {
	f.calls = append(f.calls, subject)
	f.msgs = append(f.msgs, msg)
	return nil
}

func setupTenantPromoteDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+url.QueryEscape(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{
		TranslateError: true,
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&entity.IngestionTask{},
		&entity.Knowledgebase{},
		&entity.User{},
		&entity.Tenant{},
		&entity.UserTenant{},
		&entity.Role{},
		&entity.RoleResource{},
		&entity.GroupInfo{},
		&entity.GroupMember{},
		&entity.Permission{},
		&entity.PermissionChangeLog{},
		&entity.TenantModelProvider{},
		&entity.TenantModelInstance{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	orig := dao.DB
	dao.DB = db
	t.Cleanup(func() { dao.DB = orig })
	return db
}

// insertTenantPromoteKB creates a dataset owned by tenantID and prepares the
// enterprise permission fixture so a member of that tenant passes the dataset
// resource check.
func insertTenantPromoteKB(t *testing.T, kbID, tenantID string) {
	t.Helper()
	status := string(entity.StatusValid)
	kb := &entity.Knowledgebase{
		ID:         kbID,
		TenantID:   tenantID,
		Name:       "promote-kb",
		EmbdID:     "BAAI/bge-large-zh-v1.5@Builtin",
		CreatedBy:  tenantID,
		Permission: string(entity.TenantPermissionMe),
		Status:     &status,
	}
	if err := dao.DB.Create(kb).Error; err != nil {
		t.Fatalf("create kb: %v", err)
	}
	if err := testsupport.PrepareEnterprisePermissionFixture(dao.DB, tenantID, tenantID); err != nil {
		t.Fatalf("prepare permission fixture: %v", err)
	}
}

func insertTenantPromoteTask(t *testing.T, id, datasetID, status string) {
	t.Helper()
	task := &entity.IngestionTask{
		ID:         id,
		UserID:     "u-" + id,
		DocumentID: "d-" + id,
		DatasetID:  datasetID,
		Status:     status,
	}
	if err := dao.DB.Create(task).Error; err != nil {
		t.Fatalf("create task %s: %v", id, err)
	}
}

func serveTenantPromote(t *testing.T, userID, tenantID, datasetID, taskID string) (int, map[string]interface{}) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/api/v1/datasets/:dataset_id/ingestion/tasks/:task_id/promote", (&DocumentHandler{}).PromoteIngestionTask)
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/datasets/"+datasetID+"/ingestion/tasks/"+taskID+"/promote", nil)
	req = req.WithContext(permission.WithIdentity(req.Context(), userID, tenantID))
	engine.ServeHTTP(resp, req)
	var body map[string]interface{}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, resp.Body.String())
	}
	return resp.Code, body
}

func withFakePromotePublisher(t *testing.T, pub *tenantPromoteFakePublisher) {
	t.Helper()
	orig := newPromoteTaskService
	newPromoteTaskService = func() *servicepkg.IngestionTaskService {
		svc := servicepkg.NewIngestionTaskService()
		svc.SetTaskPublisher(pub)
		return svc
	}
	t.Cleanup(func() { newPromoteTaskService = orig })
}

// R1/R2 (positive): a tenant admin promotes a task that belongs to a dataset
// they own; the high lane is reached.
func TestTenantPromoteIngestionTask_OwnerPromotesOwnTask(t *testing.T) {
	setupTenantPromoteDB(t)
	insertTenantPromoteKB(t, "ds-1", "tenant-1")
	insertTenantPromoteTask(t, "task-1", "ds-1", common.CREATED)

	pub := &tenantPromoteFakePublisher{}
	withFakePromotePublisher(t, pub)

	code, body := serveTenantPromote(t, "tenant-1", "tenant-1", "ds-1", "task-1")
	if code != http.StatusOK {
		t.Fatalf("HTTP status = %d, body = %s", code, promoteMustJSON(body))
	}
	if body["code"].(float64) != float64(common.CodeSuccess) {
		t.Fatalf("code = %v, want success, body = %s", body["code"], promoteMustJSON(body))
	}
	if len(pub.calls) != 1 || pub.calls[0] != common.TaskSubjectHigh {
		t.Fatalf("want one high-lane publish, got %v", pub.calls)
	}
}

// R3: a RUNNING task is rejected (CodeConflict), publisher not called.
func TestTenantPromoteIngestionTask_RunningRejected(t *testing.T) {
	setupTenantPromoteDB(t)
	insertTenantPromoteKB(t, "ds-1", "tenant-1")
	insertTenantPromoteTask(t, "running-task", "ds-1", common.RUNNING)

	pub := &tenantPromoteFakePublisher{}
	withFakePromotePublisher(t, pub)

	code, body := serveTenantPromote(t, "tenant-1", "tenant-1", "ds-1", "running-task")
	if code != http.StatusOK {
		t.Fatalf("HTTP status = %d, body = %s", code, promoteMustJSON(body))
	}
	if int(body["code"].(float64)) != int(common.CodeConflict) {
		t.Fatalf("code = %v, want CodeConflict", body["code"])
	}
	if len(pub.calls) != 0 {
		t.Fatalf("publisher called %d times on RUNNING task, want 0", len(pub.calls))
	}
}

// R4: a missing task returns CodeNotFound and publishes nothing.
func TestTenantPromoteIngestionTask_NotFound(t *testing.T) {
	setupTenantPromoteDB(t)
	insertTenantPromoteKB(t, "ds-1", "tenant-1")

	pub := &tenantPromoteFakePublisher{}
	withFakePromotePublisher(t, pub)

	code, body := serveTenantPromote(t, "tenant-1", "tenant-1", "ds-1", "ghost")
	if code != http.StatusOK {
		t.Fatalf("HTTP status = %d, body = %s", code, promoteMustJSON(body))
	}
	if int(body["code"].(float64)) != int(common.CodeNotFound) {
		t.Fatalf("code = %v, want CodeNotFound", body["code"])
	}
	if len(pub.calls) != 0 {
		t.Fatalf("publisher called %d times for missing task, want 0", len(pub.calls))
	}
}

// R4 (cross-dataset): the same tenant owns both datasets, but the task belongs
// to ds-2 while the path names ds-1. Permission passes (owner of ds-1) but the
// dataset mismatch is rejected with CodeNotFound — the caller cannot probe
// other datasets' tasks.
func TestTenantPromoteIngestionTask_WrongDatasetRejected(t *testing.T) {
	setupTenantPromoteDB(t)
	insertTenantPromoteKB(t, "ds-1", "tenant-1")
	insertTenantPromoteKB(t, "ds-2", "tenant-1")
	insertTenantPromoteTask(t, "task-x", "ds-2", common.CREATED)

	pub := &tenantPromoteFakePublisher{}
	withFakePromotePublisher(t, pub)

	code, body := serveTenantPromote(t, "tenant-1", "tenant-1", "ds-1", "task-x")
	if code != http.StatusOK {
		t.Fatalf("HTTP status = %d, body = %s", code, promoteMustJSON(body))
	}
	if int(body["code"].(float64)) != int(common.CodeNotFound) {
		t.Fatalf("code = %v, want CodeNotFound for cross-dataset reference", body["code"])
	}
	if len(pub.calls) != 0 {
		t.Fatalf("publisher called %d times for cross-dataset task, want 0", len(pub.calls))
	}
}

// R2 (negative): a tenant admin from tenant-2 cannot promote a task in a
// dataset owned by tenant-1. The permission engine scopes dataset lookups to the
// caller's tenant, so the foreign dataset is invisible and CheckResource returns
// ErrResourceNotFound, which the response layer maps to CodeNotFound (404) — the
// same secure "do not reveal existence" posture as the wrong-dataset case. The
// publisher must not be invoked.
func TestTenantPromoteIngestionTask_CrossTenantDenied(t *testing.T) {
	setupTenantPromoteDB(t)
	insertTenantPromoteKB(t, "ds-1", "tenant-1")
	insertTenantPromoteKB(t, "ds-2", "tenant-2")
	insertTenantPromoteTask(t, "task-1", "ds-1", common.CREATED)

	pub := &tenantPromoteFakePublisher{}
	withFakePromotePublisher(t, pub)

	code, body := serveTenantPromote(t, "tenant-2", "tenant-2", "ds-1", "task-1")
	if code != http.StatusOK {
		t.Fatalf("HTTP status = %d, body = %s", code, promoteMustJSON(body))
	}
	if int(body["code"].(float64)) != int(common.CodeNotFound) {
		t.Fatalf("code = %v, want CodeNotFound for cross-tenant request (dataset invisible to tenant-2)", body["code"])
	}
	if len(pub.calls) != 0 {
		t.Fatalf("publisher called %d times for cross-tenant request, want 0", len(pub.calls))
	}
}

func promoteMustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
