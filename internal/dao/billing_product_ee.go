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

type BillingProductDAO struct{}

func NewBillingProductDAO() *BillingProductDAO {
	return &BillingProductDAO{}
}

type ProductRow struct {
	Name                     string `gorm:"column:name"`
	QuotaStorage             int64  `gorm:"column:quota_storage"`
	QuotaApps                int    `gorm:"column:quota_apps"`
	APIRequestLimitPerMinute *int64 `gorm:"column:api_request_limit_per_minute"`
}

func (dao *BillingProductDAO) ListBillingProducts(ctx context.Context, db *gorm.DB) ([]ProductRow, error) {
	var products []ProductRow
	err := db.WithContext(ctx).Table("billing_product").
		Select("name, quota_storage, quota_apps, api_request_limit_per_minute").
		Order("priority ASC, version DESC").
		Scan(&products).Error
	return products, err
}

// GetFreeQuotaStorage returns the storage quota of the latest "Free" billing product.
func (dao *BillingProductDAO) GetFreeQuotaStorage(ctx context.Context, db *gorm.DB) int64 {
	var freeQuotaStorage int64
	if err := db.WithContext(ctx).Model(&entity.BillingProduct{}).
		Select("quota_storage").
		Where("name = ?", "Free").
		Order("version DESC").
		Limit(1).
		Scan(&freeQuotaStorage).Error; err != nil {
		freeQuotaStorage = 0
	}
	return freeQuotaStorage
}
