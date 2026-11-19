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
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ragflow/internal/entity"
)

// ErrDatasetDocumentSetSnapshotNotFound is returned when a (dataset id, documents hash)
// pair has no row. Callers report it as "the version points at a document set
// that no longer exists", which is an error rather than an empty set: a restore
// reading an empty set would delete every document the dataset has, while
// "not recorded" makes it refuse the version instead.
var ErrDatasetDocumentSetSnapshotNotFound = errors.New("dataset_document_set_snapshot: not found")

// DatasetDocumentSetSnapshotDAO persists and queries the document sets a version
// describes.
type DatasetDocumentSetSnapshotDAO struct{}

// NewDatasetDocumentSetSnapshotDAO returns a stateless DAO shared by the version writers
// and the history readers.
func NewDatasetDocumentSetSnapshotDAO() *DatasetDocumentSetSnapshotDAO {
	return &DatasetDocumentSetSnapshotDAO{}
}

// Upsert stores one document set, unless the same content is already there.
//
// The hash is the row's identity, so an insert that collides means the set
// already exists and there is nothing to write: the caller's version simply
// points at the existing row. That is what keeps a retried write idempotent, and
// what makes a restore that returns to a state the history already holds cost one
// pointer instead of a second copy.
func (dao *DatasetDocumentSetSnapshotDAO) Upsert(ctx context.Context, tx *gorm.DB, datasetID, documentsHash string, documents string) error {
	if tx == nil || datasetID == "" || documentsHash == "" || documents == "" {
		return nil
	}
	row := &entity.DatasetDocumentSetSnapshot{
		DatasetID:     datasetID,
		DocumentsHash: documentsHash,
		Documents:     documents,
	}
	return tx.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "dataset_id"}, {Name: "documents_hash"}},
			DoNothing: true,
		}).
		Create(row).Error
}

// Get returns the stored text a version's documents hash names.
func (dao *DatasetDocumentSetSnapshotDAO) Get(ctx context.Context, db *gorm.DB, datasetID, documentsHash string) (string, error) {
	if db == nil || datasetID == "" || documentsHash == "" {
		return "", ErrDatasetDocumentSetSnapshotNotFound
	}
	var row entity.DatasetDocumentSetSnapshot
	err := db.WithContext(ctx).
		Select("documents").
		Where("dataset_id = ? AND documents_hash = ?", datasetID, documentsHash).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrDatasetDocumentSetSnapshotNotFound
		}
		return "", err
	}
	return row.Documents, nil
}

// GetMany resolves several hashes of one dataset in a single query, which is what
// the history listing needs: it resolves a version's set for display, and the
// versions that inherited a set all name the same hash.
//
// A hash with no row is simply absent from the result; the caller decides whether
// that is a dangling pointer.
func (dao *DatasetDocumentSetSnapshotDAO) GetMany(ctx context.Context, db *gorm.DB, datasetID string, documentsHashes []string) (map[string]string, error) {
	resolved := make(map[string]string, len(documentsHashes))
	if db == nil || datasetID == "" || len(documentsHashes) == 0 {
		return resolved, nil
	}
	var rows []*entity.DatasetDocumentSetSnapshot
	if err := db.WithContext(ctx).
		Select("documents_hash", "documents").
		Where("dataset_id = ? AND documents_hash IN ?", datasetID, documentsHashes).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		resolved[row.DocumentsHash] = row.Documents
	}
	return resolved, nil
}

// deleteUnreferenced drops the sets no surviving version of the dataset points
// at, and reports how many rows went.
//
// It is the half of the dataset sweep that covers document sets and is
// deliberately not exported: reclamation is one dataset-level decision, and the
// only entry to it is SweepDatasetSnapshots. Two public cleanups would let a
// caller collect one table and forget the other - the state that leaves garbage
// alive without anything failing.
//
// It replaces the old adopt-before-delete step, where the delete had to copy the
// set onto a survivor because the bytes lived on the row being removed. The check
// is a single set difference against the version table, so a set a later version
// still reads is never touched. A set written by a concurrent restore is
// invisible to both halves of the statement until that transaction commits, and
// its version row commits with it, so the pair is never separated.
func (dao *DatasetDocumentSetSnapshotDAO) deleteUnreferenced(ctx context.Context, db *gorm.DB, datasetID string) (int64, error) {
	if db == nil || datasetID == "" {
		return 0, nil
	}
	// NOT EXISTS rather than NOT IN: a single NULL in the referenced set would
	// make NOT IN match nothing and delete every set of the dataset, so the
	// correlated form is the one that cannot be broken by a row whose hash a
	// later migration or a hand edit left empty.
	result := db.WithContext(ctx).
		Where("dataset_id = ? AND NOT EXISTS (?)",
			datasetID,
			db.WithContext(ctx).
				Model(&entity.DatasetVersion{}).
				Select("1").
				Where("dataset_version.dataset_id = dataset_document_set_snapshot.dataset_id AND dataset_version.documents_hash = dataset_document_set_snapshot.documents_hash"),
		).
		Delete(&entity.DatasetDocumentSetSnapshot{})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// DeleteByDataset drops a dataset's sets, used when the dataset itself is deleted.
// They are scoped to the dataset, and nothing outside it can point at them.
func (dao *DatasetDocumentSetSnapshotDAO) DeleteByDataset(ctx context.Context, db *gorm.DB, datasetID string) error {
	if db == nil || datasetID == "" {
		return nil
	}
	return db.WithContext(ctx).
		Where("dataset_id = ?", datasetID).
		Delete(&entity.DatasetDocumentSetSnapshot{}).Error
}
