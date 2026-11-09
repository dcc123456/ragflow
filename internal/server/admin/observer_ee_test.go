//go:build ee

package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	adminapi "ragflow/internal/admin"
	"ragflow/internal/common"
	"ragflow/internal/engine/nats"
	"ragflow/internal/ingestion/testutil"
	"ragflow/internal/observability"
	"ragflow/internal/server"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestMetricsWorkerStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	stop := startMetrics(ctx)
	cancel()
	stop()
	stop()
}

func assertAdminMetricSamples(t *testing.T, samples ...string) {
	t.Helper()
	if err := server.InitLocalVariables(); err != nil {
		t.Fatal(err)
	}
	server.SetServerName("admin-metrics-test")
	mode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(mode) })
	router := gin.New()
	adminapi.SetupEERouter(router)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("admin metrics HTTP status = %d", recorder.Code)
	}
	for _, sample := range samples {
		if !strings.Contains(recorder.Body.String(), sample+"\n") {
			t.Errorf("admin /metrics is missing %q", sample)
		}
	}
}

func TestAdminMetricsRefreshesRedisAndNats(t *testing.T) {
	store := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: store.Addr()})
	t.Cleanup(func() { client.Close() })
	queue := testutil.SetupNatsEngine(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := client.ZAdd(ctx, "active_users", redis.Z{Member: "expired", Score: float64(time.Now().Unix() - 600)}).Err(); err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"first", "second", "first"} {
		if err := observability.MarkUserActive(ctx, client, user); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := queue.PublishTask(common.TaskSubject, []byte(`{"task_id":"admin-metrics"}`)); err != nil {
			t.Fatal(err)
		}
	}
	if err := queue.InitConsumer(common.TaskSubject); err != nil {
		t.Fatal(err)
	}
	if messages, err := queue.PullMessages(ctx, 1); err != nil || len(messages) != 1 {
		t.Fatalf("pull one task: %v, %v", messages, err)
	}
	core, logs := observer.New(zap.InfoLevel)
	previous := common.Logger
	common.Logger = zap.New(core)
	t.Cleanup(func() { common.Logger = previous })
	updateMetrics(ctx, client, queue)
	assertAdminMetricSamples(t,
		"active_users_total 2",
		`queue_tasks_pending{queue="RAGFLOW_TASKS"} 1`,
		`queue_tasks_in_progress{queue="RAGFLOW_TASKS"} 1`,
		`queue_tasks_completed_total{queue="RAGFLOW_TASKS"} 0`,
	)
	entries := logs.FilterMessage("Admin metrics").All()
	if len(entries) != 1 || entries[0].ContextMap()["active_users"] != int64(2) || entries[0].ContextMap()["queues"] == nil {
		t.Fatalf("missing metrics summary: %+v", entries)
	}
}

func TestAdminMetricsKeepsUpdatingNatsWhenRedisFails(t *testing.T) {
	store := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: store.Addr()})
	t.Cleanup(func() { client.Close() })
	if err := client.Set(t.Context(), "active_users", "wrong-type", 0).Err(); err != nil {
		t.Fatal(err)
	}
	queue := testutil.SetupNatsEngine(t)
	if err := queue.PublishTask(common.TaskSubject, []byte(`{"task_id":"admin-metrics"}`)); err != nil {
		t.Fatal(err)
	}
	observability.SetActiveUsersCount(7)
	observability.SetQueueMetrics("RAGFLOW_TASKS", 99, 99, 99)
	updateMetrics(t.Context(), client, queue)
	assertAdminMetricSamples(t,
		"active_users_total 7",
		`queue_tasks_pending{queue="RAGFLOW_TASKS"} 1`,
		`queue_tasks_in_progress{queue="RAGFLOW_TASKS"} 0`,
		`queue_tasks_completed_total{queue="RAGFLOW_TASKS"} 0`,
	)
}

func TestAdminMetricsKeepsUpdatingRedisWhenNatsFails(t *testing.T) {
	store := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: store.Addr()})
	t.Cleanup(func() { client.Close() })
	if err := observability.MarkUserActive(t.Context(), client, "user"); err != nil {
		t.Fatal(err)
	}
	observability.SetActiveUsersCount(99)
	observability.SetQueueMetrics("RAGFLOW_TASKS", 19, 2, 8)
	updateMetrics(t.Context(), client, nats.NewNatsEngine("", 0))
	assertAdminMetricSamples(t,
		"active_users_total 1",
		`queue_tasks_pending{queue="RAGFLOW_TASKS"} 19`,
		`queue_tasks_in_progress{queue="RAGFLOW_TASKS"} 2`,
		`queue_tasks_completed_total{queue="RAGFLOW_TASKS"} 8`,
	)
}
