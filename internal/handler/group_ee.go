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
	"errors"
	"strings"

	"ragflow/internal/common"
	"ragflow/internal/service"

	"github.com/gin-gonic/gin"
)

func (h *TenantHandler) ListGroups(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	groups, err := service.NewGroupService().ListGroups(c.Request.Context(), userID, tenantID)
	if err != nil {
		writeGroupError(c, err)
		return
	}
	common.RespondSuccess(c, gin.H{"groups": groups}, "")
}

func (h *TenantHandler) CreateGroup(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	var request service.CreateGroupRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.RespondError(c, common.CodeBadRequest, "Invalid group request")
		return
	}
	group, err := service.NewGroupService().CreateGroup(c.Request.Context(), userID, tenantID, request)
	if err != nil {
		writeGroupError(c, err)
		return
	}
	common.RespondSuccess(c, group, "")
}

func (h *TenantHandler) GetGroup(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	group, err := service.NewGroupService().GetGroup(c.Request.Context(), userID, tenantID, strings.TrimSpace(c.Param("group_id")))
	if err != nil {
		writeGroupError(c, err)
		return
	}
	common.RespondSuccess(c, group, "")
}

func (h *TenantHandler) UpdateGroup(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	var request service.UpdateGroupRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.RespondError(c, common.CodeBadRequest, "Invalid group request")
		return
	}
	group, err := service.NewGroupService().UpdateGroup(c.Request.Context(), userID, tenantID, strings.TrimSpace(c.Param("group_id")), request)
	if err != nil {
		writeGroupError(c, err)
		return
	}
	common.RespondSuccess(c, group, "")
}

func (h *TenantHandler) MoveGroup(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	var request struct {
		ParentID *string `json:"parent_id"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		common.RespondError(c, common.CodeBadRequest, "Invalid group parent request")
		return
	}
	group, err := service.NewGroupService().MoveGroup(c.Request.Context(), userID, tenantID, strings.TrimSpace(c.Param("group_id")), request.ParentID)
	if err != nil {
		writeGroupError(c, err)
		return
	}
	common.RespondSuccess(c, group, "")
}

func (h *TenantHandler) DeleteGroup(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	if err := service.NewGroupService().DeleteGroup(c.Request.Context(), userID, tenantID, strings.TrimSpace(c.Param("group_id"))); err != nil {
		writeGroupError(c, err)
		return
	}
	common.RespondSuccess(c, gin.H{"success": true}, "")
}

func (h *TenantHandler) ListGroupMembers(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	members, err := service.NewGroupService().ListGroupMembers(c.Request.Context(), userID, tenantID, strings.TrimSpace(c.Param("group_id")))
	if err != nil {
		writeGroupError(c, err)
		return
	}
	common.RespondSuccess(c, gin.H{"members": members}, "")
}

func (h *TenantHandler) AddGroupMember(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	var request struct {
		UserID string `json:"user_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		common.RespondError(c, common.CodeBadRequest, "user_id is required")
		return
	}
	member, err := service.NewGroupService().AddGroupMember(c.Request.Context(), userID, tenantID, strings.TrimSpace(c.Param("group_id")), request.UserID)
	if err != nil {
		writeGroupError(c, err)
		return
	}
	common.RespondSuccess(c, member, "")
}

func (h *TenantHandler) RemoveGroupMember(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	if err := service.NewGroupService().RemoveGroupMember(c.Request.Context(), userID, tenantID, strings.TrimSpace(c.Param("group_id")), strings.TrimSpace(c.Param("user_id"))); err != nil {
		writeGroupError(c, err)
		return
	}
	common.RespondSuccess(c, gin.H{"success": true}, "")
}

func (h *TenantHandler) SetGroupMemberPermissions(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	var request service.SetGroupMemberPermissionsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.RespondError(c, common.CodeBadRequest, "Invalid group member permission request")
		return
	}
	member, err := service.NewGroupService().SetGroupMemberPermissions(c.Request.Context(), userID, tenantID,
		strings.TrimSpace(c.Param("group_id")), strings.TrimSpace(c.Param("user_id")), request.Permissions)
	if err != nil {
		writeGroupError(c, err)
		return
	}
	common.RespondSuccess(c, member, "")
}

func (h *TenantHandler) TransferGroupOwnership(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	var request struct {
		UserID string `json:"user_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		common.RespondError(c, common.CodeBadRequest, "user_id is required")
		return
	}
	group, err := service.NewGroupService().TransferGroupOwnership(c.Request.Context(), userID, tenantID,
		strings.TrimSpace(c.Param("group_id")), request.UserID)
	if err != nil {
		writeGroupError(c, err)
		return
	}
	common.RespondSuccess(c, group, "")
}

func writeGroupError(c *gin.Context, err error) {
	var coded *common.CodedError
	if errors.As(err, &coded) {
		common.RespondError(c, coded.Code, coded.Message)
		return
	}
	common.RespondError(c, common.CodeServerError, err.Error())
}
