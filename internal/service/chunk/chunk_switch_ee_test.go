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
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/engine/history"
	"ragflow/internal/engine/history/historytest"
	"ragflow/internal/entity"
)

// TestSwitchChunksRecordsAvailabilityInChunkHistory keeps the toggle in the
// chunk's own history as a version of its own. A restore rebuilds an engine row
// from a snapshot, so a toggle the history never learned about is a toggle the
// next restore silently undoes; an in-place rewrite would be just as lossy,
// because the state the overwritten version recorded would be gone.
func TestSwitchChunksRecordsAvailabilityInChunkHistory(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&entity.UserTenant{}, &entity.Knowledgebase{}, &entity.Document{}); err != nil {
		t.Fatalf("failed to migrate sqlite: %v", err)
	}
	migrateChunkVersionTableEE(t, db)
	previousDB := dao.DB
	dao.DB = db
	t.Cleanup(func() { dao.DB = previousDB })

	valid := string(entity.StatusValid)
	if err := db.Create(&entity.UserTenant{
		ID: "ut-1", UserID: "user-1", TenantID: "tenant-1", Role: "owner", Status: &valid,
	}).Error; err != nil {
		t.Fatalf("failed to create user_tenant: %v", err)
	}
	if err := db.Create(&entity.Knowledgebase{
		ID: "kb-1", TenantID: "tenant-1", Name: "dataset", EmbdID: "embed",
		Permission:   string(entity.TenantPermissionTeam),
		CreatedBy:    "user-1",
		ParserID:     string(entity.ParserTypeNaive),
		ParserConfig: entity.JSONMap{},
		Status:       &valid,
	}).Error; err != nil {
		t.Fatalf("failed to create knowledgebase: %v", err)
	}
	if err := db.Create(&entity.Document{
		ID: "doc-1", KbID: "kb-1", ParserID: string(entity.ParserTypeNaive),
		ParserConfig: entity.JSONMap{}, SourceType: "local", Type: "doc",
		CreatedBy: "user-1", Suffix: "txt",
	}).Error; err != nil {
		t.Fatalf("failed to create document: %v", err)
	}

	engine := &switchChunksEngineMock{rows: map[string]map[string]interface{}{
		"chunk-1": {
			"id": "chunk-1", "doc_id": "doc-1", "kb_id": "kb-1",
			"content_with_weight": "the body", "available_int": 1,
		},
	}}
	// The toggle records a version, and a version body lives in the document
	// engine: this test answers for that store with one held in memory.
	history.SetChunkStore(historytest.NewChunkStore())
	t.Cleanup(func() { history.SetChunkStore(nil) })
	svc := &ChunkService{docEngine: engine, kbDAO: dao.NewKnowledgebaseDAO()}

	if err := svc.SwitchChunks(chunkTestContext(t, "user-1", "tenant-1"), "kb-1", "doc-1", 0, []string{"chunk-1"}); err != nil {
		t.Fatalf("SwitchChunks() error = %v", err)
	}

	var rows []entity.ChunkVersion
	if err := db.Where("chunk_id = ?", "chunk-1").Order("version ASC").Find(&rows).Error; err != nil {
		t.Fatalf("load the chunk history: %v", err)
	}
	// The row carries only what locates the state; the state itself comes back
	// from the document engine.
	hydrated := make([]*entity.ChunkVersion, 0, len(rows))
	for i := range rows {
		hydrated = append(hydrated, &rows[i])
	}
	if err := history.HydrateChunks(t.Context(), hydrated); err != nil {
		t.Fatalf("hydrate the chunk history: %v", err)
	}
	if len(rows) != 2 || rows[0].Version != 0 || rows[1].Version != 1 {
		t.Fatalf("chunk versions = %d, want the visible row and the toggle", len(rows))
	}
	// v0 is the state the toggle replaced, stored as the whole engine row: the
	// visible value is recorded as the 1 it was instead of being left implicit.
	if visible, ok := common.GetInt(rows[0].Snapshot["available_int"]); !ok || visible != 1 {
		t.Fatalf("v0 available_int = %v (present %v), want the visible state", rows[0].Snapshot["available_int"], ok)
	}
	// The snapshot has to keep the content: the replay rebuilds the row from it.
	if got, _ := rows[0].Snapshot["content_with_weight"].(string); got != "the body" {
		t.Fatalf("v0 content = %q, want the chunk body", got)
	}
	if available, ok := common.GetInt(rows[1].Snapshot["available_int"]); !ok || available != 0 {
		t.Fatalf("v1 snapshot available_int = %v (present %v), want the hidden state", rows[1].Snapshot["available_int"], ok)
	}
	if got, _ := rows[1].Snapshot["content_with_weight"].(string); got != "the body" {
		t.Fatalf("v1 content = %q, want the chunk body", got)
	}
	// The toggle moved the dataset too, so the timeline and the chunk history
	// agree about when it happened.
	changes := loadDatasetChanges(t, "kb-1")
	if len(changes) != 1 {
		t.Fatalf("dataset changes after the toggle = %d, want 1", len(changes))
	}
	// The switch names the direction: the change set keeps the edit action, so
	// without this the timeline would read "Chunk edited" for a chunk nobody
	// edited.
	if changes[0].Kind != entity.DatasetVersionKindChunkDisable {
		t.Fatalf("toggle kind = %q, want %q", changes[0].Kind, entity.DatasetVersionKindChunkDisable)
	}
	// The entry itself names the direction, which is what the timeline line under
	// the version reads.
	if len(changes[0].Changes) != 1 ||
		changes[0].Changes[0].Action != entity.DatasetVersionChangeActionDisable {
		t.Fatalf("toggle change set = %#v, want one disable entry", changes[0].Changes)
	}

	// A row already in the requested state is not a change: the engine write runs
	// again, but neither history gains an entry.
	engine.rows["chunk-1"]["available_int"] = 0
	if err := svc.SwitchChunks(chunkTestContext(t, "user-1", "tenant-1"), "kb-1", "doc-1", 0, []string{"chunk-1"}); err != nil {
		t.Fatalf("SwitchChunks() repeat error = %v", err)
	}
	var repeated []entity.ChunkVersion
	if err := db.Where("chunk_id = ?", "chunk-1").Find(&repeated).Error; err != nil {
		t.Fatalf("reload the chunk history: %v", err)
	}
	if len(repeated) != 2 {
		t.Fatalf("chunk versions after repeating the toggle = %d, want 2", len(repeated))
	}
	if changes := loadDatasetChanges(t, "kb-1"); len(changes) != 1 {
		t.Fatalf("dataset changes after repeating the toggle = %d, want 1", len(changes))
	}

	// Switching it back is a version again.
	if err := svc.SwitchChunks(chunkTestContext(t, "user-1", "tenant-1"), "kb-1", "doc-1", 1, []string{"chunk-1"}); err != nil {
		t.Fatalf("SwitchChunks() re-enable error = %v", err)
	}
	var reenabled []entity.ChunkVersion
	if err := db.Where("chunk_id = ?", "chunk-1").Find(&reenabled).Error; err != nil {
		t.Fatalf("reload the chunk history: %v", err)
	}
	if len(reenabled) != 3 {
		t.Fatalf("chunk versions after re-enabling = %d, want 3", len(reenabled))
	}
	changes = loadDatasetChanges(t, "kb-1")
	if len(changes) != 2 {
		t.Fatalf("dataset changes after two toggles = %d, want 2", len(changes))
	}
	if changes[1].Kind != entity.DatasetVersionKindChunkEnable {
		t.Fatalf("re-enable kind = %q, want %q", changes[1].Kind, entity.DatasetVersionKindChunkEnable)
	}
	if len(changes[1].Changes) != 1 ||
		changes[1].Changes[0].Action != entity.DatasetVersionChangeActionEnable {
		t.Fatalf("re-enable change set = %#v, want one enable entry", changes[1].Changes)
	}
}

// TestSwitchChunksBatchProducesOneVersion pins the granularity of the timeline:
// one call moves a batch of chunks, and the batch is one version - not one per
// chunk. Each chunk still gets a history row of its own, which is why the entries
// under that version can name different chunk versions.
func TestSwitchChunksBatchProducesOneVersion(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&entity.UserTenant{}, &entity.Knowledgebase{}, &entity.Document{}); err != nil {
		t.Fatalf("failed to migrate sqlite: %v", err)
	}
	migrateChunkVersionTableEE(t, db)
	previousDB := dao.DB
	dao.DB = db
	t.Cleanup(func() { dao.DB = previousDB })

	valid := string(entity.StatusValid)
	if err := db.Create(&entity.UserTenant{
		ID: "ut-1", UserID: "user-1", TenantID: "tenant-1", Role: "owner", Status: &valid,
	}).Error; err != nil {
		t.Fatalf("failed to create user_tenant: %v", err)
	}
	if err := db.Create(&entity.Knowledgebase{
		ID: "kb-1", TenantID: "tenant-1", Name: "dataset", EmbdID: "embed",
		Permission:   string(entity.TenantPermissionTeam),
		CreatedBy:    "user-1",
		ParserID:     string(entity.ParserTypeNaive),
		ParserConfig: entity.JSONMap{},
		Status:       &valid,
	}).Error; err != nil {
		t.Fatalf("failed to create knowledgebase: %v", err)
	}
	if err := db.Create(&entity.Document{
		ID: "doc-1", KbID: "kb-1", ParserID: string(entity.ParserTypeNaive),
		ParserConfig: entity.JSONMap{}, SourceType: "local", Type: "doc",
		CreatedBy: "user-1", Suffix: "txt",
	}).Error; err != nil {
		t.Fatalf("failed to create document: %v", err)
	}

	engine := &switchChunksEngineMock{rows: map[string]map[string]interface{}{
		"chunk-1": {
			"id": "chunk-1", "doc_id": "doc-1", "kb_id": "kb-1",
			"content_with_weight": "the first body", "available_int": 1,
		},
		"chunk-2": {
			"id": "chunk-2", "doc_id": "doc-1", "kb_id": "kb-1",
			"content_with_weight": "the second body", "available_int": 1,
		},
	}}
	history.SetChunkStore(historytest.NewChunkStore())
	t.Cleanup(func() { history.SetChunkStore(nil) })
	svc := &ChunkService{docEngine: engine, kbDAO: dao.NewKnowledgebaseDAO()}

	if err := svc.SwitchChunks(chunkTestContext(t, "user-1", "tenant-1"), "kb-1", "doc-1", 0,
		[]string{"chunk-1", "chunk-2"}); err != nil {
		t.Fatalf("SwitchChunks() error = %v", err)
	}

	versions := loadDatasetChanges(t, "kb-1")
	if len(versions) != 1 {
		t.Fatalf("dataset versions = %d, want 1 for the whole batch", len(versions))
	}
	if versions[0].Kind != entity.DatasetVersionKindChunkDisable {
		t.Fatalf("batch kind = %q, want %q", versions[0].Kind, entity.DatasetVersionKindChunkDisable)
	}
	if len(versions[0].Changes) != 2 {
		t.Fatalf("batch change set = %#v, want one entry per chunk", versions[0].Changes)
	}
	for _, change := range versions[0].Changes {
		if change.Action != entity.DatasetVersionChangeActionDisable {
			t.Fatalf("change = %#v, want the disable action", change)
		}
	}
	// The chunk rows are per chunk, anchored at the one version the batch wrote.
	for _, chunkID := range []string{"chunk-1", "chunk-2"} {
		var rows []entity.ChunkVersion
		if err := db.Where("chunk_id = ? AND dataset_version = ?", chunkID, versions[0].Version).
			Find(&rows).Error; err != nil {
			t.Fatalf("load the history of %s: %v", chunkID, err)
		}
		if len(rows) == 0 {
			t.Fatalf("chunk %s has no history row under version %d", chunkID, versions[0].Version)
		}
	}
	// The pair is what the detail page reads back to print the version beside the
	// entry, so the switch has to resolve the same way an edit does.
	got, err := dao.NewChunkVersionDAO().VersionsAtDatasetVersion(
		context.Background(), db, "kb-1", versions[0].Version, []string{"chunk-1", "chunk-2"})
	if err != nil {
		t.Fatalf("read the versions the batch produced: %v", err)
	}
	if _, ok := got["chunk-1"]; !ok {
		t.Fatalf("chunk-1 resolves to no version under %d: %v", versions[0].Version, got)
	}
	if _, ok := got["chunk-2"]; !ok {
		t.Fatalf("chunk-2 resolves to no version under %d: %v", versions[0].Version, got)
	}
}
