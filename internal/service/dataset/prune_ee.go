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

package dataset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/engine/history"
	"ragflow/internal/entity"
	"ragflow/internal/service/chunkversion"
	"ragflow/internal/service/datasetversion"
	"ragflow/internal/storage"
)

// PruneUnreferencedRemovedDocuments drops the removed-document rows that no
// surviving dataset version can restore, and with them the blobs they kept in
// object storage.
//
// A removed document stays removed - its row, its file rows and their objects,
// its chunk history - so an older version can put it back. That is only true
// while some version still contains it: once the last version that held it is
// deleted, nothing can restore it, and every object a removal row keeps is
// unreachable. Deleting the version is therefore the moment to reclaim them; the
// alternative is a document that leaves the dataset forever and stays in the
// bucket forever.
//
// It lives with the dataset service, next to the version delete that calls it,
// rather than with the content restore that shares its per-document cleanup: the
// delete is the trigger, and a caller that reaches the history without the HTTP
// handler (a CLI, an internal task) must reclaim the same rows.
//
// The check is conservative by construction. The set of restorable documents is
// the union of the document sets the surviving versions recorded; a version whose
// set was never recorded (a writer whose lookup failed) could restore anything,
// so the pass drops nothing at all rather than guess. Live documents are never
// touched, and the per-document cleanup refuses one that is still there, so a
// restore landing beside this cannot lose its blobs.
//
// The caller must have authorized the dataset: this is the follow-up of a version
// delete, which already checked the permission, and re-resolving it here would
// only repeat that lookup.
//
// The returned count is the number of documents cleaned. A failure leaves the
// remaining rows in place - they are unreachable but intact - so a later
// version delete or an explicit cleanup still removes them.
func PruneUnreferencedRemovedDocuments(ctx context.Context, datasetID string) (int, error) {
	if dao.DB == nil || datasetID == "" {
		return 0, nil
	}
	return pruneUnreferencedRemovedDocuments(ctx, dao.DB, datasetID)
}

// pruneUnreferencedRemovedDocuments is the prune without the plumbing, so a test can
// point it at its own database.
func pruneUnreferencedRemovedDocuments(ctx context.Context, db *gorm.DB, datasetID string) (int, error) {
	referenced, complete, err := restorableDocumentIDs(ctx, db, datasetID)
	if err != nil {
		return 0, err
	}
	if !complete {
		// Some surviving version does not say what it held. Nothing proves an
		// row is unreachable, so nothing may be dropped.
		return 0, nil
	}

	rows, err := dao.NewDatasetRemovedDocumentDAO().ListByDataset(ctx, db, datasetID)
	if err != nil {
		return 0, fmt.Errorf("list the removed documents of dataset %s: %w", datasetID, err)
	}
	if len(rows) == 0 {
		return 0, nil
	}

	candidates := make([]string, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row == nil || row.DocumentID == "" {
			continue
		}
		if _, ok := referenced[row.DocumentID]; ok {
			continue
		}
		if _, ok := seen[row.DocumentID]; ok {
			continue
		}
		seen[row.DocumentID] = struct{}{}
		candidates = append(candidates, row.DocumentID)
	}
	if len(candidates) == 0 {
		return 0, nil
	}

	// A document that is still there has no row left to drop - its restore
	// consumed the rows - and the per-document cleanup refuses it. Filtering the
	// live ones out keeps that refusal from turning the whole pass into an error.
	live, err := LiveDocumentIDsEE(ctx, datasetID, candidates)
	if err != nil {
		return 0, err
	}

	purged := 0
	for _, documentID := range candidates {
		if _, ok := live[documentID]; ok {
			continue
		}
		cleaned, err := PurgeRemovedDocumentEE(ctx, datasetID, documentID)
		if err != nil {
			// A restore that landed between the listing above and the per-document
			// transaction brought the document back, and the cleanup refuses a
			// live one. That is the outcome this pass wanted - the document is
			// not garbage any more - so it is a skip, not a failure.
			var coded interface{ Code() common.ErrorCode }
			if errors.As(err, &coded) && coded.Code() == common.CodeConflict {
				continue
			}
			return purged, err
		}
		if cleaned {
			purged++
		}
	}
	return purged, nil
}

// restorableDocumentIDs returns the documents the dataset's surviving versions
// can restore, and whether that answer is complete.
//
// Complete is false when a version does not say what it held: its document set
// was never recorded, or it resolves to nothing because the row that stored it
// is gone. A version like that can restore any document, so a caller that only
// trusts a complete answer leaves every removal row alone.
func restorableDocumentIDs(ctx context.Context, db *gorm.DB, datasetID string) (map[string]struct{}, bool, error) {
	rows, err := dao.NewDatasetVersionDAO().ListDocumentSets(ctx, db, datasetID)
	if err != nil {
		return nil, false, fmt.Errorf("read the document sets of dataset %s: %w", datasetID, err)
	}
	if len(rows) == 0 {
		// No history at all: nothing says what the dataset held, so nothing says
		// what is unreachable either.
		return nil, false, nil
	}

	referenced := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row == nil || row.Documents == "" {
			return nil, false, nil
		}
		ids, err := datasetversion.ParseDocuments(row.Documents)
		if err != nil {
			return nil, false, fmt.Errorf("decode the document set of dataset %s version %d: %w", datasetID, row.Version, err)
		}
		for _, id := range ids {
			if id != "" {
				referenced[id] = struct{}{}
			}
		}
	}
	return referenced, true, nil
}

// PurgeRemovedDocumentEE cleans one document: its removal rows, the objects they
// kept in storage, the chunk history the removal left behind, the pictures inside
// it and the document states that were their only reader.
//
// It is the half of a removed-document cleanup that has no permission check of
// its own, shared by the explicit cleanup request (which resolves the dataset
// first) and by the prune a version delete runs.
//
// The work happens in a transaction that holds the removal rows for the whole
// operation. The lock is what closes the window a plain "does the document
// exist?" check leaves open: a restore locks the same rows while it puts the
// document back, so the two cannot interleave. Whoever gets the lock acts; the
// other either sees no rows (the removal rows were consumed, so the document is
// counted as clean) or refuses because the document came back. Without it, a
// restore landing between the check and the blob deletion had its blobs and
// chunk history deleted underneath it, leaving a live document whose files were
// gone.
//
// The cleaned flag reports whether anything was dropped; a document with no
// removal row is reported as already clean, so a retried request succeeds.
func PurgeRemovedDocumentEE(ctx context.Context, datasetID, documentID string) (bool, error) {
	removedDocumentDAO := dao.NewDatasetRemovedDocumentDAO()
	cleaned := false
	err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rows, err := removedDocumentDAO.ListByDocumentForUpdate(ctx, tx, datasetID, documentID)
		if err != nil {
			return fmt.Errorf("lock the removal rows of document %s: %w", documentID, err)
		}
		if len(rows) == 0 {
			// Nothing left to clean: a retried request, or a restore consumed
			// the rows first. Both are a success.
			return nil
		}
		var live int64
		if err := tx.Model(&entity.Document{}).
			Where("kb_id = ? AND id = ?", datasetID, documentID).
			Count(&live).Error; err != nil {
			return fmt.Errorf("check document %s: %w", documentID, err)
		}
		if live > 0 {
			return datasetVersionError{
				code:    common.CodeConflict,
				message: fmt.Sprintf("document %s still exists; its history is what an older version restores from", documentID),
			}
		}
		for _, row := range rows {
			if err := removeRemovedDocumentFiles(ctx, datasetID, documentID, row.Files); err != nil {
				return fmt.Errorf("purge the removed-document files of document %s: %w", documentID, err)
			}
		}
		// The chunk history is dropped through the same transaction the rows are
		// locked in, so a failure here rolls the removal rows back with it and
		// the cleanup can be retried.
		if err := chunkversion.PurgeDocumentHistoryEE(ctx, tx, datasetID, documentID); err != nil {
			return fmt.Errorf("purge the chunk history of document %s: %w", documentID, err)
		}
		if err := removedDocumentDAO.DeleteByDocument(ctx, tx, datasetID, documentID); err != nil {
			return fmt.Errorf("drop the removal rows of document %s: %w", documentID, err)
		}
		// The document's states go with the removal row that was their only reader. A
		// restore rebuilds the document from the removal row's own parser fields and
		// records a fresh state, so these rows describe a life the dataset can no
		// longer return to; leaving them behind would keep one row per
		// configuration change of a document that no longer exists.
		//
		//
		// The values themselves live in the document engine, which is not part
		// of this transaction, so its states are dropped after the commit: a
		// state is shared by every row that recorded the same values, so it can
		// go only with the last of them - which, here, is all of them.
		versions := dao.NewDocumentVersionDAO()
		if err := versions.DeleteByDocument(ctx, tx, documentID); err != nil {
			return fmt.Errorf("drop the document versions of document %s: %w", documentID, err)
		}
		cleaned = true
		return nil
	})
	if err != nil {
		return false, err
	}
	if !cleaned {
		// No removal row was removed, so the document either came back or was
		// cleaned before: its states, if any, belong to a live document now.
		return false, nil
	}
	// A failure here leaves states no surviving row reads, which is litter
	// rather than data loss - and it must not undo a purge whose rows are
	// already committed.
	if err := history.DeleteMetadataDocument(ctx, datasetID, documentID); err != nil &&
		!errors.Is(err, history.ErrMetadataUnavailable) {
		common.Warn("Content purge: failed to remove the metadata history states",
			zap.String("doc_id", documentID), zap.Error(err))
	}
	return true, nil
}

// LiveDocumentIDsEE reports which of the given ids still exist in the dataset.
func LiveDocumentIDsEE(ctx context.Context, datasetID string, documentIDs []string) (map[string]struct{}, error) {
	live := make(map[string]struct{}, len(documentIDs))
	if len(documentIDs) == 0 {
		return live, nil
	}
	var found []string
	if err := dao.DB.WithContext(ctx).
		Model(&entity.Document{}).
		Where("kb_id = ? AND id IN ?", datasetID, documentIDs).
		Pluck("id", &found).Error; err != nil {
		return nil, fmt.Errorf("check the documents of dataset %s: %w", datasetID, err)
	}
	for _, id := range found {
		live[id] = struct{}{}
	}
	return live, nil
}

// removedDocumentFiles decodes the file list a removed-document row keeps. A
// missing list is an empty one: a removal written before the list existed still
// restores the document itself, and has no blob to reclaim.
func removedDocumentFiles(files entity.JSONMap) ([]*entity.File, error) {
	items, _ := files["files"].([]interface{})
	rows := make([]*entity.File, 0, len(items))
	for _, item := range items {
		encoded, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		row := new(entity.File)
		if err := json.Unmarshal(encoded, row); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// removeRemovedDocumentFiles drops the blobs a removed document kept for a
// possible restore. The removal row holds the file locations, so a restore can
// point the rebuilt row at the same object; once nothing can restore it, those
// objects are unreachable and are reclaimed here.
//
// Best effort per object: the blob is already unreachable when the row is gone,
// so one missing object must not stop the rest of the cleanup. A storage backend
// that was never configured is not an error either - there is nothing to delete.
func removeRemovedDocumentFiles(ctx context.Context, datasetID, documentID string, files entity.JSONMap) error {
	storageImpl := storage.GetStorageFactory().GetStorage()
	if storageImpl == nil {
		return nil
	}
	removedFiles, err := removedDocumentFiles(files)
	if err != nil {
		return fmt.Errorf("decode the removed files of document %s: %w", documentID, err)
	}
	for _, file := range removedFiles {
		if file.Location == nil || *file.Location == "" {
			continue
		}
		if removeErr := storage.RemoveBlob(ctx, storageImpl, datasetID, *file.Location); removeErr != nil {
			common.Warn("Content purge: failed to remove a removed file",
				zap.String("document_id", documentID), zap.Error(removeErr))
		}
	}
	return nil
}
