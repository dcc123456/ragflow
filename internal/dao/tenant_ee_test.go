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
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"ragflow/internal/entity"
	"testing"
)

func tenantDAOTestExtraModels() []interface{} {
	return []interface{}{&entity.Role{}, &entity.RoleResource{}}
}

func TestTenantDAOCreateFailsWhenRoleTableIsMissingEE(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&entity.Tenant{}); err != nil {
		t.Fatalf("failed to migrate tenant: %v", err)
	}

	status := "1"
	tenant := &entity.Tenant{ID: "tenant-no-role-schema", Status: &status}
	if err := NewTenantDAO().Create(t.Context(), db, tenant); err == nil {
		t.Fatal("Create() error = nil, want missing role schema error")
	}

	var count int64
	if err := db.Model(&entity.Tenant{}).Where("id = ?", tenant.ID).Count(&count).Error; err != nil {
		t.Fatalf("count tenant rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("tenant count after failed role initialization = %d, want 0", count)
	}
}
