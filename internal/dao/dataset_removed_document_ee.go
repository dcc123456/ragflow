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

// ErrRemovedDocumentNotFound is returned when a document has no removal row, i.e.
// it cannot be put back by a restore.
var ErrRemovedDocumentNotFound = errors.New("dataset_removed_document: not found")

// DatasetRemovedDocumentDAO persists the rows that keep a removed document
// restorable until the dataset's owner removes them.
type DatasetRemovedDocumentDAO struct{}

// NewDatasetRemovedDocumentDAO returns a stateless DAO shared by the delete path
// (which writes), the restore path (which reads) and the manual cleanup.
func NewDatasetRemovedDocumentDAO() *DatasetRemovedDocumentDAO {
	return &DatasetRemovedDocumentDAO{}
}

// Create inserts one removal row using the caller's transaction.
func (dao *DatasetRemovedDocumentDAO) Create(ctx context.Context, tx *gorm.DB, row *entity.DatasetRemovedDocument) error {
	return tx.WithContext(ctx).Create(row).Error
}

// GetRestorable returns the newest removal row of a document. A document removed
// twice keeps the row of the last removal, which is the state a restore should
// put back.
//
// It locks the row for the caller's transaction, and must therefore be called
// with the transaction that puts the document back. The cleanup locks the same
// rows: without that, a restore could read a removal row the cleanup is about to
// delete and rebuild the document against blobs that are being removed.
func (dao *DatasetRemovedDocumentDAO) GetRestorable(ctx context.Context, db *gorm.DB, datasetID, documentID string) (*entity.DatasetRemovedDocument, error) {
	var row entity.DatasetRemovedDocument
	query := db.WithContext(ctx).
		Where("dataset_id = ? AND document_id = ?", datasetID, documentID).
		Order("create_time DESC")
	if db.Dialector.Name() != "sqlite" {
		// SQLite has no row locks and rejects the clause; its tests run the
		// same code path without one.
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRemovedDocumentNotFound
		}
		return nil, err
	}
	return &row, nil
}

// ListByDocumentForUpdate locks every removal row of one document for the
// caller's transaction. A document removed more than once keeps one row per
// removal, and the cleanup drops them together, so it has to hold all of them.
//
// The lock is what keeps the cleanup away from a document a restore is putting
// back at the same moment: the restore locks its row first, and a cleanup that
// runs after it sees no rows to drop.
func (dao *DatasetRemovedDocumentDAO) ListByDocumentForUpdate(ctx context.Context, db *gorm.DB, datasetID, documentID string) ([]*entity.DatasetRemovedDocument, error) {
	if datasetID == "" || documentID == "" {
		return nil, nil
	}
	query := db.WithContext(ctx).
		Where("dataset_id = ? AND document_id = ?", datasetID, documentID).
		Order("create_time DESC")
	if db.Dialector.Name() != "sqlite" {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var rows []*entity.DatasetRemovedDocument
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListByDataset returns a dataset's removal rows, newest first. The manual
// cleanup lists them so the owner can see what a restore can still bring back
// and what storage it costs.
func (dao *DatasetRemovedDocumentDAO) ListByDataset(ctx context.Context, db *gorm.DB, datasetID string) ([]*entity.DatasetRemovedDocument, error) {
	if datasetID == "" {
		return nil, nil
	}
	var rows []*entity.DatasetRemovedDocument
	if err := db.WithContext(ctx).
		Where("dataset_id = ?", datasetID).
		Order("create_time DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// DeleteByIDs drops the given removal rows.
func (dao *DatasetRemovedDocumentDAO) DeleteByIDs(ctx context.Context, db *gorm.DB, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return db.WithContext(ctx).
		Where("id IN ?", ids).
		Delete(&entity.DatasetRemovedDocument{}).Error
}

// DeleteByDocument drops every removal row of one document, so a cleanup leaves
// nothing behind for a document that was removed more than once.
func (dao *DatasetRemovedDocumentDAO) DeleteByDocument(ctx context.Context, db *gorm.DB, datasetID, documentID string) error {
	if datasetID == "" || documentID == "" {
		return nil
	}
	return db.WithContext(ctx).
		Where("dataset_id = ? AND document_id = ?", datasetID, documentID).
		Delete(&entity.DatasetRemovedDocument{}).Error
}

// DeleteByDataset drops a dataset's removal rows, used when the dataset itself
// is deleted.
func (dao *DatasetRemovedDocumentDAO) DeleteByDataset(ctx context.Context, db *gorm.DB, datasetID string) error {
	if datasetID == "" {
		return nil
	}
	return db.WithContext(ctx).
		Where("dataset_id = ?", datasetID).
		Delete(&entity.DatasetRemovedDocument{}).Error
}
