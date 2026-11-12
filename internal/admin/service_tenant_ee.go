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
	"gorm.io/gorm/clause"
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

func (s *Service) UpdateTenantRole(ctx context.Context, tenantID string, roleID *int32, actorUserID string) (map[string]interface{}, error) {
	actorUserID = strings.TrimSpace(actorUserID)
	if actorUserID == "" {
		return nil, common.NewCodedError(common.CodeUnauthorized, "Admin user is required")
	}
	if roleID == nil || *roleID <= 0 {
		return nil, common.NewCodedError(common.CodeBadRequest, "A Tenant system role is required")
	}
	var result map[string]interface{}
	err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tenant, err := s.tenantDAO.GetByIDForAdmin(ctx, tx, tenantID)
		if err != nil {
			if dao.IsNotFoundErr(err) {
				return common.NewCodedError(common.CodeNotFound, "Tenant not found")
			}
			return fmt.Errorf("load tenant: %w", err)
		}
		if roleID != nil {
			var role entity.Role
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id IS NULL", *roleID).Take(&role).Error; err != nil {
				if dao.IsNotFoundErr(err) {
					return common.NewCodedError(common.CodeNotFound, "System role not found")
				}
				return fmt.Errorf("load system role: %w", err)
			}
			result = map[string]interface{}{"tenant_id": tenantID, "role_id": role.ID, "role_name": role.RoleName}
		} else {
			result = map[string]interface{}{"tenant_id": tenantID, "role_id": nil, "role_name": nil}
		}
		if equalAdminRoleIDs(tenant.RoleID, roleID) {
			return nil
		}
		if err := tx.Model(&entity.Tenant{}).Where("id = ?", tenantID).Update("role_id", roleID).Error; err != nil {
			return fmt.Errorf("update tenant system role: %w", err)
		}
		beforeDataFields := map[string]interface{}{
			"tenant_id": tenantID,
			"role_id":   tenant.RoleID,
			"role_name": nil,
		}
		if tenant.RoleID != nil {
			var previousRole entity.Role
			if err := tx.Where("id = ? AND tenant_id IS NULL", *tenant.RoleID).Take(&previousRole).Error; err != nil {
				return fmt.Errorf("load previous Tenant system role: %w", err)
			}
			beforeDataFields["role_name"] = previousRole.RoleName
		}
		beforeData, err := json.Marshal(beforeDataFields)
		if err != nil {
			return fmt.Errorf("encode previous tenant role: %w", err)
		}
		afterData, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("encode updated tenant role: %w", err)
		}
		actorID := actorUserID
		status := string(entity.StatusValid)
		log := &entity.PermissionChangeLog{
			ID:           common.GenerateUUID(),
			TenantID:     tenantID,
			TargetType:   "tenant",
			TargetID:     tenantID,
			ResourceType: "tenant_role",
			ResourceID:   tenantID,
			ActionType:   "update",
			EventType:    "tenant.role.updated",
			ActorUserID:  &actorID,
			ScopeType:    "system",
			BeforeData:   stringPointer(string(beforeData)),
			AfterData:    stringPointer(string(afterData)),
			Status:       &status,
		}
		if err := tx.Create(log).Error; err != nil {
			return fmt.Errorf("record tenant role change: %w", err)
		}
		return nil
	})
	return result, err
}

// UpdateTenantStatus enables or disables a tenant and records the change.
func (s *Service) UpdateTenantStatus(ctx context.Context, tenantID, status, actorUserID string) (map[string]interface{}, error) {
	actorUserID = strings.TrimSpace(actorUserID)
	if actorUserID == "" {
		return nil, common.NewCodedError(common.CodeUnauthorized, "Admin user is required")
	}
	status = strings.TrimSpace(status)
	if status != string(entity.StatusValid) && status != string(entity.StatusInvalid) {
		return nil, common.NewCodedError(common.CodeBadRequest, "Tenant status must be 0 or 1")
	}

	var result map[string]interface{}
	err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tenant, err := s.tenantDAO.GetByIDForAdmin(ctx, tx, tenantID)
		if err != nil {
			if dao.IsNotFoundErr(err) {
				return common.NewCodedError(common.CodeNotFound, "Tenant not found")
			}
			return fmt.Errorf("load tenant: %w", err)
		}

		previousStatus := string(entity.StatusValid)
		if tenant.Status != nil {
			previousStatus = *tenant.Status
		}
		result = map[string]interface{}{"tenant_id": tenantID, "status": status}
		if previousStatus == status {
			return nil
		}
		if err := tx.Model(&entity.Tenant{}).Where("id = ?", tenantID).Update("status", status).Error; err != nil {
			return fmt.Errorf("update tenant status: %w", err)
		}

		beforeData, err := json.Marshal(map[string]string{"status": previousStatus})
		if err != nil {
			return fmt.Errorf("encode previous tenant status: %w", err)
		}
		afterData, err := json.Marshal(map[string]string{"status": status})
		if err != nil {
			return fmt.Errorf("encode updated tenant status: %w", err)
		}
		actorID := actorUserID
		logStatus := string(entity.StatusValid)
		log := &entity.PermissionChangeLog{
			ID:           common.GenerateUUID(),
			TenantID:     tenantID,
			TargetType:   "tenant",
			TargetID:     tenantID,
			ResourceType: "tenant",
			ResourceID:   tenantID,
			ActionType:   "update",
			EventType:    "tenant.status.updated",
			ActorUserID:  &actorID,
			ScopeType:    "system",
			BeforeData:   stringPointer(string(beforeData)),
			AfterData:    stringPointer(string(afterData)),
			Status:       &logStatus,
		}
		if err := tx.Create(log).Error; err != nil {
			return fmt.Errorf("record tenant status change: %w", err)
		}
		return nil
	})
	return result, err
}

func equalAdminRoleIDs(left, right *int32) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func stringPointer(value string) *string { return &value }

func (s *Service) ListPermissionLogs(ctx context.Context, filter dao.PermissionChangeLogFilter, page, pageSize int) (*PermissionLogListResult, error) {
	logs, total, err := dao.NewPermissionChangeLogDAO().ListPermissionChangeLogs(ctx, dao.DB, filter, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, err
	}
	return &PermissionLogListResult{Logs: logs, Total: total, Page: page, PageSize: pageSize}, nil
}
