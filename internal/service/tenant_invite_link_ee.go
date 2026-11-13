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
	"ragflow/internal/service/billing"

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

type TenantInviteLinkMembership struct {
	Status string `json:"status"`
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
	if err := dao.DB.WithContext(ctx).Select("id", "name", "role_id").Where("id = ?", payload.TenantID).Take(&tenant).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewCodedError(common.CodeNotFound, "Tenant invitation link is invalid or expired")
		}
		return nil, fmt.Errorf("load Tenant invitation target: %w", err)
	}
	enabled, err := tenantInvitationsEnabled(dao.DB.WithContext(ctx), &tenant)
	if err != nil {
		return nil, fmt.Errorf("load Tenant invitation permission: %w", err)
	}
	if !enabled {
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

func (s *TenantService) GetTenantInviteLinkMembershipEE(ctx context.Context, userID, token string, store tenantInviteLinkStore) (*TenantInviteLinkMembership, error) {
	payload, err := loadTenantInviteLink(ctx, token, store)
	if err != nil {
		return nil, err
	}
	var membership entity.UserTenant
	if err := dao.DB.WithContext(ctx).Select("status").
		Where("user_id = ? AND tenant_id = ?", userID, payload.TenantID).Take(&membership).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &TenantInviteLinkMembership{}, nil
		}
		return nil, fmt.Errorf("load Tenant invitation membership: %w", err)
	}
	return &TenantInviteLinkMembership{Status: tenantMembershipStatus(membership.Status)}, nil
}

func (s *TenantService) RequestTenantInviteLinkEE(ctx context.Context, userID, token string, store tenantInviteLinkStore) error {
	payload, err := loadTenantInviteLink(ctx, token, store)
	if err != nil {
		return err
	}
	err = dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var tenant entity.Tenant
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "role_id").Where("id = ?", payload.TenantID).Take(&tenant).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.NewCodedError(common.CodeNotFound, "Tenant invitation link is invalid or expired")
			}
			return fmt.Errorf("load Tenant for invitation request: %w", err)
		}
		enabled, err := tenantInvitationsEnabled(tx, &tenant)
		if err != nil {
			return fmt.Errorf("load Tenant invitation permission: %w", err)
		}
		if !enabled {
			return common.NewCodedError(common.CodeForbidden, "Tenant system role does not enable invitations")
		}
		var membership entity.UserTenant
		memberErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND tenant_id = ?", userID, payload.TenantID).Take(&membership).Error
		if memberErr != nil && !errors.Is(memberErr, gorm.ErrRecordNotFound) {
			return fmt.Errorf("load existing Tenant membership: %w", memberErr)
		}
		if memberErr == nil {
			switch tenantMembershipStatus(membership.Status) {
			case entity.TenantMembershipStatusJoined, entity.TenantMembershipStatusRequested:
				return nil
			}
		}

		before := interface{}(nil)
		if memberErr == nil {
			before = map[string]interface{}{
				"user_id": userID, "role": membership.Role,
				"status": tenantMembershipStatus(membership.Status), "invited_by": membership.InvitedBy,
			}
			switch tenantMembershipStatus(membership.Status) {
			case entity.TenantMembershipStatusInvited,
				entity.TenantMembershipStatusRejected, entity.TenantMembershipStatusRemoved:
			default:
				return common.NewCodedError(common.CodeConflict, "Tenant membership has an unsupported status")
			}
		}
		status := entity.TenantMembershipStatusRequested
		if errors.Is(memberErr, gorm.ErrRecordNotFound) {
			membership = entity.UserTenant{
				ID:        common.GenerateUUID(),
				UserID:    userID,
				TenantID:  payload.TenantID,
				InvitedBy: payload.InviterID,
				Status:    &status,
			}
			if err := tx.Create(&membership).Error; err != nil {
				return fmt.Errorf("create Tenant membership from invitation link: %w", err)
			}
		} else {
			if err := tx.Model(&entity.UserTenant{}).Where("id = ?", membership.ID).Updates(map[string]interface{}{
				"role": "", "invited_by": payload.InviterID, "status": status,
			}).Error; err != nil {
				return fmt.Errorf("create Tenant membership request from invitation link: %w", err)
			}
		}
		after := map[string]interface{}{
			"user_id": userID, "status": status, "invited_by": payload.InviterID,
			"invitation_link": hashTenantInviteToken(token),
		}
		return recordTenantPermissionChange(ctx, tx, payload.TenantID,
			"tenant.invitation_link.requested", "user", userID,
			"tenant_membership", membership.ID, "request", 0, 0, before, after)
	})
	if err != nil {
		return err
	}
	return nil
}

func (s *TenantService) ApproveTenantMember(ctx context.Context, tenantID, targetUserID string) error {
	db := dao.DB.WithContext(ctx)
	err := db.Transaction(func(tx *gorm.DB) error {
		if _, _, err := tenantMemberPermission(ctx, tx, permission.TenantIDFromContext(ctx), entity.TenantPermissionManageMembers); err != nil {
			return err
		}
		var membership entity.UserTenant
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND user_id = ?", tenantID, targetUserID).Take(&membership).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.NewCodedError(common.CodeNotFound, "Tenant member not found")
			}
			return fmt.Errorf("load Tenant membership request: %w", err)
		}
		statusBefore := tenantMembershipStatus(membership.Status)
		if statusBefore == entity.TenantMembershipStatusJoined {
			return nil
		}
		if statusBefore != entity.TenantMembershipStatusRequested {
			return common.NewCodedError(common.CodeConflict, "Tenant member has no pending join request")
		}
		role, _, err := resolveTenantInviteRoleEE(tx, tenantID)
		if err != nil {
			return err
		}
		if billingService := billing.Get(); billingService != nil && billingService.Enabled() {
			if quotaErr := billingService.CheckResources(ctx, tenantID, billing.ResourceDelta{Members: 1}); quotaErr != nil {
				if _, ok := quotaErr.(*billing.InsufficientResourceError); ok {
					return common.NewCodedError(common.CodeBillingSeatsInsufficient, quotaErr.Error())
				}
				return fmt.Errorf("check Tenant seat quota: %w", quotaErr)
			}
		}

		status := entity.TenantMembershipStatusJoined
		if err := tx.Model(&entity.UserTenant{}).Where("id = ? AND tenant_id = ?", membership.ID, tenantID).
			Updates(map[string]interface{}{"role": role.value, "status": status}).Error; err != nil {
			return fmt.Errorf("approve Tenant membership request: %w", err)
		}
		before := map[string]interface{}{
			"user_id": targetUserID, "role": membership.Role, "status": statusBefore,
		}
		after := map[string]interface{}{
			"user_id": targetUserID, "role": role.value, "status": status,
		}
		if role.roleID != nil {
			after["role_id"] = *role.roleID
		}
		if role.roleName != "" {
			after["role_name"] = role.roleName
		}
		return recordTenantPermissionChange(ctx, tx, tenantID, "tenant.member.request_approved", "user", targetUserID,
			"tenant_membership", membership.ID, "approve", 0, 0, before, after)
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
