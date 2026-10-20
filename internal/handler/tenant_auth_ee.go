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
	user, code, message := GetUser(c)
	if code != common.CodeSuccess {
		common.ResponseWithHttpCodeData(c, http.StatusUnauthorized, code, nil, message)
		return
	}
	tenantID := c.Param("tenant_id")
	if tenantID == "" {
		common.ResponseWithHttpCodeData(c, http.StatusBadRequest, common.CodeBadRequest, nil, "tenant_id is required")
		return
	}
	if user.AccessToken == nil || *user.AccessToken == "" {
		common.ResponseWithHttpCodeData(c, http.StatusUnauthorized, common.CodeUnauthorized, nil, "User access token is missing")
		return
	}
	if _, err := dao.NewUserTenantDAO().FilterByUserIDAndTenantID(c.Request.Context(), dao.DB, user.ID, tenantID); err != nil {
		common.ResponseWithHttpCodeData(c, http.StatusForbidden, common.CodeForbidden, nil, "User is not an active member of this tenant")
		return
	}

	secretKey, err := server.GetSecretKey(c.Request.Context(), kvrocks.Get())
	if err != nil {
		common.ResponseWithHttpCodeData(c, http.StatusInternalServerError, common.CodeServerError, nil, "Failed to get token signing key")
		return
	}
	authToken, err := common.DumpAccessToken(*user.AccessToken, tenantID, secretKey)
	if err != nil {
		common.ResponseWithHttpCodeData(c, http.StatusInternalServerError, common.CodeServerError, nil, "Failed to generate authorization token")
		return
	}

	c.Header("Authorization", authToken)
	c.Header("Access-Control-Expose-Headers", "Authorization")
	setOAuthAuthCookie(c, authToken)
	common.SuccessWithData(c, gin.H{"tenant_id": tenantID}, "Tenant switched")
}
