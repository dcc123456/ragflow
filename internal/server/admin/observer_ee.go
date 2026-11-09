//go:build ee

package admin

import (
	"context"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/engine"
	"ragflow/internal/engine/kvrocks"
	"ragflow/internal/engine/nats"
	"ragflow/internal/observability"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func startMetrics(ctx context.Context) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			var client *redis.Client
			if cache := kvrocks.Get(); cache != nil {
				client = cache.GetClient()
			}
			queue, _ := engine.GetMessageQueueEngine().(*nats.NatsEngine)
			updateMetrics(ctx, client, queue)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func updateMetrics(ctx context.Context, client *redis.Client, queue *nats.NatsEngine) {
	var fields []zap.Field
	defer func() { common.Info("Admin metrics", fields...) }()
	activeCtx, activeCancel := context.WithTimeout(ctx, 15*time.Second)
	if count, err := observability.ActiveUsersCount(activeCtx, client, observability.ActiveUsersWindow); err != nil {
		common.Warn("Unable to update active users", zap.Error(err))
	} else {
		observability.SetActiveUsersCount(count)
		fields = append(fields, zap.Int64("active_users", count))
	}
	activeCancel()
	if queue == nil {
		common.Warn("Unable to update queue metrics: NATS is unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	metrics, err := queue.QueueMetrics(ctx)
	if err != nil {
		common.Warn("Unable to update queue metrics", zap.Error(err))
		return
	}
	for _, metric := range metrics {
		observability.SetQueueMetrics(metric.Name, metric.Pending, metric.InProgress, metric.Completed)
	}
	fields = append(fields, zap.Any("queues", metrics))
}
