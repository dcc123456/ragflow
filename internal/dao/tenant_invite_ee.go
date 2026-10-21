//
// Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

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
