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

	"gorm.io/gorm"

	"ragflow/internal/entity"
)

// sweepTestHash renders a 32-character stand-in for a content hash; the sweep
// compares these strings, so their value never matters, only who names them. The
// width is the one a stored hash has, so a row here cannot outgrow its column.
func sweepTestHash(seed string) string {
	hash := ""
	for len(hash) < 32 {
		hash += seed
	}
	return hash[:32]
}

// insertSweepSnapshotRows stores one configuration snapshot and one document set
// under the same hash, as the version writers would.
func insertSweepSnapshotRows(t *testing.T, db *gorm.DB, datasetID, hash string) {
	t.Helper()

	if err := db.Create(&entity.DatasetConfigSnapshot{
		DatasetID:  datasetID,
		ConfigHash: hash,
		Config:     entity.JSONMap{"name": hash[:8]},
	}).Error; err != nil {
		t.Fatalf("insert a configuration snapshot: %v", err)
	}
	if err := db.Create(&entity.DatasetDocumentSetSnapshot{
		DatasetID:     datasetID,
		DocumentsHash: hash,
		Documents:     `["doc-1"]`,
	}).Error; err != nil {
		t.Fatalf("insert a document set: %v", err)
	}
}

// insertSweepVersionRow stores a version naming the given hashes; an empty string
// leaves the columns NULL, which is a version that recorded no snapshot.
func insertSweepVersionRow(t *testing.T, db *gorm.DB, datasetID string, version int64, hash string) {
	t.Helper()

	row := &entity.DatasetVersion{
		DatasetID: datasetID,
		Version:   version,
		Kind:      entity.DatasetVersionKindChunk,
	}
	if hash != "" {
		config, documents := hash, hash
		row.ConfigHash = &config
		row.DocumentsHash = &documents
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("insert version %d: %v", version, err)
	}
}

func sweepRemainingConfigHashes(t *testing.T, db *gorm.DB, datasetID string) map[string]bool {
	t.Helper()

	var hashes []string
	if err := db.Model(&entity.DatasetConfigSnapshot{}).
		Where("dataset_id = ?", datasetID).
		Pluck("config_hash", &hashes).Error; err != nil {
		t.Fatalf("list configuration snapshots: %v", err)
	}
	remaining := make(map[string]bool, len(hashes))
	for _, hash := range hashes {
		remaining[hash] = true
	}
	return remaining
}

func sweepRemainingDocumentHashes(t *testing.T, db *gorm.DB, datasetID string) map[string]bool {
	t.Helper()

	var hashes []string
	if err := db.Model(&entity.DatasetDocumentSetSnapshot{}).
		Where("dataset_id = ?", datasetID).
		Pluck("documents_hash", &hashes).Error; err != nil {
		t.Fatalf("list document sets: %v", err)
	}
	remaining := make(map[string]bool, len(hashes))
	for _, hash := range hashes {
		remaining[hash] = true
	}
	return remaining
}

// TestSweepDatasetSnapshotsReclaimsOnlyUnreferencedRows pins the grain of the
// sweep: it answers "which snapshots does this dataset still name" against the
// surviving versions, so one call collects the row the delete just orphaned
// together with anything an earlier, failed sweep left behind - and leaves every
// snapshot a surviving version still reads.
func TestSweepDatasetSnapshotsReclaimsOnlyUnreferencedRows(t *testing.T) {
	db := openVersionHistoryTestDB(t)

	shared, sole, stale := sweepTestHash("a1"), sweepTestHash("b2"), sweepTestHash("c3")
	for _, hash := range []string{shared, sole, stale} {
		insertSweepSnapshotRows(t, db, "kb-1", hash)
	}
	// Two versions share one snapshot; a third names the one about to go away.
	insertSweepVersionRow(t, db, "kb-1", 1, shared)
	insertSweepVersionRow(t, db, "kb-1", 2, shared)
	insertSweepVersionRow(t, db, "kb-1", 3, sole)

	// The delete the sweep follows: the last version naming "sole" goes away.
	if err := db.Where("dataset_id = ? AND version = ?", "kb-1", 3).
		Delete(&entity.DatasetVersion{}).Error; err != nil {
		t.Fatalf("delete version 3: %v", err)
	}

	reclaimed, err := SweepDatasetSnapshots(t.Context(), db, "kb-1")
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if reclaimed.Configs != 2 || reclaimed.DocumentSets != 2 || reclaimed.Total() != 4 {
		t.Fatalf("reclaimed = %+v, want 2 configurations, 2 document sets and a total of 4", reclaimed)
	}
	if got := sweepRemainingConfigHashes(t, db, "kb-1"); len(got) != 1 || !got[shared] {
		t.Fatalf("configuration snapshots left = %v, want only the shared one", got)
	}
	if got := sweepRemainingDocumentHashes(t, db, "kb-1"); len(got) != 1 || !got[shared] {
		t.Fatalf("document sets left = %v, want only the shared one", got)
	}

	// A second sweep finds nothing: reclamation is a state, not a work queue, so
	// calling it after every delete cannot compound.
	again, err := SweepDatasetSnapshots(t.Context(), db, "kb-1")
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if again.Total() != 0 {
		t.Fatalf("second sweep reclaimed %+v, want nothing", again)
	}
}

// TestSweepDatasetSnapshotsSurvivesVersionsWithoutSnapshots pins the failure mode
// the correlated delete exists for: versions that recorded no snapshot carry NULL
// hashes, and a set-membership test would have seen a NULL and deleted every row.
func TestSweepDatasetSnapshotsSurvivesVersionsWithoutSnapshots(t *testing.T) {
	db := openVersionHistoryTestDB(t)

	kept := sweepTestHash("a1")
	insertSweepSnapshotRows(t, db, "kb-1", kept)
	insertSweepVersionRow(t, db, "kb-1", 1, kept)
	// A later version recorded no snapshot at all.
	insertSweepVersionRow(t, db, "kb-1", 2, "")

	reclaimed, err := SweepDatasetSnapshots(t.Context(), db, "kb-1")
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if reclaimed.Total() != 0 {
		t.Fatalf("reclaimed = %+v, want nothing", reclaimed)
	}
	if got := sweepRemainingConfigHashes(t, db, "kb-1"); len(got) != 1 || !got[kept] {
		t.Fatalf("configuration snapshots left = %v, want the referenced one", got)
	}
}

// TestSweepDatasetSnapshotsLeavesOtherDatasetsAlone pins the scope: a sweep only
// ever answers for the dataset it was asked about, so a dataset whose own versions
// are intact keeps its snapshots even while a neighbour is collected.
func TestSweepDatasetSnapshotsLeavesOtherDatasetsAlone(t *testing.T) {
	db := openVersionHistoryTestDB(t)

	orphan := sweepTestHash("d4")
	insertSweepSnapshotRows(t, db, "kb-1", orphan)
	neighbour := sweepTestHash("e5")
	insertSweepSnapshotRows(t, db, "kb-2", neighbour)
	insertSweepVersionRow(t, db, "kb-2", 1, neighbour)

	reclaimed, err := SweepDatasetSnapshots(t.Context(), db, "kb-1")
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if reclaimed.Total() != 2 {
		t.Fatalf("reclaimed = %+v, want the orphaned pair", reclaimed)
	}
	if got := sweepRemainingConfigHashes(t, db, "kb-2"); len(got) != 1 || !got[neighbour] {
		t.Fatalf("neighbour snapshots = %v, want its own untouched", got)
	}
	if got := sweepRemainingDocumentHashes(t, db, "kb-2"); len(got) != 1 || !got[neighbour] {
		t.Fatalf("neighbour document sets = %v, want its own untouched", got)
	}
}
