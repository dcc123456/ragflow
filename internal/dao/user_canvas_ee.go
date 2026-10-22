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

	"ragflow/internal/entity"

	"gorm.io/gorm"
)

// CountAllAgentCanvas counts all agent canvases.
func (dao *UserCanvasDAO) CountAllAgentCanvas(ctx context.Context, db *gorm.DB) (int64, error) {
	var total int64
	err := db.WithContext(ctx).Model(&entity.UserCanvas{}).
		Where("canvas_category = ?", "agent_canvas").
		Count(&total).Error
	return total, err
}

// CountAgentCanvasByTenantIDs counts agent canvases for the given tenants.
func (dao *UserCanvasDAO) CountAgentCanvasByTenantIDs(ctx context.Context, db *gorm.DB, tenantIDs []string) (int64, error) {
	var total int64
	err := db.WithContext(ctx).Model(&entity.UserCanvas{}).
		Where("user_id IN ? AND canvas_category = ?", tenantIDs, "agent_canvas").
		Count(&total).Error
	return total, err
}

// ListCreatedByUser lists a page of canvases created by userID in the category.
func (dao *UserCanvasDAO) ListCreatedByUser(ctx context.Context, db *gorm.DB, userID, canvasCategory string, page, pageSize int) ([]*AdminUserCanvasInfo, int64, error) {
	query := db.WithContext(ctx).Model(&entity.UserCanvas{}).
		Where("user_canvas.user_id = ?", userID).
		Where("user_canvas.canvas_category = ?", canvasCategory)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var results []*AdminUserCanvasInfo
	err := query.Select("user_canvas.id, user_canvas.tenant_id, tenant.name AS tenant_name, user_canvas.avatar, user_canvas.title, user_canvas.canvas_category, user_canvas.create_date, user_canvas.update_date").
		Joins("LEFT JOIN tenant ON tenant.id = user_canvas.tenant_id").
		Order("user_canvas.create_time ASC, user_canvas.id ASC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Scan(&results).Error
	return results, total, err
}

// AdminUserCanvasInfo contains the fields selected for administrator canvas lists.
type AdminUserCanvasInfo struct {
	ID             string     `gorm:"column:id"`
	TenantID       *string    `gorm:"column:tenant_id"`
	TenantName     *string    `gorm:"column:tenant_name"`
	Avatar         *string    `gorm:"column:avatar"`
	Title          *string    `gorm:"column:title"`
	CanvasCategory string     `gorm:"column:canvas_category"`
	CreateDate     *time.Time `gorm:"column:create_date"`
	UpdateDate     *time.Time `gorm:"column:update_date"`
}
