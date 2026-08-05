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

// CountAllValidKB counts all valid knowledgebases.
func (dao *KnowledgebaseDAO) CountAllValidKB(ctx context.Context, db *gorm.DB) (int64, error) {
	var total int64
	err := db.WithContext(ctx).Model(&entity.Knowledgebase{}).
		Where("status = ?", string(entity.StatusValid)).
		Count(&total).Error
	return total, err
}

// CountKBByTenantIDs counts valid knowledgebases for the given tenants.
func (dao *KnowledgebaseDAO) CountKBByTenantIDs(ctx context.Context, db *gorm.DB, tenantIDs []string) (int64, error) {
	var total int64
	err := db.WithContext(ctx).Model(&entity.Knowledgebase{}).
		Where("tenant_id IN ? AND status = ?", tenantIDs, string(entity.StatusValid)).
		Count(&total).Error
	return total, err
}

// SumKBChunkTokenByTenantIDs sums chunk_num and token_num across valid knowledgebases.
func (dao *KnowledgebaseDAO) SumKBChunkTokenByTenantIDs(ctx context.Context, db *gorm.DB, tenantIDs []string) (chunkNum, tokenNum int64, err error) {
	var row struct {
		TotalChunkNum int64 `gorm:"column:total_chunk_num"`
		TotalTokenNum int64 `gorm:"column:total_token_num"`
	}
	err = db.WithContext(ctx).Model(&entity.Knowledgebase{}).
		Select("COALESCE(SUM(chunk_num), 0) as total_chunk_num, COALESCE(SUM(token_num), 0) as total_token_num").
		Where("tenant_id IN ? AND status = ?", tenantIDs, string(entity.StatusValid)).
		Scan(&row).Error
	return row.TotalChunkNum, row.TotalTokenNum, err
}

// KBOrphanRow holds knowledgebase orphan counts.
type KBOrphanRow struct {
	KbNoTenant int64 `gorm:"column:kb_no_tenant"`
	KbNoUser   int64 `gorm:"column:kb_no_user"`
}

// GetKBOrphans returns the count of knowledgebases whose tenant or creator no longer exists.
func (dao *KnowledgebaseDAO) GetKBOrphans(ctx context.Context, db *gorm.DB) (KBOrphanRow, error) {
	var row KBOrphanRow
	err := db.WithContext(ctx).Raw(`
		SELECT
			SUM(CASE WHEN NOT EXISTS (SELECT 1 FROM user_tenant ut WHERE ut.tenant_id = kb.tenant_id) THEN 1 ELSE 0 END) AS kb_no_tenant,
			SUM(CASE WHEN NOT EXISTS (SELECT 1 FROM user u WHERE u.id = kb.created_by) THEN 1 ELSE 0 END) AS kb_no_user
		FROM knowledgebase kb
	`).Scan(&row).Error
	return row, err
}
