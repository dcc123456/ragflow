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

// SumDocSizeByKBID returns the total document size for a knowledgebase.
func (dao *DocumentDAO) SumDocSizeByKBID(ctx context.Context, db *gorm.DB, kbID string) (int64, error) {
	var total int64
	err := db.WithContext(ctx).Model(&entity.Document{}).
		Where("kb_id = ?", kbID).
		Select("COALESCE(SUM(size), 0) AS total").
		Scan(&total).Error
	return total, err
}

// DocCountAndSizeByTenantIDs returns the total document count and size for the given tenants.
func (dao *DocumentDAO) DocCountAndSizeByTenantIDs(ctx context.Context, db *gorm.DB, tenantIDs []string) (int64, int64, error) {
	var row struct {
		DocCount  int64 `gorm:"column:doc_count"`
		TotalSize int64 `gorm:"column:total_size"`
	}
	err := db.WithContext(ctx).Raw(`
		SELECT COUNT(d.id) AS doc_count, COALESCE(SUM(d.size), 0) AS total_size
		FROM document d
		INNER JOIN knowledgebase kb ON kb.id = d.kb_id
		WHERE kb.tenant_id IN ? AND kb.status = ?
	`, tenantIDs, string(entity.StatusValid)).Scan(&row).Error
	return row.DocCount, row.TotalSize, err
}

// DocTopRow holds a document name and its size.
type DocTopRow struct {
	Name string `gorm:"column:name"`
	Size int64  `gorm:"column:size"`
}

// ListTopDocumentsByTenantIDs returns the top documents by size for the given tenants.
func (dao *DocumentDAO) ListTopDocumentsByTenantIDs(ctx context.Context, db *gorm.DB, tenantIDs []string, top int) ([]DocTopRow, error) {
	var rows []DocTopRow
	err := db.WithContext(ctx).Raw(`
		SELECT d.name AS name, d.size AS size
		FROM document d
		INNER JOIN knowledgebase kb ON kb.id = d.kb_id
		WHERE kb.tenant_id IN ? AND kb.status = ?
		ORDER BY d.size DESC
		LIMIT ?
	`, tenantIDs, string(entity.StatusValid), top).Scan(&rows).Error
	return rows, err
}

// DocOrphanRow holds document orphan counts.
type DocOrphanRow struct {
	DocNoKb       int64 `gorm:"column:doc_no_kb"`
	DocNoUser     int64 `gorm:"column:doc_no_user"`
	DocKbNoTenant int64 `gorm:"column:doc_kb_no_tenant"`
}

// GetDocOrphans returns the count of documents whose knowledgebase or tenant no longer exists.
func (dao *DocumentDAO) GetDocOrphans(ctx context.Context, db *gorm.DB) (DocOrphanRow, error) {
	var row DocOrphanRow
	err := db.WithContext(ctx).Raw(`
		SELECT
			SUM(CASE WHEN NOT EXISTS (SELECT 1 FROM knowledgebase kb WHERE kb.id = d.kb_id) THEN 1 ELSE 0 END) AS doc_no_kb,
			SUM(CASE WHEN NOT EXISTS (SELECT 1 FROM user u WHERE u.id = d.created_by) THEN 1 ELSE 0 END) AS doc_no_user,
			SUM(CASE WHEN NOT EXISTS (SELECT 1 FROM user_tenant ut WHERE ut.tenant_id = (
				SELECT kb2.tenant_id FROM knowledgebase kb2 WHERE kb2.id = d.kb_id
			)) THEN 1 ELSE 0 END) AS doc_kb_no_tenant
		FROM document d
	`).Scan(&row).Error
	return row, err
}

// DocStatAgg holds aggregate document statistics.
type DocStatAgg struct {
	TotalDocs    int64 `gorm:"column:total_docs"`
	TotalDocSize int64 `gorm:"column:total_doc_size"`
	TotalChunks  int64 `gorm:"column:total_chunks"`
	TotalTokens  int64 `gorm:"column:total_tokens"`
}

// DocCountSizeChunkToken returns aggregate document statistics across all valid documents.
func (dao *DocumentDAO) DocCountSizeChunkToken(ctx context.Context, db *gorm.DB) (DocStatAgg, error) {
	var row DocStatAgg
	err := db.WithContext(ctx).Raw(`
		SELECT COUNT(d.id) AS total_docs,
			COALESCE(SUM(d.size), 0) AS total_doc_size,
			COALESCE(SUM(d.chunk_num), 0) AS total_chunks,
			COALESCE(SUM(d.token_num), 0) AS total_tokens
		FROM document d
		INNER JOIN knowledgebase kb ON kb.id = d.kb_id
		WHERE kb.status = ?
	`, string(entity.StatusValid)).Scan(&row).Error
	return row, err
}
