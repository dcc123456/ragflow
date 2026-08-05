//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

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
