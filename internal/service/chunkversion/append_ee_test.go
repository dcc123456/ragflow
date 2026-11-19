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

package chunkversion

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"ragflow/internal/dao"
	"ragflow/internal/engine/history"
	"ragflow/internal/engine/history/historytest"
	"ragflow/internal/entity"
)

// setupChunkHistoryDB opens an in-memory database holding the chunk history,
// which is all a baseline write touches.
func setupChunkHistoryDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&entity.ChunkVersion{}); err != nil {
		t.Fatalf("auto-migrate sqlite: %v", err)
	}
	// The version bodies live in the document engine, so the tests need one
	// installed before a baseline can be written at all.
	history.SetChunkStore(historytest.NewChunkStore())
	t.Cleanup(func() { history.SetChunkStore(nil) })
	return db
}

// TestAppendAddBaselineStartsTheTimeline covers the guarantee a restore needs:
// a chunk an editor added by hand must have a row at the moment it appeared, or
// replaying a version that already held it rebuilds the document without it.
func TestAppendAddBaselineStartsTheTimeline(t *testing.T) {
	db := setupChunkHistoryDB(t)

	AppendAddBaseline(t.Context(), db, "kb-1", "doc-1", "chunk-1", 1, SnapshotMeta{EmbeddingID: "embd-1", Language: "en"}, map[string]interface{}{
		"id":                  "chunk-1",
		"doc_id":              "doc-1",
		"content_with_weight": "added by hand",
		"important_kwd":       []string{"alpha"},
	})

	var rows []entity.ChunkVersion
	if err := db.Order("version").Find(&rows).Error; err != nil {
		t.Fatalf("load the history: %v", err)
	}
	// The row carries only what locates the state; the state itself comes back
	// from the document engine.
	hydrated := make([]*entity.ChunkVersion, 0, len(rows))
	for i := range rows {
		hydrated = append(hydrated, &rows[i])
	}
	if err := history.HydrateChunks(t.Context(), hydrated); err != nil {
		t.Fatalf("hydrate the history: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("history rows = %d, want 1", len(rows))
	}
	if rows[0].Version != 0 || rows[0].Kind != entity.ChunkVersionKindAdd {
		t.Fatalf("row = v%d/%s, want v0/add", rows[0].Version, rows[0].Kind)
	}
	if got, _ := rows[0].Snapshot["content_with_weight"].(string); got != "added by hand" {
		t.Fatalf("snapshot content = %q, want the chunk's text", got)
	}

	ids, err := OpenChunkIDs(t.Context(), db, "doc-1")
	if err != nil {
		t.Fatalf("open chunk ids: %v", err)
	}
	if len(ids) != 1 || ids[0] != "chunk-1" {
		t.Fatalf("open chunks = %v, want chunk-1", ids)
	}
}

// TestAppendAddBaselineReopensARemovedChunk covers the id coming back after a
// removal: the baseline is appended past the tombstone, so the timeline reads
// "it exists again" instead of ending at the delete row.
func TestAppendAddBaselineReopensARemovedChunk(t *testing.T) {
	db := setupChunkHistoryDB(t)

	if err := db.Create(&entity.ChunkVersion{
		ChunkID: "chunk-1", Version: 0, DocID: "doc-1", DatasetID: "kb-1", DatasetVersion: 1,
		Kind: entity.ChunkVersionKindDelete, Snapshot: entity.JSONMap{},
	}).Error; err != nil {
		t.Fatalf("seed the tombstone: %v", err)
	}

	AppendAddBaseline(t.Context(), db, "kb-1", "doc-1", "chunk-1", 1, SnapshotMeta{EmbeddingID: "embd-1", Language: "en"}, map[string]interface{}{
		"id": "chunk-1", "doc_id": "doc-1", "content_with_weight": "back again",
	})

	latest, err := dao.NewChunkVersionDAO().GetLatest(t.Context(), db, "chunk-1")
	if err != nil {
		t.Fatalf("load the latest row: %v", err)
	}
	if latest.Version != 1 || latest.Kind != entity.ChunkVersionKindAdd {
		t.Fatalf("latest = v%d/%s, want v1/add", latest.Version, latest.Kind)
	}

	ids, err := OpenChunkIDs(t.Context(), db, "doc-1")
	if err != nil {
		t.Fatalf("open chunk ids: %v", err)
	}
	if len(ids) != 1 || ids[0] != "chunk-1" {
		t.Fatalf("open chunks = %v, want the chunk back", ids)
	}
}

// TestAppendParseBaselineRevivesATombstonedChunk covers the chunk id a re-parse
// brings back: the first parse left version 0 behind and a delete tombstoned the
// chunk, so writing version 0 again would be a no-op that leaves the tombstone as
// the newest row - the history would call a live chunk removed, and a content
// restore would drop it.
func TestAppendParseBaselineRevivesATombstonedChunk(t *testing.T) {
	db := setupChunkHistoryDB(t)

	for _, row := range []*entity.ChunkVersion{
		{ChunkID: "chunk-1", Version: 0, DocID: "doc-1", DatasetID: "kb-1", DatasetVersion: 1,
			Kind: entity.ChunkVersionKindParse, Snapshot: entity.JSONMap{"content_with_weight": "old"}},
		{ChunkID: "chunk-1", Version: 1, DocID: "doc-1", DatasetID: "kb-1", DatasetVersion: 1,
			Kind: entity.ChunkVersionKindDelete, Snapshot: entity.JSONMap{}},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seed the history: %v", err)
		}
	}

	if err := AppendParseBaseline(t.Context(), db, "kb-1", 1, SnapshotMeta{}, []map[string]interface{}{{
		"id": "chunk-1", "doc_id": "doc-1", "content_with_weight": "back again",
	}}); err != nil {
		t.Fatalf("AppendParseBaseline() error = %v", err)
	}

	latest, err := dao.NewChunkVersionDAO().GetLatest(t.Context(), db, "chunk-1")
	if err != nil {
		t.Fatalf("load the latest row: %v", err)
	}
	if latest.Version != 2 || latest.Kind != entity.ChunkVersionKindParse {
		t.Fatalf("latest = v%d/%s, want v2/parse", latest.Version, latest.Kind)
	}
	ids, err := OpenChunkIDs(t.Context(), db, "doc-1")
	if err != nil {
		t.Fatalf("open chunk ids: %v", err)
	}
	if len(ids) != 1 || ids[0] != "chunk-1" {
		t.Fatalf("open chunks = %v, want the chunk back", ids)
	}

	// A chunk whose newest row is a content version keeps starting at 0: its
	// timeline already says it exists.
	if err := AppendParseBaseline(t.Context(), db, "kb-1", 1, SnapshotMeta{}, []map[string]interface{}{{
		"id": "chunk-2", "doc_id": "doc-1", "content_with_weight": "fresh",
	}}); err != nil {
		t.Fatalf("AppendParseBaseline() error = %v", err)
	}
	fresh, err := dao.NewChunkVersionDAO().GetLatest(t.Context(), db, "chunk-2")
	if err != nil {
		t.Fatalf("load the fresh baseline: %v", err)
	}
	if fresh.Version != 0 || fresh.Kind != entity.ChunkVersionKindParse {
		t.Fatalf("fresh = v%d/%s, want v0/parse", fresh.Version, fresh.Kind)
	}
}

// TestAppendTombstonesWritesTheWholeSet covers the batched form: one row per chunk,
// past the newest version it has, and version 0 for a chunk the history never saw.
// Duplicates in the input collapse, so a caller can pass the same id twice.
func TestAppendTombstonesWritesTheWholeSet(t *testing.T) {
	db := setupChunkHistoryDB(t)

	if err := db.Create(&entity.ChunkVersion{
		ChunkID: "chunk-1", Version: 3, DocID: "doc-1", DatasetID: "kb-1", DatasetVersion: 1,
		Kind: entity.ChunkVersionKindParse, Snapshot: entity.JSONMap{"content_with_weight": "text"},
	}).Error; err != nil {
		t.Fatalf("seed the history: %v", err)
	}

	AppendTombstones(t.Context(), db, "kb-1", "doc-1", 1, []string{"chunk-1", "chunk-2", "chunk-2", ""}, "user-1")

	var rows []entity.ChunkVersion
	if err := db.Where("doc_id = ?", "doc-1").Order("chunk_id, version").Find(&rows).Error; err != nil {
		t.Fatalf("load the history: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("history rows = %d, want 3 (chunk-1 v3 and one tombstone per chunk)", len(rows))
	}
	if rows[1].ChunkID != "chunk-1" || rows[1].Version != 4 || rows[1].Kind != entity.ChunkVersionKindDelete {
		t.Fatalf("chunk-1 latest = v%d/%s, want v4/delete", rows[1].Version, rows[1].Kind)
	}
	if rows[2].ChunkID != "chunk-2" || rows[2].Version != 0 || rows[2].Kind != entity.ChunkVersionKindDelete {
		t.Fatalf("chunk-2 = v%d/%s, want v0/delete", rows[2].Version, rows[2].Kind)
	}
	// A removal someone asked for names them, the way an edit does.
	for _, row := range rows[1:] {
		if row.CreatedBy != "user-1" {
			t.Fatalf("tombstone of %s recorded creator %q, want the caller", row.ChunkID, row.CreatedBy)
		}
	}
	ids, err := OpenChunkIDs(t.Context(), db, "doc-1")
	if err != nil {
		t.Fatalf("open chunk ids: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("open chunks = %v, want none after the tombstones", ids)
	}
}

// TestAppendAddBaselineIsIdempotent keeps a repeated add of the same content
// (the chunk id is derived from the text) from accumulating identical rows.
func TestAppendAddBaselineIsIdempotent(t *testing.T) {
	db := setupChunkHistoryDB(t)

	row := map[string]interface{}{"id": "chunk-1", "doc_id": "doc-1", "content_with_weight": "same text"}
	AppendAddBaseline(t.Context(), db, "kb-1", "doc-1", "chunk-1", 1, SnapshotMeta{}, row)
	AppendAddBaseline(t.Context(), db, "kb-1", "doc-1", "chunk-1", 1, SnapshotMeta{}, row)

	var count int64
	if err := db.Model(&entity.ChunkVersion{}).Where("chunk_id = ?", "chunk-1").Count(&count).Error; err != nil {
		t.Fatalf("count the history: %v", err)
	}
	if count != 1 {
		t.Fatalf("history rows = %d, want 1", count)
	}
}

// TestStatesAtOrdersTwoRowsOfOneMillisecond pins the reason the anchor is a
// version number instead of a timestamp: two edits of one chunk can land in the
// same millisecond, and then a clock ranks them the same. The rows carry the
// dataset version that wrote them, so a replay at the earlier version reads the
// earlier edit and one at the later version reads the later edit.
func TestStatesAtOrdersTwoRowsOfOneMillisecond(t *testing.T) {
	db := setupChunkHistoryDB(t)

	const at = int64(1_700_000_000_000)
	millis := func(value int64) *int64 { return &value }
	for _, row := range []*entity.ChunkVersion{
		{ChunkID: "chunk-1", Version: 0, Kind: entity.ChunkVersionKindParse, DocID: "doc-1",
			DatasetID: "kb-1", DatasetVersion: 1,
			Snapshot: entity.JSONMap{"content_with_weight": "parsed"}, CreateTime: millis(at - 1000)},
		// Two edits of the same chunk in one millisecond: same clock reading,
		// different version numbers.
		{ChunkID: "chunk-1", Version: 1, Kind: entity.ChunkVersionKindEdit, DocID: "doc-1",
			DatasetID: "kb-1", DatasetVersion: 2,
			Snapshot: entity.JSONMap{"content_with_weight": "first edit"}, CreateTime: millis(at)},
		{ChunkID: "chunk-1", Version: 2, Kind: entity.ChunkVersionKindEdit, DocID: "doc-1",
			DatasetID: "kb-1", DatasetVersion: 3,
			Snapshot: entity.JSONMap{"content_with_weight": "second edit"}, CreateTime: millis(at)},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seed chunk version v%d: %v", row.Version, err)
		}
	}

	first, err := StatesAt(t.Context(), db, "doc-1", 2)
	if err != nil {
		t.Fatalf("StatesAt(2) error = %v", err)
	}
	if len(first) != 1 || !first[0].Exists {
		t.Fatalf("states at version 2 = %+v, want the single open chunk", first)
	}
	if first[0].Version != 1 || first[0].Snapshot["content_with_weight"] != "first edit" {
		t.Fatalf("replayed at version 2 = v%d %v, want the first edit",
			first[0].Version, first[0].Snapshot["content_with_weight"])
	}

	second, err := StatesAt(t.Context(), db, "doc-1", 3)
	if err != nil {
		t.Fatalf("StatesAt(3) error = %v", err)
	}
	if len(second) != 1 || !second[0].Exists {
		t.Fatalf("states at version 3 = %+v, want the single open chunk", second)
	}
	if second[0].Version != 2 || second[0].Snapshot["content_with_weight"] != "second edit" {
		t.Fatalf("replayed at version 3 = v%d %v, want the second edit",
			second[0].Version, second[0].Snapshot["content_with_weight"])
	}
}

// TestParseBaselineKeepsItsBodyOutOfTheDatabase is the boundary a reviewer asked
// for: the row keeps what locates the state, the document engine keeps the
// state. A replay must not be able to tell the difference.
func TestParseBaselineKeepsItsBodyOutOfTheDatabase(t *testing.T) {
	db := setupChunkHistoryDB(t)
	store := historytest.NewChunkStore()
	history.SetChunkStore(store)
	t.Cleanup(func() { history.SetChunkStore(nil) })

	ctx := t.Context()
	if err := AppendParseBaseline(ctx, db, "kb-1", 1, SnapshotMeta{}, []map[string]interface{}{
		{"id": "chunk-1", "doc_id": "doc-1", "content_with_weight": "曹操，字孟德", "important_kwd": []string{"alpha"}},
		{"id": "chunk-2", "doc_id": "doc-1", "content_with_weight": "刘备，字玄德", "important_kwd": []string{"beta"}},
	}); err != nil {
		t.Fatalf("AppendParseBaseline: %v", err)
	}

	var rows []*entity.ChunkVersion
	if err := db.Find(&rows).Error; err != nil {
		t.Fatalf("read the rows back: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("recorded %d rows, want 2", len(rows))
	}
	for _, row := range rows {
		if len(row.Snapshot) != 0 {
			t.Fatalf("%s still stores its body in the row: %+v", row.ChunkID, row.Snapshot)
		}
	}
	// The bodies are addressed by the pair each row is keyed by, one document
	// per version, so a parse of two chunks writes two of them.
	if ids := store.DocumentIDs("kb-1"); len(ids) != 2 {
		t.Fatalf("the engine holds %v, want one document per version", ids)
	}

	// A replay reads the state back out of the engine.
	states, err := StatesAt(ctx, db, "doc-1", time.Now().UnixMilli()+1000)
	if err != nil {
		t.Fatalf("StatesAt: %v", err)
	}
	bodies := make(map[string]string, len(states))
	for _, state := range states {
		text, _ := state.Snapshot["content_with_weight"].(string)
		bodies[state.ChunkID] = text
	}
	if bodies["chunk-1"] != "曹操，字孟德" || bodies["chunk-2"] != "刘备，字玄德" {
		t.Fatalf("replayed bodies = %+v", bodies)
	}
}

// TestAppendParseBaselineStoresTheWholeRow pins the baseline format: version 0 is
// the row the parser wrote, tokens and vector included, so rewinding to it costs
// no tokenizer run and no embedding call and reproduces exactly what ranked then.
func TestAppendParseBaselineStoresTheWholeRow(t *testing.T) {
	db := setupChunkHistoryDB(t)

	if err := AppendParseBaseline(t.Context(), db, "kb-1", 1,
		SnapshotMeta{EmbeddingID: "embd-1", Language: "en"},
		[]map[string]interface{}{{
			"id":                  "chunk-1",
			"doc_id":              "doc-1",
			"kb_id":               "kb-1",
			"docnm_kwd":           "manual.pdf",
			"content_with_weight": "the body",
			"content_ltks":        "the body",
			"content_sm_ltks":     "the body",
			"important_kwd":       []string{"kw"},
			"important_tks":       "kw",
			"available_int":       1,
			"chunk_order_int":     2,
			"q_1024_vec":          []float32{0.5, -0.25},
		}}); err != nil {
		t.Fatalf("AppendParseBaseline() error = %v", err)
	}

	var row entity.ChunkVersion
	if err := db.Where("chunk_id = ? AND version = 0", "chunk-1").Take(&row).Error; err != nil {
		t.Fatalf("load the baseline row: %v", err)
	}
	if err := history.HydrateChunks(t.Context(), []*entity.ChunkVersion{&row}); err != nil {
		t.Fatalf("hydrate the baseline: %v", err)
	}
	stored, meta, ok := FullSnapshotRow(row.Snapshot)
	if !ok {
		t.Fatalf("FullSnapshotRow(baseline) = not ok, want a copyable row: %#v", row.Snapshot)
	}
	if meta.EmbeddingID != "embd-1" || meta.Language != "en" || meta.VectorField != "q_1024_vec" {
		t.Fatalf("meta = %#v, want the producer of the derived fields", meta)
	}
	// The identity of the row is not part of the snapshot: a restore stamps the
	// live document's, so that a body is never replayed under another name.
	for _, field := range []string{"id", "doc_id", "kb_id", "docnm_kwd"} {
		if _, leaked := stored[field]; leaked {
			t.Fatalf("snapshot = %#v, want %s left out", stored, field)
		}
	}
	if stored["content_ltks"] != "the body" {
		t.Fatalf("content_ltks = %v, want the parser's tokens", stored["content_ltks"])
	}
	if vector, ok := SnapshotVector(stored["q_1024_vec"]); !ok || len(vector) != 2 {
		t.Fatalf("vector = %#v, want the parser's vector", stored["q_1024_vec"])
	}
}

// TestEmptyParseBaselineKeepsTheDocumentRecoverable: the marker makes the
// document recoverable while replaying as no chunks.
func TestEmptyParseBaselineKeepsTheDocumentRecoverable(t *testing.T) {
	db := setupChunkHistoryDB(t)

	if err := AppendEmptyParseBaseline(t.Context(), db, "kb-1", 3, "doc-1"); err != nil {
		t.Fatalf("AppendEmptyParseBaseline() error = %v", err)
	}

	known, err := HasAnyHistory(t.Context(), db, "doc-1")
	if err != nil {
		t.Fatalf("HasAnyHistory() error = %v", err)
	}
	if !known {
		t.Fatal("HasAnyHistory() = false, want the empty document to look recoverable")
	}
	marked, err := HasEmptyParseMarker(t.Context(), db, "doc-1")
	if err != nil {
		t.Fatalf("HasEmptyParseMarker() error = %v", err)
	}
	if !marked {
		t.Fatal("HasEmptyParseMarker() = false, want the parse that produced no chunks")
	}
	// A document nobody parsed has no marker, even though it has no history
	// either: an upload must keep reading as unparsed after a restore.
	marked, err = HasEmptyParseMarker(t.Context(), db, "doc-never-parsed")
	if err != nil {
		t.Fatalf("HasEmptyParseMarker() error = %v", err)
	}
	if marked {
		t.Fatal("HasEmptyParseMarker(never parsed) = true, want false")
	}

	states, err := StatesAt(t.Context(), db, "doc-1", 3)
	if err != nil {
		t.Fatalf("StatesAt() error = %v", err)
	}
	if len(states) != 0 {
		t.Fatalf("StatesAt() = %#v, want the marker to address no chunk", states)
	}

	open, err := OpenChunkIDs(t.Context(), db, "doc-1")
	if err != nil {
		t.Fatalf("OpenChunkIDs() error = %v", err)
	}
	if len(open) != 0 {
		t.Fatalf("OpenChunkIDs() = %v, want the marker left out", open)
	}

	// A re-parse that also produces nothing keeps the one marker it has.
	if err := AppendEmptyParseBaseline(t.Context(), db, "kb-1", 4, "doc-1"); err != nil {
		t.Fatalf("AppendEmptyParseBaseline() again error = %v", err)
	}
	var count int64
	if err := db.Model(&entity.ChunkVersion{}).
		Where("doc_id = ? AND chunk_id = ?", "doc-1", entity.ChunkVersionEmptyDocumentID).
		Count(&count).Error; err != nil {
		t.Fatalf("count markers: %v", err)
	}
	if count != 1 {
		t.Fatalf("markers = %d, want one", count)
	}
}
