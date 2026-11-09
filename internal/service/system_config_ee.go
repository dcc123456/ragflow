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
