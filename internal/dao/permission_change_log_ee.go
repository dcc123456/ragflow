//
// Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package dao

import (
	"context"

	"ragflow/internal/entity"

	"gorm.io/gorm"
)

// PermissionChangeLogFilter narrows the global permission audit query.
type PermissionChangeLogFilter struct {
	TenantID     string
	OperatorID   string
	ActorUserID  string
	EventType    string
	ScopeType    string
	TargetType   string
	TargetID     string
	ResourceType string
	ResourceID   string
	ActionType   string
	StartTime    *int64
	EndTime      *int64
}

// ListPermissionChangeLogs returns matching audit records and their total count.
func (dao *PermissionChangeLogDAO) ListPermissionChangeLogs(ctx context.Context, db *gorm.DB, filter PermissionChangeLogFilter, offset, limit int) ([]*entity.PermissionChangeLog, int64, error) {
	query := db.WithContext(ctx).Model(&entity.PermissionChangeLog{})
	if filter.TenantID != "" {
		query = query.Where("tenant_id = ?", filter.TenantID)
	}
	if filter.OperatorID != "" {
		query = query.Where("operator_id = ?", filter.OperatorID)
	}
	if filter.ActorUserID != "" {
		query = query.Where("actor_user_id = ?", filter.ActorUserID)
	}
	if filter.EventType != "" {
		query = query.Where("event_type = ?", filter.EventType)
	}
	if filter.ScopeType != "" {
		query = query.Where("scope_type = ?", filter.ScopeType)
	}
	if filter.TargetType != "" {
		query = query.Where("target_type = ?", filter.TargetType)
	}
	if filter.TargetID != "" {
		query = query.Where("target_id = ?", filter.TargetID)
	}
	if filter.ResourceType != "" {
		query = query.Where("resource_type = ?", filter.ResourceType)
	}
	if filter.ResourceID != "" {
		query = query.Where("resource_id = ?", filter.ResourceID)
	}
	if filter.ActionType != "" {
		query = query.Where("action_type = ?", filter.ActionType)
	}
	if filter.StartTime != nil {
		query = query.Where("create_time >= ?", *filter.StartTime)
	}
	if filter.EndTime != nil {
		query = query.Where("create_time <= ?", *filter.EndTime)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	logs := make([]*entity.PermissionChangeLog, 0)
	err := query.Order("create_time DESC").Order("id DESC").Offset(offset).Limit(limit).Find(&logs).Error
	return logs, total, err
}

// PermissionChangeLogDAO reads permission audit records.
type PermissionChangeLogDAO struct{}

func NewPermissionChangeLogDAO() *PermissionChangeLogDAO {
	return &PermissionChangeLogDAO{}
}
