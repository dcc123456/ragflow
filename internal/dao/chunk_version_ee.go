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

	"ragflow/internal/engine/history"
	"ragflow/internal/entity"
)

// ErrChunkVersionNotFound is returned when a (chunk id, version) pair has no
// row. Callers map it to "version not found" instead of leaking
// gorm.ErrRecordNotFound.
var ErrChunkVersionNotFound = errors.New("chunk_version: not found")

// ChunkVersionDAO persists and queries chunk_version rows.
type ChunkVersionDAO struct{}

// NewChunkVersionDAO returns a stateless DAO shared by the chunk service and
// the deletion paths that clean history up.
func NewChunkVersionDAO() *ChunkVersionDAO {
	return &ChunkVersionDAO{}
}

// chunkVersionMetadataColumns are the columns a version list needs. The
// snapshot blob is deliberately excluded: it can hold the whole chunk content
// and the list only shows version, author, time and what produced the row.
// kind is included because the list mirrors the server's refusal to delete a
// parse/add/delete row: without it every entry would look like plain history.
var chunkVersionMetadataColumns = []string{
	"chunk_id", "version", "doc_id", "dataset_id",
	"restored_from", "created_by", "create_time", "kind",
}

// Create inserts one version row using the caller's transaction.
//
// The snapshot body goes to the document engine first (see internal/engine/history): the
// row keeps what a reader needs to find it, not the chunk itself. That is the
// entity's BeforeCreate hook rather than a step here, so no other insert path can
// write a row whose state nobody holds.
func (dao *ChunkVersionDAO) Create(ctx context.Context, tx *gorm.DB, row *entity.ChunkVersion) error {
	return tx.WithContext(ctx).Create(row).Error
}

// UpdateSnapshot rewrites the stored state of one version in place. Edits that
// only change a chunk's annotations keep the version they belong to instead of
// opening a new one, so nothing is written to the metadata table: the version
// keeps its number, and its body keeps the address that number gives it.
//
// The rewrite is one document overwrite, which is why an in-place annotation
// edit costs no row update at all.
func (dao *ChunkVersionDAO) UpdateSnapshot(ctx context.Context, tx *gorm.DB, row *entity.ChunkVersion, snapshot entity.JSONMap) error {
	if row == nil {
		return nil
	}
	stashed := *row
	stashed.Snapshot = snapshot
	return history.StashChunk(ctx, &stashed)
}

// GetLatest returns the highest version recorded for a chunk. The snapshot is
// loaded because the writer compares it with the state an edit produced and
// skips a duplicate row.
func (dao *ChunkVersionDAO) GetLatest(ctx context.Context, db *gorm.DB, chunkID string) (*entity.ChunkVersion, error) {
	var row entity.ChunkVersion
	err := db.WithContext(ctx).
		Where("chunk_id = ?", chunkID).
		Order("version DESC").
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrChunkVersionNotFound
		}
		return nil, err
	}
	if err := history.HydrateChunks(ctx, []*entity.ChunkVersion{&row}); err != nil {
		return nil, err
	}
	return &row, nil
}

// GetLatestForUpdate returns the highest version recorded for a chunk and
// locks its row until the caller's transaction ends. The locking read is a
// current read, so writers that race on the same chunk serialize here instead
// of racing between the version check and the version insert. When no row
// exists yet the database gaps the unique index until the transaction ends,
// which is what stops two first edits from seeding version 0 twice.
func (dao *ChunkVersionDAO) GetLatestForUpdate(ctx context.Context, tx *gorm.DB, chunkID string) (*entity.ChunkVersion, error) {
	var row entity.ChunkVersion
	err := tx.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("chunk_id = ?", chunkID).
		Order("version DESC").
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrChunkVersionNotFound
		}
		return nil, err
	}
	// Hydrated inside the locking read: the writer compares the state an edit
	// produced with the one this row holds, and comparing half a row would judge
	// the edit from an empty snapshot.
	if err := history.HydrateChunks(ctx, []*entity.ChunkVersion{&row}); err != nil {
		return nil, err
	}
	return &row, nil
}

// LatestVersions returns the highest version recorded per chunk, in one query,
// for a page of listed chunks. A chunk that was never edited is absent from the
// map: its current state is the parser's output, i.e. version 0.
func (dao *ChunkVersionDAO) LatestVersions(ctx context.Context, db *gorm.DB, chunkIDs []string) (map[string]int, error) {
	latest := make(map[string]int, len(chunkIDs))
	if len(chunkIDs) == 0 {
		return latest, nil
	}
	var rows []struct {
		ChunkID string
		Version int
	}
	err := db.WithContext(ctx).
		Model(&entity.ChunkVersion{}).
		Select("chunk_id, MAX(version) AS version").
		Where("chunk_id IN ?", chunkIDs).
		Group("chunk_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		latest[row.ChunkID] = row.Version
	}
	return latest, nil
}

// VersionsAtDatasetVersion returns the highest chunk version each of the given
// chunks was written under the given dataset version, in one query. A chunk the
// version did not touch - or whose history was pruned since - is absent from the
// map.
//
// One dataset version can write two rows for the same chunk: the state an edit
// replaced and the one it produced. The answer is the maximum, which is the row
// the change produced; the other one is the state before it.
func (dao *ChunkVersionDAO) VersionsAtDatasetVersion(ctx context.Context, db *gorm.DB, datasetID string, datasetVersion int64, chunkIDs []string) (map[string]int, error) {
	versions := make(map[string]int, len(chunkIDs))
	if db == nil || datasetID == "" || datasetVersion <= 0 || len(chunkIDs) == 0 {
		return versions, nil
	}
	var rows []struct {
		ChunkID string
		Version int
	}
	err := db.WithContext(ctx).
		Model(&entity.ChunkVersion{}).
		Select("chunk_id, MAX(version) AS version").
		Where("dataset_id = ? AND dataset_version = ? AND chunk_id IN ?", datasetID, datasetVersion, chunkIDs).
		Group("chunk_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		versions[row.ChunkID] = row.Version
	}
	return versions, nil
}

// LatestVersionRows returns the newest history row of each chunk, with the
// metadata a replay needs to tell whether it already recorded its restore. The
// snapshots are left out: they can be large, and the caller already has the
// anchor state it is restoring.
func (dao *ChunkVersionDAO) LatestVersionRows(ctx context.Context, db *gorm.DB, chunkIDs []string) (map[string]*entity.ChunkVersion, error) {
	latest := make(map[string]*entity.ChunkVersion, len(chunkIDs))
	if len(chunkIDs) == 0 {
		return latest, nil
	}
	newest := db.WithContext(ctx).
		Model(&entity.ChunkVersion{}).
		Select("chunk_id, MAX(version) AS version").
		Where("chunk_id IN ?", chunkIDs).
		Group("chunk_id")
	var rows []*entity.ChunkVersion
	err := db.WithContext(ctx).
		Table("chunk_version").
		Select("chunk_version.chunk_id, chunk_version.version, chunk_version.kind, chunk_version.restored_from").
		Joins("JOIN (?) AS latest ON latest.chunk_id = chunk_version.chunk_id AND latest.version = chunk_version.version", newest).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		latest[row.ChunkID] = row
	}
	return latest, nil
}

// NextDatasetVersionAfter returns the dataset version of the row that follows
// the given chunk version in the chunk's timeline, or nil when that version is
// the last one. The pair (this row's dataset version, the next row's) is the
// window of dataset versions during which this row is the state a replay reads,
// which is what a history prune has to check against the anchors that exist.
//
// Two rows written by the same change share a number; their window is then empty
// and the pair is returned with `to` no higher than `from`, which the caller
// reads as "no anchor can select this row".
func (dao *ChunkVersionDAO) NextDatasetVersionAfter(ctx context.Context, db *gorm.DB, chunkID string, version int) (*int64, error) {
	var next struct {
		Version        int
		DatasetVersion int64
	}
	err := db.WithContext(ctx).
		Model(&entity.ChunkVersion{}).
		Select("version, dataset_version").
		Where("chunk_id = ? AND version > ?", chunkID, version).
		Order("version").
		Take(&next).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &next.DatasetVersion, nil
}

// ListByChunk returns a chunk's versions newest first, without snapshots.
func (dao *ChunkVersionDAO) ListByChunk(ctx context.Context, db *gorm.DB, chunkID string) ([]*entity.ChunkVersion, error) {
	var rows []*entity.ChunkVersion
	err := db.WithContext(ctx).
		Select(chunkVersionMetadataColumns).
		Where("chunk_id = ?", chunkID).
		Order("version DESC").
		Find(&rows).Error
	return rows, err
}

// GetByChunkAndVersion loads one version including its snapshot.
func (dao *ChunkVersionDAO) GetByChunkAndVersion(ctx context.Context, db *gorm.DB, chunkID string, version int) (*entity.ChunkVersion, error) {
	var row entity.ChunkVersion
	err := db.WithContext(ctx).
		Where("chunk_id = ? AND version = ?", chunkID, version).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrChunkVersionNotFound
		}
		return nil, err
	}
	// The caller asked for one version to read, diff or restore; the state is the
	// point of the read, so it comes back whole.
	if err := history.HydrateChunks(ctx, []*entity.ChunkVersion{&row}); err != nil {
		return nil, err
	}
	return &row, nil
}

// ListImageIDs returns the distinct chunk images the history in scope refers
// to: one image ID per snapshot that recorded one. Images are stored under a
// content-addressed object key (so an older version keeps its own object
// instead of being overwritten), which means dropping the history must also
// drop the objects it holds. Scope is the whole document when all is true and
// the given chunks otherwise.
func (dao *ChunkVersionDAO) ListImageIDs(ctx context.Context, db *gorm.DB, docID string, chunkIDs []string, all bool) ([]string, error) {
	query := db.WithContext(ctx).Model(&entity.ChunkVersion{}).
		Select("chunk_id, version, dataset_id").
		Where("chunk_id <> ?", entity.ChunkVersionEmptyDocumentID)
	if all {
		if docID == "" {
			return nil, nil
		}
		query = query.Where("doc_id = ?", docID)
	} else {
		if len(chunkIDs) == 0 {
			return nil, nil
		}
		query = query.Where("chunk_id IN ?", chunkIDs)
	}
	var rows []*entity.ChunkVersion
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	// The image id lives inside the snapshot, and the snapshot lives in the
	// document engine: one read per dataset, shared by every row it covers.
	if err := history.HydrateChunks(ctx, rows); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(rows))
	imageIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		imageID, _ := row.Snapshot["img_id"].(string)
		if imageID == "" {
			continue
		}
		if _, ok := seen[imageID]; ok {
			continue
		}
		seen[imageID] = struct{}{}
		imageIDs = append(imageIDs, imageID)
	}
	return imageIDs, nil
}

// DeleteByChunkAndVersion drops one version of a chunk and reports how many rows
// went. Whether that version may go is the service's call, not the DAO's: this
// only runs the delete, inside the caller's transaction.
func (dao *ChunkVersionDAO) DeleteByChunkAndVersion(ctx context.Context, tx *gorm.DB, chunkID string, version int) (int64, error) {
	if chunkID == "" {
		return 0, nil
	}
	result := tx.WithContext(ctx).
		Where("chunk_id = ? AND version = ?", chunkID, version).
		Delete(&entity.ChunkVersion{})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// DeleteByDataset drops every version of a dataset, used when the dataset is
// deleted. A dataset delete removes the documents without going through the
// per-document removal, so nothing else would ever collect their history: the
// removed-document cleanup only walks removal rows, and a deleted dataset
// writes none.
//
// The bodies go with the rows: the store is dropped after the rows are gone, so
// an interrupted delete leaves rows that still have their state rather than
// rows that do not.
func (dao *ChunkVersionDAO) DeleteByDataset(ctx context.Context, db *gorm.DB, datasetID string) error {
	if datasetID == "" {
		return nil
	}
	if err := db.WithContext(ctx).
		Where("dataset_id = ?", datasetID).
		Delete(&entity.ChunkVersion{}).Error; err != nil {
		return err
	}
	return history.DeleteChunkDataset(ctx, datasetID)
}

// DeleteByDocID drops every version of a document, used when the document
// (and therefore all of its chunks) is deleted or re-parsed.
//
// The document's bodies go too. They are the only copy of what those versions
// said, and the document they describe is gone, so keeping them would keep
// content nobody is allowed to read any more.
func (dao *ChunkVersionDAO) DeleteByDocID(ctx context.Context, db *gorm.DB, datasetID, docID string) error {
	if docID == "" {
		return nil
	}
	if err := db.WithContext(ctx).
		Where("doc_id = ?", docID).
		Delete(&entity.ChunkVersion{}).Error; err != nil {
		return err
	}
	if datasetID == "" {
		return nil
	}
	return history.DeleteChunkDocument(ctx, datasetID, docID)
}
