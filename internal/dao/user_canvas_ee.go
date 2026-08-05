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
