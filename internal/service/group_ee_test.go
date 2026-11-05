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
	"context"
	"errors"
	"fmt"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/permission"

	"gorm.io/gorm"
)

func groupTestContext(t *testing.T, userID string) context.Context {
	t.Helper()
	return permission.WithIdentity(t.Context(), userID, "tenant-1")
}

func TestCreateGroupUsesTenantRoleWriteAction(t *testing.T) {
	db := setupTenantRoleTestDB(t)
	seedTenantRoleTenant(t, db, "tenant-1")
	role := createGroupFeatureRole(t, db, "tenant-1", entity.RoleActionEnable|entity.RoleActionWrite)
	readerRole := entity.Role{RoleName: "reader", TenantID: stringPointer("tenant-1")}
	if err := db.Create(&readerRole).Error; err != nil {
		t.Fatalf("create reader role: %v", err)
	}
	if err := db.Create(&entity.RoleResource{RoleID: readerRole.ID, ResourceType: entity.RoleResourceTypeGroup, Action: entity.RoleActionEnable | entity.RoleActionRead}).Error; err != nil {
		t.Fatalf("create reader role permissions: %v", err)
	}
	seedTenantRoleMember(t, db, "writer-link", "writer", "tenant-1", fmt.Sprint(role.ID))
	seedTenantRoleMember(t, db, "reader-link", "reader", "tenant-1", fmt.Sprint(readerRole.ID))

	group, err := NewGroupService().CreateGroup(groupTestContext(t, "writer"), "writer", "tenant-1", CreateGroupRequest{Name: "Writers"})
	if err != nil {
		t.Fatalf("create group with write permission: %v", err)
	}
	if group.ParentID != nil || group.OwnerID != "writer-link" || group.Path == nil || *group.Path != "/"+group.ID+"/" {
		t.Fatalf("created root group = %+v", group)
	}
	if _, err := NewGroupService().CreateGroup(groupTestContext(t, "reader"), "reader", "tenant-1", CreateGroupRequest{Name: "Readers"}); err == nil {
		t.Fatal("CreateGroup succeeded without group.write")
	} else {
		var coded *common.CodedError
		if !errors.As(err, &coded) || coded.Code != common.CodeForbidden {
			t.Fatalf("CreateGroup without group.write error = %v, want forbidden", err)
		}
	}
}

func TestMoveGroupRewritesDescendantPaths(t *testing.T) {
	db := setupTenantRoleTestDB(t)
	seedTenantRoleTenant(t, db, "tenant-1")
	seedTenantRoleMember(t, db, "owner-link", "owner", "tenant-1", "owner")
	status := string(entity.StatusValid)
	rootAPath, rootAFormatted := "/a/", "/A/"
	childAPath, childAFormatted := "/a/c/", "/A/Child/"
	rootBPath, rootBFormatted := "/b/", "/B/"
	parentA := "group-a"
	if err := db.Create(&[]entity.GroupInfo{
		{ID: "group-a", Name: stringPointer("A"), OwnerID: "owner-link", TenantID: "tenant-1", Path: &rootAPath, FormattedPath: &rootAFormatted, Status: &status},
		{ID: "child-a", Name: stringPointer("Child"), OwnerID: "owner-link", TenantID: "tenant-1", ParentID: &parentA, Path: &childAPath, FormattedPath: &childAFormatted, Status: &status},
		{ID: "group-b", Name: stringPointer("B"), OwnerID: "owner-link", TenantID: "tenant-1", Path: &rootBPath, FormattedPath: &rootBFormatted, Status: &status},
	}).Error; err != nil {
		t.Fatalf("create groups: %v", err)
	}

	parentB := "group-b"
	if _, err := NewGroupService().MoveGroup(groupTestContext(t, "owner"), "owner", "tenant-1", "group-a", &parentB); err != nil {
		t.Fatalf("move group: %v", err)
	}
	var moved, child entity.GroupInfo
	if err := db.Where("id = ?", "group-a").Take(&moved).Error; err != nil {
		t.Fatalf("load moved group: %v", err)
	}
	if moved.ParentID == nil || *moved.ParentID != "group-b" || moved.Path == nil || *moved.Path != "/b/group-a/" || moved.FormattedPath == nil || *moved.FormattedPath != "/B/A/" {
		t.Fatalf("moved group = %+v", moved)
	}
	if err := db.Where("id = ?", "child-a").Take(&child).Error; err != nil {
		t.Fatalf("load moved descendant: %v", err)
	}
	if child.Path == nil || *child.Path != "/b/group-a/child-a/" || child.FormattedPath == nil || *child.FormattedPath != "/B/A/Child/" {
		t.Fatalf("moved descendant = %+v", child)
	}
}

func TestListGroupsShowsMembershipOrManagementAccess(t *testing.T) {
	db := setupTenantRoleTestDB(t)
	seedTenantRoleTenant(t, db, "tenant-1")
	seedTenantRoleMember(t, db, "owner-link", "owner", "tenant-1", "owner")
	memberRole := createGroupFeatureRole(t, db, "tenant-1", entity.RoleActionEnable|entity.RoleActionRead)
	seedTenantRoleMember(t, db, "member-link", "member", "tenant-1", fmt.Sprint(memberRole.ID))
	status := string(entity.StatusValid)
	memberStatus := entity.GroupMemberStatusJoined
	group1 := entity.GroupInfo{ID: "group-1", Name: stringPointer("Joined"), OwnerID: "owner-link", TenantID: "tenant-1", Status: &status}
	group2 := entity.GroupInfo{ID: "group-2", Name: stringPointer("Managed"), OwnerID: "owner-link", TenantID: "tenant-1", Status: &status}
	group3 := entity.GroupInfo{ID: "group-3", Name: stringPointer("Hidden"), OwnerID: "owner-link", TenantID: "tenant-1", Status: &status}
	if err := db.Create(&[]entity.GroupInfo{group1, group2, group3}).Error; err != nil {
		t.Fatalf("create groups: %v", err)
	}
	if err := db.Create(&entity.GroupMember{ID: "member-group-1", MemberID: "member-link", GroupID: group1.ID, Status: &memberStatus}).Error; err != nil {
		t.Fatalf("join first group: %v", err)
	}
	permission := entity.Permission{
		ID: "group-permission", MemberID: stringPointer("member-link"), TenantID: "tenant-1",
		ResourceType: groupPermissionResourceType, ResourceID: &group2.ID,
		Permission: entity.GroupPermissionAddMember, Status: &status,
	}
	if err := db.Create(&permission).Error; err != nil {
		t.Fatalf("grant group management permission: %v", err)
	}

	groups, err := NewGroupService().ListGroups(groupTestContext(t, "member"), "member", "tenant-1")
	if err != nil {
		t.Fatalf("ListGroups(): %v", err)
	}
	if len(groups) != 2 || groups[0].ID != group1.ID || groups[1].ID != group2.ID {
		t.Fatalf("ListGroups() IDs = %#v, want joined and managed groups only", groups)
	}
}

func TestGroupReadRequiresTenantRoleReadAction(t *testing.T) {
	db := setupTenantRoleTestDB(t)
	seedTenantRoleTenant(t, db, "tenant-1")
	readDisabledRole := createGroupFeatureRole(t, db, "tenant-1", entity.RoleActionEnable)
	seedTenantRoleMember(t, db, "member-link", "member", "tenant-1", fmt.Sprint(readDisabledRole.ID))
	status := string(entity.StatusValid)
	joined := entity.GroupMemberStatusJoined
	group := entity.GroupInfo{ID: "group-1", Name: stringPointer("Group"), OwnerID: "owner-link", TenantID: "tenant-1", Status: &status}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := db.Create(&entity.GroupMember{ID: "member-group-1", MemberID: "member-link", GroupID: group.ID, Status: &joined}).Error; err != nil {
		t.Fatalf("join group: %v", err)
	}
	if _, err := NewGroupService().ListGroups(groupTestContext(t, "member"), "member", "tenant-1"); err == nil {
		t.Fatal("ListGroups succeeded without group.read")
	} else {
		var coded *common.CodedError
		if !errors.As(err, &coded) || coded.Code != common.CodeForbidden {
			t.Fatalf("ListGroups without group.read error = %v, want forbidden", err)
		}
	}
	readRole := createGroupFeatureRole(t, db, "tenant-1", entity.RoleActionEnable|entity.RoleActionRead)
	if err := db.Model(&entity.UserTenant{}).Where("id = ?", "member-link").Update("role", fmt.Sprint(readRole.ID)).Error; err != nil {
		t.Fatalf("assign readable group role: %v", err)
	}
	groups, err := NewGroupService().ListGroups(groupTestContext(t, "member"), "member", "tenant-1")
	if err != nil || len(groups) != 1 || groups[0].ID != group.ID {
		t.Fatalf("ListGroups with group.read = (%v, %v), want one visible group", groups, err)
	}
}

func TestSetGroupMemberPermissionsRequiresTenantRoleShareAction(t *testing.T) {
	db := setupTenantRoleTestDB(t)
	seedTenantRoleTenant(t, db, "tenant-1")
	seedTenantRoleMember(t, db, "owner-link", "owner", "tenant-1", "owner")
	managerRole := createGroupFeatureRole(t, db, "tenant-1", entity.RoleActionEnable|entity.RoleActionRead|entity.RoleActionWrite)
	seedTenantRoleMember(t, db, "manager-link", "manager", "tenant-1", fmt.Sprint(managerRole.ID))
	seedTenantRoleMember(t, db, "target-link", "target", "tenant-1", "normal")
	status := string(entity.StatusValid)
	group := entity.GroupInfo{ID: "group-1", Name: stringPointer("Group"), OwnerID: "owner-link", TenantID: "tenant-1", Status: &status}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	joined := entity.GroupMemberStatusJoined
	if err := db.Create(&[]entity.GroupMember{
		{ID: "manager-membership", MemberID: "manager-link", GroupID: group.ID, Status: &joined},
		{ID: "target-membership", MemberID: "target-link", GroupID: group.ID, Status: &joined},
	}).Error; err != nil {
		t.Fatalf("create group memberships: %v", err)
	}
	if err := db.Create(&entity.Permission{
		ID: "manager-permission", MemberID: stringPointer("manager-link"), TenantID: "tenant-1",
		ResourceType: groupPermissionResourceType, ResourceID: &group.ID,
		Permission: entity.GroupPermissionConfigureMemberPermissions, Status: &status,
	}).Error; err != nil {
		t.Fatalf("grant group permission configuration: %v", err)
	}
	if _, err := NewGroupService().SetGroupMemberPermissions(groupTestContext(t, "manager"), "manager", "tenant-1", group.ID, "target", []string{"add_member"}); err == nil {
		t.Fatal("SetGroupMemberPermissions succeeded without group.share")
	} else {
		var coded *common.CodedError
		if !errors.As(err, &coded) || coded.Code != common.CodeForbidden {
			t.Fatalf("SetGroupMemberPermissions without group.share error = %v, want forbidden", err)
		}
	}
}

func TestSetGroupMemberPermissionsRejectsActor(t *testing.T) {
	db := setupTenantRoleTestDB(t)
	seedTenantRoleTenant(t, db, "tenant-1")
	seedTenantRoleMember(t, db, "owner-link", "owner", "tenant-1", "owner")
	status := string(entity.StatusValid)
	joined := entity.GroupMemberStatusJoined
	group := entity.GroupInfo{ID: "group-1", Name: stringPointer("Group"), OwnerID: "owner-link", TenantID: "tenant-1", Status: &status}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := db.Create(&entity.GroupMember{ID: "owner-group-member", MemberID: "owner-link", GroupID: group.ID, Status: &joined}).Error; err != nil {
		t.Fatalf("create group membership: %v", err)
	}

	_, err := NewGroupService().SetGroupMemberPermissions(groupTestContext(t, "owner"), "owner", "tenant-1", group.ID, "owner", []string{"add_member"})
	if err == nil {
		t.Fatal("SetGroupMemberPermissions allowed changing the actor's own permission")
	}
	var permissionCount int64
	if err := db.Model(&entity.Permission{}).Where("resource_type = ? AND resource_id = ? AND member_id = ?", groupPermissionResourceType, group.ID, "owner-link").Count(&permissionCount).Error; err != nil {
		t.Fatalf("count actor group permissions: %v", err)
	}
	if permissionCount != 0 {
		t.Fatalf("actor group permission count = %d after rejected change, want 0", permissionCount)
	}
}

func TestDeleteGroupRecursivelyRevokesMembersAndGrants(t *testing.T) {
	db := setupTenantRoleTestDB(t)
	seedTenantRoleTenant(t, db, "tenant-1")
	seedTenantRoleMember(t, db, "owner-link", "owner", "tenant-1", "owner")
	seedTenantRoleMember(t, db, "member-link", "member", "tenant-1", "normal")
	status := string(entity.StatusValid)
	childParent := "root-group"
	root := entity.GroupInfo{ID: "root-group", Name: stringPointer("Root"), OwnerID: "owner-link", TenantID: "tenant-1", Status: &status}
	child := entity.GroupInfo{ID: "child-group", Name: stringPointer("Child"), OwnerID: "owner-link", TenantID: "tenant-1", ParentID: &childParent, Status: &status}
	if err := db.Create(&[]entity.GroupInfo{root, child}).Error; err != nil {
		t.Fatalf("create groups: %v", err)
	}
	if err := db.Create(&entity.GroupMember{ID: "child-member", MemberID: "member-link", GroupID: child.ID, Status: &status}).Error; err != nil {
		t.Fatalf("create child membership: %v", err)
	}
	grant := entity.Permission{
		ID: "child-grant", MemberID: stringPointer("member-link"), TenantID: "tenant-1",
		ResourceType: groupPermissionResourceType, ResourceID: &child.ID,
		Permission: entity.GroupPermissionAddMember, Status: &status,
	}
	if err := db.Create(&grant).Error; err != nil {
		t.Fatalf("create child grant: %v", err)
	}

	if err := NewGroupService().DeleteGroup(groupTestContext(t, "owner"), "owner", "tenant-1", root.ID); err != nil {
		t.Fatalf("DeleteGroup(): %v", err)
	}
	var groups []entity.GroupInfo
	if err := db.Order("id ASC").Find(&groups).Error; err != nil {
		t.Fatalf("load deleted groups: %v", err)
	}
	for _, group := range groups {
		if group.Status == nil || *group.Status != string(entity.StatusInvalid) {
			t.Errorf("group %s status = %v, want invalid", group.ID, group.Status)
		}
	}
	var member entity.GroupMember
	if err := db.Where("id = ?", "child-member").Take(&member).Error; err != nil {
		t.Fatalf("load removed membership: %v", err)
	}
	if member.Status == nil || *member.Status != entity.GroupMemberStatusRemoved {
		t.Fatalf("child membership status = %v, want removed", member.Status)
	}
	var permission entity.Permission
	if err := db.Where("id = ?", "child-grant").Take(&permission).Error; err != nil {
		t.Fatalf("load revoked permission: %v", err)
	}
	if permission.Status == nil || *permission.Status != string(entity.StatusInvalid) {
		t.Fatalf("child permission status = %v, want invalid", permission.Status)
	}
}

func TestTransferGroupOwnershipActivatesTargetAndAuditsChange(t *testing.T) {
	db := setupTenantRoleTestDB(t)
	seedTenantRoleTenant(t, db, "tenant-1")
	seedTenantRoleMember(t, db, "owner-link", "owner", "tenant-1", "owner")
	seedTenantRoleMember(t, db, "target-link", "target", "tenant-1", "normal")
	status := string(entity.StatusValid)
	pending := entity.GroupMemberStatusInvited
	group := entity.GroupInfo{ID: "group-1", Name: stringPointer("Group"), OwnerID: "owner-link", TenantID: "tenant-1", Status: &status}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := db.Create(&[]entity.GroupMember{
		{ID: "owner-member", MemberID: "owner-link", GroupID: group.ID, Status: &status},
		{ID: "target-member", MemberID: "target-link", GroupID: group.ID, Status: &pending},
	}).Error; err != nil {
		t.Fatalf("create group memberships: %v", err)
	}

	if _, err := NewGroupService().TransferGroupOwnership(groupTestContext(t, "owner"), "owner", "tenant-1", group.ID, "target"); err != nil {
		t.Fatalf("transfer group ownership: %v", err)
	}
	var updated entity.GroupInfo
	if err := db.Where("id = ?", group.ID).Take(&updated).Error; err != nil {
		t.Fatalf("load group after transfer: %v", err)
	}
	if updated.OwnerID != "target-link" {
		t.Fatalf("group owner_id = %q, want target-link", updated.OwnerID)
	}
	var targetMember entity.GroupMember
	if err := db.Where("id = ?", "target-member").Take(&targetMember).Error; err != nil {
		t.Fatalf("load new owner membership: %v", err)
	}
	if targetMember.Status == nil || *targetMember.Status != entity.GroupMemberStatusJoined {
		t.Fatalf("new owner group membership status = %v, want joined", targetMember.Status)
	}
	var audit entity.PermissionChangeLog
	if err := db.Where("event_type = ? AND resource_id = ?", "group.owner.transferred", group.ID).Take(&audit).Error; err != nil {
		t.Fatalf("load ownership audit: %v", err)
	}
	if audit.ActorUserID == nil || *audit.ActorUserID != "owner" {
		t.Fatalf("ownership audit actor = %v, want owner", audit.ActorUserID)
	}
}

func createGroupFeatureRole(t *testing.T, db *gorm.DB, tenantID string, action int32) *entity.Role {
	t.Helper()
	role := &entity.Role{RoleName: "group-role", TenantID: stringPointer(tenantID)}
	if err := db.Create(role).Error; err != nil {
		t.Fatalf("create group feature role: %v", err)
	}
	if err := db.Create(&entity.RoleResource{RoleID: role.ID, ResourceType: entity.RoleResourceTypeGroup, Action: action}).Error; err != nil {
		t.Fatalf("create group feature role permission: %v", err)
	}
	return role
}
