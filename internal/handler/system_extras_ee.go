//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

//go:build ee

package handler

import (
	"strconv"
	"strings"

	"ragflow/internal/common"
	"ragflow/internal/dao"

	"github.com/gin-gonic/gin"
)

// Notification returns the operator-configured banner the web client shows.
func (h *SystemHandler) Notification(c *gin.Context) {
	settingsDAO := dao.NewSystemSettingsDAO()
	rows, err := settingsDAO.GetByNamePrefix(c.Request.Context(), dao.GetDB(), "notification.")
	if err != nil {
		common.ErrorWithCode(c, common.CodeServerError, err.Error())
		return
	}
	payload := map[string]interface{}{
		"content": "",
		"enabled": false,
	}
	for _, row := range rows {
		switch row.Name {
		case "notification.content":
			payload["content"] = row.Value
		case "notification.enabled":
			payload["enabled"] = settingBool(row.Value, row.DataType)
		}
	}
	common.SuccessWithData(c, payload, "")
}

// ExposeModelProvider reports whether the deployment publishes its model
// providers to end users.
func (h *SystemHandler) ExposeModelProvider(c *gin.Context) {
	common.SuccessWithData(c, gin.H{"enabled": settingEnabled(c, "expose_model_provider.enabled", false)}, "")
}

// ExposeAPI reports whether the deployment publishes its HTTP API to end users.
// A missing setting stays null, matching the python contract.
func (h *SystemHandler) ExposeAPI(c *gin.Context) {
	settingsDAO := dao.NewSystemSettingsDAO()
	rows, err := settingsDAO.GetByName(c.Request.Context(), dao.GetDB(), "expose_api.enabled")
	if err != nil || len(rows) == 0 {
		common.SuccessWithData(c, gin.H{"enabled": nil}, "")
		return
	}
	common.SuccessWithData(c, gin.H{"enabled": settingBool(rows[0].Value, rows[0].DataType)}, "")
}

// settingEnabled reads a boolean system setting, falling back when absent.
func settingEnabled(c *gin.Context, name string, fallback bool) bool {
	settingsDAO := dao.NewSystemSettingsDAO()
	rows, err := settingsDAO.GetByName(c.Request.Context(), dao.GetDB(), name)
	if err != nil || len(rows) == 0 {
		return fallback
	}
	return settingBool(rows[0].Value, rows[0].DataType)
}

// settingBool interprets a stored setting value.
func settingBool(value, dataType string) bool {
	trimmed := strings.TrimSpace(value)
	if strings.EqualFold(dataType, "bool") || strings.EqualFold(dataType, "boolean") {
		if parsed, err := strconv.ParseBool(trimmed); err == nil {
			return parsed
		}
	}
	return strings.EqualFold(strings.Trim(trimmed, `"`), "true")
}
