//
//  Copyright 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//

//go:build ee

package admin

import (
	"errors"

	"ragflow/internal/common"
	servicepkg "ragflow/internal/service"

	"github.com/gin-gonic/gin"
)

// PromoteIngestionTask promotes any tenant's ingestion task to the high-priority
// lane. System administrators are not scoped to a tenant, so no resource
// permission check is performed here; only task existence and liveness guard the
// action.
//
// Responses:
//   - 200 success
//   - 404 task not found
//   - 409 task is currently RUNNING
func (h *Handler) PromoteIngestionTask(c *gin.Context) {
	common.StartOperationLog(c, "promote_admin_ingestion_task")
	defer common.FinishOperationLog(c)
	common.SetOperationResource(c, "task_id", c.Param("task_id"))

	taskID := c.Param("task_id")
	ctx := c.Request.Context()

	if err := h.service.ingestionTaskSvc.PromoteTaskToHigh(ctx, taskID, ""); err != nil {
		switch {
		case errors.Is(err, servicepkg.ErrTaskNotFound):
			common.RespondError(c, common.CodeNotFound, "ingestion task not found")
		case errors.Is(err, servicepkg.ErrTaskRunning):
			common.RespondError(c, common.CodeConflict, "ingestion task is running")
		default:
			common.RespondError(c, common.CodeServerError, err.Error())
		}
		return
	}
	common.RespondSuccess(c, nil, "success")
}
