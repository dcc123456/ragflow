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
		if err := queue.PublishTask(common.TaskSubject, []byte(`{"task_id":"metrics"}`)); err != nil {
			t.Fatal(err)
		}
	}
	assertQueue := func(pending, inProgress, completed uint64) {
		t.Helper()
		metrics, err := queue.QueueMetrics(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(metrics) != 1 || metrics[0] != (QueueMetrics{"RAGFLOW_TASKS", pending, inProgress, completed}) {
			t.Fatalf("queue metrics = %+v, want pending=%d, inProgress=%d, completed=%d", metrics, pending, inProgress, completed)
		}
	}
	assertQueue(2, 0, 0)
	if err := queue.InitConsumer(common.TaskSubject); err != nil {
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
	if err := queue.stream.DeleteConsumer(ctx, "RAGFLOW_CONSUMER"); err != nil {
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
	if len(metrics) != 3 {
		t.Fatalf("metrics = %+v, want all three task streams", metrics)
	}
	for _, metric := range metrics {
		if metric.Pending != 0 || metric.InProgress != 0 || metric.Completed != 0 {
			t.Errorf("empty queue metrics = %+v", metric)
		}
	}
}
