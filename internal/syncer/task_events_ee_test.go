//go:build ee

package syncer

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/engine/clickhouse"
	"ragflow/internal/entity"
	"ragflow/internal/service"
	syncerconnector "ragflow/internal/syncer/connector"
	connectormock "ragflow/internal/syncer/connector/mock"
)

func TestSyncTaskTimeline(t *testing.T) {
	var mu sync.Mutex
	var events []clickhouse.SyncEvent
	old := collectTaskEvent
	collectTaskEvent = func(event clickhouse.SyncEvent) error {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
		return nil
	}
	t.Cleanup(func() { collectTaskEvent = old })
	db := setupSyncerDB(t)
	insertTaskContext(t, db, "conn-1", "kb-1", "task-1", dao.TaskTypeSync)
	taskDAO := dao.NewSyncTaskDAO(db)
	taskService := service.NewSyncTaskService(taskDAO)
	now := time.Now()
	connector := &connectormock.Connector{SyncBatches: []syncerconnector.SyncBatch{{Documents: []syncerconnector.SourceDocument{{SourceID: "a", UpdatedAt: now}}}}}
	handle := &fakeTaskHandle{msg: common.TaskMessage{TaskID: "task-1", TaskType: common.TaskTypeSyncer}}
	worker := NewTaskWorker(
		make(chan TaskEnvelope, 1),
		taskDAO,
		taskService,
		newCoordinator(taskDAO, taskService, newTestRegistry(map[string]*connectormock.Connector{"conn-1": connector}), &fakeSink{}, nil, fakeStore{}),
		NewConnectorLock(),
	)
	worker.handle(t.Context(), TaskEnvelope{TaskID: "task-1", Handle: handle})

	if handle.acks != 1 || handle.nacks != 0 {
		t.Fatalf("settlement acks=%d nacks=%d", handle.acks, handle.nacks)
	}
	var task entity.SyncLogs
	if err := db.First(&task, "id = ?", "task-1").Error; err != nil {
		t.Fatalf("load task: %v", err)
	}
	if task.Status != dao.SyncStatusDone {
		t.Fatalf("status = %s, want done", task.Status)
	}

	mu.Lock()
	defer mu.Unlock()
	if !slices.ContainsFunc(events, func(event clickhouse.SyncEvent) bool {
		return event.Component == "file" && strings.HasSuffix(event.Message, " added")
	}) {
		t.Fatalf("missing file addition event: %+v", events)
	}
	if len(events) < 4 || events[0].Message != "Task started" ||
		events[len(events)-1].Message != "Task completed" ||
		events[len(events)-1].TaskID != "task-1" {
		t.Fatalf("unexpected timeline: %+v", events)
	}
}

type timelineSink func(service.DocumentUpsertInput) (service.DocumentUpsertResult, error)

func (f timelineSink) Upsert(_ context.Context, input service.DocumentUpsertInput) (service.DocumentUpsertResult, error) {
	return f(input)
}

type timelineDeleter func(string) error

func (f timelineDeleter) DeleteDocument(_ context.Context, docID string) error {
	return f(docID)
}

func TestFileOutcomeEvents(t *testing.T) {
	var messages []string
	old := collectTaskEvent
	collectTaskEvent = func(event clickhouse.SyncEvent) error {
		if event.Component == "file" {
			messages = append(messages, event.Message)
		}
		return nil
	}
	t.Cleanup(func() { collectTaskEvent = old })
	db := setupSyncerDB(t)
	insertTaskContext(t, db, "conn-1", "kb-1", "task-1", dao.TaskTypeSync)
	d := dao.NewSyncTaskDAO(db)
	task, err := d.GetTaskContext(t.Context(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	saveErr := errors.New("private provider error")
	sink := timelineSink(func(input service.DocumentUpsertInput) (service.DocumentUpsertResult, error) {
		if input.SourceDocument.SourceID == "bad" {
			return service.DocumentUpsertResult{}, saveErr
		}
		return service.DocumentUpsertResult{DocID: input.DocumentID, Action: input.SourceDocument.SourceID}, nil
	})
	runner := NewSyncRunner(SyncRunnerConfig{}, d, nil, sink, service.NewDocumentIDResolver(fakeStore{}), nil, nil)
	stats, err := runner.processDocuments(t.Context(), task, "source", nil, []syncerconnector.SourceDocument{
		{SourceID: "added"}, {SourceID: "updated"}, {SourceID: "skipped"}, {SourceID: "bad"},
	})
	if !errors.Is(err, saveErr) || stats.Added != 1 || stats.Updated != 1 || stats.Skipped != 1 {
		t.Fatalf("stats = %+v, err = %v", stats, err)
	}
	deleteErr := errors.New("private delete error")
	for _, failing := range []bool{false, true} {
		pruner := NewPruneRunner(d, service.NewSyncTaskService(d), service.NewSyncPruneService(timelineDeleter(func(string) error {
			if failing {
				return deleteErr
			}
			return nil
		}), fakeStore{ids: map[string]struct{}{"stale": {}}}))
		// The failed attempt exits before completion and uses the same deletion path.
		if failing {
			_, err := pruner.Run(t.Context(), task, &connectormock.Connector{})
			if !errors.Is(err, deleteErr) {
				t.Fatalf("prune error = %v", err)
			}
		} else {
			if _, err := pruner.Run(t.Context(), task, &connectormock.Connector{}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, want := range []string{" added", " updated", " skipped", "[ERROR] Could not save file", "File stale deleted", "[ERROR] Could not delete file stale"} {
		if !slices.ContainsFunc(messages, func(message string) bool { return strings.Contains(message, want) }) {
			t.Errorf("missing %q in %v", want, messages)
		}
	}
	if slices.ContainsFunc(messages, func(message string) bool { return strings.Contains(message, "private") }) {
		t.Fatalf("provider details reached the event log: %v", messages)
	}
}
