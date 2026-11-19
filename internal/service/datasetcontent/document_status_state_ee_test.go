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
	"net/url"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/service/document"
)

// setupDocumentStatusDB opens a database with the rows a visibility version
// touches - the history, the state rows and the documents they describe - and
// points dao.DB at it for the test's lifetime.
func setupDocumentStatusDB(t *testing.T) *gorm.DB {
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
	if err := db.AutoMigrate(
		&entity.DatasetVersion{},
		&entity.DatasetConfigSnapshot{},
		&entity.DatasetDocumentSetSnapshot{},
		&entity.DatasetRemovedDocument{},
		&entity.DocumentVersion{},
		&entity.Connector2Kb{},
		&entity.SyncLogs{},
		&entity.Knowledgebase{},
		&entity.Document{},
	); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	oldDB := dao.DB
	dao.DB = db
	t.Cleanup(func() { dao.DB = oldDB })

	valid := string(entity.StatusValid)
	if err := db.Create(&entity.Knowledgebase{
		ID: "kb-1", TenantID: "tenant-1", Name: "test-kb", EmbdID: "embd-1",
		CreatedBy: "user-1", Permission: string(entity.TenantPermissionTeam), Status: &valid,
	}).Error; err != nil {
		t.Fatalf("seed dataset: %v", err)
	}
	return db
}

// seedStatusDocument inserts a document the visibility history can describe.
func seedStatusDocument(t *testing.T, db *gorm.DB, documentID string, status string) {
	t.Helper()
	if err := db.Create(&entity.Document{
		ID: documentID, KbID: "kb-1", ParserID: "naive", ParserConfig: entity.JSONMap{},
		CreatedBy: "user-1", Status: &status, Suffix: ".txt",
	}).Error; err != nil {
		t.Fatalf("seed document %s: %v", documentID, err)
	}
}

// loadVisibilityVersions reads the dataset's visibility versions, oldest first.
// The direction has a kind of its own, so the unnamed kind alone would miss the
// rows this writes today.
func loadVisibilityVersions(t *testing.T, db *gorm.DB) []*entity.DatasetVersion {
	t.Helper()
	var rows []*entity.DatasetVersion
	if err := db.Where("dataset_id = ? AND kind IN ?", "kb-1", []string{
		entity.DatasetVersionKindDocumentStatus,
		entity.DatasetVersionKindDocumentEnable,
		entity.DatasetVersionKindDocumentDisable,
	}).Order("version ASC").Find(&rows).Error; err != nil {
		t.Fatalf("load the visibility versions: %v", err)
	}
	return rows
}

// loadVisibilityStates reads the status state rows, oldest version first.
func loadVisibilityStates(t *testing.T, db *gorm.DB) []entity.DocumentVersion {
	t.Helper()
	var rows []entity.DocumentVersion
	if err := db.Where("kind = ?", entity.DatasetVersionKindDocumentStatus).
		Order("dataset_version ASC, document_id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("load the status states: %v", err)
	}
	return rows
}

// TestRecordDocumentStatusChangesRecordsOneVersion pins the shape of a batch
// switch: one dataset version for the whole call - the switch is one action - and
// one state row per document, so a restore can put each switch back.
func TestRecordDocumentStatusChangesRecordsOneVersion(t *testing.T) {
	db := setupDocumentStatusDB(t)
	seedStatusDocument(t, db, "doc-1", string(entity.StatusValid))
	seedStatusDocument(t, db, "doc-2", string(entity.StatusValid))

	ctx := t.Context()
	if err := document.RecordDocumentStatusChangesEE(ctx, db, "kb-1", []document.DocumentStatusChange{
		{DocumentID: "doc-1", DatasetID: "kb-1", Name: "a.pdf", Status: "0"},
		{DocumentID: "doc-2", DatasetID: "kb-1", Name: "b.pdf", Status: "0"},
	}); err != nil {
		t.Fatalf("RecordDocumentStatusChangesEE() error = %v", err)
	}

	versions := loadVisibilityVersions(t, db)
	if len(versions) != 1 {
		t.Fatalf("visibility versions = %d, want 1 for the batch", len(versions))
	}
	if versions[0].Kind != entity.DatasetVersionKindDocumentDisable {
		t.Fatalf("kind = %q, want %q", versions[0].Kind, entity.DatasetVersionKindDocumentDisable)
	}
	if len(versions[0].Changes) != 2 {
		t.Fatalf("change set = %#v, want both documents", versions[0].Changes)
	}
	states := loadVisibilityStates(t, db)
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

	// A batch that moved no document is not a moment: it records nothing, and a
	// document that already holds the status is not a state change either.
	if err := document.RecordDocumentStatusChangesEE(ctx, db, "kb-1", nil); err != nil {
		t.Fatalf("RecordDocumentStatusChangesEE(empty) error = %v", err)
	}
	if got := len(loadVisibilityVersions(t, db)); got != 1 {
		t.Fatalf("visibility versions after an empty batch = %d, want 1", got)
	}
}

// TestRecordDocumentStatusChangesNamesTheDirection pins the label the timeline
// reads: a batch that disabled documents records document_disable, one that
// enabled them document_enable, and a status that is neither keeps the unnamed
// kind, because no kind can name a direction it does not know.
func TestRecordDocumentStatusChangesNamesTheDirection(t *testing.T) {
	db := setupDocumentStatusDB(t)
	seedStatusDocument(t, db, "doc-1", string(entity.StatusValid))
	ctx := t.Context()

	record := func(status string) string {
		t.Helper()
		if err := document.RecordDocumentStatusChangesEE(ctx, db, "kb-1", []document.DocumentStatusChange{
			{DocumentID: "doc-1", DatasetID: "kb-1", Name: "a.pdf", Status: status},
		}); err != nil {
			t.Fatalf("record status %q: %v", status, err)
		}
		versions := loadVisibilityVersions(t, db)
		if len(versions) == 0 {
			t.Fatalf("status %q recorded no version", status)
		}
		return versions[len(versions)-1].Kind
	}

	if got := record(string(entity.StatusInvalid)); got != entity.DatasetVersionKindDocumentDisable {
		t.Fatalf("disable kind = %q, want %q", got, entity.DatasetVersionKindDocumentDisable)
	}
	if got := record(string(entity.StatusValid)); got != entity.DatasetVersionKindDocumentEnable {
		t.Fatalf("enable kind = %q, want %q", got, entity.DatasetVersionKindDocumentEnable)
	}
	if got := record("2"); got != entity.DatasetVersionKindDocumentStatus {
		t.Fatalf("unknown status kind = %q, want the unnamed %q", got, entity.DatasetVersionKindDocumentStatus)
	}
	// The change set names the direction too: that is what the line under the
	// version reads ("Enabled document a.pdf"), and the kind is derived from it.
	versions := loadVisibilityVersions(t, db)
	wantActions := []string{
		entity.DatasetVersionChangeActionDisable,
		entity.DatasetVersionChangeActionEnable,
		entity.DatasetVersionChangeActionUpdate,
	}
	if len(versions) != len(wantActions) {
		t.Fatalf("visibility versions = %d, want %d", len(versions), len(wantActions))
	}
	for i, version := range versions {
		if len(version.Changes) != 1 || version.Changes[0].Action != wantActions[i] {
			t.Fatalf("version %d change = %#v, want one entry with action %q",
				version.Version, version.Changes, wantActions[i])
		}
	}
	// The state rows keep the unnamed kind whatever the direction: that is the
	// value a restore reads them by.
	for _, state := range loadVisibilityStates(t, db) {
		if state.Kind != entity.DatasetVersionKindDocumentStatus {
			t.Fatalf("state row kind = %q, want %q", state.Kind, entity.DatasetVersionKindDocumentStatus)
		}
	}
}
