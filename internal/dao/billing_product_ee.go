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
	"gorm.io/gorm/clause"
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

// GetLatestByName returns the newest version of a product by name.
func (dao *BillingProductDAO) GetLatestByName(ctx context.Context, db *gorm.DB, name string) (*entity.BillingProduct, error) {
	var product entity.BillingProduct
	err := db.WithContext(ctx).
		Where("name = ?", name).
		Order("version DESC").
		Limit(1).
		Take(&product).Error
	if err != nil {
		return nil, err
	}
	return &product, nil
}

// UpsertProduct inserts a product row or updates it by ID.
func (dao *BillingProductDAO) UpsertProduct(ctx context.Context, db *gorm.DB, product *entity.BillingProduct) error {
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		UpdateAll: true,
	}).Create(product).Error
}

// GetPricePointByName returns the latest price point for a product name.
func (dao *BillingProductDAO) GetPricePointByName(ctx context.Context, db *gorm.DB, name string) (*entity.BillingPricePoint, error) {
	var point entity.BillingPricePoint
	err := db.WithContext(ctx).
		Where("product_name = ?", name).
		Order("effective_time DESC").
		Limit(1).
		Take(&point).Error
	if err != nil {
		return nil, err
	}
	return &point, nil
}

// UpsertPricePoint inserts a price point row or updates it by ID.
func (dao *BillingProductDAO) UpsertPricePoint(ctx context.Context, db *gorm.DB, point *entity.BillingPricePoint) error {
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		UpdateAll: true,
	}).Create(point).Error
}

// GetPurchasedOverview returns the purchased amount for a tenant and product.
func (dao *BillingProductDAO) GetPurchasedOverview(ctx context.Context, db *gorm.DB, tenantID, productName string) (*entity.BillingPurchasedProductOverview, error) {
	var row entity.BillingPurchasedProductOverview
	err := db.WithContext(ctx).
		Where("tenant_id = ? AND product_name = ?", tenantID, productName).
		Limit(1).
		Take(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// UpdatePurchasedQuantity applies delta without allowing a negative balance.
func (dao *BillingProductDAO) UpdatePurchasedQuantity(ctx context.Context, db *gorm.DB, tenantID, productName string, delta int64) (bool, error) {
	result := db.WithContext(ctx).Model(&entity.BillingPurchasedProductOverview{}).
		Where("tenant_id = ? AND product_name = ? AND quantity + ? >= 0", tenantID, productName, delta).
		UpdateColumn("quantity", gorm.Expr("quantity + ?", delta))
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// ListLatestByType returns the newest version of every product of a type.
func (dao *BillingProductDAO) ListLatestByType(ctx context.Context, db *gorm.DB, productType string) ([]entity.BillingProduct, error) {
	var rows []entity.BillingProduct
	err := db.WithContext(ctx).
		Where("product_type = ?", productType).
		Order("name ASC, version DESC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	latest := make([]entity.BillingProduct, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		if seen[row.Name] {
			continue
		}
		seen[row.Name] = true
		latest = append(latest, row)
	}
	return latest, nil
}
