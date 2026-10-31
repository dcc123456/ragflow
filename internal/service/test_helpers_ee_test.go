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
	"errors"
	"strconv"
	"testing"

	"gorm.io/gorm"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
)

func prepareServiceTestContextEE(t *testing.T, tenantID string, memberIDs ...string) {
	t.Helper()
	if dao.DB == nil {
		return
	}
	sqlDB, err := dao.DB.DB()
	if err != nil || sqlDB.Ping() != nil {
		return
	}
	models := []interface{}{
		&entity.User{}, &entity.UserTenant{}, &entity.Role{}, &entity.RoleResource{},
		&entity.GroupInfo{}, &entity.GroupMember{}, &entity.Permission{}, &entity.PermissionChangeLog{},
	}
	var missing []interface{}
	for _, model := range models {
		if !dao.DB.Migrator().HasTable(model) {
			missing = append(missing, model)
		}
	}
	if len(missing) > 0 {
		if err := dao.DB.AutoMigrate(missing...); err != nil {
			t.Fatalf("migrate permission test tables: %v", err)
		}
	}
	roleID := ensureServiceTestTenantRole(t, tenantID)
	for _, memberID := range memberIDs {
		if memberID == "" {
			continue
		}
		var user entity.User
		err := dao.DB.Where("id = ?", memberID).Take(&user).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			status := string(entity.StatusValid)
			if err := dao.DB.Create(&entity.User{
				ID: memberID, Nickname: memberID, Email: memberID + "@test.local",
				IsAuthenticated: "1", IsActive: "1", IsAnonymous: "0", Status: &status,
			}).Error; err != nil {
				t.Fatalf("create test user %q: %v", memberID, err)
			}
		} else if err != nil {
			t.Fatalf("load test user %q: %v", memberID, err)
		}
		var membership entity.UserTenant
		err = dao.DB.Where("user_id = ? AND tenant_id = ?", memberID, tenantID).Take(&membership).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			role := string(permission.RoleNormal)
			if memberID == tenantID {
				role = string(permission.RoleOwner)
			} else {
				role = strconv.FormatInt(int64(roleID), 10)
			}
			status := entity.TenantMembershipStatusJoined
			if err := dao.DB.Create(&entity.UserTenant{
				ID: common.GenerateToken(), UserID: memberID, TenantID: tenantID, Role: role, Status: &status,
			}).Error; err != nil {
				t.Fatalf("create test tenant membership: %v", err)
			}
		} else if err != nil {
			t.Fatalf("load test tenant membership: %v", err)
		} else if membership.Role != string(permission.RoleOwner) {
			if _, parseErr := strconv.ParseInt(membership.Role, 10, 32); parseErr != nil {
				if err := dao.DB.Model(&entity.UserTenant{}).Where("id = ?", membership.ID).
					Update("role", strconv.FormatInt(int64(roleID), 10)).Error; err != nil {
					t.Fatalf("assign test tenant role: %v", err)
				}
			}
		}
	}
}

func ensureServiceTestTenantRole(t *testing.T, tenantID string) int32 {
	t.Helper()
	var role entity.Role
	err := dao.DB.Where("tenant_id = ? AND role_name = ?", tenantID, "service-test-default").Take(&role).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		role = entity.Role{RoleName: "service-test-default", TenantID: &tenantID}
		if err := dao.DB.Create(&role).Error; err != nil {
			t.Fatalf("create test tenant role: %v", err)
		}
	} else if err != nil {
		t.Fatalf("load test tenant role: %v", err)
	}
	resourceTypes := []int64{
		entity.RoleResourceTypeDataset, entity.RoleResourceTypeChat, entity.RoleResourceTypeAgent,
		entity.RoleResourceTypeSearch, entity.RoleResourceTypeFile, entity.RoleResourceTypeGroup,
		entity.RoleResourceTypeMemory, entity.RoleResourceTypeModelProvider, entity.RoleResourceTypeMCP,
	}
	for _, resourceType := range resourceTypes {
		var count int64
		if err := dao.DB.Model(&entity.RoleResource{}).
			Where("role_id = ? AND resource_type = ?", role.ID, resourceType).Count(&count).Error; err != nil {
			t.Fatalf("check test role permissions: %v", err)
		}
		if count == 0 {
			if err := dao.DB.Create(&entity.RoleResource{
				RoleID: role.ID, ResourceType: resourceType, Action: entity.RoleActionAll,
			}).Error; err != nil {
				t.Fatalf("create test role permission: %v", err)
			}
		}
	}
	return role.ID
}

func grantServiceTestResource(t *testing.T, tenantID, resourceType, resourceID, userID string, mask int32) {
	t.Helper()
	if !dao.DB.Migrator().HasTable(&entity.Permission{}) {
		if err := dao.DB.AutoMigrate(&entity.Permission{}); err != nil {
			t.Fatalf("migrate resource permission table: %v", err)
		}
	}
	var membership entity.UserTenant
	if err := dao.DB.Where("user_id = ? AND tenant_id = ? AND status = ?", userID, tenantID, entity.TenantMembershipStatusJoined).
		Take(&membership).Error; err != nil {
		t.Fatalf("load resource grantee membership: %v", err)
	}
	memberID := membership.ID
	if err := dao.DB.Create(&entity.Permission{
		ID: common.GenerateToken(), TenantID: tenantID, ResourceType: resourceType, ResourceID: sptr(resourceID),
		MemberID: &memberID, Permission: mask, Status: sptr(string(entity.StatusValid)),
	}).Error; err != nil {
		t.Fatalf("grant test resource permission: %v", err)
	}
}

func restrictServiceTestFeatureRole(t *testing.T, tenantID, resourceType string, action int32) {
	t.Helper()
	resourceTypes := map[string]int64{
		"file": entity.RoleResourceTypeFile,
		"mcp":  entity.RoleResourceTypeMCP,
	}
	var role entity.Role
	if err := dao.DB.Where("tenant_id = ? AND role_name = ?", tenantID, "service-test-default").Take(&role).Error; err != nil {
		t.Fatalf("load member feature role: %v", err)
	}
	if err := dao.DB.Model(&entity.RoleResource{}).
		Where("role_id = ? AND resource_type = ?", role.ID, resourceTypes[resourceType]).
		Update("action", action).Error; err != nil {
		t.Fatalf("restrict member feature role: %v", err)
	}
}

// permissionChangeLogTestModels adds the permission audit log written by the enterprise tenant flows.
func permissionChangeLogTestModels() []interface{} {
	return []interface{}{
		&entity.PermissionChangeLog{}, &entity.GroupInfo{}, &entity.GroupMember{}, &entity.Permission{},
	}
}
