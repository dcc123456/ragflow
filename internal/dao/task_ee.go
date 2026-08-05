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
	"time"

	"gorm.io/gorm"
)

// TaskAggRow holds aggregated task statistics.
type TaskAggRow struct {
	AvgDuration float64 `gorm:"column:avg_duration"`
	Total       int64   `gorm:"column:total"`
	Pending     int64   `gorm:"column:pending"`
	Running     int64   `gorm:"column:running"`
	Completed   int64   `gorm:"column:completed"`
	Failed      int64   `gorm:"column:failed"`
	Retried     int64   `gorm:"column:retried"`
	Abandoned   int64   `gorm:"column:abandoned"`
}

// TaskAggByStatus returns task statistics aggregated by status.
func (dao *TaskDAO) TaskAggByStatus(ctx context.Context, db *gorm.DB) (TaskAggRow, error) {
	var row TaskAggRow
	err := db.WithContext(ctx).Raw(`
		SELECT
			COALESCE(AVG(CASE WHEN progress = 1 THEN process_duration ELSE NULL END), 0) AS avg_duration,
			COUNT(*) AS total,
			SUM(CASE WHEN progress = 0 THEN 1 ELSE 0 END) AS pending,
			SUM(CASE WHEN progress > 0 AND progress < 1 THEN 1 ELSE 0 END) AS running,
			SUM(CASE WHEN progress = 1 THEN 1 ELSE 0 END) AS completed,
			SUM(CASE WHEN progress < 0 THEN 1 ELSE 0 END) AS failed,
			SUM(CASE WHEN retry_count > 0 THEN 1 ELSE 0 END) AS retried,
			SUM(CASE WHEN retry_count >= 3 THEN 1 ELSE 0 END) AS abandoned
		FROM task
	`).Scan(&row).Error
	return row, err
}

// TaskTypeRow holds task type counts.
type TaskTypeRow struct {
	TaskType string `gorm:"column:task_type"`
	Count    int64  `gorm:"column:count"`
}

// GetTaskTypeCounts returns task counts grouped by task type.
func (dao *TaskDAO) GetTaskTypeCounts(ctx context.Context, db *gorm.DB) ([]TaskTypeRow, error) {
	var rows []TaskTypeRow
	err := db.WithContext(ctx).Raw(`
		SELECT COALESCE(task_type, '') AS task_type, COUNT(*) AS count
		FROM task
		GROUP BY task_type
	`).Scan(&rows).Error
	return rows, err
}

// TaskCountRow holds task counts by status.
type TaskCountRow struct {
	Total     int64 `gorm:"column:total"`
	Pending   int64 `gorm:"column:pending"`
	Running   int64 `gorm:"column:running"`
	Completed int64 `gorm:"column:completed"`
	Failed    int64 `gorm:"column:failed"`
}

// TaskCountByStatus returns task counts grouped by status.
func (dao *TaskDAO) TaskCountByStatus(ctx context.Context, db *gorm.DB) (TaskCountRow, error) {
	var row TaskCountRow
	err := db.WithContext(ctx).Raw(`
		SELECT
			COUNT(*) AS total,
			SUM(CASE WHEN progress = 0 THEN 1 ELSE 0 END) AS pending,
			SUM(CASE WHEN progress > 0 AND progress < 1 THEN 1 ELSE 0 END) AS running,
			SUM(CASE WHEN progress = 1 THEN 1 ELSE 0 END) AS completed,
			SUM(CASE WHEN progress < 0 THEN 1 ELSE 0 END) AS failed
		FROM task
	`).Scan(&row).Error
	return row, err
}

// GetTaskOrphans returns the count of tasks whose document no longer exists.
func (dao *TaskDAO) GetTaskOrphans(ctx context.Context, db *gorm.DB) (int64, error) {
	var total int64
	err := db.WithContext(ctx).Raw(`
		SELECT COUNT(*) FROM task t
		WHERE NOT EXISTS (SELECT 1 FROM document d WHERE d.id = t.doc_id)
	`).Scan(&total).Error
	return total, err
}

// TaskRow holds a task row for listing.
type TaskRow struct {
	ID              string     `gorm:"column:id"`
	DocName         *string    `gorm:"column:doc_name"`
	BeginAt         *time.Time `gorm:"column:begin_at"`
	Progress        float64    `gorm:"column:progress"`
	ProcessDuration float64    `gorm:"column:process_duration"`
	CreateTime      *int64     `gorm:"column:create_time"`
}

// ListTasksByTenantIDs returns tasks for the given tenants, optionally filtered by a status condition.
func (dao *TaskDAO) ListTasksByTenantIDs(ctx context.Context, db *gorm.DB, tenantIDs []string, condition string) ([]TaskRow, error) {
	var rows []TaskRow
	sql := `
		SELECT t.id AS id, d.name AS doc_name, t.begin_at AS begin_at,
			t.progress AS progress, t.process_duration AS process_duration, t.create_time AS create_time
		FROM task t
		INNER JOIN document d ON d.id = t.doc_id
		INNER JOIN knowledgebase kb ON kb.id = d.kb_id
		WHERE kb.tenant_id IN ?
	`
	args := []interface{}{tenantIDs}
	if condition != "" {
		sql += " AND " + condition
	}
	sql += " ORDER BY t.create_time DESC"
	err := db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error
	return rows, err
}
