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

	"ragflow/internal/entity"

	"gorm.io/gorm"
)

// CountUserTenantByTenantIDs counts user-tenant bindings for the given tenants.
func (dao *UserTenantDAO) CountUserTenantByTenantIDs(ctx context.Context, db *gorm.DB, tenantIDs []string) (int64, error) {
	var total int64
	err := db.WithContext(ctx).Model(&entity.UserTenant{}).
		Where("tenant_id IN ?", tenantIDs).
		Count(&total).Error
	return total, err
}

// FindActiveTenantIDForLogin prefers the last active tenant, then the user's
// personal tenant.
func (dao *UserTenantDAO) FindActiveTenantIDForLogin(ctx context.Context, db *gorm.DB, userID string) (string, error) {
	type tenantChoice struct {
		TenantID string `gorm:"column:tenant_id"`
	}
	query := func() *gorm.DB {
		return db.WithContext(ctx).Table("user_tenant").
			Select("user_tenant.tenant_id").
			Joins("JOIN tenant ON tenant.id = user_tenant.tenant_id").
			Where("user_tenant.user_id = ? AND user_tenant.status = ? AND tenant.status = ?", userID, entity.TenantMembershipStatusJoined, string(entity.StatusValid))
	}

	var choice tenantChoice
	err := query().Joins("JOIN user AS login_user ON login_user.id = user_tenant.user_id").
		Where("user_tenant.tenant_id = login_user.last_login_tenant").Take(&choice).Error
	if err == nil {
		return choice.TenantID, nil
	}
	if !IsNotFoundErr(err) {
		return "", err
	}

	err = query().Where("user_tenant.tenant_id = ?", userID).Take(&choice).Error
	if IsNotFoundErr(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return choice.TenantID, nil
}

// UpdateLastLoginTenantID records the tenant selected for the user's session.
func (dao *UserTenantDAO) UpdateLastLoginTenantID(ctx context.Context, db *gorm.DB, userID, tenantID string) error {
	return db.WithContext(ctx).Table("user").Where("id = ?", userID).
		Update("last_login_tenant", tenantID).Error
}

// GetMembersByTenantIDIncludingOwner returns joined, invited, requested, and
// left tenant members, including the owner.
func (dao *UserTenantDAO) GetMembersByTenantIDIncludingOwner(ctx context.Context, db *gorm.DB, tenantID string) ([]*TenantMemberItem, error) {
	var results []*TenantMemberItem
	err := db.WithContext(ctx).Table("user_tenant").
		Select("user_tenant.id, user_tenant.user_id, user_tenant.role, user_tenant.status, "+
			"user.nickname, user.email, user.avatar, user.is_authenticated, "+
			"user.status AS is_active, user.is_anonymous, user.is_superuser, "+
			"DATE_FORMAT(user.update_date, '%Y-%m-%dT%H:%i:%s') AS update_date").
		Joins("JOIN user ON user_tenant.user_id = user.id").
		Where("user_tenant.tenant_id = ? AND (user_tenant.status IN ? OR (user_tenant.status = ? AND user_tenant.role <> ''))",
			tenantID,
			[]string{
				entity.TenantMembershipStatusJoined,
				entity.TenantMembershipStatusInvited,
				entity.TenantMembershipStatusRequested,
			},
			entity.TenantMembershipStatusLeft,
		).
		Scan(&results).Error
	return results, err
}

type UTOrphanRow struct {
	UTNoUser         int64 `gorm:"column:ut_no_user"`
	UTNoTenantRecord int64 `gorm:"column:ut_no_tenant_record"`
}

// GetUserTenantOrphans returns the count of user_tenant rows whose user or tenant no longer exists.
func (dao *UserTenantDAO) GetUserTenantOrphans(ctx context.Context, db *gorm.DB) (UTOrphanRow, error) {
	var row UTOrphanRow
	err := db.WithContext(ctx).Raw(`
		SELECT
			SUM(CASE WHEN NOT EXISTS (SELECT 1 FROM user u WHERE u.id = ut.user_id) THEN 1 ELSE 0 END) AS ut_no_user,
			SUM(CASE WHEN NOT EXISTS (SELECT 1 FROM tenant t WHERE t.id = ut.tenant_id) THEN 1 ELSE 0 END) AS ut_no_tenant_record
		FROM user_tenant ut
	`).Scan(&row).Error
	return row, err
}
