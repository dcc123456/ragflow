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

	"ragflow/internal/entity"
)

// TestDocumentVersionLatestAtVersionSeparatesOneMillisecond pins the identity
// the table exists for: two states stamped the same millisecond stay two rows,
// and the version number, not the clock, says which one a version describes.
func TestDocumentVersionLatestAtVersionSeparatesOneMillisecond(t *testing.T) {
	db := openVersionHistoryTestDB(t)
	store := NewDocumentVersionDAO()
	ctx := context.Background()

	if err := store.CreateBatch(ctx, db, []*entity.DocumentVersion{
		{
			DocumentID: "doc-1", DatasetVersion: 5, DatasetID: "kb-1", Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "naive", ParserConfig: entity.JSONMap{"marker": "first"},
		},
		{
			DocumentID: "doc-1", DatasetVersion: 6, DatasetID: "kb-1", Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "general", ParserConfig: entity.JSONMap{"marker": "second"},
		},
	}); err != nil {
		t.Fatalf("append the states: %v", err)
	}

	var count int64
	if err := db.Model(&entity.DocumentVersion{}).Where("document_id = ?", "doc-1").Count(&count).Error; err != nil {
		t.Fatalf("count the states: %v", err)
	}
	if count != 2 {
		t.Fatalf("states = %d, want 2 (one per version, not per millisecond)", count)
	}

	atFive, err := store.LatestAtVersion(ctx, db, "doc-1", 5, entity.DatasetVersionKindDocumentConfig)
	if err != nil {
		t.Fatalf("state at version 5: %v", err)
	}
	if atFive.DatasetVersion != 5 || atFive.ParserConfig["marker"] != "first" {
		t.Fatalf("state at version 5 = v%d %v, want the first edit", atFive.DatasetVersion, atFive.ParserConfig)
	}
	atSix, err := store.LatestAtVersion(ctx, db, "doc-1", 6, entity.DatasetVersionKindDocumentConfig)
	if err != nil {
		t.Fatalf("state at version 6: %v", err)
	}
	if atSix.DatasetVersion != 6 || atSix.ParserConfig["marker"] != "second" {
		t.Fatalf("state at version 6 = v%d %v, want the second edit", atSix.DatasetVersion, atSix.ParserConfig)
	}
	// A version before the first state has none, so a replay leaves the document
	// alone instead of guessing.
	if _, err := store.LatestAtVersion(ctx, db, "doc-1", 4, entity.DatasetVersionKindDocumentConfig); err == nil {
		t.Fatal("a version before the first state resolved to a row")
	}
}

// TestDocumentVersionChangedSinceVersionScopesTheSweep pins the read a replay
// starts from: only the documents whose state a newer version produced.
func TestDocumentVersionChangedSinceVersionScopesTheSweep(t *testing.T) {
	db := openVersionHistoryTestDB(t)
	store := NewDocumentVersionDAO()
	ctx := context.Background()

	if err := store.CreateBatch(ctx, db, []*entity.DocumentVersion{
		{DocumentID: "doc-1", DatasetVersion: 2, DatasetID: "kb-1", Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "naive"},
		{DocumentID: "doc-2", DatasetVersion: 4, DatasetID: "kb-1", Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "naive"},
		{DocumentID: "doc-3", DatasetVersion: 7, DatasetID: "kb-2", Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "naive"},
	}); err != nil {
		t.Fatalf("append the states: %v", err)
	}

	moved, err := store.ChangedSinceVersion(ctx, db, "kb-1", 3, entity.DatasetVersionKindDocumentConfig)
	if err != nil {
		t.Fatalf("read the moved documents: %v", err)
	}
	if len(moved) != 1 || moved[0] != "doc-2" {
		t.Fatalf("documents moved after version 3 = %v, want [doc-2]", moved)
	}
	// Another dataset's rows never leak into the sweep.
	moved, err = store.ChangedSinceVersion(ctx, db, "kb-2", 0, entity.DatasetVersionKindDocumentConfig)
	if err != nil {
		t.Fatalf("read the moved documents of kb-2: %v", err)
	}
	if len(moved) != 1 || moved[0] != "doc-3" {
		t.Fatalf("documents of kb-2 = %v, want [doc-3]", moved)
	}
}

// TestDocumentVersionStatesAtVersionListsTheVersionOwnStates pins the read a
// restore of a metadata change starts from: the documents that version recorded
// the values of, whether or not anything about them moved afterwards.
func TestDocumentVersionStatesAtVersionListsTheVersionOwnStates(t *testing.T) {
	db := openVersionHistoryTestDB(t)
	store := NewDocumentVersionDAO()
	ctx := context.Background()

	if err := store.CreateBatch(ctx, db, []*entity.DocumentVersion{
		{DocumentID: "doc-1", DatasetVersion: 3, DatasetID: "kb-1", Kind: entity.DatasetVersionKindDocumentMetadata, MetaFieldsSnapshotID: "one"},
		{DocumentID: "doc-2", DatasetVersion: 3, DatasetID: "kb-1", Kind: entity.DatasetVersionKindDocumentMetadata, MetaFieldsSnapshotID: "two"},
		{DocumentID: "doc-3", DatasetVersion: 3, DatasetID: "kb-1", Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "naive"},
		{DocumentID: "doc-4", DatasetVersion: 4, DatasetID: "kb-1", Kind: entity.DatasetVersionKindDocumentMetadata, MetaFieldsSnapshotID: "four"},
		{DocumentID: "doc-5", DatasetVersion: 3, DatasetID: "kb-2", Kind: entity.DatasetVersionKindDocumentMetadata, MetaFieldsSnapshotID: "five"},
	}); err != nil {
		t.Fatalf("append the states: %v", err)
	}

	anchored, err := store.StatesAtVersion(ctx, db, "kb-1", 3, entity.DatasetVersionKindDocumentMetadata)
	if err != nil {
		t.Fatalf("read the states of version 3: %v", err)
	}
	if len(anchored) != 2 || anchored[0] != "doc-1" || anchored[1] != "doc-2" {
		t.Fatalf("metadata states of version 3 = %v, want [doc-1 doc-2]", anchored)
	}
	// Nothing at a version that recorded no metadata state of its own.
	anchored, err = store.StatesAtVersion(ctx, db, "kb-1", 9, entity.DatasetVersionKindDocumentMetadata)
	if err != nil {
		t.Fatalf("read the states of version 9: %v", err)
	}
	if len(anchored) != 0 {
		t.Fatalf("metadata states of version 9 = %v, want none", anchored)
	}
}

// TestDocumentVersionCreateBatchIsIdempotent pins the retry: the same
// (document, version) written twice is one row carrying the later values, which
// is what a best-effort writer needs when it fires twice.
func TestDocumentVersionCreateBatchIsIdempotent(t *testing.T) {
	db := openVersionHistoryTestDB(t)
	store := NewDocumentVersionDAO()
	ctx := context.Background()

	first := &entity.DocumentVersion{
		DocumentID: "doc-1", DatasetVersion: 3, DatasetID: "kb-1", Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "naive", ParserConfig: entity.JSONMap{"marker": "first"},
	}
	if err := store.CreateBatch(ctx, db, []*entity.DocumentVersion{first}); err != nil {
		t.Fatalf("append the state: %v", err)
	}
	again := &entity.DocumentVersion{
		DocumentID: "doc-1", DatasetVersion: 3, DatasetID: "kb-1", Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "naive", ParserConfig: entity.JSONMap{"marker": "second"},
	}
	if err := store.CreateBatch(ctx, db, []*entity.DocumentVersion{again}); err != nil {
		t.Fatalf("append the state again: %v", err)
	}

	var rows []entity.DocumentVersion
	if err := db.Where("document_id = ?", "doc-1").Find(&rows).Error; err != nil {
		t.Fatalf("read the states back: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("states = %d rows, want 1", len(rows))
	}
	if rows[0].ParserConfig["marker"] != "second" {
		t.Fatalf("state = %v, want the values written last", rows[0].ParserConfig)
	}
}

// TestDocumentVersionKindsDoNotBleed pins the reason a row names its kind: a
// settings state and a metadata state describe different parts of a document, so
// a replay of one must never read the other - an empty metadata map is a state,
// and a settings row never looked at the values at all.
func TestDocumentVersionKindsDoNotBleed(t *testing.T) {
	db := openVersionHistoryTestDB(t)
	store := NewDocumentVersionDAO()
	ctx := context.Background()

	if err := store.CreateBatch(ctx, db, []*entity.DocumentVersion{
		{
			DocumentID: "doc-1", DatasetVersion: 3, DatasetID: "kb-1", Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "naive",
		},
		{
			DocumentID: "doc-1", DatasetVersion: 4, DatasetID: "kb-1", Kind: entity.DatasetVersionKindDocumentMetadata, MetaFieldsSnapshotID: "doc-1:alice",
		},
	}); err != nil {
		t.Fatalf("append the states: %v", err)
	}

	settings, err := store.LatestAtVersion(ctx, db, "doc-1", 4, entity.DatasetVersionKindDocumentConfig)
	if err != nil {
		t.Fatalf("settings at version 4: %v", err)
	}
	if settings.DatasetVersion != 3 {
		t.Fatalf("settings version = %d, want 3 (the metadata row is not a settings state)", settings.DatasetVersion)
	}
	values, err := store.LatestAtVersion(ctx, db, "doc-1", 4, entity.DatasetVersionKindDocumentMetadata)
	if err != nil {
		t.Fatalf("metadata at version 4: %v", err)
	}
	if values.DatasetVersion != 4 || values.MetaFieldsSnapshotID != "doc-1:alice" {
		t.Fatalf("metadata state = v%d %q, want the state of version 4", values.DatasetVersion, values.MetaFieldsSnapshotID)
	}
}
