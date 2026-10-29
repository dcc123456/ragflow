//go:build ee

package admin

import (
	"strconv"

	"ragflow/internal/common"
	"ragflow/internal/service/dataset"

	"github.com/gin-gonic/gin"
)

// GetIngestionLog returns the file and run details for the log dialog.
func (h *Handler) GetIngestionLog(c *gin.Context) {
	result, code, err := dataset.GetIngestionLogForAdmin(c.Request.Context(), c.Param("log_id"))
	if err != nil {
		common.ErrorWithCode(c, code, err.Error())
		return
	}
	common.SuccessWithData(c, result, "success")
}

// GetIngestionMessages returns a run's keyset-paginated events.
func (h *Handler) GetIngestionMessages(c *gin.Context) {
	limit := 0
	if rawLimit := c.Query("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed <= 0 {
			common.ResponseWithCodeData(c, common.CodeArgumentError, nil, "limit must be a positive integer")
			return
		}
		limit = parsed
	}
	afterID, ok := adminIngestionEventCursor(c, "after_id")
	if !ok {
		return
	}
	beforeID, ok := adminIngestionEventCursor(c, "before_id")
	if !ok {
		return
	}
	result, code, err := dataset.GetIngestionMessagesForAdmin(c.Request.Context(), c.Param("log_id"), limit, afterID, beforeID)
	if err != nil {
		common.ErrorWithCode(c, code, err.Error())
		return
	}
	common.SuccessWithData(c, result, "success")
}

func adminIngestionEventCursor(c *gin.Context, name string) (*int, bool) {
	raw := c.Query(name)
	if raw == "" {
		return nil, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		common.ResponseWithCodeData(c, common.CodeArgumentError, nil, name+" must be a positive integer")
		return nil, false
	}
	return &value, true
}
