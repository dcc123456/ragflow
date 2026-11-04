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
	"ragflow/internal/common"

	"github.com/gin-gonic/gin"
)

type CreateModelGroupRequest struct {
	Name string `json:"name"`
}

func (h *ProviderHandler) CreateModelGroup(c *gin.Context) {
	var req CreateModelGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, common.CodeBadRequest, "Group name is required")
		return
	}

	userID := c.GetString("user_id")

	ctx := c.Request.Context()

	err := h.modelProviderService.CreateModelGroup(ctx, userID, req.Name)
	if err != nil {
		common.RespondError(c, common.CodeDataError, err.Error())
		return
	}

	common.RespondSuccess(c, nil, "success")
}

func (h *ProviderHandler) DeleteModelGroup(c *gin.Context) {
	groupName := c.Param("group_name")

	userID := c.GetString("user_id")

	ctx := c.Request.Context()

	err := h.modelProviderService.DeleteModelGroup(ctx, userID, groupName)
	if err != nil {
		common.RespondError(c, common.CodeDataError, err.Error())
		return
	}

	common.RespondSuccess(c, nil, "success")
}

func (h *ProviderHandler) ShowModelGroup(c *gin.Context) {
	userID := c.GetString("user_id")
	ctx := c.Request.Context()
	groupName := c.Query("group_name")
	if groupName == "" {
		// list model groups
		groups, err := h.modelProviderService.ListModelGroup(ctx, userID)
		if err != nil {
			common.RespondError(c, common.CodeDataError, err.Error())
			return
		}
		common.RespondSuccess(c, groups, "success")
		return
	}

	models, err := h.modelProviderService.ShowModelGroup(ctx, userID, groupName)
	if err != nil {
		common.RespondError(c, common.CodeDataError, err.Error())
		return
	}
	common.RespondSuccess(c, models, "success")
	return
}

type AddModelToGroupRequest struct {
	CompositeModelName string `json:"composite_model_name"`
	ModelID            string `json:"model_id"`
}

func (h *ProviderHandler) AddModelToGroup(c *gin.Context) {
	var req AddModelToGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, common.CodeBadRequest, "Group name is required")
		return
	}

	userID := c.GetString("user_id")
	ctx := c.Request.Context()
	groupName := c.Param("group_name")

	if req.CompositeModelName == "" {
		err := h.modelProviderService.AddModelToGroupByID(ctx, userID, req.ModelID, groupName)
		if err != nil {
			common.RespondError(c, common.CodeDataError, err.Error())
			return
		}
	} else {
		modelName, instanceName, providerName, err := common.ParseCompositeModelName(req.CompositeModelName)
		if err != nil {
			common.RespondError(c, common.CodeBadRequest, err.Error())
			return
		}
		err = h.modelProviderService.AddModelToGroupByName(ctx, userID, providerName, instanceName, modelName, groupName)
		if err != nil {
			common.RespondError(c, common.CodeDataError, err.Error())
			return
		}
	}

	common.RespondSuccess(c, nil, "success")
}

type DeleteModelToGroupRequest struct {
	CompositeModelName string `json:"composite_model_name"`
	ModelID            string `json:"model_id"`
}

func (h *ProviderHandler) RemoveModelFromGroup(c *gin.Context) {

	var req DeleteModelToGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, common.CodeBadRequest, "Group name is required")
		return
	}

	ctx := c.Request.Context()
	groupName := c.Param("group_name")

	if req.CompositeModelName == "" {
		err := h.modelProviderService.RemoveModelFromGroupByID(ctx, req.ModelID)
		if err != nil {
			common.RespondError(c, common.CodeDataError, err.Error())
			return
		}
	} else {
		userID := c.GetString("user_id")
		modelName, instanceName, providerName, err := common.ParseCompositeModelName(req.CompositeModelName)
		if err != nil {
			common.RespondError(c, common.CodeBadRequest, err.Error())
			return
		}
		err = h.modelProviderService.RemoveModelFromGroupByName(ctx, userID, providerName, instanceName, modelName, groupName)
		if err != nil {
			common.RespondError(c, common.CodeDataError, err.Error())
			return
		}
	}

	common.RespondSuccess(c, nil, "success")
}
