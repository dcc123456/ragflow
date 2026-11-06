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
	"testing"

	"ragflow/internal/entity"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestDefaultTenantInviteSettingSeedsAndAppliesToNewTenant(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&entity.SystemSettings{}, &entity.Tenant{}); err != nil {
		t.Fatalf("migrate tables: %v", err)
	}

	ctx := t.Context()
	enabled, err := GetDefaultTenantInviteEnabled(ctx, db)
	if err != nil {
		t.Fatalf("GetDefaultTenantInviteEnabled() error = %v", err)
	}
	if !enabled {
		t.Fatal("default invite_enabled = false, want true")
	}

	var setting entity.SystemSettings
	if err := db.Where("name = ?", DefaultTenantInviteEnabledSettingName).Take(&setting).Error; err != nil {
		t.Fatalf("load seeded setting: %v", err)
	}
	if setting.Source != "system" || setting.DataType != "bool" || setting.Value != "true" {
		t.Fatalf("seeded setting = %+v, want system bool true", setting)
	}

	if err := SetDefaultTenantInviteEnabled(ctx, db, false); err != nil {
		t.Fatalf("SetDefaultTenantInviteEnabled(false) error = %v", err)
	}
	newTenant := &entity.Tenant{ID: "tenant-new"}
	if err := ApplyDefaultTenantInviteEnabled(ctx, db, newTenant); err != nil {
		t.Fatalf("ApplyDefaultTenantInviteEnabled() error = %v", err)
	}
	if newTenant.InviteEnabled {
		t.Fatal("new tenant invite_enabled = true, want configured false")
	}

	// Re-running initialization must leave the administrator's value intact.
	if err := initializeSystemSettingsEE(ctx, db); err != nil {
		t.Fatalf("initializeSystemSettingsEE() error = %v", err)
	}
	enabled, err = GetDefaultTenantInviteEnabled(ctx, db)
	if err != nil {
		t.Fatalf("GetDefaultTenantInviteEnabled() after initialization error = %v", err)
	}
	if enabled {
		t.Fatal("default invite_enabled reset to true, want preserved false")
	}
}
