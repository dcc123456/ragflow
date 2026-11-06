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

package billing

import (
	"context"
	"errors"
	"fmt"

	"ragflow/internal/common"
	"ragflow/internal/entity"

	"gorm.io/gorm"
)

// InsufficientPointsError reports that a tenant cannot cover the requested points.
type InsufficientPointsError struct {
	TenantID  string
	Required  int64
	Available int64
}

func (e *InsufficientPointsError) Error() string {
	return fmt.Sprintf("tenant %s has %d points, need %d", e.TenantID, e.Available, e.Required)
}

// Balance is the derived points balance of a tenant.
type Balance struct {
	ConsumedPlanPoints   int64 `json:"consumed_plan_points"`
	AddonPurchasedPoints int64 `json:"addon_purchased_points"`
	ConsumedAddonPoints  int64 `json:"consumed_addon_points"`
	HeldPoints           int64 `json:"held_points"`
	AvailablePlanPoints  int64 `json:"available_plan_points"`
	AvailableAddonPoints int64 `json:"available_addon_points"`
}

func (s *Service) ensurePointAccount(ctx context.Context, tx *gorm.DB, tenantID string) (*entity.BillingPointAccount, error) {
	account, err := s.pointDAO.GetAccount(ctx, tx, tenantID)
	if err == nil {
		return account, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	created := &entity.BillingPointAccount{
		ID:                 common.GenerateUUID(),
		TenantID:           tenantID,
		HeldPoints:         0,
		ConsumedPlanPoints: 0,
	}
	if err := s.pointDAO.CreateAccountIfAbsent(ctx, tx, created); err != nil {
		return nil, err
	}
	return s.pointDAO.GetAccount(ctx, tx, tenantID)
}

// GetBalance returns the derived points balance of a tenant.
func (s *Service) GetBalance(ctx context.Context, tenantID string) (*Balance, error) {
	account, err := s.pointDAO.GetAccount(ctx, s.db, tenantID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			quota, quotaErr := s.PlanQuotaPoints(ctx, tenantID)
			if quotaErr != nil {
				return nil, quotaErr
			}
			return &Balance{AvailablePlanPoints: quota}, nil
		}
		return nil, err
	}
	quota, err := s.PlanQuotaPoints(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	balance := &Balance{
		ConsumedPlanPoints:   account.ConsumedPlanPoints,
		AddonPurchasedPoints: account.AddonPurchasedPoints,
		ConsumedAddonPoints:  account.ConsumedAddonPoints,
		HeldPoints:           account.HeldPoints,
		AvailablePlanPoints:  maxInt64(0, quota-account.ConsumedPlanPoints),
		AvailableAddonPoints: maxInt64(0, account.AddonPurchasedPoints-account.ConsumedAddonPoints),
	}
	return balance, nil
}

// Hold reserves points for a document parse. It is idempotent per key.
func (s *Service) Hold(ctx context.Context, tenantID, docID string, points int64, idempotencyKey string, metadata map[string]interface{}) (*entity.BillingPointHold, error) {
	if points <= 0 {
		return nil, fmt.Errorf("points must be positive")
	}
	var result *entity.BillingPointHold
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		account, err := s.ensurePointAccount(ctx, tx, tenantID)
		if err != nil {
			return err
		}
		account, err = s.pointDAO.LockAccount(ctx, tx, tenantID)
		if err != nil {
			return err
		}

		existing, err := s.pointDAO.HoldByIdempotencyKey(ctx, tx, idempotencyKey)
		if err == nil {
			result = existing
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		quota, err := s.PlanQuotaPoints(ctx, tenantID)
		if err != nil {
			return err
		}
		planAvailable := maxInt64(0, quota-account.ConsumedPlanPoints)
		addonAvailable := maxInt64(0, account.AddonPurchasedPoints-account.ConsumedAddonPoints)
		if planAvailable+addonAvailable < points {
			return &InsufficientPointsError{TenantID: tenantID, Required: points, Available: planAvailable + addonAvailable}
		}
		planUsed := minInt64(planAvailable, points)
		addonUsed := points - planUsed

		hold := &entity.BillingPointHold{
			ID:             common.GenerateUUID(),
			TenantID:       tenantID,
			DocID:          docID,
			Points:         points,
			PlanPoints:     planUsed,
			AddonPoints:    addonUsed,
			Status:         "held",
			IdempotencyKey: idempotencyKey,
		}
		if err := s.pointDAO.CreateHold(ctx, tx, hold); err != nil {
			return err
		}
		if planUsed > 0 {
			if _, err := s.pointDAO.InsertLedger(ctx, tx, &entity.BillingPointLedger{
				ID:             common.GenerateUUID(),
				TenantID:       tenantID,
				EventType:      "hold_created",
				Points:         -planUsed,
				Source:         "plan",
				IdempotencyKey: fmt.Sprintf("hold:%s:plan", hold.ID),
				RelatedHoldID:  &hold.ID,
				Metadata:       entity.JSONMap(metadata),
			}); err != nil {
				return err
			}
		}
		if addonUsed > 0 {
			if _, err := s.pointDAO.InsertLedger(ctx, tx, &entity.BillingPointLedger{
				ID:             common.GenerateUUID(),
				TenantID:       tenantID,
				EventType:      "hold_created",
				Points:         -addonUsed,
				Source:         "addon",
				IdempotencyKey: fmt.Sprintf("hold:%s:addon", hold.ID),
				RelatedHoldID:  &hold.ID,
				Metadata:       entity.JSONMap(metadata),
			}); err != nil {
				return err
			}
		}

		updated, err := s.pointDAO.ApplyAccountDelta(ctx, tx, tenantID, planUsed, addonUsed, points, quota)
		if err != nil {
			return err
		}
		if !updated {
			return &InsufficientPointsError{TenantID: tenantID, Required: points, Available: planAvailable + addonAvailable}
		}
		result = hold
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// CommitHold converts a hold into consumption.
func (s *Service) CommitHold(ctx context.Context, holdID string) (bool, error) {
	return s.finalizeHold(ctx, holdID, true)
}

// ReleaseHold returns a hold's points to the tenant.
func (s *Service) ReleaseHold(ctx context.Context, holdID string) (bool, error) {
	return s.finalizeHold(ctx, holdID, false)
}

func (s *Service) finalizeHold(ctx context.Context, holdID string, commit bool) (bool, error) {
	ok := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		hold, err := s.pointDAO.HoldByID(ctx, tx, holdID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if hold.Status == "committed" || hold.Status == "released" {
			ok = true
			return nil
		}
		if hold.Status != "held" {
			return nil
		}

		metadata := map[string]interface{}{}
		if original, err := s.pointDAO.LedgerForHoldType(ctx, tx, holdID, "hold_created"); err == nil && original.Metadata != nil {
			metadata = original.Metadata
		}

		// Claim the state transition before touching balances so concurrent
		// finalizers cannot double-apply deltas.
		target := "released"
		if commit {
			target = "committed"
		}
		claimed, err := s.pointDAO.UpdateHoldStatus(ctx, tx, holdID, "held", target)
		if err != nil {
			return err
		}
		if !claimed {
			ok = true
			return nil
		}

		target = "released"
		planDelta := -hold.PlanPoints
		addonDelta := -hold.AddonPoints
		eventType := "release"
		pointsSign := int64(1)
		if commit {
			target = "committed"
			planDelta = 0
			addonDelta = 0
			eventType = "consume"
			pointsSign = -1
		}

		if commit {
			if hold.PlanPoints > 0 {
				if _, err := s.pointDAO.InsertLedger(ctx, tx, &entity.BillingPointLedger{
					ID:             common.GenerateUUID(),
					TenantID:       hold.TenantID,
					EventType:      eventType,
					Points:         pointsSign * hold.PlanPoints,
					Source:         "plan",
					IdempotencyKey: fmt.Sprintf("commit:%s:plan", holdID),
					RelatedHoldID:  &holdID,
					Metadata:       entity.JSONMap(metadata),
				}); err != nil {
					return err
				}
			}
			if hold.AddonPoints > 0 {
				if _, err := s.pointDAO.InsertLedger(ctx, tx, &entity.BillingPointLedger{
					ID:             common.GenerateUUID(),
					TenantID:       hold.TenantID,
					EventType:      eventType,
					Points:         pointsSign * hold.AddonPoints,
					Source:         "addon",
					IdempotencyKey: fmt.Sprintf("commit:%s:addon", holdID),
					RelatedHoldID:  &holdID,
					Metadata:       entity.JSONMap(metadata),
				}); err != nil {
					return err
				}
			}
		} else {
			if hold.PlanPoints > 0 {
				if _, err := s.pointDAO.InsertLedger(ctx, tx, &entity.BillingPointLedger{
					ID:             common.GenerateUUID(),
					TenantID:       hold.TenantID,
					EventType:      eventType,
					Points:         hold.PlanPoints,
					Source:         "plan",
					IdempotencyKey: fmt.Sprintf("release:%s:plan", holdID),
					RelatedHoldID:  &holdID,
					Metadata:       entity.JSONMap(metadata),
				}); err != nil {
					return err
				}
			}
			if hold.AddonPoints > 0 {
				if _, err := s.pointDAO.InsertLedger(ctx, tx, &entity.BillingPointLedger{
					ID:             common.GenerateUUID(),
					TenantID:       hold.TenantID,
					EventType:      eventType,
					Points:         hold.AddonPoints,
					Source:         "addon",
					IdempotencyKey: fmt.Sprintf("release:%s:addon", holdID),
					RelatedHoldID:  &holdID,
					Metadata:       entity.JSONMap(metadata),
				}); err != nil {
					return err
				}
			}
		}

		updated, err := s.pointDAO.ApplyAccountDelta(ctx, tx, hold.TenantID, planDelta, addonDelta, -hold.Points, -1)
		if err != nil {
			return err
		}
		if !updated {
			return nil
		}
		ok = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return ok, nil
}

// Recharge adds purchased points to a tenant account.
func (s *Service) Recharge(ctx context.Context, tenantID string, points int64, idempotencyKey, description string, metadata map[string]interface{}) error {
	if points <= 0 {
		return fmt.Errorf("points must be positive")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := s.ensurePointAccount(ctx, tx, tenantID); err != nil {
			return err
		}
		exists, err := s.pointDAO.LedgerExists(ctx, tx, idempotencyKey)
		if err != nil {
			return err
		}
		if exists {
			return nil
		}
		inserted, err := s.pointDAO.InsertLedger(ctx, tx, &entity.BillingPointLedger{
			ID:             common.GenerateUUID(),
			TenantID:       tenantID,
			EventType:      "recharge",
			Points:         points,
			Source:         "addon",
			IdempotencyKey: idempotencyKey,
			Description:    &description,
			Metadata:       entity.JSONMap(metadata),
		})
		if err != nil {
			return err
		}
		if !inserted {
			return nil
		}
		return s.pointDAO.IncrementAddonPurchased(ctx, tx, tenantID, points)
	})
}

// ResetPlanConsumedPoints resets plan consumption at a new billing cycle.
func (s *Service) ResetPlanConsumedPoints(ctx context.Context, tenantID string) (bool, error) {
	return s.resetPlanConsumedPoints(ctx, s.db, tenantID)
}

// resetPlanConsumedPoints clears the consumed plan points over the given
// handle so callers can reset and write the new cycle atomically.
func (s *Service) resetPlanConsumedPoints(ctx context.Context, db *gorm.DB, tenantID string) (bool, error) {
	account, err := s.pointDAO.GetAccount(ctx, db, tenantID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return true, nil
		}
		return false, err
	}
	if account.ConsumedPlanPoints == 0 {
		return true, nil
	}
	return true, db.WithContext(ctx).Model(&entity.BillingPointAccount{}).
		Where("tenant_id = ?", tenantID).
		Update("consumed_plan_points", 0).Error
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// HoldForDoc returns the latest hold of a document, or nil when absent.
func (s *Service) HoldForDoc(ctx context.Context, docID string) (*entity.BillingPointHold, error) {
	hold, err := s.pointDAO.HoldByDocID(ctx, s.db, docID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return hold, nil
}

// ReleaseDocumentHold releases the document's uncommitted parse hold, if any.
func (s *Service) ReleaseDocumentHold(ctx context.Context, docID string) error {
	hold, err := s.HoldForDoc(ctx, docID)
	if err != nil || hold == nil || hold.Status != "held" {
		return nil
	}
	_, err = s.ReleaseHold(ctx, hold.ID)
	return err
}
