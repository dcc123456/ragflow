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

//go:build ee

package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
)

type memoryTenantInviteLinkStore struct {
	values map[string]string
	ttls   map[string]time.Duration
}

func newMemoryTenantInviteLinkStore() *memoryTenantInviteLinkStore {
	return &memoryTenantInviteLinkStore{values: map[string]string{}, ttls: map[string]time.Duration{}}
}

func (s *memoryTenantInviteLinkStore) Get(_ context.Context, key string) (string, error) {
	return s.values[key], nil
}

func (s *memoryTenantInviteLinkStore) SetNX(_ context.Context, key, value string, ttl time.Duration) (bool, error) {
	if _, exists := s.values[key]; exists {
		return false, nil
	}
	s.values[key] = value
	s.ttls[key] = ttl
	return true, nil
}

func (s *memoryTenantInviteLinkStore) Delete(_ context.Context, key string) bool {
	delete(s.values, key)
	delete(s.ttls, key)
	return true
}

func TestTenantInviteLinkCanBeUsedByMultipleUsersForSevenDays(t *testing.T) {
	db := setupTenantRoleTestDB(t)
	tenantID := "tenant-link"
	tenantName := "Link Tenant"
	status := string(entity.StatusValid)
	roleID := int32(45)
	if err := db.Create(&entity.Role{ID: roleID, RoleName: "member", TenantID: &tenantID}).Error; err != nil {
		t.Fatalf("create Tenant default role: %v", err)
	}
	if err := db.Create(&entity.Tenant{
		ID: tenantID, Name: &tenantName, ParserIDs: "",
		InviteEnabled: true, RoleID: &roleID, Status: &status,
	}).Error; err != nil {
		t.Fatalf("create Tenant: %v", err)
	}
	seedTenantRoleMember(t, db, "owner-link", "owner-user", tenantID, TenantRoleOwner)
	if err := db.Create(&entity.User{ID: "first-user", Email: "first@example.com", Nickname: "First"}).Error; err != nil {
		t.Fatalf("create first invitee: %v", err)
	}
	if err := db.Create(&entity.User{ID: "second-user", Email: "second@example.com", Nickname: "Second"}).Error; err != nil {
		t.Fatalf("create second invitee: %v", err)
	}

	store := newMemoryTenantInviteLinkStore()
	svc := NewTenantService()
	createdAt := time.Now().UTC()
	ownerCtx := permission.WithIdentity(t.Context(), "owner-user", tenantID)
	link, err := svc.CreateTenantInviteLinkEE(ownerCtx, "owner-user", tenantID, store)
	if err != nil {
		t.Fatalf("CreateTenantInviteLinkEE() error = %v", err)
	}
	if !validTenantInviteToken(link.Token) || link.TenantID != tenantID {
		t.Fatalf("created invitation link = %#v", link)
	}
	if delta := link.ExpiresAt.Sub(createdAt.Add(tenantInviteLinkTTL)); delta < -time.Minute || delta > time.Minute {
		t.Fatalf("link expiration delta from seven days = %s", delta)
	}
	key := tenantInviteLinkKey(link.Token)
	if store.ttls[key] != tenantInviteLinkTTL {
		t.Fatalf("stored TTL = %s, want %s", store.ttls[key], tenantInviteLinkTTL)
	}
	stored := store.values[key]
	var payload tenantInviteLinkPayload
	if err := json.Unmarshal([]byte(stored), &payload); err != nil {
		t.Fatalf("decode stored invite link: %v", err)
	}
	if payload.TenantID != tenantID || payload.InviterID != "owner-user" {
		t.Fatalf("stored invitation payload = %#v", payload)
	}

	info, err := svc.GetTenantInviteLinkInfoEE(ownerCtx, link.Token, store)
	if err != nil || info.TenantName != tenantName || info.Inviter != "owner-user" {
		t.Fatalf("GetTenantInviteLinkInfoEE() = (%#v, %v)", info, err)
	}
	for _, userID := range []string{"first-user", "second-user"} {
		inviteeCtx := permission.WithIdentity(t.Context(), userID, tenantID)
		if err := svc.AcceptTenantInviteLinkEE(inviteeCtx, userID, link.Token, store); err != nil {
			t.Fatalf("AcceptTenantInviteLinkEE(%q) error = %v", userID, err)
		}
		var membership entity.UserTenant
		if err := db.Where("user_id = ? AND tenant_id = ?", userID, tenantID).Take(&membership).Error; err != nil {
			t.Fatalf("load membership for %q: %v", userID, err)
		}
		if membership.Role != "45" || membership.InvitedBy != "owner-user" || membership.Status == nil || *membership.Status != entity.TenantMembershipStatusJoined {
			t.Fatalf("membership for %q = %#v", userID, membership)
		}
	}
	if store.values[key] != stored {
		t.Fatal("accepting the link consumed it; the link must remain reusable until expiry")
	}
	var auditCount int64
	if err := db.Model(&entity.PermissionChangeLog{}).
		Where("event_type IN ?", []string{"tenant.invitation_link.created", "tenant.invitation_link.accepted"}).Count(&auditCount).Error; err != nil {
		t.Fatalf("count invitation link audit events: %v", err)
	}
	if auditCount != 3 {
		t.Fatalf("invitation link audit events = %d, want create plus two accepts", auditCount)
	}
	var audit entity.PermissionChangeLog
	if err := db.Where("event_type = ?", "tenant.invitation_link.created").Take(&audit).Error; err != nil {
		t.Fatalf("load invitation link creation audit: %v", err)
	}
	if audit.ResourceID == link.Token || strings.Contains(valueOrEmpty(audit.AfterData), link.Token) {
		t.Fatal("audit log contains the raw invitation token")
	}
	if len(audit.ResourceID) > 32 || audit.ResourceID == "" {
		t.Fatalf("invitation link audit resource ID length = %d, want 1..32", len(audit.ResourceID))
	}
}

func TestTenantInviteLinkAcceptanceRejectsDisabledTenant(t *testing.T) {
	db := setupTenantRoleTestDB(t)
	tenantID := "tenant-disabled-link"
	status := string(entity.StatusValid)
	roleID := int32(46)
	if err := db.Create(&entity.Role{ID: roleID, RoleName: "member", TenantID: &tenantID}).Error; err != nil {
		t.Fatalf("create Tenant default role: %v", err)
	}
	if err := db.Create(&entity.Tenant{
		ID: tenantID, ParserIDs: "",
		InviteEnabled: false, RoleID: &roleID, Status: &status,
	}).Error; err != nil {
		t.Fatalf("create Tenant: %v", err)
	}
	token, err := newTenantInviteToken()
	if err != nil {
		t.Fatalf("generate invitation token: %v", err)
	}
	payload, err := json.Marshal(tenantInviteLinkPayload{TenantID: tenantID, InviterID: "inviter", ExpiresAt: time.Now().UTC().Add(tenantInviteLinkTTL)})
	if err != nil {
		t.Fatalf("encode invitation token: %v", err)
	}
	store := newMemoryTenantInviteLinkStore()
	store.values[tenantInviteLinkKey(token)] = string(payload)
	svc := NewTenantService()
	if err := svc.AcceptTenantInviteLinkEE(permission.WithIdentity(t.Context(), "invitee", tenantID), "invitee", token, store); err == nil {
		t.Fatal("AcceptTenantInviteLinkEE() succeeded for disabled Tenant")
	} else {
		var coded *common.CodedError
		if !errors.As(err, &coded) || coded.Code != common.CodeForbidden {
			t.Fatalf("AcceptTenantInviteLinkEE() error = %v, want forbidden disabled Tenant rejection", err)
		}
	}
	var count int64
	if err := db.Model(&entity.UserTenant{}).Where("tenant_id = ?", tenantID).Count(&count).Error; err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	if count != 0 {
		t.Fatalf("membership count = %d, want no membership for disabled Tenant", count)
	}
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
