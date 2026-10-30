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

//go:build ee

package service

import (
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
)

func TestAddMemberRejectsWhenTenantInvitesAreDisabled(t *testing.T) {
	db := setupTenantPermissionTestDB(t)
	if err := db.AutoMigrate(&entity.Tenant{}); err != nil {
		t.Fatalf("migrate tenant table: %v", err)
	}
	seedTenantMembership(t, db, "owner", "owner-user", "tenant-xyz", TenantRoleOwner)
	status := "1"
	if err := db.Create(&entity.Tenant{
		ID: "tenant-xyz", ParserIDs: "", InviteEnabled: false, Status: &status,
	}).Error; err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	s := &TenantService{userTenantDAO: dao.NewUserTenantDAO(), tenantDAO: dao.NewTenantDAO()}

	_, code, err := s.AddMember(t.Context(), "owner-user", "tenant-xyz", &AddMemberRequest{Email: "invitee@example.com"})
	if err == nil || code != common.CodeForbidden {
		t.Fatalf("AddMember() = (%v, %v), want invitation disabled forbidden", code, err)
	}
	var count int64
	if err := db.Model(&entity.UserTenant{}).Where("tenant_id = ? AND status = ?", "tenant-xyz", entity.TenantMembershipStatusInvited).Count(&count).Error; err != nil {
		t.Fatalf("count invitations: %v", err)
	}
	if count != 0 {
		t.Fatalf("pending invitation count = %d, want 0", count)
	}
}
