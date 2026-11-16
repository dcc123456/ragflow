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

	"ragflow/internal/common"
	"ragflow/internal/service"

	"github.com/gin-gonic/gin"
)

func (h *TenantHandler) UpdateResourcePermissions(c *gin.Context) {
	common.StartOperationLog(c, "update_resource_permissions")
	defer common.FinishOperationLog(c)

	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	var request service.ResourcePermissionUpdateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.RespondError(c, common.CodeBadRequest, "Invalid resource permission request")
		return
	}
	common.SetOperationResource(c, "resource_type", request.ResourceType)
	common.SetOperationResource(c, "resource_id", request.ResourceID)
	common.SetOperationResource(c, "resource_ids", request.ResourceIDs)
	if err := h.tenantService.UpdateResourcePermissions(c.Request.Context(), userID, tenantID, request); err != nil {
		writeResourcePermissionError(c, err)
		return
	}
	common.RespondSuccess(c, true, "")
}

func (h *TenantHandler) TransferResourceOwnership(c *gin.Context) {
	common.StartOperationLog(c, "transfer_resource_ownership")
	defer common.FinishOperationLog(c)

	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	var request service.ResourceOwnershipTransferRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.RespondError(c, common.CodeBadRequest, "Invalid resource ownership transfer request")
		return
	}
	common.SetOperationResource(c, "resource_type", request.ResourceType)
	common.SetOperationResource(c, "resource_id", request.ResourceID)
	common.SetOperationResource(c, "target_user_id", request.UserID)
	if err := h.tenantService.TransferResourceOwnership(c.Request.Context(), userID, tenantID, request); err != nil {
		writeResourcePermissionError(c, err)
		return
	}
	common.RespondSuccess(c, true, "")
}

func (h *TenantHandler) ListResourcePermissions(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	var request service.ResourcePermissionListRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.RespondError(c, common.CodeBadRequest, "Invalid resource permission list request")
		return
	}
	permissions, err := h.tenantService.ListResourcePermissions(c.Request.Context(), userID, tenantID, request)
	if err != nil {
		writeResourcePermissionError(c, err)
		return
	}
	common.RespondSuccess(c, permissions, "")
}

func (h *TenantHandler) ListResourcePermissionsByTarget(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	var request service.ResourcePermissionTargetListRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.RespondError(c, common.CodeBadRequest, "Invalid permission target request")
		return
	}
	permissions, err := h.tenantService.ListResourcePermissionsByTarget(c.Request.Context(), userID, tenantID, request)
	if err != nil {
		writeResourcePermissionError(c, err)
		return
	}
	common.RespondSuccess(c, permissions, "")
}

func writeResourcePermissionError(c *gin.Context, err error) {
	var coded *common.CodedError
	if errors.As(err, &coded) {
		common.RespondError(c, coded.Code, coded.Message)
		return
	}
	common.RespondError(c, common.CodeServerError, err.Error())
}
