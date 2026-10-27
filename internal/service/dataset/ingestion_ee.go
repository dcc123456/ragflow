// Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dataset

import (
	"context"
	"errors"
	"fmt"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/service"
)

// GetIngestionLogForAdmin returns the run and current document fields used by the log dialog.
// The caller must authenticate a system administrator before calling.
func GetIngestionLogForAdmin(ctx context.Context, logID string) (map[string]interface{}, common.ErrorCode, error) {
	run, code, err := getAdminIngestionLog(ctx, logID, "id", "document_id", "run_count", "document_name",
		"document_suffix", "process_begin_at", "process_duration", "operation_status")
	if err != nil {
		return nil, code, err
	}
	operationStatus := normalizeAdminIngestionOperationStatus(run.OperationStatus)
	result := map[string]interface{}{
		"id":               run.ID,
		"document_id":      run.DocumentID,
		"run_count":        0,
		"name":             run.DocumentName,
		"suffix":           run.DocumentSuffix,
		"nickname":         "",
		"create_date":      nil,
		"size":             nil,
		"chunk_count":      nil,
		"process_begin_at": run.ProcessBeginAt,
		"process_duration": run.ProcessDuration,
		"operation_status": operationStatus,
	}
	if run.RunCount != nil {
		result["run_count"] = *run.RunCount
	}

	document, err := dao.NewDocumentDAO().GetIngestionLogDocument(ctx, dao.DB, run.DocumentID)
	if err != nil {
		if dao.IsNotFoundErr(err) {
			return result, common.CodeSuccess, nil
		}
		return nil, common.CodeServerError, fmt.Errorf("get ingestion document: %w", err)
	}
	if document.Name != nil {
		result["name"] = *document.Name
	}
	result["suffix"] = document.Suffix
	result["nickname"] = ptrStringValue(document.Nickname)
	result["create_date"] = document.CreateDate
	result["size"] = document.Size
	result["chunk_count"] = document.ChunkNum
	// A queued retry can still carry the previous run's document timestamps.
	if operationStatus == string(entity.TaskStatusRunning) && document.PipelineLogID != nil &&
		*document.PipelineLogID == run.ID && document.ProcessBeginAt != nil {
		result["process_begin_at"] = document.ProcessBeginAt
		result["process_duration"] = max(0, time.Since(*document.ProcessBeginAt).Seconds())
	}
	return result, common.CodeSuccess, nil
}

// GetIngestionMessagesForAdmin returns a page of one run's persisted events.
// The caller must authenticate a system administrator before calling.
func GetIngestionMessagesForAdmin(ctx context.Context, logID string, limit int, afterID, beforeID *int) (*IngestionMessagesResponse, common.ErrorCode, error) {
	if logID == "" {
		return nil, common.CodeArgumentError, errors.New(`lack of "Log ID"`)
	}
	if afterID != nil && beforeID != nil {
		return nil, common.CodeArgumentError, errors.New("after_id and before_id are mutually exclusive")
	}
	if afterID != nil && *afterID <= 0 {
		return nil, common.CodeArgumentError, errors.New("after_id must be a positive integer")
	}
	if beforeID != nil && *beforeID <= 0 {
		return nil, common.CodeArgumentError, errors.New("before_id must be a positive integer")
	}
	if limit == 0 {
		limit = defaultIngestionMessagesLimit
	}
	if limit < 0 || limit > maxIngestionMessagesLimit {
		return nil, common.CodeArgumentError, fmt.Errorf("limit must be between 1 and %d", maxIngestionMessagesLimit)
	}

	run, code, err := getAdminIngestionLog(ctx, logID, "id", "document_id", "run_count", "operation_status")
	if err != nil {
		return nil, code, err
	}

	eventDAO := dao.NewIngestionTaskLogDAO()
	page, err := eventDAO.ListEventsPageByPipelineLogID(ctx, dao.DB, run.ID, limit, afterID, beforeID)
	if err != nil {
		return nil, common.CodeServerError, fmt.Errorf("list ingestion messages: %w", err)
	}
	messages := &IngestionMessagesResponse{
		Items:         make([]service.IngestionEventItem, 0, len(page.Events)),
		HasMoreBefore: page.HasMoreBefore,
		HasMoreAfter:  page.HasMoreAfter,
		Terminal:      isTerminalIngestionLogStatus(run.OperationStatus),
	}
	if run.RunCount != nil {
		messages.RunCount = *run.RunCount
	}
	for _, event := range page.Events {
		messages.Items = append(messages.Items, service.IngestionEventItemFromLog(event))
	}
	if len(messages.Items) > 0 {
		messages.OldestID = messages.Items[0].ID
		messages.NewestID = messages.Items[len(messages.Items)-1].ID
	}

	return messages, common.CodeSuccess, nil
}

func getAdminIngestionLog(ctx context.Context, logID string, columns ...string) (*entity.PipelineOperationLog, common.ErrorCode, error) {
	if logID == "" {
		return nil, common.CodeArgumentError, errors.New(`lack of "Log ID"`)
	}
	run, err := dao.NewPipelineOperationLogDAO().GetByID(ctx, dao.DB.Select(columns), logID)
	if err != nil {
		if dao.IsNotFoundErr(err) {
			return nil, common.CodeDataError, errors.New("log not found")
		}
		return nil, common.CodeServerError, fmt.Errorf("get ingestion log: %w", err)
	}
	if !isReadableIngestionLog(run) {
		return nil, common.CodeDataError, errors.New("log not found")
	}
	return run, common.CodeSuccess, nil
}

func normalizeAdminIngestionOperationStatus(status string) string {
	switch status {
	case "UNSTART":
		return string(entity.TaskStatusUnstart)
	case "RUNNING":
		return string(entity.TaskStatusRunning)
	case "CANCEL":
		return string(entity.TaskStatusCancel)
	case "DONE":
		return string(entity.TaskStatusDone)
	case "FAIL":
		return string(entity.TaskStatusFail)
	case "SCHEDULE":
		return string(entity.TaskStatusSchedule)
	default:
		return status
	}
}
