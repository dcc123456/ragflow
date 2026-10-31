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

package billing

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/server/config"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newGuardTestService(t *testing.T) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=rwc", filepath.Join(t.TempDir(), "billing.db"))), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, statement := range []string{
		"CREATE TABLE IF NOT EXISTS dialog (id TEXT PRIMARY KEY, tenant_id TEXT)",
		"CREATE TABLE IF NOT EXISTS knowledgebase (id TEXT PRIMARY KEY, tenant_id TEXT)",
		"CREATE TABLE IF NOT EXISTS user_canvas (id TEXT PRIMARY KEY, user_id TEXT)",
		"CREATE TABLE IF NOT EXISTS search (id TEXT PRIMARY KEY, tenant_id TEXT)",
		"CREATE TABLE IF NOT EXISTS memory (id TEXT PRIMARY KEY, tenant_id TEXT)",
		"CREATE TABLE IF NOT EXISTS user_tenant (id TEXT PRIMARY KEY, tenant_id TEXT, role TEXT, status TEXT)",
		"CREATE TABLE IF NOT EXISTS file (id TEXT PRIMARY KEY, tenant_id TEXT, type TEXT, size INTEGER)",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
	if err := db.AutoMigrate(&entity.BillingSubscription{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cfg := config.BillingConfig{
		Enabled: true,
		PlansByName: map[string]*config.BillingPlanInfo{
			"Trial":   {Name: "Trial", Priority: 1, QuotaApps: 0, QuotaMembers: 1, QuotaStorage: 100},
			"Starter": {Name: "Starter", Priority: 2, QuotaApps: 10, QuotaMembers: 5, QuotaStorage: 1000},
		},
	}
	return New(cfg, db)
}

func TestDowngradeGuardCancelsWhenQuotaExceeded(t *testing.T) {
	service := newGuardTestService(t)
	ctx := context.Background()

	if err := service.db.Exec("INSERT INTO dialog (id, tenant_id) VALUES ('d1','tenant-1')").Error; err != nil {
		t.Fatalf("insert app: %v", err)
	}
	target := "Trial"
	end := time.Now().Add(24 * time.Hour)
	row := &entity.BillingSubscription{
		ID:                 common.GenerateUUID(),
		TenantID:           "tenant-1",
		PlanName:           "Starter",
		Status:             "active",
		SubscriptionStatus: "active",
		TargetPlanName:     &target,
		EndTime:            &end,
	}
	if err := service.subscriptionDAO.UpsertSubscription(ctx, service.db, row); err != nil {
		t.Fatalf("upsert subscription: %v", err)
	}

	if err := service.checkScheduledDowngrades(ctx); err != nil {
		t.Fatalf("guard: %v", err)
	}
	updated, err := service.Subscription(ctx, "tenant-1")
	if err != nil || updated == nil {
		t.Fatalf("read subscription: %v %v", updated, err)
	}
	if updated.TargetPlanName != nil {
		t.Fatalf("target_plan_name should be cleared, got %v", *updated.TargetPlanName)
	}
	if updated.PlanName != "Starter" {
		t.Fatalf("current plan should stay Starter, got %s", updated.PlanName)
	}
}

func TestDowngradeGuardKeepsWithinLimits(t *testing.T) {
	service := newGuardTestService(t)
	ctx := context.Background()

	target := "Trial"
	end := time.Now().Add(24 * time.Hour)
	row := &entity.BillingSubscription{
		ID:                 common.GenerateUUID(),
		TenantID:           "tenant-2",
		PlanName:           "Starter",
		Status:             "active",
		SubscriptionStatus: "active",
		TargetPlanName:     &target,
		EndTime:            &end,
	}
	if err := service.subscriptionDAO.UpsertSubscription(ctx, service.db, row); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := service.checkScheduledDowngrades(ctx); err != nil {
		t.Fatalf("guard: %v", err)
	}
	updated, _ := service.Subscription(ctx, "tenant-2")
	if updated.TargetPlanName == nil || *updated.TargetPlanName != "Trial" {
		t.Fatalf("pending downgrade should be kept when within limits: %+v", updated.TargetPlanName)
	}
}
