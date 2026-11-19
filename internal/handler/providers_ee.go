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
	"strings"

	"ragflow/internal/common"
	"ragflow/internal/entity"

	"github.com/gin-gonic/gin"
)

type CreateModelGroupRequest struct {
	Name string `json:"name"`
}

func (h *ProviderHandler) CreateModelGroup(c *gin.Context) {
	common.StartOperationLog(c, "create_model_group")
	defer common.FinishOperationLog(c)

	var req CreateModelGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, common.CodeBadRequest, "Group name is required")
		return
	}

	common.SetOperationResource(c, "group_name", req.Name)
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
	common.StartOperationLog(c, "delete_model_group")
	defer common.FinishOperationLog(c)
	if id := c.Param("group_id"); id != "" {
		common.SetOperationResource(c, "group_id", id)
	}

	groupID := c.Param("group_id")

	ctx := c.Request.Context()

	err := h.modelProviderService.DeleteModelGroup(ctx, groupID)
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
	groupID := c.Param("group_id")

	models, err := h.modelProviderService.ShowModelGroup(ctx, groupID)
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
	common.StartOperationLog(c, "add_model_to_group")
	defer common.FinishOperationLog(c)
	if id := c.Param("group_id"); id != "" {
		common.SetOperationResource(c, "group_id", id)
	}

	var req AddModelToGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, common.CodeBadRequest, "Group name is required")
		return
	}

	userID := c.GetString("user_id")
	ctx := c.Request.Context()
	groupID := c.Param("group_id")

	if req.CompositeModelName == "" {
		common.SetOperationResource(c, "model_id", req.ModelID)
		err := h.modelProviderService.AddModelToGroupByID(ctx, req.ModelID, groupID)
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
		common.SetOperationResource(c, "provider_id_or_name", providerName)
		common.SetOperationResource(c, "instance_id_or_name", instanceName)
		common.SetOperationResource(c, "model_name", modelName)
		err = h.modelProviderService.AddModelToGroupByName(ctx, userID, providerName, instanceName, modelName, groupID)
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

// RemoveModelFromGroup removes a model from the group identified in the path.
// Normal rows use the composite model name; unavailable rows can be removed by
// model ID without requiring the deleted model metadata to resolve.
func (h *ProviderHandler) RemoveModelFromGroup(c *gin.Context) {
	common.StartOperationLog(c, "remove_model_from_group")
	defer common.FinishOperationLog(c)
	if id := c.Param("group_id"); id != "" {
		common.SetOperationResource(c, "group_id", id)
	}

	var req DeleteModelToGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil || (req.CompositeModelName == "" && req.ModelID == "") {
		common.RespondError(c, common.CodeBadRequest, "Model ID or composite model name is required")
		return
	}

	ctx := c.Request.Context()
	userID := c.GetString("user_id")
	groupID := c.Param("group_id")
	if req.ModelID != "" {
		common.SetOperationResource(c, "model_id", req.ModelID)
		if err := h.modelProviderService.RemoveModelFromGroupByID(ctx, req.ModelID, groupID); err != nil {
			common.RespondError(c, common.CodeDataError, err.Error())
			return
		}
		common.RespondSuccess(c, nil, "success")
		return
	}

	modelName, instanceName, providerName, err := common.ParseCompositeModelName(req.CompositeModelName)
	if err != nil {
		common.RespondError(c, common.CodeBadRequest, err.Error())
		return
	}
	common.SetOperationResource(c, "provider_id_or_name", providerName)
	common.SetOperationResource(c, "instance_id_or_name", instanceName)
	common.SetOperationResource(c, "model_name", modelName)
	err = h.modelProviderService.RemoveModelFromGroupByName(ctx, userID, providerName, instanceName, modelName, groupID)
	if err != nil {
		common.RespondError(c, common.CodeDataError, err.Error())
		return
	}

	common.RespondSuccess(c, nil, "success")
}

type SetGroupModelStatusRequest struct {
	CompositeModelName string `json:"composite_model_name" binding:"required"`
	Status             string `json:"status" binding:"required"`
}

// SetGroupModelStatus switches one group member's availability inside the
// group between active and inactive. It only flips the membership row's
// status: the group keeps its ordering and the model stays usable outside the
// group.
func (h *ProviderHandler) SetGroupModelStatus(c *gin.Context) {
	var req SetGroupModelStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, common.CodeBadRequest, "Composite model name and status are required")
		return
	}

	status := strings.ToLower(strings.TrimSpace(req.Status))
	if status != entity.ModelGroupMapStatusActive && status != entity.ModelGroupMapStatusInactive {
		common.RespondError(c, common.CodeBadRequest, "Status must be active or inactive")
		return
	}

	ctx := c.Request.Context()
	userID := c.GetString("user_id")
	groupID := c.Param("group_id")

	modelName, instanceName, providerName, err := common.ParseCompositeModelName(req.CompositeModelName)
	if err != nil {
		common.RespondError(c, common.CodeBadRequest, err.Error())
		return
	}
	err = h.modelProviderService.SetModelStatusInGroupByName(ctx, userID, providerName, instanceName, modelName, groupID, status)
	if err != nil {
		common.RespondError(c, common.CodeDataError, err.Error())
		return
	}

	common.RespondSuccess(c, nil, "success")
}

type ReorderModelGroupMembersRequest struct {
	ModelIDs []string `json:"model_ids" binding:"required"`
}

func (h *ProviderHandler) ReorderModelGroupMembers(c *gin.Context) {
	common.StartOperationLog(c, "reorder_model_group_members")
	defer common.FinishOperationLog(c)
	groupID := c.Param("group_id")
	common.SetOperationResource(c, "group_id", groupID)
	var req ReorderModelGroupMembersRequest
	if err := c.ShouldBindJSON(&req); err != nil || len(req.ModelIDs) == 0 {
		common.RespondError(c, common.CodeBadRequest, "Model IDs are required")
		return
	}
	if err := h.modelProviderService.ReorderModelGroupMembers(c.Request.Context(), groupID, req.ModelIDs); err != nil {
		common.RespondError(c, common.CodeDataError, err.Error())
		return
	}
	common.RespondSuccess(c, nil, "success")
}
