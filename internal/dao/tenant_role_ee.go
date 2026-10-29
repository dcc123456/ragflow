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

package dao

import (
	"context"
	"errors"
	"fmt"

	"ragflow/internal/entity"

	"gorm.io/gorm"
)

const defaultTenantMemberRoleName = "member"

var defaultTenantRoleResourceActions = []struct {
	resourceType int64
	action       int32
}{
	{resourceType: entity.RoleResourceTypeDataset, action: entity.RoleActionAll},          // dataset
	{resourceType: entity.RoleResourceTypeChat, action: entity.RoleActionAll},             // chat
	{resourceType: entity.RoleResourceTypeAgent, action: entity.RoleActionAll},            // agent
	{resourceType: entity.RoleResourceTypeSearch, action: entity.RoleActionAll},           // search
	{resourceType: entity.RoleResourceTypeFile, action: entity.RoleActionAll},             // file
	{resourceType: entity.RoleResourceTypeTeam, action: entity.RoleActionAll},             // team
	{resourceType: entity.RoleResourceTypeMemory, action: entity.RoleActionAll},           // memory
	{resourceType: entity.RoleResourceTypeModelProvider, action: entity.RoleActionEnable}, // model_provider
}

func initializeTenantDefaultRoleEE(ctx context.Context, db *gorm.DB, tenant *entity.Tenant) error {
	if tenant == nil {
		return fmt.Errorf("Tenant is required")
	}
	if !db.WithContext(ctx).Migrator().HasTable(&entity.Role{}) {
		return nil
	}
	if !db.WithContext(ctx).Migrator().HasTable(&entity.RoleResource{}) {
		return fmt.Errorf("cannot initialize default Tenant role: role_resource table is missing")
	}

	role, created, err := findOrCreateDefaultTenantMemberRole(db.WithContext(ctx), tenant.ID)
	if err != nil {
		return err
	}
	if created {
		if err := replaceDefaultTenantRolePermissions(db.WithContext(ctx), role.ID); err != nil {
			return fmt.Errorf("initialize default Tenant member role permissions: %w", err)
		}
	}
	tenant.RoleID = &role.ID
	return nil
}

func findOrCreateDefaultTenantMemberRole(tx *gorm.DB, tenantID string) (*entity.Role, bool, error) {
	var role entity.Role
	err := tx.Where("tenant_id = ? AND role_name = ?", tenantID, defaultTenantMemberRoleName).
		Order("id ASC").Take(&role).Error
	if err == nil {
		return &role, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, fmt.Errorf("find Tenant %s default member role: %w", tenantID, err)
	}

	role = entity.Role{RoleName: defaultTenantMemberRoleName, TenantID: &tenantID}
	if err := tx.Create(&role).Error; err != nil {
		return nil, false, fmt.Errorf("create Tenant %s default member role: %w", tenantID, err)
	}
	return &role, true, nil
}

func replaceDefaultTenantRolePermissions(tx *gorm.DB, roleID int32) error {
	if err := tx.Where("role_id = ?", roleID).Delete(&entity.RoleResource{}).Error; err != nil {
		return fmt.Errorf("remove old role permissions: %w", err)
	}
	rows := make([]entity.RoleResource, 0, len(defaultTenantRoleResourceActions))
	for _, permission := range defaultTenantRoleResourceActions {
		rows = append(rows, entity.RoleResource{
			RoleID:       roleID,
			ResourceType: permission.resourceType,
			Action:       permission.action,
		})
	}
	if err := tx.Create(&rows).Error; err != nil {
		return fmt.Errorf("create full role permissions: %w", err)
	}
	return nil
}
