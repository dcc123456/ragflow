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

func TestAPIMetrics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery(), requestBodyLimitMiddleware(), apiMetrics(), apiRetrievalMetrics())
	router.POST("/api/v1/retrieval", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.GET("/api/v1/datasets/:dataset_id", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.POST("/api/v1/datasets/:dataset_id", func(c *gin.Context) { c.Status(http.StatusInternalServerError) })
	router.DELETE("/api/v1/datasets/:dataset_id", func(*gin.Context) { panic("request failed") })
	router.PATCH("/api/v1/datasets/:dataset_id", func(c *gin.Context) {
		common.RespondError(c, common.CodeServerError, "handled failure")
	})
	router.PUT("/api/v1/datasets/:dataset_id", func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) })
	router.POST("/api/v1/upload", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.GET("/metrics", gin.WrapH(observability.MetricsHandler()))

	metricValue := func(t *testing.T, name, path string) float64 {
		t.Helper()
		recorder := httptest.NewRecorder()
		observability.MetricsHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		for line := range strings.SplitSeq(recorder.Body.String(), "\n") {
			if strings.HasPrefix(line, name+"{api_path="+strconv.Quote(path)+",") {
				fields := strings.Fields(line)
				value, err := strconv.ParseFloat(fields[len(fields)-1], 64)
				if err != nil {
					t.Fatal(err)
				}
				return value
			}
		}
		return 0
	}
	for _, request := range []struct {
		method, path, route string
		failures            float64
		bodySize            int64
	}{
		{http.MethodPost, "/api/v1/retrieval", "/api/v1/retrieval", 0, 0},
		{http.MethodGet, "/api/v1/datasets/one?limit=2", "/api/v1/datasets/:dataset_id", 0, 0},
		{http.MethodGet, "/api/v1/datasets/two", "/api/v1/datasets/:dataset_id", 0, 0},
		{http.MethodPost, "/api/v1/datasets/one", "/api/v1/datasets/:dataset_id", 1, 0},
		{http.MethodDelete, "/api/v1/datasets/one", "/api/v1/datasets/:dataset_id", 1, 0},
		{http.MethodPatch, "/api/v1/datasets/one", "/api/v1/datasets/:dataset_id", 0, 0},
		{http.MethodPut, "/api/v1/datasets/one", "/api/v1/datasets/:dataset_id", 0, 0},
		{http.MethodPost, "/api/v1/upload", "/api/v1/upload", 0, common.GetMaxJSONRequestBodyBytes() + 1},
		{http.MethodGet, "/metrics", "", 0, 0},
		{http.MethodGet, "/api/v1/not-found", "", 0, 0},
	} {
		t.Run(request.method+" "+request.path, func(t *testing.T) {
			path := request.method + " " + request.route
			if request.route == "" {
				path = request.method + " " + request.path
			}
			requests := metricValue(t, "apis_requests_total", path)
			failures := metricValue(t, "apis_failures_total", path)
			latency := metricValue(t, "apis_latency_seconds_count", path)
			req := httptest.NewRequest(request.method, request.path, nil)
			if request.bodySize > 0 {
				req = httptest.NewRequest(request.method, request.path, strings.NewReader("body"))
				req.ContentLength = request.bodySize
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			if request.bodySize > 0 && recorder.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("oversized request status = %d, want 413", recorder.Code)
			}
			want := float64(1)
			if request.route == "" || request.bodySize > 0 {
				want = 0
			}
			if got := metricValue(t, "apis_requests_total", path) - requests; got != want {
				t.Errorf("requests = %v, want %v", got, want)
			}
			if got := metricValue(t, "apis_failures_total", path) - failures; got != request.failures {
				t.Errorf("failures = %v, want %v", got, request.failures)
			}
			if got := metricValue(t, "apis_latency_seconds_count", path) - latency; got != want {
				t.Errorf("latency samples = %v, want %v", got, want)
			}
		})
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
