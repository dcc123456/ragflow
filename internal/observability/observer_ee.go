//go:build ee

package observability

import (
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type retrievalMetrics struct {
	requests prometheus.Counter
	failures prometheus.Counter
	latency  prometheus.Histogram
}

type observer struct {
	registry   *prometheus.Registry
	retrieval  map[string]retrievalMetrics
	active     prometheus.Gauge
	pending    *prometheus.GaugeVec
	inProgress *prometheus.GaugeVec
	completed  *prometheus.GaugeVec
}

var defaultObserver = newObserver(metricHostname())

func metricHostname() string {
	if hostname, ok := os.LookupEnv("HOSTNAME"); ok {
		return hostname
	}
	return "ragflow"
}

func newObserver(hostname string) *observer {
	o := &observer{
		registry:  prometheus.NewRegistry(),
		retrieval: make(map[string]retrievalMetrics, 4),
		active: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "active_users_total", Help: "Number of active users in last 5 minutes",
		}),
		pending: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "queue_tasks_pending", Help: "Tasks pending",
		}, []string{"queue"}),
		inProgress: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "queue_tasks_in_progress", Help: "Tasks in progress",
		}, []string{"queue"}),
		completed: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "queue_tasks_completed_total", Help: "Total completed tasks",
		}, []string{"queue"}),
	}
	o.registry.MustRegister(o.active, o.pending, o.inProgress, o.completed)
	for _, stage := range []struct{ name, description string }{
		{"retrieval", "retrieval"},
		{"api_retrieval", "retrieval"},
		{"retrieval_embedding", "retrieval embedding"},
		{"retrieval_rerank", "retrieval rerank"},
	} {
		labels := prometheus.Labels{"hostname": hostname}
		m := retrievalMetrics{
			requests: prometheus.NewCounter(prometheus.CounterOpts{
				Name: stage.name + "_requests_total", Help: "Total " + stage.description + " requests", ConstLabels: labels,
			}),
			failures: prometheus.NewCounter(prometheus.CounterOpts{
				Name: stage.name + "_failures_total", Help: "Total " + stage.description + " failures", ConstLabels: labels,
			}),
			latency: prometheus.NewHistogram(prometheus.HistogramOpts{
				Name: stage.name + "_latency_seconds", Help: stage.description + " latency in seconds", ConstLabels: labels,
				// Match prometheus_client's default histogram buckets, including 10s.
				Buckets: []float64{.005, .01, .025, .05, .075, .1, .25, .5, .75, 1, 2.5, 5, 7.5, 10},
			}),
		}
		o.registry.MustRegister(m.requests, m.failures, m.latency)
		o.retrieval[stage.name] = m
	}
	return o
}

// BeginRetrieval records one of the four fixed retrieval stages. Defer its
// returned function with the operation's named error to record every exit path.
func BeginRetrieval(stage string) func(*error) {
	return defaultObserver.beginRetrieval(stage)
}

func (o *observer) beginRetrieval(stage string) func(*error) {
	m, ok := o.retrieval[stage]
	if !ok {
		panic("unknown retrieval metric stage: " + stage)
	}
	m.requests.Inc()
	start := time.Now()
	return func(err *error) {
		p := recover()
		if p != nil || (err != nil && *err != nil) {
			m.failures.Inc()
		}
		m.latency.Observe(time.Since(start).Seconds())
		if p != nil {
			panic(p)
		}
	}
}

func MetricsHandler() http.Handler {
	return promhttp.HandlerFor(defaultObserver.registry, promhttp.HandlerOpts{})
}

func SetActiveUsersCount(count int64) {
	defaultObserver.active.Set(float64(count))
}

func SetQueueMetrics(name string, ready, inflight, ack uint64) {
	defaultObserver.pending.WithLabelValues(name).Set(float64(ready))
	defaultObserver.inProgress.WithLabelValues(name).Set(float64(inflight))
	defaultObserver.completed.WithLabelValues(name).Set(float64(ack))
}
