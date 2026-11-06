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
	"errors"
	"time"

	"ragflow/internal/entity"

	"gorm.io/gorm"
)

// BillingOrderDAO is the data access object for billing_payment_order.
type BillingOrderDAO struct{}

// NewBillingOrderDAO creates a billing order DAO.
func NewBillingOrderDAO() *BillingOrderDAO {
	return &BillingOrderDAO{}
}

// GetByOrderID returns the order with the given Stripe order id.
func (dao *BillingOrderDAO) GetByOrderID(ctx context.Context, db *gorm.DB, orderID string) (*entity.BillingPaymentOrder, error) {
	var order entity.BillingPaymentOrder
	err := db.WithContext(ctx).Where("order_id = ?", orderID).Limit(1).Take(&order).Error
	if err != nil {
		return nil, err
	}
	return &order, nil
}

// UpsertByOrderID inserts or updates an order keyed by order_id.
// order_id has no unique constraint in the shared schema (the Python model
// only indexes it), so this is a read-then-write inside the caller's
// transaction rather than an ON CONFLICT upsert.
func (dao *BillingOrderDAO) UpsertByOrderID(ctx context.Context, db *gorm.DB, order *entity.BillingPaymentOrder) error {
	existing, err := dao.GetByOrderID(ctx, db, order.OrderID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return db.WithContext(ctx).Create(order).Error
		}
		return err
	}
	order.ID = existing.ID
	return db.WithContext(ctx).Save(order).Error
}

// ListByTenant returns orders of a tenant ordered by creation time descending.
func (dao *BillingOrderDAO) ListByTenant(ctx context.Context, db *gorm.DB, tenantID string, limit int) ([]entity.BillingPaymentOrder, error) {
	var orders []entity.BillingPaymentOrder
	query := db.WithContext(ctx).Where("tenant_id = ?", tenantID).Order("order_created_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&orders).Error
	return orders, err
}

// ListByTenantPage returns one page of a tenant's orders ordered by creation
// time descending together with the total number of matching orders.
func (dao *BillingOrderDAO) ListByTenantPage(ctx context.Context, db *gorm.DB, tenantID string, start, end *time.Time, offset, limit int) ([]entity.BillingPaymentOrder, int64, error) {
	filter := func() *gorm.DB {
		query := db.WithContext(ctx).Model(&entity.BillingPaymentOrder{}).Where("tenant_id = ?", tenantID)
		if start != nil {
			query = query.Where("order_created_at >= ?", *start)
		}
		if end != nil {
			query = query.Where("order_created_at <= ?", *end)
		}
		return query
	}
	var total int64
	if err := filter().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var orders []entity.BillingPaymentOrder
	err := filter().Order("order_created_at DESC").Offset(offset).Limit(limit).Find(&orders).Error
	return orders, total, err
}

// ListPaidByTenantSince returns paid orders created at or after since.
func (dao *BillingOrderDAO) ListPaidByTenantSince(ctx context.Context, db *gorm.DB, tenantID string, since int64) ([]entity.BillingPaymentOrder, error) {
	var orders []entity.BillingPaymentOrder
	err := db.WithContext(ctx).
		Where("tenant_id = ? AND paid = ? AND order_created_at >= ?", tenantID, true, since).
		Order("order_created_at ASC").
		Find(&orders).Error
	return orders, err
}
