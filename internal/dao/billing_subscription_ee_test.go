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
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"ragflow/internal/entity"
)

const testGB = int64(1024 * 1024 * 1024)

func setupBillingSubscriptionDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&entity.BillingSubscription{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func seedSubscription(t *testing.T, db *gorm.DB, tenantID string) {
	t.Helper()
	start := time.Unix(1700000000, 0).UTC()
	row := &entity.BillingSubscription{
		ID:                 "row-" + tenantID,
		TenantID:           tenantID,
		PlanName:           "Starter",
		Status:             "active",
		SubscriptionStatus: "active",
		SubscriptionID:     "sub_" + tenantID,
		CustomerID:         "cus_" + tenantID,
		StartTime:          &start,
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
}

// TestUpdateStorageAddonKeepsConvergedColumns covers the race the API used to
// have: it wrote the add-on from a row snapshot taken before the Stripe call,
// so a webhook that converged the plan in between was rolled back.
func TestUpdateStorageAddonKeepsConvergedColumns(t *testing.T) {
	db := setupBillingSubscriptionDB(t)
	ctx := context.Background()
	tenantID := "tenant-addon"
	seedSubscription(t, db, tenantID)

	// The webhook converges the plan while the API call is in flight.
	if err := db.Model(&entity.BillingSubscription{}).
		Where("tenant_id = ?", tenantID).
		Updates(map[string]interface{}{"plan_name": "Pro", "status": "past_due"}).Error; err != nil {
		t.Fatalf("converge: %v", err)
	}

	dao := NewBillingSubscriptionDAO()
	itemID := "si_storage"
	if err := dao.UpdateStorageAddon(ctx, db, tenantID, &itemID, 2*testGB, nil); err != nil {
		t.Fatalf("update addon: %v", err)
	}

	got, err := dao.GetByTenantID(ctx, db, tenantID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.PlanName != "Pro" || got.Status != "past_due" {
		t.Fatalf("converged columns rolled back: plan=%q status=%q", got.PlanName, got.Status)
	}
	if got.AddonStorageBytes == nil || *got.AddonStorageBytes != 2*testGB {
		t.Fatalf("addon bytes = %v, want %d", got.AddonStorageBytes, 2*testGB)
	}
	if got.AddonSubscriptionItemID == nil || *got.AddonSubscriptionItemID != itemID {
		t.Fatalf("addon item id = %v", got.AddonSubscriptionItemID)
	}
	if got.TargetStorageBytes != nil {
		t.Fatalf("pending target must be cleared once the change is live: %v", *got.TargetStorageBytes)
	}
}

// TestUpdatePendingStorageTargetLeavesAddon checks that scheduling a downgrade
// only records the pending target and never touches the active add-on.
func TestUpdatePendingStorageTargetLeavesAddon(t *testing.T) {
	db := setupBillingSubscriptionDB(t)
	ctx := context.Background()
	tenantID := "tenant-target"
	seedSubscription(t, db, tenantID)
	if err := db.Model(&entity.BillingSubscription{}).
		Where("tenant_id = ?", tenantID).
		Updates(map[string]interface{}{"addon_storage_bytes": testGB, "schedule_id": "sub_sched_1"}).Error; err != nil {
		t.Fatalf("seed addon: %v", err)
	}

	dao := NewBillingSubscriptionDAO()
	target := int64(0)
	if err := dao.UpdatePendingStorageTarget(ctx, db, tenantID, &target, true); err != nil {
		t.Fatalf("update target: %v", err)
	}

	got, err := dao.GetByTenantID(ctx, db, tenantID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.AddonStorageBytes == nil || *got.AddonStorageBytes != testGB {
		t.Fatalf("active add-on must stay until the period ends: %v", got.AddonStorageBytes)
	}
	if got.TargetStorageBytes == nil || *got.TargetStorageBytes != 0 {
		t.Fatalf("pending target = %v, want 0", got.TargetStorageBytes)
	}
	if got.ScheduleID != nil {
		t.Fatalf("schedule id should be cleared with the pending change: %v", *got.ScheduleID)
	}
	if got.PlanName != "Starter" {
		t.Fatalf("plan changed: %q", got.PlanName)
	}
}

// TestUpdatePendingPlanChangeKeepsConvergedColumns checks that scheduling a
// downgrade writes only the pending columns.
func TestUpdatePendingPlanChangeKeepsConvergedColumns(t *testing.T) {
	db := setupBillingSubscriptionDB(t)
	ctx := context.Background()
	tenantID := "tenant-pending"
	seedSubscription(t, db, tenantID)
	if err := db.Model(&entity.BillingSubscription{}).
		Where("tenant_id = ?", tenantID).
		Updates(map[string]interface{}{"plan_name": "Pro", "status": "past_due", "addon_storage_bytes": testGB}).Error; err != nil {
		t.Fatalf("converge: %v", err)
	}

	dao := NewBillingSubscriptionDAO()
	target := "Starter"
	scheduleID := "sub_sched_pending"
	if err := dao.UpdatePendingPlanChange(ctx, db, tenantID, &target, &scheduleID); err != nil {
		t.Fatalf("update pending plan: %v", err)
	}

	got, err := dao.GetByTenantID(ctx, db, tenantID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.PlanName != "Pro" || got.Status != "past_due" {
		t.Fatalf("converged columns rolled back: plan=%q status=%q", got.PlanName, got.Status)
	}
	if got.AddonStorageBytes == nil || *got.AddonStorageBytes != testGB {
		t.Fatalf("add-on must be untouched: %v", got.AddonStorageBytes)
	}
	if got.TargetPlanName == nil || *got.TargetPlanName != target {
		t.Fatalf("target plan = %v, want %s", got.TargetPlanName, target)
	}
	if got.ScheduleID == nil || *got.ScheduleID != scheduleID {
		t.Fatalf("schedule id = %v, want %s", got.ScheduleID, scheduleID)
	}
}

// TestUpdatePendingTrialFallbackClearsPendingState checks the fallback drops the
// released schedule and the pending storage target, and nothing else.
func TestUpdatePendingTrialFallbackClearsPendingState(t *testing.T) {
	db := setupBillingSubscriptionDB(t)
	ctx := context.Background()
	tenantID := "tenant-fallback"
	seedSubscription(t, db, tenantID)
	targetStorage := int64(2 * testGB)
	if err := db.Model(&entity.BillingSubscription{}).
		Where("tenant_id = ?", tenantID).
		Updates(map[string]interface{}{
			"plan_name":            "Pro",
			"addon_storage_bytes":  testGB,
			"target_storage_bytes": targetStorage,
			"schedule_id":          "sub_sched_old",
			"target_plan_name":     "Starter",
		}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	dao := NewBillingSubscriptionDAO()
	if err := dao.UpdatePendingTrialFallback(ctx, db, tenantID, "Trial"); err != nil {
		t.Fatalf("update fallback: %v", err)
	}

	got, err := dao.GetByTenantID(ctx, db, tenantID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.PlanName != "Pro" {
		t.Fatalf("current plan must stay until Stripe cancels: %q", got.PlanName)
	}
	if got.AddonStorageBytes == nil || *got.AddonStorageBytes != testGB {
		t.Fatalf("add-on must be untouched until the deletion webhook: %v", got.AddonStorageBytes)
	}
	if got.TargetPlanName == nil || *got.TargetPlanName != "Trial" {
		t.Fatalf("target plan = %v, want Trial", got.TargetPlanName)
	}
	if got.ScheduleID != nil {
		t.Fatalf("schedule id should be cleared: %v", *got.ScheduleID)
	}
	if got.TargetStorageBytes != nil {
		t.Fatalf("pending storage target should be cleared: %v", *got.TargetStorageBytes)
	}
}
