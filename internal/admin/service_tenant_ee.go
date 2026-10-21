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

package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"

	"gorm.io/gorm"
)

type TenantListResult struct {
	Tenants  []*dao.AdminTenantItem `json:"tenants"`
	Total    int64                  `json:"total"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"page_size"`
}

type PermissionLogListResult struct {
	Logs     []*entity.PermissionChangeLog `json:"logs"`
	Total    int64                         `json:"total"`
	Page     int                           `json:"page"`
	PageSize int                           `json:"page_size"`
}

func (s *Service) ListTenants(ctx context.Context, filter dao.AdminTenantFilter, page, pageSize int) (*TenantListResult, error) {
	tenants, total, err := s.tenantDAO.ListAllTenants(ctx, dao.DB, filter, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, err
	}
	return &TenantListResult{Tenants: tenants, Total: total, Page: page, PageSize: pageSize}, nil
}

func (s *Service) UpdateTenantInviteSetting(ctx context.Context, tenantID string, enabled bool, actorUserID string) (map[string]interface{}, error) {
	actorUserID = strings.TrimSpace(actorUserID)
	if actorUserID == "" {
		return nil, common.NewCodedError(common.CodeUnauthorized, "Admin user is required")
	}
	err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tenant, err := s.tenantDAO.GetByIDForAdmin(ctx, tx, tenantID)
		if err != nil {
			if dao.IsNotFoundErr(err) {
				return common.NewCodedError(common.CodeNotFound, "Tenant not found")
			}
			return fmt.Errorf("load tenant: %w", err)
		}
		if tenant.InviteEnabled == enabled {
			return nil
		}
		result := tx.Model(&entity.Tenant{}).Where("id = ?", tenantID).Update("invite_enabled", enabled)
		if result.Error != nil {
			return fmt.Errorf("update tenant invite setting: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return common.NewCodedError(common.CodeNotFound, "Tenant not found")
		}

		beforeData, err := json.Marshal(map[string]bool{"invite_enabled": tenant.InviteEnabled})
		if err != nil {
			return fmt.Errorf("encode previous invite setting: %w", err)
		}
		afterData, err := json.Marshal(map[string]bool{"invite_enabled": enabled})
		if err != nil {
			return fmt.Errorf("encode updated invite setting: %w", err)
		}
		actorID := actorUserID
		status := string(entity.StatusValid)
		log := &entity.PermissionChangeLog{
			ID:           common.GenerateUUID(),
			TenantID:     tenantID,
			OperatorID:   "",
			TargetType:   "tenant",
			TargetID:     tenantID,
			ResourceType: "tenant",
			ResourceID:   tenantID,
			ActionType:   "update",
			EventType:    "tenant.invite_setting.updated",
			ActorUserID:  &actorID,
			ScopeType:    "system",
			BeforeData:   stringPointer(string(beforeData)),
			AfterData:    stringPointer(string(afterData)),
			Status:       &status,
		}
		if err := tx.Create(log).Error; err != nil {
			return fmt.Errorf("record tenant invite setting change: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"tenant_id": tenantID, "invite_enabled": enabled}, nil
}

func (s *Service) GetDefaultTenantInviteEnabled(ctx context.Context) (bool, error) {
	return dao.GetDefaultTenantInviteEnabled(ctx, dao.DB)
}

func (s *Service) UpdateDefaultTenantInviteEnabled(ctx context.Context, enabled bool, actorUserID string) (map[string]bool, error) {
	actorUserID = strings.TrimSpace(actorUserID)
	if actorUserID == "" {
		return nil, common.NewCodedError(common.CodeUnauthorized, "Admin user is required")
	}
	err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		previous, err := dao.GetDefaultTenantInviteEnabled(ctx, tx)
		if err != nil {
			return err
		}
		if previous == enabled {
			return nil
		}
		if err := dao.SetDefaultTenantInviteEnabled(ctx, tx, enabled); err != nil {
			return fmt.Errorf("update default tenant invite setting: %w", err)
		}

		beforeData, err := json.Marshal(map[string]bool{"invite_enabled": previous})
		if err != nil {
			return fmt.Errorf("encode previous default invite setting: %w", err)
		}
		afterData, err := json.Marshal(map[string]bool{"invite_enabled": enabled})
		if err != nil {
			return fmt.Errorf("encode updated default invite setting: %w", err)
		}
		actorID := actorUserID
		status := string(entity.StatusValid)
		settingName := dao.DefaultTenantInviteEnabledSettingName
		log := &entity.PermissionChangeLog{
			ID:           common.GenerateUUID(),
			TargetType:   "system_setting",
			TargetID:     settingName,
			ResourceType: "system_setting",
			ResourceID:   settingName,
			ActionType:   "update",
			EventType:    "tenant.invite_default.updated",
			ActorUserID:  &actorID,
			ScopeType:    "system",
			BeforeData:   stringPointer(string(beforeData)),
			AfterData:    stringPointer(string(afterData)),
			Status:       &status,
		}
		if err := tx.Create(log).Error; err != nil {
			return fmt.Errorf("record default tenant invite setting change: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]bool{"invite_enabled": enabled}, nil
}

func stringPointer(value string) *string { return &value }

func (s *Service) ListPermissionLogs(ctx context.Context, filter dao.PermissionChangeLogFilter, page, pageSize int) (*PermissionLogListResult, error) {
	logs, total, err := dao.NewPermissionChangeLogDAO().ListPermissionChangeLogs(ctx, dao.DB, filter, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, err
	}
	return &PermissionLogListResult{Logs: logs, Total: total, Page: page, PageSize: pageSize}, nil
}
