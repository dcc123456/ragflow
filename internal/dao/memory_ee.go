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

// CountMemoryByTenantIDs counts memories for the given tenants.
func (dao *MemoryDAO) CountMemoryByTenantIDs(ctx context.Context, db *gorm.DB, tenantIDs []string) (int64, error) {
	var total int64
	err := db.WithContext(ctx).Model(&entity.Memory{}).
		Where("tenant_id IN ?", tenantIDs).
		Count(&total).Error
	return total, err
}
