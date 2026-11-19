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

package chunk

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/engine/history"
	"ragflow/internal/entity"
	"ragflow/internal/service"
	"ragflow/internal/service/chunkversion"
)

// replayTestEngine records the rows a replay writes: the restored order and
// content are only observable there.
type replayTestEngine struct {
	updateChunkTestEngine
	inserted    []map[string]interface{}
	deleteCalls int
	deleteErr   error
	insertErr   error
}

func (e *replayTestEngine) InsertChunks(_ context.Context, chunks []map[string]interface{}, _, _, _ string) ([]string, error) {
	if e.insertErr != nil {
		return nil, e.insertErr
	}
	e.inserted = append(e.inserted, chunks...)
	return nil, nil
}

func (e *replayTestEngine) DeleteChunks(context.Context, map[string]interface{}, string, string) (int64, error) {
	e.deleteCalls++
	if e.deleteErr != nil {
		return 0, e.deleteErr
	}
	return 0, nil
}

func newReplayTestService(engine *replayTestEngine) *ChunkService {
	return &ChunkService{
		docEngine:               engine,
		kbDAO:                   dao.NewKnowledgebaseDAO(),
		documentDAO:             dao.NewDocumentDAO(),
		tokenizeFunc:            func(text, _ string) (string, error) { return text, nil },
		fineGrainedTokenizeFunc: func(text, _ string) (string, error) { return text, nil },
		numTokensFunc:           func(string) int { return 1 },
	}
}

// insertRebuiltHistory writes one history row under an explicit dataset version.
// The replay anchors on that number, so a test has to place its rows on the
// version timeline instead of letting them land "now".
func insertRebuiltHistory(t *testing.T, chunkID, content string, version, order int, kind string, datasetVersion int64) {
	t.Helper()
	insertRebuiltHistorySnapshot(t, chunkID, entity.JSONMap{
		"content_with_weight": content,
		"img_id":              "",
		"chunk_order_int":     order,
	}, version, kind, datasetVersion)
}

// fullTestSnapshot turns the fields a test cares about into the shape every
// writer stores: those fields plus the derived ones a replay copies. A replay
// does not derive anything, so a row without them is not a version it can put
// back.
func fullTestSnapshot(fields entity.JSONMap) entity.JSONMap {
	row := make(entity.JSONMap, len(fields)+2)
	for key, value := range fields {
		row[key] = value
	}
	if _, ok := row["content_ltks"]; !ok {
		if content, _ := row["content_with_weight"].(string); content != "" {
			row["content_ltks"] = content
		}
	}
	if _, ok := row["q_3_vec"]; !ok {
		// Whole numbers: a snapshot's vector comes back as float64, and float32
		// fractions would not compare equal after the conversion.
		row["q_3_vec"] = []float32{1, 2, 3}
	}
	return chunkversion.FullSnapshot(row, chunkversion.SnapshotMeta{EmbeddingID: "embed-3"})
}

// insertRebuiltHistorySnapshot writes one history row with the snapshot the test
// needs, for the fields insertRebuiltHistory pins to a default.
func insertRebuiltHistorySnapshot(t *testing.T, chunkID string, snapshot entity.JSONMap, version int, kind string, datasetVersion int64) {
	t.Helper()
	if chunkversion.SnapshotMetaOf(snapshot).SchemaRevision < chunkversion.FullSnapshotSchemaRevision {
		snapshot = fullTestSnapshot(snapshot)
	}
	created := int64(1700000000000) + datasetVersion
	row := &entity.ChunkVersion{
		ChunkID:        chunkID,
		Version:        version,
		DocID:          "doc-a",
		DatasetID:      "kb-1",
		DatasetVersion: datasetVersion,
		Kind:           kind,
		Snapshot:       snapshot,
	}
	row.CreateTime = &created
	// The body goes to the document engine first, exactly as the writer does:
	// a row alone would replay as a version whose state nobody holds.
	if err := history.StashChunk(context.Background(), row); err != nil {
		t.Fatalf("stash chunk version %s v%d: %v", chunkID, version, err)
	}
	if err := dao.DB.Create(row).Error; err != nil {
		t.Fatalf("insert chunk version %s v%d: %v", chunkID, version, err)
	}
}

// TestReplayDocumentChunksRestoresOrderAndHistory pins the two things a
// document restore used to get wrong: the rebuilt chunks keep the reading order
// the history recorded instead of being renumbered in chunk-id order, and the
// chunk that was edited after the anchor ends up with a restore version, so the
// list badge matches the content it is shown beside.
func TestReplayDocumentChunksRestoresOrderAndHistory(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	// The parse that was recorded as dataset version 2 wrote three chunks in
	// reading order. The ids are picked so their alphabetical order (alpha, bravo,
	// charlie) is the reverse of the reading order (bravo, charlie, alpha): a
	// replay that numbered rows by replay position, which is how the states come
	// back, would hand out a different order and fail below.
	insertRebuiltHistory(t, "chunk-alpha", "third", 0, 2, entity.ChunkVersionKindParse, 2)
	insertRebuiltHistory(t, "chunk-bravo", "first", 0, 0, entity.ChunkVersionKindParse, 2)
	insertRebuiltHistory(t, "chunk-charlie", "second", 0, 1, entity.ChunkVersionKindParse, 2)
	// The edit recorded as version 3: it is what restoring version 2 has to undo.
	insertRebuiltHistory(t, "chunk-bravo", "first edited", 1, 0, entity.ChunkVersionKindEdit, 3)
	// A chunk the document only gained under version 4, after the anchor.
	insertRebuiltHistory(t, "chunk-delta", "fourth", 0, 3, entity.ChunkVersionKindParse, 4)

	engine := &replayTestEngine{}
	svc := newReplayTestService(engine)
	ctx := chunkTestContext(t, "user-1", "tenant-1")

	count, err := svc.ReplayDocumentChunksAtEE(ctx, "kb-1", "doc-a", 2, 5)
	if err != nil {
		t.Fatalf("ReplayDocumentChunksAtEE error = %v", err)
	}
	if count != 3 {
		t.Fatalf("rebuilt chunks = %d, want 3 (the chunk added after the anchor is not part of that version)", count)
	}

	want := map[string]struct {
		content string
		order   int
	}{
		"chunk-bravo":   {"first", 0},
		"chunk-charlie": {"second", 1},
		"chunk-alpha":   {"third", 2},
	}
	if len(engine.inserted) != len(want) {
		t.Fatalf("inserted rows = %d, want %d", len(engine.inserted), len(want))
	}
	for _, row := range engine.inserted {
		chunkID, _ := row["id"].(string)
		expected, ok := want[chunkID]
		if !ok {
			t.Fatalf("rebuilt chunk %q was not part of the anchor", chunkID)
		}
		if content, _ := row["content_with_weight"].(string); content != expected.content {
			t.Fatalf("chunk %q content = %q, want %q", chunkID, content, expected.content)
		}
		order, ok := common.GetInt(row["chunk_order_int"])
		if !ok {
			t.Fatalf("chunk %q carries no chunk_order_int: %v", chunkID, row["chunk_order_int"])
		}
		if order != expected.order {
			t.Fatalf("chunk %q order = %d, want %d (the reading order the history recorded)", chunkID, order, expected.order)
		}
		// A rebuilt row must look exactly like a parsed one. The parser writes
		// only content_with_weight; a second copy in the legacy `content` field
		// diverges as soon as the chunk is edited, and a client that submits
		// `content` back silently reverts the edit.
		if _, ok := row["content"]; ok {
			t.Fatalf("chunk %q was rebuilt with the legacy `content` alias: %v", chunkID, row["content"])
		}
	}

	// The restored chunk is recorded as a restore of v0 - the parse baseline -
	// so the badge reads something other than the edit it replaced, and the
	// highest version describes the row the replay just wrote.
	restored := loadChunkVersions(t, "chunk-bravo")
	if len(restored) != 3 {
		t.Fatalf("chunk-bravo version rows = %d, want 3 (%#v)", len(restored), restored)
	}
	last := restored[2]
	if last.Version != 2 {
		t.Fatalf("restore version = %d, want 2", last.Version)
	}
	if last.Kind != entity.ChunkVersionKindRestore {
		t.Fatalf("restore kind = %q, want %q", last.Kind, entity.ChunkVersionKindRestore)
	}
	if last.RestoredFrom != 0 {
		t.Fatalf("restore source version = %d, want 0", last.RestoredFrom)
	}
	if last.CreatedBy != entity.ChunkVersionSystemActor {
		t.Fatalf("restore actor = %q, want %q - a dataset restore is not a chunk edit",
			last.CreatedBy, entity.ChunkVersionSystemActor)
	}
	if content := last.Snapshot["content_with_weight"]; content != "first" {
		t.Fatalf("restore snapshot content = %v, want %q", content, "first")
	}
	if order, _ := common.GetInt(last.Snapshot["chunk_order_int"]); order != 0 {
		t.Fatalf("restore snapshot order = %v, want 0", last.Snapshot["chunk_order_int"])
	}

	// The chunk the anchor did not hold was removed from the engine, so its
	// history has to say it is gone; otherwise the next replay brings it back.
	dropped := loadChunkVersions(t, "chunk-delta")
	if len(dropped) != 2 {
		t.Fatalf("chunk-delta version rows = %d, want 2 (%#v)", len(dropped), dropped)
	}
	if dropped[1].Kind != entity.ChunkVersionKindDelete {
		t.Fatalf("chunk-delta latest kind = %q, want %q", dropped[1].Kind, entity.ChunkVersionKindDelete)
	}
}

// TestReplayKeepsADisabledDocumentsChunksOutOfRetrieval pins the second gate on
// visibility: a document's switch is a state of the whole document, so its chunks
// hold no version of their own. A replay that read only the chunk snapshot would
// put a disabled document back into retrieval as soon as a version is restored.
func TestReplayKeepsADisabledDocumentsChunksOutOfRetrieval(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")
	disabled := string(entity.StatusInvalid)
	if err := dao.DB.Model(&entity.Document{}).Where("id = ?", "doc-a").UpdateColumn("status", disabled).Error; err != nil {
		t.Fatalf("disable document: %v", err)
	}

	insertRebuiltHistory(t, "chunk-alpha", "visible when parsed", 0, 0, entity.ChunkVersionKindParse, 2)

	engine := &replayTestEngine{}
	svc := newReplayTestService(engine)
	if _, err := svc.ReplayDocumentChunksAtEE(chunkTestContext(t, "user-1", "tenant-1"), "kb-1", "doc-a", 2, 5); err != nil {
		t.Fatalf("ReplayDocumentChunksAtEE error = %v", err)
	}
	if len(engine.inserted) != 1 {
		t.Fatalf("inserted rows = %d, want 1", len(engine.inserted))
	}
	if available, ok := common.GetInt(engine.inserted[0]["available_int"]); !ok || available != 0 {
		t.Fatalf("replayed available_int = %v, want hidden while the document is disabled", engine.inserted[0]["available_int"])
	}
}

// TestReplayDocumentChunksRestoresThePicture pins the field the chunk list reads
// as image_id. A replayed row without it lists no picture even though the version
// recorded one, and the image block comes back as text that names an image which
// is no longer there.
func TestReplayDocumentChunksRestoresThePicture(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	// A version that recorded a picture: img_id is the content-addressed object
	// key the image endpoint serves.
	insertRebuiltHistorySnapshot(t, "chunk-picture", entity.JSONMap{
		"content_with_weight": "![figure](figure)",
		"img_id":              "kb-1-chunk-picture-abc123",
		"doc_type_kwd":        "image",
		"chunk_order_int":     0,
	}, 0, entity.ChunkVersionKindParse, 1)
	// A version recorded after the picture was removed: the empty img_id is the
	// statement "this version had no picture", not a missing field.
	insertRebuiltHistorySnapshot(t, "chunk-picture-removed", entity.JSONMap{
		"content_with_weight": "text after the picture was removed",
		"img_id":              "",
		"chunk_order_int":     1,
	}, 0, entity.ChunkVersionKindParse, 1)
	// A version written before pictures were versioned carries no img_id key at
	// all, and a replay has no picture to put back.
	insertRebuiltHistorySnapshot(t, "chunk-legacy", entity.JSONMap{
		"content_with_weight": "recorded before pictures were versioned",
		"chunk_order_int":     2,
	}, 0, entity.ChunkVersionKindParse, 1)

	engine := &replayTestEngine{}
	svc := newReplayTestService(engine)
	if _, err := svc.ReplayDocumentChunksAtEE(chunkTestContext(t, "user-1", "tenant-1"), "kb-1", "doc-a", 1, 2); err != nil {
		t.Fatalf("ReplayDocumentChunksAtEE error = %v", err)
	}

	rows := make(map[string]map[string]interface{}, len(engine.inserted))
	for _, row := range engine.inserted {
		chunkID, _ := row["id"].(string)
		rows[chunkID] = row
	}
	if len(rows) != 3 {
		t.Fatalf("rebuilt rows = %d, want 3", len(rows))
	}
	if got := rows["chunk-picture"]["img_id"]; got != "kb-1-chunk-picture-abc123" {
		t.Fatalf("img_id = %#v, want the picture the version recorded", got)
	}
	if got, ok := rows["chunk-picture-removed"]["img_id"]; !ok || got != "" {
		t.Fatalf("img_id = %#v (present %v), want an explicit empty picture", rows["chunk-picture-removed"]["img_id"], ok)
	}
	if got, ok := rows["chunk-legacy"]["img_id"]; ok {
		t.Fatalf("img_id = %#v, want absent for a version older than picture versioning", got)
	}
}

// TestReplayLeavesUntouchedChunkHistoryAlone covers the cheap case the replay
// has to stay cheap in: a chunk nobody edited since the anchor changed nothing,
// so it neither gains a row nor gets its badge bumped.
func TestReplayLeavesUntouchedChunkHistoryAlone(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	insertRebuiltHistory(t, "c-1st", "first", 0, 0, entity.ChunkVersionKindParse, 1)

	engine := &replayTestEngine{}
	svc := newReplayTestService(engine)

	if _, err := svc.ReplayDocumentChunksAtEE(chunkTestContext(t, "user-1", "tenant-1"), "kb-1", "doc-a", 1, 3); err != nil {
		t.Fatalf("ReplayDocumentChunksAtEE error = %v", err)
	}

	rows := loadChunkVersions(t, "c-1st")
	if len(rows) != 1 {
		t.Fatalf("version rows = %d, want the single parse baseline (%#v)", len(rows), rows)
	}
	if rows[0].Kind != entity.ChunkVersionKindParse {
		t.Fatalf("kind = %q, want %q", rows[0].Kind, entity.ChunkVersionKindParse)
	}
}

// TestEditAfterReplayAppendsVersion is the sequence a user goes through: the
// document is restored, and the chunk is edited again afterwards. The edit has to
// land on top of the restore row, or the history stops moving.
func TestEditAfterReplayAppendsVersion(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	insertRebuiltHistory(t, "chunk-bravo", "first", 0, 0, entity.ChunkVersionKindParse, 1)
	insertRebuiltHistory(t, "chunk-bravo", "first edited", 1, 0, entity.ChunkVersionKindEdit, 2)

	engine := &replayTestEngine{}
	svc := newReplayTestService(engine)
	// GetChunk answers with whatever the replay last wrote: that is the row an
	// edit reads, and the replay restored the pre-edit text.
	engine.existingChunk = map[string]interface{}{
		"doc_id":              "doc-a",
		"content_with_weight": "first",
	}
	ctx := chunkTestContext(t, "user-1", "tenant-1")

	if _, err := svc.ReplayDocumentChunksAtEE(ctx, "kb-1", "doc-a", 1, 3); err != nil {
		t.Fatalf("ReplayDocumentChunksAtEE error = %v", err)
	}

	body := "first after restore"
	if err := svc.UpdateChunk(ctx, &service.UpdateChunkRequest{
		DatasetID:  "kb-1",
		DocumentID: "doc-a",
		ChunkID:    "chunk-bravo",
		Content:    &body,
	}); err != nil {
		t.Fatalf("UpdateChunk after restore error = %v", err)
	}

	rows := loadChunkVersions(t, "chunk-bravo")
	if len(rows) != 4 {
		t.Fatalf("version rows = %d, want 4 (parse, edit, restore, new edit): %#v", len(rows), rows)
	}
	last := rows[3]
	if last.Version != 3 {
		t.Fatalf("newest version = %d, want 3", last.Version)
	}
	if last.Kind != entity.ChunkVersionKindEdit {
		t.Fatalf("newest kind = %q, want %q", last.Kind, entity.ChunkVersionKindEdit)
	}
	if content := last.Snapshot["content_with_weight"]; content != body {
		t.Fatalf("newest content = %v, want %q", content, body)
	}
}

// TestEditAfterReplayWithExpectedVersionAppendsVersion covers the locked edit
// the chunk dialog sends: it pins the version it read, which after a restore is
// the restore row, and the edit has to CAS against that row and land on top of
// it.
func TestEditAfterReplayWithExpectedVersionAppendsVersion(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	insertRebuiltHistory(t, "chunk-bravo", "first", 0, 0, entity.ChunkVersionKindParse, 1)
	insertRebuiltHistory(t, "chunk-bravo", "first edited", 1, 0, entity.ChunkVersionKindEdit, 2)

	engine := &replayTestEngine{}
	engine.existingChunk = map[string]interface{}{
		"doc_id":              "doc-a",
		"content_with_weight": "first",
	}
	svc := newReplayTestService(engine)
	ctx := chunkTestContext(t, "user-1", "tenant-1")

	if _, err := svc.ReplayDocumentChunksAtEE(ctx, "kb-1", "doc-a", 1, 3); err != nil {
		t.Fatalf("ReplayDocumentChunksAtEE error = %v", err)
	}

	body := "first after restore"
	if err := svc.UpdateChunk(ctx, &service.UpdateChunkRequest{
		DatasetID:       "kb-1",
		DocumentID:      "doc-a",
		ChunkID:         "chunk-bravo",
		Content:         &body,
		ExpectedVersion: chunkVersionIntPtr(2),
	}); err != nil {
		t.Fatalf("locked edit after restore error = %v", err)
	}

	rows := loadChunkVersions(t, "chunk-bravo")
	if len(rows) != 4 {
		t.Fatalf("version rows = %d, want 4 (parse, edit, restore, new edit): %#v", len(rows), rows)
	}
	if content := rows[3].Snapshot["content_with_weight"]; content != body {
		t.Fatalf("newest content = %v, want %q", content, body)
	}
}

// TestEditAfterReplayReportsStaleExpectedVersion is the failure a client sees
// when it edits a chunk with the version number it read before the restore: the
// restore moved the chunk, the pinned base no longer describes it, and the edit
// is refused before either store is touched. The message has to name both
// numbers - that is what tells the caller to reload instead of hunting for a
// collaborator who does not exist.
func TestEditAfterReplayReportsStaleExpectedVersion(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	insertRebuiltHistory(t, "chunk-bravo", "first", 0, 0, entity.ChunkVersionKindParse, 1)
	insertRebuiltHistory(t, "chunk-bravo", "first edited", 1, 0, entity.ChunkVersionKindEdit, 2)

	engine := &replayTestEngine{}
	engine.existingChunk = map[string]interface{}{
		"doc_id":              "doc-a",
		"content_with_weight": "first",
	}
	svc := newReplayTestService(engine)
	ctx := chunkTestContext(t, "user-1", "tenant-1")

	if _, err := svc.ReplayDocumentChunksAtEE(ctx, "kb-1", "doc-a", 1, 3); err != nil {
		t.Fatalf("ReplayDocumentChunksAtEE error = %v", err)
	}
	writesAfterReplay := len(engine.updateCalls)

	body := "first after restore"
	err := svc.UpdateChunk(ctx, &service.UpdateChunkRequest{
		DatasetID:       "kb-1",
		DocumentID:      "doc-a",
		ChunkID:         "chunk-bravo",
		Content:         &body,
		ExpectedVersion: chunkVersionIntPtr(1),
	})
	var conflict interface{ Code() common.ErrorCode }
	if !errors.As(err, &conflict) || conflict.Code() != common.CodeConflict {
		t.Fatalf("stale edit error = %v, want CodeConflict", err)
	}
	if !strings.Contains(err.Error(), "expected 1") || !strings.Contains(err.Error(), "current is 2") {
		t.Fatalf("conflict message = %q, want it to name the pinned and the current version", err.Error())
	}
	if len(engine.updateCalls) != writesAfterReplay {
		t.Fatalf("a refused edit must not reach the engine, writes = %d, want %d", len(engine.updateCalls), writesAfterReplay)
	}
	if rows := loadChunkVersions(t, "chunk-bravo"); len(rows) != 3 {
		t.Fatalf("version rows = %d, want 3 (a refused edit records nothing): %#v", len(rows), rows)
	}
}

// TestReplayChunkOrder checks the position a replayed row carries: the recorded
// one when the snapshot has it, in whatever numeric shape the row came back in,
// and the replay slot for a history written before the position was stored.
func TestReplayChunkOrder(t *testing.T) {
	tests := []struct {
		name     string
		snapshot entity.JSONMap
		fallback int
		want     int
	}{
		{
			name:     "recorded as int",
			snapshot: entity.JSONMap{"chunk_order_int": 4},
			fallback: 9,
			want:     4,
		},
		{
			name:     "recorded as float64 after the json round trip",
			snapshot: entity.JSONMap{"chunk_order_int": float64(4)},
			fallback: 9,
			want:     4,
		},
		{
			name:     "zero is a position, not a missing value",
			snapshot: entity.JSONMap{"chunk_order_int": float64(0)},
			fallback: 9,
			want:     0,
		},
		{
			name:     "history written before the position was stored",
			snapshot: entity.JSONMap{"content_with_weight": "body"},
			fallback: 9,
			want:     9,
		},
		{
			name:     "unreadable value",
			snapshot: entity.JSONMap{"chunk_order_int": "second"},
			fallback: 9,
			want:     9,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := replayChunkOrder(tt.snapshot, tt.fallback); got != tt.want {
				t.Fatalf("replayChunkOrder = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestReplayKeepsDatasetCountersInStep pins that a replay moves the dataset
// aggregate with the document counters. The replay writes a different number
// of chunks than the document held before it, so updating only the document
// would leave the dataset list reporting totals that do not match its
// documents.
func TestReplayKeepsDatasetCountersInStep(t *testing.T) {
	setupChunkVersionTestDB(t)
	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	// insertChunkTestDoc leaves the document at 7 chunks; the dataset aggregate
	// carries the totals of the state the restore replaces.
	if err := dao.DB.Model(&entity.Knowledgebase{}).Where("id = ?", "kb-1").
		Updates(map[string]interface{}{"chunk_num": 10, "token_num": 20}).Error; err != nil {
		t.Fatalf("seed dataset counters: %v", err)
	}
	insertRebuiltHistory(t, "chunk-alpha", "first", 0, 0, entity.ChunkVersionKindParse, 1)
	insertRebuiltHistory(t, "chunk-bravo", "second", 0, 1, entity.ChunkVersionKindParse, 1)

	svc := newReplayTestService(&replayTestEngine{})
	if _, err := svc.ReplayDocumentChunksAtEE(chunkTestContext(t, "user-1", "tenant-1"), "kb-1", "doc-a", 1, 3); err != nil {
		t.Fatalf("ReplayDocumentChunksAtEE error = %v", err)
	}

	var doc entity.Document
	if err := dao.DB.Where("id = ?", "doc-a").Take(&doc).Error; err != nil {
		t.Fatalf("reload document: %v", err)
	}
	if doc.ChunkNum != 2 || doc.TokenNum != 2 {
		t.Fatalf("document counters = %d/%d, want 2/2", doc.ChunkNum, doc.TokenNum)
	}

	var kb entity.Knowledgebase
	if err := dao.DB.Where("id = ?", "kb-1").Take(&kb).Error; err != nil {
		t.Fatalf("reload dataset: %v", err)
	}
	// chunk: 10 + (2 - 7) = 5; token: 20 + (2 - 0) = 22.
	if kb.ChunkNum != 5 || kb.TokenNum != 22 {
		t.Fatalf("dataset counters = %d/%d, want 5/22", kb.ChunkNum, kb.TokenNum)
	}
}

// TestReplayKeepsChunksWhenBuildingRowsFails pins the order of the two halves:
// the rows are built before the engine is cleared. A version recorded before
// snapshots held their derived fields cannot be replayed, and failing after the
// clear would leave the document with no chunks and the old counters pointing at
// them.
func TestReplayKeepsChunksWhenBuildingRowsFails(t *testing.T) {
	setupChunkVersionTestDB(t)
	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	// A snapshot written before the whole-row shape: no derived fields and no
	// metadata, and therefore the one thing a replay cannot copy.
	if err := dao.DB.Create(&entity.ChunkVersion{
		ChunkID: "chunk-alpha", Version: 0, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1,
		Kind:     entity.ChunkVersionKindParse,
		Snapshot: entity.JSONMap{"content_with_weight": "first"},
	}).Error; err != nil {
		t.Fatalf("seed the incomplete version: %v", err)
	}

	engine := &replayTestEngine{}
	svc := newReplayTestService(engine)
	if _, err := svc.ReplayDocumentChunksAtEE(chunkTestContext(t, "user-1", "tenant-1"), "kb-1", "doc-a", 1, 3); err == nil {
		t.Fatal("a replay that could not be built reported no error")
	}

	if engine.deleteCalls != 0 {
		t.Fatalf("engine cleared %d times, want 0: the rows are built before the clear", engine.deleteCalls)
	}
	if len(engine.inserted) != 0 {
		t.Fatalf("engine wrote %d rows, want none", len(engine.inserted))
	}

	var doc entity.Document
	if err := dao.DB.Where("id = ?", "doc-a").Take(&doc).Error; err != nil {
		t.Fatalf("reload document: %v", err)
	}
	if doc.ChunkNum != 7 {
		t.Fatalf("document chunk_num = %d, want the untouched 7", doc.ChunkNum)
	}
	// The history is untouched too: the chunks are still open, which is true.
	if rows := loadChunkVersions(t, "chunk-alpha"); len(rows) != 1 {
		t.Fatalf("version rows = %d, want the single parse row", len(rows))
	}
}

// TestReplayZeroesCountersWhenTheWriteFails covers the state a failed bulk used
// to leave: the engine has no chunks while the counters still advertise the old
// ones. The counters now say zero and the message says why; the history stays
// open so a retry can put the chunks back.
func TestReplayZeroesCountersWhenTheWriteFails(t *testing.T) {
	setupChunkVersionTestDB(t)
	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")
	if err := dao.DB.Model(&entity.Knowledgebase{}).Where("id = ?", "kb-1").
		Updates(map[string]interface{}{"chunk_num": 10, "token_num": 20}).Error; err != nil {
		t.Fatalf("seed dataset counters: %v", err)
	}

	insertRebuiltHistory(t, "chunk-alpha", "first", 0, 0, entity.ChunkVersionKindParse, 1)
	insertRebuiltHistory(t, "chunk-bravo", "second", 0, 1, entity.ChunkVersionKindParse, 1)

	engine := &replayTestEngine{insertErr: errors.New("bulk rejected")}
	svc := newReplayTestService(engine)
	if _, err := svc.ReplayDocumentChunksAtEE(chunkTestContext(t, "user-1", "tenant-1"), "kb-1", "doc-a", 1, 3); err == nil {
		t.Fatal("a replay whose write failed reported no error")
	}

	// Cleared once for the replay, once to make "no chunks" true after the
	// partial bulk.
	if engine.deleteCalls != 2 {
		t.Fatalf("engine cleared %d times, want 2 (replay and compensation)", engine.deleteCalls)
	}

	var doc entity.Document
	if err := dao.DB.Where("id = ?", "doc-a").Take(&doc).Error; err != nil {
		t.Fatalf("reload document: %v", err)
	}
	if doc.ChunkNum != 0 || doc.TokenNum != 0 {
		t.Fatalf("document counters = %d/%d, want 0/0", doc.ChunkNum, doc.TokenNum)
	}
	if doc.ProgressMsg == nil || !strings.Contains(*doc.ProgressMsg, "Restore failed") {
		t.Fatalf("progress message = %v, want it to say the restore failed", doc.ProgressMsg)
	}
	// The failed replay leaves the restore's marker behind with no chunks. Without
	// it the failure is invisible: the list falls back to the previous run's
	// outcome and can call a document with no chunks completed, and the version
	// page cannot name the documents the reader has to re-parse.
	if doc.RestoredVersion == nil || *doc.RestoredVersion != 1 {
		t.Fatalf("restore marker = %v, want the replayed version 1", doc.RestoredVersion)
	}

	var kb entity.Knowledgebase
	if err := dao.DB.Where("id = ?", "kb-1").Take(&kb).Error; err != nil {
		t.Fatalf("reload dataset: %v", err)
	}
	// chunk: 10 + (0 - 7) = 3; the document never carried tokens.
	if kb.ChunkNum != 3 || kb.TokenNum != 20 {
		t.Fatalf("dataset counters = %d/%d, want 3/20", kb.ChunkNum, kb.TokenNum)
	}

	// No tombstone: the chunks are still open in the history, which is what lets
	// a retry or a replay of a later version bring them back.
	if rows := loadChunkVersions(t, "chunk-alpha"); len(rows) != 1 {
		t.Fatalf("chunk-alpha version rows = %d, want the history untouched", len(rows))
	}
}

// TestReplayRetryDoesNotRepeatRestoreRow covers the retry a failed restore
// invites: the first run recorded the restore row, so a second run finds the
// latest version already describing the restored content. Appending another row
// would move the version number on every attempt.
func TestReplayRetryDoesNotRepeatRestoreRow(t *testing.T) {
	setupChunkVersionTestDB(t)
	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	insertRebuiltHistory(t, "chunk-bravo", "first", 0, 0, entity.ChunkVersionKindParse, 1)
	insertRebuiltHistory(t, "chunk-bravo", "first edited", 1, 0, entity.ChunkVersionKindEdit, 2)

	engine := &replayTestEngine{}
	svc := newReplayTestService(engine)
	ctx := chunkTestContext(t, "user-1", "tenant-1")
	for attempt := 1; attempt <= 2; attempt++ {
		if _, err := svc.ReplayDocumentChunksAtEE(ctx, "kb-1", "doc-a", 1, 3); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}

	rows := loadChunkVersions(t, "chunk-bravo")
	if len(rows) != 3 {
		t.Fatalf("version rows = %d, want 3 (parse, edit, one restore): %#v", len(rows), rows)
	}
	last := rows[2]
	if last.Kind != entity.ChunkVersionKindRestore || last.RestoredFrom != 0 {
		t.Fatalf("latest row = %q restored_from %d, want the single restore of v0", last.Kind, last.RestoredFrom)
	}
}

// TestReplayRestoresRowIdentityAndHiddenState pins what a replay puts back
// beyond the text: the parent link, the PDF position, the title tokens, and the
// hidden flag. Without them a restored child recalls as a bare paragraph, loses
// its PDF citation and the title boost it was indexed with, and a parent row
// comes back as a searchable chunk.
func TestReplayRestoresRowIdentityAndHiddenState(t *testing.T) {
	setupChunkVersionTestDB(t)
	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	created := int64(1700000000000) + 100

	child := &entity.ChunkVersion{
		ChunkID: "child-1", Version: 0, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1,
		Kind: entity.ChunkVersionKindParse,
		Snapshot: fullTestSnapshot(entity.JSONMap{
			"content_with_weight": "child body",
			"doc_type_kwd":        "text",
			"mom_id":              "parent-1",
			"page_num_int":        []interface{}{3},
			"top_int":             []interface{}{120},
			"title_tks":           "section title",
			"title_sm_tks":        "sect title",
			"chunk_order_int":     0,
			// The annotations an edit can change: a restore has to put all four
			// back, or the chunk answers a different row than the version names.
			"important_kwd": []interface{}{"kw1", "kw2"},
			"question_kwd":  []interface{}{"how?"},
			"tag_feas":      map[string]interface{}{"t1": 0.5},
			"tag_kwd":       []interface{}{"t1"},
		}),
	}
	child.CreateTime = &created
	if err := dao.DB.Create(child).Error; err != nil {
		t.Fatalf("seed child history: %v", err)
	}

	// The parent row a parent-child parse writes: same document, no doc_type, and
	// hidden from retrieval.
	parent := &entity.ChunkVersion{
		ChunkID: "parent-1", Version: 0, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1,
		Kind: entity.ChunkVersionKindParse,
		Snapshot: fullTestSnapshot(entity.JSONMap{
			"content_with_weight": "parent body",
			"available_int":       0,
			"chunk_order_int":     common.ParentChunkOrderBase,
		}),
	}
	parent.CreateTime = &created
	if err := dao.DB.Create(parent).Error; err != nil {
		t.Fatalf("seed parent history: %v", err)
	}

	engine := &replayTestEngine{}
	svc := newReplayTestService(engine)
	count, err := svc.ReplayDocumentChunksAtEE(chunkTestContext(t, "user-1", "tenant-1"), "kb-1", "doc-a", 1, 3)
	if err != nil {
		t.Fatalf("ReplayDocumentChunksAtEE error = %v", err)
	}
	if count != 2 {
		t.Fatalf("rebuilt chunks = %d, want 2", count)
	}

	rows := make(map[string]map[string]interface{}, len(engine.inserted))
	for _, row := range engine.inserted {
		id, _ := row["id"].(string)
		rows[id] = row
	}

	childRow := rows["child-1"]
	if childRow == nil {
		t.Fatal("the child chunk was not rebuilt")
	}
	if childRow["mom_id"] != "parent-1" {
		t.Fatalf("child mom_id = %v, want the parent link", childRow["mom_id"])
	}
	for _, field := range []string{"page_num_int", "top_int", "title_tks", "title_sm_tks"} {
		if _, ok := childRow[field]; !ok {
			t.Fatalf("child %s was not restored", field)
		}
	}
	// The annotations come back with the row: keywords, questions and the tags in
	// both of their shapes. A chunk restored without them answers a different row
	// than the version names.
	if got := childRow["important_kwd"]; !reflect.DeepEqual(got, []interface{}{"kw1", "kw2"}) {
		t.Fatalf("child important_kwd = %#v, want the version's keywords", got)
	}
	if got := childRow["question_kwd"]; !reflect.DeepEqual(got, []interface{}{"how?"}) {
		t.Fatalf("child question_kwd = %#v, want the version's questions", got)
	}
	if got := childRow["tag_kwd"]; !reflect.DeepEqual(got, []interface{}{"t1"}) {
		t.Fatalf("child tag_kwd = %#v, want the version's tags", got)
	}
	if _, ok := childRow["tag_feas"]; !ok {
		t.Fatal("child tag_feas was not restored")
	}
	if available, _ := common.GetInt(childRow["available_int"]); available != 1 {
		t.Fatalf("child available_int = %v, want 1", childRow["available_int"])
	}
	if childRow["doc_type_kwd"] != "text" {
		t.Fatalf("child doc_type_kwd = %v, want text", childRow["doc_type_kwd"])
	}

	parentRow := rows["parent-1"]
	if parentRow == nil {
		t.Fatal("the parent chunk was not rebuilt")
	}
	if available, _ := common.GetInt(parentRow["available_int"]); available != 0 {
		t.Fatalf("parent available_int = %v, want it to stay hidden", parentRow["available_int"])
	}
	if _, ok := parentRow["doc_type_kwd"]; ok {
		t.Fatalf("parent doc_type_kwd = %v, want none: the parser never wrote one", parentRow["doc_type_kwd"])
	}
}

// TestReplayAnchorsHistoryAtTheRestoreVersion pins the number the restore
// reserved for itself: the restore row and the tombstone a replay writes both
// carry it, so the version row the caller records on that same number describes
// exactly the rows the replay produced - one restore, one version.
func TestReplayAnchorsHistoryAtTheRestoreVersion(t *testing.T) {
	setupChunkVersionTestDB(t)
	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	insertRebuiltHistory(t, "chunk-bravo", "first", 0, 0, entity.ChunkVersionKindParse, 1)
	insertRebuiltHistory(t, "chunk-bravo", "first edited", 1, 0, entity.ChunkVersionKindEdit, 2)
	// A chunk the document only gained after the anchor: the replay drops it, so
	// its history has to say so at the same number as the restore.
	insertRebuiltHistory(t, "chunk-delta", "added later", 0, 1, entity.ChunkVersionKindParse, 3)

	engine := &replayTestEngine{}
	svc := newReplayTestService(engine)
	if _, err := svc.ReplayDocumentChunksAtEE(chunkTestContext(t, "user-1", "tenant-1"), "kb-1", "doc-a", 1, 4); err != nil {
		t.Fatalf("ReplayDocumentChunksAtEE error = %v", err)
	}

	restored := loadChunkVersions(t, "chunk-bravo")
	last := restored[len(restored)-1]
	if last.Kind != entity.ChunkVersionKindRestore || last.DatasetVersion != 4 {
		t.Fatalf("restore row = kind %q on version %d, want a restore on the caller's 4", last.Kind, last.DatasetVersion)
	}
	dropped := loadChunkVersions(t, "chunk-delta")
	tombstone := dropped[len(dropped)-1]
	if tombstone.Kind != entity.ChunkVersionKindDelete || tombstone.DatasetVersion != 4 {
		t.Fatalf("tombstone = kind %q on version %d, want a delete on the caller's 4", tombstone.Kind, tombstone.DatasetVersion)
	}
	// The dataset's own column is the caller's to move: the replay must not spend
	// a second number on top of the one it was handed.
	var kb entity.Knowledgebase
	if err := dao.DB.Where("id = ?", "kb-1").Take(&kb).Error; err != nil {
		t.Fatalf("reload dataset: %v", err)
	}
	if kb.Version != 0 {
		t.Fatalf("dataset version = %d, want the replay to leave the column alone", kb.Version)
	}
}

// TestReplayReservesANumberWhenTheCallerBringsNone keeps the fallback working:
// a caller with no number of its own still gets its history anchored, at the cost
// of one number per document.
func TestReplayReservesANumberWhenTheCallerBringsNone(t *testing.T) {
	setupChunkVersionTestDB(t)
	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	insertRebuiltHistory(t, "chunk-bravo", "first", 0, 0, entity.ChunkVersionKindParse, 1)
	insertRebuiltHistory(t, "chunk-bravo", "first edited", 1, 0, entity.ChunkVersionKindEdit, 2)

	svc := newReplayTestService(&replayTestEngine{})
	if _, err := svc.ReplayDocumentChunksAtEE(chunkTestContext(t, "user-1", "tenant-1"), "kb-1", "doc-a", 1, 0); err != nil {
		t.Fatalf("ReplayDocumentChunksAtEE error = %v", err)
	}

	rows := loadChunkVersions(t, "chunk-bravo")
	last := rows[len(rows)-1]
	if last.Kind != entity.ChunkVersionKindRestore {
		t.Fatalf("latest kind = %q, want a restore", last.Kind)
	}
	// The dataset column starts at 0 in this fixture, so the reserved number is 1.
	if last.DatasetVersion != 1 {
		t.Fatalf("restore row version = %d, want the reserved 1", last.DatasetVersion)
	}
	var kb entity.Knowledgebase
	if err := dao.DB.Where("id = ?", "kb-1").Take(&kb).Error; err != nil {
		t.Fatalf("reload dataset: %v", err)
	}
	if kb.Version != 1 {
		t.Fatalf("dataset version = %d, want the reserved number to have advanced it", kb.Version)
	}
}

// TestReplayMarksTheDocumentAsRestored pins the marker a caller reads to tell
// restored content from parsed content: the document records the version its
// content was replayed from, next to the counters the replay wrote. With
// chunk_num it answers whether the restore brought chunks back.
func TestReplayMarksTheDocumentAsRestored(t *testing.T) {
	setupChunkVersionTestDB(t)
	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	insertRebuiltHistory(t, "chunk-alpha", "first", 0, 0, entity.ChunkVersionKindParse, 1)
	insertRebuiltHistory(t, "chunk-bravo", "second", 0, 1, entity.ChunkVersionKindParse, 1)

	svc := newReplayTestService(&replayTestEngine{})
	if _, err := svc.ReplayDocumentChunksAtEE(chunkTestContext(t, "user-1", "tenant-1"), "kb-1", "doc-a", 1, 2); err != nil {
		t.Fatalf("ReplayDocumentChunksAtEE error = %v", err)
	}

	var doc entity.Document
	if err := dao.DB.Where("id = ?", "doc-a").Take(&doc).Error; err != nil {
		t.Fatalf("reload document: %v", err)
	}
	if doc.RestoredVersion == nil || *doc.RestoredVersion != 1 {
		t.Fatalf("restored_version = %v, want the anchor 1", doc.RestoredVersion)
	}
	if doc.ChunkNum != 2 {
		t.Fatalf("chunk_num = %d, want the 2 chunks the restore brought back", doc.ChunkNum)
	}
}

// TestReplayMarksAnEmptyReplay covers the other half of the distinction: the
// version held no chunk for the document, so the restore leaves it with none.
// The marker is still written - the content did come from a restore - and the
// chunk count is what says the restore produced nothing.
func TestReplayMarksAnEmptyReplay(t *testing.T) {
	setupChunkVersionTestDB(t)
	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	// The chunk was parsed and then removed, both at or before the anchor: the
	// state the restore replays is empty, though the history is not.
	insertRebuiltHistory(t, "chunk-alpha", "first", 0, 0, entity.ChunkVersionKindParse, 1)
	insertRebuiltHistory(t, "chunk-alpha", "first", 1, 0, entity.ChunkVersionKindDelete, 2)

	engine := &replayTestEngine{}
	svc := newReplayTestService(engine)
	count, err := svc.ReplayDocumentChunksAtEE(chunkTestContext(t, "user-1", "tenant-1"), "kb-1", "doc-a", 2, 3)
	if err != nil {
		t.Fatalf("ReplayDocumentChunksAtEE error = %v", err)
	}
	if count != 0 {
		t.Fatalf("rebuilt chunks = %d, want none: the chunk was removed before the anchor", count)
	}

	var doc entity.Document
	if err := dao.DB.Where("id = ?", "doc-a").Take(&doc).Error; err != nil {
		t.Fatalf("reload document: %v", err)
	}
	if doc.RestoredVersion == nil || *doc.RestoredVersion != 2 {
		t.Fatalf("restored_version = %v, want the anchor 2", doc.RestoredVersion)
	}
	if doc.ChunkNum != 0 {
		t.Fatalf("chunk_num = %d, want 0: the version held no chunk", doc.ChunkNum)
	}
	if doc.Progress != 0 {
		t.Fatalf("progress = %v, want 0 so the next parse is what produces the chunks", doc.Progress)
	}
}

// TestReplayReplaysAFullSnapshotAsACopy pins the reason a version stores the
// whole row: a snapshot that carries the tokens and the vector is handed back as
// it was written - no tokenizer run, no embedding call - so the rebuilt chunk is
// indexed and ranked exactly as the version was.
func TestReplayReplaysAFullSnapshotAsACopy(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	// The snapshot names the language the dataset is indexed with: tokens from
	// another tokenizer are the one thing a copy cannot carry across.
	var kb entity.Knowledgebase
	if err := dao.DB.Where("id = ?", "kb-1").Take(&kb).Error; err != nil {
		t.Fatalf("load the dataset: %v", err)
	}
	language := ""
	if kb.Language != nil {
		language = *kb.Language
	}

	snapshot := chunkversion.FullSnapshot(map[string]interface{}{
		"id":                  "chunk-1",
		"doc_id":              "doc-a",
		"kb_id":               "kb-1",
		"docnm_kwd":           "manual.pdf",
		"content_with_weight": "the body",
		"content_ltks":        "the body as indexed",
		"content_sm_ltks":     "the body as indexed",
		"important_kwd":       []string{"kw"},
		"available_int":       1,
		"chunk_order_int":     4,
		"img_id":              "",
		"q_1024_vec":          []float32{0.5, -0.25},
	}, chunkversion.SnapshotMeta{EmbeddingID: "embedding-model", Language: language})
	insertRebuiltHistorySnapshot(t, "chunk-1", snapshot, 1, entity.ChunkVersionKindEdit, 4)

	previous := chunkEmbedderEE
	embedded := 0
	chunkEmbedderEE = func(context.Context, ChunkEmbedRequest) (ChunkEmbedding, error) {
		embedded++
		return ChunkEmbedding{Field: "q_1024_vec", Vector: []float64{9, 9}}, nil
	}
	t.Cleanup(func() { chunkEmbedderEE = previous })

	engine := &replayTestEngine{}
	svc := newReplayTestService(engine)
	tokenized := 0
	svc.tokenizeFunc = func(text, _ string) (string, error) {
		tokenized++
		return "recomputed", nil
	}
	if _, err := svc.ReplayDocumentChunksAtEE(chunkTestContext(t, "user-1", "tenant-1"), "kb-1", "doc-a", 5, 6); err != nil {
		t.Fatalf("ReplayDocumentChunksAtEE() error = %v", err)
	}
	if tokenized != 0 || embedded != 0 {
		t.Fatalf("replay tokenized %d times and embedded %d times, want the stored row copied", tokenized, embedded)
	}
	if len(engine.inserted) != 1 {
		t.Fatalf("engine wrote %d rows, want the replayed chunk", len(engine.inserted))
	}

	row := engine.inserted[0]
	if got, _ := row["content_ltks"].(string); got != "the body as indexed" {
		t.Fatalf("content_ltks = %v, want the stored tokens", row["content_ltks"])
	}
	vector, ok := row["q_1024_vec"].([]float64)
	if !ok || len(vector) != 2 || vector[0] != 0.5 || vector[1] != -0.25 {
		t.Fatalf("q_1024_vec = %#v, want the stored vector", row["q_1024_vec"])
	}
	// Identity and reading position describe the document being restored, not the
	// document the snapshot was taken from.
	if row["id"] != "chunk-1" || row["doc_id"] != "doc-a" || row["kb_id"] != "kb-1" {
		t.Fatalf("row identity = %v/%v/%v, want the restored chunk's", row["id"], row["doc_id"], row["kb_id"])
	}
	if row["docnm_kwd"] != "doc-a.txt" {
		t.Fatalf("docnm_kwd = %v, want the live document name", row["docnm_kwd"])
	}
	if order, ok := common.GetInt(row["chunk_order_int"]); !ok || order != 4 {
		t.Fatalf("chunk_order_int = %v, want the stored position", row["chunk_order_int"])
	}
	if available, ok := common.GetInt(row["available_int"]); !ok || available != 1 {
		t.Fatalf("available_int = %v, want the stored visibility", row["available_int"])
	}
}

// TestReplayReplaysTheVectorOfAnotherModel pins what a replay does with a
// snapshot recorded by another embedding model: it copies it. The version's
// derived fields are part of what a restore returns, and a replay derives
// nothing - no tokenizer run, no model call - so a dataset whose model changed
// since the version was written gets that version back exactly as it was, and a
// re-parse is what brings the derived fields up to date.
func TestReplayReplaysTheVectorOfAnotherModel(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	snapshot := chunkversion.FullSnapshot(map[string]interface{}{
		"id":                  "chunk-1",
		"content_with_weight": "the body",
		"content_ltks":        "the body as indexed",
		"available_int":       1,
		"chunk_order_int":     0,
		"img_id":              "",
		"q_1024_vec":          []float32{0.5, -0.25},
	}, chunkversion.SnapshotMeta{EmbeddingID: "another-model", Language: "en"})
	insertRebuiltHistorySnapshot(t, "chunk-1", snapshot, 1, entity.ChunkVersionKindEdit, 4)

	previous := chunkEmbedderEE
	embedded := 0
	chunkEmbedderEE = func(context.Context, ChunkEmbedRequest) (ChunkEmbedding, error) {
		embedded++
		return ChunkEmbedding{Field: "q_1024_vec", Vector: []float64{9, 9}}, nil
	}
	t.Cleanup(func() { chunkEmbedderEE = previous })

	engine := &replayTestEngine{}
	svc := newReplayTestService(engine)
	if _, err := svc.ReplayDocumentChunksAtEE(chunkTestContext(t, "user-1", "tenant-1"), "kb-1", "doc-a", 5, 6); err != nil {
		t.Fatalf("ReplayDocumentChunksAtEE() error = %v", err)
	}
	if embedded != 0 {
		t.Fatalf("replay embedded %d times, want the model left alone", embedded)
	}
	if len(engine.inserted) != 1 {
		t.Fatalf("engine wrote %d rows, want the replayed chunk", len(engine.inserted))
	}
	vector, ok := engine.inserted[0]["q_1024_vec"].([]float64)
	if !ok || len(vector) != 2 || vector[0] != 0.5 || vector[1] != -0.25 {
		t.Fatalf("q_1024_vec = %#v, want the stored vector", engine.inserted[0]["q_1024_vec"])
	}
	if body, _ := engine.inserted[0]["content_with_weight"].(string); body != "the body" {
		t.Fatalf("rebuilt content = %q, want the stored text", body)
	}
	if tokens, _ := engine.inserted[0]["content_ltks"].(string); tokens != "the body as indexed" {
		t.Fatalf("content_ltks = %q, want the stored tokens", tokens)
	}
}
