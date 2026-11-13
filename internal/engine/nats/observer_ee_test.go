//go:build ee

package nats

import (
	"context"
	"errors"
	"testing"
	"time"

	"ragflow/internal/common"
)

func TestQueueMetrics(t *testing.T) {
	host, port := newEmbeddedNatsServer(t)
	queue := NewNatsEngine(host, port)
	if err := queue.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(queue.nc.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for range 2 {
		if err := queue.PublishTask(common.TaskSubjectMedium, []byte(`{"task_id":"metrics"}`)); err != nil {
			t.Fatal(err)
		}
	}
	assertQueue := func(pending, inProgress, completed uint64) {
		t.Helper()
		metrics, err := queue.QueueMetrics(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var got *QueueMetrics
		for i := range metrics {
			if metrics[i].Name == "RAGFLOW_TASKS_MEDIUM" {
				got = &metrics[i]
				break
			}
		}
		if got == nil {
			t.Fatalf("medium lane missing from metrics: %+v", metrics)
		}
		want := QueueMetrics{"RAGFLOW_TASKS_MEDIUM", pending, inProgress, completed}
		if *got != want {
			t.Fatalf("medium queue metrics = %+v, want %+v", *got, want)
		}
	}
	assertQueue(2, 0, 0)
	if err := queue.InitConsumer(common.TaskSubjectMedium); err != nil {
		t.Fatal(err)
	}
	messages, err := queue.PullMessages(ctx, 1)
	if err != nil || len(messages) != 1 {
		t.Fatalf("pull = %v, %v", messages, err)
	}
	assertQueue(1, 1, 0)
	if err := messages[0].(*NatsMessageHandle).message.DoubleAck(ctx); err != nil {
		t.Fatal(err)
	}
	assertQueue(1, 0, 1)
	if err := queue.stream.DeleteConsumer(ctx, "RAGFLOW_CONSUMER_MEDIUM"); err != nil {
		t.Fatal(err)
	}
	assertQueue(1, 0, 0)
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err := queue.QueueMetrics(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was lost: %v", err)
	}
}

func TestQueueMetricsUninitialized(t *testing.T) {
	if _, err := NewNatsEngine("", 0).QueueMetrics(t.Context()); err == nil {
		t.Fatal("uninitialized JetStream was accepted")
	}
}

func TestQueueMetricsIncludesPushConsumers(t *testing.T) {
	host, port := newEmbeddedNatsServer(t)
	queue := NewNatsEngine(host, port)
	if err := queue.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(queue.nc.Close)
	if err := queue.InitSyncerStream(); err != nil {
		t.Fatal(err)
	}
	if err := queue.InitSyncerConsumer(); err != nil {
		t.Fatal(err)
	}
	if err := queue.InitKnowledgeCompileStream(); err != nil {
		t.Fatal(err)
	}
	metrics, err := queue.QueueMetrics(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 5 {
		t.Fatalf("metrics = %+v, want three task lanes plus knowledge-compile and syncer", metrics)
	}
	for _, metric := range metrics {
		if metric.Pending != 0 || metric.InProgress != 0 || metric.Completed != 0 {
			t.Errorf("empty queue metrics = %+v", metric)
		}
	}
}
