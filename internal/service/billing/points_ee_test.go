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
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ragflow/internal/entity"
	"ragflow/internal/server/config"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=rwc&_pragma=busy_timeout(500)", filepath.Join(t.TempDir(), "billing.db"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&entity.BillingPointAccount{},
		&entity.BillingPointLedger{},
		&entity.BillingPointHold{},
		&entity.BillingPaymentOrder{},
		&entity.BillingSubscription{},
		&entity.BillingWebhookEvent{},
		&entity.BillingPricePoint{},
	); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	cfg := config.BillingConfig{
		Enabled: true,
		PlansByName: map[string]*config.BillingPlanInfo{
			"Trial": {Name: "Trial", Priority: 1, QuotaPoints: 10},
		},
	}
	return New(cfg, db)
}

func TestHoldCommitRelease(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()

	hold, err := service.Hold(ctx, "tenant-1", "doc-1", 6, "hold:doc-1", map[string]interface{}{"doc_id": "doc-1"})
	if err != nil {
		t.Fatalf("hold: %v", err)
	}
	if hold.PlanPoints != 6 || hold.AddonPoints != 0 {
		t.Fatalf("hold split = plan %d addon %d, want 6/0", hold.PlanPoints, hold.AddonPoints)
	}

	balance, err := service.GetBalance(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	if balance.ConsumedPlanPoints != 6 || balance.HeldPoints != 6 || balance.AvailablePlanPoints != 4 {
		t.Fatalf("balance after hold = %+v", balance)
	}

	duplicate, err := service.Hold(ctx, "tenant-1", "doc-1", 6, "hold:doc-1", nil)
	if err != nil {
		t.Fatalf("duplicate hold: %v", err)
	}
	if duplicate.ID != hold.ID {
		t.Fatalf("duplicate hold id = %s, want %s", duplicate.ID, hold.ID)
	}
	balance, _ = service.GetBalance(ctx, "tenant-1")
	if balance.ConsumedPlanPoints != 6 {
		t.Fatalf("duplicate hold consumed %d, want 6", balance.ConsumedPlanPoints)
	}

	committed, err := service.CommitHold(ctx, hold.ID)
	if err != nil || !committed {
		t.Fatalf("commit = %v, %v", committed, err)
	}
	balance, _ = service.GetBalance(ctx, "tenant-1")
	if balance.HeldPoints != 0 || balance.ConsumedPlanPoints != 6 {
		t.Fatalf("balance after commit = %+v", balance)
	}
	if again, _ := service.CommitHold(ctx, hold.ID); !again {
		t.Fatal("second commit should be idempotent true")
	}
}

func TestReleaseRestoresConsumption(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()

	hold, err := service.Hold(ctx, "tenant-2", "doc-2", 4, "hold:doc-2", nil)
	if err != nil {
		t.Fatalf("hold: %v", err)
	}
	released, err := service.ReleaseHold(ctx, hold.ID)
	if err != nil || !released {
		t.Fatalf("release = %v, %v", released, err)
	}
	balance, _ := service.GetBalance(ctx, "tenant-2")
	if balance.ConsumedPlanPoints != 0 || balance.HeldPoints != 0 || balance.AvailablePlanPoints != 10 {
		t.Fatalf("balance after release = %+v", balance)
	}
	if again, _ := service.ReleaseHold(ctx, hold.ID); !again {
		t.Fatal("second release should be idempotent true")
	}
}

func TestHoldRejectsInsufficientPoints(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()

	_, err := service.Hold(ctx, "tenant-3", "doc-3", 11, "hold:doc-3", nil)
	var insufficient *InsufficientPointsError
	if !errors.As(err, &insufficient) {
		t.Fatalf("expected InsufficientPointsError, got %v", err)
	}
	if insufficient.Available != 10 || insufficient.Required != 11 {
		t.Fatalf("insufficient detail = %+v", insufficient)
	}
}

func TestRechargeThenHoldSpillsToAddon(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()

	if err := service.Recharge(ctx, "tenant-4", 5, "recharge:1", "test", nil); err != nil {
		t.Fatalf("recharge: %v", err)
	}
	hold, err := service.Hold(ctx, "tenant-4", "doc-4", 12, "hold:doc-4", nil)
	if err != nil {
		t.Fatalf("hold: %v", err)
	}
	if hold.PlanPoints != 10 || hold.AddonPoints != 2 {
		t.Fatalf("hold split = %d/%d, want 10/2", hold.PlanPoints, hold.AddonPoints)
	}
	balance, _ := service.GetBalance(ctx, "tenant-4")
	if balance.AddonPurchasedPoints != 5 || balance.ConsumedAddonPoints != 2 {
		t.Fatalf("balance = %+v", balance)
	}

	if _, err := service.Hold(ctx, "tenant-4", "doc-4b", 4, "hold:doc-4b", nil); err == nil {
		t.Fatal("expected insufficient points after spilling to addon")
	}
}

func TestRechargeIsIdempotentByLedgerKey(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()

	if err := service.Recharge(ctx, "tenant-5", 5, "recharge:dup", "", nil); err != nil {
		t.Fatalf("first recharge: %v", err)
	}
	if err := service.Recharge(ctx, "tenant-5", 5, "recharge:dup", "", nil); err != nil {
		t.Fatalf("second recharge: %v", err)
	}
	balance, _ := service.GetBalance(ctx, "tenant-5")
	if balance.AddonPurchasedPoints != 5 {
		t.Fatalf("duplicate recharge must not add points: got %d, want 5", balance.AddonPurchasedPoints)
	}
}

// retryOnContention retries a write that lost a SQLite lock race. Production
// databases serialise these transactions; SQLite reports the lost race as an
// error instead of blocking on lock upgrade.
func retryOnContention(fn func() (bool, error)) (bool, error) {
	var lastErr error
	for attempt := 0; attempt < 20; attempt++ {
		ok, err := fn()
		if err == nil {
			return ok, nil
		}
		if !strings.Contains(err.Error(), "database is locked") && !strings.Contains(err.Error(), "SQLITE_BUSY") {
			return ok, err
		}
		lastErr = err
		time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
	}
	return false, lastErr
}

func TestConcurrentFinalizeAppliesDeltaOnce(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	hold, err := service.Hold(ctx, "tenant-c1", "doc-c1", 3, "hold:c1", nil)
	if err != nil {
		t.Fatalf("hold: %v", err)
	}

	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := retryOnContention(func() (bool, error) { return service.CommitHold(ctx, hold.ID) })
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("commit hold: %v", err)
		}
	}

	balance, _ := service.GetBalance(ctx, "tenant-c1")
	if balance.ConsumedPlanPoints != 3 || balance.HeldPoints != 0 {
		t.Fatalf("balance after concurrent commit = %+v", balance)
	}
}

func TestConcurrentDuplicateRechargeAddsOnce(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	results := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := retryOnContention(func() (bool, error) {
				return true, service.Recharge(ctx, "tenant-c2", 5, "recharge:c2", "", nil)
			})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("recharge: %v", err)
		}
	}

	balance, _ := service.GetBalance(ctx, "tenant-c2")
	if balance.AddonPurchasedPoints != 5 {
		t.Fatalf("addon purchased = %d, want 5", balance.AddonPurchasedPoints)
	}
}
