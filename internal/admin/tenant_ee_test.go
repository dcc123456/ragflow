//
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
//

//go:build ee

package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"

	"github.com/gin-gonic/gin"
)

func newAdminEETestEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("user_id", "system-admin")
		c.Next()
	})
	RegisterEERouter(engine.Group("/api/v1/admin"), NewRouter(NewHandler(NewService())))
	return engine
}

func TestAdminTenantListAndInviteSetting(t *testing.T) {
	db := testutil.SetupTestDB(t, &entity.Tenant{}, &entity.UserTenant{}, &entity.User{}, &entity.PermissionChangeLog{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))

	status := "1"
	tenantName := "Acme"
	roleID := int32(7)
	if err := db.Create(&entity.Tenant{
		ID: "tenant-acme", Name: &tenantName, ParserIDs: "", InviteEnabled: true, RoleID: &roleID, Status: &status,
	}).Error; err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if err := db.Create(&entity.Tenant{
		ID: "tenant-other", ParserIDs: "", InviteEnabled: false, Status: &status,
	}).Error; err != nil {
		t.Fatalf("create second tenant: %v", err)
	}
	nickname := "Tenant Owner"
	user := entity.User{ID: "owner-user", Nickname: nickname, Email: "owner@example.com", IsAuthenticated: "1", IsActive: "1", IsAnonymous: "0", Status: &status}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create tenant owner: %v", err)
	}
	if err := db.Create(&entity.UserTenant{ID: "owner-membership", UserID: user.ID, TenantID: "tenant-acme", Role: "owner", Status: &status}).Error; err != nil {
		t.Fatalf("create owner membership: %v", err)
	}

	engine := newAdminEETestEngine()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants?invite_enabled=true&page=1&page_size=10", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /admin/tenants status = %d, body = %s", response.Code, response.Body.String())
	}
	for _, expected := range []string{`"total":1`, `"tenant_id":"tenant-acme"`, `"owner_email":"owner@example.com"`, `"role_id":7`, `"invite_enabled":true`} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("GET /admin/tenants response = %s, missing %s", response.Body.String(), expected)
		}
	}

	request = httptest.NewRequest(http.MethodPatch, "/api/v1/admin/tenants/tenant-acme", strings.NewReader(`{"invite_enabled":false}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"invite_enabled":false`) {
		t.Fatalf("PATCH /admin/tenants/:tenant_id = (%d, %s), want invite disabled", response.Code, response.Body.String())
	}
	var updated entity.Tenant
	if err := db.Where("id = ?", "tenant-acme").Take(&updated).Error; err != nil {
		t.Fatalf("load updated tenant: %v", err)
	}
	if updated.InviteEnabled {
		t.Fatalf("InviteEnabled = %v, want false", updated.InviteEnabled)
	}
	var changeLog entity.PermissionChangeLog
	if err := db.Where("event_type = ? AND tenant_id = ?", "tenant.invite_setting.updated", "tenant-acme").Take(&changeLog).Error; err != nil {
		t.Fatalf("load invite setting audit log: %v", err)
	}
	if changeLog.OperatorID != "" || changeLog.ActorUserID == nil || *changeLog.ActorUserID != "system-admin" || changeLog.ScopeType != "system" || changeLog.BeforeData == nil || *changeLog.BeforeData != `{"invite_enabled":true}` || changeLog.AfterData == nil || *changeLog.AfterData != `{"invite_enabled":false}` {
		t.Fatalf("invite setting audit log = %#v, want actor and before/after values", changeLog)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/permission-logs?tenant_id=tenant-acme&event_type=tenant.invite_setting.updated", nil)
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"total":1`) || !strings.Contains(response.Body.String(), `"actor_user_id":"system-admin"`) {
		t.Fatalf("GET /admin/permission-logs after invite change = (%d, %s), want the recorded admin change", response.Code, response.Body.String())
	}
}

func TestAdminDefaultTenantInviteSetting(t *testing.T) {
	db := testutil.SetupTestDB(t, &entity.SystemSettings{}, &entity.PermissionChangeLog{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))

	engine := newAdminEETestEngine()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings/default-invite-enabled", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"invite_enabled":true`) {
		t.Fatalf("GET default invite setting = (%d, %s), want true", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/default-invite-enabled", strings.NewReader(`{"invite_enabled":false}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"invite_enabled":false`) {
		t.Fatalf("PUT default invite setting = (%d, %s), want false", response.Code, response.Body.String())
	}

	var setting entity.SystemSettings
	if err := db.Where("name = ?", "tenant.default_invite_enabled").Take(&setting).Error; err != nil {
		t.Fatalf("load updated system setting: %v", err)
	}
	if setting.Value != "false" || setting.DataType != "bool" || setting.Source != "admin" {
		t.Fatalf("updated system setting = %+v, want admin bool false", setting)
	}
	var audit entity.PermissionChangeLog
	if err := db.Where("event_type = ?", "tenant.invite_default.updated").Take(&audit).Error; err != nil {
		t.Fatalf("load system setting audit row: %v", err)
	}
	if audit.ActorUserID == nil || *audit.ActorUserID != "system-admin" || audit.ScopeType != "system" || audit.BeforeData == nil || *audit.BeforeData != `{"invite_enabled":true}` || audit.AfterData == nil || *audit.AfterData != `{"invite_enabled":false}` {
		t.Fatalf("default invite audit = %+v, want actor and before/after values", audit)
	}

	request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/default-invite-enabled", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("PUT default invite setting without value status = %d, want 400", response.Code)
	}
}

func TestAdminPermissionLogListFilters(t *testing.T) {
	db := testutil.SetupTestDB(t, &entity.PermissionChangeLog{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))

	logs := []entity.PermissionChangeLog{
		{ID: "log-match", TenantID: "tenant-1", OperatorID: "membership-1", TargetType: "user", TargetID: "user-2", ResourceType: "dataset", ResourceID: "dataset-1", OldPermission: 1, NewPermission: 3, ActionType: "grant", EventType: "permission_changed", ScopeType: "tenant"},
		{ID: "log-other", TenantID: "tenant-2", OperatorID: "membership-2", TargetType: "group", TargetID: "group-2", ResourceType: "dataset", ResourceID: "dataset-2", OldPermission: 0, NewPermission: 1, ActionType: "grant", EventType: "permission_changed", ScopeType: "tenant"},
	}
	if err := db.Create(&logs).Error; err != nil {
		t.Fatalf("create permission logs: %v", err)
	}

	engine := newAdminEETestEngine()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/permission-logs?tenant_id=tenant-1&resource_type=dataset&page=1&page_size=10", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /admin/permission-logs status = %d, body = %s", response.Code, response.Body.String())
	}
	for _, expected := range []string{`"total":1`, `"id":"log-match"`, `"resource_id":"dataset-1"`} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("GET /admin/permission-logs response = %s, missing %s", response.Body.String(), expected)
		}
	}
	if strings.Contains(response.Body.String(), `"id":"log-other"`) {
		t.Fatalf("GET /admin/permission-logs returned a row outside the tenant filter: %s", response.Body.String())
	}
}

func TestAdminTenantListKeywordAndStatusValidation(t *testing.T) {
	db := testutil.SetupTestDB(t, &entity.Tenant{}, &entity.UserTenant{}, &entity.User{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))
	status := "1"
	name := "Searchable Tenant"
	if err := db.Create(&entity.Tenant{ID: "tenant-search", Name: &name, ParserIDs: "", InviteEnabled: false, Status: &status}).Error; err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	engine := newAdminEETestEngine()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants?keyword=Searchable&status=1", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"tenant_id":"tenant-search"`) {
		t.Fatalf("GET /admin/tenants keyword filter = (%d, %s)", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants?invite_enabled=enabled", nil)
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"code":400`) {
		t.Fatalf("GET /admin/tenants invalid filter = (%d, %s), want bad request", response.Code, response.Body.String())
	}
}

func tenantDAOTestExtraModels() []interface{} { return nil }
