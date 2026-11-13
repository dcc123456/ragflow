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
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"

	"gorm.io/gorm"
)

// fakeTaskPublisher records every publish call so tests can assert the lane and
// task identity without a live NATS server.
type fakeTaskPublisher struct {
	calls   []string
	msgs    []common.TaskMessage
	failErr error
}

func (f *fakeTaskPublisher) PublishTaskMessage(subject string, msg common.TaskMessage) error {
	f.calls = append(f.calls, subject)
	f.msgs = append(f.msgs, msg)
	if f.failErr != nil {
		return f.failErr
	}
	return nil
}

func newPromoteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	testDB := setupServiceTestDB(t)
	if err := testDB.AutoMigrate(&entity.IngestionTask{}); err != nil {
		t.Fatalf("migrate ingestion_task: %v", err)
	}
	orig := dao.DB
	dao.DB = testDB
	t.Cleanup(func() { dao.DB = orig })
	return testDB
}

func insertTask(t *testing.T, db *gorm.DB, id, status string) {
	t.Helper()
	task := &entity.IngestionTask{
		ID:         id,
		UserID:     "u-" + id,
		DocumentID: "d-" + id,
		DatasetID:  "ds-" + id,
		Status:     status,
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatalf("insert task %s: %v", id, err)
	}
}

// R1 (happy + adversarial): a free-tenant ("Trial") task must still be promoted
// to the HIGH lane, never downgraded to its plan's default lane. If the
// implementation mistakenly routed through resolveTier, this would publish to
// the default lane and fail. The empty datasetID means "no tenant scope" (the
// system-admin path).
func TestPromoteTaskToHigh_PublishesToHighLaneRegardlessOfPlan(t *testing.T) {
	db := newPromoteTestDB(t)
	insertTask(t, db, "free-task", common.CREATED)

	pub := &fakeTaskPublisher{}
	svc := NewIngestionTaskService()
	svc.SetTaskPublisher(pub)

	if err := svc.PromoteTaskToHigh(context.Background(), "free-task", ""); err != nil {
		t.Fatalf("PromoteTaskToHigh = %v, want nil", err)
	}
	if len(pub.calls) != 1 {
		t.Fatalf("publisher called %d times, want 1", len(pub.calls))
	}
	if pub.calls[0] != common.TaskSubjectHigh {
		t.Errorf("subject = %q, want high lane %q", pub.calls[0], common.TaskSubjectHigh)
	}
	if pub.msgs[0].TaskID != "free-task" {
		t.Errorf("TaskID = %q, want free-task", pub.msgs[0].TaskID)
	}
	if pub.msgs[0].TaskType != common.TaskTypeIngestionTask {
		t.Errorf("TaskType = %q, want %q", pub.msgs[0].TaskType, common.TaskTypeIngestionTask)
	}
}

// R1 extension: a terminal (FAILED) task may be re-prioritized for a re-run.
func TestPromoteTaskToHigh_TerminalTaskRepromotable(t *testing.T) {
	db := newPromoteTestDB(t)
	insertTask(t, db, "failed-task", common.FAILED)

	pub := &fakeTaskPublisher{}
	svc := NewIngestionTaskService()
	svc.SetTaskPublisher(pub)

	if err := svc.PromoteTaskToHigh(context.Background(), "failed-task", ""); err != nil {
		t.Fatalf("promote FAILED task = %v", err)
	}
	if len(pub.calls) != 1 || pub.calls[0] != common.TaskSubjectHigh {
		t.Fatalf("expected one high-lane publish, got %v", pub.calls)
	}
}

// R3: a RUNNING task is rejected; the publisher must NOT be called.
func TestPromoteTaskToHigh_RunningReturnsConflict(t *testing.T) {
	db := newPromoteTestDB(t)
	insertTask(t, db, "running-task", common.RUNNING)

	pub := &fakeTaskPublisher{}
	svc := NewIngestionTaskService()
	svc.SetTaskPublisher(pub)

	err := svc.PromoteTaskToHigh(context.Background(), "running-task", "")
	if !errors.Is(err, ErrTaskRunning) {
		t.Fatalf("err = %v, want ErrTaskRunning", err)
	}
	if len(pub.calls) != 0 {
		t.Fatalf("publisher called %d times on RUNNING task, want 0", len(pub.calls))
	}
}

// R4: a missing task returns ErrTaskNotFound and publishes nothing.
func TestPromoteTaskToHigh_NotFound(t *testing.T) {
	newPromoteTestDB(t)
	pub := &fakeTaskPublisher{}
	svc := NewIngestionTaskService()
	svc.SetTaskPublisher(pub)

	err := svc.PromoteTaskToHigh(context.Background(), "ghost", "")
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("err = %v, want ErrTaskNotFound", err)
	}
	if len(pub.calls) != 0 {
		t.Fatalf("publisher called %d times for missing task, want 0", len(pub.calls))
	}
}

// R4 (cross-dataset): a non-empty datasetID that does not match the task's
// dataset is rejected (ErrTaskNotInDataset) and publishes nothing. This blocks
// the cross-dataset reference attack from the tenant-admin route.
func TestPromoteTaskToHigh_WrongDatasetRejected(t *testing.T) {
	db := newPromoteTestDB(t)
	insertTask(t, db, "task-x", common.CREATED)

	pub := &fakeTaskPublisher{}
	svc := NewIngestionTaskService()
	svc.SetTaskPublisher(pub)

	err := svc.PromoteTaskToHigh(context.Background(), "task-x", "other-dataset")
	if !errors.Is(err, ErrTaskNotInDataset) {
		t.Fatalf("err = %v, want ErrTaskNotInDataset", err)
	}
	if len(pub.calls) != 0 {
		t.Fatalf("publisher called %d times for mismatched dataset, want 0", len(pub.calls))
	}
}

// R5: promoting the same pending task twice yields two high-lane publishes;
// de-duplication is the consumer's job (CAS), not the publisher's.
func TestPromoteTaskToHigh_IdempotentDoublePromote(t *testing.T) {
	db := newPromoteTestDB(t)
	insertTask(t, db, "dup-task", common.CREATED)

	pub := &fakeTaskPublisher{}
	svc := NewIngestionTaskService()
	svc.SetTaskPublisher(pub)

	if err := svc.PromoteTaskToHigh(context.Background(), "dup-task", ""); err != nil {
		t.Fatalf("first promote = %v", err)
	}
	if err := svc.PromoteTaskToHigh(context.Background(), "dup-task", ""); err != nil {
		t.Fatalf("second promote = %v", err)
	}
	if len(pub.calls) != 2 {
		t.Fatalf("publisher called %d times, want 2 (both high)", len(pub.calls))
	}
	for i, s := range pub.calls {
		if s != common.TaskSubjectHigh {
			t.Errorf("call %d subject = %q, want high lane", i, s)
		}
	}
}
