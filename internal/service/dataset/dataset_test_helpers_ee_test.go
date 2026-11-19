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

package dataset

import (
	"errors"
	"strconv"
	"testing"

	"gorm.io/gorm"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/engine/history"
	"ragflow/internal/engine/history/historytest"
	"ragflow/internal/entity"
)

// installDatasetTestChunkHistory installs the in-memory store the chunk history
// bodies live in, and removes it when the test ends.
//
// Production hands the store to history from the document engine that owns
// the connection. A test database has no engine, so without this stand-in every
// replay and every purge answers "this deployment keeps no chunk history" - a
// fixture gap that hides the behaviour the test is about.
func installDatasetTestChunkHistory(t *testing.T) {
	t.Helper()
	history.SetChunkStore(historytest.NewChunkStore())
	t.Cleanup(func() { history.SetChunkStore(nil) })
}

func datasetTestExtraModels() []interface{} {
	return []interface{}{
		&entity.Role{}, &entity.RoleResource{}, &entity.GroupInfo{}, &entity.GroupMember{},
		&entity.Permission{}, &entity.PermissionChangeLog{},
		&entity.DatasetVersion{},
		// The configuration snapshots and document sets the version rows point at.
		&entity.DatasetConfigSnapshot{},
		&entity.DatasetDocumentSetSnapshot{},
		&entity.ChunkVersion{},
		// A dataset delete collects these too, so every ee test database has to
		// carry the tables the production schema migrates.
		&entity.DatasetRemovedDocument{},
		// The version-keyed states a document-configuration diff reads: the
		// millisecond-keyed history cannot separate two edits in one millisecond.
		&entity.DocumentVersion{},
	}
}

func datasetTestMembershipRole(t *testing.T, tenantID, role string) string {
	t.Helper()
	if role == "owner" {
		return role
	}

	var tenantRole entity.Role
	err := dao.DB.Where("tenant_id = ? AND role_name = ?", tenantID, "dataset-test-member").Take(&tenantRole).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		tenantRole = entity.Role{RoleName: "dataset-test-member", TenantID: &tenantID}
		if err := dao.DB.Create(&tenantRole).Error; err != nil {
			t.Fatalf("create dataset test role: %v", err)
		}
	} else if err != nil {
		t.Fatalf("load dataset test role: %v", err)
	}

	for _, resourceType := range []int64{entity.RoleResourceTypeDataset, entity.RoleResourceTypeAgent} {
		var count int64
		if err := dao.DB.Model(&entity.RoleResource{}).
			Where("role_id = ? AND resource_type = ?", tenantRole.ID, resourceType).
			Count(&count).Error; err != nil {
			t.Fatalf("count dataset test role permissions: %v", err)
		}
		if count == 0 {
			if err := dao.DB.Create(&entity.RoleResource{
				RoleID: tenantRole.ID, ResourceType: resourceType, Action: entity.RoleActionAll,
			}).Error; err != nil {
				t.Fatalf("grant dataset test role actions: %v", err)
			}
		}
	}
	return strconv.FormatInt(int64(tenantRole.ID), 10)
}

func grantDatasetTestResource(t *testing.T, tenantID, resourceType, resourceID, userID string, actions ...string) {
	t.Helper()
	var membership entity.UserTenant
	if err := dao.DB.Where("user_id = ? AND tenant_id = ? AND status = ?",
		userID, tenantID, string(entity.StatusValid)).Take(&membership).Error; err != nil {
		t.Fatalf("load dataset test membership: %v", err)
	}
	var mask int32
	for _, action := range actions {
		switch action {
		case "read":
			mask |= entity.ResourcePermissionRead
		case "write":
			mask |= entity.ResourcePermissionWrite
		case "manage":
			mask |= entity.ResourcePermissionManage
		case "owner":
			mask |= entity.ResourcePermissionOwner
		default:
			t.Fatalf("unknown dataset test resource permission %q", action)
		}
	}
	if mask == 0 {
		t.Fatal("dataset test resource permission mask is empty")
	}
	status := string(entity.StatusValid)
	memberID, resourceIDValue := membership.ID, resourceID
	grant := entity.Permission{
		ID: common.GenerateUUID(), MemberID: &memberID, TenantID: tenantID,
		ResourceType: resourceType, ResourceID: &resourceIDValue,
		Permission: mask, Status: &status,
	}
	if err := dao.DB.Create(&grant).Error; err != nil {
		t.Fatalf("grant dataset test resource permission: %v", err)
	}
}

func enableDatasetTestModelAccess(t *testing.T, tenantID, userID, instanceID string) {
	t.Helper()
	var membership entity.UserTenant
	if err := dao.DB.Where("user_id = ? AND tenant_id = ? AND status = ?", userID, tenantID, string(entity.StatusValid)).Take(&membership).Error; err != nil {
		t.Fatalf("load dataset test model membership: %v", err)
	}
	roleID64, err := strconv.ParseInt(membership.Role, 10, 32)
	if err != nil || roleID64 <= 0 {
		t.Fatalf("parse dataset test tenant role %q: %v", membership.Role, err)
	}
	roleID := int32(roleID64)
	var roleResource entity.RoleResource
	err = dao.DB.Where("role_id = ? AND resource_type = ?", roleID, entity.RoleResourceTypeModelProvider).Take(&roleResource).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if err := dao.DB.Create(&entity.RoleResource{RoleID: roleID, ResourceType: entity.RoleResourceTypeModelProvider, Action: entity.RoleActionEnable}).Error; err != nil {
			t.Fatalf("enable dataset test model feature: %v", err)
		}
	} else if err != nil {
		t.Fatalf("load dataset test model feature: %v", err)
	} else if roleResource.Action&entity.RoleActionEnable == 0 {
		if err := dao.DB.Model(&roleResource).Update("action", roleResource.Action|entity.RoleActionEnable).Error; err != nil {
			t.Fatalf("enable dataset test model feature: %v", err)
		}
	}
	grantDatasetTestResource(t, tenantID, "model_instance", instanceID, userID, "read")
}
