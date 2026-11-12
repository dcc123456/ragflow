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
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
)

func TestAddMemberRejectsWhenTenantRoleDisablesInvitations(t *testing.T) {
	db := setupTenantPermissionTestDB(t)
	if err := db.AutoMigrate(&entity.Tenant{}, &entity.Role{}, &entity.RoleResource{}); err != nil {
		t.Fatalf("migrate tenant table: %v", err)
	}
	seedTenantMembership(t, db, "owner", "owner-user", "tenant-xyz", TenantRoleOwner)
	status := "1"
	systemRoleID := int32(77)
	if err := db.Create(&entity.Role{ID: systemRoleID, RoleName: "tenant-system"}).Error; err != nil {
		t.Fatalf("seed Tenant system role: %v", err)
	}
	if err := db.Create(&entity.Tenant{
		ID: "tenant-xyz", ParserIDs: "", RoleID: &systemRoleID, Status: &status,
	}).Error; err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	s := &TenantService{userTenantDAO: dao.NewUserTenantDAO()}

	ctx := permission.WithIdentity(t.Context(), "owner-user", "tenant-xyz")
	_, code, err := s.AddMember(ctx, "tenant-xyz", &AddMemberRequest{Email: "invitee@example.com"})
	if err == nil || code != common.CodeForbidden {
		t.Fatalf("AddMember() = (%v, %v), want forbidden because the Tenant system role does not enable invitations", code, err)
	}
	var count int64
	if err := db.Model(&entity.UserTenant{}).Where("tenant_id = ? AND status = ?", "tenant-xyz", entity.TenantMembershipStatusInvited).Count(&count).Error; err != nil {
		t.Fatalf("count invitations: %v", err)
	}
	if count != 0 {
		t.Fatalf("pending invitation count = %d, want 0", count)
	}
}
