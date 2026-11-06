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
