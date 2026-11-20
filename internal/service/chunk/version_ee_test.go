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
	"encoding/base64"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/engine/history"
	"ragflow/internal/engine/history/historytest"
	"ragflow/internal/entity"
	"ragflow/internal/service"
	"ragflow/internal/service/chunkversion"
	"ragflow/internal/storage"

	"gorm.io/gorm"
)

func chunkVersionTestService(engine *updateChunkTestEngine) *ChunkService {
	return &ChunkService{
		docEngine:   engine,
		kbDAO:       dao.NewKnowledgebaseDAO(),
		documentDAO: dao.NewDocumentDAO(),
		// The analyzer pool is not started in unit tests, and the edit path now
		// refuses to store text without its tokens, so the seams answer instead.
		tokenizeFunc:            func(text, _ string) (string, error) { return text, nil },
		fineGrainedTokenizeFunc: func(text, _ string) (string, error) { return text, nil },
	}
}

// setupChunkVersionTestDB builds the shared chunk test fixtures plus the
// history table, which only this edition's schema carries.
// chunkVersionTestStore is the document engine as these tests see it: the
// version bodies live here, not in sqlite.
var chunkVersionTestStore *historytest.ChunkStore

// migrateChunkVersionTableEE adds the dataset-history tables a chunk edit writes
// through: the version that anchors the chunk row, and the snapshot its first
// write seeds. A history write that cannot be stored now fails the edit, so the
// shared fixture carries them for every test, not only the ones about versions.
func migrateChunkVersionTableEE(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.AutoMigrate(
		&entity.ChunkVersion{},
		&entity.DatasetVersion{},
		&entity.DatasetConfigSnapshot{},
		&entity.DatasetDocumentSetSnapshot{},
		&entity.Connector2Kb{},
	); err != nil {
		t.Fatalf("migrate chunk version tables: %v", err)
	}
}

func setupChunkVersionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupChunkTestDB(t)
	// dataset_version is read by a version delete (a dataset version that anchored
	// the deleted row must be answered) and written by every edit (the chunk row is
	// anchored at the version it was recorded under), so its tables are part of
	// this fixture: the snapshot it stores and the bindings its snapshot reads.
	if err := db.AutoMigrate(
		&entity.ChunkVersion{},
		&entity.DatasetVersion{},
		&entity.DatasetConfigSnapshot{},
		&entity.DatasetDocumentSetSnapshot{},
		&entity.Connector2Kb{},
	); err != nil {
		t.Fatalf("migrate chunk versions: %v", err)
	}
	chunkVersionTestStore = historytest.NewChunkStore()
	history.SetChunkStore(chunkVersionTestStore)
	t.Cleanup(func() { history.SetChunkStore(nil) })
	pushChunkTestDB(t, db)
	return db
}

// loadChunkVersions reads a chunk's timeline back the way a reader does: the
// rows from the metadata table, their bodies from the document engine.
func loadChunkVersions(t *testing.T, chunkID string) []*entity.ChunkVersion {
	t.Helper()
	var rows []*entity.ChunkVersion
	if err := dao.DB.Where("chunk_id = ?", chunkID).Order("version ASC").Find(&rows).Error; err != nil {
		t.Fatalf("load chunk versions: %v", err)
	}
	if err := history.HydrateChunks(context.Background(), rows); err != nil {
		t.Fatalf("hydrate chunk versions of %s: %v", chunkID, err)
	}
	return rows
}

// loadDatasetVersions reads a dataset's timeline back, oldest first.
func loadDatasetVersions(t *testing.T, datasetID string) []*entity.DatasetVersion {
	t.Helper()
	var rows []*entity.DatasetVersion
	if err := dao.DB.Where("dataset_id = ?", datasetID).Order("version ASC").Find(&rows).Error; err != nil {
		t.Fatalf("load dataset versions: %v", err)
	}
	return rows
}

// loadDatasetChanges reads a dataset's timeline without its baseline: the
// baseline is the state the dataset started in, not a change anyone made.
func loadDatasetChanges(t *testing.T, datasetID string) []*entity.DatasetVersion {
	t.Helper()
	changes := make([]*entity.DatasetVersion, 0, 4)
	for _, version := range loadDatasetVersions(t, datasetID) {
		if version.Kind != entity.DatasetVersionKindBaseline {
			changes = append(changes, version)
		}
	}
	return changes
}

// TestUpdateChunkSeedsVersionZeroAndAppendsEachEdit pins the history shape: the
// first edit preserves the pre-edit row as v0 and records its result as v1,
// and every later edit appends one more version.
func TestUpdateChunkSeedsVersionZeroAndAppendsEachEdit(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	engine := &updateChunkTestEngine{
		existingChunk: map[string]interface{}{
			"doc_id":              "doc-a",
			"content_with_weight": "v0 body",
			"available_int":       1,
		},
	}
	svc := chunkVersionTestService(engine)
	ctx := chunkTestContext(t, "user-1", "tenant-1")

	for _, content := range []string{"v1 body", "v2 body"} {
		body := content
		if err := svc.UpdateChunk(ctx, &service.UpdateChunkRequest{
			DatasetID:  "kb-1",
			DocumentID: "doc-a",
			ChunkID:    "chunk-1",
			Content:    &body,
		}); err != nil {
			t.Fatalf("UpdateChunk(%q) error = %v", content, err)
		}
	}

	rows := loadChunkVersions(t, "chunk-1")
	if len(rows) != 3 {
		t.Fatalf("version rows = %d, want 3 (%#v)", len(rows), rows)
	}
	wantContents := []string{"v0 body", "v1 body", "v2 body"}
	for i, row := range rows {
		if row.Version != i {
			t.Fatalf("row %d version = %d, want %d", i, row.Version, i)
		}
		if row.DocID != "doc-a" || row.DatasetID != "kb-1" {
			t.Fatalf("row %d scope = %s/%s, want doc-a/kb-1", i, row.DocID, row.DatasetID)
		}
		if row.CreatedBy != "user-1" {
			t.Fatalf("row %d created_by = %q, want user-1", i, row.CreatedBy)
		}
		if got := row.Snapshot["content_with_weight"]; got != wantContents[i] {
			t.Fatalf("row %d content = %v, want %q", i, got, wantContents[i])
		}
		if row.DatasetVersion <= 0 {
			t.Fatalf("row %d dataset version = %d, want the version it was written under", i, row.DatasetVersion)
		}
	}
	// The first edit seeds the version the chunk started from together with the
	// one it produced: both belong to that change, so they share its number.
	if rows[0].DatasetVersion != rows[1].DatasetVersion {
		t.Fatalf("seed row version = %d, first edit = %d, want the same change",
			rows[0].DatasetVersion, rows[1].DatasetVersion)
	}
	if rows[2].DatasetVersion <= rows[1].DatasetVersion {
		t.Fatalf("second edit version = %d, want it after %d", rows[2].DatasetVersion, rows[1].DatasetVersion)
	}
	// Each edit is a dataset version of its own, recorded in the same transaction
	// as the rows it stamps: the anchor names a version that exists.
	var recorded entity.DatasetVersion
	if err := dao.DB.Where("dataset_id = ? AND version = ?", "kb-1", rows[2].DatasetVersion).
		Take(&recorded).Error; err != nil {
		t.Fatalf("the dataset version the edit is anchored at was not recorded: %v", err)
	}
	if recorded.Kind != entity.DatasetVersionKindChunkUpdate {
		t.Fatalf("dataset version kind = %q, want %q", recorded.Kind, entity.DatasetVersionKindChunkUpdate)
	}
}

// TestChunkVersionKindNamesTheAction pins the label the dataset timeline reads. A
// batch of one action gets the kind that names it, so the row says "Chunk edited"
// or "Chunk removed" instead of one label for all of them. Two cases keep the
// unnamed kind: a batch that mixed actions, which no single kind describes, and an
// empty set, which names nothing.
func TestChunkVersionKindNamesTheAction(t *testing.T) {
	change := func(action string) entity.DatasetVersionChange {
		return chunkVersionChange(action, "doc-a", "chunk-1")
	}
	tests := []struct {
		name    string
		changes []entity.DatasetVersionChange
		want    string
	}{
		{
			name:    "add",
			changes: []entity.DatasetVersionChange{change(entity.DatasetVersionChangeActionAdd)},
			want:    entity.DatasetVersionKindChunkAdd,
		},
		{
			name: "update batch",
			changes: []entity.DatasetVersionChange{
				change(entity.DatasetVersionChangeActionUpdate),
				chunkVersionChange(entity.DatasetVersionChangeActionUpdate, "doc-a", "chunk-2"),
			},
			want: entity.DatasetVersionKindChunkUpdate,
		},
		{
			name:    "delete",
			changes: []entity.DatasetVersionChange{change(entity.DatasetVersionChangeActionDelete)},
			want:    entity.DatasetVersionKindChunkDelete,
		},
		{
			name:    "enable",
			changes: []entity.DatasetVersionChange{change(entity.DatasetVersionChangeActionEnable)},
			want:    entity.DatasetVersionKindChunkEnable,
		},
		{
			name:    "disable",
			changes: []entity.DatasetVersionChange{change(entity.DatasetVersionChangeActionDisable)},
			want:    entity.DatasetVersionKindChunkDisable,
		},
		{
			name: "mixed batch",
			changes: []entity.DatasetVersionChange{
				change(entity.DatasetVersionChangeActionUpdate),
				change(entity.DatasetVersionChangeActionDelete),
			},
			want: entity.DatasetVersionKindChunk,
		},
		{
			name:    "restore",
			changes: []entity.DatasetVersionChange{change(entity.DatasetVersionChangeActionRestore)},
			want:    entity.DatasetVersionKindChunkRestore,
		},
		{
			name:    "empty",
			changes: nil,
			want:    entity.DatasetVersionKindChunk,
		},
	}
	for _, test := range tests {
		if got := chunkVersionKind(test.changes); got != test.want {
			t.Errorf("%s: kind = %q, want %q", test.name, got, test.want)
		}
	}
}

// TestUpdateChunkImageAppendsVersionsWithDistinctImgIDs pins that a picture is
// part of the history: every upload records the image ID it produced, so two
// uploads of the same chunk are two versions even when the text does not
// change, and each ID keeps naming the object of its own version.
func TestUpdateChunkImageAppendsVersionsWithDistinctImgIDs(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	engine := &updateChunkTestEngine{
		existingChunk: map[string]interface{}{
			"doc_id":              "doc-a",
			"content_with_weight": "caption",
			"img_id":              "kb-1-chunk-1-original",
			"doc_type_kwd":        "image",
		},
	}
	svc := chunkVersionTestService(engine)
	returned := []string{"chunk-1-first", "chunk-1-second"}
	stored := 0
	var currentImageIDs []string
	svc.storeChunkImageFunc = func(bucket, chunkID, currentImageID string, imageBinary []byte, mode string) (string, error) {
		currentImageIDs = append(currentImageIDs, currentImageID)
		key := returned[stored]
		stored++
		return key, nil
	}

	imageBase64 := base64.StdEncoding.EncodeToString([]byte("image-bytes"))
	ctx := chunkTestContext(t, "user-1", "tenant-1")
	for i := 0; i < 2; i++ {
		if err := svc.UpdateChunk(ctx, &service.UpdateChunkRequest{
			DatasetID:   "kb-1",
			DocumentID:  "doc-a",
			ChunkID:     "chunk-1",
			ImageBase64: &imageBase64,
		}); err != nil {
			t.Fatalf("UpdateChunk #%d error = %v", i+1, err)
		}
	}

	rows := loadChunkVersions(t, "chunk-1")
	want := []string{"kb-1-chunk-1-original", "kb-1-chunk-1-first", "kb-1-chunk-1-second"}
	if len(rows) != len(want) {
		t.Fatalf("version rows = %d, want %d", len(rows), len(want))
	}
	for i, row := range rows {
		if got := row.Snapshot["img_id"]; got != want[i] {
			t.Fatalf("row %d img_id = %v, want %q", i, got, want[i])
		}
	}
	if len(currentImageIDs) != 2 || currentImageIDs[0] != "kb-1-chunk-1-original" {
		t.Fatalf("current image IDs = %v, want the existing picture on the first upload", currentImageIDs)
	}
}

// TestUpdateChunkSkipsVersionWhenSnapshotIsUnchanged keeps a no-op save from
// recording anything at all: no chunk version and no dataset version. A version
// names a change, and a save that hands the chunk back identical names none - the
// timeline must not report a move the dataset did not make, and the chunk's own
// history gains nothing to show for it either.
//
// The engine write still runs: a request can be about a field the snapshot leaves
// out, such as the searchable tag list.
func TestUpdateChunkSkipsVersionWhenSnapshotIsUnchanged(t *testing.T) {
	db := setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	body := "same body"
	engine := &updateChunkTestEngine{
		existingChunk: map[string]interface{}{
			"doc_id":              "doc-a",
			"content_with_weight": body,
			"available_int":       1,
		},
	}
	svc := chunkVersionTestService(engine)

	if err := svc.UpdateChunk(chunkTestContext(t, "user-1", "tenant-1"), &service.UpdateChunkRequest{
		DatasetID:  "kb-1",
		DocumentID: "doc-a",
		ChunkID:    "chunk-1",
		Content:    &body,
	}); err != nil {
		t.Fatalf("UpdateChunk() error = %v", err)
	}

	if len(engine.updateCalls) != 1 {
		t.Fatalf("engine writes = %d, want the save to still reach the engine", len(engine.updateCalls))
	}
	if rows := loadChunkVersions(t, "chunk-1"); len(rows) != 0 {
		t.Fatalf("version rows = %d, want none for a save that changed nothing", len(rows))
	}
	var versions []entity.DatasetVersion
	if err := db.Where("dataset_id = ?", "kb-1").Find(&versions).Error; err != nil {
		t.Fatalf("load dataset versions: %v", err)
	}
	if len(versions) != 0 {
		t.Fatalf("dataset versions = %d, want none for a save that changed nothing", len(versions))
	}
}

// TestUpdateChunkAnnotationsRewriteTheCurrentVersion pins that only the body,
// the picture and the visibility open a version: keyword, question and tag
// edits rewrite the version the chunk is on, record no dataset version, and a
// later body edit seeds and appends from the state they left.
func TestUpdateChunkAnnotationsRewriteTheCurrentVersion(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	engine := &updateChunkTestEngine{
		existingChunk: map[string]interface{}{
			"doc_id":              "doc-a",
			"content_with_weight": "body",
			"available_int":       1,
		},
	}
	svc := chunkVersionTestService(engine)
	ctx := chunkTestContext(t, "user-1", "tenant-1")

	// The mock engine keeps the row it was given, so the test applies each
	// edit to it the way the document engine would.
	applyEdit := func(update map[string]interface{}) {
		engine.existingChunk = mergeChunkRow(engine.existingChunk.(map[string]interface{}), update)
	}

	// A keyword edit on a chunk with no history records nothing: there is no
	// version to rewrite, and the row the next version is seeded from already
	// carries the keyword. A row here would need a dataset version to anchor it -
	// exactly the timeline entry this edit must not create.
	if err := svc.UpdateChunk(ctx, &service.UpdateChunkRequest{
		DatasetID:    "kb-1",
		DocumentID:   "doc-a",
		ChunkID:      "chunk-1",
		ImportantKwd: []string{"fresh keyword"},
	}); err != nil {
		t.Fatalf("keyword edit: %v", err)
	}
	applyEdit(map[string]interface{}{"important_kwd": []string{"fresh keyword"}})
	if rows := loadChunkVersions(t, "chunk-1"); len(rows) != 0 {
		t.Fatalf("after keyword edit rows = %#v, want none", rows)
	}
	// A keyword is an annotation of content that did not change, so the
	// dataset's own timeline gained nothing either: a version there would tell
	// the reader the dataset had moved when no chunk body did, and its change
	// list would name an edit it cannot describe.
	if versions := loadDatasetVersions(t, "kb-1"); len(versions) != 0 {
		t.Fatalf("dataset versions after keyword edit = %d, want none", len(versions))
	}

	// A question edit is an annotation too, and it lands in the same place: the
	// chunk still has no version to rewrite, so the next one is seeded from the
	// live row and carries it.
	if err := svc.UpdateChunk(ctx, &service.UpdateChunkRequest{
		DatasetID:  "kb-1",
		DocumentID: "doc-a",
		ChunkID:    "chunk-1",
		Questions:  []string{"why?"},
	}); err != nil {
		t.Fatalf("question edit: %v", err)
	}
	applyEdit(map[string]interface{}{"question_kwd": []string{"why?"}})
	if rows := loadChunkVersions(t, "chunk-1"); len(rows) != 0 {
		t.Fatalf("after question edit rows = %#v, want none", rows)
	}

	// A body edit is what opens the history: v0 is the pre-edit row - the
	// annotations included - and v1 the new body.
	body := "new body"
	if err := svc.UpdateChunk(ctx, &service.UpdateChunkRequest{
		DatasetID:  "kb-1",
		DocumentID: "doc-a",
		ChunkID:    "chunk-1",
		Content:    &body,
	}); err != nil {
		t.Fatalf("body edit: %v", err)
	}
	applyEdit(map[string]interface{}{"content_with_weight": "new body"})
	rows := loadChunkVersions(t, "chunk-1")
	if len(rows) != 2 || rows[0].Version != 0 || rows[1].Version != 1 {
		t.Fatalf("after body edit rows = %#v, want v0 and v1", rows)
	}
	if got := rows[0].Snapshot["content_with_weight"]; got != "body" {
		t.Fatalf("v0 content = %v, want the pre-edit body", got)
	}
	if got := rows[1].Snapshot["content_with_weight"]; got != "new body" {
		t.Fatalf("v1 content = %v, want new body", got)
	}
	// Both versions carry the annotations the chunk was left in: a version that
	// dropped them would lose the keyword or the question on restore.
	for _, row := range rows {
		if !reflect.DeepEqual(row.Snapshot["important_kwd"], []interface{}{"fresh keyword"}) {
			t.Fatalf("v%d keyword = %#v, want the edited keyword", row.Version, row.Snapshot["important_kwd"])
		}
		if !reflect.DeepEqual(row.Snapshot["question_kwd"], []interface{}{"why?"}) {
			t.Fatalf("v%d question = %#v, want the edited question", row.Version, row.Snapshot["question_kwd"])
		}
	}
	// The body edit is the one that moved the dataset: exactly one version,
	// recorded as the chunk update it is, anchored on the appended v1. The
	// dataset's first write also seeds its baseline (version 0), which is the
	// state it started in rather than a change anyone made.
	versions := loadDatasetVersions(t, "kb-1")
	changes := loadDatasetChanges(t, "kb-1")
	if len(changes) != 1 {
		t.Fatalf("dataset changes after body edit = %d (%+v), want 1", len(changes), versions)
	}
	if changes[0].Kind != entity.DatasetVersionKindChunkUpdate {
		t.Fatalf("dataset version kind = %q, want %q", changes[0].Kind, entity.DatasetVersionKindChunkUpdate)
	}
	if rows[1].DatasetVersion != changes[0].Version {
		t.Fatalf("v1 dataset_version = %d, want the recorded version %d", rows[1].DatasetVersion, changes[0].Version)
	}

	// A keyword edit on a chunk that already has a body version rewrites that
	// version in place: the chunk stays at v1 and the dataset motion stays at
	// the one the body edit made.
	if err := svc.UpdateChunk(ctx, &service.UpdateChunkRequest{
		DatasetID:    "kb-1",
		DocumentID:   "doc-a",
		ChunkID:      "chunk-1",
		ImportantKwd: []string{"second keyword"},
	}); err != nil {
		t.Fatalf("second keyword edit: %v", err)
	}
	rows = loadChunkVersions(t, "chunk-1")
	if len(rows) != 2 || rows[1].Version != 1 {
		t.Fatalf("after second keyword edit rows = %#v, want v0 and v1 still", rows)
	}
	if !reflect.DeepEqual(rows[1].Snapshot["important_kwd"], []interface{}{"second keyword"}) {
		t.Fatalf("v1 keyword = %#v, want the rewritten keyword", rows[1].Snapshot["important_kwd"])
	}
	if got := len(loadDatasetVersions(t, "kb-1")); got != len(versions) {
		t.Fatalf("dataset versions after second keyword edit = %d, want the %d the body edit left", got, len(versions))
	}
}

// TestUpdateChunkVisibilityOpensVersions pins that the switch is versioned: a
// toggle appends a chunk version, records a dataset version, shows up in the
// diff and comes back with a restore. An in-place annotation rewrite could not
// do any of that - it would replace the state the current version recorded, so
// the history could never answer whether the chunk was hidden at a given version.
func TestUpdateChunkVisibilityOpensVersions(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	engine := &updateChunkTestEngine{
		existingChunk: map[string]interface{}{
			"doc_id":              "doc-a",
			"content_with_weight": "body",
			// A parsed row carries its tokens and vector; the baseline version is
			// recorded from it, and a replay copies them rather than deriving.
			"content_ltks":  "body",
			"q_3_vec":       []float64{1, 2, 3},
			"available_int": 1,
		},
	}
	svc := chunkVersionTestService(engine)
	ctx := chunkVersionManageContext(t, "tenant-1", "user-1", "doc-a")
	applyEdit := func(update map[string]interface{}) {
		engine.existingChunk = mergeChunkRow(engine.existingChunk.(map[string]interface{}), update)
	}

	// Disabling the chunk opens the history: v0 is the visible row the parser
	// left, v1 the hidden state.
	disabled := false
	if err := svc.UpdateChunk(ctx, &service.UpdateChunkRequest{
		DatasetID:  "kb-1",
		DocumentID: "doc-a",
		ChunkID:    "chunk-1",
		Available:  &disabled,
	}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	applyEdit(map[string]interface{}{"available_int": 0})
	rows := loadChunkVersions(t, "chunk-1")
	if len(rows) != 2 || rows[0].Version != 0 || rows[1].Version != 1 {
		t.Fatalf("after disabling rows = %#v, want v0 and v1", rows)
	}
	if visible, ok := common.GetInt(rows[0].Snapshot["available_int"]); !ok || visible != 1 {
		t.Fatalf("v0 available_int = %v (present %v), want the visible state", rows[0].Snapshot["available_int"], ok)
	}
	if stored, ok := common.GetInt(rows[1].Snapshot["available_int"]); !ok || stored != 0 {
		t.Fatalf("v1 available_int = %v, want the hidden state", rows[1].Snapshot["available_int"])
	}
	if changes := loadDatasetChanges(t, "kb-1"); len(changes) != 1 || changes[0].Kind != entity.DatasetVersionKindChunkUpdate {
		t.Fatalf("dataset changes after disabling = %#v, want one chunk update", changes)
	}

	// Re-enabling is a version of its own: the history keeps both states instead
	// of overwriting the one it had.
	enabled := true
	if err := svc.UpdateChunk(ctx, &service.UpdateChunkRequest{
		DatasetID:  "kb-1",
		DocumentID: "doc-a",
		ChunkID:    "chunk-1",
		Available:  &enabled,
	}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	applyEdit(map[string]interface{}{"available_int": 1})
	rows = loadChunkVersions(t, "chunk-1")
	if len(rows) != 3 || rows[2].Version != 2 {
		t.Fatalf("after enabling rows = %#v, want v0, v1 and v2", rows)
	}
	if visible, ok := common.GetInt(rows[2].Snapshot["available_int"]); !ok || visible != 1 {
		t.Fatalf("v2 available_int = %v (present %v), want the visible state", rows[2].Snapshot["available_int"], ok)
	}
	if changes := loadDatasetChanges(t, "kb-1"); len(changes) != 2 {
		t.Fatalf("dataset changes after two toggles = %d, want 2", len(changes))
	}
	// A toggle names no body change: every version still holds the same text, so
	// a restore rebuilds the same chunk with a different switch.
	for _, row := range rows {
		if got, _ := row.Snapshot["content_with_weight"].(string); got != "body" {
			t.Fatalf("v%d content = %q, want the untouched body", row.Version, got)
		}
	}

	// The comparison reports the switch, from hidden to visible.
	to := 2
	diff, err := svc.DiffVersions(ctx, &service.ChunkVersionDiffRequest{
		DatasetID: "kb-1", DocumentID: "doc-a", ChunkID: "chunk-1", From: 1, To: &to,
	})
	if err != nil {
		t.Fatalf("DiffVersions() error = %v", err)
	}
	var availability *service.ChunkVersionFieldDiff
	for i := range diff.Fields {
		if diff.Fields[i].Field == "available_int" {
			availability = &diff.Fields[i]
		}
	}
	if availability == nil || !availability.Changed {
		t.Fatalf("diff fields = %#v, want the visibility change", diff.Fields)
	}
	if availability.Before != 0 || availability.After != 1 {
		t.Fatalf("visibility diff = %#v, want 0 -> 1", availability)
	}

	// Restoring the hidden version puts the switch back: a restore writes the
	// state the version recorded, not the one the chunk is in now.
	if _, err := svc.RestoreVersion(ctx, &service.ChunkVersionRequest{
		DatasetID:  "kb-1",
		DocumentID: "doc-a",
		ChunkID:    "chunk-1",
		Version:    1,
	}); err != nil {
		t.Fatalf("RestoreVersion() error = %v", err)
	}
	if len(engine.updateCalls) == 0 {
		t.Fatal("restore did not reach the document engine")
	}
	restored := engine.updateCalls[len(engine.updateCalls)-1].newValue
	if stored, ok := common.GetInt(restored["available_int"]); !ok || stored != 0 {
		t.Fatalf("restored available_int = %v, want the hidden state", restored["available_int"])
	}
}

// TestRestoreChunkVersionWritesSnapshotAndAppendsVersion covers a restore: the
// stored snapshot is written back onto the live chunk, and the restore itself
// becomes the newest version without rewriting the source row.
func TestRestoreChunkVersionWritesSnapshotAndAppendsVersion(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	engine := &updateChunkTestEngine{
		existingChunk: map[string]interface{}{
			"doc_id":              "doc-a",
			"content_with_weight": "v1 body",
			"available_int":       1,
			"important_kwd":       []interface{}{"new"},
		},
	}
	svc := chunkVersionTestService(engine)

	versionDAO := dao.NewChunkVersionDAO()
	for _, row := range []*entity.ChunkVersion{
		{ChunkID: "chunk-1", Version: 0, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1, Snapshot: fullTestSnapshot(entity.JSONMap{
			"content_with_weight": "v0 body",
			"available_int":       0,
			"important_kwd":       []interface{}{"old"},
			"tag_kwd":             []interface{}{"t1", "t2"},
			"img_id":              "kb-1-chunk-1-oldhash",
			"doc_type_kwd":        "image",
		})},
		{ChunkID: "chunk-1", Version: 1, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1, Snapshot: fullTestSnapshot(entity.JSONMap{
			"content_with_weight": "v1 body",
			"available_int":       1,
			"important_kwd":       []interface{}{"new"},
		})},
	} {
		if err := versionDAO.Create(context.Background(), dao.DB, row); err != nil {
			t.Fatalf("seed version %d: %v", row.Version, err)
		}
	}

	var wikiDirtyCalls [][]string
	svc.markWikiDirtyFunc = func(_, _, _ string, chunkIDs []string) {
		wikiDirtyCalls = append(wikiDirtyCalls, chunkIDs)
	}

	resp, err := svc.RestoreVersion(chunkVersionManageContext(t, "tenant-1", "user-1", "doc-a"), &service.ChunkVersionRequest{
		DatasetID:  "kb-1",
		DocumentID: "doc-a",
		ChunkID:    "chunk-1",
		Version:    0,
	})
	if err != nil {
		t.Fatalf("RestoreVersion() error = %v", err)
	}
	if resp.Version != 2 {
		t.Fatalf("restored version = %d, want 2", resp.Version)
	}

	if len(engine.updateCalls) != 1 {
		t.Fatalf("UpdateChunks calls = %d, want 1", len(engine.updateCalls))
	}
	update := engine.updateCalls[0].newValue
	if update["content_with_weight"] != "v0 body" {
		t.Fatalf("restored content = %v, want v0 body", update["content_with_weight"])
	}
	// Visibility is part of a version, so the restore puts the switch back: v0
	// recorded a hidden chunk, and leaving the live state alone would show a
	// version the reader can no longer reach.
	if stored, ok := common.GetInt(update["available_int"]); !ok || stored != 0 {
		t.Fatalf("restored available_int = %v, want the hidden state v0 recorded", update["available_int"])
	}
	if !reflect.DeepEqual(update["important_kwd"], []string{"old"}) {
		t.Fatalf("restored important_kwd = %#v, want [old]", update["important_kwd"])
	}
	// Fields the snapshot lacks are restored as empty lists, so a keyword or tag
	// added after the version is cleared rather than left behind.
	if _, ok := update["question_kwd"].([]string); !ok {
		t.Fatalf("restored question_kwd = %#v, want an empty list", update["question_kwd"])
	}
	if !reflect.DeepEqual(update["tag_kwd"], []string{"t1", "t2"}) {
		t.Fatalf("restored tag_kwd = %#v, want the version's tags", update["tag_kwd"])
	}
	// The picture the version recorded is put back by ID: image objects are
	// content-addressed, so the ID still names the bytes of that version.
	if update["img_id"] != "kb-1-chunk-1-oldhash" {
		t.Fatalf("restored img_id = %#v, want kb-1-chunk-1-oldhash", update["img_id"])
	}
	if update["doc_type_kwd"] != "image" {
		t.Fatalf("restored doc_type_kwd = %#v, want image", update["doc_type_kwd"])
	}

	rows := loadChunkVersions(t, "chunk-1")
	if len(rows) != 3 {
		t.Fatalf("version rows = %d, want 3", len(rows))
	}
	appended := rows[2]
	if appended.Version != 2 || appended.RestoredFrom != 0 {
		t.Fatalf("appended row = v%d from v%d, want v2 from v0", appended.Version, appended.RestoredFrom)
	}
	if appended.Snapshot["content_with_weight"] != "v0 body" {
		t.Fatalf("appended content = %v, want v0 body", appended.Snapshot["content_with_weight"])
	}
	if appended.Snapshot["img_id"] != "kb-1-chunk-1-oldhash" {
		t.Fatalf("appended img_id = %v, want the restored picture", appended.Snapshot["img_id"])
	}
	// The restore is a version of its own kind, so the timeline reads "Chunk
	// restored" instead of the unnamed "Chunk changed" an unknown action falls
	// back to.
	var recordedDatasetVersion entity.DatasetVersion
	if err := dao.DB.Where("dataset_id = ? AND version = ?", "kb-1", appended.DatasetVersion).
		Take(&recordedDatasetVersion).Error; err != nil {
		t.Fatalf("the dataset version the restore is anchored at was not recorded: %v", err)
	}
	if recordedDatasetVersion.Kind != entity.DatasetVersionKindChunkRestore {
		t.Fatalf("dataset version kind = %q, want %q",
			recordedDatasetVersion.Kind, entity.DatasetVersionKindChunkRestore)
	}

	if len(wikiDirtyCalls) != 1 || !reflect.DeepEqual(wikiDirtyCalls[0], []string{"chunk-1"}) {
		t.Fatalf("wiki dirty calls = %v, want one call for chunk-1", wikiDirtyCalls)
	}
}

// TestRestoreChunkVersionHonoursADisabledDocument pins the second gate on
// visibility: the chunk's own switch is only half of it. A document is enabled or
// disabled as a whole, so a version that recorded the chunk as visible must not
// put it back into retrieval while the document it belongs to is switched off.
func TestRestoreChunkVersionHonoursADisabledDocument(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")
	disabled := string(entity.StatusInvalid)
	if err := dao.DB.Model(&entity.Document{}).Where("id = ?", "doc-a").UpdateColumn("status", disabled).Error; err != nil {
		t.Fatalf("disable the document: %v", err)
	}

	engine := &updateChunkTestEngine{
		existingChunk: map[string]interface{}{
			"doc_id":              "doc-a",
			"content_with_weight": "live body",
			"available_int":       0,
		},
	}
	svc := chunkVersionTestService(engine)
	// The version recorded nothing about visibility, which describes a visible
	// chunk: only the document's switch keeps it out of retrieval.
	if err := dao.NewChunkVersionDAO().Create(context.Background(), dao.DB, &entity.ChunkVersion{
		ChunkID: "chunk-1", Version: 0, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1,
		Snapshot: fullTestSnapshot(entity.JSONMap{
			"content_with_weight": "old body",
			"img_id":              "",
		}),
	}); err != nil {
		t.Fatalf("seed version: %v", err)
	}

	if _, err := svc.RestoreVersion(chunkVersionManageContext(t, "tenant-1", "user-1", "doc-a"), &service.ChunkVersionRequest{
		DatasetID:  "kb-1",
		DocumentID: "doc-a",
		ChunkID:    "chunk-1",
		Version:    0,
	}); err != nil {
		t.Fatalf("RestoreVersion() error = %v", err)
	}
	if len(engine.updateCalls) == 0 {
		t.Fatal("restore did not reach the document engine")
	}
	written := engine.updateCalls[len(engine.updateCalls)-1].newValue
	if got, _ := written["content_with_weight"].(string); got != "old body" {
		t.Fatalf("restored content = %q, want the recorded body", got)
	}
	if stored, ok := common.GetInt(written["available_int"]); !ok || stored != 0 {
		t.Fatalf("restored available_int = %v, want hidden while the document is disabled", written["available_int"])
	}
}

// TestRestoreChunkVersionRejectsForeignScope pins that a version recorded for
// another document or dataset is not restorable through this route.
func TestRestoreChunkVersionRejectsForeignScope(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	engine := &updateChunkTestEngine{
		existingChunk: map[string]interface{}{
			"doc_id":              "doc-a",
			"content_with_weight": "body",
		},
	}
	svc := chunkVersionTestService(engine)

	if err := dao.NewChunkVersionDAO().Create(context.Background(), dao.DB, &entity.ChunkVersion{
		ChunkID: "chunk-1", Version: 0, DocID: "doc-b", DatasetID: "kb-2", DatasetVersion: 1,
		Snapshot: entity.JSONMap{"content_with_weight": "other"},
	}); err != nil {
		t.Fatalf("seed version: %v", err)
	}

	_, err := svc.RestoreVersion(chunkVersionManageContext(t, "tenant-1", "user-1", "doc-a"), &service.ChunkVersionRequest{
		DatasetID:  "kb-1",
		DocumentID: "doc-a",
		ChunkID:    "chunk-1",
		Version:    0,
	})
	if err == nil || !strings.Contains(err.Error(), "version not found") {
		t.Fatalf("RestoreVersion() error = %v, want version not found", err)
	}
	if len(engine.updateCalls) != 0 {
		t.Fatalf("UpdateChunks calls = %d, want 0", len(engine.updateCalls))
	}
}

// TestListAndGetChunkVersionShape pins the list/detail payloads the version
// panel renders: newest first, only the highest row marked latest, and the
// snapshot exposed in the chunk shape.
func TestListAndGetChunkVersionShape(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	versionDAO := dao.NewChunkVersionDAO()
	for _, seed := range []struct {
		version int
		kind    string
		content string
	}{
		{version: 0, kind: entity.ChunkVersionKindParse, content: "v0 body"},
		{version: 1, kind: entity.ChunkVersionKindEdit, content: "v1 body"},
	} {
		if err := versionDAO.Create(context.Background(), dao.DB, &entity.ChunkVersion{
			ChunkID: "chunk-1", Version: seed.version,
			DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1, CreatedBy: "user-1",
			Kind:     seed.kind,
			Snapshot: entity.JSONMap{"content_with_weight": seed.content},
		}); err != nil {
			t.Fatalf("seed version %d: %v", seed.version, err)
		}
	}

	svc := &ChunkService{kbDAO: dao.NewKnowledgebaseDAO(), documentDAO: dao.NewDocumentDAO()}
	ctx := chunkTestContext(t, "user-1", "tenant-1")

	list, err := svc.ListVersions(ctx, &service.ChunkVersionRequest{
		DatasetID: "kb-1", DocumentID: "doc-a", ChunkID: "chunk-1",
	})
	if err != nil {
		t.Fatalf("ListVersions() error = %v", err)
	}
	if list.Total != 2 || len(list.Versions) != 2 {
		t.Fatalf("list = %#v, want two versions", list)
	}
	if list.Versions[0].Version != 1 || !list.Versions[0].Latest {
		t.Fatalf("first entry = %#v, want v1 marked latest", list.Versions[0])
	}
	if list.Versions[1].Version != 0 || list.Versions[1].Latest {
		t.Fatalf("second entry = %#v, want v0 not latest", list.Versions[1])
	}
	if list.Versions[0].CreateTime <= 0 {
		t.Fatalf("first entry create_time = %d, want raw millis", list.Versions[0].CreateTime)
	}
	// create_date stays for older clients, but it must not reintroduce the
	// RFC3339 'T' the version panel used to show.
	if list.Versions[0].CreateDate == "" || strings.Contains(list.Versions[0].CreateDate, "T") {
		t.Fatalf("first entry create_date = %q, want a no-T display string", list.Versions[0].CreateDate)
	}
	// The list has to carry the row kind: the panel mirrors the server's refusal
	// to delete a parse/add/delete row off this field, so an empty kind would
	// offer a delete the server then rejects.
	if list.Versions[0].Kind != entity.ChunkVersionKindEdit {
		t.Fatalf("first entry kind = %q, want edit", list.Versions[0].Kind)
	}
	if list.Versions[1].Kind != entity.ChunkVersionKindParse {
		t.Fatalf("second entry kind = %q, want parse", list.Versions[1].Kind)
	}

	detail, err := svc.GetVersion(ctx, &service.ChunkVersionRequest{
		DatasetID: "kb-1", DocumentID: "doc-a", ChunkID: "chunk-1", Version: 0,
	})
	if err != nil {
		t.Fatalf("GetVersion() error = %v", err)
	}
	if detail.Version != 0 || detail.Latest {
		t.Fatalf("detail = %#v, want v0 not latest", detail)
	}
	if detail.CreateTime <= 0 || detail.CreateTime != list.Versions[1].CreateTime {
		t.Fatalf("detail create_time = %d, want %d from the matching list row",
			detail.CreateTime, list.Versions[1].CreateTime)
	}
	if detail.Kind != entity.ChunkVersionKindParse {
		t.Fatalf("detail kind = %q, want parse", detail.Kind)
	}
	if detail.Chunk["content_with_weight"] != "v0 body" {
		t.Fatalf("detail content = %v, want v0 body", detail.Chunk["content_with_weight"])
	}

	// The detail and the list have to name the same current version; a panel that
	// says "latest" on one and not the other hides the restore button.
	current, err := svc.GetVersion(ctx, &service.ChunkVersionRequest{
		DatasetID: "kb-1", DocumentID: "doc-a", ChunkID: "chunk-1", Version: 1,
	})
	if err != nil {
		t.Fatalf("GetVersion(v1) error = %v", err)
	}
	if !current.Latest || !list.Versions[0].Latest {
		t.Fatalf("latest disagrees: detail=%v list=%v, want both on v1",
			current.Latest, list.Versions[0].Latest)
	}

	if _, err = svc.GetVersion(ctx, &service.ChunkVersionRequest{
		DatasetID: "kb-1", DocumentID: "doc-a", ChunkID: "chunk-1", Version: 9,
	}); err == nil || !strings.Contains(err.Error(), "version not found") {
		t.Fatalf("GetVersion(missing) error = %v, want version not found", err)
	}
}

// TestChunkVersionActorResolvesNickname pins the actor resolution of both read
// paths: a row stores the actor's user id, and the history names the user so the
// list does not show a raw id. A system-written row (the parse baseline, a
// tombstone) has no user to name, and a deleted user cannot be resolved, so both
// stay empty and the client falls back to the id it has.
func TestChunkVersionActorResolvesNickname(t *testing.T) {
	db := setupChunkVersionTestDB(t)
	if err := db.AutoMigrate(&entity.User{}); err != nil {
		t.Fatalf("migrate users: %v", err)
	}

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")
	if err := dao.DB.Create(&entity.User{
		ID: "actor-1", Nickname: "Alice", Email: "alice@test.local",
		IsAuthenticated: "1", IsActive: "1", IsAnonymous: "0",
	}).Error; err != nil {
		t.Fatalf("insert actor: %v", err)
	}

	versionDAO := dao.NewChunkVersionDAO()
	for _, row := range []*entity.ChunkVersion{
		{Version: 0, CreatedBy: "system", Kind: entity.ChunkVersionKindParse},
		{Version: 1, CreatedBy: "actor-1", Kind: entity.ChunkVersionKindEdit},
		{Version: 2, CreatedBy: "no-such-user", Kind: entity.ChunkVersionKindEdit},
	} {
		row.ChunkID = "chunk-1"
		row.DocID = "doc-a"
		row.DatasetID = "kb-1"
		row.DatasetVersion = 1
		row.Snapshot = entity.JSONMap{"content_with_weight": "body"}
		if err := versionDAO.Create(context.Background(), dao.DB, row); err != nil {
			t.Fatalf("seed version %d: %v", row.Version, err)
		}
	}

	svc := &ChunkService{kbDAO: dao.NewKnowledgebaseDAO(), documentDAO: dao.NewDocumentDAO()}
	ctx := chunkTestContext(t, "user-1", "tenant-1")

	list, err := svc.ListVersions(ctx, &service.ChunkVersionRequest{
		DatasetID: "kb-1", DocumentID: "doc-a", ChunkID: "chunk-1",
	})
	if err != nil {
		t.Fatalf("ListVersions() error = %v", err)
	}
	names := make(map[int]string, len(list.Versions))
	for _, item := range list.Versions {
		names[item.Version] = item.CreatedByName
	}
	if names[1] != "Alice" {
		t.Fatalf("v1 created_by_name = %q, want the nickname", names[1])
	}
	if names[0] != "" {
		t.Fatalf("v0 created_by_name = %q, want empty for a system written row", names[0])
	}
	if names[2] != "" {
		t.Fatalf("v2 created_by_name = %q, want empty for an unresolvable actor", names[2])
	}

	detail, err := svc.GetVersion(ctx, &service.ChunkVersionRequest{
		DatasetID: "kb-1", DocumentID: "doc-a", ChunkID: "chunk-1", Version: 1,
	})
	if err != nil {
		t.Fatalf("GetVersion() error = %v", err)
	}
	if detail.CreatedByName != "Alice" {
		t.Fatalf("detail created_by_name = %q, want the nickname", detail.CreatedByName)
	}
	if detail.CreatedBy != "actor-1" {
		t.Fatalf("detail created_by = %q, want the raw id kept for reference", detail.CreatedBy)
	}
}

// TestRemoveChunksKeepsVersionHistory pins that deleting chunks keeps the
// history describing them: a dataset content version recorded before the delete
// may need to put a chunk back. The retention purge, covered here too, is what
// finally drops the rows.
func TestRemoveChunksKeepsVersionHistory(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	engine := &parseTestDocEngine{deleteChunksCount: 1}
	svc := &ChunkService{
		docEngine:   engine,
		kbDAO:       dao.NewKnowledgebaseDAO(),
		documentDAO: dao.NewDocumentDAO(),
		markWikiDirtyFunc: func(string, string, string, []string) {
		},
	}

	// The seeded row carries the version it was written under, so "before the
	// delete" is unambiguous no matter how fast the test runs: the anchor is a
	// number, not a clock reading.
	beforeDelete := int64(1)
	versionDAO := dao.NewChunkVersionDAO()
	if err := versionDAO.Create(context.Background(), dao.DB, &entity.ChunkVersion{
		ChunkID: "chunk-1", Version: 0, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: beforeDelete,
		Kind:     entity.ChunkVersionKindParse,
		Snapshot: entity.JSONMap{"content_with_weight": "body"},
	}); err != nil {
		t.Fatalf("seed version: %v", err)
	}
	// The dataset version the parse was recorded as: the delete below has to take
	// a later number, or the tombstone would sort beside the row it buries.
	if err := dao.DB.Create(&entity.DatasetVersion{
		DatasetID: "kb-1", Version: beforeDelete, Kind: entity.DatasetVersionKindParse,
		CreateTime: func() *int64 { moment := int64(1_000_000_000_000); return &moment }(),
	}).Error; err != nil {
		t.Fatalf("seed dataset version: %v", err)
	}

	if _, err := svc.RemoveChunks(chunkTestContext(t, "user-1", "tenant-1"), &service.RemoveChunksRequest{
		DatasetID: "kb-1",
		DocID:     "doc-a",
		ChunkIDs:  []string{"chunk-1"},
	}); err != nil {
		t.Fatalf("RemoveChunks() error = %v", err)
	}
	// The history survives the delete, and the removal appends a tombstone on top
	// of it: the snapshot is what a version recorded before the delete asks back,
	// and the tombstone is what makes a later anchor show the chunk as gone. The
	// retention purge is what finally drops both rows.
	rows := loadChunkVersions(t, "chunk-1")
	if len(rows) != 2 {
		t.Fatalf("version rows after delete = %d, want 2 (the snapshot and its tombstone)", len(rows))
	}
	if rows[1].Kind != entity.ChunkVersionKindDelete {
		t.Fatalf("newest version kind = %q, want %q", rows[1].Kind, entity.ChunkVersionKindDelete)
	}
	// The tombstone carries the dataset version the delete was recorded under,
	// and the row it buries sits at an earlier one: that gap is what tells the
	// two anchors apart.
	if rows[1].DatasetVersion <= beforeDelete {
		t.Fatalf("tombstone dataset version = %d, want it after %d", rows[1].DatasetVersion, beforeDelete)
	}
	afterDelete := rows[1].DatasetVersion

	replayed, err := chunkversion.StatesAt(context.Background(), dao.DB, "doc-a", beforeDelete)
	if err != nil {
		t.Fatalf("StatesAt(before delete) error = %v", err)
	}
	if len(replayed) != 1 || replayed[0].ChunkID != "chunk-1" || !replayed[0].Exists {
		t.Fatalf("StatesAt(before delete) = %+v, want the chunk present", replayed)
	}
	replayed, err = chunkversion.StatesAt(context.Background(), dao.DB, "doc-a", afterDelete)
	if err != nil {
		t.Fatalf("StatesAt(after delete) error = %v", err)
	}
	if len(replayed) != 1 || replayed[0].Exists {
		t.Fatalf("StatesAt(after delete) = %+v, want the chunk absent", replayed)
	}

	if err := chunkversion.PurgeDocumentHistoryEE(chunkTestContext(t, "user-1", "tenant-1"), dao.DB, "kb-1", "doc-a"); err != nil {
		t.Fatalf("chunkversion.PurgeDocumentHistoryEE() error = %v", err)
	}
	if rows := loadChunkVersions(t, "chunk-1"); len(rows) != 0 {
		t.Fatalf("version rows after purge = %d, want 0", len(rows))
	}
}

// TestRemoveChunksKeepsVersionImages pins that the pictures the history holds
// survive the delete with it: image objects are content-addressed, so the row is
// the only reference an old picture has, and a restore may need it. The purge
// removes both once the owner deletes the removal row.
func TestRemoveChunksKeepsVersionImages(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	mockStorage := &chunkImageStorage{exists: true}
	factory := storage.GetStorageFactory()
	originalStorage := factory.GetStorage()
	factory.SetStorage(mockStorage)
	t.Cleanup(func() {
		factory.SetStorage(originalStorage)
	})

	engine := &parseTestDocEngine{deleteChunksCount: 1}
	svc := &ChunkService{
		docEngine:   engine,
		kbDAO:       dao.NewKnowledgebaseDAO(),
		documentDAO: dao.NewDocumentDAO(),
		markWikiDirtyFunc: func(string, string, string, []string) {
		},
	}

	versionDAO := dao.NewChunkVersionDAO()
	for _, row := range []*entity.ChunkVersion{
		{ChunkID: "chunk-1", Version: 0, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1,
			Snapshot: entity.JSONMap{"content_with_weight": "body", "img_id": "kb-1-chunk-1-oldhash"}},
		{ChunkID: "chunk-1", Version: 1, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1,
			Snapshot: entity.JSONMap{"content_with_weight": "body", "img_id": "kb-1-chunk-1-newhash"}},
	} {
		if err := versionDAO.Create(context.Background(), dao.DB, row); err != nil {
			t.Fatalf("seed version %d: %v", row.Version, err)
		}
	}

	if _, err := svc.RemoveChunks(chunkTestContext(t, "user-1", "tenant-1"), &service.RemoveChunksRequest{
		DatasetID: "kb-1",
		DocID:     "doc-a",
		ChunkIDs:  []string{"chunk-1"},
	}); err != nil {
		t.Fatalf("RemoveChunks() error = %v", err)
	}

	// The pictures travel with the history: they stay until the owner deletes the
	// removal row, because a restore may point the chunk at them again.
	if got := append([]string(nil), mockStorage.removeNames...); len(got) != 0 {
		t.Fatalf("removed objects after delete = %v, want none", got)
	}

	if err := chunkversion.PurgeDocumentHistoryEE(chunkTestContext(t, "user-1", "tenant-1"), dao.DB, "kb-1", "doc-a"); err != nil {
		t.Fatalf("chunkversion.PurgeDocumentHistoryEE() error = %v", err)
	}
	got := append([]string(nil), mockStorage.removeNames...)
	sort.Strings(got)
	removed := make(map[string]bool, len(got))
	for _, name := range got {
		removed[name] = true
	}
	if want := []string{"chunk-1-newhash", "chunk-1-oldhash"}; !removed[want[0]] || !removed[want[1]] {
		t.Fatalf("removed objects after purge = %v, want %v among them", got, want)
	}
	// The states the rows named live in the document engine now, one document per
	// version, and the whole history going takes them with it.
	if ids := chunkVersionTestStore.DocumentIDs("kb-1"); len(ids) != 0 {
		t.Fatalf("the engine still holds %v after the purge", ids)
	}
}

// TestChunkSnapshotDiffSkipsPositions pins that coordinates are versioned and
// restored but never compared: they describe the source region the chunk was
// parsed from, which an edit does not move, so a "changed" row for them would be
// a coordinate dump nobody can act on.
func TestChunkSnapshotDiffSkipsPositions(t *testing.T) {
	from := entity.JSONMap{
		"content_with_weight": "same body",
		"position_int":        []interface{}{[]interface{}{1.0, 0.0, 10.0, 0.0, 5.0}},
	}
	to := entity.JSONMap{
		"content_with_weight": "same body",
		"position_int":        []interface{}{[]interface{}{2.0, 5.0, 20.0, 30.0, 40.0}},
	}

	// Coordinates alone are not a change: a re-parse that moved the box with no
	// text behind it has nothing for a reader to review.
	fields, identical := chunkSnapshotDiff(from, to)
	if !identical {
		t.Fatal("a coordinates-only difference was reported as a change")
	}
	if len(fields) != 0 {
		t.Fatalf("fields = %#v, want none", fields)
	}

	// A real change still leaves the coordinates out of the field list.
	to["content_with_weight"] = "new body"
	fields, identical = chunkSnapshotDiff(from, to)
	if identical {
		t.Fatal("a rewritten body was reported as identical")
	}
	for _, field := range fields {
		if field.Field == "position_int" {
			t.Fatalf("fields = %#v, want no position_int row", fields)
		}
	}
}

// TestChunkSnapshotDiffSkipsImageOnTextChunks: only picture chunks compare img_id.
func TestChunkSnapshotDiffSkipsImageOnTextChunks(t *testing.T) {
	from := entity.JSONMap{"content_with_weight": "same body", "doc_type_kwd": "text"}
	to := entity.JSONMap{"content_with_weight": "same body", "doc_type_kwd": "text", "img_id": ""}

	fields, identical := chunkSnapshotDiff(from, to)
	if !identical {
		t.Fatal("a text chunk's empty img_id was reported as a change")
	}
	if len(fields) != 0 {
		t.Fatalf("fields = %#v, want none", fields)
	}

	// The same difference on a picture chunk is a change the reader can undo.
	from["doc_type_kwd"] = "image"
	from["img_id"] = "kb-1-chunk-picture-abc123"
	to["doc_type_kwd"] = "image"
	to["img_id"] = "kb-1-chunk-picture-def456"

	fields, identical = chunkSnapshotDiff(from, to)
	if identical {
		t.Fatal("a picture chunk whose image moved was reported as identical")
	}
	var reported bool
	for _, field := range fields {
		if field.Field == "img_id" {
			reported = true
		}
	}
	if !reported {
		t.Fatalf("fields = %#v, want an img_id row", fields)
	}
}

// TestChunkSnapshotDiffReportsImageOnTypeChange pins the boundary: a chunk that
// became a picture chunk has an image to compare even if its older version was
// plain text.
func TestChunkSnapshotDiffReportsImageOnTypeChange(t *testing.T) {
	from := entity.JSONMap{"content_with_weight": "body", "doc_type_kwd": "text"}
	to := entity.JSONMap{
		"content_with_weight": "body",
		"doc_type_kwd":        "image",
		"img_id":              "kb-1-chunk-picture-abc123",
	}

	fields, identical := chunkSnapshotDiff(from, to)
	if identical {
		t.Fatal("a chunk that became a picture chunk was reported as identical")
	}
	var reported bool
	for _, field := range fields {
		if field.Field == "img_id" {
			reported = true
		}
	}
	if !reported {
		t.Fatalf("fields = %#v, want an img_id row", fields)
	}
}

func TestSameChunkSnapshotComparesNumericTypes(t *testing.T) {
	engineRow := entity.JSONMap{"available_int": int64(1), "important_kwd": []interface{}{"a"}}
	stored := entity.JSONMap{"available_int": float64(1), "important_kwd": []interface{}{"a"}}

	if !sameChunkSnapshot(engineRow, stored) {
		t.Fatalf("sameChunkSnapshot(%v, %v) = false, want true", engineRow, stored)
	}
	if sameChunkSnapshot(engineRow, entity.JSONMap{"available_int": float64(0)}) {
		t.Fatalf("sameChunkSnapshot treated different values as equal")
	}
}

// TestChunkSnapshotDiffIgnoresUnrecordedVisibility pins the normalization the
// comparison applies: only a hidden row records available_int, so a version
// written before the chunk was ever toggled carries no key and still describes a
// visible chunk. Reporting "absent against 1" would call that a change.
func TestChunkSnapshotDiffIgnoresUnrecordedVisibility(t *testing.T) {
	fields, identical := chunkSnapshotDiff(
		entity.JSONMap{"content_with_weight": "body"},
		entity.JSONMap{"content_with_weight": "body", "available_int": float64(1)},
	)
	if !identical || len(fields) != 0 {
		t.Fatalf("diff = %#v (identical %v), want no change between a visible chunk and a version that stored the 1", fields, identical)
	}

	fields, identical = chunkSnapshotDiff(
		entity.JSONMap{"content_with_weight": "body"},
		entity.JSONMap{"content_with_weight": "body", "available_int": float64(0)},
	)
	if identical || len(fields) != 1 || fields[0].Field != "available_int" {
		t.Fatalf("diff = %#v (identical %v), want the visibility change", fields, identical)
	}
}

// TestRecordChunkVersionRequiresDatabase keeps the history writer from panicking
// when the metadata database is unavailable; the caller logs the error.
func TestRecordChunkVersionRequiresDatabase(t *testing.T) {
	svc := &ChunkService{}
	prev := dao.DB
	dao.DB = nil
	t.Cleanup(func() { dao.DB = prev })

	err := svc.recordChunkVersion(context.Background(), "kb-1", "doc-a", "chunk-1", 1, nil, nil, chunkversion.SnapshotMeta{})
	if err == nil {
		t.Fatalf("recordChunkVersion() error = nil, want metadata database error")
	}
}

func chunkVersionIntPtr(v int) *int { return &v }

// TestUpdateChunkExpectedVersionRejectsStaleBase pins the optimistic lock: an
// edit whose base version was superseded answers with a conflict before the
// engine is touched, while the same edit pinned to the current version lands
// and appends its version.
func TestUpdateChunkExpectedVersionRejectsStaleBase(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	engine := &updateChunkTestEngine{
		existingChunk: map[string]interface{}{
			"doc_id":              "doc-a",
			"content_with_weight": "v0 body",
			"available_int":       1,
		},
	}
	svc := chunkVersionTestService(engine)
	ctx := chunkTestContext(t, "user-1", "tenant-1")

	first := "v1 body"
	if err := svc.UpdateChunk(ctx, &service.UpdateChunkRequest{
		DatasetID:       "kb-1",
		DocumentID:      "doc-a",
		ChunkID:         "chunk-1",
		Content:         &first,
		ExpectedVersion: chunkVersionIntPtr(0),
	}); err != nil {
		t.Fatalf("first edit: %v", err)
	}
	if len(engine.updateCalls) != 1 {
		t.Fatalf("engine writes = %d, want 1", len(engine.updateCalls))
	}

	stale := "stale body"
	err := svc.UpdateChunk(ctx, &service.UpdateChunkRequest{
		DatasetID:       "kb-1",
		DocumentID:      "doc-a",
		ChunkID:         "chunk-1",
		Content:         &stale,
		ExpectedVersion: chunkVersionIntPtr(0),
	})
	var coded interface{ Code() common.ErrorCode }
	if !errors.As(err, &coded) || coded.Code() != common.CodeConflict {
		t.Fatalf("stale edit error = %v, want CodeConflict", err)
	}
	if len(engine.updateCalls) != 1 {
		t.Fatalf("a conflicting edit must not touch the engine, writes = %d", len(engine.updateCalls))
	}

	fresh := "v2 body"
	if err := svc.UpdateChunk(ctx, &service.UpdateChunkRequest{
		DatasetID:       "kb-1",
		DocumentID:      "doc-a",
		ChunkID:         "chunk-1",
		Content:         &fresh,
		ExpectedVersion: chunkVersionIntPtr(1),
	}); err != nil {
		t.Fatalf("fresh edit: %v", err)
	}
	if len(engine.updateCalls) != 2 {
		t.Fatalf("engine writes = %d, want 2", len(engine.updateCalls))
	}

	rows := loadChunkVersions(t, "chunk-1")
	if len(rows) != 3 {
		t.Fatalf("version rows = %d, want 3", len(rows))
	}
	if got := rows[2].Snapshot["content_with_weight"]; got != "v2 body" {
		t.Fatalf("latest content = %v, want v2 body", got)
	}
}

// TestUpdateChunkExpectedVersionZeroMatchesNeverEditedChunk reads the absent
// history of a never-edited chunk as version 0, so the first editor holding a
// chunk view without versions can still take the lock.
func TestUpdateChunkExpectedVersionZeroMatchesNeverEditedChunk(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	engine := &updateChunkTestEngine{
		existingChunk: map[string]interface{}{
			"doc_id":              "doc-a",
			"content_with_weight": "v0 body",
			"available_int":       1,
		},
	}
	svc := chunkVersionTestService(engine)

	body := "v1 body"
	if err := svc.UpdateChunk(chunkTestContext(t, "user-1", "tenant-1"), &service.UpdateChunkRequest{
		DatasetID:       "kb-1",
		DocumentID:      "doc-a",
		ChunkID:         "chunk-1",
		Content:         &body,
		ExpectedVersion: chunkVersionIntPtr(0),
	}); err != nil {
		t.Fatalf("first edit: %v", err)
	}

	rows := loadChunkVersions(t, "chunk-1")
	if len(rows) != 2 || rows[0].Version != 0 || rows[1].Version != 1 {
		t.Fatalf("version rows = %#v, want v0 and v1", rows)
	}
}

// TestRestoreVersionExpectedVersionRejectsStaleBase gives a restore the same
// optimistic lock an edit has: restoring over a chunk someone else edited
// answers with a conflict instead of silently overwriting their change.
func TestRestoreVersionExpectedVersionRejectsStaleBase(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	engine := &updateChunkTestEngine{
		existingChunk: map[string]interface{}{
			"doc_id":              "doc-a",
			"content_with_weight": "v0 body",
			// A parsed row carries its tokens and vector; the baseline version is
			// recorded from it, and a replay copies them rather than deriving.
			"content_ltks":  "v0 body",
			"q_3_vec":       []float64{1, 2, 3},
			"available_int": 1,
		},
	}
	svc := chunkVersionTestService(engine)
	ctx := chunkVersionManageContext(t, "tenant-1", "user-1", "doc-a")

	for _, content := range []string{"v1 body", "v2 body"} {
		body := content
		if err := svc.UpdateChunk(ctx, &service.UpdateChunkRequest{
			DatasetID:  "kb-1",
			DocumentID: "doc-a",
			ChunkID:    "chunk-1",
			Content:    &body,
		}); err != nil {
			t.Fatalf("edit %q: %v", content, err)
		}
	}
	writes := len(engine.updateCalls)

	var wikiDirtyCalls [][]string
	svc.markWikiDirtyFunc = func(_, _, _ string, chunkIDs []string) {
		wikiDirtyCalls = append(wikiDirtyCalls, chunkIDs)
	}

	_, err := svc.RestoreVersion(ctx, &service.ChunkVersionRequest{
		DatasetID:       "kb-1",
		DocumentID:      "doc-a",
		ChunkID:         "chunk-1",
		Version:         0,
		ExpectedVersion: chunkVersionIntPtr(1),
	})
	var coded interface{ Code() common.ErrorCode }
	if !errors.As(err, &coded) || coded.Code() != common.CodeConflict {
		t.Fatalf("stale restore error = %v, want CodeConflict", err)
	}
	if len(engine.updateCalls) != writes {
		t.Fatalf("a conflicting restore must not touch the engine, writes = %d", len(engine.updateCalls))
	}

	resp, err := svc.RestoreVersion(ctx, &service.ChunkVersionRequest{
		DatasetID:       "kb-1",
		DocumentID:      "doc-a",
		ChunkID:         "chunk-1",
		Version:         0,
		ExpectedVersion: chunkVersionIntPtr(2),
	})
	if err != nil {
		t.Fatalf("fresh restore: %v", err)
	}
	if resp.Version != 3 {
		t.Fatalf("restored version = %d, want 3", resp.Version)
	}

	rows := loadChunkVersions(t, "chunk-1")
	if len(rows) != 4 {
		t.Fatalf("version rows = %d, want 4", len(rows))
	}
	if rows[3].Version != 3 || rows[3].RestoredFrom != 0 {
		t.Fatalf("appended row = v%d from v%d, want v3 from v0", rows[3].Version, rows[3].RestoredFrom)
	}
	if got := rows[3].Snapshot["content_with_weight"]; got != "v0 body" {
		t.Fatalf("restored content = %v, want v0 body", got)
	}
}

// TestAnnotateChunkVersionsEE pins the badge the chunk list renders: the
// highest recorded version per chunk, and 0 for a chunk that was never edited
// (its text is still the parser's output).
func TestAnnotateChunkVersionsEE(t *testing.T) {
	setupChunkVersionTestDB(t)
	ctx := t.Context()

	versionDAO := dao.NewChunkVersionDAO()
	for _, row := range []*entity.ChunkVersion{
		{ChunkID: "chunk-a", Version: 0, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1, Snapshot: entity.JSONMap{}},
		{ChunkID: "chunk-a", Version: 2, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1, Snapshot: entity.JSONMap{}},
		{ChunkID: "chunk-b", Version: 1, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1, Snapshot: entity.JSONMap{}},
	} {
		if err := versionDAO.Create(ctx, dao.DB, row); err != nil {
			t.Fatalf("seed version %s: %v", row.ChunkID, err)
		}
	}

	svc := chunkVersionTestService(&updateChunkTestEngine{})
	chunks := []map[string]interface{}{
		{"chunk_id": "chunk-a"},
		{"chunk_id": "chunk-b"},
		{"chunk_id": "chunk-c"},
	}
	svc.annotateChunkVersionsEE(ctx, chunks)

	if got := chunks[0]["content_version"]; got != 2 {
		t.Fatalf("chunk-a version = %v, want the highest (2)", got)
	}
	if got := chunks[1]["content_version"]; got != 1 {
		t.Fatalf("chunk-b version = %v, want 1", got)
	}
	if got := chunks[2]["content_version"]; got != 0 {
		t.Fatalf("chunk-c version = %v, want 0 for a chunk with no history", got)
	}
}

// TestDiffVersionsReportsChangedFields pins the field-level shape of a
// comparison: text gets a unified diff, keyword lists get element-level
// additions/removals, scalars get before/after, and unchanged fields are
// omitted.
func TestDiffVersionsReportsChangedFields(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	ctx := chunkTestContext(t, "user-1", "tenant-1")
	versionDAO := dao.NewChunkVersionDAO()
	for _, row := range []*entity.ChunkVersion{
		{
			ChunkID: "chunk-1", Version: 0, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1,
			Snapshot: entity.JSONMap{
				"content_with_weight": "alpha\nbeta",
				"important_kwd":       []interface{}{"a", "b"},
				"available_int":       float64(1),
			},
		},
		{
			ChunkID: "chunk-1", Version: 1, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1,
			Snapshot: entity.JSONMap{
				"content_with_weight": "alpha\nBETA",
				"important_kwd":       []interface{}{"b", "c"},
				"available_int":       float64(0),
			},
		},
	} {
		if err := versionDAO.Create(ctx, dao.DB, row); err != nil {
			t.Fatalf("seed version %s: %v", row.ChunkID, err)
		}
	}

	svc := chunkVersionTestService(&updateChunkTestEngine{})
	to := 1
	resp, err := svc.DiffVersions(ctx, &service.ChunkVersionDiffRequest{
		DatasetID: "kb-1", DocumentID: "doc-a", ChunkID: "chunk-1", From: 0, To: &to,
	})
	if err != nil {
		t.Fatalf("DiffVersions() error = %v", err)
	}
	if resp.Identical || resp.To != 1 || len(resp.Fields) != 3 {
		t.Fatalf("DiffVersions() = %#v, want the body, keyword and visibility changes", resp)
	}

	byField := map[string]service.ChunkVersionFieldDiff{}
	for _, field := range resp.Fields {
		byField[field.Field] = field
	}
	text, ok := byField["content_with_weight"]
	if !ok || text.Kind != service.ChunkVersionDiffText ||
		!strings.Contains(text.Diff, "-beta") || !strings.Contains(text.Diff, "+BETA") {
		t.Fatalf("text diff = %#v, want a unified diff of the body", text)
	}
	keywords, ok := byField["important_kwd"]
	if !ok || keywords.Kind != service.ChunkVersionDiffList {
		t.Fatalf("keyword diff = %#v, want a list diff", keywords)
	}
	if len(keywords.Added) != 1 || keywords.Added[0] != "c" ||
		len(keywords.Removed) != 1 || keywords.Removed[0] != "a" {
		t.Fatalf("keyword diff = %#v, want +c/-a", keywords)
	}
	if !keywords.Changed {
		t.Fatalf("keyword changed = false, want true")
	}
	// The kept keyword rides along so the UI can render the whole list with
	// the change marked, not just the deletion.
	if !reflect.DeepEqual(keywords.Unchanged, []string{"b"}) {
		t.Fatalf("keyword unchanged = %#v, want [b]", keywords.Unchanged)
	}
	// Visibility is versioned: the pair hides the chunk, so the comparison has to
	// say so, in the state a reader understands rather than as the stored key.
	availability, ok := byField["available_int"]
	if !ok || availability.Kind != service.ChunkVersionDiffScalar || !availability.Changed {
		t.Fatalf("availability diff = %#v, want a scalar change", availability)
	}
	if availability.Before != 1 || availability.After != 0 {
		t.Fatalf("availability diff = %#v, want 1 -> 0", availability)
	}

	// A nil To means "compare with the latest version".
	latestResp, err := svc.DiffVersions(ctx, &service.ChunkVersionDiffRequest{
		DatasetID: "kb-1", DocumentID: "doc-a", ChunkID: "chunk-1", From: 0,
	})
	if err != nil {
		t.Fatalf("DiffVersions(latest) error = %v", err)
	}
	if latestResp.To != 1 || latestResp.Identical {
		t.Fatalf("DiffVersions(latest) = %#v, want a comparison against v1", latestResp)
	}
}

// TestDiffVersionsSeesKeywordAddedWithBodyEdit pins the pair a comparison sees
// after one save changes both the body and the keywords: the new version holds
// the added keyword and the old one does not, so the keyword row must come back
// as changed with the new value under "added".
func TestDiffVersionsSeesKeywordAddedWithBodyEdit(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	engine := &updateChunkTestEngine{
		existingChunk: map[string]interface{}{
			"doc_id":              "doc-a",
			"content_with_weight": "old body",
			"important_kwd":       []interface{}{"a"},
			"available_int":       1,
		},
	}
	svc := chunkVersionTestService(engine)
	ctx := chunkTestContext(t, "user-1", "tenant-1")

	body := "new body"
	if err := svc.UpdateChunk(ctx, &service.UpdateChunkRequest{
		DatasetID:    "kb-1",
		DocumentID:   "doc-a",
		ChunkID:      "chunk-1",
		Content:      &body,
		ImportantKwd: []string{"a", "b"},
	}); err != nil {
		t.Fatalf("UpdateChunk() error = %v", err)
	}

	to := 1
	resp, err := svc.DiffVersions(ctx, &service.ChunkVersionDiffRequest{
		DatasetID: "kb-1", DocumentID: "doc-a", ChunkID: "chunk-1", From: 0, To: &to,
	})
	if err != nil {
		t.Fatalf("DiffVersions() error = %v", err)
	}
	if resp.Identical {
		t.Fatalf("DiffVersions() identical = true, want the body and keyword changes")
	}

	var keywords *service.ChunkVersionFieldDiff
	for index := range resp.Fields {
		if resp.Fields[index].Field == "important_kwd" {
			keywords = &resp.Fields[index]
		}
	}
	if keywords == nil {
		t.Fatalf("fields = %#v, want the keyword row", resp.Fields)
	}
	if !keywords.Changed {
		t.Fatalf("keyword changed = false, want true")
	}
	if !reflect.DeepEqual(keywords.Added, []string{"b"}) {
		t.Fatalf("keyword added = %#v, want [b]", keywords.Added)
	}
	if !reflect.DeepEqual(keywords.Unchanged, []string{"a"}) {
		t.Fatalf("keyword unchanged = %#v, want [a]", keywords.Unchanged)
	}
}

// TestDiffVersionsKeepsUnchangedListFields pins that a keyword list both
// versions hold is still reported, so the comparison shows what the versions
// carry and not only what they lost. On its own it does not make them
// different.
func TestDiffVersionsKeepsUnchangedListFields(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	versionDAO := dao.NewChunkVersionDAO()
	for _, row := range []*entity.ChunkVersion{
		{
			ChunkID: "chunk-1", Version: 0, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1,
			Snapshot: entity.JSONMap{
				"content_with_weight": "body",
				"important_kwd":       []interface{}{"kept"},
			},
		},
		{
			ChunkID: "chunk-1", Version: 1, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1,
			Snapshot: entity.JSONMap{
				"content_with_weight": "body",
				"important_kwd":       []interface{}{"kept"},
			},
		},
	} {
		if err := versionDAO.Create(context.Background(), dao.DB, row); err != nil {
			t.Fatalf("seed version %s: %v", row.ChunkID, err)
		}
	}

	svc := chunkVersionTestService(&updateChunkTestEngine{})
	to := 1
	resp, err := svc.DiffVersions(chunkTestContext(t, "user-1", "tenant-1"), &service.ChunkVersionDiffRequest{
		DatasetID: "kb-1", DocumentID: "doc-a", ChunkID: "chunk-1", From: 0, To: &to,
	})
	if err != nil {
		t.Fatalf("DiffVersions() error = %v", err)
	}
	if !resp.Identical {
		t.Fatalf("DiffVersions() identical = false, want true")
	}
	if len(resp.Fields) != 1 {
		t.Fatalf("fields = %#v, want the kept keyword list", resp.Fields)
	}
	field := resp.Fields[0]
	if field.Field != "important_kwd" || field.Changed {
		t.Fatalf("field = %#v, want an unchanged keyword list", field)
	}
	if !reflect.DeepEqual(field.Unchanged, []string{"kept"}) {
		t.Fatalf("unchanged = %#v, want [kept]", field.Unchanged)
	}
}

// TestDiffVersionsComparesTagFeasKeys pins that the tag row of a comparison
// comes from tag_feas: its keys are the tags, so adding one shows up even
// though tag_kwd is not part of a version.
func TestDiffVersionsComparesTagFeasKeys(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	versionDAO := dao.NewChunkVersionDAO()
	for _, row := range []*entity.ChunkVersion{
		{
			ChunkID: "chunk-1", Version: 0, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1,
			Snapshot: entity.JSONMap{
				"content_with_weight": "body",
				"tag_feas":            map[string]interface{}{"rag": 3.0, "vector": 2.0},
			},
		},
		{
			ChunkID: "chunk-1", Version: 1, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1,
			Snapshot: entity.JSONMap{
				"content_with_weight": "body",
				"tag_feas":            map[string]interface{}{"vector": 2.0, "llm": 1.0},
			},
		},
	} {
		if err := versionDAO.Create(context.Background(), dao.DB, row); err != nil {
			t.Fatalf("seed version %s: %v", row.ChunkID, err)
		}
	}

	svc := chunkVersionTestService(&updateChunkTestEngine{})
	to := 1
	resp, err := svc.DiffVersions(chunkTestContext(t, "user-1", "tenant-1"), &service.ChunkVersionDiffRequest{
		DatasetID: "kb-1", DocumentID: "doc-a", ChunkID: "chunk-1", From: 0, To: &to,
	})
	if err != nil {
		t.Fatalf("DiffVersions() error = %v", err)
	}

	var tags *service.ChunkVersionFieldDiff
	for index := range resp.Fields {
		if resp.Fields[index].Field == "tag_feas" {
			tags = &resp.Fields[index]
		}
	}
	if tags == nil {
		t.Fatalf("fields = %#v, want the tag row", resp.Fields)
	}
	if tags.Kind != service.ChunkVersionDiffList || !tags.Changed {
		t.Fatalf("tag diff = %#v, want a changed list", tags)
	}
	if !reflect.DeepEqual(tags.Added, []string{"llm"}) {
		t.Fatalf("tag added = %#v, want [llm]", tags.Added)
	}
	if !reflect.DeepEqual(tags.Removed, []string{"rag"}) {
		t.Fatalf("tag removed = %#v, want [rag]", tags.Removed)
	}
	if !reflect.DeepEqual(tags.Unchanged, []string{"vector"}) {
		t.Fatalf("tag unchanged = %#v, want [vector]", tags.Unchanged)
	}
}

// TestDiffChunkStringListsKeepsUnchangedValues pins the three-way split of a
// list comparison: what the new version added, what the old one dropped, and
// what both kept.
func TestDiffChunkStringListsKeepsUnchangedValues(t *testing.T) {
	added, removed, unchanged := diffChunkStringLists(
		[]string{"dropped", "kept"},
		[]string{"kept", "new"},
	)
	if !reflect.DeepEqual(added, []string{"new"}) {
		t.Fatalf("added = %#v, want [new]", added)
	}
	if !reflect.DeepEqual(removed, []string{"dropped"}) {
		t.Fatalf("removed = %#v, want [dropped]", removed)
	}
	if !reflect.DeepEqual(unchanged, []string{"kept"}) {
		t.Fatalf("unchanged = %#v, want [kept]", unchanged)
	}

	// Identical lists have no change at all, and every value is kept.
	added, removed, unchanged = diffChunkStringLists([]string{"a", "b"}, []string{"a", "b"})
	if len(added) != 0 || len(removed) != 0 || !reflect.DeepEqual(unchanged, []string{"a", "b"}) {
		t.Fatalf("identical lists = +%v -%v =%v, want no change", added, removed, unchanged)
	}
}

// TestDiffVersionsRejectsUnknownVersion pins that a version number the history
// does not hold is reported as the shared not-found error, which the handler
// maps to 404.
func TestDiffVersionsRejectsUnknownVersion(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")
	if err := dao.NewChunkVersionDAO().Create(context.Background(), dao.DB, &entity.ChunkVersion{
		ChunkID: "chunk-1", Version: 0, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1,
		Snapshot: entity.JSONMap{"content_with_weight": "body"},
	}); err != nil {
		t.Fatalf("seed version: %v", err)
	}

	svc := chunkVersionTestService(&updateChunkTestEngine{})
	_, err := svc.DiffVersions(chunkTestContext(t, "user-1", "tenant-1"), &service.ChunkVersionDiffRequest{
		DatasetID: "kb-1", DocumentID: "doc-a", ChunkID: "chunk-1", From: 9,
	})
	if !errors.Is(err, service.ErrChunkVersionNotFound) {
		t.Fatalf("DiffVersions(missing) error = %v, want ErrChunkVersionNotFound", err)
	}
	// The message the 404 body carries names the version and the likely cause,
	// so an API caller can tell a stale number from a server fault.
	if message := err.Error(); !strings.Contains(message, "version 9") || !strings.Contains(message, "re-parse") {
		t.Fatalf("DiffVersions(missing) message = %q, want the version and the cause", message)
	}
}

// TestVersionImageReadsTheRecordedPicture pins that a stored version is what
// authorizes an old picture: the bytes are still in object storage, but the
// live chunk no longer references them, so the document image endpoint cannot
// find them and the version row is the only reference left.
func TestVersionImageReadsTheRecordedPicture(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")
	insertChunkTestDoc(t, "doc-b", "kb-1")

	picture := []byte("\x89PNG\r\n\x1a\nold picture")
	mockStorage := &chunkImageStorage{exists: true, oldBinary: picture}
	factory := storage.GetStorageFactory()
	originalStorage := factory.GetStorage()
	factory.SetStorage(mockStorage)
	t.Cleanup(func() {
		factory.SetStorage(originalStorage)
	})

	versionDAO := dao.NewChunkVersionDAO()
	for _, row := range []*entity.ChunkVersion{
		{ChunkID: "chunk-1", Version: 0, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1,
			Snapshot: entity.JSONMap{"content_with_weight": "body", "img_id": "kb-1-chunk-1-oldhash"}},
		// A version of a text-only chunk recorded no picture at all.
		{ChunkID: "chunk-1", Version: 1, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1,
			Snapshot: entity.JSONMap{"content_with_weight": "body"}},
		// An image ID naming another bucket is not readable through this one.
		{ChunkID: "chunk-1", Version: 2, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1,
			Snapshot: entity.JSONMap{"content_with_weight": "body", "img_id": "kb-9-chunk-1-oldhash"}},
		// The same chunk id under a different document must not leak.
		{ChunkID: "chunk-2", Version: 0, DocID: "doc-b", DatasetID: "kb-1", DatasetVersion: 1,
			Snapshot: entity.JSONMap{"content_with_weight": "body", "img_id": "kb-1-chunk-2-oldhash"}},
	} {
		if err := versionDAO.Create(context.Background(), dao.DB, row); err != nil {
			t.Fatalf("seed version %s: %v", row.ChunkID, err)
		}
	}

	svc := &ChunkService{kbDAO: dao.NewKnowledgebaseDAO(), documentDAO: dao.NewDocumentDAO()}
	ctx := chunkTestContext(t, "user-1", "tenant-1")

	data, err := svc.VersionImage(ctx, &service.ChunkVersionRequest{
		DatasetID: "kb-1", DocumentID: "doc-a", ChunkID: "chunk-1", Version: 0,
	})
	if err != nil {
		t.Fatalf("VersionImage() error = %v", err)
	}
	if !reflect.DeepEqual(data, picture) {
		t.Fatalf("VersionImage() = %q, want %q", data, picture)
	}

	for _, tc := range []struct {
		name string
		req  *service.ChunkVersionRequest
		want error
	}{
		{
			name: "unknown version",
			req:  &service.ChunkVersionRequest{DatasetID: "kb-1", DocumentID: "doc-a", ChunkID: "chunk-1", Version: 9},
			want: service.ErrChunkVersionNotFound,
		},
		{
			name: "version without a picture",
			req:  &service.ChunkVersionRequest{DatasetID: "kb-1", DocumentID: "doc-a", ChunkID: "chunk-1", Version: 1},
			want: service.ErrChunkVersionImageNotFound,
		},
		{
			name: "picture of another bucket",
			req:  &service.ChunkVersionRequest{DatasetID: "kb-1", DocumentID: "doc-a", ChunkID: "chunk-1", Version: 2},
			want: service.ErrChunkVersionImageNotFound,
		},
		{
			name: "history of another document",
			req:  &service.ChunkVersionRequest{DatasetID: "kb-1", DocumentID: "doc-a", ChunkID: "chunk-2", Version: 0},
			want: service.ErrChunkVersionNotFound,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.VersionImage(ctx, tc.req); !errors.Is(err, tc.want) {
				t.Fatalf("VersionImage() error = %v, want %v", err, tc.want)
			}
		})
	}

	// Bytes that object storage no longer holds (or holds as empty) are just as
	// gone as a version that never recorded a picture.
	mockStorage.oldBinary = nil
	if _, err := svc.VersionImage(ctx, &service.ChunkVersionRequest{
		DatasetID: "kb-1", DocumentID: "doc-a", ChunkID: "chunk-1", Version: 0,
	}); !errors.Is(err, service.ErrChunkVersionImageNotFound) {
		t.Fatalf("VersionImage(missing bytes) error = %v, want ErrChunkVersionImageNotFound", err)
	}
}

// TestDeleteChunkVersionDropsHistoryOnly pins what a version delete does: the
// row goes, the numbers keep their gaps, and the chunk itself is untouched.
func TestDeleteChunkVersionDropsHistoryOnly(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	versionDAO := dao.NewChunkVersionDAO()
	for _, row := range []*entity.ChunkVersion{
		{ChunkID: "chunk-1", Version: 0, Kind: entity.ChunkVersionKindParse,
			DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1, CreatedBy: "user-1",
			Snapshot: entity.JSONMap{"content_with_weight": "parsed body"}},
		{ChunkID: "chunk-1", Version: 1, Kind: entity.ChunkVersionKindEdit,
			DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1, CreatedBy: "user-1",
			Snapshot: entity.JSONMap{"content_with_weight": "first edit"}},
		{ChunkID: "chunk-1", Version: 2, Kind: entity.ChunkVersionKindEdit,
			DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1, CreatedBy: "user-1",
			Snapshot: entity.JSONMap{"content_with_weight": "second edit"}},
	} {
		if err := versionDAO.Create(context.Background(), dao.DB, row); err != nil {
			t.Fatalf("seed version %d: %v", row.Version, err)
		}
	}

	svc := &ChunkService{kbDAO: dao.NewKnowledgebaseDAO(), documentDAO: dao.NewDocumentDAO()}
	ctx := chunkVersionManageContext(t, "tenant-1", "user-1", "doc-a")

	resp, err := svc.DeleteVersion(ctx, &service.ChunkVersionRequest{
		DatasetID: "kb-1", DocumentID: "doc-a", ChunkID: "chunk-1", Version: 1,
	})
	if err != nil {
		t.Fatalf("DeleteVersion() error = %v", err)
	}
	if resp.DeletedVersion != 1 || resp.LatestVersion != 2 {
		t.Fatalf("response = %#v, want deleted 1 with latest 2", resp)
	}

	rows := loadChunkVersions(t, "chunk-1")
	if len(rows) != 2 || rows[0].Version != 0 || rows[1].Version != 2 {
		t.Fatalf("versions = %v, want the numbers to keep their gaps", rows)
	}

	// The deleted row's body goes with it: nothing can name that document again,
	// so the engine must not keep one orphan snapshot per deleted version.
	bodies, err := chunkVersionTestStore.LoadChunkHistory(context.Background(), "", "kb-1",
		[]string{
			history.ChunkDocumentID("chunk-1", 0),
			history.ChunkDocumentID("chunk-1", 1),
			history.ChunkDocumentID("chunk-1", 2),
		})
	if err != nil {
		t.Fatalf("load bodies after delete: %v", err)
	}
	if _, ok := bodies[history.ChunkDocumentID("chunk-1", 1)]; ok {
		t.Fatalf("the body of the deleted version 1 is still stored: %v", bodies)
	}
	for _, version := range []int{0, 2} {
		if _, ok := bodies[history.ChunkDocumentID("chunk-1", version)]; !ok {
			t.Fatalf("the body of the surviving version %d was removed: %v", version, bodies)
		}
	}
}

// TestDeleteChunkVersionReclaimsImageNobodyElseNeeds pins the picture cleanup: a
// version's image object goes with the row when no remaining row of the document
// names it, so a delete does not leave content-addressed objects behind.
func TestDeleteChunkVersionReclaimsImageNobodyElseNeeds(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	mockStorage := &chunkImageStorage{exists: true}
	factory := storage.GetStorageFactory()
	originalStorage := factory.GetStorage()
	factory.SetStorage(mockStorage)
	t.Cleanup(func() {
		factory.SetStorage(originalStorage)
	})

	versionDAO := dao.NewChunkVersionDAO()
	for _, row := range []*entity.ChunkVersion{
		{ChunkID: "chunk-1", Version: 0, Kind: entity.ChunkVersionKindParse,
			DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1, CreatedBy: "user-1",
			Snapshot: entity.JSONMap{"content_with_weight": "parsed body"}},
		{ChunkID: "chunk-1", Version: 1, Kind: entity.ChunkVersionKindEdit,
			DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1, CreatedBy: "user-1",
			Snapshot: entity.JSONMap{"content_with_weight": "body with a picture", "img_id": "kb-1-pic-1"}},
		{ChunkID: "chunk-1", Version: 2, Kind: entity.ChunkVersionKindEdit,
			DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1, CreatedBy: "user-1",
			Snapshot: entity.JSONMap{"content_with_weight": "the picture was replaced"}},
		// Another chunk of the same document: its picture must not be touched,
		// and its rows must not count as references to this chunk's picture.
		{ChunkID: "chunk-2", Version: 0, Kind: entity.ChunkVersionKindParse,
			DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1, CreatedBy: "user-1",
			Snapshot: entity.JSONMap{"content_with_weight": "sibling body", "img_id": "kb-1-pic-2"}},
	} {
		if err := versionDAO.Create(context.Background(), dao.DB, row); err != nil {
			t.Fatalf("seed version %d: %v", row.Version, err)
		}
	}

	svc := &ChunkService{kbDAO: dao.NewKnowledgebaseDAO(), documentDAO: dao.NewDocumentDAO()}
	if _, err := svc.DeleteVersion(chunkVersionManageContext(t, "tenant-1", "user-1", "doc-a"), &service.ChunkVersionRequest{
		DatasetID: "kb-1", DocumentID: "doc-a", ChunkID: "chunk-1", Version: 1,
	}); err != nil {
		t.Fatalf("DeleteVersion() error = %v", err)
	}

	got := append([]string(nil), mockStorage.removeNames...)
	if len(got) != 1 || got[0] != "pic-1" {
		t.Fatalf("removed objects = %v, want the one picture only the deleted version named", got)
	}
}

// TestDeleteChunkVersionKeepsImageAnotherRowNeeds pins the other half: a picture
// a remaining version still names stays, because dropping one row must not empty
// another version's picture.
func TestDeleteChunkVersionKeepsImageAnotherRowNeeds(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	mockStorage := &chunkImageStorage{exists: true}
	factory := storage.GetStorageFactory()
	originalStorage := factory.GetStorage()
	factory.SetStorage(mockStorage)
	t.Cleanup(func() {
		factory.SetStorage(originalStorage)
	})

	versionDAO := dao.NewChunkVersionDAO()
	for _, row := range []*entity.ChunkVersion{
		{ChunkID: "chunk-1", Version: 0, Kind: entity.ChunkVersionKindParse,
			DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1, CreatedBy: "user-1",
			Snapshot: entity.JSONMap{"content_with_weight": "parsed body"}},
		{ChunkID: "chunk-1", Version: 1, Kind: entity.ChunkVersionKindEdit,
			DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1, CreatedBy: "user-1",
			Snapshot: entity.JSONMap{"content_with_weight": "edited body", "img_id": "kb-1-pic-1"}},
		{ChunkID: "chunk-1", Version: 2, Kind: entity.ChunkVersionKindEdit,
			DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1, CreatedBy: "user-1",
			Snapshot: entity.JSONMap{"content_with_weight": "edited again", "img_id": "kb-1-pic-1"}},
	} {
		if err := versionDAO.Create(context.Background(), dao.DB, row); err != nil {
			t.Fatalf("seed version %d: %v", row.Version, err)
		}
	}

	svc := &ChunkService{kbDAO: dao.NewKnowledgebaseDAO(), documentDAO: dao.NewDocumentDAO()}
	if _, err := svc.DeleteVersion(chunkVersionManageContext(t, "tenant-1", "user-1", "doc-a"), &service.ChunkVersionRequest{
		DatasetID: "kb-1", DocumentID: "doc-a", ChunkID: "chunk-1", Version: 1,
	}); err != nil {
		t.Fatalf("DeleteVersion() error = %v", err)
	}

	if got := append([]string(nil), mockStorage.removeNames...); len(got) != 0 {
		t.Fatalf("removed objects = %v, want none while another version names the picture", got)
	}
}

// TestDeleteChunkVersionRefusesProtectedRows pins the rows the history depends
// on: the latest one (the live chunk's state), the parser's baseline and a
// delete marker.
func TestDeleteChunkVersionRefusesProtectedRows(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	versionDAO := dao.NewChunkVersionDAO()
	for _, row := range []*entity.ChunkVersion{
		{ChunkID: "chunk-1", Version: 0, Kind: entity.ChunkVersionKindParse,
			DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1, CreatedBy: "user-1",
			Snapshot: entity.JSONMap{"content_with_weight": "parsed body"}},
		{ChunkID: "chunk-1", Version: 1, Kind: entity.ChunkVersionKindDelete,
			DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1, CreatedBy: "user-1",
			Snapshot: entity.JSONMap{}},
		{ChunkID: "chunk-1", Version: 2, Kind: entity.ChunkVersionKindEdit,
			DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1, CreatedBy: "user-1",
			Snapshot: entity.JSONMap{"content_with_weight": "edit"}},
	} {
		if err := versionDAO.Create(context.Background(), dao.DB, row); err != nil {
			t.Fatalf("seed version %d: %v", row.Version, err)
		}
	}

	svc := &ChunkService{kbDAO: dao.NewKnowledgebaseDAO(), documentDAO: dao.NewDocumentDAO()}
	ctx := chunkVersionManageContext(t, "tenant-1", "user-1", "doc-a")

	for _, tc := range []struct {
		name    string
		version int
		code    common.ErrorCode
	}{
		{name: "latest", version: 2, code: common.CodeConflict},
		{name: "parse baseline", version: 0, code: common.CodeConflict},
		{name: "delete marker", version: 1, code: common.CodeConflict},
		{name: "missing", version: 9, code: common.CodeNotFound},
	} {
		_, err := svc.DeleteVersion(ctx, &service.ChunkVersionRequest{
			DatasetID: "kb-1", DocumentID: "doc-a", ChunkID: "chunk-1", Version: tc.version,
		})
		var coded interface{ Code() common.ErrorCode }
		if !errors.As(err, &coded) || coded.Code() != tc.code {
			t.Fatalf("%s: error = %v, want %v", tc.name, err, tc.code)
		}
	}

	if rows := loadChunkVersions(t, "chunk-1"); len(rows) != 3 {
		t.Fatalf("versions = %d, want every refused row kept", len(rows))
	}
}

// TestDeleteChunkVersionRefusesARowADatasetVersionRestores pins the anchor
// protection. A dataset version replays the chunk history up to its own number,
// so the row that applied at that anchor is what the version restores. Dropping
// it rewrites a version that is already recorded: the panel's change text falls
// back to the earlier row and a rebuild produces it. A row no anchor falls inside
// is still deletable.
func TestDeleteChunkVersionRefusesARowADatasetVersionRestores(t *testing.T) {
	db := setupChunkVersionTestDB(t)
	if err := db.AutoMigrate(&entity.DatasetVersion{}); err != nil {
		t.Fatalf("migrate dataset versions: %v", err)
	}

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	at := func(millis int64) *int64 { return &millis }
	versionDAO := dao.NewChunkVersionDAO()
	for _, row := range []*entity.ChunkVersion{
		// chunk-1: parsed under version 1, edited under 3, edited again under 5.
		{ChunkID: "chunk-1", Version: 0, Kind: entity.ChunkVersionKindParse,
			DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1, CreatedBy: "user-1",
			Snapshot: entity.JSONMap{"content_with_weight": "parsed"}, CreateTime: at(1000)},
		{ChunkID: "chunk-1", Version: 1, Kind: entity.ChunkVersionKindEdit,
			DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 3, CreatedBy: "user-1",
			Snapshot: entity.JSONMap{"content_with_weight": "first edit"}, CreateTime: at(2000)},
		{ChunkID: "chunk-1", Version: 2, Kind: entity.ChunkVersionKindEdit,
			DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 5, CreatedBy: "user-1",
			Snapshot: entity.JSONMap{"content_with_weight": "second edit"}, CreateTime: at(3000)},
		// chunk-2: a chunk whose rows sit in versions no anchor row records.
		{ChunkID: "chunk-2", Version: 0, Kind: entity.ChunkVersionKindEdit,
			DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 7, CreatedBy: "user-1",
			Snapshot: entity.JSONMap{"content_with_weight": "unrelated"}, CreateTime: at(8000)},
		{ChunkID: "chunk-2", Version: 1, Kind: entity.ChunkVersionKindEdit,
			DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 9, CreatedBy: "user-1",
			Snapshot: entity.JSONMap{"content_with_weight": "unrelated latest"}, CreateTime: at(9000)},
	} {
		if err := versionDAO.Create(context.Background(), dao.DB, row); err != nil {
			t.Fatalf("seed chunk version v%d: %v", row.Version, err)
		}
	}
	// Two dataset versions: one before this history, one inside the window in
	// which chunk-1 v1 is the state a restore reads ([3, 5)).
	for _, anchor := range []struct {
		version int64
		at      int64
	}{{version: 1, at: 500}, {version: 3, at: 2500}} {
		if err := db.Create(&entity.DatasetVersion{
			DatasetID: "kb-1", Version: anchor.version, Kind: entity.DatasetVersionKindParse,
			CreateTime: at(anchor.at),
		}).Error; err != nil {
			t.Fatalf("seed dataset version %d: %v", anchor.version, err)
		}
	}

	svc := &ChunkService{kbDAO: dao.NewKnowledgebaseDAO(), documentDAO: dao.NewDocumentDAO()}
	ctx := chunkVersionManageContext(t, "tenant-1", "user-1", "doc-a")

	// v1 is what version 1 restores, so it stays.
	_, err := svc.DeleteVersion(ctx, &service.ChunkVersionRequest{
		DatasetID: "kb-1", DocumentID: "doc-a", ChunkID: "chunk-1", Version: 1,
	})
	var coded interface{ Code() common.ErrorCode }
	if !errors.As(err, &coded) || coded.Code() != common.CodeConflict {
		t.Fatalf("DeleteVersion(anchored) error = %v, want CodeConflict", err)
	}
	if rows := loadChunkVersions(t, "chunk-1"); len(rows) != 3 {
		t.Fatalf("chunk-1 versions = %d, want the anchored row kept", len(rows))
	}

	// The other chunk's rows are outside every anchor's window and still go.
	if _, err := svc.DeleteVersion(ctx, &service.ChunkVersionRequest{
		DatasetID: "kb-1", DocumentID: "doc-a", ChunkID: "chunk-2", Version: 0,
	}); err != nil {
		t.Fatalf("DeleteVersion(unreachable row) error = %v, want it allowed", err)
	}
	if rows := loadChunkVersions(t, "chunk-2"); len(rows) != 1 || rows[0].Version != 1 {
		t.Fatalf("chunk-2 versions = %v, want only the latest left", rows)
	}
}

// TestDeleteChunkVersionAllowsARowItsSiblingAnswers pins what the version number
// settles that the clock could not.
//
// Two rows written by the same change share a dataset version, so no anchor falls
// between them: a restore at that number reads the later row, and the earlier one
// is only history. The millisecond they shared used to make the window
// unprovable, and the row was refused for that reason; the number proves the
// window empty instead, so it can go.
func TestDeleteChunkVersionAllowsARowItsSiblingAnswers(t *testing.T) {
	db := setupChunkVersionTestDB(t)
	if err := db.AutoMigrate(&entity.DatasetVersion{}); err != nil {
		t.Fatalf("migrate dataset versions: %v", err)
	}

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	const at = int64(4000)
	millis := func(moment int64) *int64 { return &moment }
	versionDAO := dao.NewChunkVersionDAO()
	for _, row := range []*entity.ChunkVersion{
		// Two rows of one chunk written by the change recorded as version 5.
		{ChunkID: "chunk-3", Version: 0, Kind: entity.ChunkVersionKindEdit,
			DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 5, CreatedBy: "user-1",
			Snapshot: entity.JSONMap{"content_with_weight": "first edit"}, CreateTime: millis(at)},
		{ChunkID: "chunk-3", Version: 1, Kind: entity.ChunkVersionKindEdit,
			DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 5, CreatedBy: "user-1",
			Snapshot: entity.JSONMap{"content_with_weight": "second edit"}, CreateTime: millis(at)},
	} {
		if err := versionDAO.Create(context.Background(), dao.DB, row); err != nil {
			t.Fatalf("seed chunk version v%d: %v", row.Version, err)
		}
	}
	if err := db.Create(&entity.DatasetVersion{
		DatasetID: "kb-1", Version: 5, Kind: entity.DatasetVersionKindChunk, CreateTime: millis(at),
	}).Error; err != nil {
		t.Fatalf("seed dataset version: %v", err)
	}

	svc := &ChunkService{kbDAO: dao.NewKnowledgebaseDAO(), documentDAO: dao.NewDocumentDAO()}
	ctx := chunkVersionManageContext(t, "tenant-1", "user-1", "doc-a")

	if _, err := svc.DeleteVersion(ctx, &service.ChunkVersionRequest{
		DatasetID: "kb-1", DocumentID: "doc-a", ChunkID: "chunk-3", Version: 0,
	}); err != nil {
		t.Fatalf("DeleteVersion(sibling row) error = %v, want it allowed", err)
	}
	if rows := loadChunkVersions(t, "chunk-3"); len(rows) != 1 || rows[0].Version != 1 {
		t.Fatalf("chunk-3 versions = %v, want only the row the anchor reads", rows)
	}
}

// TestRestoreChunkVersionCopiesTheStoredRow pins what storing the whole row is
// for: restoring a version is a copy of what it was indexed by. No tokenizer run,
// no embedding call, and the chunk ranks exactly as the version did.
func TestRestoreChunkVersionCopiesTheStoredRow(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	var kb entity.Knowledgebase
	if err := dao.DB.Where("id = ?", "kb-1").Take(&kb).Error; err != nil {
		t.Fatalf("load the dataset: %v", err)
	}
	language := ""
	if kb.Language != nil {
		language = *kb.Language
	}

	engine := &updateChunkTestEngine{
		existingChunk: map[string]interface{}{
			"doc_id":              "doc-a",
			"content_with_weight": "v1 body",
			"content_ltks":        "live tokens",
			"available_int":       1,
			"q_1024_vec":          []float64{1, 1},
		},
	}
	svc := chunkVersionTestService(engine)
	tokenized := 0
	svc.tokenizeFunc = func(text, _ string) (string, error) {
		tokenized++
		return "recomputed", nil
	}
	previousEmbedder := chunkEmbedderEE
	embedded := 0
	chunkEmbedderEE = func(context.Context, ChunkEmbedRequest) (ChunkEmbedding, error) {
		embedded++
		return ChunkEmbedding{Field: "q_1024_vec", Vector: []float64{9, 9}}, nil
	}
	t.Cleanup(func() { chunkEmbedderEE = previousEmbedder })

	if err := dao.NewChunkVersionDAO().Create(context.Background(), dao.DB, &entity.ChunkVersion{
		ChunkID: "chunk-1", Version: 0, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1,
		Kind: entity.ChunkVersionKindEdit,
		Snapshot: chunkversion.FullSnapshot(map[string]interface{}{
			"id":                  "chunk-1",
			"doc_id":              "doc-a",
			"kb_id":               "kb-1",
			"docnm_kwd":           "doc-a.txt",
			"content_with_weight": "v0 body",
			"content_ltks":        "stored tokens",
			"content_sm_ltks":     "stored tokens",
			"important_kwd":       []string{"old"},
			"tag_kwd":             []string{},
			"tag_feas":            map[string]float64{},
			"available_int":       1,
			"chunk_order_int":     2,
			"img_id":              "",
			"q_1024_vec":          []float32{0.5, -0.25},
		}, chunkversion.SnapshotMeta{EmbeddingID: "embedding-model", Language: language}),
	}); err != nil {
		t.Fatalf("seed the version: %v", err)
	}

	resp, err := svc.RestoreVersion(chunkVersionManageContext(t, "tenant-1", "user-1", "doc-a"), &service.ChunkVersionRequest{
		DatasetID:  "kb-1",
		DocumentID: "doc-a",
		ChunkID:    "chunk-1",
		Version:    0,
	})
	if err != nil {
		t.Fatalf("RestoreVersion() error = %v", err)
	}
	if resp.Version != 1 {
		t.Fatalf("restored version = %d, want the appended 1", resp.Version)
	}
	if tokenized != 0 || embedded != 0 {
		t.Fatalf("restore tokenized %d times and embedded %d times, want the stored row copied", tokenized, embedded)
	}

	if len(engine.updateCalls) != 1 {
		t.Fatalf("UpdateChunks calls = %d, want 1", len(engine.updateCalls))
	}
	update := engine.updateCalls[0].newValue
	if update["content_with_weight"] != "v0 body" {
		t.Fatalf("restored content = %v, want v0 body", update["content_with_weight"])
	}
	// The tokens and the vector come back as they were stored, not as the live
	// row's or the current model's.
	if update["content_ltks"] != "stored tokens" {
		t.Fatalf("restored content_ltks = %v, want the stored tokens", update["content_ltks"])
	}
	vector, ok := update["q_1024_vec"].([]float64)
	if !ok || len(vector) != 2 || vector[0] != 0.5 || vector[1] != -0.25 {
		t.Fatalf("restored q_1024_vec = %#v, want the stored vector", update["q_1024_vec"])
	}
	if !reflect.DeepEqual(update["important_kwd"], []string{"old"}) {
		t.Fatalf("restored important_kwd = %#v, want [old]", update["important_kwd"])
	}
}

// TestRestoreChunkVersionCopiesTheStoredVectorOfAnotherModel pins what a restore
// does with a version recorded by another embedding model: it copies it. The
// version's derived fields are part of what the version was, and a restore
// derives nothing - no tokenizer run, no model call - so the row comes back
// exactly as it was recorded, and a re-parse is what brings derived fields up to
// date.
func TestRestoreChunkVersionCopiesTheStoredVectorOfAnotherModel(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	engine := &updateChunkTestEngine{
		existingChunk: map[string]interface{}{
			"doc_id":              "doc-a",
			"content_with_weight": "v1 body",
			"available_int":       1,
			"q_1024_vec":          []float64{1, 1},
		},
	}
	svc := chunkVersionTestService(engine)
	previousEmbedder := chunkEmbedderEE
	embedded := 0
	chunkEmbedderEE = func(context.Context, ChunkEmbedRequest) (ChunkEmbedding, error) {
		embedded++
		return ChunkEmbedding{Field: "q_1024_vec", Vector: []float64{9, 9}}, nil
	}
	t.Cleanup(func() { chunkEmbedderEE = previousEmbedder })

	if err := dao.NewChunkVersionDAO().Create(context.Background(), dao.DB, &entity.ChunkVersion{
		ChunkID: "chunk-1", Version: 0, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1,
		Kind: entity.ChunkVersionKindEdit,
		Snapshot: chunkversion.FullSnapshot(map[string]interface{}{
			"content_with_weight": "v0 body",
			"content_ltks":        "stored tokens",
			"available_int":       1,
			"chunk_order_int":     0,
			"img_id":              "",
			"q_1024_vec":          []float32{0.5, -0.25},
		}, chunkversion.SnapshotMeta{EmbeddingID: "another-model", Language: "en"}),
	}); err != nil {
		t.Fatalf("seed the version: %v", err)
	}

	if _, err := svc.RestoreVersion(chunkVersionManageContext(t, "tenant-1", "user-1", "doc-a"), &service.ChunkVersionRequest{
		DatasetID:  "kb-1",
		DocumentID: "doc-a",
		ChunkID:    "chunk-1",
		Version:    0,
	}); err != nil {
		t.Fatalf("RestoreVersion() error = %v", err)
	}
	if embedded != 0 {
		t.Fatalf("embedding calls = %d, want the model left alone", embedded)
	}
	update := engine.updateCalls[0].newValue
	vector, ok := update["q_1024_vec"].([]float64)
	if !ok || len(vector) != 2 || vector[0] != 0.5 || vector[1] != -0.25 {
		t.Fatalf("restored q_1024_vec = %#v, want the stored vector", update["q_1024_vec"])
	}
	if body, _ := update["content_with_weight"].(string); body != "v0 body" {
		t.Fatalf("restored content = %q, want the stored text", body)
	}
}
