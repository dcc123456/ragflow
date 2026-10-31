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
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"ragflow/internal/server/config"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newQuotaTestService(t *testing.T) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=rwc", filepath.Join(t.TempDir(), "billing.db"))), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS dialog (id TEXT PRIMARY KEY, tenant_id TEXT)`,
		`CREATE TABLE IF NOT EXISTS knowledgebase (id TEXT PRIMARY KEY, tenant_id TEXT)`,
		`CREATE TABLE IF NOT EXISTS user_canvas (id TEXT PRIMARY KEY, user_id TEXT)`,
		`CREATE TABLE IF NOT EXISTS search (id TEXT PRIMARY KEY, tenant_id TEXT)`,
		`CREATE TABLE IF NOT EXISTS memory (id TEXT PRIMARY KEY, tenant_id TEXT)`,
		`CREATE TABLE IF NOT EXISTS user_tenant (id TEXT PRIMARY KEY, tenant_id TEXT, role TEXT, status TEXT)`,
		`CREATE TABLE IF NOT EXISTS file (id TEXT PRIMARY KEY, tenant_id TEXT, type TEXT, size INTEGER)`,
		`CREATE TABLE IF NOT EXISTS billing_subscription (id TEXT PRIMARY KEY, tenant_id TEXT, plan_name TEXT, addon_storage_bytes INTEGER, target_storage_bytes INTEGER)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
	cfg := config.BillingConfig{
		Enabled: true,
		PlansByName: map[string]*config.BillingPlanInfo{
			"Trial": {Name: "Trial", Priority: 1, QuotaApps: 1, QuotaMembers: 1, QuotaStorage: 100, QuotaPoints: 10},
		},
	}
	return New(cfg, db)
}

func TestCheckResourcesApps(t *testing.T) {
	service := newQuotaTestService(t)
	ctx := context.Background()

	if err := service.CheckResources(ctx, "tenant-a", ResourceDelta{Apps: 1}); err != nil {
		t.Fatalf("first app should fit: %v", err)
	}
	if err := service.DB().Exec("INSERT INTO dialog (id, tenant_id) VALUES ('d1','tenant-a')").Error; err != nil {
		t.Fatalf("insert dialog: %v", err)
	}
	err := service.CheckResources(ctx, "tenant-a", ResourceDelta{Apps: 1})
	var insufficient *InsufficientResourceError
	if !errors.As(err, &insufficient) || insufficient.Resource != "apps" {
		t.Fatalf("expected apps quota error, got %v", err)
	}
	if insufficient.Current != 1 || insufficient.Limit != 1 {
		t.Fatalf("apps detail = %+v", insufficient)
	}
}

func TestCheckResourcesMembers(t *testing.T) {
	service := newQuotaTestService(t)
	ctx := context.Background()

	if err := service.DB().Exec("INSERT INTO user_tenant (id, tenant_id, role, status) VALUES ('u1','tenant-b','owner','1')").Error; err != nil {
		t.Fatalf("insert member: %v", err)
	}
	err := service.CheckResources(ctx, "tenant-b", ResourceDelta{Members: 1})
	var insufficient *InsufficientResourceError
	if !errors.As(err, &insufficient) || insufficient.Resource != "members" {
		t.Fatalf("expected members quota error, got %v", err)
	}
}

func TestCheckResourcesStorage(t *testing.T) {
	service := newQuotaTestService(t)
	ctx := context.Background()

	if err := service.DB().Exec("INSERT INTO file (id, tenant_id, type, size) VALUES ('f1','tenant-c','pdf',60)").Error; err != nil {
		t.Fatalf("insert file: %v", err)
	}
	if err := service.CheckResources(ctx, "tenant-c", ResourceDelta{Storage: 40}); err != nil {
		t.Fatalf("40 bytes should fit: %v", err)
	}
	err := service.CheckResources(ctx, "tenant-c", ResourceDelta{Storage: 41})
	var insufficient *InsufficientResourceError
	if !errors.As(err, &insufficient) || insufficient.Resource != "storage" {
		t.Fatalf("expected storage quota error, got %v", err)
	}
}

func TestCheckResourcesDisabledIsNoop(t *testing.T) {
	service := New(config.BillingConfig{Enabled: false}, nil)
	if err := service.CheckResources(context.Background(), "tenant-d", ResourceDelta{Apps: 100}); err != nil {
		t.Fatalf("disabled billing must not block: %v", err)
	}
}
