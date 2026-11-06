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

package dao

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AdminIngestionTaskFilter selects current ingestion tasks across users.
type AdminIngestionTaskFilter struct {
	UserEmail    string
	Status       string
	DatasetID    string
	DatasetName  string
	DocumentName string
	Page         int
	PageSize     int
	OrderBy      string
	Desc         bool
}

// AdminIngestionTaskRow holds task and metadata query results.
type AdminIngestionTaskRow struct {
	ID            string     `gorm:"column:id"`
	PipelineLogID *string    `gorm:"column:pipeline_log_id"`
	PipelineID    *string    `gorm:"column:pipeline_id"`
	PipelineName  *string    `gorm:"column:pipeline_name"`
	UserID        string     `gorm:"column:user_id"`
	Nickname      *string    `gorm:"column:nickname"`
	Email         *string    `gorm:"column:email"`
	DocumentID    string     `gorm:"column:document_id"`
	DocumentName  *string    `gorm:"column:document_name"`
	DatasetID     string     `gorm:"column:dataset_id"`
	DatasetName   *string    `gorm:"column:dataset_name"`
	Status        string     `gorm:"column:status"`
	CreateDate    *time.Time `gorm:"column:create_date"`
	UpdateDate    *time.Time `gorm:"column:update_date"`
}

// ListForAdmin counts and pages tasks before loading display metadata.
func (d *IngestionTaskDAO) ListForAdmin(ctx context.Context, db *gorm.DB, filter AdminIngestionTaskFilter) ([]AdminIngestionTaskRow, int64, error) {
	if filter.Page < 1 || filter.Page > 1_000_000 || filter.PageSize < 1 || filter.PageSize > 100 {
		return nil, 0, fmt.Errorf("invalid ingestion task pagination")
	}
	var orderColumn string
	switch filter.OrderBy {
	case "", "update_time":
		orderColumn = "update_time"
	case "create_time":
		orderColumn = "create_time"
	default:
		return nil, 0, fmt.Errorf("unsupported ingestion task order %q", filter.OrderBy)
	}
	var total int64
	if err := adminIngestionTasksQuery(ctx, db, filter).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	tasks := make([]AdminIngestionTaskRow, 0)
	offset := (filter.Page - 1) * filter.PageSize
	if int64(offset) >= total {
		return tasks, total, nil
	}
	page := orderAdminIngestionTasks(adminIngestionTasksQuery(ctx, db, filter), orderColumn, filter.Desc).
		Select("task.id, task." + orderColumn).
		Offset(offset).Limit(filter.PageSize)
	statement := &gorm.Statement{DB: db}
	query := db.WithContext(ctx).Table("(?) AS page", page).
		Joins("JOIN ingestion_task AS task ON task.id = page.id").
		Joins("LEFT JOIN " + statement.Quote("user") + " AS usr ON usr.id = task.user_id").
		Joins("LEFT JOIN knowledgebase AS kb ON kb.id = task.dataset_id").
		Joins("LEFT JOIN document AS doc ON doc.id = task.document_id").
		Joins("LEFT JOIN user_canvas AS canvas ON canvas.id = doc.pipeline_id").
		Select("task.id, task.pipeline_log_id, doc.pipeline_id, canvas.title AS pipeline_name, " +
			"task.user_id, usr.nickname, usr.email, task.document_id, doc.name AS document_name, " +
			"task.dataset_id, kb.name AS dataset_name, task.status, task.create_date, task.update_date")
	if err := orderAdminIngestionTasks(query, orderColumn, filter.Desc).Scan(&tasks).Error; err != nil {
		return nil, 0, err
	}
	return tasks, total, nil
}

func orderAdminIngestionTasks(query *gorm.DB, column string, desc bool) *gorm.DB {
	return query.
		Order(clause.OrderByColumn{Column: clause.Column{Table: "task", Name: column}, Desc: desc}).
		Order(clause.OrderByColumn{Column: clause.Column{Table: "task", Name: "id"}, Desc: desc})
}

func adminIngestionTasksQuery(ctx context.Context, db *gorm.DB, filter AdminIngestionTaskFilter) *gorm.DB {
	query := db.WithContext(ctx).Table("ingestion_task AS task")
	if filter.UserEmail != "" {
		statement := &gorm.Statement{DB: db}
		query = query.Joins("JOIN "+statement.Quote("user")+" AS usr ON usr.id = task.user_id").
			Where("usr.email = ?", filter.UserEmail)
	}
	if filter.Status != "" {
		query = query.Where("task.status = ?", filter.Status)
	}
	if filter.DatasetID != "" {
		query = query.Where("task.dataset_id = ?", filter.DatasetID)
	}
	if filter.DatasetName != "" {
		query = query.Joins("LEFT JOIN knowledgebase AS kb ON kb.id = task.dataset_id").
			Where("LOWER(kb.name) LIKE ? ESCAPE '!'", adminIngestionNamePattern(filter.DatasetName))
	}
	if filter.DocumentName != "" {
		query = query.Joins("LEFT JOIN document AS doc ON doc.id = task.document_id").
			Where("LOWER(doc.name) LIKE ? ESCAPE '!'", adminIngestionNamePattern(filter.DocumentName))
	}
	return query
}

func adminIngestionNamePattern(name string) string {
	return "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(name)) + "%"
}
