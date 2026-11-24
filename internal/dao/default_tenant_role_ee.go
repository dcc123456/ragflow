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
	"fmt"
	"strconv"
	"strings"

	"ragflow/internal/entity"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const defaultTenantSystemRoleSettingName = "tenant.default_system_role_id"
const defaultTenantSystemRoleName = "public"

func ensureDefaultTenantSystemRoleSetting(ctx context.Context, db *gorm.DB) error {
	setting := entity.SystemSettings{
		Name:     defaultTenantSystemRoleSettingName,
		Source:   "system",
		DataType: "string",
		Value:    "",
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&setting).Error
}

// GetDefaultTenantSystemRole returns the system role assigned to new tenants.
func GetDefaultTenantSystemRole(ctx context.Context, db *gorm.DB) (*entity.Role, error) {
	if err := ensureDefaultTenantSystemRoleSetting(ctx, db); err != nil {
		return nil, fmt.Errorf("ensure default tenant system role setting: %w", err)
	}

	var setting entity.SystemSettings
	if err := db.WithContext(ctx).Where("name = ?", defaultTenantSystemRoleSettingName).Take(&setting).Error; err != nil {
		return nil, fmt.Errorf("load default tenant system role setting: %w", err)
	}
	value := strings.TrimSpace(setting.Value)
	if value == "" {
		return nil, nil
	}

	roleID, err := strconv.ParseInt(value, 10, 32)
	if err != nil || roleID <= 0 {
		return nil, fmt.Errorf("invalid %s value %q: expected a positive role ID", defaultTenantSystemRoleSettingName, setting.Value)
	}
	var role entity.Role
	if err := db.WithContext(ctx).Where("id = ? AND tenant_id IS NULL", roleID).Take(&role).Error; err != nil {
		return nil, fmt.Errorf("load configured default system role %d: %w", roleID, err)
	}
	return &role, nil
}

func ensureDefaultTenantSystemRoleEE(ctx context.Context, db *gorm.DB) error {
	if err := ensureDefaultTenantSystemRoleSetting(ctx, db); err != nil {
		return fmt.Errorf("ensure default Tenant system role setting: %w", err)
	}
	if !db.WithContext(ctx).Migrator().HasTable(&entity.RoleResource{}) {
		return fmt.Errorf("cannot initialize default Tenant system role: role_resource table is missing")
	}

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var setting entity.SystemSettings
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("name = ?", defaultTenantSystemRoleSettingName).Take(&setting).Error; err != nil {
			return fmt.Errorf("load default Tenant system role setting: %w", err)
		}
		if strings.TrimSpace(setting.Value) != "" {
			if _, err := GetDefaultTenantSystemRole(ctx, tx); err != nil {
				return err
			}
			return nil
		}

		var roles []entity.Role
		if err := tx.Where("tenant_id IS NULL").Order("id ASC").Limit(1).Find(&roles).Error; err != nil {
			return fmt.Errorf("find system role for new Tenants: %w", err)
		}
		if len(roles) > 0 {
			return SetDefaultTenantSystemRole(ctx, tx, &roles[0].ID)
		}

		description := "Default system role for new Tenants"
		role := entity.Role{RoleName: defaultTenantSystemRoleName, Description: &description}
		if err := tx.Create(&role).Error; err != nil {
			return fmt.Errorf("create default Tenant system role: %w", err)
		}
		resources := make([]entity.RoleResource, 0, 13)
		for _, permission := range []struct {
			resourceType int64
			action       int32
		}{
			{entity.RoleResourceTypeDataset, entity.RoleActionAll},
			{entity.RoleResourceTypeChat, entity.RoleActionAll},
			{entity.RoleResourceTypeAgent, entity.RoleActionAll},
			{entity.RoleResourceTypeSearch, entity.RoleActionAll},
			{entity.RoleResourceTypeFile, entity.RoleActionAll},
			{entity.RoleResourceTypeGroup, entity.RoleActionAll},
			{entity.RoleResourceTypeMemory, entity.RoleActionAll},
			{entity.RoleResourceTypeModelProvider, entity.RoleActionAll},
			{entity.RoleResourceTypeMCP, entity.RoleActionAll},
		} {
			resources = append(resources, entity.RoleResource{
				RoleID:       role.ID,
				ResourceType: permission.resourceType,
				Action:       permission.action,
			})
		}
		resources = append(resources, entity.RoleResource{
			RoleID:       role.ID,
			ResourceType: entity.RoleResourceTypeInvitation,
			Action:       entity.RoleActionEnable,
		})
		if err := tx.Create(&resources).Error; err != nil {
			return fmt.Errorf("create default Tenant system role permissions: %w", err)
		}
		if err := SetDefaultTenantSystemRole(ctx, tx, &role.ID); err != nil {
			return fmt.Errorf("set default Tenant system role: %w", err)
		}
		return nil
	})
}

// SetDefaultTenantSystemRole stores the system role assigned to new tenants.
func SetDefaultTenantSystemRole(ctx context.Context, db *gorm.DB, roleID *int32) error {
	if roleID == nil || *roleID <= 0 {
		return fmt.Errorf("a default system role is required")
	}
	var role entity.Role
	if err := db.WithContext(ctx).Where("id = ? AND tenant_id IS NULL", *roleID).Take(&role).Error; err != nil {
		return fmt.Errorf("validate default system role %d: %w", *roleID, err)
	}
	setting := entity.SystemSettings{
		Name:     defaultTenantSystemRoleSettingName,
		Source:   "admin",
		DataType: "string",
		Value:    strconv.FormatInt(int64(*roleID), 10),
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"source", "data_type", "value", "update_time", "update_date"}),
	}).Create(&setting).Error
}
