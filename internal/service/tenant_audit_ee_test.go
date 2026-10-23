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

package service

import (
	"testing"

	"ragflow/internal/entity"

	"gorm.io/gorm"
)

//func TestTenantSelfRemovalAuditUsesMembershipIDEE(t *testing.T) {
//	db := setupTenantPermissionTestDB(t)
//	seedTenantMembership(t, db, "member-link", "member-user", "tenant-1", "normal")
//	s := &TenantService{userTenantDAO: dao.NewUserTenantDAO()}
//
//	code, err := s.RemoveMember(t.Context(), "member-user", "tenant-1", "member-user")
//	if err != nil || code != common.CodeSuccess {
//		t.Fatalf("RemoveMember(self) = (%v, %v), want success", code, err)
//	}
//	assertTenantAuditOperatorEE(t, db, "tenant.member.removed", "member-user", "member-link", "member-user")
//}

//func TestTenantInviteDeclineAuditUsesMembershipIDEE(t *testing.T) {
//	db := setupTenantPermissionTestDB(t)
//	seedTenantMembership(t, db, "invite-link", "invitee-user", "tenant-1", TenantRoleInvite)
//	s := &TenantService{userTenantDAO: dao.NewUserTenantDAO()}
//
//	code, err := s.RemoveMember(t.Context(), "invitee-user", "tenant-1", "invitee-user")
//	if err != nil || code != common.CodeSuccess {
//		t.Fatalf("RemoveMember(invitation self-decline) = (%v, %v), want success", code, err)
//	}
//	assertTenantAuditOperatorEE(t, db, "tenant.invitation.declined", "invitee-user", "invite-link", "invitee-user")
//}

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
