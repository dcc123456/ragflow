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

package service

import (
	"errors"
	"strconv"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"
	"ragflow/internal/permission"

	"gorm.io/gorm"
)

func setupTenantRoleTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.SetupTestDB(t,
		&entity.Tenant{},
		&entity.UserTenant{},
		&entity.Role{},
		&entity.RoleResource{},
		&entity.RoleDefaultModel{},
		&entity.Permission{},
		&entity.GroupInfo{},
		&entity.GroupMember{},
		&entity.PermissionChangeLog{},
	)
	t.Cleanup(testutil.ReplaceDBForTest(t, db))
	return db
}

func seedTenantRoleTenant(t *testing.T, db *gorm.DB, tenantID string) {
	t.Helper()
	status := string(entity.StatusValid)
	if err := db.Create(&entity.Tenant{
		ID: tenantID, ParserIDs: "", Status: &status,
	}).Error; err != nil {
		t.Fatalf("seed Tenant %q: %v", tenantID, err)
	}
}

func seedTenantRoleMember(t *testing.T, db *gorm.DB, id, userID, tenantID, role string) {
	t.Helper()
	status := string(entity.StatusValid)
	if err := db.Create(&entity.UserTenant{
		ID: id, UserID: userID, TenantID: tenantID, Role: role, Status: &status,
	}).Error; err != nil {
		t.Fatalf("seed Tenant membership %q: %v", id, err)
	}
}

func TestTenantRoleManagementUsesTenantPermissionAndStoresMembershipRoleID(t *testing.T) {
	db := setupTenantRoleTestDB(t)
	seedTenantRoleTenant(t, db, "tenant-1")
	seedTenantRoleTenant(t, db, "tenant-2")
	seedTenantRoleMember(t, db, "owner-link", "owner-user", "tenant-1", "owner")
	seedTenantRoleMember(t, db, "manager-link", "manager-user", "tenant-1", "normal")
	seedTenantRoleMember(t, db, "member-link", "member-user", "tenant-1", "normal")
	manager := NewTenantService()
	grant, err := manager.SetTenantManagementPermissions(t.Context(), "owner-user", "tenant-1", "manager-user", "", []string{"manage_roles"})
	if err != nil {
		t.Fatalf("SetTenantManagementPermissions() for user error = %v", err)
	}
	if grant.SubjectType != "user" || grant.UserID != "manager-user" || len(grant.Permissions) != 1 || grant.Permissions[0] != "manage_roles" {
		t.Fatalf("Tenant management grant = %#v", grant)
	}
	grants, err := manager.ListTenantManagementPermissions(t.Context(), "manager-user", "tenant-1")
	if err != nil || len(grants.Grants) != 1 || grants.Grants[0].UserID != "manager-user" {
		t.Fatalf("ListTenantManagementPermissions() = (%#v, %v), want one user grant", grants, err)
	}

	role, err := manager.CreateTenantRole(t.Context(), "manager-user", "tenant-1", "Editor", "Can edit datasets")
	if err != nil {
		t.Fatalf("CreateTenantRole() error = %v", err)
	}
	if role.ID == 0 || role.RoleName != "Editor" {
		t.Fatalf("created role = %#v", role)
	}
	var roleCreatedLog entity.PermissionChangeLog
	if err := db.Where("event_type = ? AND target_id = ?", "role.created", strconv.FormatInt(int64(role.ID), 10)).
		Take(&roleCreatedLog).Error; err != nil {
		t.Fatalf("load role creation audit log: %v", err)
	}
	if roleCreatedLog.OperatorID != "manager-link" || roleCreatedLog.ActorUserID == nil || *roleCreatedLog.ActorUserID != "manager-user" {
		t.Fatalf("role creation operator = (%q, %v), want membership ID and user ID", roleCreatedLog.OperatorID, roleCreatedLog.ActorUserID)
	}

	actions := map[string]map[string]bool{
		"dataset": {"enable": true, "read": true, "write": true},
	}
	permissions, err := manager.SetTenantRolePermissions(t.Context(), "manager-user", "tenant-1", role.ID, actions)
	if err != nil {
		t.Fatalf("SetTenantRolePermissions() error = %v", err)
	}
	if !permissions.Permissions["dataset"]["write"] || permissions.Permissions["chat"]["enable"] {
		t.Fatalf("permission matrix = %#v", permissions.Permissions)
	}

	if _, err = manager.SetTenantDefaultRole(t.Context(), "manager-user", "tenant-1", &role.ID); err != nil {
		t.Fatalf("SetTenantDefaultRole() error = %v", err)
	}
	memberRole, err := manager.SetTenantMemberRole(t.Context(), "manager-user", "tenant-1", "member-user", role.ID)
	if err != nil {
		t.Fatalf("SetTenantMemberRole() by role manager error = %v", err)
	}
	if memberRole["role_id"] != role.ID {
		t.Fatalf("member role response = %#v", memberRole)
	}

	var membership entity.UserTenant
	if err := db.Where("user_id = ? AND tenant_id = ?", "member-user", "tenant-1").Take(&membership).Error; err != nil {
		t.Fatalf("load updated membership: %v", err)
	}
	if membership.Role != strconv.FormatInt(int64(role.ID), 10) {
		t.Fatalf("user_tenant.role = %q, want role id %d", membership.Role, role.ID)
	}
	memberRole, err = manager.GetTenantMemberRole(t.Context(), "manager-user", "tenant-1", "member-user")
	if err != nil || memberRole["role_id"] != role.ID {
		t.Fatalf("GetTenantMemberRole() = (%#v, %v), want role ID %d", memberRole, err, role.ID)
	}
	if err := permission.NewDatabaseChecker(db).CheckTenant(t.Context(), permission.Subject{UserID: "member-user"}, "tenant-1", permission.TenantMember); err != nil {
		t.Fatalf("role-id membership should still count as a Tenant member: %v", err)
	}

	if _, err := manager.SetTenantManagementPermissions(t.Context(), "owner-user", "tenant-1", "manager-user", "", []string{}); err != nil {
		t.Fatalf("revoke Tenant management grant: %v", err)
	}
	grants, err = manager.ListTenantManagementPermissions(t.Context(), "owner-user", "tenant-1")
	if err != nil || len(grants.Grants) != 0 {
		t.Fatalf("ListTenantManagementPermissions() after revoke = (%#v, %v), want no grants", grants, err)
	}
	if _, err := manager.CreateTenantRole(t.Context(), "manager-user", "tenant-1", "Denied after revoke", ""); err == nil {
		t.Fatal("CreateTenantRole() succeeded after its Tenant permission was revoked")
	}

	logs, err := manager.ListTenantPermissionLogs(t.Context(), "owner-user", "tenant-1", dao.PermissionChangeLogFilter{}, 1, 20)
	if err != nil {
		t.Fatalf("ListTenantPermissionLogs() error = %v", err)
	}
	if logs.Total < 6 {
		t.Fatalf("audit log count = %d, want management grant/revoke, role, permission, default-role, and member-role changes", logs.Total)
	}

	if err := manager.DeleteTenantRole(t.Context(), "owner-user", "tenant-1", role.ID); err == nil {
		t.Fatal("DeleteTenantRole() succeeded while the role is assigned")
	} else {
		var coded *common.CodedError
		if !errors.As(err, &coded) || coded.Code != common.CodeConflict {
			t.Fatalf("DeleteTenantRole() error = %v, want conflict", err)
		}
	}
}

func TestTenantRoleAPIsRejectCrossTenantRolesAndUnauthorizedChanges(t *testing.T) {
	db := setupTenantRoleTestDB(t)
	seedTenantRoleTenant(t, db, "tenant-1")
	seedTenantRoleTenant(t, db, "tenant-2")
	seedTenantRoleMember(t, db, "owner-link", "owner-user", "tenant-1", "owner")
	seedTenantRoleMember(t, db, "member-link", "member-user", "tenant-1", "normal")
	tenant2 := "tenant-2"
	role := entity.Role{RoleName: "Foreign", TenantID: &tenant2}
	if err := db.Create(&role).Error; err != nil {
		t.Fatalf("seed foreign role: %v", err)
	}
	service := NewTenantService()

	if _, err := service.GetTenantRole(t.Context(), "owner-user", "tenant-1", role.ID); err == nil {
		t.Fatal("GetTenantRole() returned a role from another Tenant")
	}
	if _, err := service.CreateTenantRole(t.Context(), "member-user", "tenant-1", "Denied", ""); err == nil {
		t.Fatal("CreateTenantRole() succeeded without Tenant role-management permission")
	} else {
		var coded *common.CodedError
		if !errors.As(err, &coded) || coded.Code != common.CodeForbidden {
			t.Fatalf("CreateTenantRole() error = %v, want forbidden", err)
		}
	}
	if _, err := service.SetTenantDefaultRole(t.Context(), "owner-user", "tenant-1", &role.ID); err == nil {
		t.Fatal("SetTenantDefaultRole() accepted a role from another Tenant")
	}
}

func TestTenantRoleManagerPermissionCanBeGrantedThroughGroup(t *testing.T) {
	db := setupTenantRoleTestDB(t)
	seedTenantRoleTenant(t, db, "tenant-1")
	seedTenantRoleMember(t, db, "owner-link", "owner-user", "tenant-1", "owner")
	seedTenantRoleMember(t, db, "member-link", "member-user", "tenant-1", "normal")
	status := string(entity.StatusValid)
	name := "Role managers"
	group := entity.GroupInfo{ID: "group-1", Name: &name, OwnerID: "member-link", TenantID: "tenant-1", Status: &status}
	member := entity.GroupMember{ID: "group-member-1", MemberID: "member-link", GroupID: group.ID, Status: &status}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("seed group: %v", err)
	}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("seed group membership: %v", err)
	}
	if _, err := NewTenantService().SetTenantManagementPermissions(t.Context(), "owner-user", "tenant-1", "", group.ID, []string{"manage_roles"}); err != nil {
		t.Fatalf("SetTenantManagementPermissions() for group error = %v", err)
	}
	grants, err := NewTenantService().ListTenantManagementPermissions(t.Context(), "owner-user", "tenant-1")
	if err != nil || len(grants.Grants) != 1 || grants.Grants[0].GroupID != group.ID {
		t.Fatalf("ListTenantManagementPermissions() for group = (%#v, %v), want one group grant", grants, err)
	}

	if _, err := NewTenantService().CreateTenantRole(t.Context(), "member-user", "tenant-1", "Editor", ""); err != nil {
		t.Fatalf("CreateTenantRole() with group permission error = %v", err)
	}
}

//func TestAcceptTenantInvitationAssignsDefaultRoleAndAudits(t *testing.T) {
//	db := setupTenantRoleTestDB(t)
//	seedTenantRoleTenant(t, db, "tenant-1")
//	role := entity.Role{RoleName: "Member", TenantID: stringPointer("tenant-1")}
//	if err := db.Create(&role).Error; err != nil {
//		t.Fatalf("seed Tenant default role: %v", err)
//	}
//	if err := db.Model(&entity.Tenant{}).Where("id = ?", "tenant-1").Update("role_id", role.ID).Error; err != nil {
//		t.Fatalf("set Tenant default role: %v", err)
//	}
//	seedTenantRoleMember(t, db, "invite-link", "invitee-user", "tenant-1", TenantRoleInvite)
//	service := &TenantService{userTenantDAO: dao.NewUserTenantDAO()}
//	if code, err := service.AcceptInvite(t.Context(), "invitee-user", "tenant-1"); err != nil || code != common.CodeSuccess {
//		t.Fatalf("AcceptInvite() = (%v, %v), want success", code, err)
//	}
//	var membership entity.UserTenant
//	if err := db.Where("id = ?", "invite-link").Take(&membership).Error; err != nil {
//		t.Fatalf("load accepted Tenant membership: %v", err)
//	}
//	if membership.Role != strconv.FormatInt(int64(role.ID), 10) {
//		t.Fatalf("accepted membership role = %q, want role ID %d", membership.Role, role.ID)
//	}
//	var logCount int64
//	if err := db.Model(&entity.PermissionChangeLog{}).Where("event_type = ? AND target_id = ?", "tenant.member.joined", "invitee-user").Count(&logCount).Error; err != nil {
//		t.Fatalf("count invitation acceptance audit: %v", err)
//	}
//	if logCount != 1 {
//		t.Fatalf("invitation acceptance audit count = %d, want 1", logCount)
//	}
//}

func stringPointer(value string) *string { return &value }
