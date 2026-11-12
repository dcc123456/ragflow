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

package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"

	"gorm.io/gorm"
)

func resolveTenantInviteRoleEE(tx *gorm.DB, tenantID string) (tenantInviteRole, bool, error) {
	var tenant entity.Tenant
	if err := tx.Select("id", "default_role_id").Where("id = ?", tenantID).Take(&tenant).Error; err != nil {
		return tenantInviteRole{}, true, fmt.Errorf("load Tenant default role: %w", err)
	}
	if tenant.DefaultRoleID == nil {
		return tenantInviteRole{}, true, common.NewCodedError(common.CodeConflict, "Tenant default role must be configured before accepting invitations")
	}
	role, err := findTenantRole(tx, tenantID, *tenant.DefaultRoleID, false)
	if err != nil {
		return tenantInviteRole{}, true, common.NewCodedError(common.CodeConflict, "Tenant default role is invalid")
	}
	roleID := role.ID
	return tenantInviteRole{
		value:    strconv.FormatInt(int64(role.ID), 10),
		roleID:   &roleID,
		roleName: role.RoleName,
	}, true, nil
}

func (s *TenantService) checkTenantInviteEnabledEE(ctx context.Context, tenantID string) (common.ErrorCode, bool, error) {
	tenant, err := dao.NewTenantDAO().GetByID(ctx, dao.DB, tenantID)
	if err != nil {
		if dao.IsNotFoundErr(err) {
			return common.CodeDataError, true, fmt.Errorf("tenant not found")
		}
		return common.CodeServerError, true, fmt.Errorf("failed to load tenant: %w", err)
	}
	enabled, err := tenantInvitationsEnabled(dao.DB.WithContext(ctx), tenant)
	if err != nil {
		return common.CodeServerError, true, fmt.Errorf("load tenant invitation permission: %w", err)
	}
	if !enabled {
		return common.CodeForbidden, true, fmt.Errorf("the Tenant system role does not enable invitations")
	}
	if tenant.DefaultRoleID == nil {
		return common.CodeConflict, true, fmt.Errorf("tenant default role must be configured before inviting members")
	}
	if _, err := findTenantRole(dao.DB.WithContext(ctx), tenantID, *tenant.DefaultRoleID, false); err != nil {
		return common.CodeConflict, true, fmt.Errorf("tenant default role is invalid")
	}
	return common.CodeSuccess, true, nil
}

func tenantInvitationsEnabled(db *gorm.DB, tenant *entity.Tenant) (bool, error) {
	if tenant.RoleID == nil {
		return false, nil
	}
	var role entity.Role
	if err := db.Where("id = ? AND tenant_id IS NULL", *tenant.RoleID).Take(&role).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	var permission entity.RoleResource
	if err := db.Where("role_id = ? AND resource_type = ?", role.ID, entity.RoleResourceTypeInvitation).Take(&permission).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return permission.Action&entity.RoleActionEnable != 0, nil
}
