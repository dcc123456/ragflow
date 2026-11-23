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
	registry    *prometheus.Registry
	retrieval   map[string]retrievalMetrics
	apiRequests *prometheus.CounterVec
	apiFailures *prometheus.CounterVec
	apiLatency  *prometheus.HistogramVec
	active      prometheus.Gauge
	pending     *prometheus.GaugeVec
	inProgress  *prometheus.GaugeVec
	completed   *prometheus.GaugeVec
}

var defaultObserver = newObserver(metricHostname())

func metricHostname() string {
	if hostname, ok := os.LookupEnv("HOSTNAME"); ok {
		return hostname
	}
	return "ragflow"
}

func newObserver(hostname string) *observer {
	labels := prometheus.Labels{"hostname": hostname}
	// Match prometheus_client's default histogram buckets, including 10s.
	buckets := []float64{.005, .01, .025, .05, .075, .1, .25, .5, .75, 1, 2.5, 5, 7.5, 10}
	o := &observer{
		registry: prometheus.NewRegistry(),
		retrieval: map[string]retrievalMetrics{
			"retrieval": {
				requests: prometheus.NewCounter(prometheus.CounterOpts{
					Name: "retrieval_requests_total", Help: "Total retrieval requests", ConstLabels: labels,
				}),
				failures: prometheus.NewCounter(prometheus.CounterOpts{
					Name: "retrieval_failures_total", Help: "Total retrieval failures", ConstLabels: labels,
				}),
				latency: prometheus.NewHistogram(prometheus.HistogramOpts{
					Name: "retrieval_latency_seconds", Help: "retrieval latency in seconds", ConstLabels: labels,
					Buckets: buckets,
				}),
			},
			"api_retrieval": {
				requests: prometheus.NewCounter(prometheus.CounterOpts{
					Name: "api_retrieval_requests_total", Help: "Total retrieval requests", ConstLabels: labels,
				}),
				failures: prometheus.NewCounter(prometheus.CounterOpts{
					Name: "api_retrieval_failures_total", Help: "Total retrieval failures", ConstLabels: labels,
				}),
				latency: prometheus.NewHistogram(prometheus.HistogramOpts{
					Name: "api_retrieval_latency_seconds", Help: "retrieval latency in seconds", ConstLabels: labels,
					Buckets: buckets,
				}),
			},
			"retrieval_embedding": {
				requests: prometheus.NewCounter(prometheus.CounterOpts{
					Name: "retrieval_embedding_requests_total", Help: "Total retrieval embedding requests", ConstLabels: labels,
				}),
				failures: prometheus.NewCounter(prometheus.CounterOpts{
					Name: "retrieval_embedding_failures_total", Help: "Total retrieval embedding failures", ConstLabels: labels,
				}),
				latency: prometheus.NewHistogram(prometheus.HistogramOpts{
					Name: "retrieval_embedding_latency_seconds", Help: "retrieval embedding latency in seconds", ConstLabels: labels,
					Buckets: buckets,
				}),
			},
			"retrieval_rerank": {
				requests: prometheus.NewCounter(prometheus.CounterOpts{
					Name: "retrieval_rerank_requests_total", Help: "Total retrieval rerank requests", ConstLabels: labels,
				}),
				failures: prometheus.NewCounter(prometheus.CounterOpts{
					Name: "retrieval_rerank_failures_total", Help: "Total retrieval rerank failures", ConstLabels: labels,
				}),
				latency: prometheus.NewHistogram(prometheus.HistogramOpts{
					Name: "retrieval_rerank_latency_seconds", Help: "retrieval rerank latency in seconds", ConstLabels: labels,
					Buckets: buckets,
				}),
			},
		},
		apiRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "apis_requests_total", Help: "Total API requests", ConstLabels: labels,
		}, []string{"api_path"}),
		apiFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "apis_failures_total", Help: "Total API failures", ConstLabels: labels,
		}, []string{"api_path"}),
		apiLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "apis_latency_seconds", Help: "API latency in seconds", ConstLabels: labels,
			Buckets: buckets,
		}, []string{"api_path"}),
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
	o.registry.MustRegister(o.apiRequests, o.apiFailures, o.apiLatency)
	o.registry.MustRegister(o.retrieval["retrieval"].requests, o.retrieval["retrieval"].failures, o.retrieval["retrieval"].latency)
	o.registry.MustRegister(o.retrieval["api_retrieval"].requests, o.retrieval["api_retrieval"].failures, o.retrieval["api_retrieval"].latency)
	o.registry.MustRegister(o.retrieval["retrieval_embedding"].requests, o.retrieval["retrieval_embedding"].failures, o.retrieval["retrieval_embedding"].latency)
	o.registry.MustRegister(o.retrieval["retrieval_rerank"].requests, o.retrieval["retrieval_rerank"].failures, o.retrieval["retrieval_rerank"].latency)
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
	return beginRequest(m.requests, m.failures, m.latency)
}

// BeginAPI records an API request labeled by its method and full route path.
// Defer its returned function with the request's error to record every exit path.
func BeginAPI(apiPath string) func(*error) {
	return defaultObserver.beginAPI(apiPath)
}

func (o *observer) beginAPI(apiPath string) func(*error) {
	return beginRequest(o.apiRequests.WithLabelValues(apiPath), o.apiFailures.WithLabelValues(apiPath), o.apiLatency.WithLabelValues(apiPath))
}

func beginRequest(requests, failures prometheus.Counter, latency prometheus.Observer) func(*error) {
	requests.Inc()
	start := time.Now()
	return func(err *error) {
		p := recover()
		if p != nil || (err != nil && *err != nil) {
			failures.Inc()
		}
		latency.Observe(time.Since(start).Seconds())
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
