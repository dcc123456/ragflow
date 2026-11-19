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

package dao

import (
	"context"
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"ragflow/internal/entity"
)

// TestDatasetVersionCreateReportsADuplicateAsExists pins the mapping both
// writers depend on: two changes that allocate the same number (a history out of
// step with the dataset's version column, a half-restored backup) must surface as
// a named condition the caller can answer with 409 - or treat as "someone already
// seeded the baseline" - instead of a 500 carrying the driver's primary-key text.
func TestDatasetVersionCreateReportsADuplicateAsExists(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&entity.DatasetVersion{}, &entity.DatasetConfigSnapshot{}); err != nil {
		t.Fatalf("failed to migrate sqlite: %v", err)
	}

	ctx := context.Background()
	versionDAO := NewDatasetVersionDAO()
	if err := versionDAO.Create(ctx, db, &entity.DatasetVersion{
		DatasetID: "kb-1", Version: 1, Kind: entity.DatasetVersionKindParse,
	}); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}

	err = versionDAO.Create(ctx, db, &entity.DatasetVersion{
		DatasetID: "kb-1", Version: 1, Kind: entity.DatasetVersionKindParse,
	})
	if !errors.Is(err, ErrDatasetVersionExists) {
		t.Fatalf("duplicate Create() error = %v, want ErrDatasetVersionExists", err)
	}

	// The same number under another dataset is a different row, not a collision.
	if err := versionDAO.Create(ctx, db, &entity.DatasetVersion{
		DatasetID: "kb-2", Version: 1, Kind: entity.DatasetVersionKindParse,
	}); err != nil {
		t.Fatalf("Create() for another dataset error = %v", err)
	}
}
