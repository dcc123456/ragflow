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

// FileStorageRow holds total file size and total document size.
type FileStorageRow struct {
	TotalFileSize int64 `gorm:"column:total_file_size"`
	TotalDocSize  int64 `gorm:"column:total_doc_size"`
}

// StorageFileSizeDocSize returns the total file size and total document size.
func (dao *FileDAO) StorageFileSizeDocSize(ctx context.Context, db *gorm.DB) (FileStorageRow, error) {
	var row FileStorageRow
	err := db.WithContext(ctx).Raw(`
		SELECT
			COALESCE(SUM(f.size), 0) AS total_file_size,
			COALESCE((SELECT SUM(d.size)
				FROM document d
				INNER JOIN knowledgebase kb ON kb.id = d.kb_id
				WHERE kb.status = ?), 0) AS total_doc_size
		FROM file f
	`, string(entity.StatusValid)).Scan(&row).Error
	return row, err
}

// FileOrphanRow holds file orphan counts.
type FileOrphanRow struct {
	FileNoTenant int64 `gorm:"column:file_no_tenant"`
	FileNoUser   int64 `gorm:"column:file_no_user"`
}

// GetFileOrphans returns the count of files whose tenant or creator no longer exists.
func (dao *FileDAO) GetFileOrphans(ctx context.Context, db *gorm.DB) (FileOrphanRow, error) {
	var row FileOrphanRow
	err := db.WithContext(ctx).Raw(`
		SELECT
			SUM(CASE WHEN NOT EXISTS (SELECT 1 FROM tenant t WHERE t.id = f.tenant_id) THEN 1 ELSE 0 END) AS file_no_tenant,
			SUM(CASE WHEN NOT EXISTS (SELECT 1 FROM user u WHERE u.id = f.created_by) THEN 1 ELSE 0 END) AS file_no_user
		FROM file f
	`).Scan(&row).Error
	return row, err
}

// FileCountAndSize returns the total file count and total file size.
func (dao *FileDAO) FileCountAndSize(ctx context.Context, db *gorm.DB) (int64, int64, error) {
	var row struct {
		FileCount int64 `gorm:"column:file_count"`
		FileSize  int64 `gorm:"column:file_size"`
	}
	err := db.WithContext(ctx).Model(&entity.File{}).
		Select("COUNT(*) AS file_count, COALESCE(SUM(size), 0) AS file_size").
		Scan(&row).Error
	return row.FileCount, row.FileSize, err
}

// FileTypeRow holds file type statistics.
type FileTypeRow struct {
	Type  string `gorm:"column:type"`
	Count int64  `gorm:"column:count"`
	Size  int64  `gorm:"column:size"`
}

// ListFileTypeStats returns statistics grouped by file type.
func (dao *FileDAO) ListFileTypeStats(ctx context.Context, db *gorm.DB) ([]FileTypeRow, error) {
	var rows []FileTypeRow
	err := db.WithContext(ctx).Model(&entity.File{}).
		Select("type, COUNT(*) AS count, COALESCE(SUM(size), 0) AS size").
		Group("type").
		Scan(&rows).Error
	return rows, err
}
