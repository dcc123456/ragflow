//
//  Copyright (c) 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//
//  This file is part of the RAGFlow Enterprise Edition and is licensed,
//  not sold, under the RAGFlow Enterprise Commercial License Agreement.
//  Unauthorized copying, modification, distribution, or disclosure of this
//  file, via any medium, is strictly prohibited.
//
//  Portions of this file may incorporate code from RAGFlow Community Edition,
//  which is licensed under the Apache License, Version 2.0.
//
//  For commercial licensing inquiries: sales@infiniflow.ai
//

//go:build ee

package admin

import (
	"errors"
	"net/http"
	"strconv"

	"ragflow/internal/common"
	"ragflow/internal/entity"

	"github.com/gin-gonic/gin"
)

type whitelistList struct {
	Entries []entity.WhiteList `json:"white_list"`
	Total   int                `json:"total"`
}

type whitelistInsertResponse struct {
	Success bool  `json:"success"`
	Added   int64 `json:"added"`
	Skipped int64 `json:"skipped"`
}

type whitelistUpdateResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type whitelistDeleteResponse struct {
	Success bool  `json:"success"`
	Deleted int64 `json:"deleted"`
}

type whitelistEmailRequest struct {
	Email string `json:"email" binding:"required"`
}

func writeWhitelistError(c *gin.Context, err error) {
	var coded *common.CodedError
	if errors.As(err, &coded) {
		common.Respond(c, int(coded.Code), coded.Code, nil, coded.Message)
		return
	}
	common.Error("Whitelist operation failed", err)
	common.Respond(c, http.StatusInternalServerError, common.CodeServerError, nil, "Whitelist operation failed")
}

func bindWhitelistJSON(c *gin.Context, request interface{}) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
	if err := c.ShouldBindJSON(request); err != nil {
		common.Respond(c, http.StatusBadRequest, common.CodeBadRequest, nil, "Invalid whitelist request")
		return false
	}
	return true
}

func (h *Handler) ShowWhiteList(c *gin.Context) {
	entries, err := h.service.ShowWhiteList(c.Request.Context())
	if err != nil {
		writeWhitelistError(c, err)
		return
	}
	common.RespondSuccess(c, whitelistList{Entries: entries, Total: len(entries)}, "")
}

func (h *Handler) AddWhiteList(c *gin.Context) {
	var request whitelistEmailRequest
	if !bindWhitelistJSON(c, &request) {
		return
	}
	result, err := h.service.AddWhiteList(c.Request.Context(), request.Email)
	if err != nil {
		writeWhitelistError(c, err)
		return
	}
	common.RespondSuccess(c, whitelistInsertResponse{Success: true, Added: result.added, Skipped: result.skipped}, "")
}

func (h *Handler) UpdateWhiteList(c *gin.Context) {
	var request whitelistEmailRequest
	if !bindWhitelistJSON(c, &request) {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		writeWhitelistError(c, common.NewCodedError(common.CodeBadRequest, "Invalid whitelist ID"))
		return
	}
	message, err := h.service.UpdateWhiteList(c.Request.Context(), id, request.Email)
	if err != nil {
		writeWhitelistError(c, err)
		return
	}
	common.RespondSuccess(c, whitelistUpdateResponse{Success: true, Message: message}, "")
}

func (h *Handler) DeleteWhiteList(c *gin.Context) {
	deleted, err := h.service.DeleteWhiteList(c.Request.Context(), c.Param("email"))
	if err != nil {
		writeWhitelistError(c, err)
		return
	}
	common.RespondSuccess(c, whitelistDeleteResponse{Success: true, Deleted: deleted}, "")
}

func (h *Handler) BatchDeleteWhiteList(c *gin.Context) {
	var request struct {
		IDs []int64 `json:"ids" binding:"required"`
	}
	if !bindWhitelistJSON(c, &request) {
		return
	}
	deleted, err := h.service.BatchDeleteWhiteList(c.Request.Context(), request.IDs)
	if err != nil {
		writeWhitelistError(c, err)
		return
	}
	common.RespondSuccess(c, whitelistDeleteResponse{Success: true, Deleted: deleted}, "")
}

func (h *Handler) BatchAddWhiteList(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxWhitelistFileBytes+(64<<10))
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		var limitError *http.MaxBytesError
		if errors.As(err, &limitError) {
			common.Respond(c, http.StatusRequestEntityTooLarge, common.CodeBadRequest, nil, "Whitelist file exceeds 5 MiB")
		} else {
			common.Respond(c, http.StatusBadRequest, common.CodeBadRequest, nil, "Invalid multipart upload")
		}
		return
	}
	form := c.Request.MultipartForm
	defer form.RemoveAll()
	count := 0
	for _, files := range form.File {
		count += len(files)
	}
	if count != 1 || len(form.File["file"]) != 1 {
		common.Respond(c, http.StatusBadRequest, common.CodeBadRequest, nil, "Provide exactly one file in the file field")
		return
	}
	file := form.File["file"][0]
	reader, err := file.Open()
	if err != nil {
		writeWhitelistError(c, err)
		return
	}
	defer reader.Close()
	result, err := h.service.BatchAddWhiteList(c.Request.Context(), file.Filename, reader)
	if err != nil {
		writeWhitelistError(c, err)
		return
	}
	common.RespondSuccess(c, whitelistInsertResponse{Success: true, Added: result.added, Skipped: result.skipped}, "")
}
