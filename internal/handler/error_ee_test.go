//go:build ee

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ragflow/internal/common"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// installNoOpLogger swaps in a logger so HandleNoRoute's Warn line has a sink.
// The production logger is set up by the server bootstrap, which unit tests
// never run.
func installNoOpLogger(t *testing.T) {
	t.Helper()
	previousLogger := common.Logger
	common.Logger = zap.NewNop()
	t.Cleanup(func() { common.Logger = previousLogger })
}

// TestHandleNoRouteOAuthLoginEmptyChannelEnterprise pins the enterprise half of
// HandleNoRoute: /api/v1/auth/login/:channel does not bind an empty :channel,
// so the request reaches NoRoute and has to answer with the MethodNotAllowed
// body Python did.
func TestHandleNoRouteOAuthLoginEmptyChannelEnterprise(t *testing.T) {
	installNoOpLogger(t)
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/login/", nil)

	HandleNoRoute(context)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d, want %d", recorder.Code, http.StatusOK)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["code"] != float64(common.CodeExceptionError) || body["message"] != "<MethodNotAllowed '405: Method Not Allowed'>" {
		t.Fatalf("body=%v", body)
	}
}

// TestHandleNoRouteIgnoresNonOAuthPaths keeps the empty-channel parity branch
// from silently widening: a sibling path must not pick up its response body.
func TestHandleNoRouteIgnoresNonOAuthPaths(t *testing.T) {
	installNoOpLogger(t)
	gin.SetMode(gin.TestMode)
	cases := map[string]string{
		"missing channel without trailing slash": "/api/v1/auth/login",
		"named channel":                          "/api/v1/auth/login/github",
		"callback path":                          "/api/v1/auth/oauth/github/callback",
		"other method":                           "/api/v1/auth/login/",
	}
	for name, path := range cases {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		method := http.MethodGet
		if name == "other method" {
			method = http.MethodPost
		}
		context.Request = httptest.NewRequest(method, path, nil)

		HandleNoRoute(context)

		if strings.Contains(recorder.Body.String(), "MethodNotAllowed") {
			t.Fatalf("%s: %s %s borrowed the OAuth login NoRoute body: %s", name, method, path, recorder.Body.String())
		}
	}
}
