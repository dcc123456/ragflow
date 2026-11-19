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
	"errors"
	"testing"

	"gorm.io/gorm"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/documentset"
	"ragflow/internal/entity"
	"ragflow/internal/service/datasetversion"
	"ragflow/internal/storage"
)

// setupPruneTestDB is the service fixture, which already carries the version,
// document-set and removal tables: a prune reads the history to know what is
// still restorable, and the cleanup writes both sides.
func setupPruneTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	return db
}

// usePruneMemoryStorage installs an object store for the test and restores
// whatever the process had when it ends.
func usePruneMemoryStorage(t *testing.T) storage.Storage {
	t.Helper()
	factory := storage.GetStorageFactory()
	previous := factory.GetStorage()
	factory.SetStorage(storage.NewMemoryStorage())
	t.Cleanup(func() { factory.SetStorage(previous) })
	return factory.GetStorage()
}

func insertPruneDocument(t *testing.T, db *gorm.DB, id, datasetID, name string) {
	t.Helper()
	documentName := name
	if err := db.Create(&entity.Document{
		ID: id, KbID: datasetID, Name: &documentName, ParserConfig: entity.JSONMap{},
	}).Error; err != nil {
		t.Fatalf("insert document %s: %v", id, err)
	}
}

// recordPruneVersion appends a version that names the given document set - the
// canonical JSON, as datasetversion.DocumentSet reads it - and stores it by
// content, the shape a writer produces when it establishes one.
func recordPruneVersion(t *testing.T, db *gorm.DB, datasetID string, version int64, kind, documents string) {
	t.Helper()
	row := &entity.DatasetVersion{
		DatasetID: datasetID,
		Version:   version,
		Kind:      kind,
	}
	if documents != "" {
		raw := []byte(documents)
		hash := documentset.Hash(raw)
		if err := dao.NewDatasetDocumentSetSnapshotDAO().Upsert(t.Context(), db, datasetID, hash, documentset.Encode(raw)); err != nil {
			t.Fatalf("store the document set of version %d: %v", version, err)
		}
		row.DocumentsHash = &hash
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("insert dataset version %d: %v", version, err)
	}
}

// pruneRemovedDocument removes a document from the dataset and rows it the
// way the delete path does, leaving its blob in the object store.
func pruneRemovedDocument(t *testing.T, db *gorm.DB, store storage.Storage, datasetID, documentID, name, blob string) {
	t.Helper()
	if err := db.Where("id = ?", documentID).Delete(&entity.Document{}).Error; err != nil {
		t.Fatalf("remove document %s: %v", documentID, err)
	}
	if err := store.Put(t.Context(), datasetID, blob, []byte("blob of "+name)); err != nil {
		t.Fatalf("store the blob of %s: %v", documentID, err)
	}
	createTime := int64(2000)
	row := &entity.DatasetRemovedDocument{
		ID:         "removed-" + documentID,
		DatasetID:  datasetID,
		DocumentID: documentID,
		Document:   entity.JSONMap{"id": documentID, "kb_id": datasetID, "name": name},
		Files: entity.JSONMap{
			"files": []interface{}{
				map[string]interface{}{"id": "file-" + documentID, "name": name, "location": blob},
			},
			"mappings": []interface{}{},
		},
		CreateTime: &createTime,
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("insert the removal row of %s: %v", documentID, err)
	}
}

func removedRowCount(t *testing.T, db *gorm.DB, datasetID, documentID string) int64 {
	t.Helper()
	var count int64
	if err := db.Model(&entity.DatasetRemovedDocument{}).
		Where("dataset_id = ? AND document_id = ?", datasetID, documentID).
		Count(&count).Error; err != nil {
		t.Fatalf("count the removal rows of %s: %v", documentID, err)
	}
	return count
}

// TestPruneDropsTheRowNoVersionHolds is the reviewer's case: the delete
// version said the document was gone, and dropping the version that still
// contained it leaves no version that can bring it back - so the row and the
// object it kept in storage go too.
func TestPruneDropsTheRowNoVersionHolds(t *testing.T) {
	db := setupPruneTestDB(t)
	store := usePruneMemoryStorage(t)
	ctx := t.Context()

	insertPruneDocument(t, db, "doc-a", "kb-1", "a.pdf")
	insertPruneDocument(t, db, "doc-b", "kb-1", "b.pdf")
	withBoth, err := datasetversion.DocumentSet(ctx, db, "kb-1")
	if err != nil {
		t.Fatalf("record the document set: %v", err)
	}
	recordPruneVersion(t, db, "kb-1", 1, entity.DatasetVersionKindUpload, withBoth)

	pruneRemovedDocument(t, db, store, "kb-1", "doc-b", "b.pdf", "blob-b")
	onlyA, err := datasetversion.DocumentSet(ctx, db, "kb-1")
	if err != nil {
		t.Fatalf("record the document set after the removal: %v", err)
	}
	recordPruneVersion(t, db, "kb-1", 2, entity.DatasetVersionKindDelete, onlyA)

	// Version 1 still holds doc-b, so nothing may be dropped yet.
	if pruned, err := pruneUnreferencedRemovedDocuments(ctx, db, "kb-1"); err != nil || pruned != 0 {
		t.Fatalf("prune with version 1 still holding doc-b = (%d, %v), want (0, nil)", pruned, err)
	}
	if removedRowCount(t, db, "kb-1", "doc-b") != 1 {
		t.Fatal("the removal row of doc-b was dropped while version 1 could still restore it")
	}

	// Version 1 is the one being deleted; version 2 is what survives.
	if err := db.Where("dataset_id = ? AND version = ?", "kb-1", 1).Delete(&entity.DatasetVersion{}).Error; err != nil {
		t.Fatalf("delete dataset version 1: %v", err)
	}
	pruned, err := pruneUnreferencedRemovedDocuments(ctx, db, "kb-1")
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("pruned = %d, want 1 (doc-b)", pruned)
	}
	if removedRowCount(t, db, "kb-1", "doc-b") != 0 {
		t.Fatal("the removal row of doc-b outlived the last version that could restore it")
	}
	if store.ObjExist(ctx, "kb-1", "blob-b") {
		t.Fatal("the blob of doc-b is still in object storage")
	}
	// doc-a is live: the prune must not have touched it.
	var live int64
	if err := db.Model(&entity.Document{}).Where("id = ?", "doc-a").Count(&live).Error; err != nil {
		t.Fatalf("count the live documents: %v", err)
	}
	if live != 1 {
		t.Fatalf("doc-a rows = %d, want the live document untouched", live)
	}
}

// TestPruneKeepsTheRowAVersionStillHolds pins the other side: the removal row of
// a document that some surviving version still contains stays, blob included,
// because that version can put it back.
func TestPruneKeepsTheRowAVersionStillHolds(t *testing.T) {
	db := setupPruneTestDB(t)
	store := usePruneMemoryStorage(t)
	ctx := t.Context()

	insertPruneDocument(t, db, "doc-a", "kb-1", "a.pdf")
	insertPruneDocument(t, db, "doc-b", "kb-1", "b.pdf")
	withBoth, err := datasetversion.DocumentSet(ctx, db, "kb-1")
	if err != nil {
		t.Fatalf("record the document set: %v", err)
	}
	// Only one version survives, and it still holds doc-b. The row exists
	// because the document was removed and then the version that removed it was
	// deleted, or because the document was removed before the set was recorded.
	recordPruneVersion(t, db, "kb-1", 1, entity.DatasetVersionKindUpload, withBoth)
	pruneRemovedDocument(t, db, store, "kb-1", "doc-b", "b.pdf", "blob-b")

	pruned, err := pruneUnreferencedRemovedDocuments(ctx, db, "kb-1")
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if pruned != 0 {
		t.Fatalf("pruned = %d, want 0 while version 1 still holds doc-b", pruned)
	}
	if removedRowCount(t, db, "kb-1", "doc-b") != 1 {
		t.Fatal("the removal row of doc-b was dropped although version 1 can restore it")
	}
	if !store.ObjExist(ctx, "kb-1", "blob-b") {
		t.Fatal("the blob of doc-b was dropped although version 1 can restore it")
	}
}

// TestPruneDropsNothingWithoutARecordedSet pins the conservative half: a version
// whose set was never recorded could restore anything, so the pass must leave
// every removal row where it is instead of reading a missing set as an empty one.
func TestPruneDropsNothingWithoutARecordedSet(t *testing.T) {
	db := setupPruneTestDB(t)
	store := usePruneMemoryStorage(t)
	ctx := t.Context()

	insertPruneDocument(t, db, "doc-a", "kb-1", "a.pdf")
	// A row that recorded no set at all.
	recordPruneVersion(t, db, "kb-1", 1, entity.DatasetVersionKindUpload, "")
	pruneRemovedDocument(t, db, store, "kb-1", "doc-b", "b.pdf", "blob-b")

	pruned, err := pruneUnreferencedRemovedDocuments(ctx, db, "kb-1")
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if pruned != 0 {
		t.Fatalf("pruned = %d, want 0 when a surviving set is unknown", pruned)
	}
	if removedRowCount(t, db, "kb-1", "doc-b") != 1 || !store.ObjExist(ctx, "kb-1", "blob-b") {
		t.Fatal("the removal row was dropped on the strength of a set that was never recorded")
	}
}

// TestPruneSkipsLiveDocuments pins that a document which came back is not
// treated as garbage: its row is consumed by the restore, so there is
// nothing to drop, and the per-document cleanup would refuse it anyway. The pass
// must skip it rather than fail on the refusal.
func TestPruneSkipsLiveDocuments(t *testing.T) {
	db := setupPruneTestDB(t)
	usePruneMemoryStorage(t)
	ctx := t.Context()

	insertPruneDocument(t, db, "doc-a", "kb-1", "a.pdf")
	emptySet, err := datasetversion.DocumentSet(ctx, db, "kb-1")
	if err != nil {
		t.Fatalf("record the empty document set: %v", err)
	}
	recordPruneVersion(t, db, "kb-1", 1, entity.DatasetVersionKindDelete, emptySet)
	// A leftover removal row of a document that is live again.
	insertPruneDocument(t, db, "doc-b", "kb-1", "b.pdf")
	createTime := int64(2000)
	if err := db.Create(&entity.DatasetRemovedDocument{
		ID:         "removed-doc-b",
		DatasetID:  "kb-1",
		DocumentID: "doc-b",
		Document:   entity.JSONMap{"id": "doc-b", "kb_id": "kb-1", "name": "b.pdf"},
		Files:      entity.JSONMap{"files": []interface{}{}, "mappings": []interface{}{}},
		CreateTime: &createTime,
	}).Error; err != nil {
		t.Fatalf("insert the removal row of doc-b: %v", err)
	}

	pruned, err := pruneUnreferencedRemovedDocuments(ctx, db, "kb-1")
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if pruned != 0 {
		t.Fatalf("pruned = %d, want 0 for a live document", pruned)
	}
	if removedRowCount(t, db, "kb-1", "doc-b") != 1 {
		t.Fatal("the removal row of a live document was dropped")
	}
}

// TestDeleteDatasetVersionPrunesWhatNoVersionCanRestore pins the cleanup at the
// service boundary rather than in the HTTP handler: whoever deletes the version -
// a request, a CLI, an internal task - reclaims the removal rows whose last
// restorable version just went away.
func TestDeleteDatasetVersionPrunesWhatNoVersionCanRestore(t *testing.T) {
	db := setupPruneTestDB(t)
	store := usePruneMemoryStorage(t)
	ctx := t.Context()

	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")
	if err := db.Model(&entity.Knowledgebase{}).Where("id = ?", "kb-1").Update("version", 2).Error; err != nil {
		t.Fatalf("pin the current version: %v", err)
	}

	insertPruneDocument(t, db, "doc-a", "kb-1", "a.pdf")
	insertPruneDocument(t, db, "doc-b", "kb-1", "b.pdf")
	withBoth, err := datasetversion.DocumentSet(ctx, db, "kb-1")
	if err != nil {
		t.Fatalf("record the document set: %v", err)
	}
	// doc-b is removed after the upload above, so only version 1 still holds it.
	pruneRemovedDocument(t, db, store, "kb-1", "doc-b", "b.pdf", "blob-b")
	onlyA, err := datasetversion.DocumentSet(ctx, db, "kb-1")
	if err != nil {
		t.Fatalf("record the document set after the removal: %v", err)
	}
	recordPruneVersion(t, db, "kb-1", 0, entity.DatasetVersionKindBaseline, onlyA)
	recordPruneVersion(t, db, "kb-1", 1, entity.DatasetVersionKindUpload, withBoth)
	recordPruneVersion(t, db, "kb-1", 2, entity.DatasetVersionKindDelete, onlyA)

	svc := testDatasetUpdateService(t)
	permissionCtx := withDatasetTestContext(ctx, "tenant-1", "tenant-1")
	if _, err := svc.DeleteDatasetVersion(permissionCtx, &DeleteDatasetVersionRequest{DatasetID: "kb-1", Version: 1}); err != nil {
		t.Fatalf("delete version 1: %v", err)
	}

	if removedRowCount(t, db, "kb-1", "doc-b") != 0 {
		t.Fatal("the removal row of doc-b outlived the last version that could restore it")
	}
	if store.ObjExist(ctx, "kb-1", "blob-b") {
		t.Fatal("the blob of doc-b is still in object storage")
	}
}

// TestPurgeRemovedDocumentEERefusesALiveDocument keeps the cleanup away from a
// document that was restored: its chunk history is what older versions replay.
func TestPurgeRemovedDocumentEERefusesALiveDocument(t *testing.T) {
	db := setupPruneTestDB(t)
	usePruneMemoryStorage(t)
	ctx := t.Context()

	insertPruneDocument(t, db, "doc-live", "kb-1", "live.pdf")
	// A removal row that a restore has not consumed yet, while the document is
	// already back: the shape a restore leaves between its two steps.
	if err := db.Create(&entity.DatasetRemovedDocument{
		ID:         "removed-live",
		DatasetID:  "kb-1",
		DocumentID: "doc-live",
		Document:   entity.JSONMap{"id": "doc-live", "kb_id": "kb-1", "name": "live.pdf"},
		Files:      entity.JSONMap{"files": []interface{}{}, "mappings": []interface{}{}},
	}).Error; err != nil {
		t.Fatalf("insert the removal row of doc-live: %v", err)
	}

	cleaned, err := PurgeRemovedDocumentEE(ctx, "kb-1", "doc-live")
	if err == nil || cleaned {
		t.Fatalf("purge of a live document = (%v, %v), want a refusal", cleaned, err)
	}
	var coded interface{ Code() common.ErrorCode }
	if !errors.As(err, &coded) || coded.Code() != common.CodeConflict {
		t.Fatalf("error = %v, want %v", err, common.CodeConflict)
	}
	if removedRowCount(t, db, "kb-1", "doc-live") != 1 {
		t.Fatal("the removal row of a live document was dropped")
	}
}

// TestPurgeRemovedDocumentEEWithoutRows treats a second cleanup as a success: a
// retried request must not fail on work that is already done.
func TestPurgeRemovedDocumentEEWithoutRows(t *testing.T) {
	setupPruneTestDB(t)
	usePruneMemoryStorage(t)

	cleaned, err := PurgeRemovedDocumentEE(t.Context(), "kb-1", "doc-gone")
	if err != nil || cleaned {
		t.Fatalf("purge without a removal row = (%v, %v), want (false, nil)", cleaned, err)
	}
}

// TestGetDatasetVersionNamesItsDocuments pins the Files list of a version: the
// stored document set holds ids, and the detail response has to answer the names
// the panel shows instead of leaving the reader with ids.
func TestGetDatasetVersionNamesItsDocuments(t *testing.T) {
	db := setupPruneTestDB(t)
	ctx := t.Context()

	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")
	insertPruneDocument(t, db, "doc-a", "kb-1", "a.pdf")
	insertPruneDocument(t, db, "doc-b", "kb-1", "b.pdf")
	documents, err := datasetversion.DocumentSet(ctx, db, "kb-1")
	if err != nil {
		t.Fatalf("record the document set: %v", err)
	}
	recordPruneVersion(t, db, "kb-1", 1, entity.DatasetVersionKindUpload, documents)

	svc := testDatasetUpdateService(t)
	detail, err := svc.GetDatasetVersion(withDatasetTestContext(ctx, "tenant-1", "tenant-1"), &DatasetVersionRequest{DatasetID: "kb-1", Version: 1})
	if err != nil {
		t.Fatalf("get version 1: %v", err)
	}
	names := map[string]string{}
	for _, document := range detail.Documents {
		names[document.ID] = document.Name
	}
	if len(names) != 2 {
		t.Fatalf("documents = %#v, want both of the set", detail.Documents)
	}
	if names["doc-a"] != "a.pdf" || names["doc-b"] != "b.pdf" {
		t.Fatalf("documents = %#v, want their names", detail.Documents)
	}
}

// TestGetDatasetVersionNamesADocumentThatIsGone pins the other half of the Files
// list: a version still holds documents that have since left the dataset, and
// their names have to come from the change set that recorded them - otherwise the
// panel lists ids.
func TestGetDatasetVersionNamesADocumentThatIsGone(t *testing.T) {
	db := setupPruneTestDB(t)
	ctx := t.Context()

	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")
	insertPruneDocument(t, db, "doc-a", "kb-1", "a.pdf")
	documents, err := datasetversion.DocumentSet(ctx, db, "kb-1")
	if err != nil {
		t.Fatalf("record the document set: %v", err)
	}
	recordPruneVersion(t, db, "kb-1", 1, entity.DatasetVersionKindUpload, documents)
	if err := db.Model(&entity.DatasetVersion{}).
		Where("dataset_id = ? AND version = ?", "kb-1", 1).
		Update("changes", entity.DatasetVersionChanges{{
			Entity: entity.DatasetVersionChangeEntityDocument,
			Action: entity.DatasetVersionChangeActionAdd,
			DocID:  "doc-a",
			Name:   "a.pdf",
		}}).Error; err != nil {
		t.Fatalf("record the change set: %v", err)
	}
	// The document leaves afterwards: no row is left to read a name from, only the
	// change set above.
	if err := db.Where("id = ?", "doc-a").Delete(&entity.Document{}).Error; err != nil {
		t.Fatalf("remove doc-a: %v", err)
	}

	svc := testDatasetUpdateService(t)
	detail, err := svc.GetDatasetVersion(withDatasetTestContext(ctx, "tenant-1", "tenant-1"), &DatasetVersionRequest{DatasetID: "kb-1", Version: 1})
	if err != nil {
		t.Fatalf("get version 1: %v", err)
	}
	if len(detail.Documents) != 1 || detail.Documents[0].Name != "a.pdf" {
		t.Fatalf("documents = %#v, want doc-a named a.pdf", detail.Documents)
	}
}

// TestGetDatasetVersionNamesADocumentRenamedAfterwards pins the backward half of
// the Files list: a version older than a rename predates every entry that names
// the document, and the document row only carries the name it has now. The rename
// entry keeps the name it replaced, so the older version still reads what the file
// was called then instead of showing the new name against the old content.
func TestGetDatasetVersionNamesADocumentRenamedAfterwards(t *testing.T) {
	db := setupPruneTestDB(t)
	ctx := t.Context()

	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")
	insertPruneDocument(t, db, "doc-a", "kb-1", "d.xlsx")
	documents, err := datasetversion.DocumentSet(ctx, db, "kb-1")
	if err != nil {
		t.Fatalf("record the document set: %v", err)
	}
	recordPruneVersion(t, db, "kb-1", 1, entity.DatasetVersionKindUpload, documents)
	// Version 2 renames it, and the document row carries the new name from then
	// on: version 1 has no rename of its own and no row with the old name.
	if err := db.Create(&entity.DatasetVersion{
		DatasetID: "kb-1",
		Version:   2,
		Kind:      entity.DatasetVersionKindDocumentRename,
		Changes: entity.DatasetVersionChanges{{
			Entity:       entity.DatasetVersionChangeEntityDocument,
			Action:       entity.DatasetVersionChangeActionRename,
			DocID:        "doc-a",
			Name:         "dd.xlsx",
			PreviousName: "d.xlsx",
		}},
	}).Error; err != nil {
		t.Fatalf("record the rename: %v", err)
	}
	if err := db.Model(&entity.Document{}).
		Where("id = ?", "doc-a").
		Update("name", "dd.xlsx").Error; err != nil {
		t.Fatalf("rename the document row: %v", err)
	}

	svc := testDatasetUpdateService(t)
	cctx := withDatasetTestContext(ctx, "tenant-1", "tenant-1")
	before, err := svc.GetDatasetVersion(cctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: 1})
	if err != nil {
		t.Fatalf("get version 1: %v", err)
	}
	if len(before.Documents) != 1 || before.Documents[0].Name != "d.xlsx" {
		t.Fatalf("version 1 documents = %#v, want doc-a named d.xlsx", before.Documents)
	}
	after, err := svc.GetDatasetVersion(cctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: 2})
	if err != nil {
		t.Fatalf("get version 2: %v", err)
	}
	// The version that renamed it reads the new name from its own change set.
	if len(after.Changes) != 1 || after.Changes[0].Name != "dd.xlsx" {
		t.Fatalf("version 2 changes = %#v, want the rename entry", after.Changes)
	}
}
