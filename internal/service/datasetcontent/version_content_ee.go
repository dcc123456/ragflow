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

// Package datasetcontent restores the content side of a dataset version. It sits
// outside the dataset service because putting documents back needs the document
// service, while the document package's own tests import the dataset service;
// keeping the coordination in a third package keeps those two acyclic.
package datasetcontent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
	"ragflow/internal/service/chunk"
	"ragflow/internal/service/chunkversion"
	"ragflow/internal/service/dataset"
	"ragflow/internal/service/datasetversion"
	"ragflow/internal/service/document"
)

// codedError carries the HTTP-level code the handler maps onto a status, the
// same way the dataset service reports its own failures.
type codedError struct {
	code    common.ErrorCode
	message string
}

func (e codedError) Error() string { return e.message }

// Code reports the error code the handler should answer with.
func (e codedError) Code() common.ErrorCode { return e.code }

// Restore puts a dataset back to the state a version recorded: the configuration
// it held and the documents and chunks it contained.
//
// Every kind restores both halves. A version records a state even when its writer
// only moved one of them - the other is reached through ConfigHash / DocumentsHash -
// and a caller who asks to return to version N means the dataset as it stood at N.
// Restoring a configuration version therefore puts back the document set that held
// when it was written and removes the documents uploaded after it, which is why the
// dry run reports both sets for every kind and the confirmation shows them.
//
// A document_config version additionally replays the parse settings of the
// documents up to its anchor: that replay is the half it recorded itself.
//
// The configuration half is planned under a row lock and only the columns that
// differ are written. The caller's expected head is checked under that same lock,
// before the content moves: a check outside it leaves a window in which a document
// is uploaded and then removed as "added after the version", and a check after the
// content work is too late, because that work has already moved the head.
//
// The content half puts the document set's documents back from the content
// removal rows, removes the ones added since, and replays every document's chunk
// history up to this version's anchor, so an edit made later is undone too.
//
// The content work is not one transaction: each document owns the one that puts it
// back or removes it, so a large dataset does not hold row locks for the length of
// the request, and every step is idempotent (an already present document is
// skipped, an already absent one is not an error), so a failure can be retried.
//
// The embedding model a content version recorded is switched in its own
// transaction before any chunk is rebuilt, so the recomputed vectors belong to the
// model being restored; the rest of the configuration and the version that records
// the restore then move in one transaction, so a restore that applied the documents
// can never lack its version. A failure after the switch puts the model back: that
// switch is the only part of the content work with no version of its own to undo it.
//
// One restore therefore appends one version: the restore itself. The state it
// replaces stays described by the version before it, and undoing a restore means
// restoring that version. The documents it removes are removed without a delete
// version of their own - that entry would describe a state that only exists in the
// middle of the restore.
//
// A document whose chunk history does not reach back to the anchor keeps the
// chunks it has and is reported under MissingDocuments, with a reason code: the
// version's content cannot be rebuilt for it. A document no removal row longer
// holds, and a document that came back with no history to rebuild it from, are
// reported the same way.
//
// A content version whose document set was never recorded is refused outright:
// the column means "not recorded", not "no documents", and decoding it as an empty
// set would remove every document the dataset holds.
func Restore(ctx context.Context, req *dataset.RestoreDatasetVersionRequest) (*dataset.RestoreDatasetVersionResponse, error) {
	if req == nil {
		return nil, codedError{code: common.CodeArgumentError, message: "request is required"}
	}
	datasetID, err := dataset.ResolveDatasetVersionTarget(ctx, req.DatasetID)
	if err != nil {
		return nil, err
	}

	target, err := dao.NewDatasetVersionDAO().GetByDatasetAndVersion(ctx, dao.DB, datasetID, req.Version)
	if err != nil {
		if errors.Is(err, dao.ErrDatasetVersionNotFound) {
			return nil, codedError{code: common.CodeNotFound, message: fmt.Sprintf("dataset version %d not found", req.Version)}
		}
		return nil, fmt.Errorf("failed to load dataset version: %w", err)
	}

	contentRestore := datasetversion.RestoresContent(target.Kind)
	var recordedIDs []string
	if contentRestore {
		// The set is only read for a kind whose restore rewrites the content:
		// refusing a configuration restore because its inherited set is missing
		// would refuse a restore that never needed the set.
		recordedIDs, err = recordedDocumentIDs(target)
		if err != nil {
			return nil, err
		}
	}

	kb, err := dao.NewKnowledgebaseDAO().GetByID(ctx, dao.DB, datasetID)
	if err != nil || kb == nil {
		return nil, codedError{code: common.CodeNotFound, message: "dataset not found"}
	}
	if req.ExpectedVersion != nil && kb.Version != *req.ExpectedVersion {
		return nil, codedError{
			code:    common.CodeConflict,
			message: fmt.Sprintf("dataset version changed: expected %d, current is %d", *req.ExpectedVersion, kb.Version),
		}
	}

	var toRestore, toDelete []string
	if contentRestore {
		// A running parse owns the documents it is writing: it appends chunks to the
		// same document a restore rebuilds, and the two share no lock. A restore
		// started under it would interleave its replay with the run's chunk writes
		// and leave a partial document behind, which is worse than refusing. The dry
		// run answers with the same reason instead of a plan, so a client can say so
		// before the user commits.
		if reason := parsingDocumentsReason(ctx, datasetID); reason != "" {
			if req.DryRun {
				return &dataset.RestoreDatasetVersionResponse{
					Version:       dataset.NoDatasetVersion,
					PreVersion:    kb.Version,
					BlockedReason: reason,
				}, nil
			}
			return nil, codedError{code: common.CodeConflict, message: reason}
		}
		currentIDs, listErr := listDocumentIDs(ctx, datasetID)
		if listErr != nil {
			return nil, listErr
		}
		toRestore, toDelete = diffDocumentSets(recordedIDs, currentIDs)
	}

	datasetSvc := dataset.NewDatasetService()
	if req.DryRun {
		// A preview has to answer "would this be refused", not only "what would it
		// write": the plan is what validates the recorded name, embedding model and
		// pipeline, and a preview that skipped it would show a change set for a
		// restore the commit then rejects. The plan reads under the dataset's row
		// lock, so the preview needs a transaction - and one that is rolled back,
		// because a preview applies nothing.
		changes, planErr := previewConfigRestore(ctx, datasetSvc, datasetID, target.Config)
		if planErr != nil {
			return nil, planErr
		}
		return &dataset.RestoreDatasetVersionResponse{
			Version:           dataset.NoDatasetVersion,
			PreVersion:        kb.Version,
			Applied:           false,
			Changes:           changes,
			RestoredDocuments: toRestore,
			DeletedDocuments:  toDelete,
		}, nil
	}

	actor := permission.UserIDFromContext(ctx)
	// The plan runs before any content moves, under the dataset's row lock: it
	// validates the configuration half - the name, the embedding model, the
	// pipeline - so a restore that could never be completed does not start removing
	// documents, and it checks the caller's expected head while the state it
	// describes is still the one on disk.
	//
	// Nothing is recorded here. The state this restore replaces is already
	// described by the version before it, so a row written now would only spend a
	// version number to repeat it. Undoing a restore is restoring that previous
	// version. The write phase plans again from the state it finds, because
	// the content work has already moved the configuration (the embedding switch),
	// and the version it records must describe what is actually written.
	var preVersion int64
	if planErr := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, locked, err := datasetSvc.PlanConfigRestoreTx(ctx, tx, datasetID, req.ExpectedVersion, target.Config)
		if err != nil {
			return err
		}
		if locked != nil {
			preVersion = locked.Version
		}
		return nil
	}); planErr != nil {
		return nil, planErr
	}

	restored := make([]string, 0, len(toRestore))
	var deleted []string
	// deletedNames are the display names of the documents this restore removed,
	// read while their rows still exist.
	var deletedNames map[string]string
	// renamed are the documents this restore put back under an earlier name, keyed
	// by id: the rest of the set keeps the name it already had.
	var renamed map[string]documentRename
	missing := newMissingSet()
	var embeddingChange *dataset.DatasetConfigChange
	restoredChunks := 0
	// restoreVersion is the one dataset version number this restore occupies. It
	// is reserved before the replay so the chunk history rows the replays write
	// can be anchored at it, and it is the number the version row closing the
	// restore is recorded on: the rows are what that version describes, so one
	// restore advances the dataset by one number instead of one per document.
	// Zero means nothing reserved a number (no history in the build, or the
	// reservation failed, or there was no chunk replay at all), and the closing
	// row then takes the next number as usual.
	restoreVersion := int64(0)
	// The chunk replay is anchored at the version's own number: a chunk history row
	// written at or below it is part of the state that version describes. The
	// number is what orders the writes - two edits of one chunk can share a
	// millisecond, and then a clock cannot tell them apart - and it also covers a
	// row written by the same change that recorded the version, since a parse batch
	// shares its number.
	atVersion := target.Version
	// The other replays (parse settings, document metadata, wiki pages) are still
	// anchored on the version's timestamp: their histories are stamped with the
	// database's clock (see entity.DBTimeMillis), so every application instance
	// compares them the same way, and an edit and the version that records it can
	// share a second - which is why the millisecond column is read rather than the
	// second-resolution date.
	atMillis := versionAnchorMillis(target)
	// replayedStatus is what the visibility replay put back, held until the
	// restore's own version number exists (see the recording below).
	var replayedStatus []document.DocumentStatusChange

	if contentRestore {
		// The embedding model a version recorded is applied before any chunk is
		// rebuilt: the replay below recomputes vectors, and they have to belong to
		// the model the dataset is being returned to, not the one it is leaving. It
		// moves before the content, so the configuration transaction at the end
		// finds the column equal, and undoing the restore (by restoring the version
		// before it) switches the model back with the rest of the configuration.
		embeddingChange, err = datasetSvc.ApplyRestoreEmbeddingSwitch(ctx, datasetID, target.Config)
		if err != nil {
			return nil, err
		}

		for _, docID := range toRestore {
			ok, restoreErr := document.RestoreRemovedDocumentEE(ctx, datasetID, docID)
			if restoreErr != nil {
				revertEmbeddingSwitch(ctx, datasetSvc, datasetID, embeddingChange)
				return nil, fmt.Errorf("restore document %s: %w", docID, restoreErr)
			}
			if !ok {
				// The row was cleaned up, or the document was removed before the
				// row existed. The restore still applies the rest, and the caller
				// sees what could not come back.
				missing.add(docID, missingReasonRemovalGone)
				continue
			}
			restored = append(restored, docID)
		}

		if len(toDelete) > 0 {
			// The names are read before the rows go: the restore's change set says
			// which documents it removed, and a removed row has no name left to
			// read afterwards, so the history page would show bare ids.
			deletedNames = documentNamesFor(toDelete)
			docSvc := document.NewDocumentService()
			if _, delErr := docSvc.DeleteDocumentsForRestore(ctx, toDelete, datasetID); delErr != nil {
				revertEmbeddingSwitch(ctx, datasetSvc, datasetID, embeddingChange)
				return nil, fmt.Errorf("remove the documents added after version %d: %w", target.Version, delErr)
			}
			deleted = toDelete
		}

		// The documents are back, and the ones that stayed may have been edited since
		// the version: replaying the chunk history is what puts the content of the
		// whole document set back as it stood at this version. Chunks added after the
		// anchor and edits made on them are undone by the replay, which rebuilds each
		// document from its history rather than merging with what is there.
		readded := make(map[string]struct{}, len(restored))
		for _, docID := range restored {
			readded[docID] = struct{}{}
		}

		// The visibility of a document is a state of the whole document, and its
		// chunks follow it without holding a version each: the switch is put back
		// before the replay below, which reads it from the document row to keep a
		// disabled document's chunks out of retrieval. Best effort, like the other
		// replays - a switch that cannot be read must not fail a restore whose
		// content is already back.
		if statuses, statusErr := document.RestoreDocumentStatusAtEE(ctx, dao.DB, datasetID, atVersion); statusErr != nil {
			common.Warn("Content restore: failed to replay the document visibility",
				zap.String("dataset_id", datasetID),
				zap.Int64("version", target.Version),
				zap.Error(statusErr))
		} else {
			replayedStatus = statuses
		}

		if len(recordedIDs) > 0 {
			chunkSvc := chunk.NewChunkService()

			// One number for the whole replay: every document's restore rows are
			// anchored at it and the version recorded below carries it, so the
			// history stays contiguous. Reserved here rather than inside the
			// replay because only the caller knows that the documents it is about
			// to replay share one restore.
			reserved, reserveErr := datasetversion.ReserveVersion(ctx, dao.DB, datasetID)
			if reserveErr != nil {
				common.Warn("Content restore: failed to reserve the version the replay is anchored at",
					zap.String("dataset_id", datasetID),
					zap.Error(reserveErr))
			} else {
				restoreVersion = reserved
			}

			var replayMissing *missingSet
			restoredChunks, replayMissing = replayRecordedChunks(
				ctx, datasetID, recordedIDs, readded, atVersion, restoreVersion, chunkSvc.ReplayDocumentChunksAtEE)
			missing.merge(replayMissing)
		}

		// A name is metadata, but it is part of what the version describes: a
		// document renamed after the version has to read as it did then, or restoring
		// would leave the timeline disagreeing with the files - and "undo the rename"
		// would have no way to happen. After the chunk replay, because a rename
		// rewrites the chunks' title fields and has to be the last word on them.
		renamed, err = restoreDocumentNames(ctx, datasetID, target, recordedIDs)
		if err != nil {
			revertEmbeddingSwitch(ctx, datasetSvc, datasetID, embeddingChange)
			return nil, fmt.Errorf("restore the document names of version %d: %w", target.Version, err)
		}
	}

	// The parse settings each document runs under are part of the state a version
	// describes: a document edited after the version has to run under the settings
	// it had then, or the next parse produces something the version never held.
	// Replayed from the document's own states at the version, which is what a
	// document_config version is about - it moves no content, so it is not a
	// content kind, but its restore is this replay and nothing else.
	//
	// A document with no state that early predates the history and keeps what it
	// has, which is what it was created with.
	//
	// The settings the replay wrote are kept in hand rather than recorded here:
	// the restore's own version is allocated by the transaction below, and the
	// states belong under it.
	var (
		replayedSettings []*entity.Document
		replayedMetadata []document.ReplayedDocumentMetadata
	)
	if contentRestore || target.Kind == entity.DatasetVersionKindDocumentConfig {
		settings, settingsErr := document.RestoreParserSettingsAtEE(ctx, dao.DB, datasetID, target.Version)
		replayedSettings = settings
		if settingsErr != nil {
			common.Warn("Content restore: failed to replay the document parse settings",
				zap.String("dataset_id", datasetID),
				zap.Int64("version", target.Version),
				zap.Error(settingsErr))
		}
	}

	// The metadata values are the other half that does not live on the document
	// row: the document set coming back says which documents existed, not what
	// their metadata said. They are replayed from their own states at the same
	// version - a document whose values moved after it gets back what it held
	// then, and one that never moved is left alone. Because the values are written
	// through the metadata store of a document that is already back, this runs
	// after the document set has been restored.
	//
	// The documents this restore brought back from their removal rows are named
	// separately: their metadata store went with the removal, so the state at the
	// version has to be written back even though nothing about their values moved
	// after it - and restoring the metadata change itself replays the documents it
	// recorded for the same reason (see RestoreDocumentMetadataAtEE).
	//
	// Best effort, like the settings replay: the values of one document must not
	// fail a restore that has already put the dataset's content back.
	if contentRestore || target.Kind == entity.DatasetVersionKindDocumentMetadata {
		metas, metadataErr := document.RestoreDocumentMetadataAtEE(ctx, dao.DB, datasetID, target.Version, restored, target.Kind == entity.DatasetVersionKindDocumentMetadata)
		replayedMetadata = metas
		if metadataErr != nil {
			common.Warn("Content restore: failed to replay the document metadata",
				zap.String("dataset_id", datasetID),
				zap.Int64("version", target.Version),
				zap.Error(metadataErr))
		}
	}

	// The compiled wiki pages are the third half that lives outside the document
	// set: a page is an artifact in the document engine, with no document row and
	// no chunk version behind it, so the replay above cannot rebuild it. It is
	// replayed from its own history at the same anchor, for the same reason the
	// metadata values are - a page edited after the version has to read as it did
	// then, or the restore leaves the timeline disagreeing with what the dataset
	// says.
	//
	// Under contentRestore only, like the chunk replay: any version describes the
	// pages as they stood when it was taken, and a version that moves no content
	// still moves the pages back with everything else.
	if contentRestore {
		if wikiErr := restoreWikiPagesAtEE(ctx, kb.TenantID, datasetID, atMillis); wikiErr != nil {
			common.Warn("Content restore: failed to replay the wiki pages",
				zap.String("dataset_id", datasetID),
				zap.Int64("at", atMillis),
				zap.Error(wikiErr))
		}
	}

	var (
		newVersion int64
		changes    []dataset.DatasetConfigChange
		pagerank   *int64
		restoredKB *entity.Knowledgebase
	)
	// commitRestore writes the configuration back and records the restore in one
	// transaction: the snapshot the version stores has to show the restored
	// configuration and the restored document set together. version is the number
	// to record on, or zero for the next free one.
	commitRestore := func(version int64) error {
		return dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// The head was checked before the content work, and that work moved the
			// configuration (the embedding switch) without recording a version. The plan
			// is therefore taken again at whatever the head is now, under the lock that
			// makes its diff and its write atomic.
			plan, lockedKB, planErr := datasetSvc.PlanConfigRestoreTx(ctx, tx, datasetID, nil, target.Config)
			if planErr != nil {
				return planErr
			}
			changes = plan.Changes
			pagerank = plan.PagerankValue
			restoredKB = lockedKB

			// The configuration goes back first, then the restore is recorded, so its
			// snapshot shows the restored configuration and the restored document set
			// together. Nothing was written before the content work: the version this
			// restore replaces is still the one before it in the history.
			if planErr = datasetSvc.ApplyConfigRestoreTx(ctx, tx, datasetID, plan); planErr != nil {
				return planErr
			}
			// Pinned at the number the replay was anchored at: the restore's chunk
			// history rows carry it, so the version that describes them has to carry
			// it too. A restore that reserved nothing (no history, or no chunk replay)
			// passes zero and takes the next number.
			newVersion, planErr = datasetversion.RecordVersionWithChangesAtVersion(
				ctx, tx, datasetID, entity.DatasetVersionKindRestore,
				actor, target.Version, version,
				restoreChanges(restored, deleted, deletedNames, renamed))
			return planErr
		})
	}
	err = commitRestore(restoreVersion)
	if err != nil && restoreVersion > 0 && errors.Is(err, datasetversion.ErrVersionConflict) {
		// Another writer recorded a version while the replay ran, and the replay can
		// take long on a large dataset. Failing here would throw away a restore whose
		// content is already in place, so the version is recorded on the next free
		// number instead: the chunk history rows stay anchored at the reserved one,
		// which is what a restore of this version replays, and that number simply
		// carries no row of its own.
		common.Warn("Content restore: the dataset moved during the replay, recording the restore on the next free version",
			zap.String("dataset_id", datasetID),
			zap.Int64("reserved_version", restoreVersion),
			zap.Error(err))
		err = commitRestore(0)
	}
	if err != nil {
		revertEmbeddingSwitch(ctx, datasetSvc, datasetID, embeddingChange)
		return nil, err
	}

	// A document that came back from its removal starts a new life: the states it
	// had before the delete were dropped with the removal row, so the settings it
	// carries now are recorded under this version. Otherwise a later restore would
	// find nothing to read and leave whatever the document happens to carry.
	if len(restored) > 0 {
		if docs, loadErr := dao.NewDocumentDAO().GetByIDs(ctx, dao.DB, restored); loadErr != nil {
			common.Warn("Content restore: failed to read back the restored documents",
				zap.String("dataset_id", datasetID),
				zap.Error(loadErr))
		} else {
			replayedSettings = append(replayedSettings, docs...)
		}
	}

	// The states the replay put back are recorded under the version that closed
	// the restore. Written after that transaction, because the number did not
	// exist while the replay ran; without them the replay would be invisible to a
	// later restore, which finds nothing newer than its own anchor and leaves the
	// replayed values in place.
	if err := document.RecordReplayedDocumentStatesAtVersionEE(ctx, dao.DB, newVersion, replayedSettings, replayedMetadata); err != nil {
		return nil, fmt.Errorf("dataset was restored, but recording the replayed states failed: %w", err)
	}
	// The switches the visibility replay put back belong to that list too: without
	// them, restoring back to the version taken before this one would find no
	// newer state and leave the replayed statuses in place.
	if err := document.RecordReplayedDocumentStatusAtVersionEE(ctx, dao.DB, newVersion, replayedStatus); err != nil {
		return nil, fmt.Errorf("dataset was restored, but recording the replayed visibility failed: %w", err)
	}

	// The pagerank column is mirrored onto every stored chunk; that write lives
	// outside the transaction like UpdateDataset's, so a failure is reported.
	//
	// It is not compensated: the transaction above has committed, so the restore
	// and its version exist and the head already describes the restored state.
	// Putting the embedding model back here would make the live row disagree with
	// the version that was just recorded - the switch belongs to the committed
	// restore now, and only the pagerank column is left behind.
	if pagerank != nil {
		if mirrorErr := datasetSvc.MirrorConfigPagerank(ctx, restoredKB, datasetID, *pagerank); mirrorErr != nil {
			return nil, fmt.Errorf("dataset was restored, but refreshing chunk pagerank failed: %w", mirrorErr)
		}
	}

	// The switch happened before the content work, so the configuration plan no
	// longer sees the column move; it is reported here to keep the answer equal to
	// what the dry run previewed.
	if embeddingChange != nil {
		changes = append(changes, *embeddingChange)
	}

	resp := &dataset.RestoreDatasetVersionResponse{
		Version:          newVersion,
		PreVersion:       preVersion,
		Applied:          true,
		Changes:          changes,
		DeletedDocuments: deleted,
	}
	if contentRestore {
		resp.RestoredDocuments = restored
		resp.RestoredChunks = restoredChunks
		resp.MissingDocuments = missing.ids
		resp.MissingDocumentReasons = missing.reasons
	}
	return resp, nil
}

// errPreviewRollback aborts the transaction a preview runs in, after the plan
// has been read. It never reaches a caller.
var errPreviewRollback = errors.New("content restore: dry run")

// previewConfigRestore returns the configuration changes a restore would write,
// and the error it would be refused with.
//
// The plan is the same one the commit runs, so the preview validates the recorded
// name, the embedding model and the pipeline - a restore whose configuration
// half is impossible is reported by the preview instead of by the commit. It
// reads under the dataset's row lock, so it needs a transaction; the transaction
// is rolled back because a preview writes nothing, and the rollback is also what
// releases the lock.
func previewConfigRestore(ctx context.Context, svc *dataset.DatasetService, datasetID string, stored entity.JSONMap) ([]dataset.DatasetConfigChange, error) {
	var changes []dataset.DatasetConfigChange
	err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		plan, _, planErr := svc.PlanConfigRestoreTx(ctx, tx, datasetID, nil, stored)
		if planErr != nil {
			return planErr
		}
		changes = plan.Changes
		return errPreviewRollback
	})
	if err != nil && !errors.Is(err, errPreviewRollback) {
		return nil, err
	}
	return changes, nil
}

// revertEmbeddingSwitch puts the dataset's embedding model back after a content
// restore failed past the switch. A nil change means the restore never moved the
// column, and there is nothing to undo.
func revertEmbeddingSwitch(ctx context.Context, svc *dataset.DatasetService, datasetID string, change *dataset.DatasetConfigChange) {
	if change == nil {
		return
	}
	if err := svc.RevertRestoreEmbeddingSwitch(context.WithoutCancel(ctx), datasetID, change); err != nil {
		common.Warn("Content restore: failed to put the embedding model back after a failed restore",
			zap.String("dataset_id", datasetID),
			zap.Any("model", change.Before),
			zap.Error(err))
	}
}

// missingReason codes explain why a document a version held could not be put
// back. They travel with the response so a client can tell the cases apart: a
// re-parse rebuilds what has a history, and nothing rebuilds // longer holds.
const (
	// missingReasonRemovalGone: no removal row holds the document.
	missingReasonRemovalGone = "removal_gone"
	// missingReasonNoChunkHistory: nothing was recorded at or before the anchor,
	// so the document's chunks cannot be rebuilt from its history.
	missingReasonNoChunkHistory = "no_chunk_history"
	// missingReasonReplayFailed: the history was there and rebuilding it failed;
	// the document is left without chunks until it is re-parsed.
	missingReasonReplayFailed = "replay_failed"
	// missingReasonHistoryUnreadable: the history could not be read at all, so
	// nothing was attempted.
	missingReasonHistoryUnreadable = "history_unreadable"
)

// missingSet collects the documents a restore could not put back, once each, with
// the reason it met them in the order it met them.
type missingSet struct {
	ids     []string
	reasons map[string]string
}

func newMissingSet() *missingSet {
	return &missingSet{reasons: make(map[string]string)}
}

// add names a document, keeping the first reason it was reported for: the removal row
// is checked before the replay, so the reason it recorded is the more fundamental
// one.
func (m *missingSet) add(docID, reason string) {
	if m == nil || docID == "" {
		return
	}
	if _, ok := m.reasons[docID]; ok {
		return
	}
	m.reasons[docID] = reason
	m.ids = append(m.ids, docID)
}

// merge folds another set in without naming a document twice.
func (m *missingSet) merge(other *missingSet) {
	if m == nil || other == nil {
		return
	}
	for _, docID := range other.ids {
		m.add(docID, other.reasons[docID])
	}
}

// recordedDocumentIDs decodes the document set a version recorded.
//
// A NULL document set is refused instead of decoded as an empty set, because the
// column carries two different meanings: "not recorded" when it is NULL - a
// version written before the document set existed, or one whose lookup failed - and
// "held no documents" when it is an empty list. Reading the first as the second
// would delete every document the dataset has.
//
// The version's row may not carry the set itself: a parse or a chunk edit
// repeats the set of the version that established it, so the load that produced
// this row follows that pointer first.
func recordedDocumentIDs(version *entity.DatasetVersion) ([]string, error) {
	if version.Documents == "" {
		return nil, codedError{
			code: common.CodeConflict,
			message: fmt.Sprintf(
				"dataset version %d does not record its documents, so its content cannot be restored",
				version.Version),
		}
	}
	ids, err := datasetversion.ParseDocuments(version.Documents)
	if err != nil {
		return nil, fmt.Errorf("decode the document set of version %d: %w", version.Version, err)
	}
	recorded := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "" {
			recorded = append(recorded, id)
		}
	}
	return recorded, nil
}

// chunkReplayer rebuilds one document's chunks as they stood at the anchor.
//
// It is injected rather than called through the chunk service so the failure path
// can be exercised: the engine is cleared before the replay writes the chunks
// back, so a failed replay leaves the document with no chunks at all.
//
// The two numbers it takes are different anchors: atVersion is the version whose
// content is rebuilt, restoreVersion the number the rebuild's own history rows
// carry - the version this restore will occupy.
type chunkReplayer func(ctx context.Context, datasetID, documentID string, atVersion int64, restoreVersion int64) (int, error)

// replayRecordedChunks rebuilds the content of every document the version
// recorded, and reports the documents it could not put back, with the reason.
//
// A document whose history cannot be read or fails to replay is missing: the
// restore must name it. Silently skipping the failure is what turns a broken
// replay into a response that says "applied" while the document's chunks are no
// longer in the retrieval engine.
//
// An anchor with nothing at or before it is ambiguous, and the history the
// document holds at any version is what resolves it:
//
//   - a document that came back from its removal has no chunks at all (the delete
//     took them), so replaying would clear an already empty engine, write nothing,
//     and let the response count it as restored content;
//   - a document that stayed may have chunks the history cannot describe - it was
//     parsed before the history existed, or the history was purged - and clearing
//     them would empty a document the version did hold.
//
// Both are reported instead, because "applied" over a document whose content is
// not there is the one answer a caller cannot act on. A document that does have
// history, all of it written after the anchor, is neither: the version described
// it with no chunks (uploaded, not yet parsed), so the replay runs and clears the
// engine, which is exactly what that version says.
// documentChunkProbeBatch bounds one IN (...) list: SQLite caps a statement at
// 999 bound parameters, and a version can record more documents than that.
const documentChunkProbeBatch = 500

// documentsWithChunks reports which of the named documents hold chunks right now.
// It separates the two empty states a restore can find: "no history" plus "no
// chunks" is a document that was never parsed, while "no history" plus "chunks"
// is a history this build cannot reach.
func documentsWithChunks(ctx context.Context, documentIDs []string) (map[string]struct{}, error) {
	chunked := make(map[string]struct{}, len(documentIDs))
	if dao.DB == nil || len(documentIDs) == 0 {
		return chunked, nil
	}
	for start := 0; start < len(documentIDs); start += documentChunkProbeBatch {
		end := start + documentChunkProbeBatch
		if end > len(documentIDs) {
			end = len(documentIDs)
		}
		var rows []struct {
			ID       string
			ChunkNum int64
		}
		if err := dao.DB.WithContext(ctx).
			Model(&entity.Document{}).
			Select("id", "chunk_num").
			Where("id IN ?", documentIDs[start:end]).
			Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("read the chunk counts of %d documents: %w", end-start, err)
		}
		for _, row := range rows {
			if row.ChunkNum > 0 {
				chunked[row.ID] = struct{}{}
			}
		}
	}
	return chunked, nil
}

func replayRecordedChunks(ctx context.Context, datasetID string, recordedIDs []string, readded map[string]struct{}, atVersion int64, restoreVersion int64, replay chunkReplayer) (int, *missingSet) {
	restored := 0
	missing := newMissingSet()
	// The documents' chunk counts are read once, on the first row that has no
	// history at all: that is the only row the answer decides, and a restore that
	// never meets one pays nothing for it.
	var (
		chunkedDocs    map[string]struct{}
		chunkProbeErr  error
		chunkProbeDone bool
	)
	probeChunks := func() (map[string]struct{}, error) {
		if !chunkProbeDone {
			chunkProbeDone = true
			chunkedDocs, chunkProbeErr = documentsWithChunks(ctx, recordedIDs)
		}
		return chunkedDocs, chunkProbeErr
	}
	for _, docID := range recordedIDs {
		states, stateErr := chunkversion.StatesAt(ctx, dao.DB, docID, atVersion)
		if stateErr != nil {
			common.Warn("Content restore: failed to read the chunk history",
				zap.String("dataset_id", datasetID),
				zap.String("document_id", docID),
				zap.Error(stateErr))
			missing.add(docID, missingReasonHistoryUnreadable)
			continue
		}
		if len(states) == 0 {
			// Nothing reaches the anchor, and the rows written after it are what say
			// which empty state this is: a document with history but none of it
			// before the anchor had no chunks then (it was uploaded and not yet
			// parsed), so the replay below clears what the engine holds and reports
			// zero - which is what the version says.
			known, historyErr := chunkversion.HasAnyHistory(ctx, dao.DB, docID)
			if historyErr != nil {
				common.Warn("Content restore: failed to read whether the document has chunk history",
					zap.String("dataset_id", datasetID),
					zap.String("document_id", docID),
					zap.Error(historyErr))
				missing.add(docID, missingReasonHistoryUnreadable)
				continue
			}
			if !known {
				// No history at all: the parser never produced a chunk for this
				// document, because uploading creates none and the parser is what
				// writes the baseline. The version held no chunks for it either, so
				// the state to put back is empty - there is nothing to clear, and
				// nothing to record for a document that never had a chunk. Counting
				// it as content that could not come back would ask for a re-parse
				// the version never had.
				chunked, chunkErr := probeChunks()
				if chunkErr != nil {
					common.Warn("Content restore: failed to read whether the document has chunks",
						zap.String("dataset_id", datasetID),
						zap.String("document_id", docID),
						zap.Error(chunkErr))
					missing.add(docID, missingReasonHistoryUnreadable)
					continue
				}
				if _, hasChunks := chunked[docID]; !hasChunks {
					continue
				}
				// Chunks now but no history is the other case: the history predates
				// the feature or was purged, and clearing the engine would drop text
				// nothing can rebuild. That document is reported instead of emptied.
				_, cameBack := readded[docID]
				common.Warn("Content restore: no chunk history reaches the version's anchor",
					zap.String("dataset_id", datasetID),
					zap.String("document_id", docID),
					zap.Bool("came_back", cameBack))
				missing.add(docID, missingReasonNoChunkHistory)
				continue
			}
		}
		count, replayErr := replay(ctx, datasetID, docID, atVersion, restoreVersion)
		if replayErr != nil {
			// The engine was cleared before the write, so this document has no
			// chunks now. Naming it is what lets the caller ask for a re-parse
			// instead of reporting a restore that lost the content.
			common.Warn("Content restore: failed to replay the document chunks",
				zap.String("dataset_id", datasetID),
				zap.String("document_id", docID),
				zap.Error(replayErr))
			missing.add(docID, missingReasonReplayFailed)
			continue
		}
		restored += count
	}
	return restored, missing
}

// restoreChanges is the change set a restore records: the documents it brought
// back and the ones it removed.
//
// deletedNames is read by the caller before the removals, while those rows still
// exist: afterwards there is no name left to look up, and a history page that
// names only ids leaves the reader unable to tell what the restore dropped.
//
// The documents the restore could not bring back are left out on purpose: they
// did not change, and the API's MissingDocuments is where that belongs.
//
// A rename entry carries the name it replaced: a version older than this restore
// has no entry of its own to answer with, and the reader of that version finds
// the document under the name the restore moved it away from.
func restoreChanges(restored, deleted []string, deletedNames map[string]string, renamed map[string]documentRename) []entity.DatasetVersionChange {
	changes := make([]entity.DatasetVersionChange, 0, len(restored)+len(deleted)+len(renamed))
	names := documentNamesFor(restored)
	for _, docID := range restored {
		changes = append(changes, entity.DatasetVersionChange{
			Entity: entity.DatasetVersionChangeEntityDocument,
			Action: entity.DatasetVersionChangeActionRestore,
			DocID:  docID,
			Name:   names[docID],
		})
	}
	for _, docID := range deleted {
		changes = append(changes, entity.DatasetVersionChange{
			Entity: entity.DatasetVersionChangeEntityDocument,
			Action: entity.DatasetVersionChangeActionDelete,
			DocID:  docID,
			Name:   deletedNames[docID],
		})
	}
	// Sorted, because the rename map has no order of its own and the change set is
	// what the history page renders.
	renamedIDs := make([]string, 0, len(renamed))
	for docID := range renamed {
		renamedIDs = append(renamedIDs, docID)
	}
	sort.Strings(renamedIDs)
	for _, docID := range renamedIDs {
		move := renamed[docID]
		changes = append(changes, entity.DatasetVersionChange{
			Entity:       entity.DatasetVersionChangeEntityDocument,
			Action:       entity.DatasetVersionChangeActionRename,
			DocID:        docID,
			Name:         move.To,
			PreviousName: move.From,
		})
	}
	return changes
}

// restoreDocumentNames puts the documents of the set back under the names they
// carried at the version, and reports the ones it moved.
//
// The name at the version is the last rename recorded at or before it; a document
// no rename reached keeps the name the version's document set recorded. A rename
// recorded after the version is undone by definition: it is exactly the edit this
// restore is removing.
//
// A document that did not come back (its row was gone) has no row to rename and
// is skipped. A name another document of the dataset already holds is skipped and
// logged: the content still came back, and refusing the whole restore over a name
// would be worse.
// documentRename is a name the restore moved a document between: what the
// document was called before it, and what the version says it should be called.
// Both sides are recorded, because the version that reads backwards needs the
// name the restore replaced (see datasetversion.PreviousNamesBeforeRenames).
type documentRename struct {
	From string
	To   string
}

func restoreDocumentNames(ctx context.Context, datasetID string, target *entity.DatasetVersion, docIDs []string) (map[string]documentRename, error) {
	if len(docIDs) == 0 {
		return nil, nil
	}
	recordedNames, err := renamedNamesAt(ctx, datasetID, target.Version)
	if err != nil {
		return nil, err
	}
	// A document renamed only after the version has no rename at or before it to
	// answer with. The rename that moved it later kept the name it replaced, so
	// that is the name this restore has to put back.
	for docID, name := range datasetversion.PreviousNamesBeforeRenames(ctx, dao.DB, datasetID, target.Version, docIDs) {
		if recordedNames[docID] != "" {
			continue
		}
		if recordedNames == nil {
			recordedNames = make(map[string]string, len(docIDs))
		}
		recordedNames[docID] = name
	}
	currentNames := documentNamesFor(docIDs)
	docSvc := document.NewDocumentService()
	var renamed map[string]documentRename
	for _, docID := range docIDs {
		live, ok := currentNames[docID]
		if !ok {
			// The document is not there: it could not be put back, and it was
			// reported under MissingDocuments.
			continue
		}
		name := recordedNames[docID]
		if name == "" || name == live {
			continue
		}
		moved, renameErr := docSvc.RenameDocumentForRestore(ctx, datasetID, docID, name)
		if renameErr != nil {
			if errors.Is(renameErr, document.ErrDocumentNameTaken) {
				common.Warn("Content restore: the recorded name is taken, keeping the current one",
					zap.String("dataset_id", datasetID),
					zap.String("document_id", docID),
					zap.String("name", name))
				continue
			}
			return renamed, fmt.Errorf("rename document %s back to %q: %w", docID, name, renameErr)
		}
		if moved {
			if renamed == nil {
				renamed = make(map[string]documentRename, len(docIDs))
			}
			renamed[docID] = documentRename{From: live, To: name}
		}
	}
	return renamed, nil
}

// renamedNamesAt reports the last name each document was given at or before the
// version. Renames ride in the change set of the version that recorded them - a
// rename version, or a restore that put names back - so replaying those in order
// is what says how a document read then. A document no rename reached is absent
// from the answer.
//
// The window is the version number, not the clock: a rename that lands in the same
// millisecond as the version being restored still has its own number, so "at or
// before this version" is a comparison the timeline can answer exactly.
func renamedNamesAt(ctx context.Context, datasetID string, datasetVersion int64) (map[string]string, error) {
	if dao.DB == nil || datasetID == "" || datasetVersion < 0 {
		return nil, nil
	}
	var rows []*entity.DatasetVersion
	if err := dao.DB.WithContext(ctx).
		Select("version", "changes").
		Where("dataset_id = ? AND kind IN ? AND version <= ?",
			datasetID,
			[]string{entity.DatasetVersionKindDocumentRename, entity.DatasetVersionKindRestore},
			datasetVersion).
		Order("version ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("read the renames of dataset %s: %w", datasetID, err)
	}
	names := make(map[string]string, len(rows))
	for _, row := range rows {
		for _, change := range row.Changes {
			docID := strings.TrimSpace(change.DocID)
			if docID == "" || change.Name == "" {
				continue
			}
			if change.Entity != entity.DatasetVersionChangeEntityDocument {
				continue
			}
			if change.Action != entity.DatasetVersionChangeActionRename {
				continue
			}
			names[docID] = change.Name
		}
	}
	if len(names) == 0 {
		return nil, nil
	}
	return names, nil
}

// documentNamesFor reads the display names of documents that exist again after
// a restore.
func documentNamesFor(ids []string) map[string]string {
	if len(ids) == 0 || dao.DB == nil {
		return nil
	}
	var docs []entity.Document
	if err := dao.DB.Select("id", "name").Where("id IN ?", ids).Find(&docs).Error; err != nil {
		return nil
	}
	names := make(map[string]string, len(docs))
	for _, doc := range docs {
		if doc.Name != nil {
			names[doc.ID] = *doc.Name
		}
	}
	return names
}

// versionAnchorMillis is the point in time a version describes, in the
// millisecond resolution the chunk history is written at. It has to be exact: a
// chunk edit made in the same second as the version must still fall on the right
// side of the anchor, which is why the version records the instant in
// milliseconds and carries no second-resolution date beside it. A version with
// no timestamp at all is treated as describing the present.
func versionAnchorMillis(version *entity.DatasetVersion) int64 {
	if version != nil && version.CreateTime != nil && *version.CreateTime > 0 {
		return *version.CreateTime
	}
	return nowFunc().UnixMilli()
}

// nowFunc is the fallback clock for a version that recorded no timestamp. It is
// a seam so a test can pin the anchor; a version written by this tree always
// carries one.
var nowFunc = func() time.Time { return time.Now() }

// listDocumentIDs returns the ids of the documents the dataset currently holds.
// Only ids are read: the document set stores ids, and the rows themselves are either
// live or removed. They come back ordered so a retry of the same restore walks
// the same documents in the same order.
func listDocumentIDs(ctx context.Context, datasetID string) ([]string, error) {
	var ids []string
	if err := dao.DB.WithContext(ctx).
		Model(&entity.Document{}).
		Where("kb_id = ?", datasetID).
		Order("id").
		Pluck("id", &ids).Error; err != nil {
		return nil, fmt.Errorf("list the documents of dataset %s: %w", datasetID, err)
	}
	return ids, nil
}

// parsingDocumentsReason answers why a restore has to wait, or "" when the
// dataset has no ingestion task that is queued or running.
//
// A parse owns the documents it is writing: it appends the chunks of the very
// document a restore rebuilds, and the two paths share no lock. Refusing is the
// only safe answer — a restore started under a running parse interleaves its
// replay with the run's chunk writes and leaves a partial document behind. The
// reason names the documents so the reader knows what to stop.
func parsingDocumentsReason(ctx context.Context, datasetID string) string {
	var documentIDs []string
	if err := dao.DB.WithContext(ctx).
		Model(&entity.IngestionTask{}).
		Where("dataset_id = ? AND status IN ?", datasetID, common.ActiveTaskStatuses).
		Order("document_id").
		Pluck("document_id", &documentIDs).Error; err != nil {
		// The check is a gate, not the work: an unreadable task table must not
		// block a restore that would otherwise run, and the writes below report
		// their own failures.
		common.Warn("Content restore: failed to read the ingestion tasks of the dataset",
			zap.String("dataset_id", datasetID), zap.Error(err))
		return ""
	}
	if len(documentIDs) == 0 {
		return ""
	}

	names := documentNamesFor(documentIDs)
	listed := make([]string, 0, 3)
	for _, id := range documentIDs {
		if len(listed) == 3 {
			break
		}
		if name := names[id]; name != "" {
			listed = append(listed, name)
			continue
		}
		listed = append(listed, id)
	}
	subject := fmt.Sprintf("%d documents", len(documentIDs))
	if len(documentIDs) == 1 {
		return fmt.Sprintf(
			"cannot restore while a document is being parsed: %s; stop it and wait for a terminal state",
			listed[0])
	}
	reason := fmt.Sprintf("cannot restore while %s are being parsed: %s", subject, strings.Join(listed, ", "))
	if remaining := len(documentIDs) - len(listed); remaining > 0 {
		reason += fmt.Sprintf(" and %d more", remaining)
	}
	return reason + "; stop them and wait for a terminal state"
}

// diffDocumentSets splits the difference between a recorded document set and the
// live one: ids only in the version must be put back, ids only live must go.
func diffDocumentSets(recordedIDs, currentIDs []string) (toRestore, toDelete []string) {
	current := make(map[string]struct{}, len(currentIDs))
	for _, id := range currentIDs {
		current[id] = struct{}{}
	}
	recorded := make(map[string]struct{}, len(recordedIDs))
	for _, id := range recordedIDs {
		recorded[id] = struct{}{}
		if _, ok := current[id]; !ok {
			toRestore = append(toRestore, id)
		}
	}
	for _, id := range currentIDs {
		if _, ok := recorded[id]; !ok {
			toDelete = append(toDelete, id)
		}
	}
	return toRestore, toDelete
}
