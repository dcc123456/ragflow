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

	"gorm.io/gorm"
)

func TestTenantSelfRemovalAuditUsesMembershipIDEE(t *testing.T) {
	db := setupTenantPermissionTestDB(t)
	seedTenantMembership(t, db, "member-link", "member-user", "tenant-1", "normal")
	s := &TenantService{userTenantDAO: dao.NewUserTenantDAO()}

	ctx := permission.WithIdentity(t.Context(), "member-user", "tenant-1")
	code, err := s.RemoveMember(ctx, "tenant-1", "member-user")
	if err != nil || code != common.CodeSuccess {
		t.Fatalf("RemoveMember(self) = (%v, %v), want success", code, err)
	}
	assertTenantAuditOperatorEE(t, db, "tenant.member.left", "member-user", "member-link", "member-user")
}

func TestTenantInviteDeclineAuditUsesMembershipIDEE(t *testing.T) {
	db := setupTenantPermissionTestDB(t)
	status := entity.TenantMembershipStatusInvited
	if err := db.Create(&entity.UserTenant{ID: "invite-link", UserID: "invitee-user", TenantID: "tenant-1", Status: &status}).Error; err != nil {
		t.Fatalf("seed pending Tenant invitation: %v", err)
	}
	s := &TenantService{userTenantDAO: dao.NewUserTenantDAO()}

	ctx := permission.WithIdentity(t.Context(), "invitee-user", "tenant-1")
	code, err := s.RemoveMember(ctx, "tenant-1", "invitee-user")
	if err != nil || code != common.CodeSuccess {
		t.Fatalf("RemoveMember(invitation self-decline) = (%v, %v), want success", code, err)
	}
	assertTenantAuditOperatorEE(t, db, "tenant.invitation.declined", "invitee-user", "invite-link", "invitee-user")
}

func assertTenantAuditOperatorEE(t *testing.T, db *gorm.DB, eventType, targetID, membershipID, userID string) {
	t.Helper()
	var changeLog entity.PermissionChangeLog
	if err := db.Where("event_type = ? AND target_id = ?", eventType, targetID).Take(&changeLog).Error; err != nil {
		t.Fatalf("load Tenant audit log %q for %q: %v", eventType, targetID, err)
	}
	if changeLog.OperatorID != membershipID || changeLog.ActorUserID == nil || *changeLog.ActorUserID != userID {
		t.Fatalf("Tenant audit operator = (%q, %v), want membership ID %q and user ID %q", changeLog.OperatorID, changeLog.ActorUserID, membershipID, userID)
	}
}
