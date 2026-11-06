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
