// Copyright (c) 2026 InfiniFlow. All rights reserved.
//go:build ee

package handler

import (
	"errors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
	"ragflow/internal/service"
	"strconv"
	"strings"
	"unicode"
)

type EvaluationHandler struct {
	service *service.EvaluationService
}

func NewEvaluationHandler() *EvaluationHandler {
	return &EvaluationHandler{service: service.NewEvaluationService()}
}
func evaluationContext(c *gin.Context) bool {
	if permission.UserIDFromContext(c.Request.Context()) == "" || permission.TenantIDFromContext(c.Request.Context()) == "" {
		common.Respond(c, http.StatusUnauthorized, common.CodeUnauthorized, nil, "Authentication required")
		return false
	}
	return true
}
func evaluationError(c *gin.Context, err error) {
	status, code := http.StatusInternalServerError, common.CodeServerError
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, permission.ErrResourceNotFound):
		status, code = http.StatusNotFound, common.CodeNotFound
	case errors.Is(err, permission.ErrUnauthenticated):
		status, code = http.StatusUnauthorized, common.CodeUnauthorized
	case errors.Is(err, permission.ErrPermissionDenied), errors.Is(err, permission.ErrMembershipNotFound), errors.Is(err, permission.ErrIdentityMismatch):
		status, code = http.StatusForbidden, common.CodeForbidden
	case errors.Is(err, service.ErrEvaluationActive):
		status, code = http.StatusConflict, common.CodeConflict
	case errors.Is(err, service.ErrEvaluationInvalid):
		status, code = http.StatusBadRequest, common.CodeBadRequest
	}
	message := err.Error()
	if status == http.StatusInternalServerError {
		common.Warn("Evaluation request failed: " + err.Error())
		message = "Evaluation operation failed"
	}
	common.Respond(c, status, code, nil, message)
}
func evaluationPage(c *gin.Context) (int, int, bool) {
	page, size := 1, 20
	var err error
	if c.Query("page") != "" {
		page, err = strconv.Atoi(c.Query("page"))
		if err != nil || page < 1 {
			common.Respond(c, 400, common.CodeBadRequest, nil, "page must be a positive integer")
			return 0, 0, false
		}
	}
	if c.Query("page_size") != "" {
		size, err = strconv.Atoi(c.Query("page_size"))
		if err != nil || size < 1 || size > 200 {
			common.Respond(c, 400, common.CodeBadRequest, nil, "page_size must be between 1 and 200")
			return 0, 0, false
		}
	}
	return page, size, true
}
func evaluationReply(c *gin.Context, data any, err error) {
	if err != nil {
		evaluationError(c, err)
		return
	}
	common.RespondSuccess(c, data, "")
}
func (h *EvaluationHandler) Collections(c *gin.Context) {
	if !evaluationContext(c) {
		return
	}
	p, n, ok := evaluationPage(c)
	if !ok {
		return
	}
	data, err := h.service.Collections(c.Request.Context(), c.Query("keywords"), p, n)
	evaluationReply(c, data, err)
}
func (h *EvaluationHandler) CreateCollection(c *gin.Context) {
	if !evaluationContext(c) {
		return
	}
	if strings.HasPrefix(c.ContentType(), "multipart/") {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<20)
		file, err := c.FormFile("file")
		if err != nil {
			common.Respond(c, 400, common.CodeBadRequest, nil, "An evaluation spreadsheet is required")
			return
		}
		if c.Request.MultipartForm != nil {
			defer c.Request.MultipartForm.RemoveAll()
		}
		stream, err := file.Open()
		if err != nil {
			evaluationError(c, err)
			return
		}
		defer stream.Close()
		body, err := io.ReadAll(io.LimitReader(stream, (32<<20)+1))
		if err != nil {
			evaluationError(c, err)
			return
		}
		if len(body) > 32<<20 {
			common.Respond(c, 413, common.CodeBadRequest, nil, "Spreadsheet exceeds 32 MiB")
			return
		}
		cases, failed, err := service.ReadEvaluationCases(file.Filename, body)
		if err != nil {
			evaluationError(c, err)
			return
		}
		name := c.PostForm("name")
		if name == "" {
			base := filepath.Base(file.Filename)
			name = strings.TrimSuffix(base, filepath.Ext(base))
		}
		value, err := h.service.CreateCollection(c.Request.Context(), name, c.PostForm("description"), c.PostForm("target_type"), cases)
		if err != nil {
			evaluationError(c, err)
			return
		}
		evaluationReply(c, entity.JSONMap{"collection_id": value.ID, "success_count": len(cases), "failure_count": failed, "total": len(cases) + failed}, nil)
		return
	}
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		TargetType  string `json:"target_type"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		common.Respond(c, 400, common.CodeBadRequest, nil, "Invalid collection request")
		return
	}
	data, err := h.service.CreateCollection(c.Request.Context(), input.Name, input.Description, input.TargetType, nil)
	evaluationReply(c, data, err)
}
func (h *EvaluationHandler) Collection(c *gin.Context) {
	if !evaluationContext(c) {
		return
	}
	data, err := h.service.Collection(c.Request.Context(), c.Param("collection_id"))
	evaluationReply(c, data, err)
}
func (h *EvaluationHandler) UpdateCollection(c *gin.Context) {
	if !evaluationContext(c) {
		return
	}
	var patch entity.JSONMap
	if err := c.ShouldBindJSON(&patch); err != nil {
		common.Respond(c, 400, common.CodeBadRequest, nil, "Invalid collection request")
		return
	}
	data, err := h.service.UpdateCollection(c.Request.Context(), c.Param("collection_id"), patch)
	evaluationReply(c, data, err)
}
func (h *EvaluationHandler) DeleteCollection(c *gin.Context) {
	if !evaluationContext(c) {
		return
	}
	ids := []string{c.Param("collection_id")}
	if ids[0] == "" {
		var input struct {
			IDs []string `json:"ids"`
		}
		if err := c.ShouldBindJSON(&input); err != nil {
			common.Respond(c, 400, common.CodeBadRequest, nil, "ids are required")
			return
		}
		ids = input.IDs
	}
	evaluationReply(c, entity.JSONMap{"deleted": true}, h.service.DeleteCollection(c.Request.Context(), ids))
}
func (h *EvaluationHandler) Cases(c *gin.Context) {
	if !evaluationContext(c) {
		return
	}
	p, n, ok := evaluationPage(c)
	if !ok {
		return
	}
	data, err := h.service.Cases(c.Request.Context(), c.Param("collection_id"), p, n)
	evaluationReply(c, data, err)
}
func (h *EvaluationHandler) CreateCase(c *gin.Context) {
	if !evaluationContext(c) {
		return
	}
	var input service.EvaluationCaseInput
	if err := c.ShouldBindJSON(&input); err != nil {
		common.Respond(c, 400, common.CodeBadRequest, nil, "Invalid case request")
		return
	}
	data, err := h.service.CreateCase(c.Request.Context(), c.Param("collection_id"), input)
	evaluationReply(c, data, err)
}
func (h *EvaluationHandler) UpdateCase(c *gin.Context) {
	if !evaluationContext(c) {
		return
	}
	var input service.EvaluationCaseInput
	if err := c.ShouldBindJSON(&input); err != nil {
		common.Respond(c, 400, common.CodeBadRequest, nil, "Invalid case request")
		return
	}
	data, err := h.service.UpdateCase(c.Request.Context(), c.Param("collection_id"), c.Param("case_id"), input)
	evaluationReply(c, data, err)
}
func (h *EvaluationHandler) DeleteCase(c *gin.Context) {
	if !evaluationContext(c) {
		return
	}
	evaluationReply(c, entity.JSONMap{"deleted": true}, h.service.DeleteCase(c.Request.Context(), c.Param("collection_id"), c.Param("case_id")))
}
func (h *EvaluationHandler) ImportCases(c *gin.Context) {
	if !evaluationContext(c) {
		return
	}
	var input struct {
		Cases []service.EvaluationCaseInput `json:"cases"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Cases == nil {
		common.Respond(c, 400, common.CodeBadRequest, nil, "cases must be an array")
		return
	}
	data, err := h.service.ImportCases(c.Request.Context(), c.Param("collection_id"), input.Cases)
	evaluationReply(c, data, err)
}
func evaluationDownload(c *gin.Context, data []byte, name string, err error) {
	if err != nil {
		evaluationError(c, err)
		return
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '/' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	runes := []rune(name)
	if len(runes) > 200 {
		name = string(runes[:195]) + ".xlsx"
	}
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	c.Data(200, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
}
func (h *EvaluationHandler) ExportCollection(c *gin.Context) {
	if !evaluationContext(c) {
		return
	}
	data, name, err := h.service.ExportCollection(c.Request.Context(), c.Param("collection_id"))
	evaluationDownload(c, data, name, err)
}
