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

package service

import (
	"context"
	"fmt"

	"ragflow/internal/common"
	"ragflow/internal/server/config"
	"ragflow/internal/service/billing"
)

// defaultTierResolver maps a tenant's billing plan to an ingestion queue tier:
// paid plans (any non-Trial plan) use the medium lane, free Trial tenants use
// the default (low) lane. Billing not being configured on a deployment is a
// deployment state, not a lookup failure, so it falls back to the paid lane and
// keeps ingestion available. A genuine subscription lookup error is returned so
// the caller settles the task as FAILED instead of silently downgrading it.
func defaultTierResolver(ctx context.Context, tenantID string) (common.Tier, error) {
	svc := billing.Get()
	if svc == nil || !svc.Enabled() {
		return common.TierMedium, nil
	}
	payload, err := svc.CurrentPlanPayload(ctx, tenantID)
	if err != nil {
		return "", fmt.Errorf("resolve tier for tenant %s: %w", tenantID, err)
	}
	plan, _ := payload["plan_name"].(string)
	return tierFromPlan(plan), nil
}

// tierFromPlan maps a resolved billing plan name to a queue tier. Trial (free)
// tenants use the default lane; every other plan - including an empty/unknown
// name - is treated as paid (medium). There is deliberately no "unknown tier":
// a plan lookup that fails returns an error before this is called, so the
// producer fails loudly instead of silently downgrading the task.
func tierFromPlan(plan string) common.Tier {
	if plan == config.BillingPlanTrialName {
		return common.TierDefault
	}
	return common.TierMedium
}
