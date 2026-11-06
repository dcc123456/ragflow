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

func (h *StatsHandler) GetModelStats(c *gin.Context) {
	common.RespondError(c, common.CodeNotImplemented, "GetModelStats not implemented")
}

func (h *StatsHandler) GetGroupStats(c *gin.Context) {
	common.RespondError(c, common.CodeNotImplemented, "GetGroupStats not implemented")
}
