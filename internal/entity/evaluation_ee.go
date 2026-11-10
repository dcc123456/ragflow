//go:build ee

// Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package entity

// EvaluationCollection groups cases evaluated against a chat or agent.
type EvaluationCollection struct {
	BaseModel
	ID          string  `gorm:"column:id;primaryKey;size:32" json:"id"`
	UserID      string  `gorm:"column:user_id;size:32;not null;default:'';index" json:"user_id"`
	TenantID    string  `gorm:"column:tenant_id;size:32;not null;index" json:"tenant_id"`
	TargetType  string  `gorm:"column:target_type;size:32;not null;default:chat;index" json:"target_type"`
	Name        string  `gorm:"column:name;size:255;not null;index" json:"name"`
	Description *string `gorm:"column:description;type:longtext" json:"description,omitempty"`
	CreatedBy   string  `gorm:"column:created_by;size:32;not null;index" json:"created_by"`
	Status      int64   `gorm:"column:status;not null;default:1;index" json:"status"`
}

func (EvaluationCollection) TableName() string { return "evaluation_collections" }

type EvaluationCase struct {
	BaseModel
	ID             string    `gorm:"column:id;primaryKey;size:32" json:"id"`
	CollectionID   string    `gorm:"column:collection_id;size:32;not null;index" json:"collection_id"`
	Variable       JSONMap   `gorm:"column:variable;type:longtext;not null" json:"variable"`
	RelevantDocIDs JSONSlice `gorm:"column:relevant_doc_ids;type:longtext" json:"relevant_doc_ids"`
	RelevantKbIDs  JSONSlice `gorm:"column:relevant_kb_ids;type:longtext" json:"relevant_kb_ids"`
	Metadata       JSONMap   `gorm:"column:metadata;type:longtext" json:"metadata"`
}

func (EvaluationCase) TableName() string { return "evaluation_cases" }

type EvaluationRun struct {
	BaseModel
	ID                  string  `gorm:"column:id;primaryKey;size:32" json:"id"`
	CollectionID        string  `gorm:"column:collection_id;size:32;not null;index" json:"collection_id"`
	TargetType          string  `gorm:"column:target_type;size:32;not null;default:chat;index" json:"target_type"`
	TargetID            string  `gorm:"column:target_id;size:32;not null;default:'';index" json:"target_id"`
	Name                string  `gorm:"column:name;size:255;not null" json:"name"`
	ConfigSnapshot      JSONMap `gorm:"column:config_snapshot;type:longtext;not null" json:"config_snapshot"`
	MetricsSummary      JSONMap `gorm:"column:metrics_summary;type:longtext" json:"metrics_summary"`
	Status              string  `gorm:"column:status;size:32;not null;default:PENDING" json:"status"`
	CreatedBy           string  `gorm:"column:created_by;size:32;not null;index" json:"created_by"`
	CompleteTime        *int64  `gorm:"column:complete_time" json:"complete_time,omitempty"`
	TaskID              string  `gorm:"column:task_id;size:32" json:"task_id"`
	ExecutionConfig     JSONMap `gorm:"column:execution_config;type:longtext" json:"-"`
	ExecutionGeneration int64   `gorm:"column:execution_generation;not null;default:0" json:"execution_generation"`
	LeaseOwner          string  `gorm:"column:lease_owner;size:64;not null;default:''" json:"-"`
	LeaseExpiresAt      *int64  `gorm:"column:lease_expires_at;index" json:"-"`
	Progress            int     `gorm:"column:progress;not null;default:0" json:"progress"`
	Error               string  `gorm:"column:error;type:longtext" json:"error,omitempty"`
}

func (EvaluationRun) TableName() string { return "evaluation_runs" }

type EvaluationResult struct {
	BaseModel
	ID              string  `gorm:"column:id;primaryKey;size:32" json:"id"`
	RunID           string  `gorm:"column:run_id;size:32;not null;index;uniqueIndex:idx_evaluation_result_run_case,priority:1" json:"run_id"`
	CaseID          string  `gorm:"column:case_id;size:32;not null;index;uniqueIndex:idx_evaluation_result_run_case,priority:2" json:"case_id"`
	GeneratedAnswer string  `gorm:"column:generated_answer;type:longtext;not null" json:"generated_answer"`
	RetrievedChunks JSONMap `gorm:"column:retrieved_chunks;type:longtext;not null" json:"retrieved_chunks"`
	Metrics         JSONMap `gorm:"column:metrics;type:longtext;not null" json:"metrics"`
	ExecutionTime   float64 `gorm:"column:execution_time;not null" json:"execution_time"`
	TokenUsage      JSONMap `gorm:"column:token_usage;type:longtext" json:"token_usage"`
	Status          string  `gorm:"column:status;size:32;not null;default:PENDING" json:"status"`
	Error           string  `gorm:"column:error;type:longtext" json:"error,omitempty"`
	Generation      int64   `gorm:"column:generation;not null;default:0" json:"generation"`
}

func (EvaluationResult) TableName() string { return "evaluation_results" }
