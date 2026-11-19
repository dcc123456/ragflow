//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

//go:build ee

package task

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/ingestion"
)

// setupBaselineAnchorDB opens an in-memory database holding the dataset and
// chunk history tables, seeds the dataset at version 5 (as if earlier parses had
// advanced it), and swaps it in as the process-wide handle the history writers
// read.
func setupBaselineAnchorDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&entity.Knowledgebase{}, &entity.ChunkVersion{}); err != nil {
		t.Fatalf("auto-migrate sqlite: %v", err)
	}
	if err := db.Create(&entity.Knowledgebase{
		ID:       "kb-1",
		TenantID: "tenant-1",
		Name:     "kb",
		Version:  5,
	}).Error; err != nil {
		t.Fatalf("seed dataset: %v", err)
	}
	origDB := dao.DB
	dao.DB = db
	t.Cleanup(func() { dao.DB = origDB })
	return db
}

// compensatingExecutor builds the executor a failed index write leaves behind:
// no engine, no pipeline, just the pieces the compensation path touches.
func compensatingExecutor(db *gorm.DB, anchor *parseBaselineAnchor) *PipelineExecutor {
	return &PipelineExecutor{
		taskCtx: &TaskContext{
			Doc: entity.Document{ID: "doc-1", KbID: "kb-1"},
		},
		deps:          &ingestion.Deps{DB: db},
		historyAnchor: anchor,
		indexWriter:   newChunkIndexWriter(nil, "ragflow_tenant-1", "kb-1", 0),
		deleteChunksFunc: func(_ context.Context, _ map[string]any, _, _ string) (int64, error) {
			return 1, nil
		},
	}
}

// TestCompensateFailedIndexWriteKeepsTheRunAnchor pins the rule a failed run
// follows: the removal it records shares the number the run already reserved for
// its baselines, so a parse that fails advances the dataset no further. Taking a
// fresh number would push the dataset's counter past a version that describes
// nothing, and the chunk history does not need one — AppendTombstones writes one
// past the latest row of each chunk, which already sorts the removal after the
// baseline it cancels.
func TestCompensateFailedIndexWriteKeepsTheRunAnchor(t *testing.T) {
	db := setupBaselineAnchorDB(t)
	// The batch before the failure wrote its baseline at the run's anchor (5).
	if err := db.Create(&entity.ChunkVersion{
		ChunkID:        "chunk-1",
		Version:        0,
		DatasetVersion: 5,
		DocID:          "doc-1",
		DatasetID:      "kb-1",
		CreatedBy:      "system",
		Kind:           entity.ChunkVersionKindParse,
		Snapshot:       entity.JSONMap{"content_with_weight": "body of chunk-1"},
	}).Error; err != nil {
		t.Fatalf("seed baseline: %v", err)
	}

	svc := compensatingExecutor(db, &parseBaselineAnchor{version: 5})
	if err := svc.compensateFailedIndexWrite(t.Context(), []map[string]any{{"id": "chunk-1"}}); err != nil {
		t.Fatalf("compensateFailedIndexWrite: %v", err)
	}

	var kb entity.Knowledgebase
	if err := db.Where("id = ?", "kb-1").Take(&kb).Error; err != nil {
		t.Fatalf("load dataset: %v", err)
	}
	if kb.Version != 5 {
		t.Fatalf("dataset version = %d, want 5: a failed run must not reserve a version", kb.Version)
	}

	var rows []entity.ChunkVersion
	if err := db.Where("doc_id = ?", "doc-1").Order("version").Find(&rows).Error; err != nil {
		t.Fatalf("load chunk history: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("chunk history rows = %d, want the baseline and its tombstone", len(rows))
	}
	for _, row := range rows {
		if row.DatasetVersion != 5 {
			t.Fatalf("chunk history row %s carries dataset version %d, want the run anchor 5",
				row.ChunkID, row.DatasetVersion)
		}
	}
	if rows[1].Kind != entity.ChunkVersionKindDelete {
		t.Fatalf("newest chunk history row kind = %s, want a delete", rows[1].Kind)
	}
}

// TestCompensateFailedIndexWriteWithoutAnchorWritesNothing covers the run that
// failed before its first batch: no baseline was written, so there is nothing to
// cancel and no number to stamp the removal with. The dataset must be untouched.
func TestCompensateFailedIndexWriteWithoutAnchorWritesNothing(t *testing.T) {
	db := setupBaselineAnchorDB(t)

	svc := compensatingExecutor(db, &parseBaselineAnchor{})
	if err := svc.compensateFailedIndexWrite(t.Context(), []map[string]any{{"id": "chunk-1"}}); err != nil {
		t.Fatalf("compensateFailedIndexWrite: %v", err)
	}

	var kb entity.Knowledgebase
	if err := db.Where("id = ?", "kb-1").Take(&kb).Error; err != nil {
		t.Fatalf("load dataset: %v", err)
	}
	if kb.Version != 5 {
		t.Fatalf("dataset version = %d, want 5: a failed run must not reserve a version", kb.Version)
	}
	var count int64
	if err := db.Model(&entity.ChunkVersion{}).Count(&count).Error; err != nil {
		t.Fatalf("count chunk history: %v", err)
	}
	if count != 0 {
		t.Fatalf("chunk history rows = %d, want none without a baseline to cancel", count)
	}
}
