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

package datasetcontent

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/engine/history"
	"ragflow/internal/engine/history/historytest"
	"ragflow/internal/entity"
)

// TestRecordedDocumentIDsRefusesUnrecordedDocuments pins the difference the
// column carries: NULL is "not recorded" and must never be read as "no
// documents", because the restore would then remove everything the dataset
// holds.
func TestRecordedDocumentIDsRefusesUnrecordedDocuments(t *testing.T) {
	ids, err := recordedDocumentIDs(&entity.DatasetVersion{Version: 7})
	if err == nil {
		t.Fatalf("ids = %v, want the restore refused", ids)
	}
	var coded codedError
	if !errors.As(err, &coded) {
		t.Fatalf("error = %v, want a coded error the handler can map", err)
	}
	if coded.Code() != common.CodeConflict {
		t.Fatalf("code = %v, want %v", coded.Code(), common.CodeConflict)
	}
	if !strings.Contains(coded.Error(), "7") {
		t.Fatalf("message = %q, want the version named", coded.Error())
	}
}

// TestRecordedDocumentIDsReadsTheRecordedShape keeps the recorded id list
// working, including an id that names no document, and reads an empty list as
// a real empty set rather than a missing one.
func TestRecordedDocumentIDsReadsTheRecordedShape(t *testing.T) {
	recorded := `["doc-1","doc-2",""]`
	ids, err := recordedDocumentIDs(&entity.DatasetVersion{Version: 3, Documents: recorded})
	if err != nil {
		t.Fatalf("recorded document set: %v", err)
	}
	if len(ids) != 2 || ids[0] != "doc-1" || ids[1] != "doc-2" {
		t.Fatalf("ids = %v, want [doc-1 doc-2] with the empty id dropped", ids)
	}

	// A version that recorded an empty dataset is a real empty set, not a
	// missing one, and restores to an empty dataset.
	ids, err = recordedDocumentIDs(&entity.DatasetVersion{Version: 2, Documents: `[]`})
	if err != nil || len(ids) != 0 {
		t.Fatalf("empty document set = (%v, %v), want no documents and no error", ids, err)
	}

	// The column is written only as an id list; anything else is reported
	// instead of guessed at - including the entry list an earlier build wrote.
	for _, broken := range []string{
		"{",
		`[{"id":"doc-1","name":"manual.pdf"}]`,
	} {
		if _, err := recordedDocumentIDs(&entity.DatasetVersion{Version: 4, Documents: broken}); err == nil {
			t.Fatalf("document set %q decoded without an error", broken)
		}
	}
}

// TestDiffDocumentSets pins the two directions a content restore moves in: a
// document recorded by the version but missing now is put back, and a document
// added after the version is removed. Order follows the input, so a retry
// processes the same documents in the same order.
func TestDiffDocumentSets(t *testing.T) {
	documentSet := []string{"a", "b", "c"}
	current := []string{"a", "c", "d", "e"}

	toRestore, toDelete := diffDocumentSets(documentSet, current)

	if len(toRestore) != 1 || toRestore[0] != "b" {
		t.Fatalf("toRestore = %v, want [b]", toRestore)
	}
	if len(toDelete) != 2 || toDelete[0] != "d" || toDelete[1] != "e" {
		t.Fatalf("toDelete = %v, want [d e]", toDelete)
	}
}

// TestRenamedNamesAtReplaysRenamesUpToTheVersion pins what a document's name
// reads at a version: the last rename recorded at or before it. A rename made
// after it is exactly the edit the restore removes, so it must not be read; a
// rename of another document says nothing about this one.
//
// The window is the version number, which is why version 7 is excluded from
// version 6 even though both were stamped in the same millisecond: the clock
// cannot separate them, the number can.
func TestRenamedNamesAtReplaysRenamesUpToTheVersion(t *testing.T) {
	db := setupRenameTestDB(t)

	renames := []struct {
		version int64
		at      int64
		docID   string
		name    string
	}{
		{5, 500, "doc-1", "a.txt"},
		{6, 1500, "doc-1", "b.txt"},
		{7, 1500, "doc-1", "c.txt"}, // same millisecond as 6, later version
		{8, 1400, "doc-2", "x.txt"},
	}
	for _, rename := range renames {
		at := rename.at
		if err := db.Create(&entity.DatasetVersion{
			DatasetID: "kb-1", Version: rename.version,
			Kind: entity.DatasetVersionKindDocumentRename, CreatedBy: "user-1",
			CreateTime: &at,
			Changes: entity.DatasetVersionChanges{{
				DocID:  rename.docID,
				Entity: entity.DatasetVersionChangeEntityDocument,
				Action: entity.DatasetVersionChangeActionRename,
				Name:   rename.name,
			}},
		}).Error; err != nil {
			t.Fatalf("seed rename %d: %v", rename.version, err)
		}
	}
	// A change of another kind says nothing about names, however recent it is.
	parseAt := int64(1900)
	if err := db.Create(&entity.DatasetVersion{
		DatasetID: "kb-1", Version: 9, Kind: entity.DatasetVersionKindParse,
		CreatedBy: "user-1", CreateTime: &parseAt,
		Changes: entity.DatasetVersionChanges{{
			DocID: "doc-1", Entity: entity.DatasetVersionChangeEntityDocument,
			Action: entity.DatasetVersionChangeActionParse, Name: "ignored.txt",
		}},
	}).Error; err != nil {
		t.Fatalf("seed parse version: %v", err)
	}

	// At version 6, the later rename of the same millisecond is not yet in
	// effect, and doc-2 has not been renamed at all.
	names, err := renamedNamesAt(t.Context(), "kb-1", 6)
	if err != nil {
		t.Fatalf("renamed names at version 6: %v", err)
	}
	if len(names) != 1 || names["doc-1"] != "b.txt" {
		t.Fatalf("names at version 6 = %v, want doc-1 b.txt", names)
	}

	// At version 8 both renames are in effect.
	names, err = renamedNamesAt(t.Context(), "kb-1", 8)
	if err != nil {
		t.Fatalf("renamed names at version 8: %v", err)
	}
	if len(names) != 2 || names["doc-1"] != "c.txt" || names["doc-2"] != "x.txt" {
		t.Fatalf("names at version 8 = %v, want doc-1 c.txt and doc-2 x.txt", names)
	}

	// A dataset with no rename in its history answers with nothing, which is what
	// makes the restore fall back to the document set's own snapshot.
	empty, err := renamedNamesAt(t.Context(), "kb-2", 8)
	if err != nil || len(empty) != 0 {
		t.Fatalf("names for an unrenamed dataset = (%v, %v), want none", empty, err)
	}
}

func TestDiffDocumentSetsIdentical(t *testing.T) {
	toRestore, toDelete := diffDocumentSets([]string{"a", "b"}, []string{"b", "a"})
	if len(toRestore) != 0 || len(toDelete) != 0 {
		t.Fatalf("identical sets produced restore=%v delete=%v", toRestore, toDelete)
	}
}

func TestDiffDocumentSetsEmptyDocumentSetRemovesEverything(t *testing.T) {
	// A version that recorded an empty dataset restores to an empty dataset. The
	// empty document set is only reachable when the column recorded a real empty list,
	// which is why a missing column is rejected before this point.
	toRestore, toDelete := diffDocumentSets([]string{}, []string{"a"})
	if len(toRestore) != 0 {
		t.Fatalf("toRestore = %v, want none", toRestore)
	}
	if len(toDelete) != 1 || toDelete[0] != "a" {
		t.Fatalf("toDelete = %v, want [a]", toDelete)
	}
}

// setupRenameTestDB opens an in-memory database holding the version table the
// rename replay reads, and points dao.DB at it for the test's lifetime.
func setupRenameTestDB(t *testing.T) *gorm.DB {
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
	if err := db.AutoMigrate(&entity.DatasetVersion{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	oldDB := dao.DB
	dao.DB = db
	t.Cleanup(func() { dao.DB = oldDB })
	return db
}

// setupContentTestDB opens an in-memory database holding the two tables the
// parsing gate reads, and points dao.DB at it for the test's lifetime.
func setupContentTestDB(t *testing.T) *gorm.DB {
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
	if err := db.AutoMigrate(&entity.Document{}, &entity.IngestionTask{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	oldDB := dao.DB
	dao.DB = db
	t.Cleanup(func() { dao.DB = oldDB })
	return db
}

// TestParsingDocumentsReasonBlocksAnInFlightRestore pins the gate: a restore is
// refused while a document is queued or parsing, because the run writes the
// chunks the restore would rebuild. The reason names the document so the reader
// knows what to stop, and a finished run stops blocking.
func TestParsingDocumentsReasonBlocksAnInFlightRestore(t *testing.T) {
	db := setupContentTestDB(t)

	if reason := parsingDocumentsReason(t.Context(), "kb-1"); reason != "" {
		t.Fatalf("idle dataset reason = %q, want none", reason)
	}

	name := "handbook.pdf"
	if err := db.Create(&entity.Document{
		ID: "doc-1", KbID: "kb-1", Name: &name, ParserConfig: entity.JSONMap{},
	}).Error; err != nil {
		t.Fatalf("seed document: %v", err)
	}
	if err := db.Create(&entity.IngestionTask{
		ID: "task-1", UserID: "user-1", DocumentID: "doc-1", DatasetID: "kb-1",
		Status: common.RUNNING,
	}).Error; err != nil {
		t.Fatalf("seed task: %v", err)
	}

	reason := parsingDocumentsReason(t.Context(), "kb-1")
	if !strings.Contains(reason, "handbook.pdf") {
		t.Fatalf("reason = %q, want the document named", reason)
	}
	if !strings.Contains(reason, "cannot restore") {
		t.Fatalf("reason = %q, want a refusal sentence", reason)
	}

	// A terminal run no longer blocks: the chunks it wrote are final.
	if err := db.Model(&entity.IngestionTask{}).Where("id = ?", "task-1").
		Update("status", common.COMPLETED).Error; err != nil {
		t.Fatalf("finish task: %v", err)
	}
	if reason := parsingDocumentsReason(t.Context(), "kb-1"); reason != "" {
		t.Fatalf("finished run reason = %q, want none", reason)
	}
}

// setupReplayTestDB opens a database with the chunk history table the replay
// reads, and points dao.DB at it.
func setupReplayTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+url.QueryEscape(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("failed to access sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	// The document table rides along because a replay reads a document's chunk
	// count to tell an upload that was never parsed from a history that is gone.
	if err := db.AutoMigrate(&entity.ChunkVersion{}, &entity.Document{}); err != nil {
		t.Fatalf("failed to migrate chunk version table: %v", err)
	}
	oldDB := dao.DB
	dao.DB = db
	t.Cleanup(func() { dao.DB = oldDB })

	// The snapshot a version row names lives in the document engine, so a replay
	// can only read what the in-memory stand-in holds.
	history.SetChunkStore(historytest.NewChunkStore())
	t.Cleanup(func() { history.SetChunkStore(nil) })
	return db
}

// TestReplayRecordedChunksReportsAFailedReplay pins the difference between a
// document that came back and one whose content did not: a failed replay names
// the document instead of leaving the restore claiming a success that lost it.
func TestReplayRecordedChunksReportsAFailedReplay(t *testing.T) {
	db := setupReplayTestDB(t)

	anchor := int64(2000)
	created := int64(1000)
	for _, docID := range []string{"doc-1", "doc-2"} {
		if err := db.Create(&entity.ChunkVersion{
			ChunkID: "chunk-" + docID, Version: 0, DocID: docID, DatasetID: "kb-1", DatasetVersion: 1,
			Kind:       entity.ChunkVersionKindParse,
			Snapshot:   entity.JSONMap{"content_with_weight": "text"},
			CreateTime: &created,
		}).Error; err != nil {
			t.Fatalf("seed chunk history of %s: %v", docID, err)
		}
	}

	var replayed []string
	replay := func(_ context.Context, _, documentID string, _, _ int64) (int, error) {
		replayed = append(replayed, documentID)
		if documentID == "doc-2" {
			return 0, errors.New("engine is down")
		}
		return 3, nil
	}

	restored, missing := replayRecordedChunks(
		t.Context(), "kb-1", []string{"doc-1", "doc-2"}, nil, anchor, 3000, replay)

	if restored != 3 {
		t.Fatalf("restored chunks = %d, want only the successful document", restored)
	}
	if len(missing.ids) != 1 || missing.ids[0] != "doc-2" {
		t.Fatalf("missing = %v, want [doc-2]", missing.ids)
	}
	if missing.reasons["doc-2"] != missingReasonReplayFailed {
		t.Fatalf("reason = %q, want %q", missing.reasons["doc-2"], missingReasonReplayFailed)
	}
	// A failure names that document and leaves the rest of the replay running.
	if len(replayed) != 2 {
		t.Fatalf("replayed = %v, want both documents attempted", replayed)
	}
}

// TestReplayRecordedChunksClearsAVersionThatHeldNoChunk covers the other empty
// state: the document does have history, all of it written after the anchor, so
// the version described it with no chunks - it was uploaded and not yet parsed.
// The replay runs and clears the engine, which is what that version says, instead
// of the document being reported as unpredictable.
func TestReplayRecordedChunksClearsAVersionThatHeldNoChunk(t *testing.T) {
	db := setupReplayTestDB(t)

	// The parse lands after the version being restored.
	created := int64(2000)
	if err := db.Create(&entity.ChunkVersion{
		ChunkID: "chunk-1", Version: 0, DocID: "doc-1", DatasetID: "kb-1", DatasetVersion: 2,
		Kind:       entity.ChunkVersionKindParse,
		Snapshot:   entity.JSONMap{"content_with_weight": "text"},
		CreateTime: &created,
	}).Error; err != nil {
		t.Fatalf("seed chunk history: %v", err)
	}

	var replayed []string
	replay := func(_ context.Context, _, documentID string, atVersion, _ int64) (int, error) {
		replayed = append(replayed, documentID)
		if atVersion != 1 {
			t.Fatalf("replay anchor = %d, want the restored version 1", atVersion)
		}
		return 0, nil
	}

	restored, missing := replayRecordedChunks(
		t.Context(), "kb-1", []string{"doc-1"}, nil, 1, 2, replay)

	if len(replayed) != 1 {
		t.Fatalf("replayed = %v, want the document cleared", replayed)
	}
	if restored != 0 {
		t.Fatalf("restored chunks = %d, want 0: the version held no chunk", restored)
	}
	if len(missing.ids) != 0 {
		t.Fatalf("missing = %v, want none: the version's state for this document is empty, not unknown", missing.ids)
	}
}

// TestReplayRecordedChunksSkipsADocumentThatWasNeverParsed pins the empty state an
// upload leaves: no chunk history and no chunks. Uploading creates no chunk, so a
// document in that shape was never parsed, and the version it is being restored to
// held no content for it either. Nothing is replayed, no history row is written,
// and the document is not reported as content that could not come back - the
// reader is not asked for a re-parse the version never had.
func TestReplayRecordedChunksSkipsADocumentThatWasNeverParsed(t *testing.T) {
	setupReplayTestDB(t)

	replayed := make([]string, 0)
	replay := func(_ context.Context, _, documentID string, _, _ int64) (int, error) {
		replayed = append(replayed, documentID)
		return 1, nil
	}

	restored, missing := replayRecordedChunks(
		t.Context(), "kb-1", []string{"doc-1"}, nil, 2000, 3000, replay)

	if len(replayed) != 0 {
		t.Fatalf("a document that was never parsed was replayed: %v", replayed)
	}
	if restored != 0 {
		t.Fatalf("restored chunks = %d, want 0", restored)
	}
	if len(missing.ids) != 0 {
		t.Fatalf("missing = %v, want none: the version held no chunk for it", missing.ids)
	}

	// A document that came back from its removal and holds no chunk is the same
	// empty state: there is nothing to put back, so it is not missing either.
	restored, missing = replayRecordedChunks(
		t.Context(), "kb-1", []string{"doc-2"}, map[string]struct{}{"doc-2": {}}, 2000, 3000, replay)

	if len(replayed) != 0 {
		t.Fatalf("a returned document that was never parsed was replayed: %v", replayed)
	}
	if restored != 0 {
		t.Fatalf("restored chunks = %d, want 0", restored)
	}
	if len(missing.ids) != 0 {
		t.Fatalf("missing = %v, want none", missing.ids)
	}
}

// TestReplayRecordedChunksReportsADocumentWhoseHistoryIsMissing keeps the other
// side of the rule: a document that holds chunks now but has no history at all is
// not an upload that was never parsed. Its history predates the feature or was
// purged, and clearing the engine would drop text nothing can rebuild, so the
// document is reported instead of emptied.
func TestReplayRecordedChunksReportsADocumentWhoseHistoryIsMissing(t *testing.T) {
	db := setupReplayTestDB(t)
	if err := db.Create(&entity.Document{
		ID:           "doc-1",
		KbID:         "kb-1",
		ParserID:     "naive",
		ParserConfig: entity.JSONMap{},
		Type:         "pdf",
		Suffix:       "pdf",
		CreatedBy:    "system",
		ChunkNum:     3,
	}).Error; err != nil {
		t.Fatalf("seed document: %v", err)
	}

	replayed := make([]string, 0)
	replay := func(_ context.Context, _, documentID string, _, _ int64) (int, error) {
		replayed = append(replayed, documentID)
		return 1, nil
	}

	restored, missing := replayRecordedChunks(
		t.Context(), "kb-1", []string{"doc-1"}, nil, 2000, 3000, replay)

	if len(replayed) != 0 {
		t.Fatalf("a document whose history is missing was replayed: %v", replayed)
	}
	if restored != 0 {
		t.Fatalf("restored chunks = %d, want 0", restored)
	}
	if len(missing.ids) != 1 || missing.ids[0] != "doc-1" {
		t.Fatalf("missing = %v, want [doc-1]", missing.ids)
	}
	if missing.reasons["doc-1"] != missingReasonNoChunkHistory {
		t.Fatalf("reason = %q, want %q", missing.reasons["doc-1"], missingReasonNoChunkHistory)
	}
}

// TestMissingSetKeepsOneEntryPerDocumentAndTheFirstReason covers the merge: a
// document whose row was already missing is still in recordedIDs, and must not
// be named twice in the response; the earlier reason is the one that survives.
func TestMissingSetKeepsOneEntryPerDocumentAndTheFirstReason(t *testing.T) {
	missing := newMissingSet()
	missing.add("doc-1", missingReasonRemovalGone)
	missing.add("doc-1", missingReasonNoChunkHistory)
	missing.merge(&missingSet{
		ids:     []string{"doc-1", "doc-2"},
		reasons: map[string]string{"doc-1": missingReasonReplayFailed, "doc-2": missingReasonNoChunkHistory},
	})

	if len(missing.ids) != 2 || missing.ids[0] != "doc-1" || missing.ids[1] != "doc-2" {
		t.Fatalf("missing = %v, want [doc-1 doc-2]", missing.ids)
	}
	if missing.reasons["doc-1"] != missingReasonRemovalGone {
		t.Fatalf("reason = %q, want the first one recorded", missing.reasons["doc-1"])
	}
	if missing.reasons["doc-2"] != missingReasonNoChunkHistory {
		t.Fatalf("reason = %q, want %q", missing.reasons["doc-2"], missingReasonNoChunkHistory)
	}
}

// TestReplayRecordedChunksSettlesEachDocumentFromOneRead keeps the probe a set:
// two documents in one restore, one parsed and one not, are answered from the same
// read and each lands on its own verdict - the parsed one reported, the unparsed
// one skipped.
func TestReplayRecordedChunksSettlesEachDocumentFromOneRead(t *testing.T) {
	db := setupReplayTestDB(t)
	if err := db.Create(&entity.Document{
		ID:           "doc-2",
		KbID:         "kb-1",
		ParserID:     "naive",
		ParserConfig: entity.JSONMap{},
		Type:         "pdf",
		Suffix:       "pdf",
		CreatedBy:    "system",
		ChunkNum:     4,
	}).Error; err != nil {
		t.Fatalf("seed document: %v", err)
	}

	replayed := make([]string, 0)
	replay := func(_ context.Context, _, documentID string, _, _ int64) (int, error) {
		replayed = append(replayed, documentID)
		return 1, nil
	}

	restored, missing := replayRecordedChunks(
		t.Context(), "kb-1", []string{"doc-1", "doc-2"}, nil, 2000, 3000, replay)

	if len(replayed) != 0 {
		t.Fatalf("a document without history was replayed: %v", replayed)
	}
	if restored != 0 {
		t.Fatalf("restored chunks = %d, want 0", restored)
	}
	if len(missing.ids) != 1 || missing.ids[0] != "doc-2" {
		t.Fatalf("missing = %v, want [doc-2]", missing.ids)
	}
	if missing.reasons["doc-2"] != missingReasonNoChunkHistory {
		t.Fatalf("reason = %q, want %q", missing.reasons["doc-2"], missingReasonNoChunkHistory)
	}
}
