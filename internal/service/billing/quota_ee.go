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
	"strings"

	"ragflow/internal/dao"
	"ragflow/internal/server/config"
)

// ResourceDelta is the requested resource increment.
type ResourceDelta struct {
	Apps    int64
	Members int64
	Storage int64
}

// InsufficientResourceError reports a quota breach with the values the web
// upgrade modal renders.
type InsufficientResourceError struct {
	Resource string
	Current  int64
	Limit    int64
	Required int64
}

func (e *InsufficientResourceError) Error() string {
	return fmt.Sprintf("%s limit reached: current %d, limit %d, requested %d", e.Resource, e.Current, e.Limit, e.Required)
}

// CheckResources verifies that the tenant can accept the requested delta.
// It returns nil when billing is disabled so callers need no branch.
func (s *Service) CheckResources(ctx context.Context, tenantID string, delta ResourceDelta) error {
	if s == nil || !s.cfg.Enabled {
		return nil
	}
	state, err := s.resolveSubscription(ctx, tenantID)
	if err != nil {
		return err
	}
	plan := state.Plan
	if plan == nil {
		plan = s.TrialPlan()
	}
	if plan == nil {
		return fmt.Errorf("no billing plan configured")
	}

	usage := dao.NewBillingUsageDAO()
	if delta.Apps > 0 {
		used, err := usage.TenantApps(ctx, s.db, tenantID)
		if err != nil {
			return err
		}
		if used+delta.Apps > plan.QuotaApps {
			return &InsufficientResourceError{Resource: "apps", Current: used, Limit: plan.QuotaApps, Required: delta.Apps}
		}
	}
	if delta.Members > 0 {
		used, err := usage.TenantMembers(ctx, s.db, tenantID)
		if err != nil {
			return err
		}
		if used+delta.Members > plan.QuotaMembers {
			return &InsufficientResourceError{Resource: "members", Current: used, Limit: plan.QuotaMembers, Required: delta.Members}
		}
	}
	if delta.Storage > 0 {
		used, err := usage.TenantStorageBytes(ctx, s.db, tenantID)
		if err != nil {
			return err
		}
		addon, _, err := s.subscriptionDAO.GetStorageBytesForTenant(ctx, s.db, tenantID)
		if err != nil {
			addon = 0
		}
		limit := plan.QuotaStorage + addon
		if used+delta.Storage > limit {
			return &InsufficientResourceError{Resource: "storage", Current: used, Limit: limit, Required: delta.Storage}
		}
	}
	return nil
}

// StorageLimit returns the effective storage limit (plan + addon) of a tenant.
func (s *Service) StorageLimit(ctx context.Context, tenantID string) (int64, error) {
	state, err := s.resolveSubscription(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	plan := state.Plan
	if plan == nil {
		plan = s.TrialPlan()
	}
	if plan == nil {
		return 0, nil
	}
	if plan.Name == config.StorageProductName {
		return plan.QuotaStorage, nil
	}
	addon, _, err := s.subscriptionDAO.GetStorageBytesForTenant(ctx, s.db, tenantID)
	if err != nil {
		addon = 0
	}
	return plan.QuotaStorage + addon, nil
}

// APIAuthAllowed reports whether the tenant's plan may use API keys.
// Starter and Pro are allowed; Trial is not, matching the Python gate.
func (s *Service) APIAuthAllowed(ctx context.Context, tenantID string) (bool, error) {
	if s == nil || !s.cfg.Enabled {
		return true, nil
	}
	subscription, err := s.Subscription(ctx, tenantID)
	if err != nil {
		return false, err
	}
	planName := config.BillingPlanTrialName
	status := ""
	if subscription != nil {
		if subscription.PlanName != "" {
			planName = subscription.PlanName
		}
		status = subscription.SubscriptionStatus
	}
	switch strings.ToLower(strings.TrimSpace(planName)) {
	case "starter", "pro":
		return status == "" || SubscriptionEntitled(status), nil
	default:
		return false, nil
	}
}
