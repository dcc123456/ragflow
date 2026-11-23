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

package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/ingestion/pipeline"
)

// AdminIngestionTask is the administrator-facing task response.
// Display fields are empty strings when their values are unavailable.
type AdminIngestionTask struct {
	ID            string     `json:"id"`
	PipelineLogID *string    `json:"pipeline_log_id"`
	PipelineID    *string    `json:"pipeline_id"`
	PipelineName  string     `json:"pipeline_name"`
	UserID        string     `json:"user_id"`
	Nickname      string     `json:"nickname"`
	Email         string     `json:"email"`
	DocumentID    string     `json:"document_id"`
	DocumentName  string     `json:"document_name"`
	DatasetID     string     `json:"dataset_id"`
	DatasetName   string     `json:"dataset_name"`
	Status        string     `json:"status"`
	CreateDate    *time.Time `json:"create_date"`
	UpdateDate    *time.Time `json:"update_date"`
}

// ListForAdmin returns a page of ingestion tasks across users.
func (s *IngestionTaskService) ListForAdmin(ctx context.Context, filter dao.AdminIngestionTaskFilter) ([]AdminIngestionTask, int64, error) {
	filter, err := normalizeAdminIngestionTaskFilter(filter)
	if err != nil {
		return nil, 0, err
	}
	tasks, total, err := s.ingestionTaskDAO.ListForAdmin(ctx, dao.DB, filter)
	if err != nil {
		return nil, 0, err
	}
	result := make([]AdminIngestionTask, 0, len(tasks))
	for _, task := range tasks {
		result = append(result, AdminIngestionTask{
			ID:            task.ID,
			PipelineLogID: task.PipelineLogID,
			PipelineID:    task.PipelineID,
			PipelineName:  resolveAdminIngestionPipelineName(task),
			UserID:        task.UserID,
			Nickname:      ptrStringValue(task.Nickname),
			Email:         ptrStringValue(task.Email),
			DocumentID:    task.DocumentID,
			DocumentName:  ptrStringValue(task.DocumentName),
			DatasetID:     task.DatasetID,
			DatasetName:   ptrStringValue(task.DatasetName),
			Status:        task.Status,
			CreateDate:    task.CreateDate,
			UpdateDate:    task.UpdateDate,
		})
	}
	return result, total, nil
}

func resolveAdminIngestionPipelineName(task dao.AdminIngestionTaskRow) string {
	if task.PipelineID != nil {
		return strings.TrimSpace(ptrStringValue(task.PipelineTitle))
	}

	parserID := strings.TrimSpace(ptrStringValue(task.ParserID))
	if parserID == "" {
		return ""
	}
	registry, err := pipeline.DefaultRegistry()
	if err == nil {
		if template, ok := registry.Get(parserID); ok && template.Title != "" {
			return template.Title
		}
	}
	return parserID
}

func normalizeAdminIngestionTaskFilter(filter dao.AdminIngestionTaskFilter) (dao.AdminIngestionTaskFilter, error) {
	filter.UserEmail = strings.TrimSpace(filter.UserEmail)
	filter.DatasetID = strings.TrimSpace(filter.DatasetID)
	filter.DatasetName = strings.TrimSpace(filter.DatasetName)
	filter.DocumentName = strings.TrimSpace(filter.DocumentName)
	filter.Status = strings.ToUpper(strings.TrimSpace(filter.Status))
	filter.OrderBy = strings.ToLower(strings.TrimSpace(filter.OrderBy))
	if filter.OrderBy == "" {
		filter.OrderBy = "update_time"
	}
	if filter.OrderBy != "create_time" && filter.OrderBy != "update_time" {
		return filter, common.NewCodedError(common.CodeBadRequest, "order_by must be create_time or update_time")
	}
	if filter.Page < 1 || filter.Page > 1_000_000 {
		return filter, common.NewCodedError(common.CodeBadRequest, "page must be between 1 and 1000000")
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		return filter, common.NewCodedError(common.CodeBadRequest, "page_size must be between 1 and 100")
	}
	switch filter.Status {
	case "", common.CREATED, common.SCHEDULED, common.RUNNING, common.STOPPING, common.COMPLETED, common.FAILED, common.STOPPED:
	default:
		return filter, common.NewCodedError(common.CodeBadRequest, fmt.Sprintf("unsupported status %q", filter.Status))
	}
	return filter, nil
}
