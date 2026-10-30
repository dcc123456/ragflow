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

package service

import (
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"
	"ragflow/internal/permission"

	"gorm.io/gorm"
)

func setupResourcePermissionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.SetupTestDB(t,
		&entity.Tenant{},
		&entity.User{},
		&entity.UserTenant{},
		&entity.Role{},
		&entity.RoleResource{},
		&entity.Permission{},
		&entity.PermissionChangeLog{},
		&entity.GroupInfo{},
		&entity.GroupMember{},
		&entity.Knowledgebase{},
		&entity.File{},
		&entity.Chat{},
		&entity.Document{},
		&entity.UserCanvas{},
		&entity.Memory{},
		&entity.Search{},
		&entity.MCPServer{},
		&entity.TenantModelProvider{},
		&entity.TenantModelInstance{},
	)
	t.Cleanup(testutil.ReplaceDBForTest(t, db))
	return db
}

func TestCreateAgentPersistsAuthenticatedUserAndActiveTenant(t *testing.T) {
	db := setupResourcePermissionTestDB(t)
	seedTenantRoleTenant(t, db, "tenant-agent")
	seedTenantRoleMember(t, db, "agent-owner-link", "agent-owner", "tenant-agent", string(permission.RoleOwner))
	ctx := permission.WithSubject(t.Context(), permission.Subject{UserID: "agent-owner", TenantID: "tenant-agent"})

	row, code, err := NewAgentService().CreateAgent(ctx, "agent-owner", &CreateAgentRequest{
		Title: sptr("Tenant-scoped Agent"),
		DSL:   entity.JSONMap{},
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if code != common.CodeSuccess {
		t.Fatalf("create agent code = %d, want success", code)
	}

	persisted, err := dao.NewUserCanvasDAO().GetByID(ctx, db, row.ID)
	if err != nil {
		t.Fatalf("load created agent: %v", err)
	}
	if persisted.UserID != "agent-owner" {
		t.Errorf("created agent user_id = %q, want authenticated user agent-owner", persisted.UserID)
	}
	if got := persisted.EffectiveTenantID(); got != "tenant-agent" {
		t.Errorf("created agent tenant_id = %q, want active tenant tenant-agent", got)
	}
}

func TestResourceSharingRequiresSystemRoleShareAction(t *testing.T) {
	db := setupResourcePermissionTestDB(t)
	seedResourcePermissionTenant(t, db)
	seedPermissionDataset(t, db, "dataset-ceiling", "owner")
	role := entity.Role{ID: 50, RoleName: "system-limited"}
	if err := db.Create(&role).Error; err != nil {
		t.Fatalf("create system role: %v", err)
	}
	if err := db.Create(&entity.RoleResource{
		RoleID: role.ID, ResourceType: entity.RoleResourceTypeDataset,
		Action: entity.RoleActionEnable | entity.RoleActionRead | entity.RoleActionWrite,
	}).Error; err != nil {
		t.Fatalf("create system role feature ceiling: %v", err)
	}
	if err := db.Model(&entity.User{}).Where("id = ?", "owner").Update("role_id", role.ID).Error; err != nil {
		t.Fatalf("assign limited system role: %v", err)
	}
	grant := entity.ResourcePermissionRead
	err := NewTenantService().UpdateResourcePermissions(t.Context(), "owner", "tenant-permission", ResourcePermissionUpdateRequest{
		ResourceType: "kb", ResourceIDs: []string{"dataset-ceiling"}, Permission: &grant, MemberList: []string{"reader-link"},
	})
	if err == nil {
		t.Fatal("resource share succeeded without system-level share permission")
	}
	var grantCount int64
	if err := db.Model(&entity.Permission{}).Where("resource_id = ?", "dataset-ceiling").Count(&grantCount).Error; err != nil {
		t.Fatalf("count dataset grants: %v", err)
	}
	if grantCount != 0 {
		t.Fatalf("resource grant count = %d after rejected share, want 0", grantCount)
	}
}

func TestResourcePermissionListingIgnoresFeatureRoleLimits(t *testing.T) {
	db := setupResourcePermissionTestDB(t)
	seedResourcePermissionTenant(t, db)
	seedPermissionDataset(t, db, "dataset-role-independent-list", "owner")
	role := entity.Role{ID: 51, RoleName: "system-no-dataset-access"}
	if err := db.Create(&role).Error; err != nil {
		t.Fatalf("create system role: %v", err)
	}
	if err := db.Model(&entity.User{}).Where("id = ?", "owner").Update("role_id", role.ID).Error; err != nil {
		t.Fatalf("assign restrictive system role: %v", err)
	}
	grantStatus := string(entity.StatusValid)
	resourceID := "dataset-role-independent-list"
	if err := db.Create(&entity.Permission{
		ID: "role-independent-grant", MemberID: stringPointer("reader-link"), TenantID: "tenant-permission",
		ResourceType: "kb", ResourceID: &resourceID, Permission: entity.ResourcePermissionRead, Status: &grantStatus,
	}).Error; err != nil {
		t.Fatalf("seed resource permission: %v", err)
	}

	grants, err := NewTenantService().ListResourcePermissions(t.Context(), "owner", "tenant-permission", ResourcePermissionListRequest{
		ResourceType: "kb", ResourceIDs: []string{resourceID},
	})
	if err != nil {
		t.Fatalf("list resource permissions with restrictive feature role: %v", err)
	}
	if len(grants) != 1 || grants[0].ID != "reader-link" || grants[0].Permissions[resourceID] != entity.ResourcePermissionRead {
		t.Fatalf("resource grants = %#v, want the direct reader grant independent of feature role", grants)
	}
}

func TestFileResourcePermissionsUseTheUnifiedAPI(t *testing.T) {
	db := setupResourcePermissionTestDB(t)
	seedResourcePermissionTenant(t, db)
	if err := db.Create(&entity.File{
		ID: "file-1", ParentID: "folder-1", TenantID: "tenant-permission", CreatedBy: "owner", Name: "report.pdf", Type: "file",
	}).Error; err != nil {
		t.Fatalf("seed file: %v", err)
	}
	grant := entity.ResourcePermissionRead
	if err := NewTenantService().UpdateResourcePermissions(t.Context(), "owner", "tenant-permission", ResourcePermissionUpdateRequest{
		ResourceType: "file", ResourceIDs: []string{"file-1"}, Permission: &grant, MemberList: []string{"reader-link"},
	}); err != nil {
		t.Fatalf("grant file resource permission: %v", err)
	}
	var stored entity.Permission
	if err := db.Where("resource_type = ? AND resource_id = ? AND member_id = ? AND status = ?",
		"file", "file-1", "reader-link", string(entity.StatusValid)).Take(&stored).Error; err != nil {
		t.Fatalf("load file permission grant: %v", err)
	}
	if stored.Permission != grant {
		t.Fatalf("stored file permission = %d, want %d", stored.Permission, grant)
	}
	if err := db.Create(&entity.File{
		ID: "folder-2", ParentID: "folder-1", TenantID: "tenant-permission", CreatedBy: "owner", Name: "Reports", Type: "folder",
	}).Error; err != nil {
		t.Fatalf("seed child folder: %v", err)
	}
	if err := NewTenantService().UpdateResourcePermissions(t.Context(), "owner", "tenant-permission", ResourcePermissionUpdateRequest{
		ResourceType: "Folder", ResourceIDs: []string{"folder-2"}, Permission: &grant, MemberList: []string{"reader-link"},
	}); err != nil {
		t.Fatalf("grant child folder permission with mixed-case type: %v", err)
	}
	items, err := NewTenantService().ListResourcePermissionsByTarget(t.Context(), "owner", "tenant-permission", ResourcePermissionTargetListRequest{
		TargetID: "reader-link", TargetType: "member",
	})
	if err != nil {
		t.Fatalf("list file permissions by target: %v", err)
	}
	if len(items) != 2 || items[0].ResourceType != "file" || items[0].ModuleType != "File" || items[1].ResourceType != "file" {
		t.Fatalf("file permissions by target = %#v, want the file and folder resources", items)
	}
}

func seedResourcePermissionTenant(t *testing.T, db *gorm.DB) {
	t.Helper()
	seedTenantRoleTenant(t, db, "tenant-permission")
	seedResourcePermissionMember(t, db, "owner-link", "owner", TenantRoleOwner)
	seedResourcePermissionMember(t, db, "reader-link", "reader", "10")
	if err := db.Create(&entity.Role{ID: 10, RoleName: "member", TenantID: stringPointer("tenant-permission")}).Error; err != nil {
		t.Fatalf("seed feature role: %v", err)
	}
	if err := db.Create(&entity.RoleResource{RoleID: 10, ResourceType: tenantRoleResourceTypes["dataset"], Action: entity.RoleActionEnable | entity.RoleActionRead | entity.RoleActionShare}).Error; err != nil {
		t.Fatalf("seed feature role permissions: %v", err)
	}
}

func seedResourcePermissionMember(t *testing.T, db *gorm.DB, memberID, userID, role string) {
	t.Helper()
	superuser := false
	active := string(entity.StatusValid)
	if err := db.Create(&entity.User{
		ID: userID, Nickname: userID, Email: userID + "@example.test", IsSuperuser: &superuser,
		IsAuthenticated: "1", IsActive: "1", IsAnonymous: "0", Status: &active,
	}).Error; err != nil {
		t.Fatalf("seed user %q: %v", userID, err)
	}
	seedTenantRoleMember(t, db, memberID, userID, "tenant-permission", role)
}

func seedPermissionDataset(t *testing.T, db *gorm.DB, id, createdBy string) {
	t.Helper()
	active := string(entity.StatusValid)
	if err := db.Create(&entity.Knowledgebase{
		ID: id, TenantID: "tenant-permission", Name: "Dataset " + id, EmbdID: "embedding",
		CreatedBy: createdBy, ParserID: "general", ParserConfig: entity.JSONMap{},
		Permission: string(entity.TenantPermissionMe), Status: &active,
	}).Error; err != nil {
		t.Fatalf("seed dataset %q: %v", id, err)
	}
}

func TestUpdateAndListResourcePermissions(t *testing.T) {
	db := setupResourcePermissionTestDB(t)
	seedResourcePermissionTenant(t, db)
	seedPermissionDataset(t, db, "dataset-1", "owner")
	groupName := "Reviewers"
	active := string(entity.StatusValid)
	if err := db.Create(&entity.GroupInfo{
		ID: "group-1", Name: &groupName, OwnerID: "owner-link", TenantID: "tenant-permission", Status: &active,
	}).Error; err != nil {
		t.Fatalf("seed group: %v", err)
	}

	permission := entity.ResourcePermissionRead | entity.ResourcePermissionManage
	request := ResourcePermissionUpdateRequest{
		TenantID: "tenant-permission", ResourceType: "kb", ResourceIDs: []string{"dataset-1"},
		Permission: &permission, MemberList: []string{"reader-link"}, GroupList: []string{"group-1"},
	}
	if err := NewTenantService().UpdateResourcePermissions(t.Context(), "owner", "tenant-permission", request); err != nil {
		t.Fatalf("update resource permissions: %v", err)
	}

	var grants []entity.Permission
	if err := db.Where("tenant_id = ? AND resource_type = ? AND resource_id = ? AND status = ?", "tenant-permission", "kb", "dataset-1", string(entity.StatusValid)).Find(&grants).Error; err != nil {
		t.Fatalf("load resource grants: %v", err)
	}
	if len(grants) != 2 {
		t.Fatalf("active grant count = %d, want 2", len(grants))
	}
	for _, grant := range grants {
		if grant.Permission != permission {
			t.Fatalf("stored permission = %d, want mask %d", grant.Permission, permission)
		}
	}

	listed, err := NewTenantService().ListResourcePermissions(t.Context(), "owner", "tenant-permission", ResourcePermissionListRequest{
		TenantID: "tenant-permission", ResourceType: "kb", ResourceIDs: []string{"dataset-1"},
	})
	if err != nil {
		t.Fatalf("list resource permissions: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("listed target count = %d, want 2", len(listed))
	}

	byTarget, err := NewTenantService().ListResourcePermissionsByTarget(t.Context(), "owner", "tenant-permission", ResourcePermissionTargetListRequest{
		TenantID: "tenant-permission", TargetID: "reader-link", TargetType: "member",
	})
	if err != nil {
		t.Fatalf("list permissions by member: %v", err)
	}
	if len(byTarget) != 1 || byTarget[0].ResourceID != "dataset-1" || byTarget[0].Permission != permission {
		t.Fatalf("permissions by member = %#v, want the dataset grant", byTarget)
	}

	permission = 0
	request.Permission = &permission
	request.GroupList = nil
	request.MemberList = []string{"reader-link"}
	if err := NewTenantService().UpdateResourcePermissions(t.Context(), "owner", "tenant-permission", request); err != nil {
		t.Fatalf("revoke resource permission: %v", err)
	}
	var activeGrantCount int64
	if err := db.Model(&entity.Permission{}).Where("tenant_id = ? AND resource_id = ? AND member_id = ? AND status = ?",
		"tenant-permission", "dataset-1", "reader-link", string(entity.StatusValid)).Count(&activeGrantCount).Error; err != nil {
		t.Fatalf("count remaining member grants: %v", err)
	}
	if activeGrantCount != 0 {
		t.Fatalf("active member grant count = %d, want 0", activeGrantCount)
	}
	var logCount int64
	if err := db.Model(&entity.PermissionChangeLog{}).Where("tenant_id = ? AND resource_id = ?", "tenant-permission", "dataset-1").Count(&logCount).Error; err != nil {
		t.Fatalf("count permission audit records: %v", err)
	}
	if logCount != 3 {
		t.Fatalf("permission audit count = %d, want 3", logCount)
	}
	var changeLog entity.PermissionChangeLog
	if err := db.Where("event_type = ? AND resource_id = ?", resourcePermissionEventGranted, "dataset-1").Take(&changeLog).Error; err != nil {
		t.Fatalf("load resource permission audit record: %v", err)
	}
	if changeLog.OperatorID != "owner-link" || changeLog.ActorUserID == nil || *changeLog.ActorUserID != "owner" {
		t.Fatalf("resource permission actor = (%q, %v), want membership ID and user ID", changeLog.OperatorID, changeLog.ActorUserID)
	}
}

func TestModelProviderPermissionExpandsToExistingInstances(t *testing.T) {
	db := setupResourcePermissionTestDB(t)
	seedResourcePermissionTenant(t, db)
	if err := db.Create(&entity.TenantModelProvider{
		ID: "provider-1", UserID: "owner", ProviderName: "DeepSeek", TenantID: "tenant-permission",
	}).Error; err != nil {
		t.Fatalf("seed model provider: %v", err)
	}
	if err := db.Create(&[]entity.TenantModelInstance{
		{ID: "instance-1", InstanceName: "default", ProviderID: "provider-1", APIKey: "secret-1", Status: "active", Extra: "{}"},
		{ID: "instance-2", InstanceName: "secondary", ProviderID: "provider-1", APIKey: "secret-2", Status: "active", Extra: "{}"},
		{ID: "instance-deleted", InstanceName: "deleted", ProviderID: "provider-1", APIKey: "secret-3", Status: "inactive", Extra: "{}"},
	}).Error; err != nil {
		t.Fatalf("seed model instances: %v", err)
	}
	permission := entity.ResourcePermissionRead | entity.ResourcePermissionWrite
	if err := NewTenantService().UpdateResourcePermissions(t.Context(), "owner", "tenant-permission", ResourcePermissionUpdateRequest{
		ResourceType: "llm", ResourceIDs: []string{"DeepSeek"}, Permission: &permission, MemberList: []string{"reader-link"},
	}); err != nil {
		t.Fatalf("grant provider permissions: %v", err)
	}
	var grants []entity.Permission
	if err := db.Where("tenant_id = ? AND member_id = ? AND status = ?", "tenant-permission", "reader-link", string(entity.StatusValid)).Find(&grants).Error; err != nil {
		t.Fatalf("load instance grants: %v", err)
	}
	if len(grants) != 2 {
		t.Fatalf("active instance grant count = %d, want only the two existing active instances", len(grants))
	}
	for _, grant := range grants {
		if grant.ResourceType != "model_instance" || grant.ResourceID == nil || *grant.ResourceID == "instance-deleted" {
			t.Fatalf("provider grant was not expanded to an active model instance: %#v", grant)
		}
	}
}

func TestResourceOwnershipTransferKeepsOneOwnerAndAuditsIt(t *testing.T) {
	db := setupResourcePermissionTestDB(t)
	seedResourcePermissionTenant(t, db)
	seedPermissionDataset(t, db, "dataset-owner", "owner")
	if err := NewTenantService().TransferResourceOwnership(t.Context(), "owner", "tenant-permission", ResourceOwnershipTransferRequest{
		ResourceType: "kb", ResourceID: "dataset-owner", UserID: "reader",
	}); err != nil {
		t.Fatalf("transfer resource ownership: %v", err)
	}
	var grants []entity.Permission
	if err := db.Where("tenant_id = ? AND resource_type = ? AND resource_id = ? AND status = ?", "tenant-permission", "kb", "dataset-owner", string(entity.StatusValid)).Find(&grants).Error; err != nil {
		t.Fatalf("load ownership grants: %v", err)
	}
	owners := 0
	for _, grant := range grants {
		if grant.Permission&entity.ResourcePermissionOwner != 0 {
			owners++
			if grant.MemberID == nil || *grant.MemberID != "reader-link" || grant.Permission != resourcePermissionMask {
				t.Fatalf("new owner grant = %#v", grant)
			}
		}
	}
	if owners != 1 {
		t.Fatalf("active owner grant count = %d, want 1", owners)
	}
	var ownershipLogCount int64
	if err := db.Model(&entity.PermissionChangeLog{}).Where("tenant_id = ? AND resource_id = ? AND event_type = ?",
		"tenant-permission", "dataset-owner", resourceOwnershipEventTransfer).Count(&ownershipLogCount).Error; err != nil {
		t.Fatalf("count ownership audit events: %v", err)
	}
	if ownershipLogCount != 2 {
		t.Fatalf("ownership transfer audit event count = %d, want previous and new owner records", ownershipLogCount)
	}
}

func TestResourcePermissionUpdateRejectsChangingActorAndOwner(t *testing.T) {
	db := setupResourcePermissionTestDB(t)
	seedResourcePermissionTenant(t, db)
	seedPermissionDataset(t, db, "dataset-self", "owner")
	active := string(entity.StatusValid)
	ownerMembershipID := "owner-link"
	if err := db.Create(&entity.Permission{
		ID: "owner-grant", MemberID: &ownerMembershipID, TenantID: "tenant-permission",
		ResourceType: "kb", ResourceID: stringPointer("dataset-self"),
		Permission: resourcePermissionMask, Status: &active,
	}).Error; err != nil {
		t.Fatalf("seed owner permission: %v", err)
	}
	readerMembershipID := "reader-link"
	if err := db.Create(&entity.Permission{
		ID: "other-owner-grant", MemberID: &readerMembershipID, TenantID: "tenant-permission",
		ResourceType: "kb", ResourceID: stringPointer("dataset-self"),
		Permission: resourcePermissionMask, Status: &active,
	}).Error; err != nil {
		t.Fatalf("seed second owner permission: %v", err)
	}
	var seededOwnerGrants []entity.Permission
	if err := db.Where("resource_id = ?", "dataset-self").Find(&seededOwnerGrants).Error; err != nil {
		t.Fatalf("load seeded owner permissions: %v", err)
	}
	if len(seededOwnerGrants) != 2 {
		t.Fatalf("seeded owner permission count = %d, want 2", len(seededOwnerGrants))
	}
	read := entity.ResourcePermissionRead
	service := NewTenantService()

	err := service.UpdateResourcePermissions(t.Context(), "owner", "tenant-permission", ResourcePermissionUpdateRequest{
		ResourceType: "kb", ResourceIDs: []string{"dataset-self"}, Permission: &read, MemberList: []string{"owner-link"},
	})
	if err == nil {
		t.Fatal("UpdateResourcePermissions allowed changing the actor's own permission")
	}
	if err := service.UpdateResourcePermissions(t.Context(), "owner", "tenant-permission", ResourcePermissionUpdateRequest{
		ResourceType: "kb", ResourceIDs: []string{"dataset-self"}, Permission: &read, MemberList: []string{"reader-link"},
	}); err == nil {
		t.Fatal("UpdateResourcePermissions allowed changing an owner grant through the generic API")
	}
	var afterRejectedUpdate int64
	if err := db.Model(&entity.Permission{}).Where("id = ?", "other-owner-grant").Count(&afterRejectedUpdate).Error; err != nil || afterRejectedUpdate != 1 {
		t.Fatalf("second owner permission after generic update = %d, error %v, want one row", afterRejectedUpdate, err)
	}
	owner := entity.ResourcePermissionOwner
	if err := service.UpdateResourcePermissions(t.Context(), "owner", "tenant-permission", ResourcePermissionUpdateRequest{
		ResourceType: "kb", ResourceIDs: []string{"dataset-self"}, Permission: &owner, MemberList: []string{"reader-link"},
	}); err == nil {
		t.Fatal("UpdateResourcePermissions allowed ownership transfer through the generic API")
	}
	var stored entity.Permission
	if err := db.Where("id = ?", "owner-grant").Take(&stored).Error; err != nil {
		t.Fatalf("load owner permission after rejected changes: %v", err)
	}
	if stored.Permission != resourcePermissionMask || stored.Status == nil || *stored.Status != active {
		t.Fatalf("owner permission after rejected changes = %+v, want unchanged active owner", stored)
	}
}

func TestResourcePermissionUpdateAllowsGroupContainingActor(t *testing.T) {
	db := setupResourcePermissionTestDB(t)
	seedResourcePermissionTenant(t, db)
	seedPermissionDataset(t, db, "dataset-group-self", "owner")
	groupName := "Owners"
	active := string(entity.StatusValid)
	joined := entity.GroupMemberStatusJoined
	if err := db.Create(&entity.GroupInfo{
		ID: "owners-group", Name: &groupName, OwnerID: "owner-link", TenantID: "tenant-permission", Status: &active,
	}).Error; err != nil {
		t.Fatalf("seed group: %v", err)
	}
	if err := db.Create(&entity.GroupMember{
		ID: "owner-group-member", MemberID: "owner-link", GroupID: "owners-group", Status: &joined,
	}).Error; err != nil {
		t.Fatalf("seed group membership: %v", err)
	}
	read := entity.ResourcePermissionRead
	err := NewTenantService().UpdateResourcePermissions(t.Context(), "owner", "tenant-permission", ResourcePermissionUpdateRequest{
		ResourceType: "kb", ResourceIDs: []string{"dataset-group-self"}, Permission: &read, GroupList: []string{"owners-group"},
	})
	if err != nil {
		t.Fatalf("UpdateResourcePermissions for a group containing the actor: %v", err)
	}
	var grant entity.Permission
	if err := db.Where("resource_id = ? AND group_id = ?", "dataset-group-self", "owners-group").Take(&grant).Error; err != nil {
		t.Fatalf("load group grant: %v", err)
	}
	if grant.Permission != read {
		t.Fatalf("group permission = %d, want %d", grant.Permission, read)
	}
}

func TestResourcePermissionUpdateAllowsGroupOwnedByActor(t *testing.T) {
	db := setupResourcePermissionTestDB(t)
	seedResourcePermissionTenant(t, db)
	seedPermissionDataset(t, db, "dataset-group-owner-self", "owner")
	groupName := "Owned group"
	active := string(entity.StatusValid)
	if err := db.Create(&entity.GroupInfo{
		ID: "owned-group", Name: &groupName, OwnerID: "owner-link", TenantID: "tenant-permission", Status: &active,
	}).Error; err != nil {
		t.Fatalf("seed group: %v", err)
	}
	read := entity.ResourcePermissionRead
	err := NewTenantService().UpdateResourcePermissions(t.Context(), "owner", "tenant-permission", ResourcePermissionUpdateRequest{
		ResourceType: "kb", ResourceIDs: []string{"dataset-group-owner-self"}, Permission: &read, GroupList: []string{"owned-group"},
	})
	if err != nil {
		t.Fatalf("UpdateResourcePermissions for a group owned by the actor: %v", err)
	}
	var grant entity.Permission
	if err := db.Where("resource_id = ? AND group_id = ?", "dataset-group-owner-self", "owned-group").Take(&grant).Error; err != nil {
		t.Fatalf("load group grant: %v", err)
	}
	if grant.Permission != read {
		t.Fatalf("group permission = %d, want %d", grant.Permission, read)
	}
}

func TestResourceOwnershipTransferRejectsSelf(t *testing.T) {
	db := setupResourcePermissionTestDB(t)
	seedResourcePermissionTenant(t, db)
	seedPermissionDataset(t, db, "dataset-self-transfer", "owner")
	err := NewTenantService().TransferResourceOwnership(t.Context(), "owner", "tenant-permission", ResourceOwnershipTransferRequest{
		ResourceType: "kb", ResourceID: "dataset-self-transfer", UserID: "owner",
	})
	if err == nil {
		t.Fatal("TransferResourceOwnership allowed transferring ownership to the actor")
	}
	var count int64
	if err := db.Model(&entity.Permission{}).Where("resource_id = ? AND (permission & ?) <> 0", "dataset-self-transfer", entity.ResourcePermissionOwner).Count(&count).Error; err != nil {
		t.Fatalf("count owner grants: %v", err)
	}
	if count != 0 {
		t.Fatalf("owner grant count after rejected self-transfer = %d, want 0", count)
	}
}

func TestResourcePermissionRejectsTargetOutsideTenant(t *testing.T) {
	db := setupResourcePermissionTestDB(t)
	seedResourcePermissionTenant(t, db)
	seedPermissionDataset(t, db, "dataset-1", "owner")
	permission := entity.ResourcePermissionRead
	err := NewTenantService().UpdateResourcePermissions(t.Context(), "owner", "tenant-permission", ResourcePermissionUpdateRequest{
		ResourceType: "kb", ResourceIDs: []string{"dataset-1"}, Permission: &permission, MemberList: []string{"missing-member"},
	})
	if err == nil {
		t.Fatal("UpdateResourcePermissions succeeded for a target outside the Tenant")
	}
	var count int64
	if err := db.Model(&entity.Permission{}).Where("resource_id = ?", "dataset-1").Count(&count).Error; err != nil {
		t.Fatalf("count resource grants: %v", err)
	}
	if count != 0 {
		t.Fatalf("resource grant count = %d after rejected update, want 0", count)
	}
}

func TestListResourcePermissionsByTargetIncludesGroupAndTenantResources(t *testing.T) {
	db := setupResourcePermissionTestDB(t)
	seedResourcePermissionTenant(t, db)
	groupName := "Reviewers"
	groupAvatar := "/avatars/reviewers.png"
	active := string(entity.StatusValid)
	if err := db.Create(&entity.GroupInfo{
		ID: "group-special", Name: &groupName, Avatar: &groupAvatar, OwnerID: "owner-link",
		TenantID: "tenant-permission", Status: &active,
	}).Error; err != nil {
		t.Fatalf("seed group: %v", err)
	}
	if err := db.Model(&entity.Tenant{}).Where("id = ?", "tenant-permission").Update("name", "Review Tenant").Error; err != nil {
		t.Fatalf("name Tenant: %v", err)
	}
	groupID, tenantID, memberID := "group-special", "tenant-permission", "reader-link"
	if err := db.Create(&[]entity.Permission{
		{
			ID: "group-management-grant", MemberID: &memberID, TenantID: tenantID,
			ResourceType: "group", ResourceID: &groupID, Permission: entity.GroupPermissionAddMember, Status: &active,
		},
		{
			ID: "tenant-management-grant", MemberID: &memberID, TenantID: tenantID,
			ResourceType: "tenant", ResourceID: &tenantID, Permission: entity.TenantPermissionManageRoles, Status: &active,
		},
	}).Error; err != nil {
		t.Fatalf("seed special-resource grants: %v", err)
	}

	items, err := NewTenantService().ListResourcePermissionsByTarget(t.Context(), "owner", tenantID, ResourcePermissionTargetListRequest{
		TargetID: memberID, TargetType: "member",
	})
	if err != nil {
		t.Fatalf("list special-resource permissions by target: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("special-resource permissions = %#v, want group and Tenant grants", items)
	}
	got := make(map[string]ResourcePermissionItem, len(items))
	for _, item := range items {
		got[item.ResourceType] = *item
	}
	if item, ok := got["group"]; !ok || item.ResourceID != groupID || item.Name != groupName || item.Avatar != groupAvatar || item.ModuleType != "Group" || item.Permission != entity.GroupPermissionAddMember || item.Editable || len(item.PermissionNames) != 1 || item.PermissionNames[0] != "add_member" {
		t.Fatalf("group permission view = %#v, want resolved group resource and its permission mask", item)
	}
	if item, ok := got["tenant"]; !ok || item.ResourceID != tenantID || item.Name != "Review Tenant" || item.ModuleType != "Tenant" || item.Permission != entity.TenantPermissionManageRoles || item.Editable || len(item.PermissionNames) != 1 || item.PermissionNames[0] != "manage_roles" {
		t.Fatalf("Tenant permission view = %#v, want resolved Tenant resource and its permission mask", item)
	}
}
