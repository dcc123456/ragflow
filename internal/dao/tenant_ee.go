//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

//go:build ee

package dao

import (
	"context"
	"fmt"
	"strings"

	"ragflow/internal/entity"

	"gorm.io/gorm"
)

// AdminTenantFilter narrows the system administrator's tenant list.
type AdminTenantFilter struct {
	Keyword       string
	Status        string
	InviteEnabled *bool
}

// AdminTenantItem is the tenant data exposed by the Admin API.
type AdminTenantItem struct {
	TenantID      string  `gorm:"column:tenant_id" json:"tenant_id"`
	Name          *string `gorm:"column:name" json:"name,omitempty"`
	OwnerUserID   *string `gorm:"column:owner_user_id" json:"owner_user_id,omitempty"`
	OwnerEmail    *string `gorm:"column:owner_email" json:"owner_email,omitempty"`
	OwnerNickname *string `gorm:"column:owner_nickname" json:"owner_nickname,omitempty"`
	InviteEnabled bool    `gorm:"column:invite_enabled" json:"invite_enabled"`
	RoleID        *int32  `gorm:"column:role_id" json:"role_id,omitempty"`
	Status        *string `gorm:"column:status" json:"status,omitempty"`
	CreateTime    *int64  `gorm:"column:create_time" json:"create_time,omitempty"`
}

// CountAllTenants counts all tenants.
func (dao *TenantDAO) CountAllTenants(ctx context.Context, db *gorm.DB) (int64, error) {
	var total int64
	err := db.WithContext(ctx).Model(&entity.Tenant{}).Count(&total).Error
	return total, err
}

// ListAllTenants returns tenant records with their owner and a total count.
func (dao *TenantDAO) ListAllTenants(ctx context.Context, db *gorm.DB, filter AdminTenantFilter, offset, limit int) ([]*AdminTenantItem, int64, error) {
	query := db.WithContext(ctx).Model(&entity.Tenant{}).
		Joins("LEFT JOIN user_tenant AS owner_membership ON owner_membership.tenant_id = tenant.id AND owner_membership.role = ? AND owner_membership.status = ?", "owner", "1").
		Joins("LEFT JOIN user AS tenant_owner ON tenant_owner.id = owner_membership.user_id")
	if filter.Status != "" {
		query = query.Where("tenant.status = ?", filter.Status)
	}
	if filter.InviteEnabled != nil {
		query = query.Where("tenant.invite_enabled = ?", *filter.InviteEnabled)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("(tenant.id LIKE ? OR tenant.name LIKE ? OR tenant_owner.email LIKE ? OR tenant_owner.nickname LIKE ?)", like, like, like, like)
	}

	var total int64
	if err := query.Distinct("tenant.id").Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count tenants: %w", err)
	}
	items := make([]*AdminTenantItem, 0)
	err := query.Select("tenant.id AS tenant_id, tenant.name, owner_membership.user_id AS owner_user_id, tenant_owner.email AS owner_email, tenant_owner.nickname AS owner_nickname, tenant.invite_enabled, tenant.role_id, tenant.status, tenant.create_time").
		Order("tenant.create_time DESC").Order("tenant.id ASC").Offset(offset).Limit(limit).Scan(&items).Error
	if err != nil {
		return nil, 0, fmt.Errorf("list tenants: %w", err)
	}
	return items, total, nil
}

// GetByIDForAdmin loads a tenant regardless of its active status.
func (dao *TenantDAO) GetByIDForAdmin(ctx context.Context, db *gorm.DB, id string) (*entity.Tenant, error) {
	var tenant entity.Tenant
	if err := db.WithContext(ctx).Where("id = ?", id).Take(&tenant).Error; err != nil {
		return nil, err
	}
	return &tenant, nil
}
