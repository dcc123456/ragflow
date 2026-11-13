//
//  Copyright 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//

//go:build ee

package service

import (
	"context"
	"errors"
	"fmt"

	"ragflow/internal/common"
	"ragflow/internal/dao"

	"gorm.io/gorm"
)

// ErrTaskNotFound is returned when the promotion target task does not exist.
var ErrTaskNotFound = errors.New("ingestion task not found")

// ErrTaskRunning is returned when the promotion target task is currently
// RUNNING; promoting an in-flight task is rejected so an operator cannot
// pre-empt a task that is already executing.
var ErrTaskRunning = errors.New("ingestion task is running")

// ErrTaskNotInDataset is returned when the promotion target task does not belong
// to the dataset named in the request path. It is surfaced as a 404 so the
// caller cannot probe whether the task exists in another tenant's dataset.
var ErrTaskNotInDataset = errors.New("ingestion task not in dataset")

// PromoteTaskToHigh re-publishes an ingestion task onto the high-priority lane.
// It does NOT change the task's database state: the original low/medium copy, if
// still queued, is left in place as a delivery fallback and is harmlessly
// de-duplicated at consumption time by the ingestor's StartRunning CAS. The
// high lane is reached only through this explicit operator action.
//
// A task that is currently RUNNING is rejected (the operator cannot re-prioritize
// an in-flight task). Terminal and pending tasks are promoted; promoting a
// terminal task re-queues it on the high lane for a re-run.
//
// datasetID, when non-empty, restricts promotion to a task that belongs to that
// dataset. The system-admin path passes an empty datasetID (it is not scoped to a
// tenant); the tenant-admin path passes the dataset from the request so a task
// cannot be promoted through a dataset it does not belong to.
func (s *IngestionTaskService) PromoteTaskToHigh(ctx context.Context, taskID, datasetID string) error {
	task, err := s.ingestionTaskDAO.GetByID(ctx, dao.DB, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTaskNotFound
		}
		return fmt.Errorf("promote task %s: load: %w", taskID, err)
	}
	if datasetID != "" && task.DatasetID != datasetID {
		return ErrTaskNotInDataset
	}
	if task.Status == common.RUNNING {
		return ErrTaskRunning
	}
	subject, err := common.SubjectForTier(common.TierHigh)
	if err != nil {
		return fmt.Errorf("promote task %s: resolve high subject: %w", taskID, err)
	}
	msg := common.TaskMessage{
		TaskID:   task.ID,
		TaskType: common.TaskTypeIngestionTask,
	}
	return s.taskPublisher.PublishTaskMessage(subject, msg)
}
