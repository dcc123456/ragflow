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

package document

import (
	"testing"

	"gorm.io/gorm"

	"ragflow/internal/entity"
)

// visibilityKinds are the dataset version kinds a visibility switch records: the
// unnamed kind rows written before the directions existed (and batches that no
// single direction describes), plus the two the switch names now.
var visibilityKinds = []string{
	entity.DatasetVersionKindDocumentStatus,
	entity.DatasetVersionKindDocumentEnable,
	entity.DatasetVersionKindDocumentDisable,
}

// loadStatusVersions reads the dataset's visibility versions, oldest first.
func loadStatusVersions(t *testing.T, db *gorm.DB, datasetID string) []*entity.DatasetVersion {
	t.Helper()
	var rows []*entity.DatasetVersion
	if err := db.Where("dataset_id = ? AND kind IN ?", datasetID, visibilityKinds).
		Order("version ASC").Find(&rows).Error; err != nil {
		t.Fatalf("load the visibility versions: %v", err)
	}
	return rows
}

// loadStatusStates reads the status state rows a version wrote.
func loadStatusStates(t *testing.T, db *gorm.DB) []entity.DocumentVersion {
	t.Helper()
	var rows []entity.DocumentVersion
	if err := db.Where("kind = ?", entity.DatasetVersionKindDocumentStatus).
		Order("dataset_version ASC, document_id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("load the status states: %v", err)
	}
	return rows
}

// TestBatchUpdateDocumentStatusRecordsOneVersion pins the shape of a batch
// switch: one dataset version for the whole call - the switch is one action - and
// one state row per document, so a restore can put each switch back.
func TestBatchUpdateDocumentStatusRecordsOneVersion(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	insertTestKB(t, "kb-1", "tenant-1", 2, 0, 0)
	insertTestDoc(t, "doc-1", "kb-1", 0, 0)
	insertTestDoc(t, "doc-2", "kb-1", 0, 0)

	svc := testDocumentService(t)
	ctx := documentTestActorContext(t.Context(), "user-1", "tenant-1")
	if _, code, err := svc.BatchUpdateDocumentStatus(ctx, "kb-1", "0", []string{"doc-1", "doc-2"}); err != nil {
		t.Fatalf("BatchUpdateDocumentStatus() code %d error %v", code, err)
	}

	versions := loadStatusVersions(t, db, "kb-1")
	if len(versions) != 1 {
		var all []entity.DatasetVersion
		db.Find(&all)
		t.Fatalf("visibility versions = %d, want 1 for the batch (all rows: %#v)", len(versions), all)
	}
	if versions[0].Kind != entity.DatasetVersionKindDocumentDisable {
		t.Fatalf("kind = %q, want the hidden direction %q", versions[0].Kind, entity.DatasetVersionKindDocumentDisable)
	}
	if len(versions[0].Changes) != 2 {
		t.Fatalf("change set = %#v, want both documents", versions[0].Changes)
	}
	states := loadStatusStates(t, db)
	if len(states) != 2 {
		t.Fatalf("status states = %d, want one per document", len(states))
	}
	for _, state := range states {
		if state.Status == nil || *state.Status != "0" {
			t.Fatalf("state of %s = %v, want the hidden status", state.DocumentID, state.Status)
		}
		if state.DatasetVersion != versions[0].Version {
			t.Fatalf("state of %s anchored at %d, want %d", state.DocumentID, state.DatasetVersion, versions[0].Version)
		}
	}

	// A switch into the status the documents already hold moves nothing, so it
	// records nothing: an entry there would say the dataset had changed.
	if _, code, err := svc.BatchUpdateDocumentStatus(ctx, "kb-1", "0", []string{"doc-1", "doc-2"}); err != nil {
		t.Fatalf("repeated BatchUpdateDocumentStatus() code %d error %v", code, err)
	}
	if got := len(loadStatusVersions(t, db, "kb-1")); got != 1 {
		t.Fatalf("visibility versions after repeating the switch = %d, want 1", got)
	}

	// Switching back is a version again, and it names only the documents it moved.
	if _, code, err := svc.BatchUpdateDocumentStatus(ctx, "kb-1", "1", []string{"doc-1"}); err != nil {
		t.Fatalf("re-enabling BatchUpdateDocumentStatus() code %d error %v", code, err)
	}
	versions = loadStatusVersions(t, db, "kb-1")
	if len(versions) != 2 {
		t.Fatalf("visibility versions after re-enabling = %d, want 2", len(versions))
	}
	if versions[1].Kind != entity.DatasetVersionKindDocumentEnable {
		t.Fatalf("kind = %q, want the enabled direction %q", versions[1].Kind, entity.DatasetVersionKindDocumentEnable)
	}
	if len(versions[1].Changes) != 1 || versions[1].Changes[0].DocID != "doc-1" {
		t.Fatalf("second change set = %#v, want only doc-1", versions[1].Changes)
	}
}

// TestRestoreDocumentStatusAtEEputsTheSwitchBack pins the restore half: the
// status a version recorded is what the document holds afterwards, and the switch
// the replay wrote is recorded under the restore's own version so a later restore
// can undo it.
func TestRestoreDocumentStatusAtEEputsTheSwitchBack(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	insertTestKB(t, "kb-1", "tenant-1", 1, 0, 0)
	insertTestDoc(t, "doc-1", "kb-1", 0, 0)

	svc := testDocumentService(t)
	ctx := documentTestActorContext(t.Context(), "user-1", "tenant-1")
	if _, _, err := svc.BatchUpdateDocumentStatus(ctx, "kb-1", "0", []string{"doc-1"}); err != nil {
		t.Fatalf("disable the document: %v", err)
	}
	versions := loadStatusVersions(t, db, "kb-1")
	if len(versions) != 1 {
		t.Fatalf("visibility versions = %d, want 1", len(versions))
	}
	if _, _, err := svc.BatchUpdateDocumentStatus(ctx, "kb-1", "1", []string{"doc-1"}); err != nil {
		t.Fatalf("re-enable the document: %v", err)
	}

	replayed, err := RestoreDocumentStatusAtEE(ctx, db, "kb-1", versions[0].Version)
	if err != nil {
		t.Fatalf("RestoreDocumentStatusAtEE() error = %v", err)
	}
	if len(replayed) != 1 || replayed[0].DocumentID != "doc-1" || replayed[0].Status != "0" {
		t.Fatalf("replayed = %#v, want the hidden status of doc-1", replayed)
	}
	var doc entity.Document
	if err := db.Where("id = ?", "doc-1").First(&doc).Error; err != nil {
		t.Fatalf("reload the document: %v", err)
	}
	if doc.Status == nil || *doc.Status != "0" {
		t.Fatalf("document status = %v, want the hidden status the version recorded", doc.Status)
	}

	// The switch already matches the version: replaying it again writes nothing.
	again, err := RestoreDocumentStatusAtEE(ctx, db, "kb-1", versions[0].Version)
	if err != nil {
		t.Fatalf("second RestoreDocumentStatusAtEE() error = %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("replayed = %#v on a second run, want nothing", again)
	}

	if err := RecordReplayedDocumentStatusAtVersionEE(ctx, db, versions[0].Version+10, replayed); err != nil {
		t.Fatalf("RecordReplayedDocumentStatusAtVersionEE() error = %v", err)
	}
	states := loadStatusStates(t, db)
	if len(states) != 3 {
		t.Fatalf("status states = %d, want the two switches and the replay", len(states))
	}
	last := states[len(states)-1]
	if last.DatasetVersion != versions[0].Version+10 || last.Status == nil || *last.Status != "0" {
		t.Fatalf("replayed state = %#v, want the hidden status at the restore's version", last)
	}
}

// TestRecordDocumentStatusChangesRecordsNothingForAnEmptyBatch pins the cheap
// case: a switch nobody moved is not a version, and it does not seed a baseline
// either.
func TestRecordDocumentStatusChangesRecordsNothingForAnEmptyBatch(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	insertTestKB(t, "kb-1", "tenant-1", 0, 0, 0)

	if err := RecordDocumentStatusChangesEE(t.Context(), db, "kb-1", nil); err != nil {
		t.Fatalf("RecordDocumentStatusChangesEE() error = %v", err)
	}
	var versions int64
	if err := db.Model(&entity.DatasetVersion{}).Where("dataset_id = ?", "kb-1").Count(&versions).Error; err != nil {
		t.Fatalf("count the versions: %v", err)
	}
	if versions != 0 {
		t.Fatalf("dataset versions = %d, want none", versions)
	}
}
