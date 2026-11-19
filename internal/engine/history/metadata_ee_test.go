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

package history

import (
	"context"
	"errors"
	"strings"
	"testing"

	"ragflow/internal/engine/history/historytest"
	"ragflow/internal/entity"
)

// useMemoryStore installs an in-memory store for the test and restores whatever
// the process had when it ends.
func useMemoryStore(t *testing.T) *historytest.MetadataStore {
	t.Helper()
	previous := currentMetadataStore()
	store := historytest.NewMetadataStore()
	SetMetadataStore(store)
	t.Cleanup(func() { SetMetadataStore(previous) })
	return store
}

// useNoStore uninstalls the store, which is the shape of a document engine that
// offers none: a write is refused because the version row no longer carries the
// values it describes.
func useNoStore(t *testing.T) {
	t.Helper()
	previous := currentMetadataStore()
	SetMetadataStore(nil)
	t.Cleanup(func() { SetMetadataStore(previous) })
}

func metadataRow(datasetID, documentID string, at int64, values entity.JSONMap) *entity.DocumentVersion {
	return &entity.DocumentVersion{
		DocumentID: documentID,
		DatasetID:  datasetID,
		MetaFields: values,
	}
}

// TestStashMovesTheValuesIntoTheStore pins what the row keeps: the name of the
// snapshot, not the values. Those are user data of arbitrary length, and the
// metadata database is not where they belong.
func TestStashMovesTheValuesIntoTheStore(t *testing.T) {
	store := useMemoryStore(t)
	ctx := t.Context()

	row := metadataRow("kb-1", "doc-1", 100, entity.JSONMap{"author": "alice", "summary": strings.Repeat("曹操", 500)})
	if err := StashMetadata(ctx, row); err != nil {
		t.Fatalf("stash: %v", err)
	}

	if row.MetaFieldsSnapshotID == "" {
		t.Fatal("the row does not name the snapshot its values were stored in")
	}
	if row.MetaFields != nil {
		t.Fatal("a stashed row still carries its values in memory, so a second copy could be written")
	}
	if !strings.HasPrefix(row.MetaFieldsSnapshotID, "doc-1:") {
		t.Fatalf("location %q is not scoped to the document", row.MetaFieldsSnapshotID)
	}
	if ids := store.SnapshotIDs("kb-1"); len(ids) != 1 || ids[0] != row.MetaFieldsSnapshotID {
		t.Fatalf("snapshots in the store = %v, want exactly %q", ids, row.MetaFieldsSnapshotID)
	}

	if err := HydrateMetadata(ctx, []*entity.DocumentVersion{row}); err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	if row.MetaFields["author"] != "alice" || row.MetaFields["summary"] != strings.Repeat("曹操", 500) {
		t.Fatalf("hydrated values = %v, want the ones that were stashed", row.MetaFields)
	}
}

// TestStashWithoutStoreReportsUnavailable pins the failure a deployment with no
// store gets: the version row does not carry the values, so a write that cannot
// reach the engine has nowhere to put them.
func TestStashWithoutStoreReportsUnavailable(t *testing.T) {
	useNoStore(t)

	row := metadataRow("kb-1", "doc-1", 100, entity.JSONMap{"author": "alice"})
	if err := StashMetadata(t.Context(), row); !errors.Is(err, ErrMetadataUnavailable) {
		t.Fatalf("stash error = %v, want %v", err, ErrMetadataUnavailable)
	}

	if row.MetaFieldsSnapshotID != "" {
		t.Fatalf("location = %q, want none without a store", row.MetaFieldsSnapshotID)
	}
}

// TestStashKeysSnapshotsByContentWithinADocument pins both halves of the name:
// the same values stored twice reach one snapshot, and two documents holding the
// same values do not share one - a purge of one document's history must not
// reach the other's.
func TestStashKeysSnapshotsByContentWithinADocument(t *testing.T) {
	store := useMemoryStore(t)
	ctx := t.Context()

	first := metadataRow("kb-1", "doc-1", 100, entity.JSONMap{"author": "alice"})
	second := metadataRow("kb-1", "doc-1", 200, entity.JSONMap{"author": "alice"})
	other := metadataRow("kb-1", "doc-2", 100, entity.JSONMap{"author": "alice"})

	if err := StashMetadataRows(ctx, []*entity.DocumentVersion{first, second, other}); err != nil {
		t.Fatalf("stash: %v", err)
	}

	if first.MetaFieldsSnapshotID != second.MetaFieldsSnapshotID {
		t.Fatalf("identical values are stored twice: %q vs %q", first.MetaFieldsSnapshotID, second.MetaFieldsSnapshotID)
	}
	if first.MetaFieldsSnapshotID == other.MetaFieldsSnapshotID {
		t.Fatal("two documents share one snapshot, so purging one history would break the other")
	}
	if ids := store.SnapshotIDs("kb-1"); len(ids) != 2 {
		t.Fatalf("snapshots in the store = %v, want one per document", ids)
	}
}

// TestStashStoresAnEmptyMapAsASnapshot pins that a removal - an empty map - is a
// snapshot like any other: it is what a replay of that version puts back, and
// the row has no inline copy left to mean it.
func TestStashStoresAnEmptyMapAsASnapshot(t *testing.T) {
	store := useMemoryStore(t)
	row := metadataRow("kb-1", "doc-1", 100, entity.JSONMap{})
	if err := StashMetadata(t.Context(), row); err != nil {
		t.Fatalf("stash: %v", err)
	}
	if row.MetaFieldsSnapshotID == "" {
		t.Fatal("a removal names no snapshot, so a replay would leave the values in place")
	}
	if ids := store.SnapshotIDs("kb-1"); len(ids) != 1 || ids[0] != row.MetaFieldsSnapshotID {
		t.Fatalf("snapshots in the store = %v, want exactly %q", ids, row.MetaFieldsSnapshotID)
	}
	if err := HydrateMetadata(t.Context(), []*entity.DocumentVersion{row}); err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	if row.MetaFields == nil || len(row.MetaFields) != 0 {
		t.Fatalf("hydrated values = %v, want an empty map", row.MetaFields)
	}
}

// TestStashNamesANilAndAnEmptyMapAlike pins the digest side of the same idea: a
// nil map and an empty one describe one snapshot, so they reach one name instead
// of two that hold the same bytes.
func TestStashNamesANilAndAnEmptyMapAlike(t *testing.T) {
	store := useMemoryStore(t)
	ctx := t.Context()

	empty := metadataRow("kb-1", "doc-1", 100, entity.JSONMap{})
	absent := metadataRow("kb-1", "doc-2", 100, nil)
	if err := StashMetadataRows(ctx, []*entity.DocumentVersion{empty, absent}); err != nil {
		t.Fatalf("stash: %v", err)
	}
	if SnapshotID("doc-1", nil) != SnapshotID("doc-1", entity.JSONMap{}) {
		t.Fatalf("a nil map and an empty one reach different names: %q vs %q",
			SnapshotID("doc-1", nil), SnapshotID("doc-1", entity.JSONMap{}))
	}
	if ids := store.SnapshotIDs("kb-1"); len(ids) != 2 {
		t.Fatalf("snapshots in the store = %v, want one per document", ids)
	}
}

// TestStashRefusesAnUnencodableRow pins that values which cannot be encoded are
// a failure, not a silent downgrade: the batch is abandoned, no row is pointed at
// a snapshot, and the caller is told why.
func TestStashRefusesAnUnencodableRow(t *testing.T) {
	store := useMemoryStore(t)

	good := metadataRow("kb-1", "doc-1", 100, entity.JSONMap{"author": "alice"})
	bad := metadataRow("kb-1", "doc-2", 100, entity.JSONMap{"author": make(chan int)})

	if err := StashMetadataRows(t.Context(), []*entity.DocumentVersion{good, bad}); err == nil {
		t.Fatal("stashing an unencodable snapshot reported success")
	}
	for _, row := range []*entity.DocumentVersion{good, bad} {
		if row.MetaFieldsSnapshotID != "" {
			t.Fatalf("document %s names snapshot %q after a failed batch", row.DocumentID, row.MetaFieldsSnapshotID)
		}
	}
	if ids := store.SnapshotIDs("kb-1"); len(ids) != 0 {
		t.Fatalf("snapshots in the store = %v, want none after a failed batch", ids)
	}
}

// failingStore refuses every write, which is the shape of an engine that is
// reachable but will not hold the values.
type failingStore struct {
	historytest.MetadataStore
}

func (s *failingStore) StashMetadataHistory(context.Context, string, string, []map[string]interface{}) error {
	return errors.New("engine refused the write")
}

// TestStashReturnsAStoreFailure pins the policy chunk history follows: a store
// that refuses the values fails the call and leaves the row exactly as it was -
// no snapshot id naming a snapshot nobody wrote.
func TestStashReturnsAStoreFailure(t *testing.T) {
	previous := currentMetadataStore()
	SetMetadataStore(&failingStore{})
	t.Cleanup(func() { SetMetadataStore(previous) })

	row := metadataRow("kb-1", "doc-1", 100, entity.JSONMap{"author": "alice"})
	err := StashMetadata(t.Context(), row)
	if err == nil {
		t.Fatal("stashing into a refusing store reported success")
	}
	if !errors.Is(err, ErrMetadataUnavailable) && !strings.Contains(err.Error(), "engine refused the write") {
		t.Fatalf("error = %v, want the store's failure", err)
	}
	if row.MetaFieldsSnapshotID != "" {
		t.Fatalf("location = %q, want none after a failed write", row.MetaFieldsSnapshotID)
	}
	if row.MetaFields["author"] != "alice" {
		t.Fatalf("values = %v, want them left as they were", row.MetaFields)
	}
	if row.MetaFieldsStashed {
		t.Fatal("a failed write marked the row as stashed")
	}
}

// TestBatchMarksRowsStashed pins the seam the entity hook reads: a batch that
// wrote the snapshots tells the hook not to write them again, one request each.
func TestBatchMarksRowsStashed(t *testing.T) {
	useMemoryStore(t)
	row := metadataRow("kb-1", "doc-1", 100, entity.JSONMap{"author": "alice"})
	if err := StashMetadataRows(t.Context(), []*entity.DocumentVersion{row}); err != nil {
		t.Fatalf("stash: %v", err)
	}
	if !row.MetaFieldsStashed {
		t.Fatal("a row whose snapshot the batch wrote is not marked as stashed")
	}
}

// TestHydrateSkipsRowsThatNameNoSnapshot pins the other half: a row with no
// snapshot - a settings row, or a metadata row a deployment without a store never
// wrote - has nothing to load, so it is left alone instead of being reported
// missing.
func TestHydrateSkipsRowsThatNameNoSnapshot(t *testing.T) {
	useMemoryStore(t)
	row := metadataRow("kb-1", "doc-1", 100, nil)
	if err := HydrateMetadata(t.Context(), []*entity.DocumentVersion{row}); err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	if row.MetaFields != nil || row.MetaFieldsSnapshotID != "" {
		t.Fatalf("row = %+v, want it untouched", row)
	}
}

// TestHydrateFailsWhenTheSnapshotIsGone pins the failure a reader must not paper
// over: a row naming a snapshot that is not there is an error, not an empty map,
// because an empty map is how a removal is recorded.
func TestHydrateFailsWhenTheSnapshotIsGone(t *testing.T) {
	useMemoryStore(t)
	row := metadataRow("kb-1", "doc-1", 100, nil)
	row.MetaFieldsSnapshotID = "doc-1:deadbeef"

	err := HydrateMetadata(t.Context(), []*entity.DocumentVersion{row})
	if !errors.Is(err, ErrMetadataMissing) {
		t.Fatalf("hydrate error = %v, want %v", err, ErrMetadataMissing)
	}
	if row.MetaFields != nil {
		t.Fatalf("values = %v, want them left unset on failure", row.MetaFields)
	}
}

// TestHydrateWithoutStoreReportsUnavailable pins the other failure: a row that
// names a snapshot in a deployment whose engine holds none is unavailable, not
// empty - the caller decides whether it can live with that.
func TestHydrateWithoutStoreReportsUnavailable(t *testing.T) {
	useNoStore(t)
	row := metadataRow("kb-1", "doc-1", 100, nil)
	row.MetaFieldsSnapshotID = "doc-1:deadbeef"

	if err := HydrateMetadata(t.Context(), []*entity.DocumentVersion{row}); !errors.Is(err, ErrMetadataUnavailable) {
		t.Fatalf("hydrate error = %v, want %v", err, ErrMetadataUnavailable)
	}
}

// TestDeleteDocumentRemovesOnlyThatDocumentsSnapshots pins the purge grain: the
// snapshots of one document go, the ones of its neighbour - which may hold the
// very same values - stay.
func TestDeleteDocumentRemovesOnlyThatDocumentsSnapshots(t *testing.T) {
	store := useMemoryStore(t)
	ctx := t.Context()

	gone := metadataRow("kb-1", "doc-1", 100, entity.JSONMap{"author": "alice"})
	kept := metadataRow("kb-1", "doc-2", 100, entity.JSONMap{"author": "alice"})
	if err := StashMetadataRows(ctx, []*entity.DocumentVersion{gone, kept}); err != nil {
		t.Fatalf("stash: %v", err)
	}

	if err := DeleteMetadataDocument(ctx, "kb-1", "doc-1"); err != nil {
		t.Fatalf("delete document: %v", err)
	}
	if ids := store.SnapshotIDs("kb-1"); len(ids) != 1 || ids[0] != kept.MetaFieldsSnapshotID {
		t.Fatalf("snapshots in the store = %v, want only %q", ids, kept.MetaFieldsSnapshotID)
	}
}

// TestDeleteDatasetDropsTheWholeHistory pins the dataset grain: a removed
// dataset leaves no snapshots behind, and a later dataset reusing the id inherits
// none.
func TestDeleteDatasetDropsTheWholeHistory(t *testing.T) {
	store := useMemoryStore(t)
	ctx := t.Context()

	for _, row := range []*entity.DocumentVersion{
		metadataRow("kb-1", "doc-1", 100, entity.JSONMap{"author": "alice"}),
		metadataRow("kb-1", "doc-2", 100, entity.JSONMap{"author": "bob"}),
		metadataRow("kb-2", "doc-3", 100, entity.JSONMap{"author": "carol"}),
	} {
		if err := StashMetadata(ctx, row); err != nil {
			t.Fatalf("stash: %v", err)
		}
	}

	if err := DeleteMetadataDataset(ctx, "kb-1"); err != nil {
		t.Fatalf("delete dataset: %v", err)
	}
	if ids := store.SnapshotIDs("kb-1"); len(ids) != 0 {
		t.Fatalf("snapshots of the removed dataset = %v, want none", ids)
	}
	if ids := store.SnapshotIDs("kb-2"); len(ids) != 1 {
		t.Fatalf("snapshots of the surviving dataset = %v, want its own untouched", ids)
	}
}

// TestPurgeWithoutStoreReportsUnavailable pins that a purge in a deployment with
// no store is reported, not silently counted as done.
func TestPurgeWithoutStoreReportsUnavailable(t *testing.T) {
	useNoStore(t)
	if err := DeleteMetadataDocument(t.Context(), "kb-1", "doc-1"); !errors.Is(err, ErrMetadataUnavailable) {
		t.Fatalf("delete document error = %v, want %v", err, ErrMetadataUnavailable)
	}
	if err := DeleteMetadataDataset(t.Context(), "kb-1"); !errors.Is(err, ErrMetadataUnavailable) {
		t.Fatalf("delete dataset error = %v, want %v", err, ErrMetadataUnavailable)
	}
	if MetadataAvailable() {
		t.Fatal("MetadataAvailable() reported a store where none is installed")
	}
}
