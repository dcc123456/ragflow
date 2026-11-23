//go:build ee

package observability

import (
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestRetrievalMetrics(t *testing.T) {
	o := newObserver("test-host")
	for _, stage := range []string{"retrieval", "api_retrieval", "retrieval_embedding", "retrieval_rerank"} {
		t.Run(stage, func(t *testing.T) {
			func() (err error) {
				defer o.beginRetrieval(stage)(&err)
				return nil
			}()
			failure := errors.New("operation failed")
			if err := func() (err error) {
				defer o.beginRetrieval(stage)(&err)
				return failure
			}(); err != failure {
				t.Fatalf("error changed: %v", err)
			}
			func() {
				defer func() {
					if p := recover(); p != failure {
						t.Errorf("panic changed: %v", p)
					}
				}()
				var err error
				defer o.beginRetrieval(stage)(&err)
				panic(failure)
			}()
			families, err := o.registry.Gather()
			if err != nil {
				t.Fatal(err)
			}
			for _, family := range families {
				metric := family.Metric[0]
				switch family.GetName() {
				case stage + "_requests_total":
					if metric.GetCounter().GetValue() != 3 {
						t.Errorf("requests = %v, want 3", metric.GetCounter().GetValue())
					}
					if len(metric.Label) != 1 || metric.Label[0].GetName() != "hostname" || metric.Label[0].GetValue() != "test-host" {
						t.Errorf("unexpected labels: %v", metric.Label)
					}
				case stage + "_failures_total":
					if metric.GetCounter().GetValue() != 2 {
						t.Errorf("failures = %v, want 2", metric.GetCounter().GetValue())
					}
				case stage + "_latency_seconds":
					histogram := metric.GetHistogram()
					if histogram.GetSampleCount() != 3 || histogram.GetSampleSum() < 0 {
						t.Errorf("unexpected histogram: %v", histogram)
					}
					if len(histogram.Bucket) != 14 || histogram.Bucket[13].GetUpperBound() != 10 {
						t.Errorf("histogram buckets differ from Python defaults: %v", histogram.Bucket)
					}
				}
			}
		})
	}
}

func TestMetricsConcurrentRequests(t *testing.T) {
	o := newObserver("concurrent-host")
	var wg sync.WaitGroup
	for range 64 {
		wg.Go(func() {
			var err error
			defer o.beginRetrieval("retrieval")(&err)
		})
	}
	wg.Wait()
	families, err := o.registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() == "retrieval_requests_total" && family.Metric[0].GetCounter().GetValue() != 64 {
			t.Fatalf("requests = %v, want 64", family.Metric[0].GetCounter().GetValue())
		}
	}
}

func TestAPIMetrics(t *testing.T) {
	o := newObserver("api-host")
	for _, path := range []string{"GET /api/v1/datasets", "POST /api/v1/datasets"} {
		func() {
			var err error
			defer o.beginAPI(path)(&err)
		}()
		func() {
			err := errors.New("request failed")
			defer o.beginAPI(path)(&err)
		}()
		func() {
			failure := errors.New("request panicked")
			defer func() {
				if got := recover(); got != failure {
					t.Errorf("panic = %v, want %v", got, failure)
				}
			}()
			var err error
			defer o.beginAPI(path)(&err)
			panic(failure)
		}()
	}
	var wg sync.WaitGroup
	for range 64 {
		wg.Go(func() {
			var err error
			defer o.beginAPI("GET /api/v1/datasets")(&err)
		})
	}
	wg.Wait()
	families, err := o.registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, family := range families {
		if !strings.HasPrefix(family.GetName(), "apis_") {
			continue
		}
		seen++
		if len(family.Metric) != 2 {
			t.Fatalf("%s has %d series, want 2", family.GetName(), len(family.Metric))
		}
		for _, metric := range family.Metric {
			if len(metric.Label) != 2 || metric.Label[0].GetName() != "api_path" || metric.Label[1].GetName() != "hostname" || metric.Label[1].GetValue() != "api-host" {
				t.Fatalf("unexpected labels: %v", metric.Label)
			}
			var count uint64 = 3
			switch metric.Label[0].GetValue() {
			case "GET /api/v1/datasets":
				count += 64
			case "POST /api/v1/datasets":
			default:
				t.Fatalf("unexpected API path: %v", metric.Label)
			}
			switch family.GetName() {
			case "apis_requests_total":
				if got := metric.GetCounter().GetValue(); got != float64(count) {
					t.Errorf("requests = %v, want %d", got, count)
				}
			case "apis_failures_total":
				if got := metric.GetCounter().GetValue(); got != 2 {
					t.Errorf("failures = %v, want 2", got)
				}
			case "apis_latency_seconds":
				histogram := metric.GetHistogram()
				if histogram.GetSampleCount() != count || histogram.GetSampleSum() < 0 || len(histogram.Bucket) != 14 || histogram.Bucket[13].GetUpperBound() != 10 {
					t.Errorf("unexpected histogram: %v", histogram)
				}
			}
		}
	}
	if seen != 3 {
		t.Errorf("API metric families = %d, want 3", seen)
	}
}

func TestMetricsExpositionAndGauges(t *testing.T) {
	previous := defaultObserver
	defaultObserver = newObserver("exposition-host")
	t.Cleanup(func() { defaultObserver = previous })
	SetActiveUsersCount(17)
	SetQueueMetrics("tasks", 10, 3, 21)
	SetQueueMetrics("tasks", 4, 2, 22)
	var err error
	BeginRetrieval("retrieval")(&err)
	BeginAPI("POST /api/v1/retrieval")(&err)
	recorder := httptest.NewRecorder()
	MetricsHandler().ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	if recorder.Code != 200 || !strings.HasPrefix(recorder.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("invalid metrics response: %d %v", recorder.Code, recorder.Header())
	}
	for _, sample := range []string{
		"# TYPE active_users_total gauge\nactive_users_total 17\n",
		"queue_tasks_pending{queue=\"tasks\"} 4\n",
		"queue_tasks_in_progress{queue=\"tasks\"} 2\n",
		"# TYPE queue_tasks_completed_total gauge",
		"queue_tasks_completed_total{queue=\"tasks\"} 22\n",
		"retrieval_requests_total{hostname=\"exposition-host\"} 1\n",
		"apis_requests_total{api_path=\"POST /api/v1/retrieval\",hostname=\"exposition-host\"} 1\n",
		"apis_failures_total{api_path=\"POST /api/v1/retrieval\",hostname=\"exposition-host\"} 0\n",
		"apis_latency_seconds_count{api_path=\"POST /api/v1/retrieval\",hostname=\"exposition-host\"} 1\n",
	} {
		if !strings.Contains(recorder.Body.String(), sample) {
			t.Errorf("missing metric %q", sample)
		}
	}
}

func TestMetricHostname(t *testing.T) {
	t.Setenv("HOSTNAME", "custom-host")
	if metricHostname() != "custom-host" {
		t.Fatal("HOSTNAME was ignored")
	}
	if err := os.Unsetenv("HOSTNAME"); err != nil {
		t.Fatal(err)
	}
	if metricHostname() != "ragflow" {
		t.Fatal("missing HOSTNAME did not default to ragflow")
	}
}
