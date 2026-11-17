//go:build ee

package router

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"ragflow/internal/handler"
	"ragflow/internal/service"
	"testing"
)

func TestSelfDeletionRouteRequiresAuthentication(t *testing.T) {
	engine := gin.New()
	authorized := engine.Group("", func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) })
	r := &Router{userHandler: handler.NewUserHandler(service.NewUserService(), service.NewUserTenantService())}
	RegisterEEAuthRouter(authorized, r)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/api/v1/users/me", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", response.Code)
	}
}
