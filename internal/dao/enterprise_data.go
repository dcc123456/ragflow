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

type EnterpriseDataDAO struct{}

func NewEnterpriseDataDAO() *EnterpriseDataDAO {
	return &EnterpriseDataDAO{}
}

type StorageAggRow struct {
	TotalFileSize int64 `gorm:"column:total_file_size"`
	TotalDocSize  int64 `gorm:"column:total_doc_size"`
}

func (dao *EnterpriseDataDAO) GetStorageSummary() (StorageAggRow, error) {
	var row StorageAggRow
	err := DB.Raw(`
		SELECT
			(SELECT COALESCE(SUM(size), 0) FROM file) AS total_file_size,
			(SELECT COALESCE(SUM(size), 0) FROM document) AS total_doc_size
	`).Scan(&row).Error
	return row, err
}

type TaskAggRow struct {
	Total     int64 `gorm:"column:total"`
	Pending   int64 `gorm:"column:pending"`
	Running   int64 `gorm:"column:running"`
	Completed int64 `gorm:"column:completed"`
	Failed    int64 `gorm:"column:failed"`
}

func (dao *EnterpriseDataDAO) GetTaskSummary() (TaskAggRow, error) {
	var row TaskAggRow
	err := DB.Raw(`
		SELECT
			COUNT(*) AS total,
			SUM(CASE WHEN progress = 0 THEN 1 ELSE 0 END) AS pending,
			SUM(CASE WHEN progress > 0 AND progress < 1 THEN 1 ELSE 0 END) AS running,
			SUM(CASE WHEN progress = 1 THEN 1 ELSE 0 END) AS completed,
			SUM(CASE WHEN progress = -1 THEN 1 ELSE 0 END) AS failed
		FROM task
	`).Scan(&row).Error
	return row, err
}

type KBOrphanRow struct {
	KbNoTenant int64 `gorm:"column:kb_no_tenant"`
	KbNoUser   int64 `gorm:"column:kb_no_user"`
}

func (dao *EnterpriseDataDAO) GetKBOrphans() (KBOrphanRow, error) {
	var row KBOrphanRow
	err := DB.Raw(`
		SELECT
			SUM(CASE WHEN NOT EXISTS (SELECT 1 FROM user_tenant ut WHERE ut.tenant_id = kb.tenant_id) THEN 1 ELSE 0 END) AS kb_no_tenant,
			SUM(CASE WHEN NOT EXISTS (SELECT 1 FROM user u WHERE u.id = kb.created_by) THEN 1 ELSE 0 END) AS kb_no_user
		FROM knowledgebase kb
	`).Scan(&row).Error
	return row, err
}

type DocOrphanRow struct {
	DocNoKb       int64 `gorm:"column:doc_no_kb"`
	DocNoUser     int64 `gorm:"column:doc_no_user"`
	DocKbNoTenant int64 `gorm:"column:doc_kb_no_tenant"`
}

func (dao *EnterpriseDataDAO) GetDocOrphans() (DocOrphanRow, error) {
	var row DocOrphanRow
	err := DB.Raw(`
		SELECT
			SUM(CASE WHEN kb.id IS NULL THEN 1 ELSE 0 END) AS doc_no_kb,
			SUM(CASE WHEN u.id IS NULL THEN 1 ELSE 0 END) AS doc_no_user,
			SUM(CASE WHEN kb.id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM user_tenant ut WHERE ut.tenant_id = kb.tenant_id) THEN 1 ELSE 0 END) AS doc_kb_no_tenant
		FROM document d
		LEFT JOIN knowledgebase kb ON d.kb_id = kb.id
		LEFT JOIN user u ON d.created_by = u.id
	`).Scan(&row).Error
	return row, err
}

func (dao *EnterpriseDataDAO) GetTaskOrphans() (int64, error) {
	var count int64
	err := DB.Raw(`
		SELECT COUNT(*) FROM task t WHERE NOT EXISTS (SELECT 1 FROM document d WHERE d.id = t.doc_id)
	`).Scan(&count).Error
	return count, err
}

type FileOrphanRow struct {
	FileNoTenant int64 `gorm:"column:file_no_tenant"`
	FileNoUser   int64 `gorm:"column:file_no_user"`
}

func (dao *EnterpriseDataDAO) GetFileOrphans() (FileOrphanRow, error) {
	var row FileOrphanRow
	err := DB.Raw(`
		SELECT
			SUM(CASE WHEN NOT EXISTS (SELECT 1 FROM user_tenant ut WHERE ut.tenant_id = f.tenant_id) THEN 1 ELSE 0 END) AS file_no_tenant,
			SUM(CASE WHEN NOT EXISTS (SELECT 1 FROM user u WHERE u.id = f.created_by) THEN 1 ELSE 0 END) AS file_no_user
		FROM file f
	`).Scan(&row).Error
	return row, err
}

type UTOrphanRow struct {
	UTNoUser         int64 `gorm:"column:ut_no_user"`
	UTNoTenantRecord int64 `gorm:"column:ut_no_tenant_record"`
}

func (dao *EnterpriseDataDAO) GetUserTenantOrphans() (UTOrphanRow, error) {
	var row UTOrphanRow
	err := DB.Raw(`
		SELECT
			SUM(CASE WHEN NOT EXISTS (SELECT 1 FROM user u WHERE u.id = ut.user_id) THEN 1 ELSE 0 END) AS ut_no_user,
			SUM(CASE WHEN NOT EXISTS (SELECT 1 FROM tenant t WHERE t.id = ut.tenant_id) THEN 1 ELSE 0 END) AS ut_no_tenant_record
		FROM user_tenant ut
	`).Scan(&row).Error
	return row, err
}
