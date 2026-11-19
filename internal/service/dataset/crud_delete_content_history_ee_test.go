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

package dataset

import (
	"testing"

	"ragflow/internal/dao"
	"ragflow/internal/engine/history"
	"ragflow/internal/engine/history/historytest"
	"ragflow/internal/entity"
	"ragflow/internal/storage"
)

// TestDeleteDatasetDropsContentHistoryRows pins what a dataset delete collects
// that no other path would: the chunk history (chunk text and image ids of
// every edit), the removed-document rows, the document states, and the metadata
// values those states named in the document engine.
// The documents are removed directly rather than through the per-document
// removal, so the removed-document cleanup — which only walks removal rows —
// never sees them, and a removal row left behind would still name documents of
// a dataset that no longer exists.
func TestDeleteDatasetDropsContentHistoryRows(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	if err := db.AutoMigrate(
		&entity.DatasetVersion{},
		&entity.DatasetConfigSnapshot{},
		&entity.DatasetDocumentSetSnapshot{},
		&entity.DatasetRemovedDocument{},
		&entity.DocumentVersion{},
	); err != nil {
		t.Fatalf("migrate content history tables: %v", err)
	}
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	if err := db.Create(&entity.ChunkVersion{
		ChunkID:        "chunk-1",
		Version:        0,
		DocID:          "doc-1",
		DatasetID:      "kb-1",
		DatasetVersion: 1,
		Kind:           entity.ChunkVersionKindParse,
		Snapshot:       entity.JSONMap{"content_with_weight": "body", "img_id": "kb-1-chunk-1-hash"},
	}).Error; err != nil {
		t.Fatalf("insert chunk version: %v", err)
	}
	if err := db.Create(&entity.DatasetRemovedDocument{
		ID:         "removal-1",
		DatasetID:  "kb-1",
		DocumentID: "doc-1",
		Document:   entity.JSONMap{"id": "doc-1"},
		Files:      entity.JSONMap{"files": []interface{}{}, "mappings": []interface{}{}},
	}).Error; err != nil {
		t.Fatalf("insert removed document: %v", err)
	}
	if err := db.Create(&entity.DocumentVersion{
		DocumentID: "doc-1", DatasetVersion: 1, DatasetID: "kb-1",
		Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "naive",
		ParserConfig: entity.JSONMap{"marker": "x"},
	}).Error; err != nil {
		t.Fatalf("insert parser settings: %v", err)
	}
	// The metadata values live in the document engine rather than in a table, so
	// a store stands in for it: a metadata row names the state holding its
	// values, and what the delete reaches there is the only thing that keeps a
	// removed dataset from leaving them behind.
	store := historytest.NewMetadataStore()
	history.SetMetadataStore(store)
	t.Cleanup(func() { history.SetMetadataStore(nil) })
	if err := db.Create(&entity.DocumentVersion{
		DocumentID: "doc-1", DatasetVersion: 1, DatasetID: "kb-1",
		Kind:       entity.DatasetVersionKindDocumentMetadata,
		MetaFields: entity.JSONMap{"author": "alice"},
	}).Error; err != nil {
		t.Fatalf("insert metadata state: %v", err)
	}
	if err := db.Create(&entity.DatasetDocumentSetSnapshot{
		DatasetID: "kb-1", DocumentsHash: "hash-1", Documents: `["doc-1"]`,
	}).Error; err != nil {
		t.Fatalf("insert document set: %v", err)
	}

	// The bucket is emptied before the transaction; a memory storage keeps the
	// delete path free of a real object store.
	factory := storage.GetStorageFactory()
	previous := factory.GetStorage()
	factory.SetStorage(storage.NewMemoryStorage())
	t.Cleanup(func() { factory.SetStorage(previous) })

	if ids := store.SnapshotIDs("kb-1"); len(ids) != 1 {
		t.Fatalf("states before the delete = %v, want one to drop", ids)
	}

	kb, err := dao.NewKnowledgebaseDAO().GetByID(t.Context(), db, "kb-1")
	if err != nil {
		t.Fatalf("load dataset: %v", err)
	}
	service := testDatasetUpdateService(t)
	if err := service.deleteDataset(t.Context(), kb); err != nil {
		t.Fatalf("delete dataset: %v", err)
	}

	for _, tc := range []struct {
		name  string
		model interface{}
	}{
		{name: "chunk versions", model: &entity.ChunkVersion{}},
		{name: "removed documents", model: &entity.DatasetRemovedDocument{}},
		{name: "document sets", model: &entity.DatasetDocumentSetSnapshot{}},
		{name: "document states", model: &entity.DocumentVersion{}},
	} {
		var count int64
		if err := db.Model(tc.model).Where("dataset_id = ?", "kb-1").Count(&count).Error; err != nil {
			t.Fatalf("count %s: %v", tc.name, err)
		}
		if count != 0 {
			t.Fatalf("%s left after the dataset delete = %d, want 0", tc.name, count)
		}
	}

	if ids := store.SnapshotIDs("kb-1"); len(ids) != 0 {
		t.Fatalf("metadata states left in the engine = %v, want none", ids)
	}
}
