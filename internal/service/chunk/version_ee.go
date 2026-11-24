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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/engine/history"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
	"ragflow/internal/service"
	"ragflow/internal/service/chunkversion"
	"ragflow/internal/service/datasetversion"
	"ragflow/internal/storage"
	"ragflow/internal/utility"
)

const (
	// chunkVersionWriteAttempts bounds the retry loop that resolves "next
	// version" from the highest stored row. A concurrent edit of the same chunk
	// can win the unique (chunk_id, version) index between the read and the
	// insert; each attempt re-reads, so the loop converges as long as no more
	// than this many writers collide.
	chunkVersionWriteAttempts = 3

	chunkVersionTimeFormat = "2006-01-02 15:04:05"
)

// chunkVersionFullSnapshot stores the whole engine row, tokens and vector
// included, so a restore is a copy and the same snapshot drives change checks.
func chunkVersionFullSnapshot(row map[string]interface{}, meta chunkversion.SnapshotMeta) entity.JSONMap {
	if row == nil {
		return entity.JSONMap{}
	}
	return chunkversion.FullSnapshot(row, meta)
}

// sameChunkSnapshot compares two snapshots by their JSON encoding. Rows arrive
// from the document engine and from MySQL with different numeric types
// (int vs float64) for the same field, so structural equality is not a sound
// comparison; the encoded form is.
func sameChunkSnapshot(a, b entity.JSONMap) bool {
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return bytes.Equal(left, right)
}

// mergeChunkRow returns the chunk row an update produces: the previous row
// with every field of the update map written over it.
func mergeChunkRow(previous, update map[string]interface{}) map[string]interface{} {
	merged := make(map[string]interface{}, len(previous)+len(update))
	for key, value := range previous {
		merged[key] = value
	}
	for key, value := range update {
		merged[key] = value
	}
	return merged
}

// newChunkVersionRow builds one history row. kind is explicit rather than
// inferred from restoredFrom, because a restore can target version 0 - the parse
// baseline - and that row still has to read as a restore instead of as an edit.
func newChunkVersionRow(datasetID, documentID, chunkID string, datasetVersion int64, version int, kind string, restoredFrom int, snapshot entity.JSONMap, createdBy string) *entity.ChunkVersion {
	return &entity.ChunkVersion{
		ChunkID:        chunkID,
		Version:        version,
		RestoredFrom:   restoredFrom,
		CreatedBy:      createdBy,
		DatasetID:      datasetID,
		DocID:          documentID,
		DatasetVersion: datasetVersion,
		Kind:           kind,
		Snapshot:       snapshot,
	}
}

// recordChunkVersionEE snapshots the row a successful edit produced. The first
// edit seeds version 0: with the pre-edit state when the edit changed what the
// chunk is (its body, its picture, its visibility), or with the edited state
// itself when only annotations (keywords, questions, tags, positions) changed,
// in which case the chunk stays at v0. A chunk nobody edited holds no rows.
//
// The document engine write has already committed by the time this runs, so a
// failure is logged instead of returned: the chunk content is what the user
// asked to change, and retrying would only apply the edit twice.
func (s *ChunkService) recordChunkVersionEE(ctx context.Context, datasetID, documentID, chunkID string, datasetVersion int64, previous, update map[string]interface{}, meta chunkversion.SnapshotMeta) {
	if err := s.recordChunkVersion(ctx, datasetID, documentID, chunkID, datasetVersion, previous, update, meta); err != nil {
		common.Warn("Chunk update: failed to record the chunk version",
			zap.String("dataset_id", datasetID),
			zap.String("chunk_id", chunkID),
			zap.Error(err))
	}
}

func (s *ChunkService) recordChunkVersion(ctx context.Context, datasetID, documentID, chunkID string, datasetVersion int64, previous, update map[string]interface{}, meta chunkversion.SnapshotMeta) error {
	if dao.DB == nil {
		return fmt.Errorf("metadata database is not initialized")
	}
	previousSnapshot := chunkVersionFullSnapshot(previous, meta)
	nextSnapshot := chunkVersionFullSnapshot(mergeChunkRow(previous, update), meta)
	createdBy := permission.UserIDFromContext(ctx)

	var err error
	for attempt := 0; attempt < chunkVersionWriteAttempts; attempt++ {
		err = dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			_, err := appendChunkVersionTx(ctx, tx, datasetID, documentID, chunkID, datasetVersion, previousSnapshot, nextSnapshot,
				entity.ChunkVersionKindEdit, 0, createdBy)
			return err
		})
		if err == nil {
			return nil
		}
		if !isRetryableChunkVersionErr(err) {
			return err
		}
	}
	return err
}

// appendChunkVersionTx records nextSnapshot as the chunk's current state inside
// the caller's transaction, seeding version 0 when the chunk has no history
// yet. A change to the body, to the picture or to the chunk's visibility opens
// a new version; every other field is an annotation of the same content, so it
// rewrites the current version in place. The latest row must be locked (FOR UPDATE) or the
// transaction the only writer: callers either run alone or arrived through
// beginChunkEdit, which locks it first. It returns the version the chunk
// carries afterwards: the appended row's number, or the unchanged one when the
// row was updated in place or needed no write.
func appendChunkVersionTx(
	ctx context.Context,
	tx *gorm.DB,
	datasetID, documentID, chunkID string,
	datasetVersion int64,
	previousSnapshot, nextSnapshot entity.JSONMap,
	kind string,
	restoredFrom int,
	createdBy string,
) (int, error) {
	versionDAO := dao.NewChunkVersionDAO()
	latest, err := versionDAO.GetLatestForUpdate(ctx, tx, chunkID)
	if errors.Is(err, dao.ErrChunkVersionNotFound) {
		// A chunk that was never edited is at version 0, so its first edit
		// either seeds v0 with the pre-edit state and appends v1 (its body,
		// picture or visibility changed), or stores the edited annotations as
		// v0 itself.
		if chunkVersionBumps(previousSnapshot, nextSnapshot, kind) {
			if err = versionDAO.Create(ctx, tx, newChunkVersionRow(datasetID, documentID, chunkID, datasetVersion, 0, kind, restoredFrom, previousSnapshot, createdBy)); err != nil {
				return 0, err
			}
			return 1, versionDAO.Create(ctx, tx, newChunkVersionRow(datasetID, documentID, chunkID, datasetVersion, 1, kind, restoredFrom, nextSnapshot, createdBy))
		}
		return 0, versionDAO.Create(ctx, tx, newChunkVersionRow(datasetID, documentID, chunkID, datasetVersion, 0, kind, restoredFrom, nextSnapshot, createdBy))
	}
	if err != nil {
		return 0, err
	}
	if sameChunkSnapshot(latest.Snapshot, nextSnapshot) {
		return latest.Version, nil
	}
	if !chunkVersionBumps(latest.Snapshot, nextSnapshot, kind) {
		return latest.Version, versionDAO.UpdateSnapshot(ctx, tx, latest, nextSnapshot)
	}
	return latest.Version + 1, versionDAO.Create(ctx, tx, newChunkVersionRow(datasetID, documentID, chunkID, datasetVersion, latest.Version+1, kind, restoredFrom, nextSnapshot, createdBy))
}

// chunkVersionBumps reports whether an edit opens a new version. What the chunk
// is - its body, its picture, and whether it takes part in retrieval - opens
// one; keywords, questions, tags and positions are annotations of that same
// content and stay on the version they belong to. A restore is always recorded
// as a version of its own, so "restored from vN" keeps naming the action.
//
// Visibility is versioned even though it is curation: hiding a chunk is a
// decision about this content that a reader has to be able to see and undo, and
// an in-place rewrite would lose it - the state the replaced version recorded is
// gone. One version per toggle is the price of an append-only history.
func chunkVersionBumps(previous, next entity.JSONMap, kind string) bool {
	if kind == entity.ChunkVersionKindRestore {
		return true
	}
	if chunkSnapshotText(previous, "content_with_weight") != chunkSnapshotText(next, "content_with_weight") {
		return true
	}
	if chunkSnapshotText(previous, "img_id") != chunkSnapshotText(next, "img_id") {
		return true
	}
	return chunkSnapshotAvailability(previous) != chunkSnapshotAvailability(next)
}

// chunkSnapshotAvailability reads visibility: 0 hides, an absent key means 1.
func chunkSnapshotAvailability(snapshot entity.JSONMap) int {
	if value, ok := common.GetInt(snapshot["available_int"]); ok && value == 0 {
		return 0
	}
	return 1
}

// chunkSwitchMoves reports whether a bulk toggle actually moves the row the
// engine returned. A row already in the requested state opens no version and
// records no dataset version, and a row the engine did not answer cannot be
// compared at all.
func chunkSwitchMoves(existing map[string]interface{}, availableInt int) bool {
	if existing == nil {
		return false
	}
	return chunkSnapshotAvailability(existing) != availableInt
}

// chunkEditMovesDataset answers the same question for the dataset timeline, on
// the row as the engine held it and the update the caller built: an edit that
// only annotates the chunk (keywords, questions, tags, visibility) leaves the
// dataset's own history alone. It is the gate both edit routes share - the
// history-recording transaction and the deployment that keeps only the dataset
// history - so the two cannot disagree about what a dataset version is.
func chunkEditMovesDataset(previous, update map[string]interface{}) bool {
	return chunkVersionBumps(previous, mergeChunkRow(previous, update), entity.ChunkVersionKindEdit)
}

// isRetryableChunkVersionErr reports whether a failed version write may be
// retried with a fresh read: a unique (chunk_id, version) collision, or an
// InnoDB deadlock between two writers that both had to seed version 0 (their
// locking reads gap-lock the empty index and their inserts conflict).
func isRetryableChunkVersionErr(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1213
}

// chunkEditConflict answers an edit whose expected_version no longer matches the
// chunk's current version, mapped to HTTP 409 by the handlers. Both numbers are
// named because the caller has to know what it ran into: an edit issued against a
// chunk the caller has not re-read since a restore always lands here, and
// "someone else edited it" would send that caller looking for a collaborator who
// does not exist.
func chunkEditConflict(expected, current int) updateChunkError {
	return updateChunkError{
		code:    common.CodeConflict,
		message: fmt.Sprintf("chunk version changed: expected %d, current is %d; reload the chunk and retry", expected, current),
	}
}

// chunkHistoryVersion records the dataset version a batch of chunk history rows
// was produced by and returns its number.
//
// The rows carry that number as their replay anchor, so it has to be obtained
// before they are written. A version that cannot be stored is returned as an
// error: the edit that produced it fails with it, instead of writing rows no
// timeline entry can reach. A caller that gets 0 must not write the rows - 0 is
// the baseline's number, not "unset".
func chunkHistoryVersion(ctx context.Context, datasetID string, changes []entity.DatasetVersionChange) (int64, error) {
	return datasetversion.RecordContentVersionChangesOwnTx(ctx, datasetID, chunkVersionKind(changes), changes)
}

// chunkVersionKind is the dataset version kind a batch of chunk changes produces.
//
// Every writer batches one action, so the action names the kind and the timeline
// reads "Chunk edited" / "Chunk removed" / "Chunk added" / "Chunk restored" /
// "Chunk enabled" / "Chunk disabled" instead of one label for all of them. A
// restore is a content change of its own kind (DatasetVersionKindChunkRestore),
// not the document-level kind of the same name: it replays one chunk's history.
// Two cases keep the unnamed kind: a batch that mixed actions, which no single
// kind describes, and an action this switch does not know.
func chunkVersionKind(changes []entity.DatasetVersionChange) string {
	kind := ""
	for _, change := range changes {
		var current string
		switch change.Action {
		case entity.DatasetVersionChangeActionAdd:
			current = entity.DatasetVersionKindChunkAdd
		case entity.DatasetVersionChangeActionUpdate:
			current = entity.DatasetVersionKindChunkUpdate
		case entity.DatasetVersionChangeActionDelete:
			current = entity.DatasetVersionKindChunkDelete
		case entity.DatasetVersionChangeActionRestore:
			current = entity.DatasetVersionKindChunkRestore
		case entity.DatasetVersionChangeActionEnable:
			current = entity.DatasetVersionKindChunkEnable
		case entity.DatasetVersionChangeActionDisable:
			current = entity.DatasetVersionKindChunkDisable
		default:
			return entity.DatasetVersionKindChunk
		}
		if kind != "" && kind != current {
			return entity.DatasetVersionKindChunk
		}
		kind = current
	}
	if kind == "" {
		return entity.DatasetVersionKindChunk
	}
	return kind
}

// chunkVersionChangeAction names the dataset change a chunk history row records:
// a restore is its own action, every other kind is an edit.
func chunkVersionChangeAction(kind string) string {
	if kind == entity.ChunkVersionKindRestore {
		return entity.DatasetVersionChangeActionRestore
	}
	return entity.DatasetVersionChangeActionUpdate
}

// beginChunkEditEE is the shared edit hook of editions that keep a chunk
// history.
//
// Every edit takes the transactional route, with or without a lock token: the
// version and its body are written first and the transaction stays open until
// the engine write lands. That ordering is what makes a failure reportable - a
// body the engine refused rolls the version back and the chunk is never touched
// - instead of leaving an edit that succeeded with no history to show for it.
func (s *ChunkService) beginChunkEditEE(
	ctx context.Context,
	datasetID, documentID, chunkID string,
	expected *int,
	previous, update map[string]interface{},
	meta chunkversion.SnapshotMeta,
) (func(error) error, int, error) {
	// An engine that cannot hold a version body keeps no history: the edit goes
	// ahead without one, exactly as in an edition that does not record history at
	// all, instead of failing every edit on such a deployment.
	if !history.ChunkAvailable() {
		return nil, 0, nil
	}
	return s.beginChunkEdit(ctx, datasetID, documentID, chunkID, expected,
		entity.ChunkVersionKindEdit, 0, previous, update, meta)
}

// beginChunkEdit CASes the chunk's history version. Inside one transaction it
// locks the latest history row, requires it to still be the version the caller
// read, appends the state this edit produces and keeps the transaction open
// until the caller commits it after the document engine write. A writer whose
// base version was superseded therefore fails before touching the chunk, and a
// writer whose engine write fails rolls its version row back, keeping the
// "highest version describes the row the last edit left behind" invariant.
//
// A nil expected skips the check but still records the version inside the
// transaction, so a restore can name the version it came from without a second
// write path; the shared edit route uses the unlocked post-write recording
// instead. kind is what the row says it is - an edit or a restore - because a
// restore of version 0 carries restoredFrom 0, which cannot stand for "not a
// restore".
//
// An edit whose update leaves the chunk exactly as it is writes neither row: the
// returned hook commits nothing and the chunk keeps the version it had, which is
// what the returned number reports. A restore always records, because the action
// is the version.
func (s *ChunkService) beginChunkEdit(
	ctx context.Context,
	datasetID, documentID, chunkID string,
	expected *int,
	kind string,
	restoredFrom int,
	previous, update map[string]interface{},
	meta chunkversion.SnapshotMeta,
) (func(error) error, int, error) {
	if dao.DB == nil {
		return nil, 0, fmt.Errorf("metadata database is not initialized")
	}
	if expected != nil && *expected < 0 {
		return nil, 0, updateChunkError{
			code:    common.CodeArgumentError,
			message: "`expected_version` must be a non-negative integer",
		}
	}
	previousSnapshot := chunkVersionFullSnapshot(previous, meta)
	nextSnapshot := chunkVersionFullSnapshot(mergeChunkRow(previous, update), meta)
	createdBy := permission.UserIDFromContext(ctx)

	var tx *gorm.DB
	version := 0
	// hasVersionRow records whether the chunk already has a version to rewrite.
	// It cannot be read off version: a parsed chunk's baseline is version 0, so
	// "0" means both "v0 exists" and "no history at all".
	hasVersionRow := false
	rollback := func() {
		if err := tx.Rollback().Error; err != nil {
			common.Warn("Chunk edit: failed to roll back the version transaction",
				zap.String("chunk_id", chunkID), zap.Error(err))
		}
	}

	for attempt := 0; attempt < chunkVersionWriteAttempts; attempt++ {
		tx = dao.DB.WithContext(ctx).Begin()
		latest, err := dao.NewChunkVersionDAO().GetLatestForUpdate(ctx, tx, chunkID)
		switch {
		case errors.Is(err, dao.ErrChunkVersionNotFound):
			// A chunk that was never edited carries version 0; any other
			// expectation no longer describes the chunk.
			if expected != nil && *expected != 0 {
				rollback()
				return nil, 0, chunkEditConflict(*expected, 0)
			}
		case err != nil:
			rollback()
			if isRetryableChunkVersionErr(err) {
				continue
			}
			return nil, 0, err
		default:
			if expected != nil && *expected != latest.Version {
				rollback()
				return nil, 0, chunkEditConflict(*expected, latest.Version)
			}
			version = latest.Version
			hasVersionRow = true
		}
		// An edit that hands the chunk back unchanged records nothing: no chunk
		// version and no dataset version. The two are written together precisely so
		// they cannot disagree, and an identical snapshot is already what the version
		// writer treats as "nothing to write" (see appendChunkVersionTx) - the dataset
		// version, written first, was the half that still appeared and told the
		// timeline the dataset had moved when nothing had. A restore is exempt: it
		// records the action itself, which no snapshot can express.
		//
		// The engine write stays with the caller. Fields the snapshot leaves out -
		// the searchable tag list, the derived tokens, available_int when it does not
		// hide the row - can still be the point of the request, and dropping the call
		// would drop them. The returned hook has no transaction behind it and
		// succeeds, so a failed engine write is still reported by the caller.
		//
		// The version check above still ran: an edit based on a chunk that has moved
		// on is refused rather than answered with a no-op, because the caller's page
		// shows content this request never read.
		if kind != entity.ChunkVersionKindRestore && sameChunkSnapshot(previousSnapshot, nextSnapshot) {
			rollback()
			return func(error) error { return nil }, version, nil
		}
		// The dataset version is recorded here, in the same transaction, because
		// the chunk row it stamps has to carry its number. Doing it in one
		// transaction also keeps the two halves of one edit together: a failure
		// below rolls the version back, so the history never claims a change the
		// content did not make (and never leaves an edit with no version).
		//
		// Only an edit that changes what the chunk is - its body, its picture or
		// its visibility, or a restore, which records the action itself - is a
		// version. Keywords, questions, tags and positions are annotations of the
		// same content, so an edit that only touches them moves nothing on the
		// dataset's timeline: an entry there would say the dataset had changed
		// while its own change list named a chunk edit it cannot describe.
		//
		// The chunk keeps the annotation by rewriting the version it is on (see
		// appendChunkVersionTx), which the dataset version below takes no part in.
		// A chunk with no version yet has nothing to rewrite, so it records no row
		// at all: the next version seeded from the live row already carries the
		// annotation, because that row is what the seeding snapshots. Writing a row
		// here would need an anchor, and a history row cannot be anchored at "no
		// version" (see ChunkVersion.BeforeCreate).
		datasetVersion := int64(0)
		if chunkVersionBumps(previousSnapshot, nextSnapshot, kind) {
			changes := []entity.DatasetVersionChange{
				chunkVersionChange(chunkVersionChangeAction(kind), documentID, chunkID),
			}
			recorded, versionErr := datasetversion.RecordVersionWithChanges(ctx, tx, datasetID,
				chunkVersionKind(changes), createdBy, 0, nil, changes)
			if versionErr != nil {
				rollback()
				if isRetryableChunkVersionErr(versionErr) || errors.Is(versionErr, datasetversion.ErrVersionConflict) {
					continue
				}
				return nil, 0, versionErr
			}
			datasetVersion = recorded
		} else if !hasVersionRow {
			rollback()
			return func(error) error { return nil }, version, nil
		}
		version, err = appendChunkVersionTx(ctx, tx, datasetID, documentID, chunkID, datasetVersion, previousSnapshot, nextSnapshot, kind, restoredFrom, createdBy)
		if err != nil {
			rollback()
			if isRetryableChunkVersionErr(err) {
				continue
			}
			return nil, 0, err
		}
		finish := func(fail error) error {
			if fail != nil {
				rollback()
				return nil
			}
			return tx.Commit().Error
		}
		return finish, version, nil
	}
	return nil, 0, fmt.Errorf("failed to record the chunk version after %d attempts", chunkVersionWriteAttempts)
}

// appendRestoredVersion records a restore as a new version holding the
// restored snapshot, leaving the source version's row untouched so the history
// stays append-only. The returned version is the new highest one.
func (s *ChunkService) appendRestoredVersion(ctx context.Context, req *service.ChunkVersionRequest, snapshot entity.JSONMap) (int, error) {
	versionDAO := dao.NewChunkVersionDAO()
	createdBy := permission.UserIDFromContext(ctx)

	var lastErr error
	for attempt := 0; attempt < chunkVersionWriteAttempts; attempt++ {
		next := 0
		lastErr = dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// The dataset version goes in first: the restored row carries its
			// number, and a rolled-back transaction takes both with it, so the
			// history never names a version the dataset does not have.
			changes := []entity.DatasetVersionChange{
				chunkVersionChange(entity.DatasetVersionChangeActionRestore, req.DocumentID, req.ChunkID),
			}
			datasetVersion, err := datasetversion.RecordVersionWithChanges(ctx, tx, req.DatasetID,
				chunkVersionKind(changes), createdBy, 0, nil, changes)
			if err != nil {
				return err
			}
			next = 1
			latest, err := versionDAO.GetLatestForUpdate(ctx, tx, req.ChunkID)
			switch {
			case errors.Is(err, dao.ErrChunkVersionNotFound):
			case err != nil:
				return err
			default:
				next = latest.Version + 1
			}
			return versionDAO.Create(ctx, tx, newChunkVersionRow(req.DatasetID, req.DocumentID, req.ChunkID,
				datasetVersion, next, entity.ChunkVersionKindRestore, req.Version, snapshot, createdBy))
		})
		if lastErr == nil {
			return next, nil
		}
		if !isRetryableChunkVersionErr(lastErr) && !errors.Is(lastErr, datasetversion.ErrVersionConflict) {
			return 0, lastErr
		}
	}
	return 0, lastErr
}

// ListVersions returns a chunk's version history, newest first.
// annotateChunkVersionsEE stamps each listed chunk with the version it is
// currently at, so the chunk list can show "vN" beside the text. A chunk with
// no history row is at v0 — the state the parser produced — hence the map is
// consulted instead of assumed to cover every id.
func (s *ChunkService) annotateChunkVersionsEE(ctx context.Context, chunks []map[string]interface{}) {
	if len(chunks) == 0 || dao.DB == nil {
		return
	}
	ids := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		if id, _ := chunk["chunk_id"].(string); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	latest, err := dao.NewChunkVersionDAO().LatestVersions(ctx, dao.DB, ids)
	if err != nil {
		// The page itself is still valid; only the badge is missing.
		common.Warn("Chunk list: failed to load chunk versions", zap.Error(err))
		return
	}
	for _, chunk := range chunks {
		id, _ := chunk["chunk_id"].(string)
		if id == "" {
			continue
		}
		chunk["content_version"] = latest[id]
	}
}

func (s *ChunkService) ListVersions(ctx context.Context, req *service.ChunkVersionRequest) (*service.ListChunkVersionsResponse, error) {
	doc, _, err := s.versionTarget(ctx, req, permission.ResourcePermissionRead, permission.ActionRead)
	if err != nil {
		return nil, err
	}

	rows, err := dao.NewChunkVersionDAO().ListByChunk(ctx, dao.DB, req.ChunkID)
	if err != nil {
		return nil, fmt.Errorf("failed to list chunk versions: %w", err)
	}

	// Rows are newest first and (chunk_id, version) is the key, so the first row
	// holds the highest version of this chunk. Latest is decided on that version,
	// the same way the detail view and the list badge decide it - one chunk id has
	// one version sequence, so all three have to agree on which row is current.
	// The document/dataset filter below only drops rows that do not belong here;
	// it is a guard, not a second scope for the version numbers.
	latest := -1
	if len(rows) > 0 {
		latest = rows[0].Version
	}
	versions := make([]service.ChunkVersionSummary, 0, len(rows))
	kept := make([]*entity.ChunkVersion, 0, len(rows))
	for _, row := range rows {
		if row.DocID != doc.ID || row.DatasetID != doc.KbID {
			continue
		}
		kept = append(kept, row)
		versions = append(versions, service.ChunkVersionSummary{
			Version:        row.Version,
			CreatedBy:      row.CreatedBy,
			RestoredFrom:   row.RestoredFrom,
			CreateTime:     chunkVersionCreatedAt(row.CreateTime),
			CreateDate:     formatChunkVersionTime(row.CreateTime),
			Latest:         row.Version == latest,
			Kind:           row.Kind,
			DatasetVersion: row.DatasetVersion,
		})
	}

	resolveChunkVersionActors(ctx, versions)
	s.applyChunkVersionFacts(ctx, doc.KbID, kept, versions)

	return &service.ListChunkVersionsResponse{
		ChunkID:  req.ChunkID,
		Total:    len(versions),
		Versions: versions,
	}, nil
}

// chunkVersionOrigin says where a history row came from. The actor answers most
// rows; a row the system wrote is a parse baseline, a replay, or a removal. The
// last one takes the dataset version's kind: a re-parse and a dataset restore
// both drop chunks with no actor, and only that version says which did it.
func chunkVersionOrigin(kind, createdBy, datasetVersionKind string) string {
	switch kind {
	case entity.ChunkVersionKindParse:
		return service.ChunkVersionOriginParse
	case entity.ChunkVersionKindAdd, entity.ChunkVersionKindEdit:
		// A parse baseline is kind "parse", so an add or an edit is the page
		// acting on the chunk, including the enable switch.
		return service.ChunkVersionOriginChunk
	case entity.ChunkVersionKindRestore:
		if chunkVersionHasSystemActor(createdBy) {
			// Only a dataset restore's replay restores without a user.
			return service.ChunkVersionOriginRestore
		}
		return service.ChunkVersionOriginChunk
	case entity.ChunkVersionKindDelete:
		if !chunkVersionHasSystemActor(createdBy) {
			return service.ChunkVersionOriginChunk
		}
		switch datasetVersionKind {
		case entity.DatasetVersionKindRestore:
			return service.ChunkVersionOriginRestore
		case "":
			// The version is gone, so the history cannot tell rather than guess.
			return ""
		default:
			// A parse, or the cleanup of a failed run: the pipeline dropped it.
			return service.ChunkVersionOriginParse
		}
	}
	return ""
}

// chunkVersionHasSystemActor reports a row with no user on it: an empty actor and
// the "system" marker mean the same thing.
func chunkVersionHasSystemActor(createdBy string) bool {
	actor := strings.TrimSpace(createdBy)
	return actor == "" || actor == entity.ChunkVersionSystemActor
}

// chunkVersionTrigger names the user whose request produced the row: the row's own
// actor when a person wrote it, otherwise whoever triggered the dataset version it
// was anchored at - the answer a history list means by "who changed this".
func chunkVersionTrigger(row *entity.ChunkVersion, fact dao.DatasetVersionFact) string {
	if row == nil {
		return ""
	}
	if !chunkVersionHasSystemActor(row.CreatedBy) {
		return strings.TrimSpace(row.CreatedBy)
	}
	return strings.TrimSpace(fact.CreatedBy)
}

// applyChunkVersionFacts fills the two fields a row cannot answer alone: Origin
// and TriggeredBy. A history of edits costs no query - only a system-written row
// needs its dataset version.
func (s *ChunkService) applyChunkVersionFacts(ctx context.Context, datasetID string, rows []*entity.ChunkVersion, versions []service.ChunkVersionSummary) {
	if len(rows) == 0 || len(rows) != len(versions) {
		return
	}
	facts := chunkVersionDatasetFacts(ctx, datasetID, rows)
	triggers := make([]string, 0, len(versions))
	for i, row := range rows {
		fact := facts[row.DatasetVersion]
		versions[i].Origin = chunkVersionOrigin(row.Kind, row.CreatedBy, fact.Kind)
		versions[i].TriggeredBy = chunkVersionTrigger(row, fact)
		triggers = append(triggers, versions[i].TriggeredBy)
	}
	// The list names the person, so the id is resolved once for all rows.
	names := chunkVersionActorNames(ctx, triggers)
	for i := range versions {
		if name := strings.TrimSpace(names[versions[i].TriggeredBy]); name != "" {
			versions[i].TriggeredByName = name
		}
	}
}

// chunkVersionDatasetFacts reads the dataset version behind every system-written
// row, whose actor and origin both come from the version it was anchored at.
func chunkVersionDatasetFacts(ctx context.Context, datasetID string, rows []*entity.ChunkVersion) map[int64]dao.DatasetVersionFact {
	if dao.DB == nil {
		return nil
	}
	needed := make([]int64, 0, len(rows))
	seen := make(map[int64]struct{}, len(rows))
	for _, row := range rows {
		if !chunkVersionHasSystemActor(row.CreatedBy) {
			continue
		}
		if _, ok := seen[row.DatasetVersion]; ok {
			continue
		}
		seen[row.DatasetVersion] = struct{}{}
		needed = append(needed, row.DatasetVersion)
	}
	if len(needed) == 0 {
		return nil
	}
	facts, err := dao.NewDatasetVersionDAO().FactsByVersion(ctx, dao.DB, datasetID, needed)
	if err != nil {
		// The reader still gets the kind and the actor; only these two are lost.
		common.Warn("Chunk history: failed to read the dataset versions behind system rows",
			zap.String("dataset_id", datasetID),
			zap.Error(err))
		return nil
	}
	return facts
}

// chunkVersionActorNames resolves actor ids to nicknames in one batched lookup.
// The history stores the id; naming the actor is a display concern, so it is
// resolved at read time and a best-effort failure just leaves the ids in place.
func chunkVersionActorNames(ctx context.Context, actors []string) map[string]string {
	if dao.DB == nil || len(actors) == 0 {
		return nil
	}
	ids := make([]string, 0, len(actors))
	seen := make(map[string]struct{}, len(actors))
	for _, actor := range actors {
		actor = strings.TrimSpace(actor)
		if actor == "" {
			continue
		}
		if _, ok := seen[actor]; ok {
			continue
		}
		seen[actor] = struct{}{}
		ids = append(ids, actor)
	}
	if len(ids) == 0 {
		return nil
	}
	names, err := dao.NewUserDAO().GetNicknamesByIDs(ctx, dao.DB, ids)
	if err != nil {
		common.Warn("Failed to resolve chunk version actors", zap.Error(err))
		return nil
	}
	return names
}

// resolveChunkVersionActors fills CreatedByName in place for the versions that
// name a user. A system-written row (the parse baseline, a tombstone) and an
// actor whose user no longer exists stay empty, so the client falls back to the
// raw id instead of showing a blank actor.
func resolveChunkVersionActors(ctx context.Context, versions []service.ChunkVersionSummary) {
	if len(versions) == 0 {
		return
	}
	actors := make([]string, 0, len(versions))
	for _, version := range versions {
		actors = append(actors, version.CreatedBy)
	}
	names := chunkVersionActorNames(ctx, actors)
	if len(names) == 0 {
		return
	}
	for i := range versions {
		if name := strings.TrimSpace(names[versions[i].CreatedBy]); name != "" {
			versions[i].CreatedByName = name
		}
	}
}

// GetVersion returns one version with the field set it stored.
func (s *ChunkService) GetVersion(ctx context.Context, req *service.ChunkVersionRequest) (*service.GetChunkVersionResponse, error) {
	doc, _, err := s.versionTarget(ctx, req, permission.ResourcePermissionRead, permission.ActionRead)
	if err != nil {
		return nil, err
	}

	versionDAO := dao.NewChunkVersionDAO()
	row, err := versionDAO.GetByChunkAndVersion(ctx, dao.DB, req.ChunkID, req.Version)
	if err != nil {
		if errors.Is(err, dao.ErrChunkVersionNotFound) {
			return nil, fmt.Errorf("version not found")
		}
		return nil, fmt.Errorf("failed to get chunk version: %w", err)
	}
	if row.DocID != doc.ID || row.DatasetID != doc.KbID {
		return nil, fmt.Errorf("version not found")
	}

	// The detail answers with the same Latest the list shows, so it has to read the
	// same highest version. A missing row means a concurrent prune removed the
	// history between the two reads, which leaves this version as the last known
	// state; anything else is a real failure and is not swallowed.
	latest := row.Version
	if highest, err := versionDAO.GetLatest(ctx, dao.DB, req.ChunkID); err == nil {
		latest = highest.Version
	} else if !errors.Is(err, dao.ErrChunkVersionNotFound) {
		return nil, fmt.Errorf("failed to get the latest chunk version: %w", err)
	}

	response := &service.GetChunkVersionResponse{
		Version:        row.Version,
		CreatedBy:      row.CreatedBy,
		RestoredFrom:   row.RestoredFrom,
		CreateTime:     chunkVersionCreatedAt(row.CreateTime),
		CreateDate:     formatChunkVersionTime(row.CreateTime),
		Latest:         row.Version == latest,
		Kind:           row.Kind,
		DatasetVersion: row.DatasetVersion,
		// The client sees what the version said, not how it was indexed: a full
		// snapshot carries the token columns, the vector and the metadata that
		// names its producer, and none of those are the panel's business.
		Chunk: map[string]interface{}(chunkversion.ClientSnapshot(row.Snapshot)),
	}
	actors := []service.ChunkVersionSummary{{CreatedBy: row.CreatedBy}}
	resolveChunkVersionActors(ctx, actors)
	response.CreatedByName = actors[0].CreatedByName
	// The detail reports the origin and the trigger as well: the preview is where a
	// reader asks whether they made this change or a dataset restore did.
	facts := chunkVersionDatasetFacts(ctx, doc.KbID, []*entity.ChunkVersion{row})
	fact := facts[row.DatasetVersion]
	response.Origin = chunkVersionOrigin(row.Kind, row.CreatedBy, fact.Kind)
	response.TriggeredBy = chunkVersionTrigger(row, fact)
	if names := chunkVersionActorNames(ctx, []string{response.TriggeredBy}); len(names) > 0 {
		response.TriggeredByName = names[response.TriggeredBy]
	}
	return response, nil
}

// chunkVersionError carries the HTTP-facing code of a refused history operation,
// so the handler can answer 404/409 instead of a blanket 500.
type chunkVersionError struct {
	code    common.ErrorCode
	message string
}

func (e chunkVersionError) Error() string { return e.message }

func (e chunkVersionError) Code() common.ErrorCode { return e.code }

// chunkVersionDeleteRefusal names the rows a chunk's history cannot lose. The
// latest row is the state the live chunk carries; a parse row is the baseline a
// replay rebuilds from; a delete row is what tells a replay the chunk was gone
// from that point on. Everything else (edits and restores) is only history.
func chunkVersionDeleteRefusal(row *entity.ChunkVersion, latest int) error {
	if row.Version == latest {
		return chunkVersionError{
			code:    common.CodeConflict,
			message: fmt.Sprintf("version %d is the current version and cannot be deleted", row.Version),
		}
	}
	switch row.Kind {
	case entity.ChunkVersionKindParse:
		return chunkVersionError{
			code:    common.CodeConflict,
			message: fmt.Sprintf("version %d is the parse baseline of this chunk and cannot be deleted", row.Version),
		}
	case entity.ChunkVersionKindAdd:
		return chunkVersionError{
			code:    common.CodeConflict,
			message: fmt.Sprintf("version %d is where this chunk was created and cannot be deleted", row.Version),
		}
	case entity.ChunkVersionKindDelete:
		return chunkVersionError{
			code:    common.CodeConflict,
			message: fmt.Sprintf("version %d marks when the chunk was removed and cannot be deleted", row.Version),
		}
	}
	return nil
}

// refuseAnchoredVersionDelete refuses to drop a history row that a dataset
// version still restores from.
//
// A dataset version's number is the anchor a content restore replays the chunk
// history to, and the row that applies at an anchor is the newest one carrying a
// number at or below it. Dropping a row in the middle of the timeline therefore
// rewrites what an existing version restores: the anchor falls back to the state
// before it, and both the text the version panel shows and the
// document a rebuild produces silently become the older one. The row is refused
// instead of rewriting a version that is already recorded; a row that no anchor
// reaches - its whole window falls after the last version, or before the first -
// is still deletable.
func refuseAnchoredVersionDelete(ctx context.Context, db *gorm.DB, datasetID string, row *entity.ChunkVersion) error {
	if row == nil || datasetID == "" {
		return nil
	}
	next, err := dao.NewChunkVersionDAO().NextDatasetVersionAfter(ctx, db, row.ChunkID, row.Version)
	if err != nil {
		return fmt.Errorf("failed to read the next version of chunk %s: %w", row.ChunkID, err)
	}
	// The window this row describes is [its dataset version, the next row's). Two
	// rows written by the same change share a number, which leaves no version
	// between them: an anchor at that number is answered by the later sibling, so
	// this row can go. That is the case the old millisecond window had to refuse
	// (two writes cannot be told apart by a clock) and the version number settles.
	if next != nil && *next <= row.DatasetVersion {
		return nil
	}
	anchor, err := dao.NewDatasetVersionDAO().FirstVersionBetween(ctx, db, datasetID, row.DatasetVersion, next)
	if err != nil {
		if errors.Is(err, dao.ErrDatasetVersionNotFound) {
			return nil
		}
		return fmt.Errorf("failed to read the dataset versions anchored on chunk version %d: %w", row.Version, err)
	}
	return chunkVersionError{
		code: common.CodeConflict,
		message: fmt.Sprintf(
			"version %d cannot be deleted: restoring dataset version %d requires it", row.Version, anchor),
	}
}

// DeleteVersion drops one version from a chunk's history.
//
// Deleting history never rewrites the chunk: the row goes and the remaining
// numbers keep their gaps, so a later version never has to be renumbered. The
// rows the history depends on - and the ones a dataset version still replays -
// are refused by chunkVersionDeleteRefusal and refuseAnchoredVersionDelete.
func (s *ChunkService) DeleteVersion(ctx context.Context, req *service.ChunkVersionRequest) (*service.DeleteChunkVersionResponse, error) {
	if req == nil || req.ChunkID == "" {
		return nil, chunkVersionError{code: common.CodeArgumentError, message: "chunk_id is required"}
	}
	doc, _, err := s.versionTarget(ctx, req, permission.ResourcePermissionManage, permission.ActionWrite)
	if err != nil {
		return nil, err
	}

	versionDAO := dao.NewChunkVersionDAO()
	// Fail fast on an unknown version before taking the history lock; the check is
	// repeated inside the transaction, where it decides the delete.
	if _, err := versionDAO.GetByChunkAndVersion(ctx, dao.DB, req.ChunkID, req.Version); err != nil {
		if errors.Is(err, dao.ErrChunkVersionNotFound) {
			return nil, chunkVersionError{code: common.CodeNotFound, message: "version not found"}
		}
		return nil, fmt.Errorf("failed to get chunk version: %w", err)
	}

	var latestVersion int
	// The picture the deleted version names, read before the row goes: after the
	// commit there is nothing left to ask which image it pointed at.
	var deletedImageID string
	err = dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// The same lock an edit or a restore takes, so a delete cannot race a new
		// version into existence and then remove the row that version superseded.
		latest, lockErr := versionDAO.GetLatestForUpdate(ctx, tx, req.ChunkID)
		if lockErr != nil {
			if errors.Is(lockErr, dao.ErrChunkVersionNotFound) {
				return chunkVersionError{code: common.CodeNotFound, message: "version not found"}
			}
			return fmt.Errorf("failed to load the latest chunk version: %w", lockErr)
		}
		latestVersion = latest.Version

		row, loadErr := versionDAO.GetByChunkAndVersion(ctx, tx, req.ChunkID, req.Version)
		if loadErr != nil {
			if errors.Is(loadErr, dao.ErrChunkVersionNotFound) {
				return chunkVersionError{code: common.CodeNotFound, message: "version not found"}
			}
			return fmt.Errorf("failed to get chunk version: %w", loadErr)
		}
		// The history is scoped to the document and dataset in the path, so a
		// chunk id read through the wrong dataset resolves to nothing.
		if row.DocID != doc.ID || row.DatasetID != doc.KbID {
			return chunkVersionError{code: common.CodeNotFound, message: "version not found"}
		}
		if refusal := chunkVersionDeleteRefusal(row, latest.Version); refusal != nil {
			return refusal
		}
		if anchored := refuseAnchoredVersionDelete(ctx, tx, doc.KbID, row); anchored != nil {
			return anchored
		}
		if imageID, ok := row.Snapshot["img_id"].(string); ok {
			deletedImageID = imageID
		}
		if _, deleteErr := versionDAO.DeleteByChunkAndVersion(ctx, tx, req.ChunkID, req.Version); deleteErr != nil {
			return fmt.Errorf("failed to delete chunk version: %w", deleteErr)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// The row is gone, so the body it named can never be read again: drop it now
	// instead of leaving one orphan document per deleted version in the engine.
	// The delete already committed, so a failed removal is logged rather than
	// returned - the caller cannot retry against a row that no longer exists, and
	// the body is unreachable either way.
	if history.ChunkAvailable() {
		bodyID := history.ChunkDocumentID(req.ChunkID, req.Version)
		if _, purgeErr := history.DeleteChunkDocuments(ctx, doc.KbID, []string{bodyID}); purgeErr != nil {
			common.Warn("Chunk history: failed to remove the body of a deleted version",
				zap.String("chunk_id", req.ChunkID),
				zap.Int("version", req.Version),
				zap.Error(purgeErr))
		}
	}

	// The picture this version alone pointed at has no reader left. The object
	// key is "<chunk id>-<content hash>", so only rows of this chunk can name the
	// same object; the check runs after the row is gone so the read cannot count
	// the version being dropped.
	if deletedImageID != "" {
		s.reclaimChunkImage(ctx, doc.KbID, req.ChunkID, deletedImageID)
	}

	return &service.DeleteChunkVersionResponse{
		ChunkID:        req.ChunkID,
		DeletedVersion: req.Version,
		LatestVersion:  latestVersion,
	}, nil
}

// reclaimChunkImage drops the picture object of a deleted version when no
// remaining row of the same chunk names it.
//
// The search is one chunk wide and no wider: an object's key is
// "<chunk id>-<content hash>", so a picture is reachable only from that chunk's
// rows - the live chunk, which the highest version mirrors, or one of its
// versions. The object survives while any of them names it and goes when the
// last one does; an append that stacks a new picture onto the old one writes a
// new key and leaves the old object to the versions that still name it. A
// reference set that cannot be read keeps the object - a failed read is not
// evidence that nobody needs it - and a failed removal only logs, because the
// row is already gone and retrying the delete cannot find it.
func (s *ChunkService) reclaimChunkImage(ctx context.Context, datasetID, chunkID, imageID string) {
	storageImpl := storage.GetStorageFactory().GetStorage()
	if storageImpl == nil || chunkID == "" {
		return
	}
	objectKey := chunkversion.ImageObjectName(datasetID, imageID)
	if objectKey == "" {
		return
	}
	referenced, err := dao.NewChunkVersionDAO().ListImageIDs(ctx, dao.DB, "", []string{chunkID}, false)
	if err != nil {
		common.Warn("Chunk history: kept the image of a deleted version, its references could not be read",
			zap.String("image_id", imageID), zap.Error(err))
		return
	}
	for _, id := range referenced {
		if id == imageID {
			return
		}
	}
	if removeErr := storageImpl.Remove(ctx, datasetID, objectKey); removeErr != nil {
		common.Warn("Chunk history: failed to remove the image of a deleted version",
			zap.String("image_id", imageID), zap.Error(removeErr))
	}
}

// The versioned fields grouped by how their difference is rendered: text
// fields get a unified diff, list fields an element-level difference, and scalar
// fields before/after values.
//
// position_int is versioned and restored like the rest, but it is not compared:
// it describes where the chunk sits in the source file, which an edit does not
// move, so a "changed" row for it would be a coordinate dump nobody can act on.
var (
	chunkVersionTextFields = []string{"content_with_weight"}
	// tag_feas is the tag list a chunk actually carries: its keys are the tag
	// names. tag_kwd is versioned as well - an edit writes it and a restore has to
	// put it back - but it is not compared here: the two travel together on every
	// write that names tags, so a row for each would state the same change twice.
	chunkVersionListFields   = []string{"important_kwd", "question_kwd", "tag_feas"}
	chunkVersionScalarFields = []string{"doc_type_kwd", "img_id"}
)

// DiffVersions compares two stored versions of a chunk and reports each
// versioned field that changed. Both sides are read from the history table, so
// the comparison never needs the document engine: the stored snapshots hold the
// whole text a diff needs. A nil To compares From against the latest version.
func (s *ChunkService) DiffVersions(ctx context.Context, req *service.ChunkVersionDiffRequest) (*service.ChunkVersionDiffResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("chunk_id is required")
	}
	doc, _, err := s.versionTarget(ctx, &service.ChunkVersionRequest{
		DatasetID:  req.DatasetID,
		DocumentID: req.DocumentID,
		ChunkID:    req.ChunkID,
	}, permission.ResourcePermissionRead, permission.ActionRead)
	if err != nil {
		return nil, err
	}

	versionDAO := dao.NewChunkVersionDAO()
	fromRow, err := versionDAO.GetByChunkAndVersion(ctx, dao.DB, req.ChunkID, req.From)
	if err != nil {
		if errors.Is(err, dao.ErrChunkVersionNotFound) {
			return nil, chunkVersionNotFound(req.From)
		}
		return nil, fmt.Errorf("failed to get chunk version: %w", err)
	}
	if fromRow.DocID != doc.ID || fromRow.DatasetID != doc.KbID {
		return nil, chunkVersionNotFound(req.From)
	}

	toVersion := req.From
	toRow := fromRow
	if req.To == nil {
		latest, err := versionDAO.GetLatest(ctx, dao.DB, req.ChunkID)
		if err != nil {
			if errors.Is(err, dao.ErrChunkVersionNotFound) {
				return nil, chunkVersionNotFound(req.From)
			}
			return nil, fmt.Errorf("failed to get the latest chunk version: %w", err)
		}
		toVersion = latest.Version
	} else {
		toVersion = *req.To
	}
	if toVersion != req.From {
		toRow, err = versionDAO.GetByChunkAndVersion(ctx, dao.DB, req.ChunkID, toVersion)
		if err != nil {
			if errors.Is(err, dao.ErrChunkVersionNotFound) {
				return nil, chunkVersionNotFound(toVersion)
			}
			return nil, fmt.Errorf("failed to get chunk version: %w", err)
		}
		if toRow.DocID != doc.ID || toRow.DatasetID != doc.KbID {
			return nil, chunkVersionNotFound(toVersion)
		}
	}

	fields, identical := chunkSnapshotDiff(fromRow.Snapshot, toRow.Snapshot)
	return &service.ChunkVersionDiffResponse{
		ChunkID:   req.ChunkID,
		From:      req.From,
		To:        toVersion,
		Identical: identical,
		Fields:    fields,
	}, nil
}

// VersionImage returns the picture a stored version recorded. Its bytes are
// content-addressed and survive a replace or a removal, but the live index no
// longer references an old picture, so the regular document image endpoint
// cannot authorize it: the version row is the authority here.
func (s *ChunkService) VersionImage(ctx context.Context, req *service.ChunkVersionRequest) ([]byte, error) {
	if req == nil {
		return nil, fmt.Errorf("chunk_id is required")
	}
	doc, _, err := s.versionTarget(ctx, req, permission.ResourcePermissionRead, permission.ActionRead)
	if err != nil {
		return nil, err
	}

	row, err := dao.NewChunkVersionDAO().GetByChunkAndVersion(ctx, dao.DB, req.ChunkID, req.Version)
	if err != nil {
		if errors.Is(err, dao.ErrChunkVersionNotFound) {
			return nil, chunkVersionNotFound(req.Version)
		}
		return nil, fmt.Errorf("failed to get chunk version: %w", err)
	}
	if row.DocID != doc.ID || row.DatasetID != doc.KbID {
		return nil, chunkVersionNotFound(req.Version)
	}

	imageID, _ := row.Snapshot["img_id"].(string)
	if imageID == "" {
		return nil, service.ErrChunkVersionImageNotFound
	}
	// An image ID is "<bucket>-<object key>" and the bucket is the dataset the
	// picture was written for, so a row naming another one is corrupt (or
	// hand-edited) and its bytes are not read. The prefix is matched the same
	// way the writer and the delete sweep match it, because a dataset id may
	// itself contain a dash.
	objectKey := chunkversion.ImageObjectName(doc.KbID, imageID)
	if objectKey == "" || objectKey == imageID {
		return nil, service.ErrChunkVersionImageNotFound
	}

	storageImpl := storage.GetStorageFactory().GetStorage()
	if storageImpl == nil {
		return nil, fmt.Errorf("storage not initialized")
	}
	data, err := storageImpl.Get(ctx, doc.KbID, objectKey)
	if err != nil || len(data) == 0 {
		return nil, service.ErrChunkVersionImageNotFound
	}
	return data, nil
}

// chunkVersionNotFound reports a missing version the way an operator can act
// on: a chunk that was never edited has no history row, and a re-parse drops
// the history it had. It still matches service.ErrChunkVersionNotFound through
// errors.Is, so the handler keeps answering 404.
func chunkVersionNotFound(version int) error {
	return fmt.Errorf(
		"%w: version %d does not exist; the chunk may never have been edited, or a re-parse dropped its history",
		service.ErrChunkVersionNotFound, version,
	)
}

// chunkSnapshotDiff reports the versioned fields that differ between two
// snapshots, in a deterministic order (text, lists, scalars), and whether any
// field actually changed.
//
// List fields are reported even when both versions kept the same values: the
// comparison exists to show what each version holds, so a keyword list that
// survived is context, not noise. Text and scalar fields are only reported when
// they changed, because they are either long or unreadable. position_int is not
// compared at all - see the field groups above.
//
// Visibility is compared as visible/hidden (1/0) rather than by the stored key:
// a version that recorded none was visible, so "absent" against "1" is not a
// change, while enabling or disabling a chunk is one a reader can see.
func chunkSnapshotDiff(from, to entity.JSONMap) ([]service.ChunkVersionFieldDiff, bool) {
	fields := make([]service.ChunkVersionFieldDiff, 0, 8)
	identical := true

	for _, name := range chunkVersionTextFields {
		oldText, newText := chunkSnapshotText(from, name), chunkSnapshotText(to, name)
		if diff := utility.UnifiedDiff(oldText, newText); diff != "" {
			identical = false
			fields = append(fields, service.ChunkVersionFieldDiff{
				Field:   name,
				Kind:    service.ChunkVersionDiffText,
				Changed: true,
				Diff:    diff,
			})
		}
	}

	for _, name := range chunkVersionListFields {
		added, removed, unchanged := diffChunkStringLists(chunkVersionListValues(from, name), chunkVersionListValues(to, name))
		changed := len(added) > 0 || len(removed) > 0
		if !changed && len(unchanged) == 0 {
			continue
		}
		if changed {
			identical = false
		}
		fields = append(fields, service.ChunkVersionFieldDiff{
			Field:     name,
			Kind:      service.ChunkVersionDiffList,
			Changed:   changed,
			Added:     added,
			Removed:   removed,
			Unchanged: unchanged,
		})
	}

	// Visibility is compared as the state it describes (1 visible, 0 hidden)
	// rather than as the stored key: a version that recorded none was visible,
	// so "absent" against "1" is the same state and not a change.
	if before, after := chunkSnapshotAvailability(from), chunkSnapshotAvailability(to); before != after {
		identical = false
		fields = append(fields, service.ChunkVersionFieldDiff{
			Field:   "available_int",
			Kind:    service.ChunkVersionDiffScalar,
			Changed: true,
			Before:  before,
			After:   after,
		})
	}

	for _, name := range chunkVersionScalarFields {
		// img_id only describes a picture chunk; text chunks never carry one.
		if name == "img_id" && !chunkVersionHoldsImage(from) && !chunkVersionHoldsImage(to) {
			continue
		}
		before, after := from[name], to[name]
		if sameChunkVersionValue(before, after) {
			continue
		}
		identical = false
		fields = append(fields, service.ChunkVersionFieldDiff{
			Field:   name,
			Kind:    service.ChunkVersionDiffScalar,
			Changed: true,
			Before:  before,
			After:   after,
		})
	}

	return fields, identical
}

// chunkVersionHoldsImage reports whether a version describes a picture chunk.
func chunkVersionHoldsImage(snapshot entity.JSONMap) bool {
	kind, _ := snapshot["doc_type_kwd"].(string)
	return kind == "image"
}

// chunkSnapshotText reads a text field the way a snapshot records it:
// content_with_weight first, then the parser's content key.
func chunkSnapshotText(snapshot entity.JSONMap, name string) string {
	if text, ok := snapshot[name].(string); ok {
		return text
	}
	if name == "content_with_weight" {
		if text, ok := snapshot["content"].(string); ok {
			return text
		}
	}
	return ""
}

// chunkVersionListValues returns the elements a list field holds in a snapshot.
// tag_feas is a weight map rather than a list, so its keys are the tag names
// the comparison works with; every other list field is already a string list.
func chunkVersionListValues(snapshot entity.JSONMap, name string) []string {
	if name == "tag_feas" {
		return chunkTagNames(snapshot[name])
	}
	return chunkStringSlice(snapshot[name])
}

// chunkTagNames lists the tags a tag_feas map holds. The map reaches this code
// as JSON from MySQL or as an engine value (map[string]float64 or the extractor's
// ordered weights), so it is normalized through JSON instead of type switched.
func chunkTagNames(value interface{}) []string {
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	weights := map[string]float64{}
	if err := json.Unmarshal(raw, &weights); err != nil {
		return nil
	}
	names := make([]string, 0, len(weights))
	for name := range weights {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// diffChunkStringLists returns the values added to, removed from and kept in a
// list field. The kept values are reported alongside the changes so the caller
// can render the whole list with the changes marked, instead of only the
// deletions. The comparison is multiset-aware so a duplicate that survives is
// not reported as a change, and each slice keeps the order its values first
// appear in (removed in the old list, added and kept in the new one).
func diffChunkStringLists(oldValues, newValues []string) (added, removed, unchanged []string) {
	oldCount := make(map[string]int, len(oldValues))
	for _, value := range oldValues {
		oldCount[value]++
	}
	newCount := make(map[string]int, len(newValues))
	for _, value := range newValues {
		newCount[value]++
	}

	seen := make(map[string]bool, len(newValues))
	for _, value := range newValues {
		if newCount[value] > oldCount[value] && !seen[value] {
			seen[value] = true
			added = append(added, value)
		}
	}
	seen = make(map[string]bool, len(oldValues))
	for _, value := range oldValues {
		if oldCount[value] > newCount[value] && !seen[value] {
			seen[value] = true
			removed = append(removed, value)
		}
	}
	seen = make(map[string]bool, len(newValues))
	for _, value := range newValues {
		if oldCount[value] > 0 && !seen[value] {
			seen[value] = true
			unchanged = append(unchanged, value)
		}
	}
	return added, removed, unchanged
}

// sameChunkVersionValue compares two snapshot values structurally. Rows reach
// this comparison from MySQL JSON, where the same number can arrive as int or
// float64, so the rendered form is compared instead of the dynamic type.
func sameChunkVersionValue(a, b interface{}) bool {
	return fmt.Sprint(a) == fmt.Sprint(b)
}

// RestoreVersion writes a stored version back onto the live chunk and records
// the restore as a new version. The chunk id is stable across edits, so the
// paragraph markers, citations and any compiled structure referencing it
// survive; only the field set the version holds is rewritten.
func (s *ChunkService) RestoreVersion(ctx context.Context, req *service.ChunkVersionRequest) (*service.RestoreChunkVersionResponse, error) {
	if s.docEngine == nil {
		return nil, fmt.Errorf("doc engine not initialized")
	}
	doc, kb, err := s.versionTarget(ctx, req, permission.ResourcePermissionManage, permission.ActionWrite)
	if err != nil {
		return nil, err
	}

	versionDAO := dao.NewChunkVersionDAO()
	row, err := versionDAO.GetByChunkAndVersion(ctx, dao.DB, req.ChunkID, req.Version)
	if err != nil {
		if errors.Is(err, dao.ErrChunkVersionNotFound) {
			return nil, fmt.Errorf("version not found")
		}
		return nil, fmt.Errorf("failed to get chunk version: %w", err)
	}
	if row.DocID != doc.ID || row.DatasetID != doc.KbID {
		return nil, fmt.Errorf("version not found")
	}

	indexName := fmt.Sprintf("ragflow_%s", kb.TenantID)
	existingRow, err := s.docEngine.GetChunk(ctx, indexName, req.ChunkID, []string{req.DatasetID})
	if err != nil {
		return nil, fmt.Errorf("failed to get existing chunk: %w", err)
	}
	existing, ok := existingRow.(map[string]interface{})
	if !ok || existing == nil {
		// The chunk is no longer in the index: a later parse did not produce it,
		// so its history survives but the row to rewind does not. Restoring the
		// document version that held it is what brings it back.
		return nil, fmt.Errorf("chunk not found: a later parse dropped it; restore the document version that held it instead")
	}
	if documentID, _ := existing["doc_id"].(string); documentID != req.DocumentID {
		return nil, fmt.Errorf("chunk not found")
	}

	language := datasetLanguageOf(kb)
	meta := chunkSnapshotMeta(kb, language)
	// A full snapshot carries the row it describes, derived fields included, so
	// putting it back is a copy: no tokenizer run and no embedding round-trip,
	// and the restored chunk ranks exactly as the version did. A version recorded
	// before snapshots held their derived fields cannot be restored without
	// producing them again, and says so instead of returning half a row.
	restore, err := chunkRestoreRow(row.Snapshot)
	if err != nil {
		return nil, err
	}
	// A document is enabled or disabled as a whole, and its chunks follow it: a
	// version that recorded the chunk as visible must not put it back into
	// retrieval while the document it belongs to is switched off. The chunk's own
	// switch is only half of the gate.
	if doc.Status != nil && *doc.Status != string(entity.StatusValid) {
		restore["available_int"] = 0
	}
	// Coordinates are the source region the chunk was parsed from, not part of
	// what an edit or a rewind rewrites. A version recorded without them leaves
	// the live ones alone - they still describe the same document region - and a
	// version that carries them puts its own back through chunkRestoreRow.
	restore["id"] = req.ChunkID

	var newVersion int
	if req.ExpectedVersion != nil {
		// The caller pins the version it read, so the restore CASes the
		// history like an edit does: a stale base version fails before the
		// chunk is touched, and the restored row commits only after the
		// engine write lands.
		finish, version, err := s.beginChunkEdit(ctx, req.DatasetID, req.DocumentID, req.ChunkID, req.ExpectedVersion,
			entity.ChunkVersionKindRestore, req.Version, existing, restore, meta)
		if err != nil {
			return nil, err
		}
		if err = s.docEngine.UpdateChunks(ctx, map[string]interface{}{
			"id":     req.ChunkID,
			"doc_id": req.DocumentID,
		}, restore, indexName, req.DatasetID); err != nil {
			_ = finish(err)
			return nil, fmt.Errorf("failed to restore chunk version: %w", err)
		}
		if err := finish(nil); err != nil {
			common.Warn("Chunk restore: failed to commit the restored version",
				zap.String("chunk_id", req.ChunkID),
				zap.Int("source_version", req.Version),
				zap.Error(err))
		}
		newVersion = version
	} else {
		if err = s.docEngine.UpdateChunks(ctx, map[string]interface{}{
			"id":     req.ChunkID,
			"doc_id": req.DocumentID,
		}, restore, indexName, req.DatasetID); err != nil {
			return nil, fmt.Errorf("failed to restore chunk version: %w", err)
		}

		// The appended row must describe the whole row the restore left behind,
		// not just the fields the version rewrote: fields the snapshot does not
		// mention (an untouched picture, a token column) stay as they are. It is
		// stored as a full snapshot, so rewinding again from the restored state
		// is a copy as well.
		newVersion, err = s.appendRestoredVersion(ctx, req,
			chunkVersionFullSnapshot(mergeChunkRow(existing, restore), meta))
		if err != nil {
			common.Warn("Chunk restore: failed to record the restored version",
				zap.String("chunk_id", req.ChunkID),
				zap.Int("source_version", req.Version),
				zap.Error(err))
			newVersion = req.Version
		}
	}

	// A compiled (Wiki/essence/…) product may quote this chunk, and the restore
	// can change its content or availability, so schedule the same refresh an
	// edit does. The dataset version was recorded above, together with the chunk
	// row it stamps: both restore routes write it inside their transaction.
	s.markWikiDirty(ctx, kb.TenantID, req.DatasetID, req.DocumentID, []string{req.ChunkID})

	return &service.RestoreChunkVersionResponse{Version: newVersion}, nil
}

// versionTarget resolves the chunk's dataset and document and enforces that
// the route's dataset and document agree. Dataset resource access stays at
// read, dataset role access follows the operation, and the document grant sets
// the required resource level.
func (s *ChunkService) versionTarget(ctx context.Context, req *service.ChunkVersionRequest, documentPermission int32, roleAction permission.Action) (*entity.Document, *entity.Knowledgebase, error) {
	if req == nil || req.ChunkID == "" {
		return nil, nil, fmt.Errorf("chunk_id is required")
	}
	checker := permission.NewDatabaseChecker(dao.DB)
	datasetRef := permission.ResourceRef{Kind: permission.ResourceKindDataset, ID: req.DatasetID}
	if err := checker.CheckResourcePermission(ctx, datasetRef, permission.ResourcePermissionRead); err != nil {
		return nil, nil, err
	}
	if err := checker.CheckFeature(ctx, permission.FeatureDataset, roleAction); err != nil {
		return nil, nil, err
	}
	kb, err := s.getKnowledgebaseByID(ctx, req.DatasetID)
	if err != nil || kb == nil {
		return nil, nil, fmt.Errorf("knowledge base not found")
	}
	doc, err := dao.NewDocumentDAO().GetByID(ctx, dao.DB, req.DocumentID)
	if err != nil || doc == nil {
		return nil, nil, fmt.Errorf("document not found")
	}
	if doc.KbID != req.DatasetID {
		return nil, nil, fmt.Errorf("document does not belong to this dataset")
	}
	if err := checker.CheckResourcePermission(ctx, permission.ResourceRef{Kind: permission.ResourceKindDocument, ID: doc.ID}, documentPermission); err != nil {
		return nil, nil, err
	}
	return doc, kb, nil
}

// chunkRestoreKeepLive lists the fields a rewound chunk keeps from its live row:
// identity, reading position, page region, parent link, ranking state and the
// compiled-product markers. They describe where the row sits and what it is,
// not what it says, and a content rewind must not move a chunk, re-parent it or
// turn a compiled row back into a plain one.
var chunkRestoreKeepLive = map[string]struct{}{
	"id":                            {},
	"doc_id":                        {},
	"kb_id":                         {},
	"docnm_kwd":                     {},
	"create_time":                   {},
	"update_time":                   {},
	"_score":                        {},
	"mom_id":                        {},
	"chunk_order_int":               {},
	"page_num_int":                  {},
	"top_int":                       {},
	"create_timestamp_flt":          {},
	"rank_int":                      {},
	"rank_flt":                      {},
	"pagerank_fea":                  {},
	"type_kwd":                      {},
	"knowledge_graph_kwd":           {},
	"compile_kwd":                   {},
	"compilation_template_kind_kwd": {},
	"name_kwd":                      {},
	"source_chunk_ids":              {},
}

// chunkRestoreRow turns a stored version into the engine update that puts the
// chunk back into that state.
//
// A full snapshot carries the tokens and the vector the version was ranked by,
// so restoring it is a copy: nothing is re-tokenized and no model is called, and
// the chunk ranks exactly as it did. A version recorded before snapshots held
// their derived fields cannot be restored without producing them again, which a
// restore does not do, so it is reported instead of returning half a row.
func chunkRestoreRow(snapshot entity.JSONMap) (map[string]interface{}, error) {
	update, ok := chunkVerbatimRestore(snapshot)
	if !ok {
		return nil, fmt.Errorf("this version predates the snapshot format that stores tokens and vectors; re-parse the document to rebuild it")
	}
	return update, nil
}

// chunkVerbatimRestore builds the copy described by chunkRestoreRow.
//
// The admission test is chunkversion's: a full snapshot that records the vector
// and the tokens of the text it holds is one a restore can copy. A dataset whose
// embedding model or language changed since the version was written still gets
// its recorded row back - restoring a version returns that version, and a
// re-parse is what brings derived fields up to date.
func chunkVerbatimRestore(snapshot entity.JSONMap) (map[string]interface{}, bool) {
	row, stored, ok := chunkversion.FullSnapshotRow(snapshot)
	if !ok {
		return nil, false
	}

	update := make(map[string]interface{}, len(row))
	for key, value := range row {
		if _, keep := chunkRestoreKeepLive[key]; keep {
			continue
		}
		update[key] = value
	}
	// List fields are normalized to the shape the engine writes: a snapshot that
	// went through JSON carries []interface{}.
	update["important_kwd"] = chunkStringSlice(row["important_kwd"])
	update["question_kwd"] = chunkStringSlice(row["question_kwd"])
	update["tag_kwd"] = chunkStringSlice(row["tag_kwd"])
	if _, ok := row["available_int"]; !ok {
		update["available_int"] = chunkSnapshotAvailability(snapshot)
	}
	if _, ok := row["tag_feas"]; !ok {
		update["tag_feas"] = map[string]float64{}
	}
	// The stored vector came back through JSON as []interface{}; the engine
	// write takes a typed slice.
	if vector, ok := chunkversion.SnapshotVector(row[stored.VectorField]); ok {
		update[stored.VectorField] = vector
	}
	return update, true
}

// chunkVersionCreatedAt mirrors datasetVersionCreatedAt: hand the raw
// millisecond instant to the client, which formats it in the reader's time
// zone. A missing timestamp coalesces to 0 so the JSON shape is stable.
func chunkVersionCreatedAt(millis *int64) int64 {
	if millis != nil && *millis > 0 {
		return *millis
	}
	return 0
}

// formatChunkVersionTime renders the legacy create_date string for clients
// that still read it. The web UI uses chunkVersionCreatedAt and formats the
// raw instant locally, like the dataset version list.
func formatChunkVersionTime(millis *int64) string {
	if millis == nil {
		return ""
	}
	date := time.UnixMilli(*millis)
	if value, ok := utility.FormatTimeToString(&date, chunkVersionTimeFormat).(string); ok {
		return value
	}
	return ""
}
