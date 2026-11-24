//
//  Copyright (c) 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//
//  This file is part of the RAGFlow Enterprise Edition and is licensed,
//  not sold, under the RAGFlow Enterprise Commercial License Agreement.
//  Unauthorized copying, modification, distribution, or disclosure of this
//  file, via any medium, is strictly prohibited.
//
//  Portions of this file may incorporate code from RAGFlow Community Edition,
//  which is licensed under the Apache License, Version 2.0.
//
//  For commercial licensing inquiries: sales@infiniflow.ai
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
	{resourceType: entity.RoleResourceTypeGroup, action: entity.RoleActionAll},            // group
	{resourceType: entity.RoleResourceTypeMemory, action: entity.RoleActionAll},           // memory
	{resourceType: entity.RoleResourceTypeModelProvider, action: entity.RoleActionEnable}, // model_provider
	{resourceType: entity.RoleResourceTypeModelGroup, action: entity.RoleActionEnable},    // model_group
	{resourceType: entity.RoleResourceTypeDataSource, action: entity.RoleActionEnable},    // data_source
	{resourceType: entity.RoleResourceTypeChatChannel, action: entity.RoleActionEnable},   // chat_channel
	{resourceType: entity.RoleResourceTypeMCP, action: entity.RoleActionAll},              // mcp
}

func initializeTenantDefaultRoleEE(ctx context.Context, db *gorm.DB, tenant *entity.Tenant) error {
	if tenant == nil {
		return fmt.Errorf("Tenant is required")
	}
	if !db.WithContext(ctx).Migrator().HasTable(&entity.Role{}) {
		return fmt.Errorf("cannot initialize default Tenant role: role table is missing")
	}
	if !db.WithContext(ctx).Migrator().HasTable(&entity.RoleResource{}) {
		return fmt.Errorf("cannot initialize default Tenant role: role_resource table is missing")
	}
	if err := ensureDefaultTenantSystemRoleEE(ctx, db); err != nil {
		return fmt.Errorf("initialize default Tenant system role: %w", err)
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
	tenant.DefaultRoleID = &role.ID
	systemRole, err := GetDefaultTenantSystemRole(ctx, db)
	if err != nil {
		return fmt.Errorf("load default Tenant system role: %w", err)
	}
	if systemRole == nil {
		return fmt.Errorf("a default system role must be configured before creating a Tenant")
	}
	tenant.RoleID = &systemRole.ID
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
