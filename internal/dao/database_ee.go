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
	"ragflow/internal/entity"

	"gorm.io/gorm"
)

// permissionDataModelsEE returns the enterprise authorization entities.
func permissionDataModelsEE() []interface{} {
	return []interface{}{
		&entity.Role{},
		&entity.RoleResource{},
		&entity.RoleDefaultModel{},
		&entity.GroupInfo{},
		&entity.GroupMember{},
		&entity.Permission{},
		&entity.PermissionChangeLog{},
		&entity.TenantModelGroup{},
		&entity.TenantModelGroupMap{},
	}
}

func initializeSystemSettingsEE(ctx context.Context, db *gorm.DB) error {
	return ensureWhitelistSetting(ctx, db)
}
