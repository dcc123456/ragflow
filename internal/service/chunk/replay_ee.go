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

package chunk

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/service/chunkversion"
	"ragflow/internal/service/datasetversion"
)

// ReplayDocumentChunksAtEE puts a document's chunks back as they stood at
// `atVersion`, replaying the chunk history. The document's engine rows are
// cleared first, so the result is that moment's state instead of a merge with
// the current rows.
//
// It is the content step of a dataset restore, and only that step: the caller
// has already put the document rows and the configuration back and reserved the
// version the restore occupies. What this brings is the chunks. Chunks added
// after the anchor stay absent because they have no version at or before it;
// chunks removed before the anchor stay absent because their latest version
// there is a tombstone.
//
// The anchor is the dataset version number: a chunk history row carries the
// version that wrote it, and the state at an anchor is the newest row at or
// below it (see chunkversion.StatesAt).
//
// A chunk whose stored snapshot is a full row - an edit or an earlier restore -
// comes back as a copy of that row, tokens and vector included, so the restore
// costs no tokenizer run and no embedding call and the chunk ranks exactly as
// the version did. A version whose snapshot is not a full row cannot be copied,
// and this does not derive one: the row is refused and the document is reported
// as missing, which is where the caller asks for a re-parse.
//
// The replayed rows keep the reading position the snapshot recorded, so a
// document comes back in the order it was in rather than in chunk-id order.
//
// Recording the restore in the chunk history is part of the replay, not a
// caller concern: see recordReplayedChunkHistory for what the rows mean.
//
// restoreVersion is the dataset version number the restore occupies, reserved by
// caller before it replays any document: the history rows this replay writes
// carry it, and the restore's own version row lands on it too, so one restore
// advances the dataset by exactly one version. A non-positive value leaves the
// replay reserving a number for itself, which costs one number per document and
// is only what a caller that keeps no shared number can ask for.
func (s *ChunkService) ReplayDocumentChunksAtEE(ctx context.Context, datasetID, documentID string, atVersion int64, restoreVersion int64) (int, error) {
	if s.docEngine == nil {
		return 0, fmt.Errorf("doc engine not initialized")
	}
	if dao.DB == nil {
		return 0, fmt.Errorf("metadata database is not initialized")
	}
	kb, err := s.kbDAO.GetByID(ctx, dao.DB, datasetID)
	if err != nil || kb == nil {
		return 0, fmt.Errorf("dataset not found")
	}
	doc, err := s.documentDAO.GetByID(ctx, dao.DB, documentID)
	if err != nil || doc == nil {
		return 0, fmt.Errorf("document not found")
	}

	states, err := chunkversion.StatesAt(ctx, dao.DB, documentID, atVersion)
	if err != nil {
		return 0, err
	}

	// The chunks the history calls open right now are the ones this replay is
	// about to drop: at the anchor they either did not exist yet or were already
	// deleted. They are read before any row is written below.
	openBefore, err := chunkversion.OpenChunkIDs(ctx, dao.DB, documentID)
	if err != nil {
		common.Warn("Chunk restore: failed to read the open chunks of the document",
			zap.String("document_id", documentID),
			zap.Error(err))
		openBefore = nil
	}

	indexName := fmt.Sprintf("ragflow_%s", kb.TenantID)
	language := datasetLanguageOf(kb)
	// The rows are built before the engine is touched. Tokenizing a chunk can
	// fail, and doing that after the clear would leave the document with no
	// chunks at all - worse than keeping the ones it already has.
	rows := make([]map[string]interface{}, 0, len(states))
	for _, state := range states {
		if !state.Exists {
			continue
		}
		row, buildErr := s.replayChunkRow(kb, doc, state, len(rows))
		if buildErr != nil {
			return len(rows), buildErr
		}
		rows = append(rows, row)
	}

	var tokenNum int64
	for _, row := range rows {
		if content, ok := row["content_with_weight"].(string); ok {
			tokenNum += int64(s.numTokens(content))
		}
	}

	// The replay is the whole state of the document at that moment: anything the
	// engine holds now is either replayed below or did not exist then.
	if _, err := s.docEngine.DeleteChunks(ctx, map[string]interface{}{"doc_id": documentID}, indexName, datasetID); err != nil {
		return 0, fmt.Errorf("clear the document chunks: %w", err)
	}
	if len(rows) > 0 {
		if _, err := s.docEngine.InsertChunks(ctx, rows, indexName, datasetID, language); err != nil {
			// The engine is empty (or partly written) and the replay is not there:
			// say so in the counters instead of returning over a document that
			// claims the chunks it no longer has.
			s.compensateFailedChunkRestore(ctx, datasetID, documentID, atVersion, indexName)
			return 0, fmt.Errorf("write the replayed chunks: %w", err)
		}
	}

	// The engine now holds the anchor state, so the history has to agree with it:
	// a chunk whose history moved past the anchor gets the restore row, and a
	// chunk the anchor dropped gets the tombstone. Both are best effort, for the
	// same reason the edit path records after its engine write.
	s.recordReplayedChunkHistory(ctx, datasetID, documentID, states, openBefore, restoreVersion)

	// The document counters and the dataset aggregate move together: the replay
	// can replay a different number of chunks than the document held before it,
	// and the dataset list would otherwise keep reporting the pre-restore totals.
	if countErr := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current entity.Document
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", documentID).Take(&current).Error; err != nil {
			return err
		}
		// A replay that wrote no row left the document without content. Reporting
		// progress 1.0 would tell the list the document is parsed while there is
		// nothing to retrieve from it: the version held no chunk, and the next parse
		// is what produces them.
		progress := 1.0
		progressMsg := fmt.Sprintf("Restored %d chunk(s) from the version history", len(rows))
		if len(rows) == 0 {
			progress = 0.0
			progressMsg = "The version held no chunk for this document; parse it to rebuild them"
		}
		if err := tx.WithContext(ctx).
			Model(&entity.Document{}).
			Where("id = ?", documentID).
			Updates(map[string]interface{}{
				"chunk_num":    len(rows),
				"token_num":    tokenNum,
				"progress":     progress,
				"progress_msg": progressMsg,
				// The marker that says where this content came from, written with
				// the counters so the two can never disagree: chunk_num > 0 with a
				// version here is a restore that brought chunks back, 0 with a
				// version is a version that held none. A parse clears it.
				"restored_version": atVersion,
			}).Error; err != nil {
			return err
		}
		chunkDelta := int64(len(rows)) - current.ChunkNum
		tokenDelta := tokenNum - current.TokenNum
		if chunkDelta == 0 && tokenDelta == 0 {
			return nil
		}
		return tx.WithContext(ctx).Model(&entity.Knowledgebase{}).
			Where("id = ?", datasetID).
			Updates(map[string]interface{}{
				"chunk_num": gorm.Expr("CASE WHEN chunk_num + ? >= 0 THEN chunk_num + ? ELSE 0 END", chunkDelta, chunkDelta),
				"token_num": gorm.Expr("CASE WHEN token_num + ? >= 0 THEN token_num + ? ELSE 0 END", tokenDelta, tokenDelta),
			}).Error
	}); countErr != nil {
		// The counters are derived: both lists can recompute them, and the content
		// and the history are already in step. Returning an error here would only
		// invite a retry that replays the same chunks and records the restore
		// again, so the failure is logged and the restore stands.
		common.Warn("Chunk restore: failed to update the restored counters",
			zap.String("dataset_id", datasetID),
			zap.String("document_id", documentID),
			zap.Error(countErr))
	}
	return len(rows), nil
}

// compensateFailedChunkRestore runs when the engine was cleared but the replay
// could not be written back. It leaves the records describing the same document:
// no chunks in the engine, zero chunks in the counters, and a message that says
// why. The chunk history is deliberately left alone - it still calls the chunks
// open, which is what lets a later replay bring them back; a tombstone would make
// the loss look deliberate and cut that path off.
//
// The restore marker is written here too, with zero chunks: the document was part
// of the restore, and "marker set, chunk_num 0" is exactly what a version holding
// no content for it looks like. Without the marker the failure is invisible to
// every reader of the document - the list falls back to the previous ingest
// task's outcome and can call a document with no chunks completed, and the
// version page cannot tell which of its documents need a re-parse.
//
// Clearing again first makes "no chunks" true even when the bulk wrote part of
// the batch. If that second clear fails the engine still holds rows, so the
// counters are left alone rather than claiming zero over content that exists.
func (s *ChunkService) compensateFailedChunkRestore(ctx context.Context, datasetID, documentID string, atVersion int64, indexName string) {
	writeCtx := context.WithoutCancel(ctx)
	if _, err := s.docEngine.DeleteChunks(writeCtx, map[string]interface{}{"doc_id": documentID}, indexName, datasetID); err != nil {
		common.Warn("Chunk restore: failed to clear the partial chunks after a failed replay",
			zap.String("dataset_id", datasetID),
			zap.String("document_id", documentID),
			zap.Error(err))
		return
	}
	err := dao.DB.WithContext(writeCtx).Transaction(func(tx *gorm.DB) error {
		var current entity.Document
		if err := tx.WithContext(writeCtx).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", documentID).Take(&current).Error; err != nil {
			return err
		}
		if err := tx.WithContext(writeCtx).
			Model(&entity.Document{}).
			Where("id = ?", documentID).
			Updates(map[string]interface{}{
				"chunk_num":        0,
				"token_num":        0,
				"progress":         0.0,
				"progress_msg":     "Restore failed; the document has no chunks until it is re-parsed",
				"restored_version": atVersion,
			}).Error; err != nil {
			return err
		}
		chunkDelta := -current.ChunkNum
		tokenDelta := -current.TokenNum
		if chunkDelta == 0 && tokenDelta == 0 {
			return nil
		}
		return tx.WithContext(writeCtx).Model(&entity.Knowledgebase{}).
			Where("id = ?", datasetID).
			Updates(map[string]interface{}{
				"chunk_num": gorm.Expr("CASE WHEN chunk_num + ? >= 0 THEN chunk_num + ? ELSE 0 END", chunkDelta, chunkDelta),
				"token_num": gorm.Expr("CASE WHEN token_num + ? >= 0 THEN token_num + ? ELSE 0 END", tokenDelta, tokenDelta),
			}).Error
	})
	if err != nil {
		common.Warn("Chunk restore: failed to zero the counters after a failed replay",
			zap.String("dataset_id", datasetID),
			zap.String("document_id", documentID),
			zap.Error(err))
	}
}

// replayChunkRow turns a stored full snapshot into the engine row that carries
// it. The document name and the dataset come from the live document because they
// describe where the chunk sits, not what it says; the reading position comes
// from the snapshot, because that is the document order being restored.
//
// The row is copied, never rebuilt: the version recorded the tokens and the
// vector it was ranked by, and producing them again would mean a tokenizer run
// per chunk on a path that restores thousands of them. A version recorded before
// snapshots held their derived fields cannot be replayed at all, and says so.
func (s *ChunkService) replayChunkRow(
	kb *entity.Knowledgebase,
	doc *entity.Document,
	state chunkversion.ChunkState,
	fallbackOrder int,
) (map[string]interface{}, error) {
	row, ok := chunkReplayStoredRow(state, kb, doc, fallbackOrder)
	if !ok {
		return nil, fmt.Errorf("chunk %s: its version holds no full snapshot to replay; re-parse the document to rebuild it", state.ChunkID)
	}
	if kb.Pagerank != 0 {
		row[common.PAGERANK_FLD] = kb.Pagerank
	}
	return row, nil
}

// chunkReplayStoredRow replays a chunk from the stored row of a full snapshot.
//
// The row a version recorded is handed back as it was written - content, tokens,
// keywords, positions, picture and vector - so a document restore costs no
// tokenizer run and no embedding call, and the chunk comes back ranking exactly
// as the version did.
//
// It refuses only when the snapshot is not a full row at all (a version recorded
// before snapshots held their derived fields), because there is nothing to copy
// and a restore does not derive anything: a dataset whose embedding model or
// language changed since the version was written still gets its recorded row
// back - restoring a version returns that version, and a re-parse is what brings
// derived fields up to date.
func chunkReplayStoredRow(
	state chunkversion.ChunkState,
	kb *entity.Knowledgebase,
	doc *entity.Document,
	fallbackOrder int,
) (map[string]interface{}, bool) {
	row, stored, ok := chunkversion.FullSnapshotRow(state.Snapshot)
	if !ok || kb == nil || doc == nil {
		return nil, false
	}

	replayed := make(map[string]interface{}, len(row)+4)
	for key, value := range row {
		replayed[key] = value
	}
	// Where the chunk sits is the live document's, not the snapshot's: the name,
	// the dataset and the id describe the document being restored, and the row a
	// snapshot holds left them out on purpose.
	docName := ""
	if doc.Name != nil {
		docName = *doc.Name
	}
	replayed["id"] = state.ChunkID
	replayed["doc_id"] = doc.ID
	replayed["kb_id"] = kb.ID
	replayed["docnm_kwd"] = docName
	replayed["create_time"] = time.Now().Format("2006-01-02 15:04:05")
	// The reading position is the document order being restored; a row that
	// recorded none keeps the slot the replay writes it in.
	replayed["chunk_order_int"] = replayChunkOrder(state.Snapshot, fallbackOrder)
	replayed["available_int"] = chunkReplayAvailability(state.Snapshot, doc)
	// The stored vector came back through JSON as []interface{}; the engine
	// write takes a typed slice.
	if vector, ok := chunkversion.SnapshotVector(row[stored.VectorField]); ok {
		replayed[stored.VectorField] = vector
	}
	return replayed, true
}

// chunkReplayAvailability answers whether a replayed chunk takes part in
// retrieval: its own switch as the version recorded it, gated by the document's.
//
// The snapshot records a hidden row (a parent chunk, a staged wiki page, a chunk
// someone hid). Forcing those back to 1 would put rows into retrieval that were
// never part of the searchable content; a history recorded before the field was
// stored describes chunks that were visible, so absence means visible.
//
// The document's switch is a second gate the snapshot cannot express: a document
// is enabled or disabled as a whole, and its chunks follow it without each
// holding a version of their own. Replaying them as visible would put a disabled
// document back into retrieval the moment a version is restored. `doc` is the row
// as the restore has just put it back (see
// document.RestoreDocumentStatusAtEE), so the status read here is the one of the
// version being restored.
func chunkReplayAvailability(snapshot entity.JSONMap, doc *entity.Document) int {
	availableInt := 1
	if value, ok := snapshot["available_int"]; ok {
		if parsed, ok := common.GetInt(value); ok {
			availableInt = parsed
		}
	}
	if doc != nil && doc.Status != nil && *doc.Status != string(entity.StatusValid) {
		availableInt = 0
	}
	return availableInt
}

// replayChunkOrder reads the reading position a snapshot recorded. A history row
// written before the position was stored carries none, and the rebuilt chunk
// keeps the slot it was written in, so an older history still restores instead
// of stacking every chunk on position zero.
//
// The value is normalized to int: the snapshot keeps whatever the engine
// returned, and that is int64 on one engine and float64 after a JSON round trip
// on another.
func replayChunkOrder(snapshot entity.JSONMap, fallback int) int {
	if raw, ok := snapshot["chunk_order_int"]; ok {
		if order, ok := common.GetInt(raw); ok {
			return order
		}
	}
	return fallback
}

// recordReplayedChunkHistory puts the chunk history back in step with the content
// the replay just wrote.
//
// A chunk whose history moved past the anchor is recorded as a restore of the
// version the snapshot came from. Without that row the newest version would
// still describe the state the restore replaced, so the chunk list would show a
// version number beside text that no longer matches it, and the invariant that
// the highest version describes the live row would be broken.
//
// A chunk the anchor did not hold was removed from the engine by the replay, so
// its history gets a tombstone: without it the history keeps calling the chunk
// open, and a later replay of a newer version would bring it back.
//
// Both writes are best effort for the same reason the edit path records after
// its engine write: the document content is what the user asked for, and a
// missing row only makes the next replay less accurate.
func (s *ChunkService) recordReplayedChunkHistory(
	ctx context.Context,
	datasetID, documentID string,
	states []chunkversion.ChunkState,
	openBefore []string,
	restoreVersion int64,
) {
	if dao.DB == nil {
		return
	}
	writeCtx := context.WithoutCancel(ctx)
	// The rows are written by the replay, not by a chunk edit, so they name the
	// system actor - the same actor the tombstones below get. Who asked for the
	// restore is recorded on the dataset version row the replay closes, so the
	// timeline still names the operator; crediting the chunk rows to them would
	// claim an edit they never made on the chunk page or through the chunk API.
	createdBy := entity.ChunkVersionSystemActor

	anchor := make(map[string]chunkversion.ChunkState, len(states))
	chunkIDs := make([]string, 0, len(states))
	for _, state := range states {
		if !state.Exists {
			continue
		}
		anchor[state.ChunkID] = state
		chunkIDs = append(chunkIDs, state.ChunkID)
	}

	versionDAO := dao.NewChunkVersionDAO()
	// The rows this replay writes are anchored at the number the restore
	// occupies: they describe what the restore put back, so they have to sort
	// after the state they were replayed from and at or before the version that
	// records the restore - which is the same number, the one the caller
	// reserved. Rows at or before an anchor are what a later replay reads, so a
	// restore of this version finds its own chunk history and a restore of any
	// earlier one does not.
	//
	// A caller that brought no number has the replay take one of its own. That
	// costs a number per document, and the restore's own row then lands after
	// all of them, which is why the restore path reserves one for everyone.
	if restoreVersion <= 0 {
		reserved, reserveErr := datasetversion.ReserveVersion(writeCtx, dao.DB, datasetID)
		if reserveErr != nil {
			common.Warn("Chunk restore: failed to reserve a dataset version for the rebuilt chunk history",
				zap.String("dataset_id", datasetID),
				zap.String("document_id", documentID),
				zap.Error(reserveErr))
		}
		restoreVersion = reserved
	}
	// One reading stamps every restore row of this replay: they describe the
	// same moment, and a clock query per row would put the document's chunk count
	// in round trips on the restore path.
	var createTime *int64
	if stamp, err := entity.DBTimeMillis(dao.DB); err == nil {
		createTime = &stamp
	}
	// The lookup is batched: a rebuilt document can hold tens of thousands of
	// chunks, and one IN list that long does not fit a statement.
	const versionLookupBatch = 500
	for start := 0; start < len(chunkIDs); start += versionLookupBatch {
		end := start + versionLookupBatch
		if end > len(chunkIDs) {
			end = len(chunkIDs)
		}
		batch := chunkIDs[start:end]
		latest, err := versionDAO.LatestVersionRows(writeCtx, dao.DB, batch)
		if err != nil {
			common.Warn("Chunk restore: failed to read the latest chunk versions",
				zap.String("document_id", documentID),
				zap.Error(err))
			continue
		}
		for _, chunkID := range batch {
			state := anchor[chunkID]
			current, ok := latest[chunkID]
			// A chunk still sitting at the version the anchor read was not
			// changed by the restore, so it keeps its row - and a chunk nobody
			// ever edited keeps having no row at all.
			if !ok || current.Version <= state.Version {
				continue
			}
			// A previous attempt already recorded this restore and failed after
			// it (the counters, or the response). A second row would claim the
			// content moved again, and every retry would push the version number
			// further.
			if current.Kind == entity.ChunkVersionKindRestore && current.RestoredFrom == state.Version {
				continue
			}
			// Without a reserved number the row cannot be anchored, and 0 is the
			// baseline's number rather than "unset": writing it would place the
			// restore before every earlier version instead of after them.
			if restoreVersion <= 0 {
				continue
			}
			row := newChunkVersionRow(datasetID, documentID, chunkID, restoreVersion, current.Version+1,
				entity.ChunkVersionKindRestore, state.Version, state.Snapshot, createdBy)
			row.CreateTime = createTime
			if err := versionDAO.Create(writeCtx, dao.DB, row); err != nil {
				common.Warn("Chunk restore: failed to record the restored version",
					zap.String("chunk_id", chunkID),
					zap.Int("restored_from", state.Version),
					zap.Error(err))
			}
		}
	}

	dropped := make([]string, 0, len(openBefore))
	for _, chunkID := range openBefore {
		if _, ok := anchor[chunkID]; ok {
			continue
		}
		dropped = append(dropped, chunkID)
	}
	if len(dropped) > 0 {
		// No actor: the replay dropped these chunks because the replayed history
		// no longer contains them, not because a user removed them.
		chunkversion.AppendTombstones(writeCtx, dao.DB, datasetID, documentID, restoreVersion, dropped, "")
	}
}
