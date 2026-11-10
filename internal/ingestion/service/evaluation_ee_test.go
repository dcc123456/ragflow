//go:build ee

package service

import (
	"context"
	"testing"
	"time"

	"ragflow/internal/entity"
	"ragflow/internal/ingestion"
	"ragflow/internal/ingestion/testutil"
)

func TestEvaluationReconcilerJoinsWorkerLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	e := &Ingestor{ctx: ctx, deps: &ingestion.Deps{
		DB: testutil.SetupTestDB(t, &entity.EvaluationRun{}),
		MQ: testutil.SetupNatsEngine(t),
	}}
	e.startEvaluationTaskReconciler()
	joined := make(chan struct{})
	go func() { e.workerWg.Wait(); close(joined) }()
	select {
	case <-joined:
		t.Fatal("worker lifecycle did not retain evaluation reconciler")
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	select {
	case <-joined:
	case <-time.After(2 * time.Second):
		t.Fatal("evaluation reconciler did not stop with workers")
	}
}
