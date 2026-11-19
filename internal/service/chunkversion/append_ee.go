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

package chunkversion

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/engine/history"
	"ragflow/internal/entity"
)

// baselineBatch bounds one insert, so a document with tens of thousands of
// chunks does not build a single oversized statement.
const baselineBatch = 200

// AppendParseBaseline records version 0 for the chunks a parse just wrote.
//
// Without it a chunk nobody edited has no history at all, and a content version
// could not put it back: the parser writes the engine row and moves on. The row
// is stored as "parse" so a replay knows it is the text the parser produced, not
// an edit.
//
// The snapshot is the whole engine row, tokens and vector included, so a restore
// of version 0 is a copy rather than a re-parse: the parser's derived fields are
// what ranked at that version, and recomputing them would need the embedding
// model and could not reproduce them after the model changed. `meta` names the
// producer of those fields.
//
// A chunk that already carries a version 0 (a re-parse that kept the chunk id)
// is left alone: the history of that chunk already starts there.
//
// A chunk whose newest row is a tombstone is the one exception. Chunk ids are
// derived from the text, so a chunk that was removed - by a bulk delete, by a
// re-parse that dropped it, or by a failed index write that was compensated -
// comes back under the same id when it is parsed again. Writing version 0 for it
// would conflict with the row the first parse left behind and be a no-op, so the
// history would keep calling a live chunk removed and a content restore would
// drop it. Those chunks are recorded one past their latest row instead, the same
// way a re-added chunk starts (AppendAddBaseline).
func AppendParseBaseline(ctx context.Context, database *gorm.DB, datasetID string, datasetVersion int64, meta SnapshotMeta, rows []map[string]interface{}) error {
	if database == nil || datasetID == "" || len(rows) == 0 {
		return nil
	}

	// One reading stamps the batch: the rows were written together, and a clock
	// query per row would put the batch size in round trips on the parse path. A
	// nil stamp leaves the entity hook to fill each row from the same clock.
	var createTime *int64
	if stamp, err := entity.DBTimeMillis(database); err == nil {
		createTime = &stamp
	}

	versions := make([]*entity.ChunkVersion, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	chunkIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		chunkID, _ := row["id"].(string)
		documentID, _ := row["doc_id"].(string)
		if chunkID == "" || documentID == "" {
			continue
		}
		if _, ok := seen[chunkID]; ok {
			continue
		}
		seen[chunkID] = struct{}{}
		chunkIDs = append(chunkIDs, chunkID)
		versions = append(versions, &entity.ChunkVersion{
			ChunkID:        chunkID,
			Version:        0,
			DocID:          documentID,
			DatasetID:      datasetID,
			DatasetVersion: datasetVersion,
			CreatedBy:      "system",
			Kind:           entity.ChunkVersionKindParse,
			Snapshot:       FullSnapshot(row, meta),
			CreateTime:     createTime,
		})
	}
	if len(versions) == 0 {
		return nil
	}
	// The chunks the history already recorded as removed are written one past
	// their tombstone; see the doc comment. A failed read falls back to the plain
	// version 0 rows: a conflict there is a no-op, and a stale tombstone only
	// narrows what a replay brings back.
	if tombstones, err := latestTombstoneVersions(ctx, database, chunkIDs); err != nil {
		common.Warn("Chunk parse: failed to read the chunk history of a parse batch",
			zap.String("dataset_id", datasetID), zap.Error(err))
	} else {
		for _, version := range versions {
			if tombstone, ok := tombstones[version.ChunkID]; ok {
				version.Version = tombstone + 1
			}
		}
	}

	// The batch goes to the document engine in one request, so a parse costs one
	// bulk write instead of one per chunk. A batch that could not be stored fails
	// the baseline: a row whose body nobody holds is worse than a missing row,
	// because a replay would rebuild the chunk from an empty snapshot.
	if err := history.StashChunkRows(ctx, versions); err != nil {
		return err
	}

	return database.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		CreateInBatches(versions, baselineBatch).Error
}

// AppendEmptyParseBaseline records a parse that produced no chunks, so a
// restore can tell an empty result from a lost history. The reserved chunk id
// addresses no chunk; readers skip it. One row per document.
func AppendEmptyParseBaseline(ctx context.Context, database *gorm.DB, datasetID string, datasetVersion int64, documentID string) error {
	if database == nil || datasetID == "" || documentID == "" || datasetVersion <= 0 {
		return nil
	}
	row := &entity.ChunkVersion{
		ChunkID:        entity.ChunkVersionEmptyDocumentID,
		Version:        0,
		DocID:          documentID,
		DatasetID:      datasetID,
		DatasetVersion: datasetVersion,
		Kind:           entity.ChunkVersionKindParse,
	}
	return database.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(row).Error
}

// HasEmptyParseMarker looks only for the empty-parse marker, not any history.
func HasEmptyParseMarker(ctx context.Context, database *gorm.DB, documentID string) (bool, error) {
	if database == nil || documentID == "" {
		return false, nil
	}
	var count int64
	if err := database.WithContext(ctx).
		Model(&entity.ChunkVersion{}).
		Where("doc_id = ? AND chunk_id = ?", documentID, entity.ChunkVersionEmptyDocumentID).
		Limit(1).
		Count(&count).Error; err != nil {
		return false, fmt.Errorf("count the parse marker of document %s: %w", documentID, err)
	}
	return count > 0, nil
}

// AppendAddBaseline records the row a manually added chunk starts its history
// with.
//
// The parser writes baselines for the chunks it produces (AppendParseBaseline),
// but AddChunk writes the engine row and moves on. Without this row the chunk has
// no history at all, and a replay of a content version that already held it can
// only rebuild the chunks its timeline knows: the added chunk would be dropped
// from the restored document with nothing reporting it.
//
// A new chunk starts at version 0; a chunk whose id comes back after a removal
// starts one past its latest row, so the timeline reads "it exists again" instead
// of ending at the tombstone.
//
// It is best effort: the engine row is already written, and a missing baseline
// only narrows what a replay can bring back.
func AppendAddBaseline(ctx context.Context, database *gorm.DB, datasetID, documentID, chunkID string, datasetVersion int64, meta SnapshotMeta, row map[string]interface{}) {
	if database == nil || datasetID == "" || documentID == "" || chunkID == "" {
		return
	}
	latest, err := dao.NewChunkVersionDAO().GetLatest(ctx, database, chunkID)
	version := 0
	switch {
	case err == nil && latest != nil:
		if latest.Kind != entity.ChunkVersionKindDelete {
			// Already on the timeline: a chunk id is derived from the text, so a
			// repeated add of the same content is the row that exists.
			return
		}
		version = latest.Version + 1
	case err != nil && !errors.Is(err, dao.ErrChunkVersionNotFound):
		common.Warn("Chunk add: failed to read the chunk history",
			zap.String("chunk_id", chunkID), zap.Error(err))
		return
	}
	record := &entity.ChunkVersion{
		ChunkID:        chunkID,
		Version:        version,
		DocID:          documentID,
		DatasetID:      datasetID,
		DatasetVersion: datasetVersion,
		CreatedBy:      "system",
		Kind:           entity.ChunkVersionKindAdd,
		Snapshot:       FullSnapshot(row, meta),
	}
	if err := history.StashChunk(ctx, record); err != nil {
		common.Warn("Chunk add: failed to store the chunk baseline body",
			zap.String("chunk_id", chunkID), zap.Error(err))
		return
	}
	if err := database.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(record).Error; err != nil {
		common.Warn("Chunk add: failed to record the chunk baseline",
			zap.String("chunk_id", chunkID), zap.Error(err))
	}
}

// AppendTombstones records that the given chunks were removed. A replay of a
// content version recorded after the removal has to know they did not exist;
// without the row it would rebuild them from their last content version.
//
// It is best effort: the chunks are already gone from the engine, and a missing
// tombstone only widens what a replay brings back.
//
// The whole set is written in two round trips - one grouped read of the latest
// version per chunk, one batched insert - rather than two per chunk: a
// document-wide pass runs on every re-parse and every bulk delete, and the parse
// it follows has already waited on the engine.
func AppendTombstones(ctx context.Context, database *gorm.DB, datasetID, documentID string, datasetVersion int64, chunkIDs []string, createdBy string) {
	if database == nil || datasetID == "" || documentID == "" || len(chunkIDs) == 0 {
		return
	}
	// A removal someone asked for carries their id, the same way an edit does; a
	// removal a parse produced - and one a dataset restore's replay produced - has
	// no user behind it and stays "system". Falling back here rather than at every
	// caller keeps the read side honest: a tombstone never claims an unnamed actor
	// when one was known.
	if strings.TrimSpace(createdBy) == "" {
		createdBy = entity.ChunkVersionSystemActor
	}
	unique := make([]string, 0, len(chunkIDs))
	seen := make(map[string]struct{}, len(chunkIDs))
	for _, chunkID := range chunkIDs {
		if chunkID == "" {
			continue
		}
		if _, ok := seen[chunkID]; ok {
			continue
		}
		seen[chunkID] = struct{}{}
		unique = append(unique, chunkID)
	}
	if len(unique) == 0 {
		return
	}

	versionDAO := dao.NewChunkVersionDAO()
	// One reading for the removal: every tombstone describes the same moment.
	var createTime *int64
	if stamp, err := entity.DBTimeMillis(database); err == nil {
		createTime = &stamp
	}
	for start := 0; start < len(unique); start += baselineBatch {
		end := start + baselineBatch
		if end > len(unique) {
			end = len(unique)
		}
		batch := unique[start:end]
		latest, err := versionDAO.LatestVersions(ctx, database, batch)
		if err != nil {
			common.Warn("Chunk delete: failed to read the chunk history for the tombstones",
				zap.String("dataset_id", datasetID), zap.Error(err))
			continue
		}
		rows := make([]*entity.ChunkVersion, 0, len(batch))
		for _, chunkID := range batch {
			next := 0
			if version, ok := latest[chunkID]; ok {
				next = version + 1
			}
			rows = append(rows, &entity.ChunkVersion{
				ChunkID:        chunkID,
				Version:        next,
				DocID:          documentID,
				DatasetID:      datasetID,
				DatasetVersion: datasetVersion,
				CreatedBy:      createdBy,
				Kind:           entity.ChunkVersionKindDelete,
				Snapshot:       entity.JSONMap{},
				CreateTime:     createTime,
			})
		}
		if err := database.WithContext(ctx).
			Clauses(clause.OnConflict{DoNothing: true}).
			CreateInBatches(rows, baselineBatch).Error; err != nil {
			common.Warn("Chunk delete: failed to record the removal tombstones",
				zap.String("dataset_id", datasetID), zap.Error(err))
		}
	}
}

// latestTombstoneVersions reports, for the chunks whose newest history row is a
// tombstone, the version of that tombstone. A chunk with no row, or with a row
// that says it exists, is absent from the map. It is what lets a parse baseline
// land past a removal instead of behind it; see AppendParseBaseline.
func latestTombstoneVersions(ctx context.Context, database *gorm.DB, chunkIDs []string) (map[string]int, error) {
	tombstones := make(map[string]int, len(chunkIDs))
	if database == nil || len(chunkIDs) == 0 {
		return tombstones, nil
	}
	versionDAO := dao.NewChunkVersionDAO()
	for start := 0; start < len(chunkIDs); start += baselineBatch {
		end := start + baselineBatch
		if end > len(chunkIDs) {
			end = len(chunkIDs)
		}
		newest, err := versionDAO.LatestVersionRows(ctx, database, chunkIDs[start:end])
		if err != nil {
			return tombstones, err
		}
		for chunkID, row := range newest {
			if row != nil && row.Kind == entity.ChunkVersionKindDelete {
				tombstones[chunkID] = row.Version
			}
		}
	}
	return tombstones, nil
}

// AppendDocumentTombstones records the removal of every chunk the document's
// history knows. It is the whole-document form of AppendTombstones, used when a
// re-parse or a bulk delete clears the document.
func AppendDocumentTombstones(ctx context.Context, database *gorm.DB, datasetID, documentID string, datasetVersion int64, createdBy string) {
	if database == nil || documentID == "" {
		return
	}
	var chunkIDs []string
	if err := database.WithContext(ctx).
		Model(&entity.ChunkVersion{}).
		Where("doc_id = ?", documentID).
		Distinct().
		Pluck("chunk_id", &chunkIDs).Error; err != nil {
		common.Warn("Chunk delete: failed to list the chunk history for the tombstones",
			zap.String("doc_id", documentID), zap.Error(err))
		return
	}
	AppendTombstones(ctx, database, datasetID, documentID, datasetVersion, chunkIDs, createdBy)
}

// OpenChunkIDs lists the ids of the document's chunks whose latest history row
// says they exist at the present moment.
//
// A re-parse rewrites a document's chunk set, and chunk ids are derived from the
// text, so the ids a run did not write are exactly the chunks that disappeared.
// The caller diffs this set against the ids a run produced and tombstones the
// difference; without that, a replay of the version the run recorded would bring
// the dropped chunks back to life.
func OpenChunkIDs(ctx context.Context, database *gorm.DB, documentID string) ([]string, error) {
	if database == nil || documentID == "" {
		return nil, nil
	}
	var chunkIDs []string
	err := database.WithContext(ctx).
		Model(&entity.ChunkVersion{}).
		Where("doc_id = ?", documentID).
		Where("chunk_id <> ?", entity.ChunkVersionEmptyDocumentID).
		Where("version = (SELECT MAX(v.version) FROM chunk_version v WHERE v.chunk_id = chunk_version.chunk_id)").
		Where("kind <> ?", entity.ChunkVersionKindDelete).
		Pluck("chunk_id", &chunkIDs).Error
	if err != nil {
		return nil, err
	}
	return chunkIDs, nil
}

// HasAnyHistory reports whether the document has chunk history at all, at any
// dataset version.
//
// A replay that finds nothing at or before its anchor cannot tell two situations
// apart by itself: a document that had no chunks then (uploaded, never parsed
// before the anchor) and a document whose history does not reach it at all (not
// written before the history existed, or purged). The first means the version
// described an empty document and the engine has to be cleared; the second means
// clearing would destroy content the history cannot restore. Rows written after
// the anchor are what separate them.
func HasAnyHistory(ctx context.Context, database *gorm.DB, documentID string) (bool, error) {
	if database == nil || documentID == "" {
		return false, nil
	}
	var count int64
	if err := database.WithContext(ctx).
		Model(&entity.ChunkVersion{}).
		Where("doc_id = ?", documentID).
		Limit(1).
		Count(&count).Error; err != nil {
		return false, fmt.Errorf("count the chunk history of document %s: %w", documentID, err)
	}
	return count > 0, nil
}

// StatesAt replays a document's chunk history up to dataset version
// `atVersion`: for every chunk the latest version recorded at or before that
// number wins, and a delete row means the chunk did not exist then. Chunks whose
// first version is later are absent, which is how a restore drops what was added
// after the target version.
//
// The anchor is the dataset's version number, not its timestamp: two writes of
// the same chunk can land in the same millisecond, so a clock cannot order them,
// while the number the writers took from the version recorder can. A row written
// by the same change as the anchor (a parse batch, a bulk toggle) shares its
// number and is therefore part of the state that change produced.
//
// The result is ordered by chunk id, so a rebuild writes batches in a stable
// order and a retry produces the same engine content.
func StatesAt(ctx context.Context, database *gorm.DB, documentID string, atVersion int64) ([]ChunkState, error) {
	if database == nil || documentID == "" {
		return nil, nil
	}

	var rows []*entity.ChunkVersion
	if err := database.WithContext(ctx).
		Where("doc_id = ? AND dataset_version <= ?", documentID, atVersion).
		Order("chunk_id, version").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load the chunk history of document %s: %w", documentID, err)
	}
	// The empty-parse marker addresses no chunk.
	chunkRows := make([]*entity.ChunkVersion, 0, len(rows))
	for _, row := range rows {
		if row.ChunkID == entity.ChunkVersionEmptyDocumentID {
			continue
		}
		chunkRows = append(chunkRows, row)
	}
	rows = chunkRows
	if len(rows) == 0 {
		return nil, nil
	}
	// The state of a chunk is what the caller rebuilds from, so a snapshot that
	// cannot be read fails the replay instead of rebuilding half a document.
	if err := history.HydrateChunks(ctx, rows); err != nil {
		return nil, err
	}

	latest := make(map[string]*entity.ChunkVersion, len(rows))
	for _, row := range rows {
		current, ok := latest[row.ChunkID]
		if !ok || row.Version > current.Version {
			latest[row.ChunkID] = row
		}
	}

	states := make([]ChunkState, 0, len(latest))
	for chunkID, row := range latest {
		states = append(states, ChunkState{
			ChunkID:  chunkID,
			Version:  row.Version,
			Snapshot: row.Snapshot,
			Exists:   row.Kind != entity.ChunkVersionKindDelete,
		})
	}
	sort.Slice(states, func(i, j int) bool { return states[i].ChunkID < states[j].ChunkID })
	return states, nil
}
