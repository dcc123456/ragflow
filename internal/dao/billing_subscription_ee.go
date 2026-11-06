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

type BillingSubscriptionDAO struct{}

func NewBillingSubscriptionDAO() *BillingSubscriptionDAO {
	return &BillingSubscriptionDAO{}
}

// CountActiveSubscriptions counts all active billing subscriptions.
func (dao *BillingSubscriptionDAO) CountActiveSubscriptions(ctx context.Context, db *gorm.DB) (int64, error) {
	var total int64
	err := db.WithContext(ctx).Model(&entity.BillingSubscription{}).
		Where("subscription_status = ?", "active").
		Count(&total).Error
	return total, err
}

type QuotaUsageRow struct {
	PlanName       string  `gorm:"column:plan_name"`
	UserCount      int64   `gorm:"column:user_count"`
	AvgStorageUsed float64 `gorm:"column:avg_storage_used"`
	AvgAppsUsed    float64 `gorm:"column:avg_apps_used"`
	TotalAPITokens int64   `gorm:"column:total_api_tokens"`
}

func (dao *BillingSubscriptionDAO) ListQuotaUsage(ctx context.Context, db *gorm.DB) ([]QuotaUsageRow, error) {
	var rows []QuotaUsageRow
	err := db.WithContext(ctx).Raw(`
		WITH active_users AS (
			SELECT DISTINCT u.id, sub.plan_name
			FROM user u
			INNER JOIN billing_subscription sub
				ON u.id = sub.tenant_id AND sub.subscription_status = 'active'
			WHERE u.status != '0' AND u.is_anonymous = '0'
		),
		user_counts AS (
			SELECT plan_name, COUNT(*) AS user_count
			FROM active_users GROUP BY plan_name
		),
		storage_stats AS (
			SELECT au.plan_name, AVG(COALESCE(ds.total_size, 0)) AS avg_storage_used
			FROM active_users au
			LEFT JOIN (
				SELECT kb.tenant_id, SUM(d.size) AS total_size
				FROM knowledgebase kb
				INNER JOIN document d ON d.kb_id = kb.id
				WHERE kb.status = '1'
				GROUP BY kb.tenant_id
			) ds ON au.id = ds.tenant_id
			GROUP BY au.plan_name
		),
		app_stats AS (
			SELECT au.plan_name, AVG(COALESCE(uc.app_count, 0)) AS avg_apps_used
			FROM active_users au
			LEFT JOIN (
				SELECT user_id, COUNT(*) AS app_count
				FROM user_canvas
				WHERE canvas_category = 'agent_canvas'
				GROUP BY user_id
			) uc ON au.id = uc.user_id
			GROUP BY au.plan_name
		),
		token_stats AS (
			SELECT au.plan_name, SUM(COALESCE(at.token_count, 0)) AS total_api_tokens
			FROM active_users au
			LEFT JOIN (
				SELECT tenant_id, COUNT(*) AS token_count
				FROM api_token
				GROUP BY tenant_id
			) at ON au.id = at.tenant_id
			GROUP BY au.plan_name
		)
		SELECT
			uc.plan_name,
			uc.user_count,
			s.avg_storage_used,
			a.avg_apps_used,
			t.total_api_tokens
		FROM user_counts uc
		LEFT JOIN storage_stats s ON uc.plan_name = s.plan_name
		LEFT JOIN app_stats a ON uc.plan_name = a.plan_name
		LEFT JOIN token_stats t ON uc.plan_name = t.plan_name
		ORDER BY uc.plan_name
	`).Scan(&rows).Error
	return rows, err
}

// GetByTenantID returns the main subscription of a tenant.
func (dao *BillingSubscriptionDAO) GetByTenantID(ctx context.Context, db *gorm.DB, tenantID string) (*entity.BillingSubscription, error) {
	var subscription entity.BillingSubscription
	err := db.WithContext(ctx).Where("tenant_id = ?", tenantID).Limit(1).Take(&subscription).Error
	if err != nil {
		return nil, err
	}
	return &subscription, nil
}

// GetByCustomerID returns the subscription that belongs to a Stripe customer.
func (dao *BillingSubscriptionDAO) GetByCustomerID(ctx context.Context, db *gorm.DB, customerID string) (*entity.BillingSubscription, error) {
	var subscription entity.BillingSubscription
	err := db.WithContext(ctx).Where("customer_id = ?", customerID).Limit(1).Take(&subscription).Error
	if err != nil {
		return nil, err
	}
	return &subscription, nil
}

// UpsertSubscription inserts or updates the tenant's main subscription by tenant_id.
func (dao *BillingSubscriptionDAO) UpsertSubscription(ctx context.Context, db *gorm.DB, subscription *entity.BillingSubscription) error {
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "tenant_id"}},
		UpdateAll: true,
	}).Create(subscription).Error
}

// UpdateStorageAddon writes the storage add-on columns taken from a Stripe
// subscription response. Only these columns are touched: a webhook may have
// converged the plan, status or period of the same row in the meantime, and
// saving a whole row snapshot read before the Stripe call would roll that back.
func (dao *BillingSubscriptionDAO) UpdateStorageAddon(ctx context.Context, db *gorm.DB, tenantID string, itemID *string, addonBytes int64, targetBytes *int64) error {
	return db.WithContext(ctx).Model(&entity.BillingSubscription{}).
		Where("tenant_id = ?", tenantID).
		Updates(map[string]interface{}{
			"addon_subscription_item_id": itemID,
			"addon_storage_bytes":        addonBytes,
			"target_storage_bytes":       targetBytes,
		}).Error
}

// UpdatePendingStorageTarget records the scheduled storage target, or clears it
// when targetBytes is nil. The active add-on is left untouched: it is owned by
// webhook convergence. clearScheduleID drops a schedule id that no longer backs
// a pending change.
func (dao *BillingSubscriptionDAO) UpdatePendingStorageTarget(ctx context.Context, db *gorm.DB, tenantID string, targetBytes *int64, clearScheduleID bool) error {
	updates := map[string]interface{}{"target_storage_bytes": targetBytes}
	if clearScheduleID {
		updates["schedule_id"] = nil
	}
	return db.WithContext(ctx).Model(&entity.BillingSubscription{}).
		Where("tenant_id = ?", tenantID).
		Updates(updates).Error
}

// UpdatePendingPlanChange records the plan that takes effect at the period end
// together with the schedule that applies it. Only these two columns are
// written: the row snapshot the caller holds predates the Stripe call, so
// saving it whole would roll back a concurrent webhook convergence.
func (dao *BillingSubscriptionDAO) UpdatePendingPlanChange(ctx context.Context, db *gorm.DB, tenantID string, targetPlan, scheduleID *string) error {
	return db.WithContext(ctx).Model(&entity.BillingSubscription{}).
		Where("tenant_id = ?", tenantID).
		Updates(map[string]interface{}{
			"target_plan_name": targetPlan,
			"schedule_id":      scheduleID,
		}).Error
}

// UpdatePendingTrialFallback records the period-end fallback to Trial. The
// schedule was released before cancelling, and a pending storage target can no
// longer apply, so both are cleared in the same column-scoped write.
func (dao *BillingSubscriptionDAO) UpdatePendingTrialFallback(ctx context.Context, db *gorm.DB, tenantID, targetPlan string) error {
	return db.WithContext(ctx).Model(&entity.BillingSubscription{}).
		Where("tenant_id = ?", tenantID).
		Updates(map[string]interface{}{
			"target_plan_name":     targetPlan,
			"schedule_id":          nil,
			"target_storage_bytes": nil,
		}).Error
}

// GetStorageStateForTenant returns the raw add-on and pending target byte
// values, preserving NULL so callers can distinguish "no pending change" from
// an explicit zero target.
func (dao *BillingSubscriptionDAO) GetStorageStateForTenant(ctx context.Context, db *gorm.DB, tenantID string) (*int64, *int64, error) {
	var row struct {
		AddonStorageBytes  *int64
		TargetStorageBytes *int64
	}
	err := db.WithContext(ctx).Model(&entity.BillingSubscription{}).
		Select("addon_storage_bytes, target_storage_bytes").
		Where("tenant_id = ?", tenantID).
		Limit(1).
		Scan(&row).Error
	if err != nil {
		return nil, nil, err
	}
	return row.AddonStorageBytes, row.TargetStorageBytes, nil
}

// GetStorageBytesForTenant returns (addon bytes, target bytes) of a tenant.
func (dao *BillingSubscriptionDAO) GetStorageBytesForTenant(ctx context.Context, db *gorm.DB, tenantID string) (int64, int64, error) {
	var row struct {
		AddonStorageBytes  *int64
		TargetStorageBytes *int64
	}
	err := db.WithContext(ctx).Model(&entity.BillingSubscription{}).
		Select("addon_storage_bytes, target_storage_bytes").
		Where("tenant_id = ?", tenantID).
		Limit(1).
		Scan(&row).Error
	if err != nil {
		return 0, 0, err
	}
	var addon, target int64
	if row.AddonStorageBytes != nil {
		addon = *row.AddonStorageBytes
	}
	if row.TargetStorageBytes != nil {
		target = *row.TargetStorageBytes
	}
	return addon, target, nil
}

// ListScheduledDowngrades returns subscriptions with a pending plan or storage downgrade.
func (dao *BillingSubscriptionDAO) ListScheduledDowngrades(ctx context.Context, db *gorm.DB) ([]entity.BillingSubscription, error) {
	var rows []entity.BillingSubscription
	err := db.WithContext(ctx).
		Where("subscription_status = ?", "active").
		Where("target_plan_name IS NOT NULL OR (target_storage_bytes IS NOT NULL AND addon_storage_bytes IS NOT NULL AND target_storage_bytes <> addon_storage_bytes)").
		Order("end_time ASC").
		Find(&rows).Error
	return rows, err
}

// ListActive returns subscriptions that can still grant paid features.
func (dao *BillingSubscriptionDAO) ListActive(ctx context.Context, db *gorm.DB) ([]entity.BillingSubscription, error) {
	var rows []entity.BillingSubscription
	err := db.WithContext(ctx).
		Where("subscription_status IN ?", []string{"active", "trialing"}).
		Find(&rows).Error
	return rows, err
}
