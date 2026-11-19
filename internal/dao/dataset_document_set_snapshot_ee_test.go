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

	"ragflow/internal/documentset"
	"ragflow/internal/entity"
)

// TestDocumentSetUpsertKeepsOneRowPerContent pins the identity the whole change
// rests on: writing the same set twice costs one row, and a different set costs a
// second one.
func TestDocumentSetUpsertKeepsOneRowPerContent(t *testing.T) {
	db := openVersionHistoryTestDB(t)
	store := NewDatasetDocumentSetSnapshotDAO()
	ctx := context.Background()

	first := []byte(`[{"id":"doc-1"}]`)
	second := []byte(`[{"id":"doc-1"},{"id":"doc-2"}]`)
	for _, raw := range [][]byte{first, first, second} {
		hash := documentset.Hash(raw)
		if err := store.Upsert(ctx, db, "kb-1", hash, documentset.Encode(raw)); err != nil {
			t.Fatalf("upsert a set: %v", err)
		}
	}

	var count int64
	if err := db.Model(&entity.DatasetDocumentSetSnapshot{}).Where("dataset_id = ?", "kb-1").Count(&count).Error; err != nil {
		t.Fatalf("count the stored sets: %v", err)
	}
	if count != 2 {
		t.Fatalf("stored sets = %d, want 2 (one per distinct content)", count)
	}

	// A hash is only unique within its dataset: the same content in another dataset
	// is another row, which is what keeps the sweep from reaching across datasets.
	if err := store.Upsert(ctx, db, "kb-2", documentset.Hash(first), documentset.Encode(first)); err != nil {
		t.Fatalf("upsert the same content into another dataset: %v", err)
	}
	if err := db.Model(&entity.DatasetDocumentSetSnapshot{}).Count(&count).Error; err != nil {
		t.Fatalf("count every stored set: %v", err)
	}
	if count != 3 {
		t.Fatalf("stored sets = %d, want 3", count)
	}
}
