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
	"errors"
	"fmt"
	"sort"

	"gorm.io/gorm"

	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/service/datasetversion"
)

// DocumentStatusChange is one document a visibility switch moved, with the state
// it moved to.
//
// A document's chunks follow the switch, so the whole document leaves or rejoins
// retrieval at once. That is a state of the document, not of each chunk, which is
// why it is recorded here as one row per document instead of a version row per
// chunk: a parsed document can hold thousands of chunks, and the replay applies
// the document's status to all of them (see chunk.replayChunkRow).
type DocumentStatusChange struct {
	DocumentID string
	DatasetID  string
	// Name is what the timeline shows for the change. It is read while the row
	// still exists and is not recorded in the state row.
	Name   string
	Status string
}

// documentStatusChangeAction names the change a visibility switch records: its
// direction, so the line reads "Enabled document a.pdf". A status that is
// neither of the two the switch writes ("1" and "0") keeps the edit action,
// because no action can name a direction it does not know.
func documentStatusChangeAction(status string) string {
	switch status {
	case string(entity.StatusValid):
		return entity.DatasetVersionChangeActionEnable
	case string(entity.StatusInvalid):
		return entity.DatasetVersionChangeActionDisable
	default:
		return entity.DatasetVersionChangeActionUpdate
	}
}

// documentStatusVersionKind is the dataset version kind the change set of a
// visibility switch produces: the direction, so the timeline reads "Document
// enabled" or "Document disabled" instead of one label for both.
//
// Every caller moves a batch to one status (see BatchUpdateDocumentStatus), so
// the direction names the whole version. Two cases keep the unnamed kind: a batch
// that moved documents both ways, which no single kind describes, and a change
// whose action names no direction - a switch to a status that is neither of the
// two the switch writes.
func documentStatusVersionKind(changes []entity.DatasetVersionChange) string {
	kind := ""
	for _, change := range changes {
		var current string
		switch change.Action {
		case entity.DatasetVersionChangeActionEnable:
			current = entity.DatasetVersionKindDocumentEnable
		case entity.DatasetVersionChangeActionDisable:
			current = entity.DatasetVersionKindDocumentDisable
		default:
			return entity.DatasetVersionKindDocumentStatus
		}
		if kind != "" && kind != current {
			return entity.DatasetVersionKindDocumentStatus
		}
		kind = current
	}
	if kind == "" {
		return entity.DatasetVersionKindDocumentStatus
	}
	return kind
}

// RecordDocumentStatusChangesEE records the visibility switch of a batch of
// documents as one dataset version, with one state row per document so a restore
// can put the switch back.
//
// One version for the whole batch, not one per document: the switch is a single
// action the caller took, and a dataset with a hundred documents would otherwise
// spend a hundred numbers on it. An empty batch records nothing - a toggle into
// the status a document already had is not a moment.
func RecordDocumentStatusChangesEE(ctx context.Context, db *gorm.DB, datasetID string, changes []DocumentStatusChange) error {
	if db == nil || datasetID == "" || len(changes) == 0 {
		return nil
	}

	ordered := make([]DocumentStatusChange, len(changes))
	copy(ordered, changes)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].DocumentID < ordered[j].DocumentID })

	// The state rows keep the unnamed kind, because that is the value a restore
	// reads them by (see RestoreDocumentStatusAtEE); the change set names the
	// direction, so the line reads "Enabled document a.pdf" and the version kind
	// derived from it says the same.
	rows := make([]*entity.DocumentVersion, 0, len(ordered))
	datasetChanges := make([]entity.DatasetVersionChange, 0, len(ordered))
	for _, change := range ordered {
		status := change.Status
		rows = append(rows, &entity.DocumentVersion{
			DocumentID: change.DocumentID,
			DatasetID:  datasetID,
			Kind:       entity.DatasetVersionKindDocumentStatus,
			Status:     &status,
		})
		datasetChanges = append(datasetChanges, entity.DatasetVersionChange{
			DocID:  change.DocumentID,
			Entity: entity.DatasetVersionChangeEntityDocument,
			Action: documentStatusChangeAction(change.Status),
			Name:   change.Name,
		})
	}
	datasetKind := documentStatusVersionKind(datasetChanges)

	// The switch has already been written when this runs; a caller that walked
	// away must not take the history with it, so the version outlives the request.
	ctx = context.WithoutCancel(ctx)
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		version, err := datasetversion.RecordContentVersionChanges(ctx, tx, datasetID,
			datasetKind, datasetChanges)
		if err != nil {
			return err
		}
		return writeDocumentStatusStates(ctx, tx, version, rows)
	})
}

// RecordReplayedDocumentStatusAtVersionEE records the statuses a restore put
// back under the restore's own version.
//
// The replay runs before the restore version exists - its number is allocated in
// the transaction that closes the restore - so it returns what it wrote and the
// caller records it here. Without these rows the replay would be invisible to a
// later restore: restoring back to the checkpoint taken before the restore would
// find no newer state and leave the replayed status in place.
func RecordReplayedDocumentStatusAtVersionEE(ctx context.Context, db *gorm.DB, datasetVersion int64, states []DocumentStatusChange) error {
	if db == nil || datasetVersion < 0 || len(states) == 0 {
		return nil
	}
	rows := make([]*entity.DocumentVersion, 0, len(states))
	for _, state := range states {
		status := state.Status
		rows = append(rows, &entity.DocumentVersion{
			DocumentID: state.DocumentID,
			DatasetID:  state.DatasetID,
			Kind:       entity.DatasetVersionKindDocumentStatus,
			Status:     &status,
		})
	}
	return writeDocumentStatusStates(ctx, db, datasetVersion, rows)
}

// RestoreDocumentStatusAtEE puts the visibility of the dataset's documents back
// to what it was at the version, and returns what it wrote.
//
// Only the documents whose switch moved after the version are touched: one that
// nobody toggled still holds the status it held then, so writing it would be a
// no-op with a write. A document with no state that early predates the history,
// and is left alone rather than guessed at.
//
// The chunk replay reads the status from the document row, so this has to run
// before it: a disabled document's chunks must come back hidden.
func RestoreDocumentStatusAtEE(ctx context.Context, db *gorm.DB, datasetID string, datasetVersion int64) ([]DocumentStatusChange, error) {
	if db == nil || datasetID == "" {
		return nil, nil
	}
	ctx = datasetversion.WithStateWritesSuppressed(ctx)
	versions := dao.NewDocumentVersionDAO()

	moved, err := versions.ChangedSinceVersion(ctx, db, datasetID, datasetVersion,
		entity.DatasetVersionKindDocumentStatus)
	if err != nil {
		return nil, fmt.Errorf("read the document versions of dataset %s: %w", datasetID, err)
	}
	sort.Strings(moved)

	// A document that is still gone - removed after the anchor and not brought
	// back by this restore - has no row to put a switch on.
	documentIDs, err := liveDocumentIDs(ctx, db, datasetID, moved)
	if err != nil {
		return nil, fmt.Errorf("read the live documents of dataset %s: %w", datasetID, err)
	}

	replayed := make([]DocumentStatusChange, 0, len(documentIDs))
	for _, documentID := range documentIDs {
		status, found, err := documentStatusAt(ctx, db, documentID, datasetVersion)
		if err != nil {
			return replayed, fmt.Errorf("read the status of document %s: %w", documentID, err)
		}
		if !found {
			// The document predates this history: the status it carries is the one
			// it was created with.
			continue
		}
		current, err := currentDocumentStatus(ctx, db, documentID)
		if err != nil {
			return replayed, fmt.Errorf("read the document %s: %w", documentID, err)
		}
		if current != nil && *current == status {
			continue
		}
		// The column only: a restore returns the switch, not a fresh edit, so the
		// document must not look updated in the list afterwards.
		if err := db.WithContext(ctx).Model(&entity.Document{}).
			Where("id = ?", documentID).
			UpdateColumn("status", status).Error; err != nil {
			return replayed, fmt.Errorf("restore the status of document %s: %w", documentID, err)
		}
		replayed = append(replayed, DocumentStatusChange{
			DocumentID: documentID,
			DatasetID:  datasetID,
			Status:     status,
		})
	}
	return replayed, nil
}

// documentStatusAt returns the status the document held at the version: the
// newest state at or before that number.
func documentStatusAt(ctx context.Context, db *gorm.DB, documentID string, datasetVersion int64) (string, bool, error) {
	row, err := dao.NewDocumentVersionDAO().LatestAtVersion(ctx, db, documentID, datasetVersion,
		entity.DatasetVersionKindDocumentStatus)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	if row.Status == nil {
		return "", false, nil
	}
	return *row.Status, true, nil
}

// currentDocumentStatus reads the status the document holds now.
func currentDocumentStatus(ctx context.Context, db *gorm.DB, documentID string) (*string, error) {
	var doc entity.Document
	if err := db.WithContext(ctx).Select("id", "status").Where("id = ?", documentID).First(&doc).Error; err != nil {
		return nil, err
	}
	return doc.Status, nil
}

// writeDocumentStatusStates stores status state rows under the given version.
func writeDocumentStatusStates(ctx context.Context, db *gorm.DB, datasetVersion int64, rows []*entity.DocumentVersion) error {
	if db == nil || datasetVersion < 0 || len(rows) == 0 {
		return nil
	}
	for _, row := range rows {
		row.DatasetVersion = datasetVersion
	}
	if err := dao.NewDocumentVersionDAO().CreateBatch(ctx, db, rows); err != nil {
		return fmt.Errorf("document: failed to record the document version of the status: %w", err)
	}
	return nil
}
