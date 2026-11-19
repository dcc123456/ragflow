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

	"ragflow/internal/engine/history"
	"ragflow/internal/engine/history/historytest"
	"ragflow/internal/entity"
)

// migrateDatasetVersionTableEE adds the history table the dataset service and
// the content paths write to. Only the enterprise edition records versions, so
// the table type itself only exists there.
func migrateDatasetVersionTableEE(t *testing.T, db *gorm.DB) {
	t.Helper()

	// Every table a version write touches has to exist, not only the version row
	// itself: appending a version stores the configuration snapshot it ran under
	// (dataset_config_snapshot) and, for the kinds that move them, the
	// per-document parse settings and metadata values the restore reads back. A
	// missing one of those
	// fails the append, and the tests would be measuring the fixture instead of
	// the behaviour. A recorded state also reads the dataset's data-source
	// bindings and, when a restore drops one, cancels its sync tasks, so both
	// tables are part of the history's storage.
	if err := db.AutoMigrate(
		&entity.DatasetVersion{},
		&entity.DatasetConfigSnapshot{},
		&entity.DatasetDocumentSetSnapshot{},
		&entity.DatasetRemovedDocument{},
		&entity.DocumentVersion{},
		&entity.Connector2Kb{},
		&entity.SyncLogs{},
	); err != nil {
		t.Fatalf("failed to migrate dataset_version: %v", err)
	}
}

// installMetadataHistoryStoreEE stands an in-memory document engine in for the
// real one. A metadata state row names the state that holds its values instead
// of carrying them, so every path that writes one needs a store - and the tests
// get one by default rather than each remembering to install it.
func installMetadataHistoryStoreEE(t *testing.T) {
	t.Helper()
	history.SetMetadataStore(historytest.NewMetadataStore())
	t.Cleanup(func() { history.SetMetadataStore(nil) })
}
