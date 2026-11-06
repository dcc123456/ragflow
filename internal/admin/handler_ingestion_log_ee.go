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
	"strconv"

	"ragflow/internal/common"
	"ragflow/internal/service/dataset"

	"github.com/gin-gonic/gin"
)

// GetIngestionLog returns the file and run details for the log dialog.
func (h *Handler) GetIngestionLog(c *gin.Context) {
	result, code, err := dataset.GetIngestionLogForAdmin(c.Request.Context(), c.Param("log_id"))
	if err != nil {
		common.RespondError(c, code, err.Error())
		return
	}
	common.RespondSuccess(c, result, "success")
}

// GetIngestionMessages returns a run's keyset-paginated events.
func (h *Handler) GetIngestionMessages(c *gin.Context) {
	limit := 0
	if rawLimit := c.Query("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed <= 0 {
			common.RespondError(c, common.CodeArgumentError, "limit must be a positive integer")
			return
		}
		limit = parsed
	}
	afterID, ok := adminIngestionEventCursor(c, "after_id")
	if !ok {
		return
	}
	beforeID, ok := adminIngestionEventCursor(c, "before_id")
	if !ok {
		return
	}
	result, code, err := dataset.GetIngestionMessagesForAdmin(c.Request.Context(), c.Param("log_id"), limit, afterID, beforeID)
	if err != nil {
		common.RespondError(c, code, err.Error())
		return
	}
	common.RespondSuccess(c, result, "success")
}

func adminIngestionEventCursor(c *gin.Context, name string) (*int, bool) {
	raw := c.Query(name)
	if raw == "" {
		return nil, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		common.RespondError(c, common.CodeArgumentError, name+" must be a positive integer")
		return nil, false
	}
	return &value, true
}
