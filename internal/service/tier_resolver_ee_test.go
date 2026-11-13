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
	"strings"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/server/config"
	"ragflow/internal/service/billing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TestTierFromPlan verifies the paid/free plan mapping. Only the exact Trial
// name maps to the default (low) lane; every other plan - including empty,
// unknown, and case-variant names - is treated as paid (medium). An
// implementation that silently invents an "unknown tier" or flips the mapping
// fails here.
func TestTierFromPlan(t *testing.T) {
	cases := []struct {
		name string
		plan string
		want common.Tier
	}{
		{"free trial -> default", config.BillingPlanTrialName, common.TierDefault},
		{"paid pro -> medium", "Pro", common.TierMedium},
		{"paid enterprise -> medium", "Enterprise", common.TierMedium},
		{"empty plan -> medium (paid default)", "", common.TierMedium},
		{"unknown plan -> medium", "some-future-plan", common.TierMedium},
		{"trial case variant -> medium (exact match only)", "trial", common.TierMedium},
		{"trial with whitespace -> medium", " Trial ", common.TierMedium},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := tierFromPlan(c.plan); got != c.want {
				t.Fatalf("tierFromPlan(%q) = %q, want %q", c.plan, got, c.want)
			}
		})
	}
}

// withBillingService installs a billing service for the duration of a test and
// restores the previous default on cleanup.
func withBillingService(t *testing.T, svc *billing.Service) {
	t.Helper()
	restore := billing.SetDefault(svc)
	t.Cleanup(restore)
}

// TestDefaultTierResolverBillingNotConfigured: with no billing service, or one
// with billing disabled, the resolver must keep ingestion available on the
// paid (medium) lane rather than failing or downgrading.
func TestDefaultTierResolverBillingNotConfigured(t *testing.T) {
	withBillingService(t, nil)
	if got, err := defaultTierResolver(t.Context(), "tenant-1"); err != nil || got != common.TierMedium {
		t.Fatalf("nil service: tier = %q, err = %v, want medium/nil", got, err)
	}

	disabled := billing.New(config.BillingConfig{Enabled: false}, nil)
	withBillingService(t, disabled)
	if got, err := defaultTierResolver(t.Context(), "tenant-1"); err != nil || got != common.TierMedium {
		t.Fatalf("disabled service: tier = %q, err = %v, want medium/nil", got, err)
	}
}

// TestDefaultTierResolverPlanLookupErrorFailsLoudly: a genuine subscription
// lookup error must be returned (so the caller settles the task FAILED), never
// swallowed into a default tier. An empty sqlite schema has no subscription
// table, so CurrentPlanPayload returns a query error.
func TestDefaultTierResolverPlanLookupErrorFailsLoudly(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// Enabled=true with no billing_subscription table => lookup errors.
	enabled := billing.New(config.BillingConfig{Enabled: true}, db)
	withBillingService(t, enabled)

	tier, err := defaultTierResolver(t.Context(), "tenant-1")
	if err == nil {
		t.Fatalf("lookup error must be returned, got tier %q with nil error", tier)
	}
	if tier != "" {
		t.Fatalf("on error the tier must be empty, got %q", tier)
	}
	if !strings.Contains(err.Error(), "tenant-1") {
		t.Fatalf("error should identify the tenant, got %v", err)
	}
}
