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
	"math"
	"sort"

	"gorm.io/gorm"

	"ragflow/internal/dao"
	"ragflow/internal/engine/history"
	"ragflow/internal/entity"
	"ragflow/internal/service/datasetversion"
)

// recordDocumentMetadataState records the metadata values a document now holds
// under the version that describes the change, so a later restore can put them
// back.
//
// It is called wherever the values can move: a document edit, a batch metadata
// update, and the parser's own writes during ingestion.
//
// The version is usually not known here: the flows that write metadata record
// their version last, so the state is parked in the flow's sink and written by
// FlushStates once the number exists (see datasetversion.WithStateSink). A
// caller that opened no sink is a change of its own - a single connector update,
// a document edit through the service - and the version is recorded here, unless
// the values did not move: a save that repeats them is not a moment.
//
// The values themselves are the only copy of what this version described, so a
// write that cannot be recorded is returned to the caller instead of swallowed:
// a row recorded without them would replay an empty map over the document's
// metadata. The row that failed is not written at all, so the history never names
// a state nobody holds. That is the rule chunk history follows for a body.
func recordDocumentMetadataState(ctx context.Context, db *gorm.DB, documentID, datasetID string, meta map[string]interface{}) error {
	if db == nil || documentID == "" || datasetID == "" {
		return nil
	}
	row, err := metadataStateRow(ctx, db, documentID, datasetID, meta)
	if err != nil {
		return err
	}
	if datasetversion.EnqueueStates(ctx, []*entity.DocumentVersion{row}) {
		// The flow writes them under the number its change records. Two writes
		// of one document in the same flow collapse onto that version's single
		// row, which is the state the flow leaves behind.
		return nil
	}
	if !metadataStateMoved(ctx, db, documentID, row) {
		return nil
	}
	return recordMetadataVersionTx(ctx, datasetID,
		[]entity.DatasetVersionChange{{
			Entity: entity.DatasetVersionChangeEntityDocument,
			Action: entity.DatasetVersionChangeActionUpdate,
			DocID:  documentID,
		}}, row)
}

// recordDocumentMetadataEditEE records one document's metadata edit: the version
// that names the change and the snapshot that makes it restorable.
func recordDocumentMetadataEditEE(ctx context.Context, datasetID string, doc *entity.Document, before, after map[string]interface{}) error {
	row, err := metadataStateRow(ctx, dao.DB, doc.ID, datasetID, after)
	if err != nil {
		return err
	}
	return recordMetadataVersionTx(ctx, datasetID,
		[]entity.DatasetVersionChange{{
			DocID:       doc.ID,
			Entity:      entity.DatasetVersionChangeEntityDocument,
			Action:      entity.DatasetVersionChangeActionUpdate,
			Name:        documentNameOf(doc),
			MetaChanges: metadataFieldChanges(before, after),
		}}, row)
}

// recordMetadataVersionTx records the version of one metadata change and the
// state that makes it restorable in a single transaction: the number addresses
// the row, so the two cannot be recorded apart.
//
// The snapshot is already in the document engine by the time this runs (see
// metadataStateRow): the transaction below stays inside the metadata database,
// which is what keeps the dataset row lock it takes short.
func recordMetadataVersionTx(ctx context.Context, datasetID string, changes []entity.DatasetVersionChange, row *entity.DocumentVersion) error {
	if dao.DB == nil {
		return errors.New("document: no database is open")
	}
	// The values are already written; a caller that walked away must not take
	// the history with it, so the version write outlives the request.
	ctx = context.WithoutCancel(ctx)
	return dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		version, err := datasetversion.RecordContentVersionChanges(ctx, tx, datasetID,
			entity.DatasetVersionKindDocumentMetadata, changes)
		if err != nil {
			return err
		}
		return writeDocumentMetadataStates(ctx, tx, version, []*entity.DocumentVersion{row})
	})
}

// recordDocumentVersionMetadata records the values a document came to hold under
// the version that recorded the change, for the callers that already have the
// number.
//
// The values take the same route as every other state row: into the
// content-addressed state, so an unchanged state costs no second copy. A store
// that refuses them fails the call, like the rest of the history.
func recordDocumentVersionMetadata(ctx context.Context, db *gorm.DB, datasetVersion int64, documentID, datasetID string, meta map[string]interface{}) error {
	if db == nil || documentID == "" || datasetID == "" || datasetVersion < 0 {
		return nil
	}
	row, err := metadataStateRow(ctx, db, documentID, datasetID, meta)
	if err != nil {
		return err
	}
	return writeDocumentMetadataStates(ctx, db, datasetVersion, []*entity.DocumentVersion{row})
}

// metadataStateRow builds the version-keyed state of one document's values,
// moving them into the document engine the way every metadata state is stored.
//
// An engine that holds no metadata snapshots is an error, not a row without
// values: the values are the only copy of what this version described, so there
// is nothing to fall back on. Versioning supports Elasticsearch and Infinity;
// the other engines report the failure when a value changes instead of recording
// a row a restore would read back empty.
func metadataStateRow(ctx context.Context, db *gorm.DB, documentID, datasetID string, meta map[string]interface{}) (*entity.DocumentVersion, error) {
	row := &entity.DocumentVersion{
		DocumentID: documentID,
		DatasetID:  datasetID,
		Kind:       entity.DatasetVersionKindDocumentMetadata,
		MetaFields: metadataSnapshot(meta),
	}
	if err := history.StashMetadata(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}

// writeDocumentMetadataStates stores metadata state rows under the given version.
func writeDocumentMetadataStates(ctx context.Context, db *gorm.DB, datasetVersion int64, rows []*entity.DocumentVersion) error {
	if db == nil || datasetVersion < 0 || len(rows) == 0 {
		return nil
	}
	for _, row := range rows {
		row.DatasetVersion = datasetVersion
	}
	if err := dao.NewDocumentVersionDAO().CreateBatch(ctx, db, rows); err != nil {
		return fmt.Errorf("document: failed to record the document version of the metadata: %w", err)
	}
	return nil
}

// metadataStateMoved reports whether the state differs from the newest one
// recorded for the document. It is only used when no flow is going to record the
// version: a write that would append a version describing the values the
// document already had is not a moment, and the history keeps one row per
// document that never moved.
func metadataStateMoved(ctx context.Context, db *gorm.DB, documentID string, row *entity.DocumentVersion) bool {
	latest, err := dao.NewDocumentVersionDAO().LatestAtVersion(
		ctx, db, documentID, math.MaxInt64, entity.DatasetVersionKindDocumentMetadata)
	if err != nil {
		// A failed read must not lose the row: append the state, which is what a
		// history of "the state after this write" is for.
		return true
	}
	// Snapshots are content addressed, so the same name is the same values.
	return latest.MetaFieldsSnapshotID != row.MetaFieldsSnapshotID
}

// parkDocumentMetadataState parks the values a document is created with as its
// first recorded state: a document is created without metadata, and that "no
// values" is a state like any other.
//
// Without the row for the version that added the document, a restore to that
// version would find nothing to put back and leave the values a later edit wrote
// in place - the document would come back without its content but with metadata
// it did not have then. It is parked, not written: the creation runs inside the
// insert transaction, where a version write of its own would wait on the dataset
// row the transaction already holds. The values are stored on the row's way into
// the table (entity.DocumentVersion.BeforeCreate), so a store that refuses them
// fails the flush that writes them.
func parkDocumentMetadataState(ctx context.Context, docs []*entity.Document) {
	if len(docs) == 0 {
		return
	}
	// An engine that keeps no metadata snapshots has nowhere to put the empty
	// state: the deployment keeps no metadata history at all, and an upload it
	// can complete must not fail over a state it would never read.
	if !history.MetadataAvailable() {
		return
	}
	rows := make([]*entity.DocumentVersion, 0, len(docs))
	for _, doc := range docs {
		if doc == nil || doc.ID == "" || doc.KbID == "" {
			continue
		}
		rows = append(rows, &entity.DocumentVersion{
			DocumentID: doc.ID,
			DatasetID:  doc.KbID,
			Kind:       entity.DatasetVersionKindDocumentMetadata,
			MetaFields: entity.JSONMap{},
		})
	}
	datasetversion.EnqueueStates(ctx, rows)
}

// ReplayedDocumentMetadata is one document's values as a restore put them back.
type ReplayedDocumentMetadata struct {
	DocumentID string
	DatasetID  string
	Meta       map[string]interface{}
}

// RecordReplayedDocumentStatesAtVersionEE records the states a restore replay
// put back under the restore's own version.
//
// The replay runs before the restore version exists - its number is allocated in
// the transaction that closes the restore - so it returns what it wrote and the
// caller records it here. Without these rows the replay would be invisible to a
// later restore: restoring back to the checkpoint taken before the restore would
// find nothing newer and leave the replayed values in place.
func RecordReplayedDocumentStatesAtVersionEE(ctx context.Context, db *gorm.DB, datasetVersion int64, settings []*entity.Document, metas []ReplayedDocumentMetadata) error {
	if db == nil || datasetVersion < 0 {
		return nil
	}
	if len(settings) > 0 {
		if err := writeDocumentParserSettingStates(ctx, db, datasetVersion, parserSettingStateRows(db, settings)); err != nil {
			return err
		}
	}
	for _, state := range metas {
		if err := recordDocumentVersionMetadata(ctx, db, datasetVersion, state.DocumentID, state.DatasetID, state.Meta); err != nil {
			return err
		}
	}
	return nil
}

// RestoreDocumentMetadataAtEE puts the metadata values of the dataset's
// documents back to what they were at the version.
//
// Only the documents whose values moved after the version are touched: a
// document nobody touched still holds the values it held then, so rewriting it
// would be a no-op with a write. A document the history knew at that version but
// recorded no values for held none - it was created with an empty map - and gets
// an empty one back; a document with no state that early at all predates the
// history and is left alone rather than guessed at.
//
// restoredFromRemoval names the documents this restore brought back from their
// removal rows. They are replayed even when no state is newer than the version:
// removing a document drops every row of its metadata store (see
// DeleteDocumentAllMetadata), so after a restore of a version that held the
// document, "no newer state" no longer means "the store still holds what it held
// then" - the store is empty and the version's state is the only copy left.
//
// anchorIsMetadataChange says the version being restored is the metadata change
// itself. The documents it recorded values for are then replayed as well, for the
// same reason: restoring that change has to produce the values it recorded,
// whatever the store holds now.
//
// The values it wrote are returned rather than recorded here: see
// RecordReplayedDocumentStatesAtVersionEE.
func RestoreDocumentMetadataAtEE(ctx context.Context, db *gorm.DB, datasetID string, datasetVersion int64, restoredFromRemoval []string, anchorIsMetadataChange bool) ([]ReplayedDocumentMetadata, error) {
	return restoreDocumentMetadataAt(ctx, db, datasetID, datasetVersion, restoredFromRemoval, anchorIsMetadataChange, &documentMetadataWriter{})
}

// metadataVersionWriter applies one version's values to a document. The replay
// reads the history and hands each row to a writer; the document service is the
// production implementation, and the interface exists so the replay can be
// tested without a document engine.
type metadataVersionWriter interface {
	writeDocumentMetadataHistory(ctx context.Context, documentID string, meta map[string]interface{}) error
}

// documentMetadataWriter writes through the document service, which owns the
// metadata store of the document engine.
//
// The service is built on the first write, not when the replay starts: a
// restore of a version whose documents never touched their metadata has nothing
// to write, and constructing a service only to not use it would make every
// restore pay for the wiring.
type documentMetadataWriter struct {
	service *DocumentService
}

func (w *documentMetadataWriter) writeDocumentMetadataHistory(ctx context.Context, documentID string, meta map[string]interface{}) error {
	if w.service == nil {
		w.service = NewDocumentService()
	}
	_, err := w.service.writeDocumentMetadataSnapshot(ctx, documentID, meta)
	return err
}

// restoreDocumentMetadataAt is the replay without the service construction, so
// a test can supply its own writer.
func restoreDocumentMetadataAt(ctx context.Context, db *gorm.DB, datasetID string, datasetVersion int64, restoredFromRemoval []string, anchorIsMetadataChange bool, writer metadataVersionWriter) ([]ReplayedDocumentMetadata, error) {
	if db == nil || datasetID == "" {
		return nil, nil
	}
	ctx = datasetversion.WithStateWritesSuppressed(ctx)
	versions := dao.NewDocumentVersionDAO()

	// The version-keyed states are the only record: a document whose values moved
	// after the target is one whose state row was produced by a newer version.
	moved, err := versions.ChangedSinceVersion(ctx, db, datasetID, datasetVersion, entity.DatasetVersionKindDocumentMetadata)
	if err != nil {
		return nil, fmt.Errorf("read the document versions of dataset %s: %w", datasetID, err)
	}
	// A document the restore brought back from its removal is replayed whether or
	// not a state is newer than the version: its store was dropped with the
	// removal, so the state at the version is the only copy of its values left.
	candidates := make(map[string]struct{}, len(moved)+len(restoredFromRemoval))
	for _, id := range moved {
		candidates[id] = struct{}{}
	}
	for _, id := range restoredFromRemoval {
		if id != "" {
			candidates[id] = struct{}{}
		}
	}
	// Restoring the metadata change itself puts back the values it recorded, even
	// though nothing about those documents moved after it.
	if anchorIsMetadataChange {
		anchored, anchoredErr := versions.StatesAtVersion(ctx, db, datasetID, datasetVersion, entity.DatasetVersionKindDocumentMetadata)
		if anchoredErr != nil {
			return nil, fmt.Errorf("read the metadata states of dataset %s at version %d: %w", datasetID, datasetVersion, anchoredErr)
		}
		for _, id := range anchored {
			candidates[id] = struct{}{}
		}
	}
	documentIDs := make([]string, 0, len(candidates))
	for id := range candidates {
		documentIDs = append(documentIDs, id)
	}
	sort.Strings(documentIDs)

	// A document that is still gone - removed after the anchor and not brought
	// back by this restore, because its row was cleaned up - has no metadata
	// to put back. Writing one would fail the replay for every document after it,
	// so the absent ones are left out instead.
	documentIDs, err = liveDocumentIDs(ctx, db, datasetID, documentIDs)
	if err != nil {
		return nil, fmt.Errorf("read the live documents of dataset %s: %w", datasetID, err)
	}
	replayed := make([]ReplayedDocumentMetadata, 0, len(documentIDs))
	for _, documentID := range documentIDs {
		state, found, err := metadataAt(ctx, db, documentID, datasetVersion)
		if err != nil {
			return nil, fmt.Errorf("read the metadata of document %s: %w", documentID, err)
		}
		if !found {
			// No values at or before the version. The values are recorded from
			// the document's creation on - the version that added it records the
			// empty map - so a document whose settings the history already knew
			// at that version held no values then, and the replay puts an empty
			// map back. A document the history did not know that early predates
			// it: what it held is not recorded anywhere, and it is left alone
			// rather than guessed at.
			known, knownErr := documentKnownAt(ctx, db, documentID, datasetVersion)
			if knownErr != nil {
				return nil, fmt.Errorf("read whether document %s predates the history: %w", documentID, knownErr)
			}
			if !known {
				continue
			}
			state.meta = entity.JSONMap{}
		}
		if err := writer.writeDocumentMetadataHistory(ctx, documentID, state.meta); err != nil {
			return replayed, fmt.Errorf("restore the metadata of document %s: %w", documentID, err)
		}
		replayed = append(replayed, ReplayedDocumentMetadata{
			DocumentID: documentID,
			DatasetID:  datasetID,
			Meta:       state.meta,
		})
	}
	return replayed, nil
}

// metadataState is the map a replay puts back.
type metadataState struct {
	meta entity.JSONMap
}

// metadataAt returns the values the document held at the version: the newest
// state at or before that number, with the values loaded back from wherever they
// are stored.
func metadataAt(ctx context.Context, db *gorm.DB, documentID string, datasetVersion int64) (metadataState, bool, error) {
	var state metadataState
	row, err := dao.NewDocumentVersionDAO().LatestAtVersion(ctx, db, documentID, datasetVersion, entity.DatasetVersionKindDocumentMetadata)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return state, false, nil
		}
		return state, false, err
	}
	staged := []*entity.DocumentVersion{row}
	if err := history.HydrateMetadata(ctx, staged); err != nil {
		return state, false, err
	}
	return metadataState{meta: row.MetaFields}, true, nil
}

// documentKnownAt reports whether the history knew the document at the version:
// its parse settings were recorded at or before it. A document created under the
// history has its settings recorded by the insert that created it, while one
// that predates the history has no state row that early - which is what tells a
// "created with no metadata" document from one nobody recorded.
func documentKnownAt(ctx context.Context, db *gorm.DB, documentID string, datasetVersion int64) (bool, error) {
	var count int64
	if err := db.WithContext(ctx).
		Model(&entity.DocumentVersion{}).
		Where("document_id = ? AND dataset_version <= ? AND kind = ?",
			documentID, datasetVersion, entity.DatasetVersionKindDocumentConfig).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// liveDocumentIDs narrows the given ids to the documents the dataset currently
// holds, preserving their order.
func liveDocumentIDs(ctx context.Context, db *gorm.DB, datasetID string, documentIDs []string) ([]string, error) {
	if len(documentIDs) == 0 {
		return nil, nil
	}
	var present []string
	if err := db.WithContext(ctx).
		Model(&entity.Document{}).
		Where("kb_id = ? AND id IN ?", datasetID, documentIDs).
		Pluck("id", &present).Error; err != nil {
		return nil, err
	}
	live := make(map[string]struct{}, len(present))
	for _, id := range present {
		live[id] = struct{}{}
	}
	kept := make([]string, 0, len(present))
	for _, id := range documentIDs {
		if _, ok := live[id]; ok {
			kept = append(kept, id)
		}
	}
	return kept, nil
}

// metadataSnapshot copies the values a state row holds, dropping the nil
// entries: the metadata store treats an absent key and a null one the same, and
// keeping both shapes would make two states that describe one map reach two
// names.
func metadataSnapshot(meta map[string]interface{}) entity.JSONMap {
	snapshot := make(entity.JSONMap, len(meta))
	for key, value := range meta {
		if value == nil {
			continue
		}
		snapshot[key] = value
	}
	return snapshot
}
