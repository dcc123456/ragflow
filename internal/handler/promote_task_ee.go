//
//  Copyright 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//

//go:build ee

package handler

import (
	"errors"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/permission"
	servicepkg "ragflow/internal/service"

	"github.com/gin-gonic/gin"
)

// newPromoteTaskService builds the ingestion-task service used by the
// tenant-admin promote handler. It is a variable so tests can substitute a
// service wired to a fake publisher without a live NATS server. The real
// publisher wraps the process-wide message-queue engine, so constructing it per
// call is cheap.
var newPromoteTaskService = func() *servicepkg.IngestionTaskService {
	svc := servicepkg.NewIngestionTaskService()
	svc.SetTaskPublisher(servicepkg.NewMessageQueueTaskPublisher())
	return svc
}

// PromoteIngestionTask promotes a tenant's ingestion task to the high-priority
// lane. The caller must already hold update rights on the dataset named in the
// path (enforced by permission.NewDatabaseChecker, which scopes to the caller's
// tenant). Because the engine scopes dataset lookups to the caller's tenant, a
// dataset the caller does not own is simply invisible: both a cross-tenant
// request and a task that does not belong to the dataset resolve to 404, so the
// caller cannot probe other tenants' tasks.
//
// Responses:
//   - 200 success
//   - 404 the caller lacks update rights on the dataset, or the task does not
//     belong to it (the engine does not reveal datasets outside the tenant)
//   - 409 task is currently RUNNING
func (h *DocumentHandler) PromoteIngestionTask(c *gin.Context) {
	datasetID := c.Param("dataset_id")
	taskID := c.Param("task_id")
	ctx := c.Request.Context()

	if err := permission.NewDatabaseChecker(dao.DB).CheckResource(ctx, permission.ResourceRef{Kind: permission.ResourceKindDataset, ID: datasetID}, permission.OperationUpdate); err != nil {
		respondPermissionError(c, err, false)
		return
	}

	svc := newPromoteTaskService()
	if err := svc.PromoteTaskToHigh(ctx, taskID, datasetID); err != nil {
		switch {
		case errors.Is(err, servicepkg.ErrTaskNotFound), errors.Is(err, servicepkg.ErrTaskNotInDataset):
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
