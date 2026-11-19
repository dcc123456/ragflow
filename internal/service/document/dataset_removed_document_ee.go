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

package document

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
)

// A removed document stays restorable until the dataset's owner removes its
// row. Nothing expires on a timer: a removal row, its blob and its chunk
// history are deleted together, and only by that explicit cleanup.
//
// A restore is the other way a removal goes away: it consumes the rows without
// touching the blobs, because the document that comes back takes them over.

// removedDocumentRuntimeKeys names the document columns that describe the run
// the document was in, not what it is. RestoreRemovedDocumentEE resets them
// before inserting the row, so a removed copy of them is dead weight that a
// reader of the JSON could mistake for the state a restore puts back.
var removedDocumentRuntimeKeys = []string{
	"progress",
	"progress_msg",
	"process_begin_at",
	"process_duration",
	"token_num",
	"chunk_num",
}

// recordDocumentRemovalEE copies the document row and its
// file-manager linkage into the removal row before the delete makes them
// unrecoverable. The delete is refused when the copy fails: a document that
// cannot be restored must not be removed silently, because the content history
// promises it can be.
func recordDocumentRemovalEE(ctx context.Context, doc *entity.Document, kb *entity.Knowledgebase) error {
	if dao.DB == nil || doc == nil || kb == nil {
		return nil
	}

	documentPayload, err := removedDocumentJSONMap(doc)
	if err != nil {
		return fmt.Errorf("encode document %s for the removal row: %w", doc.ID, err)
	}
	// The parse state is deliberately not removed: a restore resets it and the
	// chunk replay or the next parse recomputes it, so the stored values would
	// never be read. ProgressMsg is the reason it matters rather than being
	// cosmetic - it is longtext, and a failed parse writes a whole error trace
	// into it.
	for _, key := range removedDocumentRuntimeKeys {
		delete(documentPayload, key)
	}

	mappings, err := dao.NewFile2DocumentDAO().GetByDocumentID(ctx, dao.DB, doc.ID)
	if err != nil {
		return fmt.Errorf("load file linkage of document %s: %w", doc.ID, err)
	}
	fileDAO := dao.NewFileDAO()
	removedFiles := make([]interface{}, 0, len(mappings))
	removedMappings := make([]interface{}, 0, len(mappings))
	for _, mapping := range mappings {
		payload, mapErr := removedDocumentJSONMap(mapping)
		if mapErr != nil {
			return fmt.Errorf("encode file mapping of document %s: %w", doc.ID, mapErr)
		}
		removedMappings = append(removedMappings, payload)
		if mapping.FileID == nil {
			continue
		}
		file, fileErr := fileDAO.GetByID(ctx, dao.DB, *mapping.FileID)
		if fileErr != nil || file == nil {
			// The mapping outlived its file row; the document can still be put
			// back, just without a file-manager entry for that file.
			continue
		}
		filePayload, filePayloadErr := removedDocumentJSONMap(file)
		if filePayloadErr != nil {
			return fmt.Errorf("encode file %s of document %s: %w", file.ID, doc.ID, filePayloadErr)
		}
		removedFiles = append(removedFiles, filePayload)
	}

	row := &entity.DatasetRemovedDocument{
		ID:         common.GenerateUUID(),
		DatasetID:  kb.ID,
		DocumentID: doc.ID,
		Document:   documentPayload,
		Files: entity.JSONMap{
			"files":    removedFiles,
			"mappings": removedMappings,
		},
	}
	if err := dao.NewDatasetRemovedDocumentDAO().Create(ctx, dao.DB, row); err != nil {
		return fmt.Errorf("record document %s for the content restore: %w", doc.ID, err)
	}
	return nil
}

// errRemovalConsumed unwinds the restore transaction when the removal row is gone:
// the owner cleaned it up, or another restore took it first. The caller reports
// the document as missing rather than failing the whole restore.
var errRemovalConsumed = errors.New("removal consumed")

// RestoreRemovedDocumentEE puts a removed document back from its removal row:
// the document row, its file rows and their mappings. It reports false when the
// removal row was cleaned up (or never existed) and the document is not there
// either, which tells the caller the document cannot come back.
//
// The document returns unparsed. Its chunks left the retrieval engine with the
// delete, so the row restarts at zero chunks instead of claiming content that is
// not there; re-parsing is what brings it back.
//
// A document that is already back reports true even though its row is gone:
// the rows are consumed by the restore that put it back, and a second call that
// waited on the same lock has nothing to insert but nothing to report as missing
// either.
//
// The removal rows are consumed in the same transaction that puts the document
// back. A live document is not a removed document: leaving the rows behind would
// keep a removal that the removed-document list hides (it filters live
// documents) and the cleanup refuses (it refuses a document that exists), so the
// removal and its blobs would never be reclaimed. Only the rows are dropped; the
// file rows just inserted point at the same blobs, so removing the objects here
// would delete the restored document's own file.
func RestoreRemovedDocumentEE(ctx context.Context, datasetID, documentID string) (bool, error) {
	if dao.DB == nil || datasetID == "" || documentID == "" {
		return false, nil
	}
	removedDocumentDAO := dao.NewDatasetRemovedDocumentDAO()
	err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// The row is read inside the transaction and under its lock: the
		// cleanup locks the same rows while it deletes the blobs and the chunk
		// history, so one of the two sees a removal and the other sees none.
		row, loadErr := removedDocumentDAO.GetRestorable(ctx, tx, datasetID, documentID)
		if errors.Is(loadErr, dao.ErrRemovedDocumentNotFound) {
			// The rows can be gone because another restore consumed them a
			// moment ago: a second click on restore waits for the lock, the first
			// call puts the document back, and the second must report it as back
			// rather than as missing. Only a document that is not there either is
			// actually unrecoverable.
			var back int64
			if countErr := tx.Model(&entity.Document{}).
				Where("kb_id = ? AND id = ?", datasetID, documentID).
				Count(&back).Error; countErr != nil {
				return countErr
			}
			if back > 0 {
				return nil
			}
			return errRemovalConsumed
		}
		if loadErr != nil {
			return fmt.Errorf("load the removal row of document %s: %w", documentID, loadErr)
		}

		doc := &entity.Document{}
		if decodeErr := decodeRemovedDocumentRow(row.Document, doc); decodeErr != nil {
			return fmt.Errorf("decode removed document %s: %w", documentID, decodeErr)
		}
		files, filesErr := removedDocumentRows[entity.File](row.Files, "files")
		if filesErr != nil {
			return fmt.Errorf("decode removed files of document %s: %w", documentID, filesErr)
		}
		mappings, mappingsErr := removedDocumentRows[entity.File2Document](row.Files, "mappings")
		if mappingsErr != nil {
			return fmt.Errorf("decode removed file mappings of document %s: %w", documentID, mappingsErr)
		}

		var existing int64
		if countErr := tx.Model(&entity.Document{}).Where("id = ?", documentID).Count(&existing).Error; countErr != nil {
			return countErr
		}
		if existing > 0 {
			// Already back (a retried restore). Nothing to insert twice, but the
			// rows are still leftovers: consume them, so a live document keeps
			// no row and nothing can delete a blob on its behalf.
			return removedDocumentDAO.DeleteByDocument(ctx, tx, datasetID, documentID)
		}

		// The removal leaves the parse state out (see
		// removedDocumentRuntimeKeys); resetting it here as well keeps a row
		// written before that was true from reporting a run the restored document
		// is not in.
		doc.Progress = 0
		doc.ProgressMsg = nil
		doc.ProcessBeginAt = nil
		doc.ProcessDuration = 0
		doc.TokenNum = 0
		doc.ChunkNum = 0
		if createErr := tx.Create(doc).Error; createErr != nil {
			return createErr
		}
		// The settings the document comes back with are parked for the version
		// that closes the restore: a later restore must be able to say what it ran
		// under, and the rows written before the delete were dropped with the
		// removal. Parked, not written - this runs inside the restore's
		// transaction, where a version write of its own would wait on rows the
		// transaction already holds.
		parkDocumentParserSettingStates(ctx, tx, []*entity.Document{doc})
		// The values are the other half: the removal deleted them with the
		// document, so the state it comes back with is the empty one, recorded
		// under the version that closes the restore.
		parkDocumentMetadataState(ctx, []*entity.Document{doc})
		counter := tx.Model(&entity.Knowledgebase{}).
			Where("id = ?", datasetID).
			Update("doc_num", gorm.Expr("doc_num + 1"))
		if counter.Error != nil {
			return counter.Error
		}
		if counter.RowsAffected == 0 {
			// The dataset went away while the removal was still restorable. Put
			// the document row back with the transaction instead of leaving one
			// whose kb_id names nothing.
			return fmt.Errorf("dataset %s no longer exists", datasetID)
		}
		for _, file := range files {
			if createErr := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(file).Error; createErr != nil {
				return createErr
			}
		}
		for _, mapping := range mappings {
			if createErr := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(mapping).Error; createErr != nil {
				return createErr
			}
		}
		// Consume the removal row: the document is live again, so it is no longer a
		// removed document and there is nothing left for the cleanup to do. Only
		// the rows go — the file rows just inserted point at the same blobs, so
		// deleting the objects here would delete the restored document's file.
		return removedDocumentDAO.DeleteByDocument(ctx, tx, datasetID, documentID)
	})
	if errors.Is(err, errRemovalConsumed) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("restore document %s: %w", documentID, err)
	}
	return true, nil
}

// The blobs a removal kept are reclaimed by the dataset cleanup, through the
// dataset package's own removeRemovedDocumentFiles: it is the caller that owns the
// removal row, and it does not import this package to reach the objects.

// keepFileBlobForDocumentRestore reports whether a removed document's blob must
// stay in storage. The removal holds the file location, so a restore can point
// the rebuilt file row at the same object.
func keepFileBlobForDocumentRestore() bool { return true }

// removedDocumentJSONMap turns a row into the JSON shape the removal row stores, so a
// restore can decode it back into the same entity without a bespoke mapping.
func removedDocumentJSONMap(value interface{}) (entity.JSONMap, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoded := entity.JSONMap{}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

// decodeRemovedDocumentRow turns a removed JSON object back into the entity it was
// copied from.
func decodeRemovedDocumentRow(payload entity.JSONMap, into interface{}) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, into)
}

// removedDocumentRows decodes the list a removal keeps under key. A missing list is
// an empty one: a removal written before the list existed still restores the
// document itself.
func removedDocumentRows[T any](payload entity.JSONMap, key string) ([]*T, error) {
	items, _ := payload[key].([]interface{})
	rows := make([]*T, 0, len(items))
	for _, item := range items {
		encoded, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		row := new(T)
		if err := json.Unmarshal(encoded, row); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}
