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

package dao

import (
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/entity"
)

// TestDocumentListRestoredEmptyParseShowsCompleted: with the parse marker an
// empty restore reads as completed; without it, as unparsed.
func TestDocumentListRestoredEmptyParseShowsCompleted(t *testing.T) {
	db := setupDocumentTestDB(t)
	if err := db.AutoMigrate(
		&entity.User{},
		&entity.UserCanvas{},
		&entity.File{},
		&entity.File2Document{},
		&entity.IngestionTask{},
	); err != nil {
		t.Fatalf("migrate document-list dependencies: %v", err)
	}
	if err := db.Exec(`CREATE TABLE chunk_version (
		chunk_id TEXT NOT NULL, version INTEGER NOT NULL, doc_id TEXT NOT NULL,
		dataset_id TEXT NOT NULL, dataset_version INTEGER NOT NULL,
		created_by TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL DEFAULT '',
		create_time INTEGER NOT NULL DEFAULT 0, PRIMARY KEY (chunk_id, version)
	)`).Error; err != nil {
		t.Fatalf("create chunk_version: %v", err)
	}

	restored := int64(1)
	for _, suffix := range []string{"with-marker", "without-marker"} {
		documentID := "doc-" + suffix
		if err := db.Create(&entity.Document{
			ID: documentID, KbID: "kb-1", ParserID: "naive", ParserConfig: entity.JSONMap{},
			SourceType: "local", Type: "document", CreatedBy: "user-1",
			Name: sp(suffix), Suffix: "pdf", RestoredVersion: &restored,
		}).Error; err != nil {
			t.Fatalf("create document %s: %v", documentID, err)
		}
		if err := db.Create(&entity.File{
			ID: "file-" + suffix, ParentID: "parent-1", TenantID: "tenant-1",
			CreatedBy: "user-1", Name: suffix, Type: "document",
		}).Error; err != nil {
			t.Fatalf("create file for %s: %v", documentID, err)
		}
		if err := db.Create(&entity.File2Document{
			ID: "link-" + suffix, FileID: sp("file-" + suffix), DocumentID: sp(documentID),
		}).Error; err != nil {
			t.Fatalf("link file to %s: %v", documentID, err)
		}
		if err := db.Create(&entity.IngestionTask{
			ID: "task-" + suffix, UserID: "user-1", DocumentID: documentID,
			DatasetID: "kb-1", Status: common.COMPLETED,
		}).Error; err != nil {
			t.Fatalf("create task for %s: %v", documentID, err)
		}
	}
	if err := db.Exec(`INSERT INTO chunk_version
		(chunk_id, version, doc_id, dataset_id, dataset_version, kind, create_time)
		VALUES (?, 0, ?, 'kb-1', 1, 'parse', 0)`,
		entity.ChunkVersionEmptyDocumentID, "doc-with-marker").Error; err != nil {
		t.Fatalf("seed the parse marker: %v", err)
	}

	documents, _, err := NewDocumentDAO().ListByKBIDWithOptions(t.Context(), db,
		DocumentListOptions{KbID: "kb-1", Limit: 10})
	if err != nil {
		t.Fatalf("list documents: %v", err)
	}
	statuses := make(map[string]string, len(documents))
	for _, document := range documents {
		if document.IngestionStatus != nil {
			statuses[document.ID] = *document.IngestionStatus
		}
	}
	if got := statuses["doc-with-marker"]; got != common.COMPLETED {
		t.Fatalf("restored empty parse status = %q, want %q", got, common.COMPLETED)
	}
	if got := statuses["doc-without-marker"]; got != common.UNSTART {
		t.Fatalf("restored unparsed status = %q, want %q", got, common.UNSTART)
	}
}
