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
	result, err := h.service.ListTenants(c.Request.Context(), filter, page, pageSize)
	if err != nil {
		writeRoleError(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

type updateTenantRoleRequest struct {
	RoleID *int32 `json:"role_id"`
}

func (h *Handler) UpdateTenantRole(c *gin.Context) {
	tenantID := strings.TrimSpace(c.Param("tenant_id"))
	if tenantID == "" {
		common.RespondError(c, common.CodeBadRequest, "tenant_id is required")
		return
	}
	var request updateTenantRoleRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.RoleID == nil || *request.RoleID <= 0 {
		common.Respond(c, http.StatusBadRequest, common.CodeBadRequest, nil, "a positive system role ID is required")
		return
	}
	actorID, ok := requireAdminActorID(c)
	if !ok {
		return
	}
	result, err := h.service.UpdateTenantRole(c.Request.Context(), tenantID, request.RoleID, actorID)
	if err != nil {
		writeRoleError(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

type updateTenantStatusRequest struct {
	Status string `json:"status"`
}

func (h *Handler) UpdateTenantStatus(c *gin.Context) {
	tenantID := strings.TrimSpace(c.Param("tenant_id"))
	if tenantID == "" {
		common.RespondError(c, common.CodeBadRequest, "tenant_id is required")
		return
	}
	var request updateTenantStatusRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.RespondError(c, common.CodeBadRequest, "status must be 0 or 1")
		return
	}
	actorID, ok := requireAdminActorID(c)
	if !ok {
		return
	}
	result, err := h.service.UpdateTenantStatus(c.Request.Context(), tenantID, request.Status, actorID)
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
