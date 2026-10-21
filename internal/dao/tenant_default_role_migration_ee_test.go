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

package dao

import (
	"fmt"
	"testing"

	"ragflow/internal/entity"
)

func TestMigrateTenantDefaultRolesEE(t *testing.T) {
	db := openGroupMigrationDB(t)
	if err := db.AutoMigrate(
		&entity.Tenant{},
		&entity.UserTenant{},
		&entity.User{},
		&entity.SystemSettings{},
		&entity.Role{},
		&entity.RoleResource{},
		&entity.GroupInfo{},
		&entity.GroupMember{},
		&entity.Permission{},
	); err != nil {
		t.Fatalf("auto-migrate Tenant role tables: %v", err)
	}

	status := string(entity.StatusValid)
	inactive := string(entity.StatusInvalid)
	if err := db.Create(&[]entity.Tenant{
		{ID: "tenant-1", LLMID: "", EmbdID: "", ASRID: "", Img2TxtID: "", RerankID: "", ParserIDs: "", Status: &status},
		{ID: "tenant-2", LLMID: "", EmbdID: "", ASRID: "", Img2TxtID: "", RerankID: "", ParserIDs: "", Status: &status},
	}).Error; err != nil {
		t.Fatalf("create Tenants: %v", err)
	}

	systemRole := entity.Role{RoleName: defaultTenantMemberRoleName}
	if err := db.Create(&systemRole).Error; err != nil {
		t.Fatalf("create same-name system role: %v", err)
	}
	existingTenantRole := entity.Role{RoleName: defaultTenantMemberRoleName, TenantID: stringPointer("tenant-2")}
	if err := db.Create(&existingTenantRole).Error; err != nil {
		t.Fatalf("create existing Tenant member role: %v", err)
	}
	if err := db.Create(&entity.RoleResource{RoleID: existingTenantRole.ID, ResourceType: 1, Action: 1}).Error; err != nil {
		t.Fatalf("create existing member role permission: %v", err)
	}
	customTenantID := "tenant-1"
	customRole := entity.Role{RoleName: "Custom", TenantID: &customTenantID}
	if err := db.Create(&customRole).Error; err != nil {
		t.Fatalf("create custom Tenant role: %v", err)
	}
	if err := db.Create(&[]entity.UserTenant{
		{ID: "owner-1", UserID: "user-owner-1", TenantID: "tenant-1", Role: "owner", Status: &status},
		{ID: "normal-1", UserID: "user-normal-1", TenantID: "tenant-1", Role: "normal", Status: &status},
		{ID: "admin-1", UserID: "user-admin-1", TenantID: "tenant-1", Role: "admin", Status: &status},
		{ID: "invite-1", UserID: "user-invite-1", TenantID: "tenant-1", Role: "invite", Status: &status},
		{ID: "inactive-1", UserID: "user-inactive-1", TenantID: "tenant-1", Role: "normal", Status: &inactive},
		{ID: "custom-member-1", UserID: "user-custom-1", TenantID: "tenant-1", Role: fmt.Sprint(customRole.ID), Status: &status},
		{ID: "normal-2", UserID: "user-normal-2", TenantID: "tenant-2", Role: "normal", Status: &status},
	}).Error; err != nil {
		t.Fatalf("create Tenant memberships: %v", err)
	}

	if err := migratePermissionDataEE(t.Context(), db); err != nil {
		t.Fatalf("migrate enterprise permission data: %v", err)
	}

	defaultRoleIDs := make(map[string]int32)
	for _, tenantID := range []string{"tenant-1", "tenant-2"} {
		var tenant entity.Tenant
		if err := db.Where("id = ?", tenantID).Take(&tenant).Error; err != nil {
			t.Fatalf("load Tenant %s: %v", tenantID, err)
		}
		if !tenant.InviteEnabled {
			t.Fatalf("legacy Tenant %s invitations are disabled after migration, want enabled", tenantID)
		}
		if tenant.RoleID == nil {
			t.Fatalf("Tenant %s has no default role", tenantID)
		}
		defaultRoleIDs[tenantID] = *tenant.RoleID

		var role entity.Role
		if err := db.Where("id = ? AND tenant_id = ?", *tenant.RoleID, tenantID).Take(&role).Error; err != nil {
			t.Fatalf("load Tenant %s default member role: %v", tenantID, err)
		}
		if role.RoleName != defaultTenantMemberRoleName {
			t.Fatalf("Tenant %s default role name = %q, want %q", tenantID, role.RoleName, defaultTenantMemberRoleName)
		}

		var permissions []entity.RoleResource
		if err := db.Where("role_id = ?", role.ID).Order("resource_type ASC").Find(&permissions).Error; err != nil {
			t.Fatalf("load Tenant %s member permissions: %v", tenantID, err)
		}
		if tenantID == "tenant-2" {
			if len(permissions) != 1 || permissions[0].ResourceType != 1 || permissions[0].Action != 1 {
				t.Fatalf("existing Tenant 2 member permissions = %+v, want its original single permission", permissions)
			}
			continue
		}
		if len(permissions) != len(defaultTenantRoleResourceActions) {
			t.Fatalf("Tenant %s member permission count = %d, want %d", tenantID, len(permissions), len(defaultTenantRoleResourceActions))
		}
		for index, expected := range defaultTenantRoleResourceActions {
			if permissions[index].ResourceType != expected.resourceType || permissions[index].Action != expected.action {
				t.Fatalf("Tenant %s member permission[%d] = {resource_type:%d action:%d}, want {resource_type:%d action:%d}",
					tenantID, index, permissions[index].ResourceType, permissions[index].Action, expected.resourceType, expected.action)
			}
		}
	}
	if defaultRoleIDs["tenant-2"] != existingTenantRole.ID {
		t.Fatalf("Tenant 2 default role ID = %d, want existing member role %d", defaultRoleIDs["tenant-2"], existingTenantRole.ID)
	}

	wantMembershipRoles := map[string]string{
		"owner-1":         "owner",
		"normal-1":        fmt.Sprint(defaultRoleIDs["tenant-1"]),
		"admin-1":         fmt.Sprint(defaultRoleIDs["tenant-1"]),
		"invite-1":        "invite",
		"inactive-1":      fmt.Sprint(defaultRoleIDs["tenant-1"]),
		"custom-member-1": fmt.Sprint(customRole.ID),
		"normal-2":        fmt.Sprint(defaultRoleIDs["tenant-2"]),
	}
	var memberships []entity.UserTenant
	if err := db.Order("id ASC").Find(&memberships).Error; err != nil {
		t.Fatalf("load migrated memberships: %v", err)
	}
	for _, membership := range memberships {
		if want := wantMembershipRoles[membership.ID]; membership.Role != want {
			t.Errorf("membership %s role = %q, want %q", membership.ID, membership.Role, want)
		}
	}
	var adminGrant entity.Permission
	if err := db.Where("tenant_id = ? AND resource_type = ? AND resource_id = ? AND member_id = ? AND status = ?",
		"tenant-1", "tenant", "tenant-1", "admin-1", string(entity.StatusValid)).Take(&adminGrant).Error; err != nil {
		t.Fatalf("load migrated Tenant admin permissions: %v", err)
	}
	wantAdminPermission := entity.TenantPermissionManageMembers | entity.TenantPermissionManageRoles
	if adminGrant.Permission != wantAdminPermission {
		t.Fatalf("migrated Tenant admin permission mask = %d, want %d", adminGrant.Permission, wantAdminPermission)
	}

	var unchangedSystemRole entity.Role
	if err := db.Where("id = ?", systemRole.ID).Take(&unchangedSystemRole).Error; err != nil {
		t.Fatalf("load same-name system role: %v", err)
	}
	if unchangedSystemRole.TenantID != nil {
		t.Fatalf("same-name system role unexpectedly assigned to Tenant %q", *unchangedSystemRole.TenantID)
	}

	if err := migratePermissionDataEE(t.Context(), db); err != nil {
		t.Fatalf("repeat enterprise permission migration: %v", err)
	}
	var tenant entity.Tenant
	if err := db.Where("id = ?", "tenant-1").Take(&tenant).Error; err != nil {
		t.Fatalf("reload Tenant after repeated migration: %v", err)
	}
	if tenant.RoleID == nil || *tenant.RoleID != defaultRoleIDs["tenant-1"] {
		t.Fatalf("repeated migration default role_id = %v, want %d", tenant.RoleID, defaultRoleIDs["tenant-1"])
	}
	var membership entity.UserTenant
	if err := db.Where("id = ?", "normal-1").Take(&membership).Error; err != nil {
		t.Fatalf("reload membership after repeated migration: %v", err)
	}
	if membership.Role != fmt.Sprint(defaultRoleIDs["tenant-1"]) {
		t.Fatalf("repeated migration membership role = %q, want %d", membership.Role, defaultRoleIDs["tenant-1"])
	}

	if err := db.Model(&entity.Tenant{}).Where("id = ?", "tenant-1").Update("invite_enabled", false).Error; err != nil {
		t.Fatalf("disable invitations after initial migration: %v", err)
	}
	customAction := int32(3)
	if err := db.Model(&entity.RoleResource{}).
		Where("role_id = ? AND resource_type = ?", defaultRoleIDs["tenant-2"], 1).
		Update("action", customAction).Error; err != nil {
		t.Fatalf("customize existing member role permission: %v", err)
	}
	if err := setDatabaseMigrationVersionForMarker(t.Context(), db, permissionMigrationEEMarker, "v1.0.0-rc3.dev2"); err != nil {
		t.Fatalf("reset enterprise permission migration marker: %v", err)
	}
	if err := migratePermissionDataEE(t.Context(), db); err != nil {
		t.Fatalf("rerun enterprise permission migration after version bump: %v", err)
	}
	if err := db.Where("id = ?", "tenant-1").Take(&tenant).Error; err != nil {
		t.Fatalf("reload Tenant after version bump: %v", err)
	}
	if tenant.InviteEnabled {
		t.Fatal("later permission migration re-enabled invitations after an administrator disabled them")
	}
	var preservedPermission entity.RoleResource
	if err := db.Where("role_id = ? AND resource_type = ?", defaultRoleIDs["tenant-2"], 1).
		Take(&preservedPermission).Error; err != nil {
		t.Fatalf("load existing member role permission after version bump: %v", err)
	}
	if preservedPermission.Action != customAction {
		t.Fatalf("existing member role action after version bump = %d, want custom action %d", preservedPermission.Action, customAction)
	}
}

func TestTenantDAOCreateInitializesDefaultRoleEE(t *testing.T) {
	db := openGroupMigrationDB(t)
	if err := db.AutoMigrate(&entity.Tenant{}, &entity.Role{}, &entity.RoleResource{}); err != nil {
		t.Fatalf("auto-migrate Tenant role tables: %v", err)
	}

	status := string(entity.StatusValid)
	tenant := &entity.Tenant{
		ID: "tenant-new", LLMID: "", EmbdID: "", ASRID: "", Img2TxtID: "", RerankID: "", ParserIDs: "", Status: &status,
	}
	if err := NewTenantDAO().Create(t.Context(), db, tenant); err != nil {
		t.Fatalf("create Tenant: %v", err)
	}
	if tenant.RoleID == nil {
		t.Fatal("new Tenant has no default role ID")
	}

	var role entity.Role
	if err := db.Where("id = ? AND tenant_id = ?", *tenant.RoleID, tenant.ID).Take(&role).Error; err != nil {
		t.Fatalf("load new Tenant default role: %v", err)
	}
	if role.RoleName != defaultTenantMemberRoleName {
		t.Fatalf("new Tenant default role name = %q, want %q", role.RoleName, defaultTenantMemberRoleName)
	}

	var permissions []entity.RoleResource
	if err := db.Where("role_id = ?", role.ID).Find(&permissions).Error; err != nil {
		t.Fatalf("load new Tenant default role permissions: %v", err)
	}
	if len(permissions) != len(defaultTenantRoleResourceActions) {
		t.Fatalf("new Tenant default permission count = %d, want %d", len(permissions), len(defaultTenantRoleResourceActions))
	}
}

func stringPointer(value string) *string { return &value }
