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

package admin

import (
	"net/http"
	"strconv"
	"strings"

	"ragflow/internal/common"
	"ragflow/internal/dao"

	"github.com/gin-gonic/gin"
)

const maxAdminPageSize = 100

func requireAdminActorID(c *gin.Context) (string, bool) {
	value, ok := c.Get("user_id")
	actorUserID, valid := value.(string)
	if !ok || !valid || strings.TrimSpace(actorUserID) == "" {
		common.Respond(c, http.StatusUnauthorized, common.CodeUnauthorized, nil, "Not authenticated")
		return "", false
	}
	return strings.TrimSpace(actorUserID), true
}

func parseAdminPagination(c *gin.Context) (int, int, bool) {
	page, err := common.ParseRequestIntPositive(c, c.Query("page"), "page", 1)
	if err != nil || page < 1 {
		common.RespondError(c, common.CodeBadRequest, "page must be a positive integer")
		return 0, 0, false
	}
	pageSize, err := common.ParseRequestIntPositive(c, c.Query("page_size"), "page_size", 10)
	if err != nil || pageSize < 1 || pageSize > maxAdminPageSize {
		common.RespondError(c, common.CodeBadRequest, "page_size must be between 1 and 100")
		return 0, 0, false
	}
	return page, pageSize, true
}

func parseAdminOptionalTime(c *gin.Context, key string) (*int64, bool) {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return nil, true
	}
	timestamp, err := strconv.ParseInt(value, 10, 64)
	if err != nil || timestamp < 0 {
		common.RespondError(c, common.CodeBadRequest, key+" must be a non-negative Unix timestamp in milliseconds")
		return nil, false
	}
	return &timestamp, true
}

// ListTenants returns a paginated system-wide tenant list.
func (h *Handler) ListTenants(c *gin.Context) {
	page, pageSize, ok := parseAdminPagination(c)
	if !ok {
		return
	}
	filter := dao.AdminTenantFilter{
		Keyword: strings.TrimSpace(c.Query("keyword")),
		Status:  strings.TrimSpace(c.Query("status")),
	}
	if raw := strings.TrimSpace(c.Query("invite_enabled")); raw != "" {
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			common.RespondError(c, common.CodeBadRequest, "invite_enabled must be true or false")
			return
		}
		filter.InviteEnabled = &enabled
	}
	result, err := h.service.ListTenants(c.Request.Context(), filter, page, pageSize)
	if err != nil {
		writeRoleError(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

type updateTenantInviteSettingRequest struct {
	InviteEnabled *bool `json:"invite_enabled"`
}

// UpdateTenantInviteSetting changes whether tenant members can invite users.
func (h *Handler) UpdateTenantInviteSetting(c *gin.Context) {
	tenantID := strings.TrimSpace(c.Param("tenant_id"))
	if tenantID == "" {
		common.RespondError(c, common.CodeBadRequest, "tenant_id is required")
		return
	}
	var request updateTenantInviteSettingRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.InviteEnabled == nil {
		common.Respond(c, http.StatusBadRequest, common.CodeBadRequest, nil, "invite_enabled is required")
		return
	}
	actorID, ok := requireAdminActorID(c)
	if !ok {
		return
	}
	result, err := h.service.UpdateTenantInviteSetting(c.Request.Context(), tenantID, *request.InviteEnabled, actorID)
	if err != nil {
		writeRoleError(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

// ShowDefaultTenantInviteEnabled returns the default invitation setting for new tenants.
func (h *Handler) ShowDefaultTenantInviteEnabled(c *gin.Context) {
	enabled, err := h.service.GetDefaultTenantInviteEnabled(c.Request.Context())
	if err != nil {
		writeRoleError(c, err)
		return
	}
	common.RespondSuccess(c, gin.H{"invite_enabled": enabled}, "")
}

type updateDefaultTenantInviteEnabledRequest struct {
	InviteEnabled *bool `json:"invite_enabled"`
}

// UpdateDefaultTenantInviteEnabled changes the invitation default for new tenants.
func (h *Handler) UpdateDefaultTenantInviteEnabled(c *gin.Context) {
	var request updateDefaultTenantInviteEnabledRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.InviteEnabled == nil {
		common.Respond(c, http.StatusBadRequest, common.CodeBadRequest, nil, "invite_enabled is required")
		return
	}
	actorUserID, ok := requireAdminActorID(c)
	if !ok {
		return
	}
	result, err := h.service.UpdateDefaultTenantInviteEnabled(c.Request.Context(), *request.InviteEnabled, actorUserID)
	if err != nil {
		writeRoleError(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

// ListPermissionLogs returns global permission audit records for system administrators.
func (h *Handler) ListPermissionLogs(c *gin.Context) {
	page, pageSize, ok := parseAdminPagination(c)
	if !ok {
		return
	}
	startTime, ok := parseAdminOptionalTime(c, "start_time")
	if !ok {
		return
	}
	endTime, ok := parseAdminOptionalTime(c, "end_time")
	if !ok {
		return
	}
	if startTime != nil && endTime != nil && *startTime > *endTime {
		common.RespondError(c, common.CodeBadRequest, "start_time must not be later than end_time")
		return
	}
	filter := dao.PermissionChangeLogFilter{
		TenantID:     strings.TrimSpace(c.Query("tenant_id")),
		OperatorID:   strings.TrimSpace(c.Query("operator_id")),
		ActorUserID:  strings.TrimSpace(c.Query("actor_user_id")),
		EventType:    strings.TrimSpace(c.Query("event_type")),
		ScopeType:    strings.TrimSpace(c.Query("scope_type")),
		TargetType:   strings.TrimSpace(c.Query("target_type")),
		TargetID:     strings.TrimSpace(c.Query("target_id")),
		ResourceType: strings.TrimSpace(c.Query("resource_type")),
		ResourceID:   strings.TrimSpace(c.Query("resource_id")),
		ActionType:   strings.TrimSpace(c.Query("action_type")),
		StartTime:    startTime,
		EndTime:      endTime,
	}
	result, err := h.service.ListPermissionLogs(c.Request.Context(), filter, page, pageSize)
	if err != nil {
		writeRoleError(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}
