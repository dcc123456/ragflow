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

package permission

import (
	"context"
	"strconv"

	"gorm.io/gorm"
)

type tenantRoleRecord struct {
	ID       int64  `gorm:"column:id"`
	TenantID string `gorm:"column:tenant_id"`
}

func (tenantRoleRecord) TableName() string { return "role" }

func tenantRoleMembershipEnabled(db *gorm.DB) bool {
	if db == nil {
		return false
	}
	migrator := db.Migrator()
	return migrator.HasTable(&tenantRoleRecord{}) && migrator.HasColumn(&tenantRoleRecord{}, "TenantID")
}

func isTenantRoleID(ctx context.Context, db *gorm.DB, tenantID, roleValue string) (bool, error) {
	roleID, err := strconv.ParseInt(roleValue, 10, 64)
	if err != nil || roleID <= 0 || !tenantRoleMembershipEnabled(db) {
		return false, nil
	}
	var count int64
	if err := db.WithContext(ctx).Model(&tenantRoleRecord{}).
		Where("id = ? AND tenant_id = ?", roleID, tenantID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
