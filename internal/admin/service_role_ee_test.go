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

package admin

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"

	"github.com/gin-gonic/gin"
)

func TestListRoles(t *testing.T) {
	db := testutil.SetupTestDB(t, &entity.Role{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))

	adminService := NewService()
	roles, err := adminService.ListRoles(t.Context())
	if err != nil {
		t.Fatalf("ListRoles() on empty table error = %v", err)
	}
	if roles == nil || len(roles) != 0 {
		t.Fatalf("ListRoles() on empty table = %#v, want an empty slice", roles)
	}

	description := "System-level access"
	tenantID := "tenant-1"
	seed := []entity.Role{
		{ID: 3, RoleName: "system_editor", Description: &description},
		{ID: 2, RoleName: "tenant_reader", TenantID: &tenantID},
		{ID: 1, RoleName: "system_reader"},
	}
	if err = db.Create(&seed).Error; err != nil {
		t.Fatalf("create roles: %v", err)
	}

	roles, err = adminService.ListRoles(t.Context())
	if err != nil {
		t.Fatalf("ListRoles() error = %v", err)
	}
	if len(roles) != 2 {
		t.Fatalf("ListRoles() returned %d roles, want 2 system roles", len(roles))
	}

	firstID, ok := roles[0]["id"].(int32)
	if !ok || firstID != 1 || roles[0]["role_name"] != "system_reader" {
		t.Fatalf("first role = %#v, want system_reader with id 1", roles[0])
	}
	secondID, ok := roles[1]["id"].(int32)
	if !ok || secondID != 3 || roles[1]["role_name"] != "system_editor" {
		t.Fatalf("second role = %#v, want system_editor with id 3", roles[1])
	}
	if _, ok := roles[1]["tenant_id"]; ok {
		t.Fatalf("system role response unexpectedly contains tenant_id: %#v", roles[1])
	}
	gotDescription, ok := roles[1]["description"].(*string)
	if !ok || gotDescription == nil || *gotDescription != description {
		t.Fatalf("description = %#v, want %q", roles[1]["description"], description)
	}
}

func TestListRolesWithPermission(t *testing.T) {
	db := testutil.SetupTestDB(t, &entity.Role{}, &entity.RoleResource{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))

	tenantID := "tenant-1"
	roles := []entity.Role{
		{ID: 1, RoleName: "system_reader"},
		{ID: 2, RoleName: "tenant_reader", TenantID: &tenantID},
	}
	if err := db.Create(&roles).Error; err != nil {
		t.Fatalf("create roles: %v", err)
	}
	roleResources := []entity.RoleResource{
		{ID: 1, RoleID: 1, ResourceType: 1, Action: roleActionEnable | roleActionWrite},
		{ID: 2, RoleID: 1, ResourceType: 2, Action: roleActionRead | roleActionShare},
	}
	if err := db.Create(&roleResources).Error; err != nil {
		t.Fatalf("create role resources: %v", err)
	}

	rolesWithPermissions, err := NewService().ListRolesWithPermission(t.Context())
	if err != nil {
		t.Fatalf("ListRolesWithPermission() error = %v", err)
	}
	if len(rolesWithPermissions) != 1 {
		t.Fatalf("got %d roles, want only the system role", len(rolesWithPermissions))
	}
	if rolesWithPermissions[0]["role_name"] != "system_reader" {
		t.Fatalf("role_name = %#v, want system_reader", rolesWithPermissions[0]["role_name"])
	}

	permissions, ok := rolesWithPermissions[0]["permissions"].(map[string]map[string]bool)
	if !ok {
		t.Fatalf("permissions = %#v, want a resource permission map", rolesWithPermissions[0]["permissions"])
	}
	if got := permissions["dataset"]; !got["enable"] || got["read"] || !got["write"] || got["share"] {
		t.Fatalf("dataset permissions = %#v, want enable/write only", got)
	}
	if got := permissions["chat"]; got["enable"] || !got["read"] || got["write"] || !got["share"] {
		t.Fatalf("chat permissions = %#v, want read/share only", got)
	}
	if got := permissions["file"]; got["enable"] || got["read"] || got["write"] || got["share"] {
		t.Fatalf("file permissions = %#v, want all permissions disabled", got)
	}
}

func TestListResources(t *testing.T) {
	resources, err := NewService().ListResources(t.Context())
	if err != nil {
		t.Fatalf("ListResources() error = %v", err)
	}
	got, ok := resources["resource_types"].([]string)
	if !ok || len(got) != len(roleResourceNames) {
		t.Fatalf("resource_types = %#v, want %d resource types", resources["resource_types"], len(roleResourceNames))
	}
}

func TestSystemRoleDefaultModels(t *testing.T) {
	db := testutil.SetupTestDB(t,
		&entity.Role{},
		&entity.RoleDefaultModel{},
		&entity.TenantModelProvider{},
		&entity.TenantModelInstance{},
		&entity.TenantModel{},
	)
	t.Cleanup(testutil.ReplaceDBForTest(t, db))

	tenantID := "tenant-1"
	role := entity.Role{RoleName: "public"}
	if err := db.Create(&role).Error; err != nil {
		t.Fatalf("create system role: %v", err)
	}
	if err := db.Create(&entity.Role{RoleName: "tenant-only", TenantID: &tenantID}).Error; err != nil {
		t.Fatalf("create tenant role: %v", err)
	}
	if err := db.Create(&entity.TenantModelProvider{
		ID: "provider-1", UserID: "model-owner", TenantID: "tenant-1", ProviderName: "OpenAI",
	}).Error; err != nil {
		t.Fatalf("create model provider: %v", err)
	}
	if err := db.Create(&entity.TenantModelInstance{
		ID: "instance-1", InstanceName: "default", ProviderID: "provider-1", APIKey: "key", Status: "active",
	}).Error; err != nil {
		t.Fatalf("create model instance: %v", err)
	}
	if err := db.Create(&entity.TenantModel{
		ID: "model-1", ModelName: "chat-model", ProviderID: "provider-1", InstanceID: "instance-1",
		ModelType: int(entity.ModelTypeChat), Status: "active",
	}).Error; err != nil {
		t.Fatalf("create chat model: %v", err)
	}

	service := NewService()
	result, err := service.ShowRoleDefaultModels(t.Context(), "public")
	if err != nil {
		t.Fatalf("ShowRoleDefaultModels() on empty role error = %v", err)
	}
	if result["setup_status"] != "not_set" || len(result["model_list"].([]map[string]interface{})) != 0 {
		t.Fatalf("ShowRoleDefaultModels() on empty role = %#v, want not_set with no models", result)
	}

	if _, err := service.ShowRoleDefaultModels(t.Context(), "tenant-only"); err == nil {
		t.Fatal("ShowRoleDefaultModels() returned a tenant role from the system-role API")
	} else {
		assertRoleErrorCode(t, err, common.CodeNotFound)
	}

	updated, err := service.SetRoleDefaultModel(t.Context(), "public", "model-1", "llm")
	if err != nil || !updated {
		t.Fatalf("SetRoleDefaultModel() = (%v, %v), want (true, nil)", updated, err)
	}
	result, err = service.ShowRoleDefaultModels(t.Context(), "public")
	if err != nil {
		t.Fatalf("ShowRoleDefaultModels() after set error = %v", err)
	}
	items := result["model_list"].([]map[string]interface{})
	if result["setup_status"] != "partial" || len(items) != 1 {
		t.Fatalf("ShowRoleDefaultModels() after set = %#v, want one partial configuration", result)
	}
	if items[0]["role_id"] != role.ID || items[0]["model_type"] != "llm" ||
		items[0]["model_id"] != "model-1" || items[0]["tenant_id"] != "model-owner" {
		t.Fatalf("default model item = %#v, want model owner and selected model", items[0])
	}

	if _, err := service.SetRoleDefaultModel(t.Context(), "public", "model-1", "embedding"); err == nil {
		t.Fatal("SetRoleDefaultModel() accepted a chat model as an embedding model")
	} else {
		assertRoleErrorCode(t, err, common.CodeBadRequest)
	}
	if _, err := service.SetRoleDefaultModel(t.Context(), "public", "model-1", "chat"); err == nil {
		t.Fatal("SetRoleDefaultModel() accepted an unsupported default model type")
	} else {
		assertRoleErrorCode(t, err, common.CodeBadRequest)
	}

	reset, err := service.ResetRoleDefaultModel(t.Context(), "public", "llm")
	if err != nil || !reset {
		t.Fatalf("ResetRoleDefaultModel() = (%v, %v), want (true, nil)", reset, err)
	}
	result, err = service.ShowRoleDefaultModels(t.Context(), "public")
	if err != nil || result["setup_status"] != "not_set" || len(result["model_list"].([]map[string]interface{})) != 0 {
		t.Fatalf("ShowRoleDefaultModels() after reset = (%#v, %v), want not_set", result, err)
	}

	completeRows := make([]entity.RoleDefaultModel, 0, len(roleDefaultModelTypes))
	for modelType := range roleDefaultModelTypes {
		completeRows = append(completeRows, entity.RoleDefaultModel{
			RoleID: role.ID, ModelType: modelType, ModelID: "model-" + modelType, TenantID: "model-owner",
		})
	}
	if err := db.Create(&completeRows).Error; err != nil {
		t.Fatalf("create complete role default models: %v", err)
	}
	result, err = service.ShowRoleDefaultModels(t.Context(), "public")
	if err != nil || result["setup_status"] != "complete" {
		t.Fatalf("ShowRoleDefaultModels() with all model types = (%#v, %v), want complete", result, err)
	}
}

func TestShowRoleDefaultModelsRoute(t *testing.T) {
	db := testutil.SetupTestDB(t, &entity.Role{}, &entity.RoleDefaultModel{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))
	if err := db.Create(&entity.Role{RoleName: "public"}).Error; err != nil {
		t.Fatalf("create system role: %v", err)
	}

	originalMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(originalMode) })

	engine := gin.New()
	RegisterEERouter(engine.Group("/api/v1/admin"), NewRouter(NewHandler(NewService())))
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/roles/public/default-models", nil)
	engine.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("GET /admin/roles/public/default-models status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"setup_status":"not_set"`) || !strings.Contains(response.Body.String(), `"model_list":[]`) {
		t.Fatalf("GET /admin/roles/public/default-models response = %s, want empty model list and not_set status", response.Body.String())
	}
}

func TestListRolesWithPermissionRoute(t *testing.T) {
	db := testutil.SetupTestDB(t, &entity.Role{}, &entity.RoleResource{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))

	originalMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(originalMode) })

	engine := gin.New()
	RegisterEERouter(engine.Group("/api/v1/admin"), NewRouter(NewHandler(NewService())))

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/roles/permission", nil)
	engine.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("GET /admin/roles/permission status = %d, body = %s; want %d", response.Code, response.Body.String(), http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), `"data":[]`) {
		t.Fatalf("GET /admin/roles/permission response = %s; want an empty role list", response.Body.String())
	}
}

func TestSystemRoleCRUDAndPermissionChanges(t *testing.T) {
	db := testutil.SetupTestDB(t, &entity.Role{}, &entity.RoleResource{}, &entity.Tenant{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))

	tenantID := "tenant-1"
	if err := db.Create(&entity.Role{RoleName: "reader", TenantID: &tenantID}).Error; err != nil {
		t.Fatalf("create tenant role: %v", err)
	}

	adminService := NewService()
	created, err := adminService.CreateRole(t.Context(), " reader ", "Read access")
	if err != nil {
		t.Fatalf("CreateRole() error = %v", err)
	}
	roleID, ok := created["id"].(int32)
	if !ok || created["role_name"] != "reader" {
		t.Fatalf("CreateRole() = %#v, want system role reader", created)
	}

	if _, err = adminService.CreateRole(t.Context(), "reader", "duplicate"); err == nil {
		t.Fatal("CreateRole() accepted a duplicate system role name")
	} else {
		assertRoleErrorCode(t, err, common.CodeConflict)
	}

	updated, err := adminService.UpdateRole(t.Context(), "reader", "Updated description")
	if err != nil {
		t.Fatalf("UpdateRole() error = %v", err)
	}
	if updated["description"] == nil || *updated["description"].(*string) != "Updated description" {
		t.Fatalf("updated description = %#v", updated["description"])
	}

	if _, err = adminService.GrantRolePermission(t.Context(), "reader", []string{"read", "share"}, "dataset"); err != nil {
		t.Fatalf("GrantRolePermission() error = %v", err)
	}
	if _, err = adminService.RevokeRolePermission(t.Context(), "reader", []string{"share"}, "dataset"); err != nil {
		t.Fatalf("RevokeRolePermission() error = %v", err)
	}
	if _, err = adminService.GrantRolePermission(t.Context(), "reader", []string{"write"}, "model_provider"); err == nil {
		t.Fatal("GrantRolePermission() accepted write on model_provider")
	} else {
		assertRoleErrorCode(t, err, common.CodeBadRequest)
	}

	permissionsResult, err := adminService.ShowRolePermission(t.Context(), "reader")
	if err != nil {
		t.Fatalf("ShowRolePermission() error = %v", err)
	}
	permissions, ok := permissionsResult["permissions"].(map[string]map[string]bool)
	if !ok || !permissions["dataset"]["read"] || permissions["dataset"]["share"] || permissions["dataset"]["write"] {
		t.Fatalf("dataset permissions = %#v, want read only", permissions["dataset"])
	}
	if role, ok := permissionsResult["role"].(map[string]interface{}); !ok || role["id"] != roleID {
		t.Fatalf("role detail = %#v, want role ID %d", permissionsResult["role"], roleID)
	}

	roles, err := adminService.ListRolesWithPermission(t.Context())
	if err != nil {
		t.Fatalf("ListRolesWithPermission() error = %v", err)
	}
	if len(roles) != 1 || roles[0]["role_name"] != "reader" {
		t.Fatalf("ListRolesWithPermission() = %#v, want only the system role", roles)
	}
}

func TestDropSystemRoleChecksTenantAssignment(t *testing.T) {
	db := testutil.SetupTestDB(t, &entity.Role{}, &entity.RoleResource{}, &entity.RoleDefaultModel{}, &entity.Tenant{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))

	role := entity.Role{RoleName: "tenant_default"}
	if err := db.Create(&role).Error; err != nil {
		t.Fatalf("create system role: %v", err)
	}
	if err := db.Create(&entity.RoleResource{RoleID: role.ID, ResourceType: 1, Action: roleActionRead}).Error; err != nil {
		t.Fatalf("create role permission: %v", err)
	}
	if err := db.Create(&entity.RoleDefaultModel{
		RoleID: role.ID, ModelType: "llm", ModelID: "model-1", TenantID: "model-owner",
	}).Error; err != nil {
		t.Fatalf("create role default model: %v", err)
	}

	assignedRoleID := role.ID
	if err := db.Create(&entity.Tenant{
		ID:        "tenant-1",
		LLMID:     "builtin",
		EmbdID:    "builtin",
		ASRID:     "builtin",
		RerankID:  "builtin",
		ParserIDs: "{}",
		RoleID:    &assignedRoleID,
	}).Error; err != nil {
		t.Fatalf("create tenant assignment: %v", err)
	}

	if _, err := NewService().DropRole(t.Context(), role.RoleName); err == nil {
		t.Fatal("DropRole() deleted a system role assigned to a Tenant")
	} else {
		assertRoleErrorCode(t, err, common.CodeConflict)
	}

	if err := db.Model(&entity.Tenant{}).Where("id = ?", "tenant-1").Update("role_id", nil).Error; err != nil {
		t.Fatalf("clear tenant role assignment: %v", err)
	}
	if _, err := NewService().DropRole(t.Context(), role.RoleName); err != nil {
		t.Fatalf("DropRole() error = %v", err)
	}

	var remainingPermissions int64
	if err := db.Model(&entity.RoleResource{}).Where("role_id = ?", role.ID).Count(&remainingPermissions).Error; err != nil {
		t.Fatalf("count role permissions: %v", err)
	}
	if remainingPermissions != 0 {
		t.Fatalf("role permissions remaining = %d, want 0", remainingPermissions)
	}
	var remainingDefaultModels int64
	if err := db.Model(&entity.RoleDefaultModel{}).Where("role_id = ?", role.ID).Count(&remainingDefaultModels).Error; err != nil {
		t.Fatalf("count role default models: %v", err)
	}
	if remainingDefaultModels != 0 {
		t.Fatalf("role default models remaining = %d, want 0", remainingDefaultModels)
	}
}

func assertRoleErrorCode(t *testing.T, err error, want common.ErrorCode) {
	t.Helper()
	var coded *common.CodedError
	if !errors.As(err, &coded) {
		t.Fatalf("error = %v, want coded error %d", err, want)
	}
	if coded.Code != want {
		t.Fatalf("error code = %d, want %d (%s)", coded.Code, want, coded.Message)
	}
}
