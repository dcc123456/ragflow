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
	"os"
	"strings"

	"ragflow/internal/common"
	"ragflow/internal/utility"

	"github.com/gin-gonic/gin"
)

// GetEnableAdmin admin access endpoint
// @Description Enable admin access endpoint
// @Tags system
// @Produce plain
// @Router /api/v1/system/enable-admin [GET]
// GetEnableAdmin reports whether the admin console is enabled for this
// deployment, mirroring python's settings.ENABLE_ADMIN.
func (h *SystemHandler) GetEnableAdmin(c *gin.Context) {
	common.RespondSuccess(c, gin.H{"enable": enableAdminFlag()}, "")
}

// enableAdminFlag reads ENABLE_ADMIN once per call; the python default is off.
func enableAdminFlag() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ENABLE_ADMIN"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// GetCores /api/v1/system/cores [GET]
func (h *SystemHandler) GetCores(c *gin.Context) {
	common.RespondError(c, common.CodeNotImplemented, "Get cores not implemented")
}

// SetCores /api/v1/system/cores [PUT]
func (h *SystemHandler) SetCores(c *gin.Context) {
	common.StartOperationLog(c, "set_system_cores")
	defer common.FinishOperationLog(c)
	common.RespondError(c, common.CodeNotImplemented, "Set cores not implemented")
}

// GetMemory /api/v1/system/memory [GET]
func (h *SystemHandler) GetMemory(c *gin.Context) {
	common.RespondError(c, common.CodeNotImplemented, "Get memory not implemented")
}

// SetMemory /api/v1/system/memory [PUT]
func (h *SystemHandler) SetMemory(c *gin.Context) {
	common.StartOperationLog(c, "set_system_memory")
	defer common.FinishOperationLog(c)
	common.RespondError(c, common.CodeNotImplemented, "Set memory not implemented")
}

// GetConcurrency /api/v1/system/concurrency [GET]
func (h *SystemHandler) GetConcurrency(c *gin.Context) {
	common.RespondError(c, common.CodeNotImplemented, "Get concurrency not implemented")
}

// SetConcurrency /api/v1/system/concurrency [PUT]
func (h *SystemHandler) SetConcurrency(c *gin.Context) {
	common.StartOperationLog(c, "set_system_concurrency")
	defer common.FinishOperationLog(c)
	common.RespondError(c, common.CodeNotImplemented, "Set concurrency not implemented")
}

// GetQuota /api/v1/system/quota [GET]
func (h *SystemHandler) GetQuota(c *gin.Context) {
	quota := utility.GetAPIServiceQuota()
	if quota == nil {
		common.RespondError(c, common.CodeNotFound, "API Service quota is not set")
	}
	common.RespondSuccess(c, quota, "success")
}

// ListFeatures /api/v1/system/features [GET]
func (h *SystemHandler) ListFeatures(c *gin.Context) {
	ctx := c.Request.Context()

	features, err := h.systemService.ListFeatures(ctx)
	if err != nil {
		common.RespondError(c, common.CodeServerError, err.Error())
		return
	}

	common.RespondSuccess(c, features, "SUCCESS")
}
