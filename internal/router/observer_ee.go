//go:build ee

package router

import (
	"errors"
	"net/http"

	"ragflow/internal/common"
	"ragflow/internal/engine/kvrocks"
	"ragflow/internal/handler"
	"ragflow/internal/observability"

	"github.com/gin-gonic/gin"
)

func apiMetrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.FullPath()
		if path == "" || path == "/metrics" {
			c.Next()
			return
		}
		var err error
		defer observability.BeginAPI(c.Request.Method + " " + path)(&err)
		defer func() {
			if c.Writer.Status() >= http.StatusInternalServerError {
				err = errors.New("API HTTP request failed")
			}
		}()
		c.Next()
	}
}

func apiRetrievalMetrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}
		switch c.FullPath() {
		case "/api/v1/retrieval", "/api/v1/datasets/search", "/api/v1/datasets/:dataset_id/search":
		default:
			c.Next()
			return
		}
		var err error
		defer observability.BeginRetrieval("api_retrieval")(&err)
		defer func() {
			if c.Writer.Status() >= http.StatusInternalServerError {
				err = errors.New("retrieval HTTP request failed")
			}
		}()
		c.Next()
	}
}

func userHeartbeat(c *gin.Context) {
	user, code, message := handler.GetUser(c)
	if code != common.CodeSuccess {
		common.RespondError(c, code, message)
		return
	}
	client := kvrocks.Get()
	if client == nil {
		common.RespondError(c, common.CodeServerError, "Cache is unavailable")
		return
	}
	if err := observability.MarkUserActive(c.Request.Context(), client.GetClient(), user.ID); err != nil {
		common.ErrorCtx(c.Request.Context(), "Failed to mark user active", err)
		common.RespondError(c, common.CodeServerError, "Failed to update heartbeat")
		return
	}
	common.RespondSuccess(c, nil, "")
}
