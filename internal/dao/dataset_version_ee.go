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
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ragflow/internal/documentset"
	"ragflow/internal/entity"
)

// ErrDatasetVersionNotFound is returned when a (dataset id, version) pair has
// no row. Callers map it to "version not found" instead of leaking
// gorm.ErrRecordNotFound.
var ErrDatasetVersionNotFound = errors.New("dataset_version: not found")

// ErrDatasetVersionExists is returned when the row about to be written is already
// there. Two writers that allocate the same number (a dataset whose history is
// out of step with its version column, a backup restored in the middle of a
// transaction) then get an answer that names the condition instead of a driver's
// primary-key text, and the caller decides: a baseline seed treats it as "someone
// got here first", anything else answers 409.
var ErrDatasetVersionExists = errors.New("dataset_version: version already exists")

// DatasetVersionDAO persists and queries dataset_version rows.
type DatasetVersionDAO struct{}

// NewDatasetVersionDAO returns a stateless DAO shared by the dataset service
// and the listing endpoints.
func NewDatasetVersionDAO() *DatasetVersionDAO {
	return &DatasetVersionDAO{}
}

// datasetVersionMetadataColumns are the columns a version list needs. The
// snapshots are deliberately excluded: they can be large and the list only shows
// version, kind, author and time. The two content hashes are not: they are cheap,
// and a row that carries one is what the next write inherits from (and what tells
// the page which versions share a state).
var datasetVersionMetadataColumns = []string{
	"dataset_id", "version", "kind", "restored_from",
	"created_by", "create_time", "documents_hash", "config_hash",
}

// Create inserts one version row using the caller's transaction. A primary-key
// collision on (dataset_id, version) is reported as ErrDatasetVersionExists, so a
// lost allocation race does not surface as a 500 carrying the driver's text.
func (dao *DatasetVersionDAO) Create(ctx context.Context, tx *gorm.DB, row *entity.DatasetVersion) error {
	if err := tx.WithContext(ctx).Create(row).Error; err != nil {
		if IsDuplicateKeyErr(err) {
			// Both links are kept: the named condition for the caller that decides
			// what to do, and the driver error for the readers that still classify
			// by errors.Is(err, gorm.ErrDuplicatedKey).
			return fmt.Errorf("%w: dataset %s version %d: %w",
				ErrDatasetVersionExists, row.DatasetID, row.Version, err)
		}
		return err
	}
	return nil
}

// GetLatest returns the highest version recorded for a dataset, without the
// configuration snapshot: the writer only needs the number.
func (dao *DatasetVersionDAO) GetLatest(ctx context.Context, db *gorm.DB, datasetID string) (*entity.DatasetVersion, error) {
	var row entity.DatasetVersion
	err := db.WithContext(ctx).
		Select("dataset_id", "version", "kind").
		Where("dataset_id = ?", datasetID).
		Order("version DESC").
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDatasetVersionNotFound
		}
		return nil, err
	}
	return &row, nil
}

// GetLatestForUpdate returns the highest version and locks its row until the
// caller's transaction ends, which serializes concurrent version creation on
// the same dataset. The dataset row itself is locked as well by the callers
// that mutate it; this lock exists so two writers cannot allocate one number.
//
// Both content hashes are read as well: the configuration writer compares the
// snapshot an update produced with the one the head carries and skips a version
// that would repeat the current one, and the content writer inherits the head's
// hashes. Neither snapshot is resolved - pulling the document set here would put
// the megabytes these pointers exist to avoid back on the write path.
func (dao *DatasetVersionDAO) GetLatestForUpdate(ctx context.Context, tx *gorm.DB, datasetID string) (*entity.DatasetVersion, error) {
	var row entity.DatasetVersion
	err := tx.WithContext(ctx).
		Select("dataset_id", "version", "kind", "config_hash", "documents_hash").
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("dataset_id = ?", datasetID).
		Order("version DESC").
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDatasetVersionNotFound
		}
		return nil, err
	}
	// The snapshot itself is not read: every writer compares hashes, and nobody
	// on the write path needs the bytes.
	return &row, nil
}

// GetByDatasetAndVersion loads one version with the configuration that applied
// to it, read from the snapshot its hash names, and the document set it
// describes, stored on the row or on the version that established it.
func (dao *DatasetVersionDAO) GetByDatasetAndVersion(ctx context.Context, db *gorm.DB, datasetID string, version int64) (*entity.DatasetVersion, error) {
	var row entity.DatasetVersion
	err := db.WithContext(ctx).
		Where("dataset_id = ? AND version = ?", datasetID, version).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDatasetVersionNotFound
		}
		return nil, err
	}
	if err := dao.resolveConfig(ctx, db, &row); err != nil {
		return nil, err
	}
	if err := dao.resolveDocuments(ctx, db, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// DatasetVersionFact is what a chunk history reader needs from the dataset version
// a row was written under: what produced it, and the user it is attributed to.
type DatasetVersionFact struct {
	// Kind is the version's own kind. It says whether a re-parse or a restore
	// dropped a chunk, which the chunk row cannot: both removals name no actor.
	Kind string
	// CreatedBy is the user the version is attributed to - the only record of
	// whose request produced a chunk row the system wrote.
	CreatedBy string
}

// FactsByVersion reads the kind and the attributed user of each named version. It
// selects three columns and hydrates nothing: resolving a version would pull a
// configuration snapshot and a document set that a history reader never looks at.
func (dao *DatasetVersionDAO) FactsByVersion(ctx context.Context, db *gorm.DB, datasetID string, versions []int64) (map[int64]DatasetVersionFact, error) {
	if db == nil || datasetID == "" || len(versions) == 0 {
		return nil, nil
	}
	var rows []struct {
		Version   int64
		Kind      string
		CreatedBy string
	}
	if err := db.WithContext(ctx).
		Model(&entity.DatasetVersion{}).
		Select("version", "kind", "created_by").
		Where("dataset_id = ? AND version IN ?", datasetID, versions).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	facts := make(map[int64]DatasetVersionFact, len(rows))
	for _, row := range rows {
		facts[row.Version] = DatasetVersionFact{Kind: row.Kind, CreatedBy: row.CreatedBy}
	}
	return facts, nil
}

// GetPrevious loads the newest version older than the given one, with the
// configuration that applied to it.
//
// It answers "what did this version change?" without assuming the number before
// it still exists: history entries can be deleted, and the diff belongs to the
// state the dataset actually moved from.
func (dao *DatasetVersionDAO) GetPrevious(ctx context.Context, db *gorm.DB, datasetID string, version int64) (*entity.DatasetVersion, error) {
	var row entity.DatasetVersion
	err := db.WithContext(ctx).
		Where("dataset_id = ? AND version < ?", datasetID, version).
		Order("version DESC").
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDatasetVersionNotFound
		}
		return nil, err
	}
	if err := dao.resolveConfig(ctx, db, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// resolveConfig fills in a version's configuration from the snapshot its hash
// names.
//
// The snapshot is read with one primary-key lookup rather than a scan back
// through the history: the distance to the last configuration change is
// unbounded, so a scan would be slowest exactly when a dataset never moved its
// settings - the case the pointer exists for.
//
// A dangling hash is an error rather than an empty configuration: a restore
// reading an empty snapshot would wipe the settings it was supposed to bring
// back. The hash is truncated in the message because it is a content id, of which
// the first bytes are enough to find the row in a log.
func (dao *DatasetVersionDAO) resolveConfig(ctx context.Context, db *gorm.DB, row *entity.DatasetVersion) error {
	if row == nil || row.Config != nil || row.ConfigHash == nil {
		return nil
	}
	config, err := NewDatasetConfigSnapshotDAO().Get(ctx, db, row.DatasetID, *row.ConfigHash)
	if err != nil {
		if errors.Is(err, ErrDatasetConfigSnapshotNotFound) {
			return fmt.Errorf("dataset version %d points at configuration %s, which no longer exists", row.Version, shortConfigHash(*row.ConfigHash))
		}
		return err
	}
	row.Config = config
	return nil
}

// shortConfigHash trims a content hash to the prefix that identifies it in a log.
func shortConfigHash(configHash string) string {
	const keep = 8
	if len(configHash) <= keep {
		return configHash
	}
	return configHash[:keep]
}

// resolveDocuments fills in a version's document set from the row its hash names.
//
// The set is read with one primary-key lookup for the same reason the
// configuration snapshot is: the distance to the version that last moved it is
// unbounded - it is exactly the number of parses and chunk edits in between, which
// is the case the pointer exists for - so a scan back through the history would be
// slowest exactly when a dataset never moved its documents.
//
// A dangling hash is an error rather than an empty set: the two are not the same
// question. "Not recorded" refuses a restore, while an empty set would ask it to
// delete every document the dataset has. That is why the field is a string whose
// empty value means "not resolved": an empty set is the text "[]", not "".
//
// No version row is ever rewritten to make this work. The set is stored by content
// in dataset_document_set_snapshot, so deleting the version that wrote it leaves every other
// pointer valid - the reason the old "adopt the blob onto a survivor before
// deleting" step could be removed.
func (dao *DatasetVersionDAO) resolveDocuments(ctx context.Context, db *gorm.DB, row *entity.DatasetVersion) error {
	if row == nil || row.Documents != "" || row.DocumentsHash == nil {
		return nil
	}
	documents, err := NewDatasetDocumentSetSnapshotDAO().Get(ctx, db, row.DatasetID, *row.DocumentsHash)
	if err != nil {
		if errors.Is(err, ErrDatasetDocumentSetSnapshotNotFound) {
			return fmt.Errorf("dataset version %d points at document set %s, which no longer exists",
				row.Version, documentset.ShortHash(*row.DocumentsHash))
		}
		return err
	}
	row.Documents = documents
	return nil
}

// FirstVersionBetween returns the lowest version of the dataset whose number
// falls in [from, to), or ErrDatasetVersionNotFound when none does.
//
// The numbers are the anchors a content restore replays the chunk history to
// ("every chunk row written at or before this version"), so "an anchor in this
// window" means "this window is what some version's restore reads". Chunk
// history pruning asks it before dropping a row: removing a row an anchor still
// resolves to would silently change what that version restores. A nil `to`
// leaves the window open at the top.
func (dao *DatasetVersionDAO) FirstVersionBetween(ctx context.Context, db *gorm.DB, datasetID string, from int64, to *int64) (int64, error) {
	var row entity.DatasetVersion
	query := db.WithContext(ctx).
		Select("version").
		Where("dataset_id = ? AND version >= ?", datasetID, from)
	if to != nil {
		query = query.Where("version < ?", *to)
	}
	err := query.Order("version").Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, ErrDatasetVersionNotFound
		}
		return 0, err
	}
	return row.Version, nil
}

// List returns a dataset's versions newest first, without the snapshots. A
// non-nil beforeVersion pages backwards from that version (exclusive), which
// stays correct while new versions are being appended.
func (dao *DatasetVersionDAO) List(ctx context.Context, db *gorm.DB, datasetID string, beforeVersion *int64, kinds []string, limit int) ([]*entity.DatasetVersion, error) {
	if limit <= 0 {
		limit = 50
	}
	query := db.WithContext(ctx).
		Select(datasetVersionMetadataColumns).
		Where("dataset_id = ?", datasetID)
	if beforeVersion != nil {
		query = query.Where("version < ?", *beforeVersion)
	}
	if len(kinds) > 0 {
		query = query.Where("kind IN ?", kinds)
	}
	var rows []*entity.DatasetVersion
	if err := query.Order("version DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListDocumentSets returns every version of the dataset with its document set
// resolved, oldest first.
//
// It is what a caller reads to know which documents the surviving history can
// still restore. A set is written by the version that established it and named by
// the hash the others inherit, so the rows come back with Documents filled in
// wherever a set was recorded at all; a row that resolves to nothing never
// recorded one, which reads as "unknown" rather than "empty".
//
// The hashes are resolved in one query rather than one per row: every version
// inheriting a set names the same hash, so the distinct hashes are far fewer than
// the rows.
func (dao *DatasetVersionDAO) ListDocumentSets(ctx context.Context, db *gorm.DB, datasetID string) ([]*entity.DatasetVersion, error) {
	if db == nil || datasetID == "" {
		return nil, nil
	}
	var rows []*entity.DatasetVersion
	if err := db.WithContext(ctx).
		Select("dataset_id", "version", "documents_hash").
		Where("dataset_id = ?", datasetID).
		Order("version").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	hashes := make([]string, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row.DocumentsHash == nil {
			continue
		}
		if _, ok := seen[*row.DocumentsHash]; ok {
			continue
		}
		seen[*row.DocumentsHash] = struct{}{}
		hashes = append(hashes, *row.DocumentsHash)
	}
	sets, err := NewDatasetDocumentSetSnapshotDAO().GetMany(ctx, db, datasetID, hashes)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.DocumentsHash == nil {
			continue
		}
		documents, ok := sets[*row.DocumentsHash]
		if !ok {
			return nil, fmt.Errorf("dataset version %d points at document set %s, which no longer exists",
				row.Version, documentset.ShortHash(*row.DocumentsHash))
		}
		row.Documents = documents
	}
	return rows, nil
}

// DeleteByDataset drops a dataset's history, used when the dataset itself is
// deleted. The configuration snapshots and the document sets go with it: both are
// scoped to the dataset, and nothing outside it can point at them.
func (dao *DatasetVersionDAO) DeleteByDataset(ctx context.Context, db *gorm.DB, datasetID string) error {
	if datasetID == "" {
		return nil
	}
	if err := db.WithContext(ctx).
		Where("dataset_id = ?", datasetID).
		Delete(&entity.DatasetVersion{}).Error; err != nil {
		return err
	}
	if err := NewDatasetConfigSnapshotDAO().DeleteByDataset(ctx, db, datasetID); err != nil {
		return err
	}
	return NewDatasetDocumentSetSnapshotDAO().DeleteByDataset(ctx, db, datasetID)
}

// DeleteByDatasetAndVersion drops one version of a dataset and reports how many
// rows went. Whether that version may go is the service's call, not the DAO's:
// this only runs the delete, inside the caller's transaction.
func (dao *DatasetVersionDAO) DeleteByDatasetAndVersion(ctx context.Context, tx *gorm.DB, datasetID string, version int64) (int64, error) {
	if datasetID == "" {
		return 0, nil
	}
	result := tx.WithContext(ctx).
		Where("dataset_id = ? AND version = ?", datasetID, version).
		Delete(&entity.DatasetVersion{})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}
