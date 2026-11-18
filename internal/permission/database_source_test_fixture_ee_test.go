//
// Copyright (c) 2026 InfiniFlow. All rights reserved.
//
// RAGFlow Enterprise Edition — Proprietary and Confidential.
//
// This file is part of the RAGFlow Enterprise Edition and is licensed,
// not sold, under the RAGFlow Enterprise Commercial License Agreement.
// Unauthorized copying, modification, distribution, or disclosure of this
// file, via any medium, is strictly prohibited.
//
// Portions of this file may incorporate code from RAGFlow Community Edition,
// which is licensed under the Apache License, Version 2.0.
//
// For commercial licensing inquiries: sales@infiniflow.ai
//

//go:build ee

package permission

import (
	"errors"
	"fmt"
	"strconv"

	"gorm.io/gorm"

	"ragflow/internal/common"
	"ragflow/internal/entity"
)

func newEditionDatabaseChecker(db *gorm.DB) *Checker { return NewDatabaseChecker(db) }

func prepareEnterpriseDatabaseSourceFixture(db *gorm.DB, tenantID string) error {
	models := []any{
		&entity.Tenant{}, &entity.Role{}, &entity.RoleResource{}, &entity.Permission{},
		&entity.GroupInfo{}, &entity.GroupMember{},
	}
	if err := db.AutoMigrate(models...); err != nil {
		return fmt.Errorf("migrate permission test tables: %w", err)
	}

	systemRole, err := databaseSourceTestRole(db, "permission-test-system", nil)
	if err != nil {
		return err
	}
	tenantRole, err := databaseSourceTestRole(db, "permission-test-member", &tenantID)
	if err != nil {
		return err
	}
	for _, role := range []*entity.Role{systemRole, tenantRole} {
		if err := databaseSourceTestGrantDatasetFeature(db, role.ID); err != nil {
			return err
		}
	}

	var tenant entity.Tenant
	err = db.Where("id = ?", tenantID).Take(&tenant).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		status := string(entity.StatusValid)
		if err := db.Create(&entity.Tenant{
			ID: tenantID, ParserIDs: "", RoleID: &systemRole.ID, DefaultRoleID: &tenantRole.ID, Status: &status,
		}).Error; err != nil {
			return fmt.Errorf("create permission test tenant: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("load permission test tenant: %w", err)
	} else if err := db.Model(&entity.Tenant{}).Where("id = ?", tenantID).Updates(map[string]any{
		"role_id": systemRole.ID, "default_role_id": tenantRole.ID,
	}).Error; err != nil {
		return fmt.Errorf("update permission test tenant roles: %w", err)
	}

	var memberships []entity.UserTenant
	if err := db.Where("tenant_id = ? AND status = ?", tenantID, string(entity.StatusValid)).Find(&memberships).Error; err != nil {
		return fmt.Errorf("load permission test memberships: %w", err)
	}
	for _, membership := range memberships {
		if membership.Role == string(RoleOwner) {
			continue
		}
		if _, err := strconv.ParseInt(membership.Role, 10, 32); err == nil {
			continue
		}
		if err := db.Model(&entity.UserTenant{}).Where("id = ?", membership.ID).
			Update("role", strconv.FormatInt(int64(tenantRole.ID), 10)).Error; err != nil {
			return fmt.Errorf("set permission test membership role: %w", err)
		}
	}

	var datasets []entity.Knowledgebase
	if err := db.Where("tenant_id = ? AND permission = ?", tenantID, string(entity.TenantPermissionTeam)).Find(&datasets).Error; err != nil {
		return fmt.Errorf("load team-visible permission test datasets: %w", err)
	}
	for _, dataset := range datasets {
		for _, membership := range memberships {
			if membership.Role == string(RoleOwner) {
				continue
			}
			if err := databaseSourceTestGrantResource(db, tenantID, "kb", dataset.ID, membership.ID, entity.ResourcePermissionRead|entity.ResourcePermissionWrite); err != nil {
				return err
			}
		}
		var documents []entity.Document
		if err := db.Where("kb_id = ? AND status = ?", dataset.ID, string(entity.StatusValid)).Find(&documents).Error; err != nil {
			return fmt.Errorf("load permission test documents: %w", err)
		}
		for _, document := range documents {
			for _, membership := range memberships {
				if membership.Role == string(RoleOwner) {
					continue
				}
				if err := databaseSourceTestGrantResource(db, tenantID, "document", document.ID, membership.ID, entity.ResourcePermissionRead); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func databaseSourceTestRole(db *gorm.DB, name string, tenantID *string) (*entity.Role, error) {
	query := db.Where("role_name = ?", name)
	if tenantID == nil {
		query = query.Where("tenant_id IS NULL")
	} else {
		query = query.Where("tenant_id = ?", *tenantID)
	}
	var role entity.Role
	if err := query.Take(&role).Error; err == nil {
		return &role, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("load permission test role %q: %w", name, err)
	}
	role = entity.Role{RoleName: name, TenantID: tenantID}
	if err := db.Create(&role).Error; err != nil {
		return nil, fmt.Errorf("create permission test role %q: %w", name, err)
	}
	return &role, nil
}

func databaseSourceTestGrantDatasetFeature(db *gorm.DB, roleID int32) error {
	var count int64
	query := db.Model(&entity.RoleResource{}).Where("role_id = ? AND resource_type = ?", roleID, entity.RoleResourceTypeDataset)
	if err := query.Count(&count).Error; err != nil {
		return fmt.Errorf("check permission test role feature: %w", err)
	}
	if count > 0 {
		return nil
	}
	if err := db.Create(&entity.RoleResource{RoleID: roleID, ResourceType: entity.RoleResourceTypeDataset, Action: entity.RoleActionAll}).Error; err != nil {
		return fmt.Errorf("grant permission test dataset feature: %w", err)
	}
	return nil
}

func databaseSourceTestGrantResource(db *gorm.DB, tenantID, resourceType, resourceID, memberID string, mask int32) error {
	var count int64
	if err := db.Model(&entity.Permission{}).
		Where("tenant_id = ? AND resource_type = ? AND resource_id = ? AND member_id = ? AND (group_id IS NULL OR group_id = '')",
			tenantID, resourceType, resourceID, memberID).Count(&count).Error; err != nil {
		return fmt.Errorf("check permission test %s grant: %w", resourceType, err)
	}
	if count > 0 {
		return nil
	}
	status := string(entity.StatusValid)
	if err := db.Create(&entity.Permission{
		ID: common.GenerateToken(), TenantID: tenantID, ResourceType: resourceType, ResourceID: &resourceID,
		MemberID: &memberID, Permission: mask, Status: &status,
	}).Error; err != nil {
		return fmt.Errorf("grant permission test %s access: %w", resourceType, err)
	}
	return nil
}
