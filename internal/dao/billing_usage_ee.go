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
