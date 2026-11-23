//go:build ee

package service

import (
	"context"
	"errors"
	"fmt"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/engine/clickhouse"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
	permissionresponse "ragflow/internal/permission/response"

	"gorm.io/gorm"
)

type SyncEventsResponse struct {
	Items         []clickhouse.SyncEvent `json:"items"`
	OldestID      int                    `json:"oldest_id"`
	NewestID      int                    `json:"newest_id"`
	HasMoreBefore bool                   `json:"has_more_before"`
	HasMoreAfter  bool                   `json:"has_more_after"`
	Terminal      bool                   `json:"terminal"`
}

func ListSyncEvents(ctx context.Context, taskID string, limit int, afterID, beforeID *int) (*SyncEventsResponse, common.ErrorCode, error) {
	if limit == 0 {
		limit = 200
	}
	if limit < 1 || limit > 500 || (afterID != nil && beforeID != nil) {
		return nil, common.CodeArgumentError, errors.New("limit must be between 1 and 500 and cursors are mutually exclusive")
	}

	checker := permission.NewDatabaseChecker(dao.DB)
	if err := checker.CheckResource(ctx, permission.ResourceRef{Kind: permission.ResourceKindSyncTask, ID: taskID}, permission.OperationRead); err != nil {
		code, hidden := permissionresponse.NormalizeHidden(err)
		return nil, code, hidden
	}
	var task entity.SyncLogs
	err := dao.DB.WithContext(ctx).Select("connector_id", "kb_id", "status").
		Where("id = ?", taskID).Take(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		code, hidden := permissionresponse.NormalizeHidden(permission.ErrResourceNotFound)
		return nil, code, hidden
	}
	if err != nil {
		return nil, common.CodeServerError, fmt.Errorf("load sync task: %w", err)
	}
	for _, ref := range []permission.ResourceRef{
		{Kind: permission.ResourceKindConnector, ID: task.ConnectorID},
		{Kind: permission.ResourceKindDataset, ID: task.KbID},
	} {
		if err := checker.CheckResource(ctx, ref, permission.OperationRead); err != nil {
			code, hidden := permissionresponse.NormalizeHidden(err)
			return nil, code, hidden
		}
	}

	response := &SyncEventsResponse{
		Items:    []clickhouse.SyncEvent{},
		Terminal: task.Status == dao.SyncStatusDone || task.Status == dao.SyncStatusFail || task.Status == dao.SyncStatusCancel,
	}
	driver := clickhouse.GetDriver()
	if driver == nil {
		return response, common.CodeSuccess, nil
	}
	page, err := driver.ListSyncEvents(ctx, taskID, limit, afterID, beforeID)
	if err != nil {
		return nil, common.CodeServerError, fmt.Errorf("list sync task events: %w", err)
	}
	if len(page.Events) != 0 {
		response.Items = page.Events
		response.OldestID = page.Events[0].ID
		response.NewestID = page.Events[len(page.Events)-1].ID
	}
	response.HasMoreBefore = page.HasMoreBefore
	response.HasMoreAfter = page.HasMoreAfter
	return response, common.CodeSuccess, nil
}
