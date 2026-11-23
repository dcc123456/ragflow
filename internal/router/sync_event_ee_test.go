//go:build ee

package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSyncEventsRouteUsesAuthenticatedEnterpriseGroup(t *testing.T) {
	engine := gin.New()
	authorized := engine.Group("", func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) })
	RegisterEEAuthRouter(authorized, &Router{})
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/connectors/sync-logs/task/events", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}
