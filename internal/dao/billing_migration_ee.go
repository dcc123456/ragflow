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
	"encoding/json"

	"ragflow/internal/entity"

	"gorm.io/gorm"
)

// BillingMigrationDAO converges billing schema created by older releases.
type BillingMigrationDAO struct{}

// NewBillingMigrationDAO creates a billing migration DAO.
func NewBillingMigrationDAO() *BillingMigrationDAO {
	return &BillingMigrationDAO{}
}

// EnsureIndexes adds the five shared unique indexes when absent.
// It never renames or drops an existing index: the shared-schema rule
// reconciles by column set, not by name.
func (dao *BillingMigrationDAO) EnsureIndexes(ctx context.Context, db *gorm.DB) error {
	indexes := []struct {
		table   string
		name    string
		columns []string
	}{
		{"billing_subscription", "uni_billing_subscription_tenant", []string{"tenant_id"}},
		{"billing_point_account", "uni_billing_point_account_tenant", []string{"tenant_id"}},
		{"billing_webhook_event", "uni_billing_webhook_event_event", []string{"event_id"}},
		{"billing_point_ledger", "uni_billing_point_ledger_idem", []string{"idempotency_key"}},
		{"billing_point_hold", "uni_billing_point_hold_idem", []string{"idempotency_key"}},
	}
	for _, index := range indexes {
		exists, err := hasUniqueIndex(ctx, db, index.table, index.columns)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if err := addUniqueIndex(ctx, db, index.table, index.name, index.columns); err != nil {
			return err
		}
	}
	return nil
}

// MigrateLegacySchema ports the column changes shipped by the Python billing
// migrations so a Go deployment can take over an existing database.
func (dao *BillingMigrationDAO) MigrateLegacySchema(ctx context.Context, db *gorm.DB) error {
	columns := []struct {
		table  string
		column string
		ddl    string
	}{
		{"billing_product", "quota_points", "BIGINT NULL"},
		{"billing_product", "priority", "INT NOT NULL DEFAULT 0"},
		{"billing_product", "api_request_limit_per_minute", "BIGINT NULL DEFAULT 500"},
		{"billing_pricepoint", "price_amount", "INT NULL"},
		{"billing_pricepoint", "price_currency", "VARCHAR(3) NULL"},
		{"billing_pricepoint", "included_free_amount", "INT NULL"},
		{"billing_pricepoint", "unit", "VARCHAR(32) NULL"},
		{"billing_pricepoint", "unit_quantity", "INT NULL"},
		{"billing_pricepoint", "consuming_point_amount", "INT NULL"},
		{"billing_subscription", "addon_subscription_item_id", "VARCHAR(255) NULL"},
		{"billing_subscription", "addon_storage_bytes", "BIGINT NULL"},
		{"billing_subscription", "target_storage_bytes", "BIGINT NULL"},
		{"billing_subscription", "target_plan_name", "VARCHAR(255) NULL"},
		{"billing_payment_order", "product_ids", "TEXT NULL"},
		{"billing_payment_order", "product_names", "TEXT NULL"},
		{"billing_payment_order", "product_quantities", "TEXT NULL"},
		{"billing_payment_order", "product_amount_cents", "TEXT NULL"},
		{"billing_payment_order", "price_ids", "TEXT NULL"},
		{"billing_point_ledger", "source", "VARCHAR(16) NOT NULL DEFAULT 'plan'"},
		{"billing_point_hold", "plan_points", "BIGINT NOT NULL DEFAULT 0"},
		{"billing_point_hold", "addon_points", "BIGINT NOT NULL DEFAULT 0"},
	}
	for _, column := range columns {
		exists, err := columnExists(ctx, db, column.table, column.column)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if err := addColumnIfNotExists(ctx, db, column.table, column.column, column.ddl); err != nil {
			return err
		}
	}

	if err := dao.backfillOrderJSONArrays(ctx, db); err != nil {
		return err
	}
	if err := dao.mergeLegacyTargetStorage(ctx, db); err != nil {
		return err
	}
	if err := dropColumnIfExists(ctx, db, "billing_point_account", "available_points"); err != nil {
		return err
	}
	if err := dropColumnIfExists(ctx, db, "billing_product", "usage_stat_type"); err != nil {
		return err
	}
	return nil
}

type legacyOrderRow struct {
	ID             string
	ProductID      *string
	ProductName    *string
	PriceID        *string
	AmountCents    *int64
	ProductIDs     *string
	ProductNames   *string
	PriceIDs       *string
	ProductAmounts *string
}

func (dao *BillingMigrationDAO) backfillOrderJSONArrays(ctx context.Context, db *gorm.DB) error {
	hasProductID, err := columnExists(ctx, db, "billing_payment_order", "product_id")
	if err != nil {
		return err
	}
	hasProductName, err := columnExists(ctx, db, "billing_payment_order", "product_name")
	if err != nil {
		return err
	}
	hasPriceID, err := columnExists(ctx, db, "billing_payment_order", "price_id")
	if err != nil {
		return err
	}
	if !hasProductID && !hasProductName && !hasPriceID {
		return nil
	}

	var rows []legacyOrderRow
	selects := []string{"id", "amount_cents", "product_ids", "product_names", "price_ids", "product_amount_cents"}
	if hasProductID {
		selects = append(selects, "product_id")
	}
	if hasProductName {
		selects = append(selects, "product_name")
	}
	if hasPriceID {
		selects = append(selects, "price_id")
	}
	if err := db.WithContext(ctx).Table("billing_payment_order").Select(selects).Find(&rows).Error; err != nil {
		return err
	}

	for _, row := range rows {
		if row.ProductIDs != nil && *row.ProductIDs != "" {
			continue
		}
		productID := billingDerefString(row.ProductID)
		productName := billingDerefString(row.ProductName)
		priceID := billingDerefString(row.PriceID)
		if productID == "" && productName == "" && priceID == "" {
			continue
		}
		amount := int64(0)
		if row.AmountCents != nil {
			amount = *row.AmountCents
		}
		updates := map[string]interface{}{
			"product_ids":          billingJSONArray([]string{productID}),
			"product_names":        billingJSONArray([]string{productName}),
			"product_quantities":   billingJSONArray([]int64{1}),
			"product_amount_cents": billingJSONArray([]int64{amount}),
			"price_ids":            billingJSONArray([]string{priceID}),
		}
		if err := db.WithContext(ctx).Table("billing_payment_order").Where("id = ?", row.ID).Updates(updates).Error; err != nil {
			return err
		}
	}
	return nil
}

func (dao *BillingMigrationDAO) mergeLegacyTargetStorage(ctx context.Context, db *gorm.DB) error {
	exists, err := columnExists(ctx, db, "billing_subscription", "target_quantity_bytes")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if err := db.WithContext(ctx).Exec(
		"UPDATE billing_subscription SET target_storage_bytes = target_quantity_bytes WHERE target_storage_bytes IS NULL AND target_quantity_bytes IS NOT NULL",
	).Error; err != nil {
		return err
	}
	return dropColumnIfExists(ctx, db, "billing_subscription", "target_quantity_bytes")
}

func billingDerefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func billingJSONArray(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		return "[]"
	}
	return string(payload)
}

// billingDataModelsEE lists the billing tables this build owns. Python keeps
// reading them, so column sets must stay identical.
func billingDataModelsEE() []interface{} {
	return []interface{}{
		&entity.BillingProduct{},
		&entity.BillingPricePoint{},
		&entity.BillingPurchasedProductOverview{},
		&entity.BillingPaymentOrder{},
		&entity.BillingWebhookEvent{},
		&entity.BillingSubscription{},
		&entity.BillingPointAccount{},
		&entity.BillingPointLedger{},
		&entity.BillingPointHold{},
	}
}

// migrateBillingSchemaEE converges legacy billing schemas and their indexes.
func migrateBillingSchemaEE(ctx context.Context, db *gorm.DB) error {
	migration := NewBillingMigrationDAO()
	if err := migration.MigrateLegacySchema(ctx, db); err != nil {
		return err
	}
	return migration.EnsureIndexes(ctx, db)
}
