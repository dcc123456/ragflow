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

//go:build ee

package dao

import (
	"context"

	"ragflow/internal/entity"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BillingPointDAO is the data access object for point accounts, ledgers and holds.
type BillingPointDAO struct{}

// NewBillingPointDAO creates a billing point DAO.
func NewBillingPointDAO() *BillingPointDAO {
	return &BillingPointDAO{}
}

// GetAccount returns the point account of a tenant, or nil when absent.
func (dao *BillingPointDAO) GetAccount(ctx context.Context, db *gorm.DB, tenantID string) (*entity.BillingPointAccount, error) {
	var account entity.BillingPointAccount
	err := db.WithContext(ctx).Where("tenant_id = ?", tenantID).Limit(1).Take(&account).Error
	if err != nil {
		return nil, err
	}
	return &account, nil
}

// CreateAccountIfAbsent inserts the account row when it does not exist.
func (dao *BillingPointDAO) CreateAccountIfAbsent(ctx context.Context, db *gorm.DB, account *entity.BillingPointAccount) error {
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "tenant_id"}},
		DoNothing: true,
	}).Create(account).Error
}

// LockAccount selects the account row for update inside the current transaction.
func (dao *BillingPointDAO) LockAccount(ctx context.Context, db *gorm.DB, tenantID string) (*entity.BillingPointAccount, error) {
	var account entity.BillingPointAccount
	query := db.WithContext(ctx)
	if db.Dialector.Name() != "sqlite" {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := query.Where("tenant_id = ?", tenantID).Limit(1).Take(&account).Error
	if err != nil {
		return nil, err
	}
	return &account, nil
}

// ApplyAccountDelta adjusts consumed and held counters atomically.
// The conditional WHERE clause keeps plan consumption within quota when
// planQuotaLimit is non-negative; callers pass -1 to skip that guard.
func (dao *BillingPointDAO) ApplyAccountDelta(ctx context.Context, db *gorm.DB, tenantID string, planDelta, addonDelta, heldDelta, planQuotaLimit int64) (bool, error) {
	query := db.WithContext(ctx).Model(&entity.BillingPointAccount{}).Where("tenant_id = ?", tenantID)
	if planQuotaLimit >= 0 && planDelta > 0 {
		query = query.Where("consumed_plan_points + ? <= ?", planDelta, planQuotaLimit)
	}
	// Counters are floored at zero so a release that crosses a cycle reset can
	// never turn into free quota.
	result := query.Updates(map[string]interface{}{
		"consumed_plan_points":  gorm.Expr("CASE WHEN consumed_plan_points + ? < 0 THEN 0 ELSE consumed_plan_points + ? END", planDelta, planDelta),
		"consumed_addon_points": gorm.Expr("CASE WHEN consumed_addon_points + ? < 0 THEN 0 ELSE consumed_addon_points + ? END", addonDelta, addonDelta),
		"held_points":           gorm.Expr("CASE WHEN held_points + ? < 0 THEN 0 ELSE held_points + ? END", heldDelta, heldDelta),
	})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// IncrementAddonPurchased adds purchased points to the account.
func (dao *BillingPointDAO) IncrementAddonPurchased(ctx context.Context, db *gorm.DB, tenantID string, points int64) error {
	return db.WithContext(ctx).Model(&entity.BillingPointAccount{}).
		Where("tenant_id = ?", tenantID).
		UpdateColumn("addon_purchased_points", gorm.Expr("addon_purchased_points + ?", points)).Error
}

// HoldByID returns a hold row by primary key.
func (dao *BillingPointDAO) HoldByID(ctx context.Context, db *gorm.DB, holdID string) (*entity.BillingPointHold, error) {
	var hold entity.BillingPointHold
	err := db.WithContext(ctx).Where("id = ?", holdID).Limit(1).Take(&hold).Error
	if err != nil {
		return nil, err
	}
	return &hold, nil
}

// HoldByDocID returns the most recent hold of a document.
func (dao *BillingPointDAO) HoldByDocID(ctx context.Context, db *gorm.DB, docID string) (*entity.BillingPointHold, error) {
	var hold entity.BillingPointHold
	err := db.WithContext(ctx).Where("doc_id = ?", docID).Order("create_time DESC").Limit(1).Take(&hold).Error
	if err != nil {
		return nil, err
	}
	return &hold, nil
}

// HoldByIdempotencyKey returns a hold by its idempotency key.
func (dao *BillingPointDAO) HoldByIdempotencyKey(ctx context.Context, db *gorm.DB, key string) (*entity.BillingPointHold, error) {
	var hold entity.BillingPointHold
	err := db.WithContext(ctx).Where("idempotency_key = ?", key).Limit(1).Take(&hold).Error
	if err != nil {
		return nil, err
	}
	return &hold, nil
}

// CreateHold inserts a hold row.
func (dao *BillingPointDAO) CreateHold(ctx context.Context, db *gorm.DB, hold *entity.BillingPointHold) error {
	return db.WithContext(ctx).Create(hold).Error
}

// UpdateHoldStatus moves a hold to the given status when it is still held.
func (dao *BillingPointDAO) UpdateHoldStatus(ctx context.Context, db *gorm.DB, holdID, from, to string) (bool, error) {
	result := db.WithContext(ctx).Model(&entity.BillingPointHold{}).
		Where("id = ? AND status = ?", holdID, from).
		Update("status", to)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// InsertLedger appends a ledger row. It reports whether this call won the
// unique idempotency key, so callers only apply the balance delta once.
func (dao *BillingPointDAO) InsertLedger(ctx context.Context, db *gorm.DB, ledger *entity.BillingPointLedger) (bool, error) {
	result := db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "idempotency_key"}},
		DoNothing: true,
	}).Create(ledger)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// LedgerForHoldType returns the first ledger row of a hold with the given event type.
func (dao *BillingPointDAO) LedgerForHoldType(ctx context.Context, db *gorm.DB, holdID, eventType string) (*entity.BillingPointLedger, error) {
	var ledger entity.BillingPointLedger
	err := db.WithContext(ctx).
		Where("related_hold_id = ? AND event_type = ?", holdID, eventType).
		Order("create_time ASC").
		Limit(1).
		Take(&ledger).Error
	if err != nil {
		return nil, err
	}
	return &ledger, nil
}

// ListLedger returns ledger rows of a tenant, newest first.
func (dao *BillingPointDAO) ListLedger(ctx context.Context, db *gorm.DB, tenantID string, offset, limit int) ([]entity.BillingPointLedger, int64, error) {
	var total int64
	if err := db.WithContext(ctx).Model(&entity.BillingPointLedger{}).Where("tenant_id = ?", tenantID).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []entity.BillingPointLedger
	err := db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("create_time DESC").
		Offset(offset).
		Limit(limit).
		Find(&rows).Error
	return rows, total, err
}

// ListHolds returns hold rows of a tenant, newest first.
func (dao *BillingPointDAO) ListHolds(ctx context.Context, db *gorm.DB, tenantID string, offset, limit int) ([]entity.BillingPointHold, int64, error) {
	var total int64
	if err := db.WithContext(ctx).Model(&entity.BillingPointHold{}).Where("tenant_id = ?", tenantID).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []entity.BillingPointHold
	err := db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("create_time DESC").
		Offset(offset).
		Limit(limit).
		Find(&rows).Error
	return rows, total, err
}

// LedgerExists reports whether a ledger row with the key already exists.
func (dao *BillingPointDAO) LedgerExists(ctx context.Context, db *gorm.DB, key string) (bool, error) {
	var count int64
	err := db.WithContext(ctx).Model(&entity.BillingPointLedger{}).Where("idempotency_key = ?", key).Count(&count).Error
	return count > 0, err
}
