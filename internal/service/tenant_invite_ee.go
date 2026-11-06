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
	"fmt"
	"strconv"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"

	"gorm.io/gorm"
)

func resolveTenantInviteRoleEE(tx *gorm.DB, tenantID string) (tenantInviteRole, bool, error) {
	var tenant entity.Tenant
	if err := tx.Select("id", "role_id").Where("id = ?", tenantID).Take(&tenant).Error; err != nil {
		return tenantInviteRole{}, true, fmt.Errorf("load Tenant default role: %w", err)
	}
	if tenant.RoleID == nil {
		return tenantInviteRole{}, true, common.NewCodedError(common.CodeConflict, "Tenant default role must be configured before accepting invitations")
	}
	role, err := findTenantRole(tx, tenantID, *tenant.RoleID, false)
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
	if !tenant.InviteEnabled {
		return common.CodeForbidden, true, fmt.Errorf("tenant invitations are disabled")
	}
	if tenant.RoleID == nil {
		return common.CodeConflict, true, fmt.Errorf("tenant default role must be configured before inviting members")
	}
	if _, err := findTenantRole(dao.DB.WithContext(ctx), tenantID, *tenant.RoleID, false); err != nil {
		return common.CodeConflict, true, fmt.Errorf("tenant default role is invalid")
	}
	return common.CodeSuccess, true, nil
}
