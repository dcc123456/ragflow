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
	"errors"
	"fmt"
	"strconv"
	"strings"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

type configExtras struct {
	EmailVerificationEnabled bool `json:"emailVerificationEnabled"`
}

func emailVerificationEnabled(ctx context.Context, db *gorm.DB) (bool, error) {
	if db == nil {
		return false, fmt.Errorf("database is unavailable")
	}
	var setting entity.SystemSettings
	err := db.WithContext(ctx).Where("name = ?", "email_verification.enabled").Take(&setting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	enabled, err := strconv.ParseBool(strings.TrimSpace(setting.Value))
	if err != nil {
		return false, fmt.Errorf("invalid email verification setting")
	}
	return enabled, nil
}

func loadConfigExtras(ctx context.Context) (configExtras, error) {
	enabled, err := emailVerificationEnabled(ctx, dao.DB)
	if err != nil {
		// Keep public configuration available; registration checks the setting separately.
		common.Warn("Failed to load public email verification configuration", zap.Error(err))
		return configExtras{}, nil
	}
	return configExtras{EmailVerificationEnabled: enabled}, nil
}
