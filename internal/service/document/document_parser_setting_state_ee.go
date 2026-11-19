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

// recordDocumentParserSettingStates records the parse settings of the given
// documents under the version that describes the change, so a later restore can
// put those settings back.
//
// It is called wherever the settings can move: the document is created, one
// document is edited, and the dataset's settings are applied to every document.
//
// The version is not known here: the flows that write settings record their
// version last, so the states are parked in the flow's sink and written by
// FlushStates once the number exists (see datasetversion.WithStateSink). A
// caller that opened no sink gets nothing recorded, which is why every flow that
// creates documents opens one.
//
// The write is not best effort: the states are what a restore reads, so a flow
// that cannot record them reports the failure instead of leaving an edit with no
// history behind it.
func recordDocumentParserSettingStates(ctx context.Context, db *gorm.DB, docs []*entity.Document) error {
	rows := parserSettingStateRows(db, docs)
	if len(rows) == 0 {
		return nil
	}
	if datasetversion.EnqueueStates(ctx, rows) {
		return nil
	}
	return recordOwnParserSettingVersions(ctx, db, rows)
}

// parkDocumentParserSettingStates is the recorder for a caller that already owns
// a transaction: the states are parked in the flow's sink and written by
// FlushStates, never by a version write of their own - that write would wait on
// the dataset row the open transaction already holds.
func parkDocumentParserSettingStates(ctx context.Context, db *gorm.DB, docs []*entity.Document) {
	datasetversion.EnqueueStates(ctx, parserSettingStateRows(db, docs))
}

// recordOwnParserSettingVersions is the fallback for a flow with no state sink
// (a request that rewrites one document's settings before parsing it): the
// settings moved, so each dataset among the rows records the version that
// describes it and the rows are written under that number.
func recordOwnParserSettingVersions(ctx context.Context, db *gorm.DB, rows []*entity.DocumentVersion) error {
	groups := make(map[string][]*entity.DocumentVersion, 1)
	for _, row := range rows {
		if row == nil || row.DatasetID == "" {
			continue
		}
		groups[row.DatasetID] = append(groups[row.DatasetID], row)
	}
	for datasetID, group := range groups {
		changes := make([]entity.DatasetVersionChange, 0, len(group))
		for _, row := range group {
			changes = append(changes, entity.DatasetVersionChange{
				Entity: entity.DatasetVersionChangeEntityDocument,
				Action: entity.DatasetVersionChangeActionUpdate,
				DocID:  row.DocumentID,
			})
		}
		version, err := datasetversion.RecordContentVersionChangesOwnTx(
			ctx, datasetID, entity.DatasetVersionKindDocumentConfig, changes)
		if err != nil {
			return err
		}
		if err := writeDocumentParserSettingStates(ctx, db, version, group); err != nil {
			return err
		}
	}
	return nil
}

// recordDocumentVersionSettings writes the settings the given documents carry
// under the version that just recorded the change.
//
// The state itself is read from the documents the caller already updated, so a
// concurrent edit cannot be mistaken for this one.
func recordDocumentVersionSettings(ctx context.Context, db *gorm.DB, datasetVersion int64, docs []*entity.Document) error {
	return writeDocumentParserSettingStates(ctx, db, datasetVersion, parserSettingStateRows(db, docs))
}

// recordDocumentVersionSettingsTx records a settings version together with the
// settings that make it restorable, in one transaction.
func recordDocumentVersionSettingsTx(ctx context.Context, datasetID string, changes []entity.DatasetVersionChange, docs []*entity.Document) error {
	if dao.DB == nil {
		return errors.New("document: no database is open")
	}
	rows := parserSettingStateRows(dao.DB, docs)
	// The settings are already written; the history outlives a caller that
	// walked away, the same way a parse's does.
	ctx = context.WithoutCancel(ctx)
	return dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		version, err := datasetversion.RecordContentVersionChanges(ctx, tx, datasetID,
			entity.DatasetVersionKindDocumentConfig, changes)
		if err != nil {
			return err
		}
		return writeDocumentParserSettingStates(ctx, tx, version, rows)
	})
}

// parserSettingStateRows builds the states one batch of documents runs under.
//
// One moment for the batch, taken from the metadata database: the column is not
// the identity (the version is) but a reader of the history shows it, and it is
// the moment the settings became true as far as the engine is concerned.
func parserSettingStateRows(db *gorm.DB, docs []*entity.Document) []*entity.DocumentVersion {
	if len(docs) == 0 {
		return nil
	}
	rows := make([]*entity.DocumentVersion, 0, len(docs))
	for _, doc := range docs {
		if doc == nil || doc.ID == "" || doc.KbID == "" {
			continue
		}
		rows = append(rows, &entity.DocumentVersion{
			DocumentID:        doc.ID,
			DatasetID:         doc.KbID,
			Kind:              entity.DatasetVersionKindDocumentConfig,
			ParserID:          doc.ParserID,
			PipelineID:        doc.PipelineID,
			PipelineVersionID: doc.PipelineVersionID,
			ParserConfig:      cloneParserConfigForDocument(doc.ParserConfig),
		})
	}
	return rows
}

// writeDocumentParserSettingStates stores state rows under the given version.
func writeDocumentParserSettingStates(ctx context.Context, db *gorm.DB, datasetVersion int64, rows []*entity.DocumentVersion) error {
	if db == nil || datasetVersion < 0 || len(rows) == 0 {
		return nil
	}
	for _, row := range rows {
		row.DatasetVersion = datasetVersion
	}
	if err := dao.NewDocumentVersionDAO().CreateBatch(ctx, db, rows); err != nil {
		return fmt.Errorf("document: failed to record the parser settings of a version: %w", err)
	}
	return nil
}

// RestoreParserSettingsAtEE puts the parse settings of the dataset's documents
// back to what they were at the version.
//
// Only the documents whose settings moved after the version are touched: a
// document nobody touched still carries the settings it had then, so rewriting
// it would be a no-op with a write. A document with no state row that early
// predates the history, and is left alone rather than guessed at.
//
// The settings it wrote are returned rather than recorded here: the restore has
// no version number yet (it is allocated in the transaction that closes the
// restore), so the caller records them under it once it has one
// (RecordReplayedDocumentStatesAtVersionEE).
func RestoreParserSettingsAtEE(ctx context.Context, db *gorm.DB, datasetID string, datasetVersion int64) ([]*entity.Document, error) {
	if db == nil || datasetID == "" {
		return nil, nil
	}
	ctx = datasetversion.WithStateWritesSuppressed(ctx)
	versions := dao.NewDocumentVersionDAO()

	moved, err := versions.ChangedSinceVersion(ctx, db, datasetID, datasetVersion, entity.DatasetVersionKindDocumentConfig)
	if err != nil {
		return nil, fmt.Errorf("read the document versions of dataset %s: %w", datasetID, err)
	}
	documentIDs := make([]string, 0, len(moved))
	documentIDs = append(documentIDs, moved...)
	sort.Strings(documentIDs)

	replayed := make([]*entity.Document, 0, len(documentIDs))
	for _, documentID := range documentIDs {
		state, found, err := parserSettingsAt(ctx, db, documentID, datasetVersion)
		if err != nil {
			return nil, fmt.Errorf("read the parser settings of document %s: %w", documentID, err)
		}
		if !found {
			// The document existed before this history started recording; the
			// settings it carries now are the ones it was created with.
			continue
		}
		if err := db.WithContext(ctx).
			Model(&entity.Document{}).
			Where("id = ? AND kb_id = ?", documentID, datasetID).
			Updates(map[string]interface{}{
				"parser_id":           state.parserID,
				"pipeline_id":         state.pipelineID,
				"pipeline_version_id": state.pipelineVersionID,
				"parser_config":       state.parserConfig,
			}).Error; err != nil {
			return nil, fmt.Errorf("restore the parser settings of document %s: %w", documentID, err)
		}
		replayed = append(replayed, &entity.Document{
			ID: documentID, KbID: datasetID,
			ParserID: state.parserID, PipelineID: state.pipelineID,
			PipelineVersionID: state.pipelineVersionID, ParserConfig: state.parserConfig,
		})
	}
	return replayed, nil
}

// parserSettingsState is the value a replay puts back.
type parserSettingsState struct {
	parserID          string
	pipelineID        *string
	pipelineVersionID *string
	parserConfig      entity.JSONMap
}

// parserSettingsAt returns the settings the document ran under at the version:
// the newest state at or before that number.
func parserSettingsAt(ctx context.Context, db *gorm.DB, documentID string, datasetVersion int64) (parserSettingsState, bool, error) {
	var state parserSettingsState
	row, err := dao.NewDocumentVersionDAO().LatestAtVersion(ctx, db, documentID, datasetVersion, entity.DatasetVersionKindDocumentConfig)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return state, false, nil
		}
		return state, false, err
	}
	return parserSettingsState{
		parserID:          row.ParserID,
		pipelineID:        row.PipelineID,
		pipelineVersionID: row.PipelineVersionID,
		parserConfig:      row.ParserConfig,
	}, true, nil
}
