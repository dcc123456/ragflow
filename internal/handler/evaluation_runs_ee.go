// Copyright (c) 2026 InfiniFlow. All rights reserved.
//go:build ee

package handler

import (
	"errors"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/service"
	"strings"
)

func (h *EvaluationHandler) Runs(c *gin.Context) {
	if !evaluationContext(c) {
		return
	}
	p, n, ok := evaluationPage(c)
	if !ok {
		return
	}
	data, err := h.service.Runs(c.Request.Context(), c.Query("target_id"), c.Query("collection_id"), c.Query("keywords"), p, n)
	evaluationReply(c, data, err)
}
func (h *EvaluationHandler) CreateRun(c *gin.Context) {
	if !evaluationContext(c) {
		return
	}
	var input service.EvaluationRunInput
	if err := c.ShouldBindJSON(&input); err != nil {
		common.Respond(c, 400, common.CodeBadRequest, nil, "Invalid run configuration")
		return
	}
	data, err := h.service.CreateRun(c.Request.Context(), input)
	evaluationReply(c, data, err)
}
func (h *EvaluationHandler) Run(c *gin.Context) {
	if !evaluationContext(c) {
		return
	}
	data, err := h.service.Run(c.Request.Context(), c.Param("run_id"))
	evaluationReply(c, data, err)
}
func (h *EvaluationHandler) UpdateRun(c *gin.Context) {
	if !evaluationContext(c) {
		return
	}
	var patch entity.JSONMap
	if err := c.ShouldBindJSON(&patch); err != nil {
		common.Respond(c, 400, common.CodeBadRequest, nil, "Invalid run request")
		return
	}
	if status, ok := patch["status"]; ok {
		if status != "CANCEL" || len(patch) != 1 {
			common.Respond(c, 400, common.CodeBadRequest, nil, "Only status CANCEL is accepted")
			return
		}
		if _, err := h.service.Run(c.Request.Context(), c.Param("run_id")); err != nil {
			evaluationError(c, err)
			return
		}
		if err := h.service.CancelExecution(c.Request.Context(), c.Param("run_id")); err != nil {
			evaluationError(c, err)
			return
		}
		data, err := h.service.Run(c.Request.Context(), c.Param("run_id"))
		evaluationReply(c, data, err)
		return
	}
	data, err := h.service.UpdateRun(c.Request.Context(), c.Param("run_id"), patch)
	evaluationReply(c, data, err)
}
func (h *EvaluationHandler) DeleteRun(c *gin.Context) {
	if !evaluationContext(c) {
		return
	}
	evaluationReply(c, entity.JSONMap{"deleted": true}, h.service.DeleteRun(c.Request.Context(), c.Param("run_id")))
}
func (h *EvaluationHandler) Results(c *gin.Context) {
	if !evaluationContext(c) {
		return
	}
	p, n, ok := evaluationPage(c)
	if !ok {
		return
	}
	data, err := h.service.Results(c.Request.Context(), c.Param("run_id"), p, n)
	evaluationReply(c, data, err)
}
func (h *EvaluationHandler) ClearResult(c *gin.Context) {
	if !evaluationContext(c) {
		return
	}
	part := "result"
	if strings.HasSuffix(c.FullPath(), "/answer") {
		part = "answer"
	}
	if strings.Contains(c.FullPath(), "/metrics") {
		part = "metrics"
	}
	err := h.service.ClearResult(c.Request.Context(), c.Param("run_id"), c.Param("case_id"), part, c.Param("metric"))
	evaluationReply(c, entity.JSONMap{"cleared": true}, err)
}
func (h *EvaluationHandler) StartExecution(c *gin.Context) {
	if !evaluationContext(c) {
		return
	}
	var input struct {
		Mode      string   `json:"mode"`
		CaseIDs   []string `json:"case_ids"`
		MetricIDs []string `json:"metrics"`
	}
	if err := c.ShouldBindJSON(&input); err != nil && !errors.Is(err, io.EOF) {
		common.Respond(c, 400, common.CodeBadRequest, nil, "Invalid execution request")
		return
	}
	if input.Mode == "" {
		input.Mode = "generate_and_score"
	}
	if input.Mode != "generate_and_score" && input.Mode != "generate" && input.Mode != "score" {
		common.Respond(c, 400, common.CodeBadRequest, nil, "Invalid execution mode")
		return
	}
	if _, err := h.service.Run(c.Request.Context(), c.Param("run_id")); err != nil {
		evaluationError(c, err)
		return
	}
	data, err := h.service.StartExecution(c.Request.Context(), c.Param("run_id"), input.Mode, input.CaseIDs, input.MetricIDs)
	if err != nil {
		evaluationError(c, err)
		return
	}
	common.Respond(c, http.StatusAccepted, common.CodeSuccess, data, "Execution queued")
}
func (h *EvaluationHandler) ExportRun(c *gin.Context) {
	if !evaluationContext(c) {
		return
	}
	var ids []string
	if raw := c.Query("case_ids"); raw != "" {
		ids = strings.Split(raw, ",")
	}
	data, name, err := h.service.ExportRun(c.Request.Context(), c.Param("run_id"), ids)
	evaluationDownload(c, data, name, err)
}
