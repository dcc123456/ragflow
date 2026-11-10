//go:build ee

// Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package dao

import (
	"context"
	"gorm.io/gorm"
	"ragflow/internal/entity"
)

// EvaluationDAO scopes child resources through their owning collection.
type EvaluationDAO struct{}

func NewEvaluationDAO() *EvaluationDAO { return &EvaluationDAO{} }

func (d *EvaluationDAO) GetCollection(ctx context.Context, db *gorm.DB, tenantID, id string) (*entity.EvaluationCollection, error) {
	var collection entity.EvaluationCollection
	err := db.WithContext(ctx).Where("id = ? AND tenant_id = ? AND status = ?", id, tenantID, 1).Take(&collection).Error
	return &collection, err
}

func (d *EvaluationDAO) GetCase(ctx context.Context, db *gorm.DB, tenantID, id string) (*entity.EvaluationCase, error) {
	var c entity.EvaluationCase
	err := db.WithContext(ctx).Model(&entity.EvaluationCase{}).Select("evaluation_cases.*").
		Joins("JOIN evaluation_collections ON evaluation_collections.id = evaluation_cases.collection_id").
		Where("evaluation_cases.id = ? AND evaluation_collections.tenant_id = ? AND evaluation_collections.status = ?", id, tenantID, 1).Take(&c).Error
	return &c, err
}

func (d *EvaluationDAO) GetRun(ctx context.Context, db *gorm.DB, tenantID, id string) (*entity.EvaluationRun, error) {
	var run entity.EvaluationRun
	err := db.WithContext(ctx).Model(&entity.EvaluationRun{}).Select("evaluation_runs.*").
		Joins("JOIN evaluation_collections ON evaluation_collections.id = evaluation_runs.collection_id").
		Where("evaluation_runs.id = ? AND evaluation_collections.tenant_id = ? AND evaluation_collections.status = ?", id, tenantID, 1).Take(&run).Error
	return &run, err
}

func (d *EvaluationDAO) GetResult(ctx context.Context, db *gorm.DB, tenantID, id string) (*entity.EvaluationResult, error) {
	var result entity.EvaluationResult
	err := db.WithContext(ctx).Model(&entity.EvaluationResult{}).Select("evaluation_results.*").
		Joins("JOIN evaluation_runs ON evaluation_runs.id = evaluation_results.run_id").
		Joins("JOIN evaluation_collections ON evaluation_collections.id = evaluation_runs.collection_id").
		Joins("JOIN evaluation_cases ON evaluation_cases.id = evaluation_results.case_id AND evaluation_cases.collection_id = evaluation_collections.id").
		Where("evaluation_results.id = ? AND evaluation_collections.tenant_id = ? AND evaluation_collections.status = ?", id, tenantID, 1).Take(&result).Error
	return &result, err
}
