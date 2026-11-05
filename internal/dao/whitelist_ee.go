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

//go:build ee

package dao

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"ragflow/internal/common"
	"ragflow/internal/entity"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const WhitelistEnabledSettingName = "enable_whitelist"

func ensureWhitelistSetting(ctx context.Context, db *gorm.DB) error {
	setting := entity.SystemSettings{Name: WhitelistEnabledSettingName, Source: "variable", DataType: "bool", Value: "false"}
	return db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&setting).Error
}

// WhitelistEnabled reads the current switch without caching admission decisions.
func WhitelistEnabled(ctx context.Context, db *gorm.DB) (bool, error) {
	var setting entity.SystemSettings
	err := db.WithContext(ctx).Where("name = ?", WhitelistEnabledSettingName).Take(&setting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read registration whitelist setting: %w", err)
	}
	switch strings.TrimSpace(setting.Value) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("invalid registration whitelist setting: expected true or false")
	}
}

// WhitelistContains uses database equality to match the admission list.
func WhitelistContains(ctx context.Context, db *gorm.DB, email string) (bool, error) {
	var entry entity.WhiteList
	err := db.WithContext(ctx).Select("email").Where("email = ?", email).Take(&entry).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return err == nil, err
}

// ensureWhitelistSchema prepares whitelist storage and leaves historical rows unchanged.
func ensureWhitelistSchema(ctx context.Context, db *gorm.DB) error {
	db = db.WithContext(ctx)
	if !db.Migrator().HasTable(&entity.WhiteList{}) {
		return db.Migrator().CreateTable(&entity.WhiteList{})
	}
	var entries []entity.WhiteList
	if err := db.Order("id ASC").Find(&entries).Error; err != nil {
		return err
	}
	for _, entry := range entries {
		if err := entity.ValidateWhitelistEmail(entry.Email); err != nil {
			common.Warn("Invalid whitelist email preserved; repair or remove the row by ID using the admin whitelist API",
				zap.Int64("id", entry.ID), zap.String("email", entry.Email), zap.Error(err))
		}
	}
	indexes, err := db.Migrator().GetIndexes(&entity.WhiteList{})
	if err != nil {
		return err
	}
	for _, index := range indexes {
		unique, _ := index.Unique()
		columns := index.Columns()
		if unique && len(columns) == 1 && columns[0] == "email" {
			return nil
		}
	}
	if err := db.Migrator().CreateIndex(&entity.WhiteList{}, "idx_white_list_email"); err != nil {
		return fmt.Errorf("create whitelist email unique index: %w; repair the conflicting rows and rerun --migrate; inspect conflicting rows with: %s", err,
			"SELECT id, email FROM white_list WHERE email IN (SELECT email FROM white_list GROUP BY email HAVING COUNT(*) > 1) ORDER BY id;")
	}
	return nil
}
