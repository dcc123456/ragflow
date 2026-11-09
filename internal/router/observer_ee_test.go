//go:build ee

package router

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/observability"

	"github.com/gin-gonic/gin"
)

func retrievalMetricValue(t *testing.T, name string) float64 {
	t.Helper()
	recorder := httptest.NewRecorder()
	observability.MetricsHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for line := range strings.SplitSeq(recorder.Body.String(), "\n") {
		if strings.HasPrefix(line, name+"{") {
			fields := strings.Fields(line)
			value, err := strconv.ParseFloat(fields[len(fields)-1], 64)
			if err != nil {
				t.Fatal(err)
			}
			return value
		}
	}
	t.Fatalf("metric %s was not exposed", name)
	return 0
}

func TestAPIRetrievalMetrics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery(), apiRetrievalMetrics())
	router.POST("/api/v1/retrieval", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.POST("/api/v1/datasets/search", func(c *gin.Context) { c.Status(http.StatusInternalServerError) })
	router.POST("/api/v1/datasets/:dataset_id/search", func(*gin.Context) { panic("retrieval failed") })
	router.GET("/api/v1/retrieval", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.POST("/unrelated", func(c *gin.Context) { c.Status(http.StatusOK) })
	requests := retrievalMetricValue(t, "api_retrieval_requests_total")
	failures := retrievalMetricValue(t, "api_retrieval_failures_total")
	for _, request := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/retrieval"},
		{http.MethodPost, "/api/v1/datasets/search"},
		{http.MethodPost, "/api/v1/datasets/one/search"},
		{http.MethodGet, "/api/v1/retrieval"},
		{http.MethodPost, "/unrelated"},
		{http.MethodPost, "/not-found"},
	} {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(request.method, request.path, nil))
	}
	if got := retrievalMetricValue(t, "api_retrieval_requests_total") - requests; got != 3 {
		t.Errorf("requests = %v, want 3", got)
	}
	if got := retrievalMetricValue(t, "api_retrieval_failures_total") - failures; got != 2 {
		t.Errorf("failures = %v, want 2", got)
	}
}

func TestAPIRetrievalMetricsHandledApplicationError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(apiRetrievalMetrics())
	router.POST("/api/v1/retrieval", func(c *gin.Context) {
		common.RespondError(c, common.CodeServerError, "handled failure")
	})
	failures := retrievalMetricValue(t, "api_retrieval_failures_total")
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/retrieval", nil))
	if got := retrievalMetricValue(t, "api_retrieval_failures_total") - failures; got != 0 {
		t.Errorf("handled application error counted as an uncaught failure: %v", got)
	}
}

func TestUserHeartbeatRequiresAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/heartbeat", userHeartbeat)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/heartbeat", nil))
	if !strings.Contains(recorder.Body.String(), `"code":401`) {
		t.Fatalf("unauthenticated heartbeat response: %s", recorder.Body.String())
	}
}
