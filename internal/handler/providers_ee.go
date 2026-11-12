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

	ctx := c.Request.Context()

	err := h.modelProviderService.DeleteModelGroup(ctx, groupName)
	if err != nil {
		common.RespondError(c, common.CodeDataError, err.Error())
		return
	}

	common.RespondSuccess(c, nil, "success")
}

// ListModelGroups returns the metadata of every model group the caller owns in
// the current tenant.
func (h *ProviderHandler) ListModelGroups(c *gin.Context) {
	userID := c.GetString("user_id")
	ctx := c.Request.Context()

	groups, err := h.modelProviderService.ListModelGroup(ctx, userID)
	if err != nil {
		common.RespondError(c, common.CodeDataError, err.Error())
		return
	}
	common.RespondSuccess(c, groups, "success")
}

// ShowModelGroup returns the models configured in one group.
func (h *ProviderHandler) ShowModelGroup(c *gin.Context) {
	ctx := c.Request.Context()
	groupName := c.Param("group_name")

	models, err := h.modelProviderService.ShowModelGroup(ctx, groupName)
	if err != nil {
		common.RespondError(c, common.CodeDataError, err.Error())
		return
	}
	common.RespondSuccess(c, models, "success")
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
		err := h.modelProviderService.AddModelToGroupByID(ctx, req.ModelID, groupName)
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
	CompositeModelName string `json:"composite_model_name" binding:"required"`
}

// RemoveModelFromGroup removes a model from the group named in the path. The
// model is always identified by its composite name so that the request is
// scoped to a group and a tenant-checked model; a bare mapping-row ID would
// let any authenticated user delete rows belonging to other users.
func (h *ProviderHandler) RemoveModelFromGroup(c *gin.Context) {
	var req DeleteModelToGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, common.CodeBadRequest, "Composite model name is required")
		return
	}

	ctx := c.Request.Context()
	userID := c.GetString("user_id")
	groupName := c.Param("group_name")

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

	common.RespondSuccess(c, nil, "success")
}
