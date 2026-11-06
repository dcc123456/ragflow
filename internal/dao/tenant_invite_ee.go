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
	"fmt"
	"strconv"
	"strings"

	"ragflow/internal/entity"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const DefaultTenantInviteEnabledSettingName = "tenant.default_invite_enabled"

func ensureDefaultTenantInviteEnabled(ctx context.Context, db *gorm.DB) error {
	setting := entity.SystemSettings{
		Name:     DefaultTenantInviteEnabledSettingName,
		Source:   "system",
		DataType: "bool",
		Value:    "true",
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&setting).Error
}

// GetDefaultTenantInviteEnabled returns the system default, creating it as
// enabled when the setting has not been initialized yet.
func GetDefaultTenantInviteEnabled(ctx context.Context, db *gorm.DB) (bool, error) {
	if err := ensureDefaultTenantInviteEnabled(ctx, db); err != nil {
		return false, fmt.Errorf("ensure default tenant invite setting: %w", err)
	}
	var setting entity.SystemSettings
	if err := db.WithContext(ctx).Where("name = ?", DefaultTenantInviteEnabledSettingName).Take(&setting).Error; err != nil {
		return false, fmt.Errorf("load default tenant invite setting: %w", err)
	}
	value := strings.TrimSpace(setting.Value)
	if value != "true" && value != "false" {
		return false, fmt.Errorf("invalid %s value %q: expected true or false", DefaultTenantInviteEnabledSettingName, setting.Value)
	}
	return value == "true", nil
}

// SetDefaultTenantInviteEnabled stores the value controlled by the system admin.
func SetDefaultTenantInviteEnabled(ctx context.Context, db *gorm.DB, enabled bool) error {
	value := strconv.FormatBool(enabled)
	setting := entity.SystemSettings{
		Name:     DefaultTenantInviteEnabledSettingName,
		Source:   "admin",
		DataType: "bool",
		Value:    value,
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"source", "data_type", "value", "update_time", "update_date"}),
	}).Create(&setting).Error
}

// ApplyDefaultTenantInviteEnabled initializes a newly-created tenant from the
// system setting. The tenant and setting must be read through the same DB/tx.
func ApplyDefaultTenantInviteEnabled(ctx context.Context, db *gorm.DB, tenant *entity.Tenant) error {
	enabled, err := GetDefaultTenantInviteEnabled(ctx, db)
	if err != nil {
		return err
	}
	tenant.InviteEnabled = enabled
	return nil
}
