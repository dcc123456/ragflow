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

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ragflow/internal/entity"
)

// DocumentVersionDAO persists and queries the per-document states a dataset
// version produced.
type DocumentVersionDAO struct{}

// NewDocumentVersionDAO returns a stateless DAO shared by the version writers
// and the content restore path.
func NewDocumentVersionDAO() *DocumentVersionDAO {
	return &DocumentVersionDAO{}
}

// CreateBatch appends the states of one version. A duplicate is overwritten
// rather than ignored: (document_id, dataset_version, kind) is the identity, so
// a retry of the same write carries the same state, and a second write is the
// writer correcting itself.
//
// A write never collapses into an earlier row: two edits in the same millisecond
// belong to different versions and therefore to different rows, which is the
// reason this table exists.
func (dao *DocumentVersionDAO) CreateBatch(ctx context.Context, db *gorm.DB, rows []*entity.DocumentVersion) error {
	if db == nil || len(rows) == 0 {
		return nil
	}
	return db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "document_id"}, {Name: "kind"}, {Name: "dataset_version"}},
			// meta_fields is not among the updated columns: the values live in
			// the document engine, and the row only names the snapshot.
			DoUpdates: clause.AssignmentColumns([]string{
				"dataset_id", "parser_id", "pipeline_id", "pipeline_version_id",
				"parser_config", "meta_fields_snapshot_id",
			}),
		}).
		CreateInBatches(rows, 200).Error
}

// LatestAtVersion returns the newest state of one kind at or before the version,
// or gorm.ErrRecordNotFound when the document has no row that early.
//
// The comparison is on numbers, so a row written for a later version is never
// mistaken for this one even when both landed in the same millisecond. The kind
// narrows it to the part of the document the caller is replaying: a settings row
// and a metadata row describe different things, and an empty map is a state.
func (dao *DocumentVersionDAO) LatestAtVersion(ctx context.Context, db *gorm.DB, documentID string, datasetVersion int64, kind string) (*entity.DocumentVersion, error) {
	if db == nil || documentID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var row entity.DocumentVersion
	if err := db.WithContext(ctx).
		Where("document_id = ? AND dataset_version <= ? AND kind = ?", documentID, datasetVersion, kind).
		Order("dataset_version DESC").
		Take(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// ChangedSinceVersion lists the documents of the dataset whose state of one kind
// was produced by a version newer than the given one. Those are the only
// documents a replay has to touch: a document nobody touched still carries what
// it had.
func (dao *DocumentVersionDAO) ChangedSinceVersion(ctx context.Context, db *gorm.DB, datasetID string, datasetVersion int64, kind string) ([]string, error) {
	if db == nil || datasetID == "" {
		return nil, nil
	}
	var documentIDs []string
	if err := db.WithContext(ctx).
		Model(&entity.DocumentVersion{}).
		Where("dataset_id = ? AND dataset_version > ? AND kind = ?", datasetID, datasetVersion, kind).
		Distinct().
		Order("document_id").
		Pluck("document_id", &documentIDs).Error; err != nil {
		return nil, err
	}
	return documentIDs, nil
}

// StatesAtVersion lists the documents the version itself recorded a state of one
// kind for. A replay of a metadata change needs it: the values that version
// recorded are what restoring it has to produce, and that holds whether or not
// anything about the document moved afterwards - the store it lives in can have
// been emptied in between (a removal drops it, and a restore that came before
// this read may not have put the values back).
func (dao *DocumentVersionDAO) StatesAtVersion(ctx context.Context, db *gorm.DB, datasetID string, datasetVersion int64, kind string) ([]string, error) {
	if db == nil || datasetID == "" {
		return nil, nil
	}
	var documentIDs []string
	if err := db.WithContext(ctx).
		Model(&entity.DocumentVersion{}).
		Where("dataset_id = ? AND dataset_version = ? AND kind = ?", datasetID, datasetVersion, kind).
		Distinct().
		Order("document_id").
		Pluck("document_id", &documentIDs).Error; err != nil {
		return nil, err
	}
	return documentIDs, nil
}

// DeleteByDataset drops every state row of a dataset, used when the dataset
// itself is removed.
func (dao *DocumentVersionDAO) DeleteByDataset(ctx context.Context, db *gorm.DB, datasetID string) error {
	if db == nil || datasetID == "" {
		return nil
	}
	return db.WithContext(ctx).
		Where("dataset_id = ?", datasetID).
		Delete(&entity.DocumentVersion{}).Error
}

// DeleteByDocument drops every state row of one document. It belongs to the
// purge of that document's removal row: the row carries the state the document
// is restored with, so these rows have no reader left once it is gone.
func (dao *DocumentVersionDAO) DeleteByDocument(ctx context.Context, db *gorm.DB, documentID string) error {
	if db == nil || documentID == "" {
		return nil
	}
	return db.WithContext(ctx).
		Where("document_id = ?", documentID).
		Delete(&entity.DocumentVersion{}).Error
}
