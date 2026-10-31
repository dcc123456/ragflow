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

	"gorm.io/gorm"
)

// BillingUsageDAO reads the tenant resource counters used by quota checks.
type BillingUsageDAO struct{}

// NewBillingUsageDAO creates a billing usage DAO.
func NewBillingUsageDAO() *BillingUsageDAO {
	return &BillingUsageDAO{}
}

// TenantApps counts billable applications of a tenant:
// chat assistants, knowledge bases, agents, searches and memories.
func (dao *BillingUsageDAO) TenantApps(ctx context.Context, db *gorm.DB, tenantID string) (int64, error) {
	// user_canvas is keyed by user_id in the shared schema; the other
	// application tables use tenant_id.
	tables := []struct {
		name   string
		column string
	}{
		{"dialog", "tenant_id"},
		{"knowledgebase", "tenant_id"},
		{"user_canvas", "user_id"},
		{"search", "tenant_id"},
		{"memory", "tenant_id"},
	}
	var total int64
	for _, table := range tables {
		var count int64
		if err := db.WithContext(ctx).Table(table.name).Where(table.column+" = ?", tenantID).Count(&count).Error; err != nil {
			return 0, err
		}
		total += count
	}
	return total, nil
}

// TenantMembers counts billable tenant members (owner/admin/normal and active).
func (dao *BillingUsageDAO) TenantMembers(ctx context.Context, db *gorm.DB, tenantID string) (int64, error) {
	var count int64
	err := db.WithContext(ctx).Table("user_tenant").
		Where("tenant_id = ? AND ((status = ? AND role IN ?) OR status = ?)", tenantID, "1", []string{"owner", "admin", "normal"}, "2").
		Count(&count).Error
	return count, err
}

// TenantStorageBytes sums the size of non-folder files owned by the tenant.
func (dao *BillingUsageDAO) TenantStorageBytes(ctx context.Context, db *gorm.DB, tenantID string) (int64, error) {
	var total int64
	err := db.WithContext(ctx).Table("file").
		Select("COALESCE(SUM(size), 0)").
		Where("tenant_id = ? AND type <> ?", tenantID, "folder").
		Scan(&total).Error
	return total, err
}

// TenantAPITokenCount counts API tokens of a tenant.
func (dao *BillingUsageDAO) TenantAPITokenCount(ctx context.Context, db *gorm.DB, tenantID string) (int64, error) {
	var count int64
	err := db.WithContext(ctx).Table("api_token").Where("tenant_id = ?", tenantID).Count(&count).Error
	return count, err
}
