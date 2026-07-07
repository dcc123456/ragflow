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
	"time"
)

type EnterpriseTaskDAO struct{}

func NewEnterpriseTaskDAO() *EnterpriseTaskDAO {
	return &EnterpriseTaskDAO{}
}

type TaskAggSummaryRow struct {
	Total       int64   `gorm:"column:total"`
	Pending     int64   `gorm:"column:pending"`
	Running     int64   `gorm:"column:running"`
	Completed   int64   `gorm:"column:completed"`
	Failed      int64   `gorm:"column:failed"`
	AvgDuration float64 `gorm:"column:avg_duration"`
	Retried     int64   `gorm:"column:retried"`
	Abandoned   int64   `gorm:"column:abandoned"`
}

func (dao *EnterpriseTaskDAO) GetTaskAggSummary() (TaskAggSummaryRow, error) {
	var row TaskAggSummaryRow
	err := DB.Raw(`
		SELECT
			COUNT(*) AS total,
			SUM(CASE WHEN progress = 0 THEN 1 ELSE 0 END) AS pending,
			SUM(CASE WHEN progress > 0 AND progress < 1 THEN 1 ELSE 0 END) AS running,
			SUM(CASE WHEN progress = 1 THEN 1 ELSE 0 END) AS completed,
			SUM(CASE WHEN progress = -1 THEN 1 ELSE 0 END) AS failed,
			COALESCE(AVG(CASE WHEN progress = 1 THEN process_duration ELSE NULL END), 0) AS avg_duration,
			SUM(CASE WHEN retry_count > 0 THEN 1 ELSE 0 END) AS retried,
			SUM(CASE WHEN retry_count >= 3 THEN 1 ELSE 0 END) AS abandoned
		FROM task
	`).Scan(&row).Error
	return row, err
}

type TaskTypeCountRow struct {
	TaskType string `gorm:"column:task_type"`
	Count    int64  `gorm:"column:count"`
}

func (dao *EnterpriseTaskDAO) GetTaskTypeCounts() ([]TaskTypeCountRow, error) {
	var rows []TaskTypeCountRow
	err := DB.Raw(`
		SELECT task_type, COUNT(*) AS count
		FROM task
		GROUP BY task_type
		ORDER BY count DESC
	`).Scan(&rows).Error
	return rows, err
}

type TaskDetailRow struct {
	ID              string     `gorm:"column:id"`
	DocID           string     `gorm:"column:doc_id"`
	Progress        float64    `gorm:"column:progress"`
	ProcessDuration float64    `gorm:"column:process_duration"`
	BeginAt         *time.Time `gorm:"column:begin_at"`
	TaskType        string     `gorm:"column:task_type"`
	RetryCount      int64      `gorm:"column:retry_count"`
	CreateTime      *int64     `gorm:"column:create_time"`
	DocName         *string    `gorm:"column:doc_name"`
}

func (dao *EnterpriseTaskDAO) ListTasksByTenantIDs(tenantIDs []string, condition string) ([]TaskDetailRow, error) {
	query := DB.Table("task t").
		Select(`t.id, t.doc_id, t.progress, t.process_duration,
			t.begin_at, t.task_type, t.retry_count, t.create_time,
			d.name AS doc_name`).
		Joins("JOIN document d ON t.doc_id = d.id").
		Joins("JOIN knowledgebase k ON d.kb_id = k.id").
		Where("k.tenant_id IN ?", tenantIDs)

	if condition != "" {
		query = query.Where(condition)
	}

	query = query.Order("t.create_time DESC")

	var rows []TaskDetailRow
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
