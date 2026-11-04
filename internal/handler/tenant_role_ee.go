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

package handler

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"ragflow/internal/common"
	"ragflow/internal/dao"

	"github.com/gin-gonic/gin"
)

const maxTenantRoleHandlerPageSize = 200

func (h *TenantHandler) ListTenantRoles(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	page, pageSize, ok := parseTenantRolePagination(c)
	if !ok {
		return
	}
	result, err := h.tenantService.ListTenantRoles(c.Request.Context(), userID, tenantID, c.Query("keyword"), page, pageSize)
	if err != nil {
		writeTenantRoleError(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

func (h *TenantHandler) CreateTenantRole(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	var request struct {
		RoleName    string `json:"role_name" binding:"required"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		common.RespondError(c, common.CodeBadRequest, "role_name is required")
		return
	}
	result, err := h.tenantService.CreateTenantRole(c.Request.Context(), userID, tenantID, request.RoleName, request.Description)
	if err != nil {
		writeTenantRoleError(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

func (h *TenantHandler) GetTenantRole(c *gin.Context) {
	userID, tenantID, roleID, ok := tenantRolePathContext(c)
	if !ok {
		return
	}
	result, err := h.tenantService.GetTenantRole(c.Request.Context(), userID, tenantID, roleID)
	if err != nil {
		writeTenantRoleError(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

func (h *TenantHandler) UpdateTenantRole(c *gin.Context) {
	userID, tenantID, roleID, ok := tenantRolePathContext(c)
	if !ok {
		return
	}
	var request struct {
		RoleName    *string `json:"role_name"`
		Description *string `json:"description"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		common.RespondError(c, common.CodeBadRequest, "invalid request body")
		return
	}
	result, err := h.tenantService.UpdateTenantRole(c.Request.Context(), userID, tenantID, roleID, request.RoleName, request.Description)
	if err != nil {
		writeTenantRoleError(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

func (h *TenantHandler) DeleteTenantRole(c *gin.Context) {
	userID, tenantID, roleID, ok := tenantRolePathContext(c)
	if !ok {
		return
	}
	if err := h.tenantService.DeleteTenantRole(c.Request.Context(), userID, tenantID, roleID); err != nil {
		writeTenantRoleError(c, err)
		return
	}
	common.RespondSuccess(c, map[string]interface{}{"success": true}, "")
}

func (h *TenantHandler) ListTenantRoleResources(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	result, err := h.tenantService.ListTenantRoleResources(c.Request.Context(), userID, tenantID)
	if err != nil {
		writeTenantRoleError(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

func (h *TenantHandler) GetTenantRolePermissions(c *gin.Context) {
	userID, tenantID, roleID, ok := tenantRolePathContext(c)
	if !ok {
		return
	}
	result, err := h.tenantService.GetTenantRolePermissions(c.Request.Context(), userID, tenantID, roleID)
	if err != nil {
		writeTenantRoleError(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

func (h *TenantHandler) SetTenantRolePermissions(c *gin.Context) {
	userID, tenantID, roleID, ok := tenantRolePathContext(c)
	if !ok {
		return
	}
	var request struct {
		Permissions map[string]map[string]bool `json:"permissions"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		common.RespondError(c, common.CodeBadRequest, "invalid request body")
		return
	}
	result, err := h.tenantService.SetTenantRolePermissions(c.Request.Context(), userID, tenantID, roleID, request.Permissions)
	if err != nil {
		writeTenantRoleError(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

func (h *TenantHandler) GetTenantDefaultRole(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	result, err := h.tenantService.GetTenantDefaultRole(c.Request.Context(), userID, tenantID)
	if err != nil {
		writeTenantRoleError(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

func (h *TenantHandler) SetTenantDefaultRole(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	var request struct {
		RoleID json.RawMessage `json:"role_id"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || len(request.RoleID) == 0 {
		common.RespondError(c, common.CodeBadRequest, "role_id is required; use null to clear the default")
		return
	}
	var roleID *int32
	if string(request.RoleID) != "null" {
		parsed, err := strconv.ParseInt(strings.TrimSpace(string(request.RoleID)), 10, 32)
		if err != nil || parsed <= 0 {
			common.RespondError(c, common.CodeBadRequest, "role_id must be a positive integer or null")
			return
		}
		value := int32(parsed)
		roleID = &value
	}
	result, err := h.tenantService.SetTenantDefaultRole(c.Request.Context(), userID, tenantID, roleID)
	if err != nil {
		writeTenantRoleError(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

func (h *TenantHandler) ListTenantManagementPermissions(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	result, err := h.tenantService.ListTenantManagementPermissions(c.Request.Context(), userID, tenantID)
	if err != nil {
		writeTenantRoleError(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

func (h *TenantHandler) SetTenantManagementPermissions(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	var request struct {
		UserID      string    `json:"user_id"`
		GroupID     string    `json:"group_id"`
		Permissions *[]string `json:"permissions"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Permissions == nil {
		common.RespondError(c, common.CodeBadRequest, "permissions must be an array; use an empty array to revoke the grant")
		return
	}
	result, err := h.tenantService.SetTenantManagementPermissions(c.Request.Context(), userID, tenantID, request.UserID, request.GroupID, *request.Permissions)
	if err != nil {
		writeTenantRoleError(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

func (h *TenantHandler) GetTenantMemberRole(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	targetUserID := strings.TrimSpace(c.Param("user_id"))
	if targetUserID == "" {
		common.RespondError(c, common.CodeBadRequest, "user_id is required")
		return
	}
	result, err := h.tenantService.GetTenantMemberRole(c.Request.Context(), userID, tenantID, targetUserID)
	if err != nil {
		writeTenantRoleError(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

func (h *TenantHandler) SetTenantMemberRole(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	targetUserID := strings.TrimSpace(c.Param("user_id"))
	if targetUserID == "" {
		common.RespondError(c, common.CodeBadRequest, "user_id is required")
		return
	}
	var request struct {
		RoleID int32 `json:"role_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.RoleID <= 0 {
		common.RespondError(c, common.CodeBadRequest, "role_id must be a positive integer")
		return
	}
	result, err := h.tenantService.SetTenantMemberRole(c.Request.Context(), userID, tenantID, targetUserID, request.RoleID)
	if err != nil {
		writeTenantRoleError(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

func (h *TenantHandler) GetTenantMyPermissions(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	result, err := h.tenantService.GetTenantMyPermissions(c.Request.Context(), userID, tenantID)
	if err != nil {
		writeTenantRoleError(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

func (h *TenantHandler) ListTenantPermissionLogs(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	page, pageSize, ok := parseTenantRolePagination(c)
	if !ok {
		return
	}
	filter := dao.PermissionChangeLogFilter{
		ActorUserID:  c.Query("actor_user_id"),
		EventType:    c.Query("event_type"),
		TargetType:   c.Query("target_type"),
		TargetID:     c.Query("target_id"),
		ResourceType: c.Query("resource_type"),
		ResourceID:   c.Query("resource_id"),
		ActionType:   c.Query("action_type"),
	}
	filter.StartTime, ok = parseTenantPermissionLogTime(c, "start_time")
	if !ok {
		return
	}
	filter.EndTime, ok = parseTenantPermissionLogTime(c, "end_time")
	if !ok {
		return
	}
	if filter.StartTime != nil && filter.EndTime != nil && *filter.StartTime > *filter.EndTime {
		common.RespondError(c, common.CodeBadRequest, "start_time must not be later than end_time")
		return
	}
	result, err := h.tenantService.ListTenantPermissionLogs(c.Request.Context(), userID, tenantID, filter, page, pageSize)
	if err != nil {
		writeTenantRoleError(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

func parseTenantRolePagination(c *gin.Context) (int, int, bool) {
	page, err := common.ParseRequestIntPositive(c, c.Query("page"), "page", 1)
	if err != nil || page < 1 {
		common.RespondError(c, common.CodeBadRequest, "page must be a positive integer")
		return 0, 0, false
	}
	pageSize, err := common.ParseRequestIntPositive(c, c.Query("page_size"), "page_size", 20)
	if err != nil || pageSize < 1 || pageSize > maxTenantRoleHandlerPageSize {
		common.RespondError(c, common.CodeBadRequest, "page_size must be between 1 and 200")
		return 0, 0, false
	}
	return page, pageSize, true
}

func parseTenantPermissionLogTime(c *gin.Context, key string) (*int64, bool) {
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

func tenantRoleRequestContext(c *gin.Context) (string, string, bool) {
	user, code, message := GetUser(c)
	if code != common.CodeSuccess {
		common.RespondError(c, code, message)
		return "", "", false
	}
	tenantID := strings.TrimSpace(c.GetString("tenant_id"))
	if tenantID == "" {
		common.RespondError(c, common.CodeBadRequest, "Current Tenant context is required")
		return "", "", false
	}
	return user.ID, tenantID, true
}

func tenantRolePathContext(c *gin.Context) (string, string, int32, bool) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return "", "", 0, false
	}
	roleID, err := strconv.ParseInt(c.Param("role_id"), 10, 32)
	if err != nil || roleID <= 0 {
		common.RespondError(c, common.CodeBadRequest, "role_id must be a positive integer")
		return "", "", 0, false
	}
	return userID, tenantID, int32(roleID), true
}

func writeTenantRoleError(c *gin.Context, err error) {
	var coded *common.CodedError
	if errors.As(err, &coded) {
		common.RespondError(c, coded.Code, coded.Message)
		return
	}
	common.RespondError(c, common.CodeServerError, err.Error())
}
