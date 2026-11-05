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

package service

import (
	"encoding/json"
	"errors"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
)

func TestMCPServerPermissionsUsePolicyAcrossServiceOperations(t *testing.T) {
	db := setupServiceTestDB(t)
	if err := db.AutoMigrate(&entity.MCPServer{}); err != nil {
		t.Fatalf("migrate MCP server: %v", err)
	}
	pushServiceDB(t, db)
	addMCPOwnerMembership(t, db, "owner", "team")
	active := string(entity.StatusValid)
	if err := db.Create(&entity.UserTenant{
		ID: "mcp-member-membership", UserID: "member", TenantID: "team",
		Role: string(permission.RoleNormal), InvitedBy: "owner", Status: &active,
	}).Error; err != nil {
		t.Fatalf("create MCP member membership: %v", err)
	}
	addMCPOwnerMembership(t, db, "member", "member")
	if err := db.Create(&entity.MCPServer{
		ID: "team-server", Name: "shared", TenantID: "team", URL: "https://example.com/mcp",
		ServerType: mcpServerTypeStreamableHTTP,
	}).Error; err != nil {
		t.Fatalf("create MCP server: %v", err)
	}

	server, captured := newMCPDiscoveryTestServer(t)
	defer server.Close()
	withMCPDiscoveryOverrides(t, server)
	service := NewMCPService()
	ctx := serviceTestContext(t, "member", "team", "owner")
	restrictServiceTestFeatureRole(t, "team", "mcp", testRoleActionEnable|testRoleActionRead)

	if _, code, err := service.GetMCPServer(ctx, "team-server"); err == nil || code != common.CodeNotFound || !errors.Is(err, permission.ErrPermissionDenied) {
		t.Fatalf("member direct read should be hidden: code=%d err=%v", code, err)
	}
	listed, code, err := service.ListMCPServers(ctx, "team", nil, "", 0, 0, "create_time", false)
	if err != nil || code != common.CodeSuccess || len(listed.MCPServers) != 0 || listed.Total != 0 {
		t.Fatalf("member list should hide an unshared server: response=%#v code=%d err=%v", listed, code, err)
	}

	name, _ := json.Marshal("renamed")
	if _, code, err := service.UpdateMCPServer(ctx, "team-server", UpdateMCPServerRequest{"name": name}); err == nil || code != common.CodeNotFound || !errors.Is(err, permission.ErrPermissionDenied) {
		t.Fatalf("member update should be hidden: code=%d err=%v", code, err)
	}
	if _, code, err := service.DeleteMCPServer(ctx, "team-server"); err == nil || code != common.CodeNotFound || !errors.Is(err, permission.ErrPermissionDenied) {
		t.Fatalf("member delete should be hidden: code=%d err=%v", code, err)
	}
	if _, err := service.TestServer(ctx, "team-server", &TestServerRequest{
		URL: "https://example.com/mcp", ServerType: mcpServerTypeStreamableHTTP,
	}); err == nil || !errors.Is(err, permission.ErrPermissionDenied) {
		t.Fatalf("member connection test should require update permission: %v", err)
	}
	personalCtx := serviceTestContext(t, "member", "member")
	created, code, err := service.CreateMCPServer(personalCtx, CreateMCPServerRequest{
		Name: "new", URL: server.URL, ServerType: mcpServerTypeStreamableHTTP,
	})
	if err != nil || code != common.CodeSuccess || created.TenantID != "member" {
		t.Fatalf("member should create after switching to their personal tenant: result=%+v code=%d err=%v", created, code, err)
	}
	if len(captured()) == 0 {
		t.Fatal("MCP creation did not discover tools from the configured server")
	}
}

func TestModelPermissionsAllowMemberUseButHideInstanceConfiguration(t *testing.T) {
	db := setupModelProviderServiceTestDB(t)
	useModelProviderServiceTestDB(t, db)
	seedModelProviderServiceScope(t, db)
	activeStatus := "1"
	if err := db.Create(&entity.UserTenant{
		ID:        "user-tenant-member",
		UserID:    "member-1",
		TenantID:  "user-1",
		Role:      string(permission.RoleNormal),
		InvitedBy: "user-1",
		Status:    &activeStatus,
	}).Error; err != nil {
		t.Fatalf("seed tenant member: %v", err)
	}
	if err := db.Create(&entity.UserTenant{
		ID: "member-1-owner", UserID: "member-1", TenantID: "member-1",
		Role: string(permission.RoleOwner), InvitedBy: "member-1", Status: &activeStatus,
	}).Error; err != nil {
		t.Fatalf("seed member's personal tenant: %v", err)
	}

	ctx := serviceTestContext(t, "member-1", "user-1", "user-1")
	grantServiceTestResource(t, "user-1", "model_instance", "instance-1", "member-1", testResourcePermissionRead)
	service := NewModelProviderService()
	providers, code, err := service.ListProvidersOfTenant(ctx, "member-1")
	if err != nil || code != common.CodeSuccess || len(providers) != 0 {
		t.Fatalf("ListProvidersOfTenant() = (%v, %v, %d providers), want success without exposing the owner's provider configuration", code, err, len(providers))
	}

	instance, showCode, showErr := service.ShowProviderInstance(ctx, "OpenAI", "instance-1", "member-1")
	if showErr != nil || showCode != common.CodeSuccess {
		t.Fatalf("ShowProviderInstance() = (%v, %v), want visible shared instance", showCode, showErr)
	}
	if _, ok := instance["api_key"]; ok {
		t.Fatalf("shared instance exposed API key: %#v", instance)
	}

	factory := NewModelFactory()
	access := Identity{UserID: "member-1", TenantID: "user-1"}
	if _, err = factory.resolveConfig(ctx, access, entity.ModelTypeChat, "model-1"); err != nil {
		t.Fatalf("member should be able to use the tenant model: %v", err)
	}
	if _, err = factory.resolveConfig(ctx, access, entity.ModelTypeChat, "gpt-test@default@OpenAI"); err != nil {
		t.Fatalf("member should be able to use the composite model reference: %v", err)
	}

	code, err = service.AlterModel(ctx, "OpenAI", "default", "gpt-test", "member-1", "", map[string]interface{}{"status": "inactive"})
	if err == nil || code != common.CodeNotFound {
		t.Fatalf("AlterModel() = (%v, %v), want hidden model update denial", code, err)
	}
	var model entity.TenantModel
	if err := db.Where("id = ?", "model-1").First(&model).Error; err != nil {
		t.Fatalf("reload tenant model: %v", err)
	}
	if model.Status != "active" {
		t.Fatalf("denied update changed model status to %q", model.Status)
	}
}

func TestSkillSpacePermissionsUsePolicyAcrossServiceOperations(t *testing.T) {
	db := setupServiceTestDB(t)
	if err := db.AutoMigrate(&entity.SkillSpace{}); err != nil {
		t.Fatalf("migrate skill spaces: %v", err)
	}
	pushServiceDB(t, db)

	active := string(entity.StatusValid)
	for _, membership := range []entity.UserTenant{
		{ID: "skill-owner-membership", UserID: "owner", TenantID: "team", Role: string(permission.RoleOwner), Status: &active},
		{ID: "skill-member-membership", UserID: "member", TenantID: "team", Role: string(permission.RoleNormal), InvitedBy: "owner", Status: &active},
		{ID: "skill-member-personal-owner", UserID: "member", TenantID: "member", Role: string(permission.RoleOwner), InvitedBy: "member", Status: &active},
		{ID: "skill-outsider-membership", UserID: "outsider", TenantID: "other", Role: string(permission.RoleOwner), Status: &active},
	} {
		if err := db.Create(&membership).Error; err != nil {
			t.Fatalf("create membership %q: %v", membership.ID, err)
		}
	}
	folder := entity.File{
		ID: "skill-folder", ParentID: "root", TenantID: "team", CreatedBy: "owner",
		Name: "shared", Type: "folder", SourceType: "skill_space",
	}
	if err := db.Create(&folder).Error; err != nil {
		t.Fatalf("create skill folder: %v", err)
	}
	space := entity.SkillSpace{
		ID: "shared-space", TenantID: "team", Name: "shared", FolderID: folder.ID,
		Status: entity.SpaceStatusActive,
	}
	if err := db.Create(&space).Error; err != nil {
		t.Fatalf("create skill space: %v", err)
	}

	service := NewSkillSpaceService(nil)
	ctx := serviceTestContext(t, "member", "team", "owner")
	outsiderCtx := serviceTestContext(t, "outsider", "other")
	ownerCtx := serviceTestContext(t, "owner", "team", "member")
	restrictServiceTestFeatureRole(t, "team", "file", testRoleActionEnable|testRoleActionRead)
	grantServiceTestResource(t, "team", "file", space.ID, "member", testResourcePermissionRead)

	listed, code, err := service.ListSpaces(ctx, "member")
	if err != nil || code != common.CodeSuccess {
		t.Fatalf("member list: code=%d err=%v", code, err)
	}
	spaces, ok := listed["spaces"].([]map[string]interface{})
	if !ok || len(spaces) != 1 || spaces[0]["id"] != space.ID {
		t.Fatalf("member list result = %#v, want only %q", listed["spaces"], space.ID)
	}

	if _, code, err := service.GetSpace(ctx, "member", space.ID); err != nil || code != common.CodeSuccess {
		t.Fatalf("member read: code=%d err=%v", code, err)
	}
	if _, code, err := service.UpdateSpace(ctx, "member", space.ID, &UpdateSpaceRequest{Description: "forbidden"}); err == nil || code != common.CodeNotFound || !errors.Is(err, permission.ErrPermissionDenied) {
		t.Fatalf("member update should be hidden: code=%d err=%v", code, err)
	}
	if code, err := service.DeleteSpace(ctx, "member", space.ID, nil); err == nil || code != common.CodeNotFound || !errors.Is(err, permission.ErrPermissionDenied) {
		t.Fatalf("member delete should be hidden: code=%d err=%v", code, err)
	}
	if _, code, err := service.GetSpace(outsiderCtx, "outsider", space.ID); err == nil || code != common.CodeNotFound || !errors.Is(err, permission.ErrResourceNotFound) {
		t.Fatalf("outsider read should be hidden: code=%d err=%v", code, err)
	}

	personalCtx := serviceTestContext(t, "member", "member")
	created, code, err := service.CreateSpace(personalCtx, "member", &CreateSpaceRequest{Name: "personal"})
	if err != nil || code != common.CodeSuccess || created["tenant_id"] != "member" {
		t.Fatalf("member should create a personal skill space after switching tenants: result=%#v code=%d err=%v", created, code, err)
	}
	if tenantID, err := resolveSkillSpaceTenantID(ownerCtx, "owner", "default", permission.OperationUse); err != nil || tenantID != "team" {
		t.Fatalf("default skill space tenant = %q, err=%v; want team", tenantID, err)
	}

	updated, code, err := service.UpdateSpace(ownerCtx, "owner", space.ID, &UpdateSpaceRequest{Description: "updated"})
	if err != nil || code != common.CodeSuccess || updated["description"] != "updated" {
		t.Fatalf("owner update: result=%#v code=%d err=%v", updated, code, err)
	}

	indexer := NewSkillIndexerService()
	if err := indexer.BatchIndexSkills(ctx, "member", space.ID, nil, nil, ""); !errors.Is(err, permission.ErrPermissionDenied) {
		t.Fatalf("member indexing should be denied before engine access, got %v", err)
	}
	if err := indexer.DeleteSkillIndex(ctx, "member", space.ID, "skill", nil); !errors.Is(err, permission.ErrPermissionDenied) {
		t.Fatalf("member index deletion should be denied before engine access, got %v", err)
	}
	if _, _, err := NewSkillSearchService().UpdateConfig(ctx, "member", &UpdateConfigRequest{
		SpaceID: space.ID, EmbdID: "model", VectorSimilarityWeight: 0.5,
		SimilarityThreshold: 0.2, TopK: 10,
	}); !errors.Is(err, permission.ErrPermissionDenied) {
		t.Fatalf("member config update should be denied, got %v", err)
	}
	if _, _, err := NewSkillSearchService().Search(outsiderCtx, "outsider", &SearchRequest{SpaceID: space.ID}, nil); !errors.Is(err, permission.ErrResourceNotFound) {
		t.Fatalf("outsider search should be denied before engine access, got %v", err)
	}
}
