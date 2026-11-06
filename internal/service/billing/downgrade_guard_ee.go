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
	"fmt"
	"os"
	"strings"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/server/config"

	"github.com/stripe/stripe-go/v87"
	"github.com/stripe/stripe-go/v87/subscription"
	"github.com/stripe/stripe-go/v87/subscriptionschedule"

	"go.uber.org/zap"
)

const downgradeCheckInterval = 10 * time.Minute

// StartDowngradeGuard launches the background quota guard.
// It is a no-op unless billing and the guard are enabled.
func (s *Service) StartDowngradeGuard(ctx context.Context) {
	if s == nil || !s.cfg.Enabled || !downgradeGuardEnabled() {
		return
	}
	go func() {
		ticker := time.NewTicker(downgradeCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.checkScheduledDowngrades(ctx); err != nil {
					common.Warn("Billing downgrade guard sweep failed", zap.Error(err))
				}
			}
		}
	}()
}

func downgradeGuardEnabled() bool {
	value := strings.TrimSpace(os.Getenv("DOWNGRADE_GUARD_ENABLED"))
	if value == "" {
		return true
	}
	return !strings.EqualFold(value, "false")
}

// checkScheduledDowngrades cancels pending downgrades that would exceed the
// target plan's quota at the end of the current period.
func (s *Service) checkScheduledDowngrades(ctx context.Context) error {
	rows, err := s.subscriptionDAO.ListScheduledDowngrades(ctx, s.db)
	if err != nil {
		return err
	}
	usage := dao.NewBillingUsageDAO()
	for index := range rows {
		row := rows[index]
		if row.EndTime == nil {
			continue
		}
		exceeded, reason, checkErr := s.downgradeExceeded(ctx, &row, usage)
		if checkErr != nil {
			common.Warn("Billing downgrade guard check failed", zap.String("tenant_id", row.TenantID), zap.Error(checkErr))
			continue
		}
		if !exceeded {
			continue
		}
		if cancelErr := s.CancelScheduledChange(ctx, &row); cancelErr != nil {
			common.Warn("Billing downgrade guard cancel failed", zap.String("tenant_id", row.TenantID), zap.Error(cancelErr))
			continue
		}
		common.Warn("Billing scheduled downgrade cancelled: quota exceeded",
			zap.String("tenant_id", row.TenantID),
			zap.String("reason", reason))
	}
	return nil
}

func (s *Service) downgradeExceeded(ctx context.Context, row *entity.BillingSubscription, usage *dao.BillingUsageDAO) (bool, string, error) {
	apps, err := usage.TenantApps(ctx, s.db, row.TenantID)
	if err != nil {
		return false, "", err
	}
	members, err := usage.TenantMembers(ctx, s.db, row.TenantID)
	if err != nil {
		return false, "", err
	}
	storage, err := usage.TenantStorageBytes(ctx, s.db, row.TenantID)
	if err != nil {
		return false, "", err
	}

	targetPlanName := row.PlanName
	if row.TargetPlanName != nil && *row.TargetPlanName != "" {
		targetPlanName = *row.TargetPlanName
	}
	plan, ok := s.cfg.Plan(targetPlanName)
	if !ok {
		plan = s.TrialPlan()
	}
	if plan == nil {
		return false, "", nil
	}
	if apps > plan.QuotaApps {
		return true, "apps", nil
	}
	if members > plan.QuotaMembers {
		return true, "members", nil
	}
	// Storage decreases are evaluated independently of plan changes. A paid
	// plan downgrade keeps its add-on unless the change also targets storage;
	// a fall back to Trial drops the add-on entirely.
	storageLimit := plan.QuotaStorage
	if plan.Name != config.BillingPlanTrialName {
		if row.TargetStorageBytes != nil {
			storageLimit += *row.TargetStorageBytes
		} else if row.AddonStorageBytes != nil {
			storageLimit += *row.AddonStorageBytes
		}
	}
	if storage > storageLimit {
		return true, "storage", nil
	}
	return false, "", nil
}

// CancelScheduledChange releases the Stripe schedule and clears the pending
// downgrade markers so the tenant keeps the current plan.
func (s *Service) CancelScheduledChange(ctx context.Context, row *entity.BillingSubscription) error {
	if row.CustomerID == "" && row.SubscriptionID == "" {
		// Nothing exists on the Stripe side to undo.
		row.TargetPlanName = nil
		row.TargetStorageBytes = nil
		row.ScheduleID = nil
		return s.subscriptionDAO.UpsertSubscription(ctx, s.db, row)
	}
	if err := s.EnsureStripe(); err != nil {
		return err
	}
	confirmed := false
	foundSchedule := false
	if row.TargetPlanName != nil && strings.EqualFold(*row.TargetPlanName, config.BillingPlanTrialName) {
		sub, err := subscription.Get(row.SubscriptionID, nil)
		if err != nil {
			return err
		}
		if sub.CancelAtPeriodEnd {
			if _, err := subscription.Update(row.SubscriptionID, &stripe.SubscriptionParams{
				CancelAtPeriodEnd: stripe.Bool(false),
			}); err != nil {
				return err
			}
		}
		// cancel_at_period_end is already false on a retry: the change is gone.
		confirmed = !sub.CancelAtPeriodEnd
	}
	if row.CustomerID != "" {
		iter := subscriptionschedule.List(&stripe.SubscriptionScheduleListParams{
			Customer: stripe.String(row.CustomerID),
		})
		for iter.Next() {
			schedule := iter.SubscriptionSchedule()
			if schedule == nil || schedule.Status != "active" {
				continue
			}
			if schedule.Subscription.ID != "" && row.SubscriptionID != "" && schedule.Subscription.ID != row.SubscriptionID {
				continue
			}
			foundSchedule = true
			if _, releaseErr := subscriptionschedule.Release(schedule.ID, nil); releaseErr != nil {
				return releaseErr
			}
			confirmed = true
		}
		if err := iter.Err(); err != nil {
			return err
		}
	}
	if !confirmed && foundSchedule {
		return fmt.Errorf("pending downgrade could not be cancelled")
	}
	row.TargetPlanName = nil
	row.TargetStorageBytes = nil
	row.ScheduleID = nil
	return s.subscriptionDAO.UpsertSubscription(ctx, s.db, row)
}
