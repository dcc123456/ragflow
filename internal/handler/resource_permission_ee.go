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
	"errors"

	"ragflow/internal/common"
	"ragflow/internal/service"

	"github.com/gin-gonic/gin"
)

func (h *TenantHandler) UpdateResourcePermissions(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	var request service.ResourcePermissionUpdateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ResponseWithCodeData(c, common.CodeBadRequest, nil, "Invalid resource permission request")
		return
	}
	if err := h.tenantService.UpdateResourcePermissions(c.Request.Context(), userID, tenantID, request); err != nil {
		writeResourcePermissionError(c, err)
		return
	}
	common.SuccessWithData(c, true, "")
}

func (h *TenantHandler) TransferResourceOwnership(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	var request service.ResourceOwnershipTransferRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ResponseWithCodeData(c, common.CodeBadRequest, nil, "Invalid resource ownership transfer request")
		return
	}
	if err := h.tenantService.TransferResourceOwnership(c.Request.Context(), userID, tenantID, request); err != nil {
		writeResourcePermissionError(c, err)
		return
	}
	common.SuccessWithData(c, true, "")
}

func (h *TenantHandler) ListResourcePermissions(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	var request service.ResourcePermissionListRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ResponseWithCodeData(c, common.CodeBadRequest, nil, "Invalid resource permission list request")
		return
	}
	permissions, err := h.tenantService.ListResourcePermissions(c.Request.Context(), userID, tenantID, request)
	if err != nil {
		writeResourcePermissionError(c, err)
		return
	}
	common.SuccessWithData(c, permissions, "")
}

func (h *TenantHandler) ListResourcePermissionsByTarget(c *gin.Context) {
	userID, tenantID, ok := tenantRoleRequestContext(c)
	if !ok {
		return
	}
	var request service.ResourcePermissionTargetListRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ResponseWithCodeData(c, common.CodeBadRequest, nil, "Invalid permission target request")
		return
	}
	permissions, err := h.tenantService.ListResourcePermissionsByTarget(c.Request.Context(), userID, tenantID, request)
	if err != nil {
		writeResourcePermissionError(c, err)
		return
	}
	common.SuccessWithData(c, permissions, "")
}

func writeResourcePermissionError(c *gin.Context, err error) {
	var coded *common.CodedError
	if errors.As(err, &coded) {
		common.ResponseWithCodeData(c, coded.Code, nil, coded.Message)
		return
	}
	common.ResponseWithCodeData(c, common.CodeServerError, nil, err.Error())
}
