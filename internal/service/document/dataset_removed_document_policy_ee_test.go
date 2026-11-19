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

// expectsBlobRetained reports whether a document delete leaves the blob in
// storage. The enterprise edition keeps it for the content restore window.
func expectsBlobRetained() bool { return true }

// countRemovedDocuments counts the removal rows the delete wrote, so a test can
// assert the document stayed restorable.
func countRemovedDocuments(t *testing.T, db *gorm.DB, datasetID string) int64 {
	t.Helper()

	var count int64
	if err := db.Model(&entity.DatasetRemovedDocument{}).
		Where("dataset_id = ?", datasetID).
		Count(&count).Error; err != nil {
		t.Fatalf("count removed documents: %v", err)
	}
	return count
}
