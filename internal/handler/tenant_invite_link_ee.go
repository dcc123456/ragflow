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

package handler

import (
	"strings"

	"ragflow/internal/common"
	"ragflow/internal/engine/kvrocks"

	"github.com/gin-gonic/gin"
)

func (h *TenantHandler) CreateTenantInviteLink(c *gin.Context) {
	store := tenantInviteLinkStore(c)
	if store == nil {
		return
	}
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	if requestedTenantID := strings.TrimSpace(c.Param("tenant_id")); requestedTenantID == "" || requestedTenantID != tenantID {
		common.RespondError(c, common.CodeForbidden, "Tenant does not match the active token context")
		return
	}
	link, err := h.tenantService.CreateTenantInviteLinkEE(c.Request.Context(), userID, tenantID, store)
	if err != nil {
		writeTenantRoleError(c, err)
		return
	}
	common.RespondSuccess(c, link, "success")
}

func (h *TenantHandler) GetTenantInviteLinkInfo(c *gin.Context) {
	store := tenantInviteLinkStore(c)
	if store == nil {
		return
	}
	token := strings.TrimSpace(c.Param("token"))
	if token == "" {
		common.RespondError(c, common.CodeBadRequest, "invitation token is required")
		return
	}
	info, err := h.tenantService.GetTenantInviteLinkInfoEE(c.Request.Context(), token, store)
	if err != nil {
		writeTenantRoleError(c, err)
		return
	}
	common.RespondSuccess(c, info, "success")
}

func (h *TenantHandler) GetTenantInviteLinkMembership(c *gin.Context) {
	user, code, message := GetUser(c)
	if code != common.CodeSuccess {
		common.RespondError(c, code, message)
		return
	}
	token := strings.TrimSpace(c.Param("token"))
	if token == "" {
		common.RespondError(c, common.CodeBadRequest, "invitation token is required")
		return
	}
	store := tenantInviteLinkStore(c)
	if store == nil {
		return
	}
	membership, err := h.tenantService.GetTenantInviteLinkMembershipEE(c.Request.Context(), user.ID, token, store)
	if err != nil {
		writeTenantRoleError(c, err)
		return
	}
	common.RespondSuccess(c, membership, "success")
}

func (h *TenantHandler) RequestTenantInviteLink(c *gin.Context) {
	user, code, message := GetUser(c)
	if code != common.CodeSuccess {
		common.RespondError(c, code, message)
		return
	}
	token := strings.TrimSpace(c.Param("token"))
	if token == "" {
		common.RespondError(c, common.CodeBadRequest, "invitation token is required")
		return
	}
	store := tenantInviteLinkStore(c)
	if store == nil {
		return
	}
	if err := h.tenantService.RequestTenantInviteLinkEE(c.Request.Context(), user.ID, token, store); err != nil {
		writeTenantRoleError(c, err)
		return
	}
	common.RespondSuccess(c, true, "success")
}

func (h *TenantHandler) ApproveTenantMember(c *gin.Context) {
	_, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	targetUserID := strings.TrimSpace(c.Param("user_id"))
	if targetUserID == "" {
		common.RespondError(c, common.CodeBadRequest, "user_id is required")
		return
	}
	if err := h.tenantService.ApproveTenantMember(c.Request.Context(), tenantID, targetUserID); err != nil {
		writeTenantRoleError(c, err)
		return
	}
	common.RespondSuccess(c, true, "success")
}

func tenantInviteLinkStore(c *gin.Context) *kvrocks.Client {
	store := kvrocks.Get()
	if store == nil || !kvrocks.IsEnabled() {
		common.RespondError(c, common.CodeServerError, "Tenant invitation link storage is unavailable")
		return nil
	}
	return store
}
