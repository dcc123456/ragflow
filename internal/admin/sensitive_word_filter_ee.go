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
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"unicode/utf8"

	"ragflow/internal/common"
	"ragflow/internal/compliance"
	"ragflow/internal/dao"
	"ragflow/internal/storage"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (h *Handler) UploadSensitiveWords(c *gin.Context) {
	common.StartOperationLog(c, "upload_admin_sensitive_words")
	defer common.FinishOperationLog(c)

	// Allow multipart headers while bounding the complete request before parsing.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, compliance.MaxFileSize+1024*1024)
	form, err := c.MultipartForm()
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			common.Respond(c, http.StatusBadRequest, common.CodeArgumentError, nil, "Only a txt file up to 30 MiB is allowed.")
		} else {
			common.Respond(c, http.StatusBadRequest, common.CodeArgumentError, nil, "File is required.")
		}
		return
	}
	files := form.File["file"]
	if len(files) != 1 || files[0] == nil {
		common.Respond(c, http.StatusBadRequest, common.CodeArgumentError, nil, "One file is required.")
		return
	}
	result, err := h.service.UploadSensitiveWords(c.Request.Context(), files[0])
	if err != nil {
		var coded *common.CodedError
		if errors.As(err, &coded) && coded.Code == common.CodeArgumentError {
			common.Respond(c, http.StatusBadRequest, common.CodeArgumentError, nil, err.Error())
		} else {
			common.Respond(c, http.StatusInternalServerError, common.CodeServerError, nil, err.Error())
		}
		return
	}
	common.RespondSuccess(c, result, "Sensitive word file uploaded")
}

func (s *Service) UploadSensitiveWords(ctx context.Context, file *multipart.FileHeader) (map[string]interface{}, error) {
	if !strings.HasSuffix(strings.ToLower(file.Filename), ".txt") {
		return nil, common.NewCodedError(common.CodeArgumentError, "Only .txt files are allowed.")
	}
	if file.Size > compliance.MaxFileSize {
		return nil, common.NewCodedError(common.CodeArgumentError, "Only a txt file up to 30 MiB is allowed.")
	}
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	content, err := io.ReadAll(io.LimitReader(reader, compliance.MaxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(content) > compliance.MaxFileSize {
		return nil, common.NewCodedError(common.CodeArgumentError, "Only a txt file up to 30 MiB is allowed.")
	}
	if !utf8.Valid(content) {
		return nil, common.NewCodedError(common.CodeArgumentError, "Only UTF-8 txt files are allowed.")
	}
	store := storage.GetStorageFactory().GetStorage()
	if store == nil {
		return nil, fmt.Errorf("sensitive word filter storage is not initialized")
	}
	if err := store.Put(ctx, compliance.Bucket, compliance.ObjectKey, content); err != nil {
		return nil, fmt.Errorf("store sensitive word filter file: %w", err)
	}
	settingsDAO := dao.NewSystemSettingsDAO()
	if err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := settingsDAO.SaveOrCreate(ctx, tx, compliance.FileNameSetting, file.Filename, "variable", "string"); err != nil {
			return err
		}
		return settingsDAO.SaveOrCreate(ctx, tx, compliance.FileVersionSetting, common.GenerateToken(), "variable", "string")
	}); err != nil {
		return nil, fmt.Errorf("save sensitive word filter settings: %w", err)
	}
	count := 0
	for _, line := range compliance.SplitLines(strings.TrimPrefix(string(content), "\ufeff")) {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return map[string]interface{}{"size": len(content), "word_count": count, "filename": file.Filename}, nil
}

func (h *Handler) DownloadSensitiveWords(c *gin.Context) {
	common.StartOperationLog(c, "download_admin_sensitive_words")
	defer common.FinishOperationLog(c)

	content, filename, err := h.service.DownloadSensitiveWords(c.Request.Context())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.Respond(c, http.StatusNotFound, common.CodeNotFound, nil, "Sensitive words file is not uploaded.")
		} else {
			common.Respond(c, http.StatusInternalServerError, common.CodeServerError, nil, err.Error())
		}
		return
	}
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	c.Data(http.StatusOK, "text/plain; charset=utf-8", content)
}

func (s *Service) DownloadSensitiveWords(ctx context.Context) ([]byte, string, error) {
	store := storage.GetStorageFactory().GetStorage()
	if store == nil {
		return nil, "", fmt.Errorf("sensitive word filter storage is not initialized")
	}
	exists, err := store.ObjectExists(ctx, compliance.Bucket, compliance.ObjectKey)
	if err != nil {
		return nil, "", err
	}
	if !exists {
		return nil, "", gorm.ErrRecordNotFound
	}
	content, err := store.Get(ctx, compliance.Bucket, compliance.ObjectKey)
	if err != nil {
		return nil, "", err
	}
	settings, err := dao.NewSystemSettingsDAO().GetByName(ctx, dao.DB, compliance.FileNameSetting)
	if err != nil {
		return nil, "", err
	}
	filename := "compliance.txt"
	if len(settings) > 0 && settings[0].Value != "" {
		filename = settings[0].Value
	}
	return content, filename, nil
}
