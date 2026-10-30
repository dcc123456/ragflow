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
		common.ErrorWithCode(c, common.CodeForbidden, "Tenant does not match the active token context")
		return
	}
	link, err := h.tenantService.CreateTenantInviteLinkEE(c.Request.Context(), userID, tenantID, store)
	if err != nil {
		writeTenantRoleError(c, err)
		return
	}
	common.SuccessWithData(c, link, "success")
}

func (h *TenantHandler) GetTenantInviteLinkInfo(c *gin.Context) {
	store := tenantInviteLinkStore(c)
	if store == nil {
		return
	}
	token := strings.TrimSpace(c.Param("token"))
	if token == "" {
		common.ErrorWithCode(c, common.CodeBadRequest, "invitation token is required")
		return
	}
	info, err := h.tenantService.GetTenantInviteLinkInfoEE(c.Request.Context(), token, store)
	if err != nil {
		writeTenantRoleError(c, err)
		return
	}
	common.SuccessWithData(c, info, "success")
}

func (h *TenantHandler) AcceptTenantInviteLink(c *gin.Context) {
	user, code, message := GetUser(c)
	if code != common.CodeSuccess {
		common.ErrorWithCode(c, code, message)
		return
	}
	token := strings.TrimSpace(c.Param("token"))
	if token == "" {
		common.ErrorWithCode(c, common.CodeBadRequest, "invitation token is required")
		return
	}
	store := tenantInviteLinkStore(c)
	if store == nil {
		return
	}
	if err := h.tenantService.AcceptTenantInviteLinkEE(c.Request.Context(), user.ID, token, store); err != nil {
		writeTenantRoleError(c, err)
		return
	}
	common.SuccessWithData(c, true, "success")
}

func tenantInviteLinkStore(c *gin.Context) *kvrocks.Client {
	store := kvrocks.Get()
	if store == nil || !kvrocks.IsEnabled() {
		common.ErrorWithCode(c, common.CodeServerError, "Tenant invitation link storage is unavailable")
		return nil
	}
	return store
}
