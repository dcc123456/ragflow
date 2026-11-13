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

	"ragflow/internal/entity"
	"ragflow/internal/permission"

	"gorm.io/gorm"
)

func markJoinedTenantMemberLeftEE(ctx context.Context, tx *gorm.DB, tenantID, targetUserID string, membership *entity.UserTenant) (bool, error) {
	oldStatus := tenantMembershipStatus(membership.Status)
	if oldStatus != entity.TenantMembershipStatusJoined {
		return false, nil
	}

	newStatus := entity.TenantMembershipStatusLeft
	eventType, actionType := "tenant.member.removed", "remove"
	if permission.UserIDFromContext(ctx) == targetUserID {
		eventType, actionType = "tenant.member.left", "leave"
	}
	if err := tx.Model(&entity.UserTenant{}).
		Where("id = ? AND tenant_id = ?", membership.ID, tenantID).
		Update("status", newStatus).Error; err != nil {
		return false, fmt.Errorf("mark Tenant member as left: %w", err)
	}

	before := map[string]interface{}{
		"user_id": membership.UserID, "role": membership.Role,
		"invited_by": membership.InvitedBy, "status": oldStatus,
	}
	after := map[string]interface{}{
		"user_id": membership.UserID, "role": membership.Role,
		"invited_by": membership.InvitedBy, "status": newStatus,
	}
	if err := recordTenantPermissionChange(ctx, tx, tenantID, eventType, "user", targetUserID, "tenant_membership", membership.ID, actionType, 0, 0, before, after); err != nil {
		return false, err
	}
	return true, nil
}
