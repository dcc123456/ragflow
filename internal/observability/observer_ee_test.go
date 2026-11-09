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

func TestMetricsExpositionAndGauges(t *testing.T) {
	previous := defaultObserver
	defaultObserver = newObserver("exposition-host")
	t.Cleanup(func() { defaultObserver = previous })
	SetActiveUsersCount(17)
	SetQueueMetrics("tasks", 10, 3, 21)
	SetQueueMetrics("tasks", 4, 2, 22)
	var err error
	BeginRetrieval("retrieval")(&err)
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
