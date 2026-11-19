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
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"ragflow/internal/entity"
)

func setupChunkVersionTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		TranslateError: true,
	})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&entity.ChunkVersion{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return db
}

// TestVersionsAtDatasetVersionPicksTheProducedRow pins the read behind a chunk
// entry's version. One dataset version can write two rows for the same chunk -
// the state the change replaced and the one it produced - so the higher number
// is the entry's. A row written under another version, and a chunk the version
// did not touch, stay out of the answer.
func TestVersionsAtDatasetVersionPicksTheProducedRow(t *testing.T) {
	db := setupChunkVersionTestDB(t)
	seed := []entity.ChunkVersion{
		{ChunkID: "c1", Version: 1, DocID: "doc-1", DatasetID: "kb-1", DatasetVersion: 7},
		{ChunkID: "c1", Version: 2, DocID: "doc-1", DatasetID: "kb-1", DatasetVersion: 7},
		{ChunkID: "c1", Version: 3, DocID: "doc-1", DatasetID: "kb-1", DatasetVersion: 8},
		{ChunkID: "c2", Version: 1, DocID: "doc-1", DatasetID: "kb-1", DatasetVersion: 7},
		{ChunkID: "c3", Version: 4, DocID: "doc-2", DatasetID: "kb-2", DatasetVersion: 7},
	}
	for i := range seed {
		if err := db.Session(&gorm.Session{SkipHooks: true}).Create(&seed[i]).Error; err != nil {
			t.Fatalf("failed to seed a chunk version: %v", err)
		}
	}

	got, err := NewChunkVersionDAO().VersionsAtDatasetVersion(
		context.Background(), db, "kb-1", 7, []string{"c1", "c2", "c3", "c4"})
	if err != nil {
		t.Fatalf("failed to read the versions: %v", err)
	}
	if got["c1"] != 2 || got["c2"] != 1 {
		t.Fatalf("versions = %v, want c1=2 c2=1", got)
	}
	if _, ok := got["c3"]; ok {
		t.Fatalf("c3 belongs to another dataset, but the read returned it: %v", got)
	}
	if _, ok := got["c4"]; ok {
		t.Fatalf("c4 was not touched by the version, but the read returned it: %v", got)
	}
}

func TestVersionsAtDatasetVersionEmptyInputIsEmpty(t *testing.T) {
	got, err := NewChunkVersionDAO().VersionsAtDatasetVersion(
		context.Background(), nil, "", 0, nil)
	if err != nil {
		t.Fatalf("empty read failed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty read = %v, want an empty map", got)
	}
}
