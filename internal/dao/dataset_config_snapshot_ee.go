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

// ErrDatasetConfigSnapshotNotFound is returned when a (dataset id, config hash)
// pair has no row. Callers map it to "the version points at a configuration that
// no longer exists", which is an error rather than an empty snapshot: a restore
// reading an empty configuration would wipe the settings it was supposed to
// bring back.
var ErrDatasetConfigSnapshotNotFound = errors.New("dataset_config_snapshot: not found")

// DatasetConfigSnapshotDAO persists and queries the dataset configuration
// snapshots.
type DatasetConfigSnapshotDAO struct{}

// NewDatasetConfigSnapshotDAO returns a stateless DAO shared by the version
// writers and the history readers.
func NewDatasetConfigSnapshotDAO() *DatasetConfigSnapshotDAO {
	return &DatasetConfigSnapshotDAO{}
}

// Upsert stores one configuration snapshot, unless the same content is already
// there.
//
// The hash is the row's identity, so an insert that collides means the snapshot
// already exists and there is nothing to write: the caller's version simply points
// at the existing row. That is what makes an unchanged configuration cost one row
// no matter how many versions describe it, and what keeps a retried write
// idempotent.
func (dao *DatasetConfigSnapshotDAO) Upsert(ctx context.Context, tx *gorm.DB, datasetID, configHash string, snapshot entity.JSONMap) error {
	if tx == nil || datasetID == "" || configHash == "" || snapshot == nil {
		return nil
	}
	row := &entity.DatasetConfigSnapshot{
		DatasetID:  datasetID,
		ConfigHash: configHash,
		Config:     snapshot,
	}
	return tx.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "dataset_id"}, {Name: "config_hash"}},
			DoNothing: true,
		}).
		Create(row).Error
}

// Get returns the snapshot a version's hash names.
func (dao *DatasetConfigSnapshotDAO) Get(ctx context.Context, db *gorm.DB, datasetID, configHash string) (entity.JSONMap, error) {
	if db == nil || datasetID == "" || configHash == "" {
		return nil, ErrDatasetConfigSnapshotNotFound
	}
	var row entity.DatasetConfigSnapshot
	err := db.WithContext(ctx).
		Select("config").
		Where("dataset_id = ? AND config_hash = ?", datasetID, configHash).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDatasetConfigSnapshotNotFound
		}
		return nil, err
	}
	return row.Config, nil
}

// DeleteUnreferenced drops the snapshots no surviving version of the dataset
// points at, and reports how many rows went.
//
// It is the half of the dataset sweep that covers configuration snapshots and is
// deliberately not exported: reclamation is one dataset-level decision, and the
// only entry to it is SweepDatasetSnapshots. Two public cleanups would let a
// caller collect one table and forget the other - the state that leaves garbage
// alive without anything failing.
//
// The check is a single set difference against the version table, so a snapshot
// that later versions still read is never touched - that is the whole point of
// keeping the configuration out of the version row. A snapshot written by a
// concurrent restore is invisible to both halves of the statement until that
// transaction commits, and its version row commits with it, so the pair is never
// separated.
func (dao *DatasetConfigSnapshotDAO) deleteUnreferenced(ctx context.Context, db *gorm.DB, datasetID string) (int64, error) {
	if db == nil || datasetID == "" {
		return 0, nil
	}
	// NOT EXISTS rather than NOT IN: a single NULL in the referenced set would
	// make NOT IN match nothing and delete every snapshot of the dataset, so the
	// correlated form is the one that cannot be broken by a row whose hash a
	// later migration or a hand edit left empty.
	result := db.WithContext(ctx).
		Where("dataset_id = ? AND NOT EXISTS (?)",
			datasetID,
			db.WithContext(ctx).
				Model(&entity.DatasetVersion{}).
				Select("1").
				Where("dataset_version.dataset_id = dataset_config_snapshot.dataset_id AND dataset_version.config_hash = dataset_config_snapshot.config_hash"),
		).
		Delete(&entity.DatasetConfigSnapshot{})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// DeleteByDataset drops every snapshot of a dataset, used when the dataset
// itself is deleted.
func (dao *DatasetConfigSnapshotDAO) DeleteByDataset(ctx context.Context, db *gorm.DB, datasetID string) error {
	if db == nil || datasetID == "" {
		return nil
	}
	return db.WithContext(ctx).
		Where("dataset_id = ?", datasetID).
		Delete(&entity.DatasetConfigSnapshot{}).Error
}
