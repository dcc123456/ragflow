//go:build ee

package handler

import (
	"ragflow/internal/common"
	"ragflow/internal/service"

	"github.com/gin-gonic/gin"
)

// ListSyncEvents returns an adjacent page of one sync task's log.
func ListSyncEvents(c *gin.Context) {
	limit, ok := ingestionEventCursor(c, "limit")
	if !ok {
		return
	}
	afterID, ok := ingestionEventCursor(c, "after_id")
	if !ok {
		return
	}
	beforeID, ok := ingestionEventCursor(c, "before_id")
	if !ok {
		return
	}
	pageSize := 0
	if limit != nil {
		pageSize = *limit
	}
	result, code, err := service.ListSyncEvents(c.Request.Context(), c.Param("task_id"), pageSize, afterID, beforeID)
	if err != nil {
		common.RespondError(c, code, err.Error())
		return
	}
	common.RespondSuccess(c, result, "success")
}
