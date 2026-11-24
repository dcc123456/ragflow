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

package service

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"ragflow/internal/dao"
	"ragflow/internal/engine/history"
	"ragflow/internal/engine/history/historytest"
	"ragflow/internal/entity"
	"ragflow/internal/service/chunkversion"
	"ragflow/internal/service/datasetversion"
)

// setupParseVersionDB opens an in-memory database with the tables a finished
// parse touches, and swaps it in as the process-wide handle the history writers
// read.
func setupParseVersionDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&entity.Knowledgebase{},
		// A parse version records the document set the dataset held, and a read
		// that fails is an error now instead of an empty set the version could
		// not hand over later.
		&entity.Document{},
		&entity.DatasetVersion{},
		&entity.ChunkVersion{},
		// The first version of a dataset seeds its baseline, and a baseline
		// records the state - including the data-source bindings, which live in
		// their own table.
		&entity.Connector2Kb{},
		// The configuration snapshot and the document set a version points at:
		// without them the version write fails and the parse leaves no history.
		&entity.DatasetConfigSnapshot{},
		&entity.DatasetDocumentSetSnapshot{},
	); err != nil {
		t.Fatalf("auto-migrate sqlite: %v", err)
	}
	if err := db.Create(&entity.Knowledgebase{ID: "kb-1", TenantID: "tenant-1", Name: "kb"}).Error; err != nil {
		t.Fatalf("seed dataset: %v", err)
	}
	// The chunk history bodies live in the document engine, which the tests stand
	// in for with the in-memory store. Without it every baseline append answers
	// "this deployment keeps no chunk history" and the parse version has nothing
	// to replay.
	history.SetChunkStore(historytest.NewChunkStore())
	t.Cleanup(func() { history.SetChunkStore(nil) })
	origDB := dao.DB
	dao.DB = db
	t.Cleanup(func() { dao.DB = origDB })
	return db
}

// seedChunkBaseline writes the history row a parse leaves behind for a chunk.
func seedChunkBaseline(t *testing.T, chunkID string) {
	t.Helper()
	rows := []map[string]interface{}{{
		"id":                  chunkID,
		"doc_id":              "doc-1",
		"kb_id":               "kb-1",
		"content_with_weight": "body of " + chunkID,
	}}
	// The baseline is anchored at a dataset version; the parse version the test
	// records afterwards takes a later number.
	if err := chunkversion.AppendParseBaseline(t.Context(), dao.DB, "kb-1", 1, chunkversion.SnapshotMeta{}, rows); err != nil {
		t.Fatalf("append baseline %s: %v", chunkID, err)
	}
}

// latestParseVersion returns the parse version a run recorded.
func latestParseVersion(t *testing.T) entity.DatasetVersion {
	t.Helper()
	var versions []entity.DatasetVersion
	if err := dao.DB.Where("dataset_id = ? AND kind = ?", "kb-1", entity.DatasetVersionKindParse).
		Order("version").
		Find(&versions).Error; err != nil {
		t.Fatalf("load versions: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("parse versions = %d, want 1", len(versions))
	}
	return versions[0]
}

// TestRecordParseVersionReplaysTheChunksItDescribes is the ordering guarantee a
// restore depends on: the version is written after the parser's chunk baselines,
// so replaying the chunk history at the version's own anchor finds them.
//
// Recorded the other way round — when the parse was requested, before the parser
// runs — the anchor precedes every chunk row and restoring that version rebuilds
// the document empty.
func TestRecordParseVersionReplaysTheChunksItDescribes(t *testing.T) {
	setupParseVersionDB(t)
	seedChunkBaseline(t, "chunk-1")
	seedChunkBaseline(t, "chunk-2")

	recordParseVersion(t.Context(), dao.DB, "doc-1", "kb-1", "manual.pdf", []string{"chunk-1", "chunk-2"}, 0)

	version := latestParseVersion(t)
	if version.CreateTime == nil {
		t.Fatal("version create_time is nil, want the millisecond anchor")
	}

	// The change set rides in the version row: no second table to read.
	if changes := version.Changes; len(changes) != 1 ||
		changes[0].Entity != entity.DatasetVersionChangeEntityDocument ||
		changes[0].Action != entity.DatasetVersionChangeActionParse ||
		changes[0].DocID != "doc-1" ||
		changes[0].Name != "manual.pdf" {
		t.Fatalf("changes = %+v, want one parsed-document entry", changes)
	}

	states, err := chunkversion.StatesAt(t.Context(), dao.DB, "doc-1", version.Version)
	if err != nil {
		t.Fatalf("replay chunk history: %v", err)
	}
	if len(states) != 2 {
		t.Fatalf("replay at the version anchor = %d chunks, want 2", len(states))
	}
}

// TestRecordParseVersionTombstonesChunksTheRerunDropped covers the re-parse
// case: chunk ids come from the text, so a chunk the new run did not produce is
// one that disappeared from the engine, and the replay of the version recorded
// right after must not bring it back.
func TestRecordParseVersionTombstonesChunksTheRerunDropped(t *testing.T) {
	setupParseVersionDB(t)
	seedChunkBaseline(t, "chunk-kept")
	seedChunkBaseline(t, "chunk-dropped")

	// The re-parse produced only chunk-kept.
	recordParseVersion(t.Context(), dao.DB, "doc-1", "kb-1", "manual.pdf", []string{"chunk-kept"}, 0)

	version := latestParseVersion(t)
	states, err := chunkversion.StatesAt(t.Context(), dao.DB, "doc-1", version.Version)
	if err != nil {
		t.Fatalf("replay chunk history: %v", err)
	}
	// The replay reports the dropped chunk as a tombstone (Exists=false), which a
	// rebuild skips; what matters is that it never comes back with content.
	seen := make(map[string]bool, len(states))
	for _, state := range states {
		seen[state.ChunkID] = state.Exists
	}
	if !seen["chunk-kept"] {
		t.Fatalf("replay at the re-parse version = %+v, want chunk-kept present", states)
	}
	if seen["chunk-dropped"] {
		t.Fatalf("replay at the re-parse version = %+v, want chunk-dropped absent", states)
	}

	// The history before the re-parse still describes both, so restoring an older
	// version keeps working.
	openIDs, err := chunkversion.OpenChunkIDs(t.Context(), dao.DB, "doc-1")
	if err != nil {
		t.Fatalf("open chunk ids: %v", err)
	}
	if len(openIDs) != 1 || openIDs[0] != "chunk-kept" {
		t.Fatalf("open chunks after the re-parse = %v, want only chunk-kept", openIDs)
	}
}

// TestRecordParseVersionEmptiedDocument covers a run that produced no chunks at
// all: it still cleared the document's chunk set, so it needs a version and
// tombstones for every chunk the history still claims.
func TestRecordParseVersionEmptiedDocument(t *testing.T) {
	setupParseVersionDB(t)
	seedChunkBaseline(t, "chunk-1")
	seedChunkBaseline(t, "chunk-2")

	recordParseVersion(t.Context(), dao.DB, "doc-1", "kb-1", "manual.pdf", nil, 0)

	version := latestParseVersion(t)
	states, err := chunkversion.StatesAt(t.Context(), dao.DB, "doc-1", version.Version)
	if err != nil {
		t.Fatalf("replay chunk history: %v", err)
	}
	for _, state := range states {
		if state.Exists {
			t.Fatalf("replay after an empty parse = %+v, want no chunk present", states)
		}
	}

	openIDs, err := chunkversion.OpenChunkIDs(t.Context(), dao.DB, "doc-1")
	if err != nil {
		t.Fatalf("open chunk ids: %v", err)
	}
	if len(openIDs) != 0 {
		t.Fatalf("open chunks after an empty parse = %v, want none", openIDs)
	}
}

// TestRecordParseVersionRecordsARunThatChangedNothing pins the rule the history
// follows: it records runs, not diffs. A parse that succeeds leaves a version
// even when the document already had no chunks — a user who parsed an empty file
// still finds the run in the version list, and every restore point it adds
// rebuilds the same (empty) content.
func TestRecordParseVersionRecordsARunThatChangedNothing(t *testing.T) {
	setupParseVersionDB(t)

	recordParseVersion(t.Context(), dao.DB, "doc-1", "kb-1", "manual.pdf", nil, 0)

	version := latestParseVersion(t)
	if version.Version <= 0 {
		t.Fatalf("version = %d, want a positive number", version.Version)
	}
	if changes := version.Changes; len(changes) != 1 ||
		changes[0].Entity != entity.DatasetVersionChangeEntityDocument ||
		changes[0].Action != entity.DatasetVersionChangeActionParse ||
		changes[0].DocID != "doc-1" {
		t.Fatalf("changes = %+v, want the parsed-document entry", changes)
	}

	// The run occupies one number and the dataset row carries it, so the next run
	// takes the following one.
	var kb entity.Knowledgebase
	if err := dao.DB.Where("id = ?", "kb-1").Take(&kb).Error; err != nil {
		t.Fatalf("load dataset: %v", err)
	}
	if kb.Version != version.Version {
		t.Fatalf("dataset version = %d, want the recorded %d", kb.Version, version.Version)
	}

	states, err := chunkversion.StatesAt(t.Context(), dao.DB, "doc-1", version.Version)
	if err != nil {
		t.Fatalf("replay chunk history: %v", err)
	}
	if len(states) != 0 {
		t.Fatalf("replay at the version = %+v, want no chunk", states)
	}
}

// TestRecordParseVersionKeepsTheNonSourceRowsTheRunWrote pins what the finalizer
// must treat as "kept": parent chunks and compiled products are written into
// the same document and recorded as version 0 exactly like a source chunk.
// Diffing against the source-only id set tombstoned them on every parse, and
// neither AppendParseBaseline (which never rewrites version 0) nor the version
// delete (which refuses a tombstone) could undo it, so a content restore would
// rebuild the document without its parents and products, forever.
func TestRecordParseVersionKeepsTheNonSourceRowsTheRunWrote(t *testing.T) {
	setupParseVersionDB(t)
	seedChunkBaseline(t, "source-1")
	seedChunkBaseline(t, "parent-1")
	seedChunkBaseline(t, "wiki-1")

	// The written set the executor now passes: source chunk, parent row and the
	// compiler's product.
	recordParseVersion(t.Context(), dao.DB, "doc-1", "kb-1", "manual.pdf",
		[]string{"source-1", "parent-1", "wiki-1"}, 0)

	version := latestParseVersion(t)
	states, err := chunkversion.StatesAt(t.Context(), dao.DB, "doc-1", version.Version)
	if err != nil {
		t.Fatalf("replay chunk history: %v", err)
	}
	for _, state := range states {
		if !state.Exists {
			t.Fatalf("replay at the parse version = %+v, want every written row present", states)
		}
	}

	openIDs, err := chunkversion.OpenChunkIDs(t.Context(), dao.DB, "doc-1")
	if err != nil {
		t.Fatalf("open chunk ids: %v", err)
	}
	if len(openIDs) != 3 {
		t.Fatalf("open chunks after the parse = %v, want all three written rows", openIDs)
	}
}

// TestRecordParseVersionPinsTheReservedAnchor covers the one-version-per-parse
// contract: the parser reserves one number for the baselines it writes batch by
// batch, and the version row that closes the run carries that same number.
// Taking the next one would advance the dataset twice per parse and leave the
// reserved number in the history as a gap nothing explains.
func TestRecordParseVersionPinsTheReservedAnchor(t *testing.T) {
	db := setupParseVersionDB(t)
	seedChunkBaseline(t, "chunk-kept")
	seedChunkBaseline(t, "chunk-dropped")

	anchor, err := datasetversion.ReserveVersion(t.Context(), db, "kb-1")
	if err != nil {
		t.Fatalf("ReserveVersion: %v", err)
	}
	if anchor <= 0 {
		t.Fatalf("reserved version = %d, want a positive number", anchor)
	}
	// The re-parse wrote chunk-kept at the number it reserved.
	if err := chunkversion.AppendParseBaseline(t.Context(), db, "kb-1", anchor, chunkversion.SnapshotMeta{}, []map[string]interface{}{{
		"id":                  "chunk-kept",
		"doc_id":              "doc-1",
		"kb_id":               "kb-1",
		"content_with_weight": "kept body",
	}}); err != nil {
		t.Fatalf("append baseline: %v", err)
	}

	recordParseVersion(t.Context(), db, "doc-1", "kb-1", "manual.pdf", []string{"chunk-kept"}, anchor)

	version := latestParseVersion(t)
	if version.Version != anchor {
		t.Fatalf("parse version = %d, want the reserved anchor %d", version.Version, anchor)
	}
	var kb entity.Knowledgebase
	if err := db.Where("id = ?", "kb-1").Take(&kb).Error; err != nil {
		t.Fatalf("load dataset: %v", err)
	}
	if kb.Version != anchor {
		t.Fatalf("dataset version = %d, want %d: one parse advances one version", kb.Version, anchor)
	}

	states, err := chunkversion.StatesAt(t.Context(), db, "doc-1", version.Version)
	if err != nil {
		t.Fatalf("replay chunk history: %v", err)
	}
	seen := make(map[string]bool, len(states))
	for _, state := range states {
		seen[state.ChunkID] = state.Exists
	}
	if !seen["chunk-kept"] {
		t.Fatalf("replay at the pinned version = %+v, want chunk-kept present", states)
	}
	if seen["chunk-dropped"] {
		t.Fatalf("replay at the pinned version = %+v, want chunk-dropped absent", states)
	}

	// The number taken from the dataset row is the number the row now carries, so
	// no version was left reserved and unused.
	var count int64
	if err := db.Model(&entity.DatasetVersion{}).
		Where("dataset_id = ? AND version = ?", "kb-1", anchor+1).
		Count(&count).Error; err != nil {
		t.Fatalf("count versions: %v", err)
	}
	if count != 0 {
		t.Fatalf("version %d exists, want the parse to close at %d alone", anchor+1, anchor)
	}
}

// TestRecordParseVersionLandsUnderAConcurrentVersion covers the case a re-parse
// hits on a busy dataset: the run reserved its anchor before it started writing
// chunks, and another writer took a later number while it indexed. The run still
// closes on the number it reserved - that number is what its chunk rows are
// anchored at - instead of failing a parse whose chunks are already indexed.
func TestRecordParseVersionLandsUnderAConcurrentVersion(t *testing.T) {
	db := setupParseVersionDB(t)
	seedChunkBaseline(t, "chunk-kept")

	anchor, err := datasetversion.ReserveVersion(t.Context(), db, "kb-1")
	if err != nil {
		t.Fatalf("reserve the parse anchor: %v", err)
	}
	// The window between the reservation and the close: a chunk edit elsewhere in
	// the dataset records a version on a later number.
	if _, err := datasetversion.RecordContentVersionChangesOwnTx(t.Context(), "kb-1",
		entity.DatasetVersionKindChunk, nil); err != nil {
		t.Fatalf("record the concurrent version: %v", err)
	}

	dropped, err := recordParseVersion(t.Context(), db, "doc-1", "kb-1", "manual.pdf",
		[]string{"chunk-kept"}, anchor)
	if err != nil {
		t.Fatalf("record the parse version after a later one: %v", err)
	}
	if len(dropped) != 0 {
		t.Fatalf("dropped = %v, want none: the run kept its chunk", dropped)
	}

	version := latestParseVersion(t)
	if version.Version != anchor {
		t.Fatalf("parse version = %d, want the reserved %d", version.Version, anchor)
	}
	// The column keeps pointing at the newer number: it is what the next
	// allocation is taken past.
	var kb entity.Knowledgebase
	if err := db.Where("id = ?", "kb-1").Take(&kb).Error; err != nil {
		t.Fatalf("load dataset: %v", err)
	}
	if kb.Version <= anchor {
		t.Fatalf("dataset version = %d, want it left past the pinned %d", kb.Version, anchor)
	}

	// The parse's own chunk rows still replay at its number.
	states, err := chunkversion.StatesAt(t.Context(), db, "doc-1", version.Version)
	if err != nil {
		t.Fatalf("replay chunk history: %v", err)
	}
	seen := make(map[string]bool, len(states))
	for _, state := range states {
		seen[state.ChunkID] = state.Exists
	}
	if !seen["chunk-kept"] {
		t.Fatalf("replay at the pinned version = %+v, want chunk-kept present", states)
	}
}

// TestRecordParseVersionSkipsWithoutADataset keeps the best-effort writer quiet on
// the debug and engine-less paths, where there is no dataset to version.
func TestRecordParseVersionSkipsWithoutADataset(t *testing.T) {
	setupParseVersionDB(t)

	recordParseVersion(t.Context(), dao.DB, "doc-1", "", "manual.pdf", []string{"chunk-1"}, 0)

	var count int64
	if err := dao.DB.Model(&entity.DatasetVersion{}).Count(&count).Error; err != nil {
		t.Fatalf("count versions: %v", err)
	}
	if count != 0 {
		t.Fatalf("versions = %d, want none without a dataset", count)
	}
}
