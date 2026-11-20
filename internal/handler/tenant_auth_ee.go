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
	"net/http"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/engine/kvrocks"
	"ragflow/internal/server"

	"github.com/gin-gonic/gin"
)

func (h *TenantHandler) SwitchTenant(c *gin.Context) {
	common.StartOperationLog(c, "switch_tenant")
	defer common.FinishOperationLog(c)
	common.SetOperationResource(c, "target_tenant_id", c.Param("tenant_id"))

	user, code, message := GetUser(c)
	if code != common.CodeSuccess {
		common.Respond(c, http.StatusUnauthorized, code, nil, message)
		return
	}
	tenantID := c.Param("tenant_id")
	if tenantID == "" {
		common.Respond(c, http.StatusBadRequest, common.CodeBadRequest, nil, "tenant_id is required")
		return
	}
	if user.AccessToken == nil || *user.AccessToken == "" {
		common.Respond(c, http.StatusUnauthorized, common.CodeUnauthorized, nil, "User access token is missing")
		return
	}
	if _, err := dao.NewUserTenantDAO().FilterByUserIDAndTenantID(c.Request.Context(), dao.DB, user.ID, tenantID); err != nil {
		common.Respond(c, http.StatusForbidden, common.CodeForbidden, nil, "User is not an active member of this tenant")
		return
	}
	if _, err := dao.NewTenantDAO().GetByID(c.Request.Context(), dao.DB, tenantID); err != nil {
		common.Respond(c, http.StatusForbidden, common.CodeForbidden, nil, "This tenant is inactive or unavailable")
		return
	}

	secretKey, err := server.GetSecretKey(c.Request.Context(), kvrocks.Get())
	if err != nil {
		common.Respond(c, http.StatusInternalServerError, common.CodeServerError, nil, "Failed to get token signing key")
		return
	}
	authToken, err := common.DumpAccessToken(*user.AccessToken, tenantID, secretKey)
	if err != nil {
		common.Respond(c, http.StatusInternalServerError, common.CodeServerError, nil, "Failed to generate authorization token")
		return
	}
	if err := dao.NewUserTenantDAO().UpdateLastLoginTenantID(c.Request.Context(), dao.DB, user.ID, tenantID); err != nil {
		common.Respond(c, http.StatusInternalServerError, common.CodeServerError, nil, "Failed to save last login tenant")
		return
	}

	c.Header("Authorization", authToken)
	c.Header("Access-Control-Expose-Headers", "Authorization")
	//setOAuthAuthCookie(c, authToken)
	common.RespondSuccess(c, gin.H{"tenant_id": tenantID}, "Tenant switched")
}
