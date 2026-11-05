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
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	tenantInviteLinkKeyPrefix = "ragflow:tenant:invite-link:"
	tenantInviteLinkTTL       = 7 * 24 * time.Hour
)

type tenantInviteLinkStore interface {
	Get(context.Context, string) (string, error)
	SetNX(context.Context, string, string, time.Duration) (bool, error)
	Delete(context.Context, string) bool
}

type tenantInviteLinkPayload struct {
	TenantID  string    `json:"tenant_id"`
	InviterID string    `json:"inviter_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type TenantInviteLink struct {
	Token     string    `json:"token"`
	TenantID  string    `json:"tenant_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type TenantInviteLinkInfo struct {
	TenantID   string    `json:"tenant_id"`
	TenantName string    `json:"tenant_name"`
	Inviter    string    `json:"inviter"`
	ExpiresAt  time.Time `json:"expires_at"`
}

func (s *TenantService) CreateTenantInviteLinkEE(ctx context.Context, userID, tenantID string, store tenantInviteLinkStore) (*TenantInviteLink, error) {
	if store == nil {
		return nil, common.NewCodedError(common.CodeServerError, "Tenant invitation link storage is unavailable")
	}
	if _, _, err := tenantMemberPermission(ctx, dao.DB.WithContext(ctx), permission.TenantIDFromContext(ctx), entity.TenantPermissionManageMembers); err != nil {
		return nil, common.NewCodedError(tenantRoleErrorCode(err), err.Error())
	}
	if code, err := s.checkTenantInviteEnabled(ctx, tenantID); err != nil {
		return nil, common.NewCodedError(code, err.Error())
	}

	for range 3 {
		token, err := newTenantInviteToken()
		if err != nil {
			return nil, fmt.Errorf("generate Tenant invitation token: %w", err)
		}
		expiresAt := time.Now().UTC().Add(tenantInviteLinkTTL)
		payload := tenantInviteLinkPayload{TenantID: tenantID, InviterID: userID, ExpiresAt: expiresAt}
		value, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode Tenant invitation link: %w", err)
		}
		key := tenantInviteLinkKey(token)
		created, err := store.SetNX(ctx, key, string(value), tenantInviteLinkTTL)
		if err != nil {
			return nil, fmt.Errorf("store Tenant invitation link: %w", err)
		}
		if !created {
			continue
		}

		tokenHash := hashTenantInviteToken(token)
		err = dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return recordTenantPermissionChange(ctx, tx, permission.TenantIDFromContext(ctx),
				"tenant.invitation_link.created", "tenant", tenantID,
				"tenant_invitation_link", tokenHash[:32], "create", 0, 0, nil,
				map[string]interface{}{"expires_at": expiresAt, "reusable": true})
		})
		if err != nil {
			store.Delete(ctx, key)
			return nil, fmt.Errorf("audit Tenant invitation link: %w", err)
		}
		return &TenantInviteLink{Token: token, TenantID: tenantID, ExpiresAt: expiresAt}, nil
	}
	return nil, common.NewCodedError(common.CodeConflict, "could not create a unique Tenant invitation link")
}

func (s *TenantService) GetTenantInviteLinkInfoEE(ctx context.Context, token string, store tenantInviteLinkStore) (*TenantInviteLinkInfo, error) {
	payload, err := loadTenantInviteLink(ctx, token, store)
	if err != nil {
		return nil, err
	}
	var tenant entity.Tenant
	if err := dao.DB.WithContext(ctx).Select("id", "name", "invite_enabled").Where("id = ?", payload.TenantID).Take(&tenant).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewCodedError(common.CodeNotFound, "Tenant invitation link is invalid or expired")
		}
		return nil, fmt.Errorf("load Tenant invitation target: %w", err)
	}
	if !tenant.InviteEnabled {
		return nil, common.NewCodedError(common.CodeNotFound, "Tenant invitation link is invalid or expired")
	}
	var inviter entity.User
	if err := dao.DB.WithContext(ctx).Select("id", "nickname", "email").Where("id = ?", payload.InviterID).Take(&inviter).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("load Tenant invitation sender: %w", err)
	}
	inviterName := strings.TrimSpace(inviter.Nickname)
	if inviterName == "" {
		inviterName = inviter.Email
	}
	tenantName := ""
	if tenant.Name != nil {
		tenantName = *tenant.Name
	}
	return &TenantInviteLinkInfo{
		TenantID:   tenant.ID,
		TenantName: tenantName,
		Inviter:    inviterName,
		ExpiresAt:  payload.ExpiresAt,
	}, nil
}

func (s *TenantService) AcceptTenantInviteLinkEE(ctx context.Context, userID, token string, store tenantInviteLinkStore) error {
	payload, err := loadTenantInviteLink(ctx, token, store)
	if err != nil {
		return err
	}
	err = dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var tenant entity.Tenant
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "invite_enabled", "role_id").Where("id = ?", payload.TenantID).Take(&tenant).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.NewCodedError(common.CodeNotFound, "Tenant invitation link is invalid or expired")
			}
			return fmt.Errorf("load Tenant for invitation acceptance: %w", err)
		}
		if !tenant.InviteEnabled {
			return common.NewCodedError(common.CodeForbidden, "Tenant invitations are disabled")
		}
		role, _, err := resolveTenantInviteRoleEE(tx, payload.TenantID)
		if err != nil {
			return err
		}

		var membership entity.UserTenant
		memberErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND tenant_id = ?", userID, payload.TenantID).Take(&membership).Error
		if memberErr != nil && !errors.Is(memberErr, gorm.ErrRecordNotFound) {
			return fmt.Errorf("load existing Tenant membership: %w", memberErr)
		}
		if memberErr == nil && tenantMembershipStatus(membership.Status) == entity.TenantMembershipStatusJoined {
			return nil
		}

		before := interface{}(nil)
		if memberErr == nil {
			before = map[string]interface{}{
				"user_id": userID, "role": membership.Role,
				"status": tenantMembershipStatus(membership.Status), "invited_by": membership.InvitedBy,
			}
			switch tenantMembershipStatus(membership.Status) {
			case entity.TenantMembershipStatusInvited, entity.TenantMembershipStatusRequested,
				entity.TenantMembershipStatusRejected, entity.TenantMembershipStatusLeft,
				entity.TenantMembershipStatusRemoved:
			default:
				return common.NewCodedError(common.CodeConflict, "Tenant membership has an unsupported status")
			}
		}
		status := entity.TenantMembershipStatusJoined
		if errors.Is(memberErr, gorm.ErrRecordNotFound) {
			membership = entity.UserTenant{
				ID:        common.GenerateUUID(),
				UserID:    userID,
				TenantID:  payload.TenantID,
				Role:      role.value,
				InvitedBy: payload.InviterID,
				Status:    &status,
			}
			if err := tx.Create(&membership).Error; err != nil {
				return fmt.Errorf("create Tenant membership from invitation link: %w", err)
			}
		} else {
			if err := tx.Model(&entity.UserTenant{}).Where("id = ?", membership.ID).Updates(map[string]interface{}{
				"role": role.value, "invited_by": payload.InviterID, "status": status,
			}).Error; err != nil {
				return fmt.Errorf("join Tenant from invitation link: %w", err)
			}
		}
		after := map[string]interface{}{
			"user_id": userID, "role": role.value, "status": status, "invited_by": payload.InviterID,
			"invitation_link": hashTenantInviteToken(token),
		}
		if role.roleID != nil {
			after["role_id"] = *role.roleID
		}
		if role.roleName != "" {
			after["role_name"] = role.roleName
		}
		return recordTenantPermissionChange(ctx, tx, permission.TenantIDFromContext(ctx),
			"tenant.invitation_link.accepted", "user", userID,
			"tenant_membership", membership.ID, "join", 0, 0, before, after)
	})
	if err != nil {
		return err
	}
	return nil
}

func loadTenantInviteLink(ctx context.Context, token string, store tenantInviteLinkStore) (*tenantInviteLinkPayload, error) {
	if store == nil || !validTenantInviteToken(token) {
		return nil, common.NewCodedError(common.CodeNotFound, "Tenant invitation link is invalid or expired")
	}
	value, err := store.Get(ctx, tenantInviteLinkKey(token))
	if err != nil {
		return nil, fmt.Errorf("load Tenant invitation link: %w", err)
	}
	if value == "" {
		return nil, common.NewCodedError(common.CodeNotFound, "Tenant invitation link is invalid or expired")
	}
	var payload tenantInviteLinkPayload
	if err := json.Unmarshal([]byte(value), &payload); err != nil || payload.TenantID == "" || payload.InviterID == "" || !payload.ExpiresAt.After(time.Now().UTC()) {
		return nil, common.NewCodedError(common.CodeNotFound, "Tenant invitation link is invalid or expired")
	}
	return &payload, nil
}

func newTenantInviteToken() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func validTenantInviteToken(token string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(decoded) == 32
}

func tenantInviteLinkKey(token string) string {
	return tenantInviteLinkKeyPrefix + hashTenantInviteToken(token)
}

func hashTenantInviteToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
