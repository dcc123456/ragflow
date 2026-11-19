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

	"ragflow/internal/dao"
	"ragflow/internal/entity"
)

// deleteVersionCount counts the versions of one kind a dataset accumulated.
func deleteVersionCount(t *testing.T, datasetID, kind string) int64 {
	t.Helper()

	var count int64
	if err := dao.DB.Model(&entity.DatasetVersion{}).
		Where("dataset_id = ? AND kind = ?", datasetID, kind).
		Count(&count).Error; err != nil {
		t.Fatalf("count %s versions: %v", kind, err)
	}
	return count
}

// TestDeleteDocumentsRecordsOneDeleteVersion is the behaviour a user's delete
// gets: one entry in the history naming the documents that went.
func TestDeleteDocumentsRecordsOneDeleteVersion(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	migrateDatasetVersionTableEE(t, db)
	insertUserTenantForAccessCheck(t, "user-1", "tenant-1")
	insertTestKB(t, "kb-1", "tenant-1", 2, 70, 30)
	insertTestDoc(t, "doc-1", "kb-1", 30, 10)
	insertTestDoc(t, "doc-2", "kb-1", 40, 20)

	svc := testDocumentService(t)
	ctx := documentTestContext(t.Context())

	deleted, err := svc.DeleteDocuments(ctx, []string{"doc-1", "doc-2"}, false, "kb-1")
	if err != nil {
		t.Fatalf("DeleteDocuments error = %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted = %d, want 2", deleted)
	}
	if got := deleteVersionCount(t, "kb-1", entity.DatasetVersionKindDelete); got != 1 {
		t.Fatalf("delete versions = %d, want 1", got)
	}
}

// TestDeleteDocumentsForRestoreRecordsNoVersion covers the same removal when a
// restore drives it: the restore writes one version of its own describing where
// the dataset ended up, so a delete entry here would show the user a deletion
// they never asked for, of a state that only exists mid-restore.
func TestDeleteDocumentsForRestoreRecordsNoVersion(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	migrateDatasetVersionTableEE(t, db)
	insertUserTenantForAccessCheck(t, "user-1", "tenant-1")
	insertTestKB(t, "kb-1", "tenant-1", 2, 70, 30)
	insertTestDoc(t, "doc-1", "kb-1", 30, 10)
	insertTestDoc(t, "doc-2", "kb-1", 40, 20)

	svc := testDocumentService(t)
	ctx := documentTestContext(t.Context())

	deleted, err := svc.DeleteDocumentsForRestore(ctx, []string{"doc-2"}, "kb-1")
	if err != nil {
		t.Fatalf("DeleteDocumentsForRestore error = %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	for _, kind := range []string{
		entity.DatasetVersionKindDelete,
		entity.DatasetVersionKindUpload,
	} {
		if got := deleteVersionCount(t, "kb-1", kind); got != 0 {
			t.Fatalf("%s versions = %d, want none: the restore records the transition itself", kind, got)
		}
	}

	// The row is gone all the same: the version is the only thing suppressed.
	if _, err := dao.NewDocumentDAO().GetByID(ctx, db, "doc-2"); err == nil {
		t.Fatal("doc-2 should have been removed")
	}
}
