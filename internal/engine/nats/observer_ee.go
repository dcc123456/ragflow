//go:build ee

package nats

import (
	"context"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
)

type QueueMetrics struct {
	Name       string
	Pending    uint64
	InProgress uint64
	Completed  uint64
}

func (n *NatsEngine) QueueMetrics(ctx context.Context) ([]QueueMetrics, error) {
	if n.jetStream == nil {
		return nil, errors.New("NATS jetstream is not initialized")
	}
	var result []QueueMetrics
	// The ingestion task queue is now three physically isolated streams (one
	// per tier). Report each lane so the metrics endpoint exposes per-tier
	// depth instead of a single monolithic figure.
	var queues []struct{ stream, consumer string }
	for _, tier := range tierOrder {
		spec := tierStreams[tier]
		queues = append(queues, struct{ stream, consumer string }{spec.streamName, spec.consumerName})
	}
	queues = append(queues,
		struct{ stream, consumer string }{knowledgeCompileStreamName, "KNOWLEDGE_COMPILE_CONSUMER"},
		struct{ stream, consumer string }{syncerStreamName, syncerConsumerName},
	)
	for _, queue := range queues {
		stream, err := n.jetStream.Stream(ctx, queue.stream)
		if errors.Is(err, jetstream.ErrStreamNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("queue metrics for %s: %w", queue.stream, err)
		}
		metric := QueueMetrics{Name: queue.stream, Pending: stream.CachedInfo().State.Msgs}
		consumer, err := stream.Consumer(ctx, queue.consumer)
		var info *jetstream.ConsumerInfo
		if errors.Is(err, jetstream.ErrNotPullConsumer) {
			var push jetstream.PushConsumer
			push, err = stream.PushConsumer(ctx, queue.consumer)
			if err == nil {
				info = push.CachedInfo()
			}
		} else if err == nil {
			info = consumer.CachedInfo()
		}
		if errors.Is(err, jetstream.ErrConsumerNotFound) {
			result = append(result, metric)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("queue consumer metrics for %s: %w", queue.stream, err)
		}
		metric.Pending = info.NumPending
		metric.InProgress = uint64(info.NumAckPending)
		// JetStream exposes the contiguous acknowledgement floor. It advances
		// after gaps are acknowledged and resets when the consumer is recreated.
		metric.Completed = info.AckFloor.Consumer
		result = append(result, metric)
	}
	return result, nil
}
