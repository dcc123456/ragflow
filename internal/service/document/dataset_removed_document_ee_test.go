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
	"net/url"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"ragflow/internal/dao"
	"ragflow/internal/entity"
)

func setupRemovedDocumentTestDB(t *testing.T) *gorm.DB {
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
		&entity.Knowledgebase{},
		&entity.Document{},
		&entity.File{},
		&entity.File2Document{},
		&entity.DatasetRemovedDocument{},
	); err != nil {
		t.Fatalf("failed to migrate the removed-document tables: %v", err)
	}

	oldDB := dao.DB
	dao.DB = db
	t.Cleanup(func() { dao.DB = oldDB })
	return db
}

func removedDocumentStringPtr(value string) *string { return &value }

// TestRemovedDocumentRowStampsItsRemovalMoment pins the single timestamp a removal row
// carries. It is written by the create hook rather than by the caller, because
// the "last removal wins" lookup and the removed-documents list both order by
// it, and a caller that forgets it would leave those reads without an order.
func TestRemovedDocumentRowStampsItsRemovalMoment(t *testing.T) {
	db := setupRemovedDocumentTestDB(t)

	row := &entity.DatasetRemovedDocument{
		ID:         "removed-1",
		DatasetID:  "kb-1",
		DocumentID: "doc-1",
		Document:   entity.JSONMap{"id": "doc-1", "kb_id": "kb-1"},
		Files:      entity.JSONMap{"files": []interface{}{}, "mappings": []interface{}{}},
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("insert removal row: %v", err)
	}
	if row.CreateTime == nil || *row.CreateTime <= 0 {
		t.Fatalf("create_time = %v, want the moment of the removal", row.CreateTime)
	}

	var stored entity.DatasetRemovedDocument
	if err := db.Where("id = ?", "removed-1").Take(&stored).Error; err != nil {
		t.Fatalf("reload removal row: %v", err)
	}
	if stored.CreateTime == nil || *stored.CreateTime != *row.CreateTime {
		t.Fatalf("stored create_time = %v, want %d", stored.CreateTime, *row.CreateTime)
	}
}

// TestRemovalAndRestoreDocumentRoundTrip covers the promise the content history
// makes: a document removed after a removal is written comes back with its file
// linkage and the dataset counter, and it comes back unparsed because its chunks
// are not in the retrieval engine any more.
func TestRemovalAndRestoreDocumentRoundTrip(t *testing.T) {
	db := setupRemovedDocumentTestDB(t)

	kb := &entity.Knowledgebase{
		ID:           "kb-1",
		TenantID:     "tenant-1",
		Name:         "Dataset",
		CreatedBy:    "tenant-1",
		ParserConfig: entity.JSONMap{},
		DocNum:       1,
	}
	if err := db.Create(kb).Error; err != nil {
		t.Fatalf("insert dataset: %v", err)
	}
	doc := &entity.Document{
		ID:           "doc-1",
		KbID:         "kb-1",
		ParserID:     "naive",
		ParserConfig: entity.JSONMap{},
		Type:         "pdf",
		CreatedBy:    "tenant-1",
		SourceType:   "local",
		Suffix:       "pdf",
		Name:         removedDocumentStringPtr("a.pdf"),
		Location:     removedDocumentStringPtr("kb-1/a.pdf"),
		ChunkNum:     4,
		TokenNum:     100,
		Progress:     1,
	}
	if err := db.Create(doc).Error; err != nil {
		t.Fatalf("insert document: %v", err)
	}
	file := &entity.File{
		ID:        "file-1",
		ParentID:  "folder-1",
		TenantID:  "tenant-1",
		CreatedBy: "tenant-1",
		Name:      "a.pdf",
		Type:      "pdf",
		Location:  removedDocumentStringPtr("kb-1/a.pdf"),
	}
	if err := db.Create(file).Error; err != nil {
		t.Fatalf("insert file: %v", err)
	}
	fileID, documentID := "file-1", "doc-1"
	if err := db.Create(&entity.File2Document{ID: "f2d-1", FileID: &fileID, DocumentID: &documentID}).Error; err != nil {
		t.Fatalf("insert file mapping: %v", err)
	}

	if err := recordDocumentRemovalEE(t.Context(), doc, kb); err != nil {
		t.Fatalf("record the removal: %v", err)
	}

	// Delete the way the document path does: the row and the counter go, the
	// blob and the removal row stays.
	if err := db.Delete(&entity.Document{}, "id = ?", "doc-1").Error; err != nil {
		t.Fatalf("delete document: %v", err)
	}
	if err := db.Model(&entity.Knowledgebase{}).Where("id = ?", "kb-1").
		Update("doc_num", gorm.Expr("doc_num - 1")).Error; err != nil {
		t.Fatalf("decrement doc_num: %v", err)
	}
	if err := db.Delete(&entity.File{}, "id = ?", "file-1").Error; err != nil {
		t.Fatalf("delete file: %v", err)
	}
	if err := db.Delete(&entity.File2Document{}, "id = ?", "f2d-1").Error; err != nil {
		t.Fatalf("delete file mapping: %v", err)
	}

	restored, err := RestoreRemovedDocumentEE(t.Context(), "kb-1", "doc-1")
	if err != nil {
		t.Fatalf("restore document: %v", err)
	}
	if !restored {
		t.Fatal("restore reported the document could not come back")
	}

	var back entity.Document
	if err := db.Where("id = ?", "doc-1").Take(&back).Error; err != nil {
		t.Fatalf("reload restored document: %v", err)
	}
	if back.Name == nil || *back.Name != "a.pdf" {
		t.Fatalf("restored name = %v, want a.pdf", back.Name)
	}
	if back.ChunkNum != 0 || back.TokenNum != 0 || back.Progress != 0 {
		t.Fatalf("restored counters = %d/%d/%v, want an unparsed document",
			back.ChunkNum, back.TokenNum, back.Progress)
	}

	var counter entity.Knowledgebase
	if err := db.Where("id = ?", "kb-1").Take(&counter).Error; err != nil {
		t.Fatalf("reload dataset: %v", err)
	}
	if counter.DocNum != 1 {
		t.Fatalf("doc_num = %d, want 1", counter.DocNum)
	}

	var fileCount, mappingCount int64
	if err := db.Model(&entity.File{}).Where("id = ?", "file-1").Count(&fileCount).Error; err != nil {
		t.Fatalf("count files: %v", err)
	}
	if err := db.Model(&entity.File2Document{}).Where("id = ?", "f2d-1").Count(&mappingCount).Error; err != nil {
		t.Fatalf("count mappings: %v", err)
	}
	if fileCount != 1 || mappingCount != 1 {
		t.Fatalf("file/mapping restored = %d/%d, want 1/1", fileCount, mappingCount)
	}

	// The restore consumes the removal rows in the transaction that puts the
	// document back: a live document is not a removed document, and its row
	// would be hidden from the cleanup list and refused by the cleanup call.
	// Only the rows go; the blobs stay with the file rows the restore inserted.
	var removedCount int64
	if err := db.Model(&entity.DatasetRemovedDocument{}).
		Where("dataset_id = ? AND document_id = ?", "kb-1", "doc-1").
		Count(&removedCount).Error; err != nil {
		t.Fatalf("count removal rows: %v", err)
	}
	if removedCount != 0 {
		t.Fatalf("removal rows after restore = %d, want the removal consumed", removedCount)
	}
}

// TestRemovalOmitsTheParseState covers what the removed-document JSON does not carry:
// the run the document was in. A restore resets it and the chunk replay
// recomputes it, so storing it would only put a stale progress message (longtext,
// and a whole error trace on a failed parse) in the removal row.
func TestRemovalOmitsTheParseState(t *testing.T) {
	db := setupRemovedDocumentTestDB(t)

	kb := &entity.Knowledgebase{
		ID:           "kb-1",
		TenantID:     "tenant-1",
		Name:         "Dataset",
		CreatedBy:    "tenant-1",
		ParserConfig: entity.JSONMap{},
		DocNum:       1,
	}
	if err := db.Create(kb).Error; err != nil {
		t.Fatalf("insert dataset: %v", err)
	}
	progressMsg := "Restored 5 chunk(s) from the version history"
	beginAt := time.Now()
	status := "1"
	contentHash := "24e6e4c5c74528ef14e04a48fa1a4d01"
	doc := &entity.Document{
		ID:              "doc-1",
		KbID:            "kb-1",
		ParserID:        "naive",
		ParserConfig:    entity.JSONMap{"chunk_token_num": 128},
		Type:            "pdf",
		Suffix:          "pdf",
		CreatedBy:       "tenant-1",
		UserID:          "tenant-1",
		Name:            removedDocumentStringPtr("a.pdf"),
		Location:        removedDocumentStringPtr("kb-1/a.pdf"),
		Size:            137114,
		Progress:        1,
		ProgressMsg:     &progressMsg,
		ProcessBeginAt:  &beginAt,
		ProcessDuration: 17.2,
		ChunkNum:        5,
		TokenNum:        2634,
		Status:          &status,
		ContentHash:     &contentHash,
	}
	if err := db.Create(doc).Error; err != nil {
		t.Fatalf("insert document: %v", err)
	}

	if err := recordDocumentRemovalEE(t.Context(), doc, kb); err != nil {
		t.Fatalf("record the removal: %v", err)
	}

	var row entity.DatasetRemovedDocument
	if err := db.Where("document_id = ?", "doc-1").Take(&row).Error; err != nil {
		t.Fatalf("reload removal row: %v", err)
	}
	for _, key := range removedDocumentRuntimeKeys {
		if _, ok := row.Document[key]; ok {
			t.Fatalf("removed document carries the parse state %q", key)
		}
	}
	// Everything that describes the document itself is still there: a restore
	// rebuilds the row from these.
	for _, key := range []string{
		"id", "kb_id", "name", "location", "size", "type", "suffix",
		"parser_id", "parser_config", "created_by", "user_id", "content_hash", "status",
	} {
		if _, ok := row.Document[key]; !ok {
			t.Fatalf("removed document lost the identity field %q", key)
		}
	}

	// The document comes back with no parse state, whatever the row held.
	if err := db.Delete(&entity.Document{}, "id = ?", "doc-1").Error; err != nil {
		t.Fatalf("delete document: %v", err)
	}
	restored, err := RestoreRemovedDocumentEE(t.Context(), "kb-1", "doc-1")
	if err != nil {
		t.Fatalf("restore document: %v", err)
	}
	if !restored {
		t.Fatal("restore reported the document could not come back")
	}
	var back entity.Document
	if err := db.Where("id = ?", "doc-1").Take(&back).Error; err != nil {
		t.Fatalf("reload restored document: %v", err)
	}
	if back.Progress != 0 || back.ChunkNum != 0 || back.TokenNum != 0 ||
		back.ProcessDuration != 0 || back.ProgressMsg != nil || back.ProcessBeginAt != nil {
		t.Fatalf("restored parse state = %v/%d/%d/%v, want it reset",
			back.Progress, back.ChunkNum, back.TokenNum, back.ProgressMsg)
	}
	if back.ParserID != "naive" || back.ContentHash == nil || *back.ContentHash != contentHash {
		t.Fatalf("restored identity = parser %q hash %v, want it copied from the removal row",
			back.ParserID, back.ContentHash)
	}
}

// TestRestoreRemovedDocumentConsumesLeftoverRows covers a retried restore: the
// document is already back, so the removal rows are a leftover. They are
// consumed without touching the blobs, which the live file rows still point at.
func TestRestoreRemovedDocumentConsumesLeftoverRows(t *testing.T) {
	db := setupRemovedDocumentTestDB(t)

	if err := db.Create(&entity.Document{
		ID:           "doc-1",
		KbID:         "kb-1",
		ParserConfig: entity.JSONMap{},
	}).Error; err != nil {
		t.Fatalf("insert live document: %v", err)
	}
	row := &entity.DatasetRemovedDocument{
		ID:         "removed-1",
		DatasetID:  "kb-1",
		DocumentID: "doc-1",
		Document:   entity.JSONMap{"id": "doc-1", "kb_id": "kb-1"},
		Files: entity.JSONMap{
			"files": []interface{}{
				map[string]interface{}{"id": "file-1", "location": "kb-1/a.pdf"},
			},
			"mappings": []interface{}{},
		},
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("insert removal row: %v", err)
	}

	restored, err := RestoreRemovedDocumentEE(t.Context(), "kb-1", "doc-1")
	if err != nil {
		t.Fatalf("restore already restored document: %v", err)
	}
	if !restored {
		t.Fatal("a document that is already back must report a successful restore")
	}

	var rows int64
	if err := db.Model(&entity.DatasetRemovedDocument{}).
		Where("dataset_id = ? AND document_id = ?", "kb-1", "doc-1").
		Count(&rows).Error; err != nil {
		t.Fatalf("count removal rows: %v", err)
	}
	if rows != 0 {
		t.Fatalf("removal rows after a retried restore = %d, want none", rows)
	}
}

// TestRestoreRemovedDocumentWhenAlreadyBack covers the second of two restores
// racing on the same document: the first consumes the removal row and puts the
// document back, so the second finds no rows. It must report the document as
// back, not as missing, or the caller would list a document it just restored as
// unrecoverable.
func TestRestoreRemovedDocumentWhenAlreadyBack(t *testing.T) {
	db := setupRemovedDocumentTestDB(t)

	if err := db.Create(&entity.Document{
		ID:           "doc-1",
		KbID:         "kb-1",
		ParserConfig: entity.JSONMap{},
	}).Error; err != nil {
		t.Fatalf("insert live document: %v", err)
	}

	restored, err := RestoreRemovedDocumentEE(t.Context(), "kb-1", "doc-1")
	if err != nil {
		t.Fatalf("restore an already restored document: %v", err)
	}
	if !restored {
		t.Fatal("a document that is already back must report a successful restore")
	}
}

// TestRestoreRemovedDocumentAfterCleanup reports false for a document whose
// row was removed, so the caller can list it as unrecoverable instead of
// failing the whole restore.
func TestRestoreRemovedDocumentAfterCleanup(t *testing.T) {
	db := setupRemovedDocumentTestDB(t)

	row := &entity.DatasetRemovedDocument{
		ID:         "removed-1",
		DatasetID:  "kb-1",
		DocumentID: "doc-1",
		Document:   entity.JSONMap{"id": "doc-1", "kb_id": "kb-1", "name": "a.pdf"},
		Files:      entity.JSONMap{"files": []interface{}{}, "mappings": []interface{}{}},
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("insert the removal row: %v", err)
	}
	// The owner cleaned the removed document up, which drops the row.
	if err := db.Delete(&entity.DatasetRemovedDocument{}, "id = ?", "removed-1").Error; err != nil {
		t.Fatalf("clean up the removal row: %v", err)
	}

	restored, err := RestoreRemovedDocumentEE(t.Context(), "kb-1", "doc-1")
	if err != nil {
		t.Fatalf("restore cleaned document: %v", err)
	}
	if restored {
		t.Fatal("a cleaned removal must not restore a document")
	}
}
