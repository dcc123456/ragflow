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
	"encoding/json"
	"net/http"

	"ragflow/internal/common"

	"github.com/gin-gonic/gin"
)

type updateDefaultSystemRoleRequest struct {
	RoleID json.RawMessage `json:"role_id"`
}

// ShowDefaultTenantSystemRole returns the system role applied to new tenants.
func (h *Handler) ShowDefaultTenantSystemRole(c *gin.Context) {
	role, err := h.service.GetDefaultTenantSystemRole(c.Request.Context())
	if err != nil {
		writeRoleError(c, err)
		return
	}

	var roleID *int32
	var roleName *string
	if role != nil {
		roleID = &role.ID
		roleName = &role.RoleName
	}
	common.RespondSuccess(c, gin.H{"role_id": roleID, "role_name": roleName}, "")
}

// UpdateDefaultTenantSystemRole changes the system role applied to new tenants.
func (h *Handler) UpdateDefaultTenantSystemRole(c *gin.Context) {
	common.StartOperationLog(c, "set_admin_default_tenant_role")
	defer common.FinishOperationLog(c)

	if _, ok := requireAdminActorID(c); !ok {
		return
	}
	var request updateDefaultSystemRoleRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.Respond(c, http.StatusBadRequest, common.CodeBadRequest, nil, "a positive system role ID is required")
		return
	}
	if len(request.RoleID) == 0 {
		common.Respond(c, http.StatusBadRequest, common.CodeBadRequest, nil, "a positive system role ID is required")
		return
	}
	var value int32
	if err := json.Unmarshal(request.RoleID, &value); err != nil || value <= 0 {
		common.Respond(c, http.StatusBadRequest, common.CodeBadRequest, nil, "a positive system role ID is required")
		return
	}
	common.SetOperationResource(c, "role_id", value)
	selectedRoleID := &value
	role, err := h.service.SetDefaultTenantSystemRole(c.Request.Context(), selectedRoleID)
	if err != nil {
		writeRoleError(c, err)
		return
	}

	var roleID *int32
	var roleName *string
	if role != nil {
		roleID = &role.ID
		roleName = &role.RoleName
	}
	common.RespondSuccess(c, gin.H{"role_id": roleID, "role_name": roleName}, "")
}
