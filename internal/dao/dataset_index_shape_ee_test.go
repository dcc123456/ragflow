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
	"net/url"
	"sort"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"ragflow/internal/entity"
)

// versionHistoryModels are the tables whose indexes this file pins.
func versionHistoryModels() []interface{} {
	return []interface{}{
		&entity.DatasetVersion{},
		&entity.DatasetConfigSnapshot{},
		&entity.DatasetDocumentSetSnapshot{},
		&entity.DatasetRemovedDocument{},
		&entity.ChunkVersion{},
		&entity.DocumentVersion{},
	}
}

func openVersionHistoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+url.QueryEscape(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{
		TranslateError: true,
	})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("failed to access sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(versionHistoryModels()...); err != nil {
		t.Fatalf("failed to migrate the history tables: %v", err)
	}
	return db
}

// indexColumns returns an index's key columns in index order.
func indexColumns(t *testing.T, db *gorm.DB, index string) []string {
	t.Helper()
	var rows []struct {
		Seqno int
		Name  string
	}
	if err := db.Raw("PRAGMA index_info('" + index + "')").Scan(&rows).Error; err != nil {
		t.Fatalf("read index %s: %v", index, err)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Seqno < rows[j].Seqno })
	columns := make([]string, 0, len(rows))
	for _, row := range rows {
		columns = append(columns, row.Name)
	}
	return columns
}

// assertPrimaryKeyColumns checks a table's primary-key column order, which
// PRAGMA index_info cannot report.
func assertPrimaryKeyColumns(t *testing.T, db *gorm.DB, table string, want []string) {
	t.Helper()
	var rows []struct {
		Name string
		Pk   int
	}
	if err := db.Raw("SELECT name, pk FROM pragma_table_info('" + table + "') WHERE pk > 0 ORDER BY pk").
		Scan(&rows).Error; err != nil {
		t.Fatalf("read the %s primary key: %v", table, err)
	}
	got := make([]string, 0, len(rows))
	for _, row := range rows {
		got = append(got, row.Name)
	}
	if len(got) != len(want) {
		t.Fatalf("%s primary key = %v, want %v", table, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s primary key = %v, want %v", table, got, want)
		}
	}
}

func assertIndexColumns(t *testing.T, db *gorm.DB, index string, want []string) {
	t.Helper()
	got := indexColumns(t, db, index)
	if len(got) != len(want) {
		t.Fatalf("index %s columns = %v, want %v", index, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("index %s columns = %v, want %v", index, got, want)
		}
	}
}

// TestVersionTablesDeclareTheIndexesTheirQueriesUse pins the shapes the DAOs
// depend on, because a shape is invisible in a query: a wrong column order still
// answers, just by scanning more than it reads.
//
//   - the removed-documents list sorts one dataset by removal time
//     (DatasetRemovedDocumentDAO.ListByDataset),
//   - the per-document states are read back by "which documents of this dataset
//     moved after a version", which deduplicates document ids and orders them by
//     the number (DocumentVersionDAO.ChangedSinceVersion),
//   - that same read seeks one kind of one document at a version
//     (DocumentVersionDAO.LatestAtVersion), which is why the primary key orders
//     kind ahead of the version bound and needs no second index,
//   - the snapshot sweep tests each version's two hashes in a correlated
//     NOT EXISTS (DatasetConfigSnapshotDAO.deleteUnreferenced and its document
//     set counterpart), so the dataset leading the primary key is not enough.
func TestVersionTablesDeclareTheIndexesTheirQueriesUse(t *testing.T) {
	db := openVersionHistoryTestDB(t)

	expected := map[string][]string{
		"idx_removed_document_time":     {"dataset_id", "create_time"},
		"idx_chunk_version_doc":         {"doc_id"},
		"idx_document_version_dataset":  {"dataset_id", "kind", "dataset_version"},
		"idx_dataset_version_config":    {"dataset_id", "config_hash"},
		"idx_dataset_version_documents": {"dataset_id", "documents_hash"},
	}
	for index, columns := range expected {
		assertIndexColumns(t, db, index, columns)
	}
	// The per-document read is answered by the primary key itself: equality on
	// document_id and kind, a descending range on dataset_version.
	assertPrimaryKeyColumns(t, db, "document_version",
		[]string{"document_id", "kind", "dataset_version"})
}

// TestVersionTablesCarryNoUnusedIndexes pins the other half: an index nothing
// reads is a write every version append pays for. A lone kind index loses to the
// dataset-leading primary key, and a version is never ranged over by its write
// time (the anchor is read one row at a time).
//
// The hash indexes are absent from this list on purpose: they look like they
// duplicate a dataset-leading lookup, but the sweep tests a hash equality, which
// the primary key cannot narrow - see TestVersionTablesDeclareTheIndexesTheirQueriesUse.
func TestVersionTablesCarryNoUnusedIndexes(t *testing.T) {
	db := openVersionHistoryTestDB(t)

	unused := []struct {
		model interface{}
		index string
	}{
		{&entity.DatasetVersion{}, "idx_dataset_version_kind"},
		{&entity.DatasetVersion{}, "idx_dataset_version_create_time"},
		{&entity.DatasetVersion{}, "idx_dataset_version_time"},
		// Superseded by the primary key's column order.
		{&entity.DocumentVersion{}, "idx_document_version_latest"},
	}
	for _, entry := range unused {
		if db.Migrator().HasIndex(entry.model, entry.index) {
			t.Fatalf("%s is declared again; nothing queries it", entry.index)
		}
	}
}

// TestDocumentStatesInOneMillisecondStayTwoStates covers the rule the table
// exists for: two writes that land in one millisecond are two states, because
// their identity is the version number and not the clock. A clock-keyed row
// would have to let the second write take the first one's row over, and the
// version that recorded the first change would then read back the second one.
func TestDocumentStatesInOneMillisecondStayTwoStates(t *testing.T) {
	db := openVersionHistoryTestDB(t)
	store := NewDocumentVersionDAO()
	ctx := context.Background()

	first := &entity.DocumentVersion{
		DocumentID: "doc-1", DatasetVersion: 4, DatasetID: "kb-1",
		Kind:     entity.DatasetVersionKindDocumentConfig,
		ParserID: "general", ParserConfig: entity.JSONMap{"chunk_token_num": float64(128)},
	}
	second := &entity.DocumentVersion{
		DocumentID: "doc-1", DatasetVersion: 5, DatasetID: "kb-1",
		Kind:     entity.DatasetVersionKindDocumentConfig,
		ParserID: "naive", ParserConfig: entity.JSONMap{"chunk_token_num": float64(512)},
	}
	if err := store.CreateBatch(ctx, db, []*entity.DocumentVersion{first, second}); err != nil {
		t.Fatalf("append the two states: %v", err)
	}

	var rows []entity.DocumentVersion
	if err := db.Where("document_id = ?", "doc-1").Order("dataset_version").Find(&rows).Error; err != nil {
		t.Fatalf("read the states: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("states at one millisecond = %d, want both", len(rows))
	}
	if rows[0].ParserID != "general" || rows[1].ParserID != "naive" {
		t.Fatalf("states = %q, %q; want one per version", rows[0].ParserID, rows[1].ParserID)
	}

	// Each version reads back the state it produced, not the later one.
	atFour, err := store.LatestAtVersion(ctx, db, "doc-1", 4, entity.DatasetVersionKindDocumentConfig)
	if err != nil {
		t.Fatalf("read the state at version 4: %v", err)
	}
	if atFour.ParserID != "general" {
		t.Fatalf("state at version 4 = %q, want the first write", atFour.ParserID)
	}
	atFive, err := store.LatestAtVersion(ctx, db, "doc-1", 5, entity.DatasetVersionKindDocumentConfig)
	if err != nil {
		t.Fatalf("read the state at version 5: %v", err)
	}
	if atFive.ParserID != "naive" {
		t.Fatalf("state at version 5 = %q, want the second write", atFive.ParserID)
	}
}
