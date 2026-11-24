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
	"strings"
	"testing"

	"gorm.io/gorm"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/documentset"
	"ragflow/internal/entity"
	pipelinepkg "ragflow/internal/ingestion/pipeline"
	"ragflow/internal/permission"
	"ragflow/internal/service"
	"ragflow/internal/service/datasetversion"
)

// stringValueOf reads an optional column the way the service does: a NULL string
// column is an empty value, not a missing one.
func stringValueOf(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// listDatasetVersions returns the stored history, oldest first, with the
// configuration each version ran under resolved from its own table - the snapshot
// is not a column of the version row, so a raw read would leave it empty.
func listDatasetVersions(t *testing.T, datasetID string) []*entity.DatasetVersion {
	t.Helper()
	var rows []*entity.DatasetVersion
	if err := dao.DB.Where("dataset_id = ?", datasetID).Order("version").Find(&rows).Error; err != nil {
		t.Fatalf("list dataset versions: %v", err)
	}
	for _, row := range rows {
		if row.ConfigHash == nil {
			continue
		}
		snapshot, err := dao.NewDatasetConfigSnapshotDAO().Get(t.Context(), dao.DB, datasetID, *row.ConfigHash)
		if err != nil {
			t.Fatalf("resolve the configuration of version %d: %v", row.Version, err)
		}
		row.Config = snapshot
	}
	return rows
}

// TestUpdateDatasetRecordsConfigVersion pins the recording contract: the first
// configuration change seeds a baseline from the pre-change state and appends
// the state it produced, and the dataset's counters follow.
func TestUpdateDatasetRecordsConfigVersion(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	name := "Renamed"
	if _, code, err := testDatasetUpdateService(t).UpdateDataset(
		withDatasetTestContext(t.Context(), "tenant-1", "tenant-1"),
		"kb-1",
		service.UpdateDatasetRequest{Name: &name},
	); err != nil {
		t.Fatalf("update: code=%d err=%v", code, err)
	}

	rows := listDatasetVersions(t, "kb-1")
	if len(rows) != 2 {
		t.Fatalf("versions = %d, want 2 (baseline, config)", len(rows))
	}
	if rows[0].Version != 0 || rows[0].Kind != entity.DatasetVersionKindBaseline {
		t.Fatalf("first row = v%d/%s, want v0/baseline", rows[0].Version, rows[0].Kind)
	}
	if got := rows[0].Config["name"]; got != "Dataset" {
		t.Fatalf("baseline name = %v, want the pre-change name", got)
	}
	if rows[1].Version != 1 || rows[1].Kind != entity.DatasetVersionKindDatasetConfig {
		t.Fatalf("second row = v%d/%s, want v1/config", rows[1].Version, rows[1].Kind)
	}
	if got := rows[1].Config["name"]; got != "Renamed" {
		t.Fatalf("config name = %v, want the new name", got)
	}
	if rows[1].CreatedBy != "tenant-1" {
		t.Fatalf("created_by = %q, want the acting user", rows[1].CreatedBy)
	}

	var kb entity.Knowledgebase
	if err := db.Where("id = ?", "kb-1").Take(&kb).Error; err != nil {
		t.Fatalf("reload dataset: %v", err)
	}
	if kb.Version != 1 {
		t.Fatalf("dataset version = %d, want 1", kb.Version)
	}
}

// TestUpdateDatasetSkipsUnchangedConfig keeps the history free of rows that
// merely repeat the current state: clients that resend every field on save must
// not grow it on every keystroke.
func TestUpdateDatasetSkipsUnchangedConfig(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	svc := testDatasetUpdateService(t)
	description := "changed"
	for i := 0; i < 2; i++ {
		if _, code, err := svc.UpdateDataset(ctx, "kb-1", service.UpdateDatasetRequest{Description: &description}); err != nil {
			t.Fatalf("update #%d: code=%d err=%v", i+1, code, err)
		}
	}

	rows := listDatasetVersions(t, "kb-1")
	if len(rows) != 2 {
		t.Fatalf("versions = %d, want 2 (baseline and the real change)", len(rows))
	}

	var kb entity.Knowledgebase
	if err := db.Where("id = ?", "kb-1").Take(&kb).Error; err != nil {
		t.Fatalf("reload dataset: %v", err)
	}
	if kb.Version != 1 {
		t.Fatalf("dataset version = %d, want 1", kb.Version)
	}
}

// TestUpdateDatasetSkipsReserialisedParserConfig pins the dedupe on the rebuilt
// state: the settings form sends the whole parser_config on every save, in its
// own shape, so a save that edits something else still submits a configuration
// that is not a verbatim copy of the stored one. The rebuild lands on the stored
// state, so nothing is written and no version is recorded.
func TestUpdateDatasetSkipsReserialisedParserConfig(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	// The stored configuration is the canonical shape: what the builder produced
	// the last time the parsing settings were written.
	dsl, err := service.LoadPipelineDSL(t.Context(), false, string(entity.ParserTypeNaive), nil, nil)
	if err != nil || len(dsl) == 0 {
		t.Fatalf("load the builtin DSL: %v", err)
	}
	stored, err := pipelinepkg.BuildParserConfig(dsl, nil)
	if err != nil {
		t.Fatalf("build the stored parser config: %v", err)
	}
	if err := db.Model(&entity.Knowledgebase{}).Where("id = ?", "kb-1").
		Update("parser_config", stored).Error; err != nil {
		t.Fatalf("store the canonical parser config: %v", err)
	}

	// The form's round trip: the same settings carrying a parameter the builder no
	// longer knows. It is not a verbatim copy of what is stored, so the request
	// gets past the presence check - and rebuilds to the stored state.
	submitted := entity.JSONMap{}
	for componentID, params := range stored {
		copied := map[string]interface{}{"retired_param": "dropped by the rebuild"}
		if current, ok := params.(map[string]interface{}); ok {
			for key, value := range current {
				copied[key] = value
			}
		}
		submitted[componentID] = copied
	}

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	svc := testDatasetUpdateService(t)
	if _, code, err := svc.UpdateDataset(ctx, "kb-1", service.UpdateDatasetRequest{ParserConfig: submitted}); err != nil {
		t.Fatalf("update: code=%d err=%v", code, err)
	}

	var kb entity.Knowledgebase
	if err := db.Where("id = ?", "kb-1").Take(&kb).Error; err != nil {
		t.Fatalf("reload dataset: %v", err)
	}
	if rows := listDatasetVersions(t, "kb-1"); len(rows) != 0 {
		t.Fatalf("versions = %d, want none: the save described the state the dataset already had", len(rows))
	}
	if kb.Version != 0 {
		t.Fatalf("dataset version = %d, want 0", kb.Version)
	}
	if !datasetJSONEqual(kb.ParserConfig, stored) {
		t.Fatalf("parser_config = %#v, want the stored state untouched", kb.ParserConfig)
	}

	// The comparison is against the head, not the history: a change that lands
	// back on a state an older version already holds is still a change, and is
	// recorded as one - on the same snapshot row, under a new version number.
	renamed := "Renamed"
	if _, code, err := svc.UpdateDataset(ctx, "kb-1", service.UpdateDatasetRequest{Name: &renamed}); err != nil {
		t.Fatalf("rename: code=%d err=%v", code, err)
	}
	original := "Dataset"
	if _, code, err := svc.UpdateDataset(ctx, "kb-1", service.UpdateDatasetRequest{Name: &original}); err != nil {
		t.Fatalf("rename back: code=%d err=%v", code, err)
	}
	rows := listDatasetVersions(t, "kb-1")
	if len(rows) != 3 {
		t.Fatalf("versions = %d, want 3 (baseline, rename, rename back)", len(rows))
	}
	if rows[2].Kind != entity.DatasetVersionKindDatasetConfig || rows[2].Version != 2 {
		t.Fatalf("last row = v%d/%s, want v2/config", rows[2].Version, rows[2].Kind)
	}
	if rows[0].ConfigHash == nil || rows[2].ConfigHash == nil || *rows[0].ConfigHash != *rows[2].ConfigHash {
		t.Fatalf("hashes = %v, %v; want the returned state to share the baseline's snapshot",
			rows[0].ConfigHash, rows[2].ConfigHash)
	}
}

// TestListAndGetDatasetVersions covers the read side: newest first with the
// current version, and one version with its snapshot.
func TestListAndGetDatasetVersions(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	svc := testDatasetUpdateService(t)
	description := "first change"
	if _, code, err := svc.UpdateDataset(ctx, "kb-1", service.UpdateDatasetRequest{Description: &description}); err != nil {
		t.Fatalf("update: code=%d err=%v", code, err)
	}

	list, err := svc.ListDatasetVersions(ctx, &ListDatasetVersionsRequest{DatasetID: "kb-1"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if list.CurrentVersion != 1 {
		t.Fatalf("current version = %d, want 1", list.CurrentVersion)
	}
	if len(list.Versions) != 2 {
		t.Fatalf("versions = %d, want 2", len(list.Versions))
	}
	if list.Versions[0].Version != 1 || list.Versions[1].Version != 0 {
		t.Fatalf("versions order = %d,%d, want 1,0", list.Versions[0].Version, list.Versions[1].Version)
	}

	detail, err := svc.GetDatasetVersion(ctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: 1})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := detail.Config["description"]; got != "first change" {
		t.Fatalf("description = %v, want the recorded value", got)
	}

	if _, err := svc.GetDatasetVersion(ctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: 9}); err == nil {
		t.Fatal("expected an unknown version to fail")
	} else {
		var coded interface{ Code() common.ErrorCode }
		if !errors.As(err, &coded) || coded.Code() != common.CodeNotFound {
			t.Fatalf("error = %v, want CodeNotFound", err)
		}
	}
}

// TestGetDatasetVersionReturnsChangeSet covers the read side of the change set:
// the detail endpoint reports the documents and chunks a version touched, in the
// order the DAO returns them.
func TestGetDatasetVersionReturnsChangeSet(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	svc := testDatasetUpdateService(t)
	description := "changed"
	if _, code, err := svc.UpdateDataset(ctx, "kb-1", service.UpdateDatasetRequest{Description: &description}); err != nil {
		t.Fatalf("update: code=%d err=%v", code, err)
	}

	// The writer stores the set in the order it renders: documents first. A seed
	// that bypasses the writer has to do the same, or it would test an order no
	// version ever has.
	if err := db.Model(&entity.DatasetVersion{}).
		Where("dataset_id = ? AND version = ?", "kb-1", 1).
		Update("changes", entity.DatasetVersionChanges{
			{
				// A document entry names itself once: its doc id is its identity.
				DocID: "doc-1", Entity: entity.DatasetVersionChangeEntityDocument,
				Action: entity.DatasetVersionChangeActionAdd, Name: "manual.pdf",
			},
			{
				// A chunk entry stores its chunk and nothing else: the text is read
				// from the chunk history when the version is opened.
				DocID: "doc-1", Entity: entity.DatasetVersionChangeEntityChunk,
				Action: entity.DatasetVersionChangeActionUpdate, ChunkID: "chunk-1",
			},
			{
				// A chunk whose document has left the dataset: the entry still names
				// the chunk, and the document name simply stays empty.
				DocID: "doc-gone", Entity: entity.DatasetVersionChangeEntityChunk,
				Action: entity.DatasetVersionChangeActionDelete, ChunkID: "chunk-2",
			},
		}).Error; err != nil {
		t.Fatalf("seed change set: %v", err)
	}
	docName := "manual.pdf"
	if err := dao.DB.Create(&entity.Document{ID: "doc-1", KbID: "kb-1", Name: &docName, ParserConfig: entity.JSONMap{}}).Error; err != nil {
		t.Fatalf("seed document: %v", err)
	}

	// The text a chunk entry shows is replayed from the chunk history at the
	// version's anchor, so seed the history the write would have left behind:
	// an edit of chunk-1, and for chunk-2 an edit a removal then covered. The
	// rows are backdated because the writer records a chunk change before the
	// version that describes it.
	var versionRow entity.DatasetVersion
	if err := db.Where("dataset_id = ? AND version = ?", "kb-1", 1).Take(&versionRow).Error; err != nil {
		t.Fatalf("load version: %v", err)
	}
	anchor := *versionRow.CreateTime
	editTime, beforeEditTime, deleteTime := anchor-2000, anchor-3000, anchor-1000
	history := []entity.ChunkVersion{
		{ChunkID: "chunk-1", Version: 1, DocID: "doc-1", DatasetID: "kb-1", DatasetVersion: 1,
			Kind: entity.ChunkVersionKindEdit, Snapshot: entity.JSONMap{"content_with_weight": "body"},
			CreateTime: &editTime},
		{ChunkID: "chunk-2", Version: 1, DocID: "doc-gone", DatasetID: "kb-1", DatasetVersion: 1,
			Kind: entity.ChunkVersionKindEdit, Snapshot: entity.JSONMap{"content_with_weight": "removed body"},
			CreateTime: &beforeEditTime},
		{ChunkID: "chunk-2", Version: 2, DocID: "doc-gone", DatasetID: "kb-1", DatasetVersion: 1,
			Kind: entity.ChunkVersionKindDelete, Snapshot: entity.JSONMap{},
			CreateTime: &deleteTime},
	}
	if err := dao.DB.Create(&history).Error; err != nil {
		t.Fatalf("seed chunk history: %v", err)
	}

	detail, err := svc.GetDatasetVersion(ctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: 1})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// The client renders the millisecond instant, not the server-local date
	// string: the latter is an offset away in another time zone.
	if detail.CreateTime <= 0 {
		t.Fatalf("detail create_time = %d, want the millisecond timestamp", detail.CreateTime)
	}
	if len(detail.Changes) != 3 {
		t.Fatalf("changes = %d, want 3", len(detail.Changes))
	}
	// A configuration version lists what it moved, diffed against the previous
	// surviving version: version 1 changed the description, and the baseline
	// holds the state it started from.
	if len(detail.ConfigChanges) != 1 || detail.ConfigChanges[0].Field != "description" {
		t.Fatalf("config changes = %#v, want the description", detail.ConfigChanges)
	}
	if got := detail.ConfigChanges[0].After; got != "changed" {
		t.Fatalf("description after = %v, want the value the update wrote", got)
	}
	// Ordered by entity: document before chunk.
	if detail.Changes[0].Entity != entity.DatasetVersionChangeEntityDocument || detail.Changes[0].Name != "manual.pdf" {
		t.Fatalf("first change = %#v, want the document", detail.Changes[0])
	}
	// A chunk entry stores only its document's id, so the name is resolved on
	// read: the panel has to say which document the chunk is in. Look the entries
	// up by what they name rather than by position.
	byChunk := make(map[string]DatasetVersionChangeDetail, len(detail.Changes))
	for _, change := range detail.Changes {
		byChunk[change.ChunkID] = change
	}
	if got := byChunk["chunk-1"]; got.Action != entity.DatasetVersionChangeActionUpdate || got.DocName != "manual.pdf" {
		t.Fatalf("chunk-1 change = %#v, want the edit with its document name", got)
	}
	// A chunk entry carries no text: the change list names the chunk and its
	// document, and the text the chunk held at this version is read from the
	// chunk's own history instead of being replayed into every entry.
	if got := byChunk["chunk-1"].Name; got != "" {
		t.Fatalf("chunk-1 text = %q, want no replayed text in the change set", got)
	}
	if got := byChunk["chunk-2"]; got.DocName != "" {
		t.Fatalf("chunk-2 doc_name = %q, want empty for a document that left the dataset", got.DocName)
	}
	if got := byChunk["chunk-2"].Name; got != "" {
		t.Fatalf("chunk-2 text = %q, want no replayed text even for a removed chunk", got)
	}
	// A document entry carries no chunk id: the one id it has is the document.
	if got := detail.Changes[0]; got.ChunkID != "" || got.DocID != "doc-1" {
		t.Fatalf("document entry = %#v, want the document id only", got)
	}

	// A version without entries reports an empty list, not a null the UI has to
	// special-case.
	baseline, err := svc.GetDatasetVersion(ctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: 0})
	if err != nil {
		t.Fatalf("get baseline: %v", err)
	}
	if baseline.Changes == nil || len(baseline.Changes) != 0 {
		t.Fatalf("baseline changes = %#v, want an empty list", baseline.Changes)
	}
}

// TestGetDatasetVersionCarriesMetaChanges pins the metadata diff the panel
// renders: the values live in the metadata store, which has no diff of its own,
// so the change entry is the only place the edit is written down and the read
// path has to pass it through rather than drop it.
// TestGetDatasetVersionCarriesPreviousName pins the rename entry: the panel reads
// it as "old name to new name", and the row is the only place the old name
// survives once the document row carries the new one.
func TestGetDatasetVersionCarriesPreviousName(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	svc := testDatasetUpdateService(t)
	description := "changed"
	if _, code, err := svc.UpdateDataset(ctx, "kb-1", service.UpdateDatasetRequest{Description: &description}); err != nil {
		t.Fatalf("update: code=%d err=%v", code, err)
	}

	if err := db.Model(&entity.DatasetVersion{}).
		Where("dataset_id = ? AND version = ?", "kb-1", 1).
		Update("changes", entity.DatasetVersionChanges{
			{
				DocID: "doc-1", Entity: entity.DatasetVersionChangeEntityDocument,
				Action:       entity.DatasetVersionChangeActionRename,
				Name:         "三国人物2222.txt",
				PreviousName: "三国人物.txt",
			},
		}).Error; err != nil {
		t.Fatalf("seed change set: %v", err)
	}
	docName := "三国人物2222.txt"
	if err := dao.DB.Create(&entity.Document{ID: "doc-1", KbID: "kb-1", Name: &docName, ParserConfig: entity.JSONMap{}}).Error; err != nil {
		t.Fatalf("seed document: %v", err)
	}

	detail, err := svc.GetDatasetVersion(ctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: 1})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(detail.Changes) != 1 {
		t.Fatalf("changes = %d, want 1", len(detail.Changes))
	}
	if got := detail.Changes[0]; got.Name != "三国人物2222.txt" || got.PreviousName != "三国人物.txt" {
		t.Fatalf("rename entry = %+v, want both names", got)
	}
}

func TestGetDatasetVersionCarriesMetaChanges(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	svc := testDatasetUpdateService(t)
	description := "changed"
	if _, code, err := svc.UpdateDataset(ctx, "kb-1", service.UpdateDatasetRequest{Description: &description}); err != nil {
		t.Fatalf("update: code=%d err=%v", code, err)
	}

	if err := db.Model(&entity.DatasetVersion{}).
		Where("dataset_id = ? AND version = ?", "kb-1", 1).
		Updates(map[string]interface{}{
			"kind": entity.DatasetVersionKindDocumentMetadata,
			"changes": entity.DatasetVersionChanges{
				{
					DocID:  "doc-1",
					Entity: entity.DatasetVersionChangeEntityDocument,
					Action: entity.DatasetVersionChangeActionUpdate,
					Name:   "manual.pdf",
					MetaChanges: []entity.MetadataFieldChange{
						{Key: "author", Before: json.RawMessage(`"alice"`), After: json.RawMessage(`"bob"`)},
						// An introduced key keeps an absent before rather than an
						// empty one: the page tells the two apart.
						{Key: "tags", After: json.RawMessage(`["a","b"]`)},
					},
				},
			},
		}).Error; err != nil {
		t.Fatalf("seed change set: %v", err)
	}

	detail, err := svc.GetDatasetVersion(ctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: 1})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(detail.Changes) != 1 {
		t.Fatalf("changes = %d, want 1", len(detail.Changes))
	}
	got := detail.Changes[0].MetaChanges
	if len(got) != 2 {
		t.Fatalf("meta changes = %d, want 2", len(got))
	}
	if got[0].Key != "author" || string(got[0].Before) != `"alice"` || string(got[0].After) != `"bob"` {
		t.Fatalf("first meta change = %+v, want author alice -> bob", got[0])
	}
	if got[1].Key != "tags" || got[1].Before != nil {
		t.Fatalf("second meta change = %+v, want an introduced key with no before", got[1])
	}
}

// TestDocumentConfigVersionReportsPerDocumentSettingsDiff pins what a
// document-configuration version says about each document it touched: the parse
// settings that differ from the ones the document ran under before, as one group
// per document. The version stores no diff of its own - the settings live in the
// per-document states - so the read side has to go and fetch them.
func TestDocumentConfigVersionReportsPerDocumentSettingsDiff(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	anchor := int64(1762764000000)
	name := "handbook.pdf"
	if err := db.Create(&entity.Document{
		ID: "doc-1", KbID: "kb-1", Name: &name, ParserConfig: entity.JSONMap{},
	}).Error; err != nil {
		t.Fatalf("seed document: %v", err)
	}
	// The settings the document was created with (version 0), then the ones this
	// version (1) records. The states are addressed by number, so both can share
	// a millisecond and still be two states.
	//
	// The newer row also carries what the parser template fills in on save: the
	// prompt an older row left empty. The panel lists the configuration as it
	// stands, so that one is reported too.
	emptyPipeline := ""
	settings := []entity.DocumentVersion{
		{DocumentID: "doc-1", DatasetVersion: 0, DatasetID: "kb-1",
			Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "naive",
			ParserConfig: entity.JSONMap{
				"chunk_token_num": 512,
				"delimiter":       "\n",
				"Extractor:AutoExtractDefault": entity.JSONMap{
					"keywords": entity.JSONMap{
						"system_prompt": "", "enabled": false, "top_n": 0,
					},
				},
			}},
		{DocumentID: "doc-1", DatasetVersion: 1, DatasetID: "kb-1",
			Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "naive",
			PipelineID: &emptyPipeline,
			ParserConfig: entity.JSONMap{
				"chunk_token_num": 1024,
				"delimiter":       "\n",
				"Extractor:AutoExtractDefault": entity.JSONMap{
					"keywords": entity.JSONMap{
						"system_prompt": "Role You are a text analyzer.", "enabled": true, "top_n": 2,
					},
				},
			}},
		// A document whose settings were first recorded by this change: there is no
		// earlier state to compare against, so it has no group of its own.
		{DocumentID: "doc-2", DatasetVersion: 1, DatasetID: "kb-1",
			Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "naive",
			ParserConfig: entity.JSONMap{"chunk_token_num": 512}},
	}
	if err := db.Create(&settings).Error; err != nil {
		t.Fatalf("seed settings states: %v", err)
	}
	version := &entity.DatasetVersion{
		DatasetID: "kb-1", Version: 1, Kind: entity.DatasetVersionKindDocumentConfig,
		CreatedBy: "tenant-1", CreateTime: &anchor,
		Changes: entity.DatasetVersionChanges{
			{DocID: "doc-1", Entity: entity.DatasetVersionChangeEntityDocument,
				Action: entity.DatasetVersionChangeActionUpdate, Name: "handbook.pdf"},
			{DocID: "doc-2", Entity: entity.DatasetVersionChangeEntityDocument,
				Action: entity.DatasetVersionChangeActionUpdate, Name: "manual.pdf"},
		},
	}
	if err := db.Create(version).Error; err != nil {
		t.Fatalf("seed version: %v", err)
	}

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	detail, err := testDatasetUpdateService(t).GetDatasetVersion(ctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: 1})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(detail.DocumentConfigChanges) != 1 {
		t.Fatalf("groups = %#v, want only the document with an earlier history row", detail.DocumentConfigChanges)
	}
	group := detail.DocumentConfigChanges[0]
	if group.DocID != "doc-1" || group.DocName != "handbook.pdf" {
		t.Fatalf("group = %#v, want the document the version touched", group)
	}
	// The parameters as the newer row carries them, never the whole parser_config
	// blob. The prompt the template filled in for an empty one reads as
	// "— → value" on the page, and the panel lists the configuration as it stands;
	// the pipeline binding the newer row carries empty stays out.
	byField := make(map[string]DatasetConfigChange, len(group.Changes))
	for _, change := range group.Changes {
		byField[change.Field] = change
	}
	want := []string{
		"parser_config.Extractor:AutoExtractDefault.keywords.enabled",
		"parser_config.Extractor:AutoExtractDefault.keywords.system_prompt",
		"parser_config.Extractor:AutoExtractDefault.keywords.top_n",
		"parser_config.chunk_token_num",
	}
	fields := make([]string, 0, len(byField))
	for _, change := range group.Changes {
		fields = append(fields, change.Field)
	}
	if strings.Join(fields, ",") != strings.Join(want, ",") {
		t.Fatalf("fields = %v, want %v", fields, want)
	}
	if got, ok := byField["parser_config.chunk_token_num"].After.(float64); !ok || got != 1024 {
		t.Fatalf("after = %v, want the value this version recorded", byField["parser_config.chunk_token_num"].After)
	}
	if got, ok := byField["parser_config.Extractor:AutoExtractDefault.keywords.enabled"].After.(bool); !ok || !got {
		t.Fatalf("enabled after = %v, want the new value", got)
	}
	// The change set still names both documents: the group is the detail beside
	// it, not a replacement for it.
	if len(detail.Changes) != 2 {
		t.Fatalf("changes = %d, want both documents", len(detail.Changes))
	}
	// A version of another kind reports none: its documents' parse settings did
	// not move with it.
	configVersion := &entity.DatasetVersion{
		DatasetID: "kb-1", Version: 2, Kind: entity.DatasetVersionKindDatasetConfig,
		CreatedBy: "tenant-1", CreateTime: &anchor,
	}
	if err := db.Create(configVersion).Error; err != nil {
		t.Fatalf("seed config version: %v", err)
	}
	if got, err := testDatasetUpdateService(t).GetDatasetVersion(
		ctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: 2}); err != nil {
		t.Fatalf("get config version: %v", err)
	} else if len(got.DocumentConfigChanges) != 0 {
		t.Fatalf("config version groups = %#v, want none", got.DocumentConfigChanges)
	}
}

// TestDocumentConfigVersionReportsAParseModeSwitch pins the switch from a built-in
// method to a pipeline: the mode line names it, parser_id is cleared because the
// method is no longer part of the selection, and the pipeline binding arrives as a
// line of its own.
func TestDocumentConfigVersionReportsAParseModeSwitch(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	anchor := int64(1762764000000)
	name := "012.txt"
	if err := db.Create(&entity.Document{
		ID: "doc-1", KbID: "kb-1", Name: &name, ParserConfig: entity.JSONMap{},
	}).Error; err != nil {
		t.Fatalf("seed document: %v", err)
	}
	canvas, canvasVersion := "canvas-7f3c", "canvas-version-7f3c"
	alias := "v3"
	seedCanvasVersion(t, db, canvas, canvasVersion, 0)
	if err := db.Model(&entity.UserCanvasVersion{}).Where("id = ?", canvasVersion).
		Update("alias", alias).Error; err != nil {
		t.Fatalf("name the canvas version: %v", err)
	}
	settings := []entity.DocumentVersion{
		{DocumentID: "doc-1", DatasetVersion: 0, DatasetID: "kb-1",
			Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "general",
			ParserConfig: entity.JSONMap{"chunk_token_num": 512}},
		// The save that bound the pipeline: parser_id is left as it was, which is
		// what the update writes, and the configuration is unchanged.
		{DocumentID: "doc-1", DatasetVersion: 1, DatasetID: "kb-1",
			Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "general",
			PipelineID:        &canvas,
			PipelineVersionID: &canvasVersion,
			ParserConfig:      entity.JSONMap{"chunk_token_num": 512}},
	}
	if err := db.Create(&settings).Error; err != nil {
		t.Fatalf("seed settings states: %v", err)
	}
	version := &entity.DatasetVersion{
		DatasetID: "kb-1", Version: 1, Kind: entity.DatasetVersionKindDocumentConfig,
		CreatedBy: "tenant-1", CreateTime: &anchor,
		Changes: entity.DatasetVersionChanges{
			{DocID: "doc-1", Entity: entity.DatasetVersionChangeEntityDocument,
				Action: entity.DatasetVersionChangeActionUpdate, Name: name},
		},
	}
	if err := db.Create(version).Error; err != nil {
		t.Fatalf("seed version: %v", err)
	}

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	detail, err := testDatasetUpdateService(t).GetDatasetVersion(
		ctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: 1})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(detail.DocumentConfigChanges) != 1 {
		t.Fatalf("groups = %#v, want the document the version touched", detail.DocumentConfigChanges)
	}
	group := detail.DocumentConfigChanges[0]
	byField := make(map[string]DatasetConfigChange, len(group.Changes))
	fields := make([]string, 0, len(group.Changes))
	for _, change := range group.Changes {
		byField[change.Field] = change
		fields = append(fields, change.Field)
	}
	// The mode says what happened, the method says what the document came from, and
	// the pipeline and its version say where it went.
	want := []string{"parse_type", "parser_id", "pipeline_id", "pipeline_version_id"}
	if strings.Join(fields, ",") != strings.Join(want, ",") {
		t.Fatalf("fields = %v, want %v", fields, want)
	}
	if mode := byField["parse_type"]; mode.Before != "builtin" || mode.After != "pipeline" {
		t.Fatalf("parse_type = %#v, want the switch from the built-in method to a pipeline", mode)
	}
	if method := byField["parser_id"]; method.Before != "general" || method.After != "" {
		t.Fatalf("parser_id = %#v, want the built-in method cleared", method)
	}
	// No canvas row backs the id in this database, so it stays a name the reader can
	// still act on rather than an empty cell.
	if pipeline := byField["pipeline_id"]; pipeline.Before != nil || pipeline.After != canvas {
		t.Fatalf("pipeline_id = %#v, want the pipeline the document moved to", pipeline)
	}
	if version := byField["pipeline_version_id"]; version.Before != nil || version.After != alias {
		t.Fatalf("pipeline_version_id = %#v, want the version's alias", version)
	}
}

// TestDocumentConfigVersionNamesThePipelineWhenOnlyTheVersionMoved covers a
// version switch inside the pipeline the document already ran: no pipeline row
// moves, and "a -> b" alone would not say which pipeline those aliases belong to.
// The group carries the pipeline on both sides so the panel can print
// "pip (alias: a) -> pip (alias: b)".
func TestDocumentConfigVersionNamesThePipelineWhenOnlyTheVersionMoved(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	anchor := int64(1762764000000)
	name := "012.txt"
	if err := db.Create(&entity.Document{
		ID: "doc-1", KbID: "kb-1", Name: &name, ParserConfig: entity.JSONMap{},
	}).Error; err != nil {
		t.Fatalf("seed document: %v", err)
	}
	canvas := "canvas-7f3c"
	seedDatasetUpdateCanvas(t, canvas, "tenant-1", datasetUpdateCanvasDSL("Parser:Shared", "chunk_token_num"))
	if err := db.Model(&entity.UserCanvas{}).Where("id = ?", canvas).
		Update("title", "pip").Error; err != nil {
		t.Fatalf("name the canvas: %v", err)
	}
	versionA, versionB := "canvas-version-a", "canvas-version-b"
	seedCanvasVersion(t, db, canvas, versionA, 0)
	seedCanvasVersion(t, db, canvas, versionB, 1000)
	if err := db.Model(&entity.UserCanvasVersion{}).Where("id = ?", versionA).
		Update("alias", "a").Error; err != nil {
		t.Fatalf("name the first version: %v", err)
	}
	if err := db.Model(&entity.UserCanvasVersion{}).Where("id = ?", versionB).
		Update("alias", "b").Error; err != nil {
		t.Fatalf("name the second version: %v", err)
	}
	settings := []entity.DocumentVersion{
		{DocumentID: "doc-1", DatasetVersion: 0, DatasetID: "kb-1",
			Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "general",
			PipelineID:        &canvas,
			PipelineVersionID: &versionA,
			ParserConfig:      entity.JSONMap{"chunk_token_num": 512}},
		{DocumentID: "doc-1", DatasetVersion: 1, DatasetID: "kb-1",
			Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "general",
			PipelineID:        &canvas,
			PipelineVersionID: &versionB,
			ParserConfig:      entity.JSONMap{"chunk_token_num": 512}},
	}
	if err := db.Create(&settings).Error; err != nil {
		t.Fatalf("seed settings states: %v", err)
	}
	version := &entity.DatasetVersion{
		DatasetID: "kb-1", Version: 1, Kind: entity.DatasetVersionKindDocumentConfig,
		CreatedBy: "tenant-1", CreateTime: &anchor,
		Changes: entity.DatasetVersionChanges{
			{DocID: "doc-1", Entity: entity.DatasetVersionChangeEntityDocument,
				Action: entity.DatasetVersionChangeActionUpdate, Name: name},
		},
	}
	if err := db.Create(version).Error; err != nil {
		t.Fatalf("seed version: %v", err)
	}

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	detail, err := testDatasetUpdateService(t).GetDatasetVersion(
		ctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: 1})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(detail.DocumentConfigChanges) != 1 {
		t.Fatalf("groups = %#v, want the document the version touched", detail.DocumentConfigChanges)
	}
	group := detail.DocumentConfigChanges[0]
	byField := make(map[string]DatasetConfigChange, len(group.Changes))
	fields := make([]string, 0, len(group.Changes))
	for _, change := range group.Changes {
		byField[change.Field] = change
		fields = append(fields, change.Field)
	}
	// The pipeline row comes before the version row it labels, so the panel folds
	// the alias into the pipeline it belongs to.
	want := []string{"pipeline_id", "pipeline_version_id"}
	if strings.Join(fields, ",") != strings.Join(want, ",") {
		t.Fatalf("fields = %v, want %v", fields, want)
	}
	if pipeline := byField["pipeline_id"]; pipeline.Before != "pip" || pipeline.After != "pip" {
		t.Fatalf("pipeline_id = %#v, want the pipeline the version belongs to on both sides", pipeline)
	}
	if version := byField["pipeline_version_id"]; version.Before != "a" || version.After != "b" {
		t.Fatalf("pipeline_version_id = %#v, want the aliases of both versions", version)
	}
}

// TestDocumentConfigVersionReportsAParameterWithNoPreviousValue pins the reading
// of a parameter the newer state carries and the older one did not: the panel
// lists the configuration as it stands, so it is reported, with its missing older
// side left empty.
func TestDocumentConfigVersionReportsAParameterWithNoPreviousValue(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	anchor := int64(1762764000000)
	name := "012.txt"
	if err := db.Create(&entity.Document{
		ID: "doc-1", KbID: "kb-1", Name: &name, ParserConfig: entity.JSONMap{},
	}).Error; err != nil {
		t.Fatalf("seed document: %v", err)
	}
	settings := []entity.DocumentVersion{
		{DocumentID: "doc-1", DatasetVersion: 0, DatasetID: "kb-1",
			Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "general",
			ParserConfig: entity.JSONMap{
				"Parser:Shared": map[string]interface{}{"order_index": 1},
			}},
		{DocumentID: "doc-1", DatasetVersion: 1, DatasetID: "kb-1",
			Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "general",
			ParserConfig: entity.JSONMap{
				"Parser:Shared": map[string]interface{}{
					"order_index": 1,
					"pages":       []interface{}{[]interface{}{1, 100000}},
				},
			}},
	}
	if err := db.Create(&settings).Error; err != nil {
		t.Fatalf("seed settings states: %v", err)
	}
	version := &entity.DatasetVersion{
		DatasetID: "kb-1", Version: 1, Kind: entity.DatasetVersionKindDocumentConfig,
		CreatedBy: "tenant-1", CreateTime: &anchor,
		Changes: entity.DatasetVersionChanges{
			{DocID: "doc-1", Entity: entity.DatasetVersionChangeEntityDocument,
				Action: entity.DatasetVersionChangeActionUpdate, Name: name},
		},
	}
	if err := db.Create(version).Error; err != nil {
		t.Fatalf("seed version: %v", err)
	}

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	detail, err := testDatasetUpdateService(t).GetDatasetVersion(
		ctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: 1})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(detail.DocumentConfigChanges) != 1 {
		t.Fatalf("groups = %#v, want the document the version touched", detail.DocumentConfigChanges)
	}
	byField := make(map[string]DatasetConfigChange)
	for _, change := range detail.DocumentConfigChanges[0].Changes {
		byField[change.Field] = change
	}
	pages, ok := byField["parser_config.Parser:Shared.pages"]
	if !ok {
		t.Fatalf("changes = %#v, want the parameter the newer state carries", detail.DocumentConfigChanges[0].Changes)
	}
	if pages.Before != nil {
		t.Fatalf("pages before = %#v, want the missing side left empty", pages.Before)
	}
	if pages.After == nil {
		t.Fatalf("pages after = %#v, want the value the newer state carries", pages.After)
	}
}

// TestDocumentConfigVersionDropsAParameterTheNewerStateLost is the other half: a
// parameter only the older state carries is not listed, because the panel reads
// the configuration as it stands now.
func TestDocumentConfigVersionDropsAParameterTheNewerStateLost(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	anchor := int64(1762764000000)
	name := "012.txt"
	if err := db.Create(&entity.Document{
		ID: "doc-1", KbID: "kb-1", Name: &name, ParserConfig: entity.JSONMap{},
	}).Error; err != nil {
		t.Fatalf("seed document: %v", err)
	}
	settings := []entity.DocumentVersion{
		{DocumentID: "doc-1", DatasetVersion: 0, DatasetID: "kb-1",
			Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "general",
			ParserConfig: entity.JSONMap{
				"Parser:Shared": map[string]interface{}{
					"order_index": 1,
					"pages":       []interface{}{[]interface{}{1, 100000}},
				},
			}},
		{DocumentID: "doc-1", DatasetVersion: 1, DatasetID: "kb-1",
			Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "general",
			ParserConfig: entity.JSONMap{
				"Parser:Shared": map[string]interface{}{"order_index": 1},
			}},
	}
	if err := db.Create(&settings).Error; err != nil {
		t.Fatalf("seed settings states: %v", err)
	}
	version := &entity.DatasetVersion{
		DatasetID: "kb-1", Version: 1, Kind: entity.DatasetVersionKindDocumentConfig,
		CreatedBy: "tenant-1", CreateTime: &anchor,
		Changes: entity.DatasetVersionChanges{
			{DocID: "doc-1", Entity: entity.DatasetVersionChangeEntityDocument,
				Action: entity.DatasetVersionChangeActionUpdate, Name: name},
		},
	}
	if err := db.Create(version).Error; err != nil {
		t.Fatalf("seed version: %v", err)
	}

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	detail, err := testDatasetUpdateService(t).GetDatasetVersion(
		ctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: 1})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// The only difference between the two states is a key the newer one lost:
	// the panel reads the configuration as it stands, so it is not listed.
	if len(detail.DocumentConfigChanges) != 0 {
		t.Fatalf("groups = %#v, want a parameter the newer state lost left out", detail.DocumentConfigChanges)
	}
}

// TestDocumentConfigVersionReportsAParserSwitchAndItsParameters pins the
// "switched to Q&A" case: the method line names the switch, and the parameters
// the newer state carries are listed as they stand - the one the old template had
// and the new one emptied reads as "value → []".
func TestDocumentConfigVersionReportsAParserSwitchAndItsParameters(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	anchor := int64(1762764000000)
	name := "012.txt"
	if err := db.Create(&entity.Document{
		ID: "doc-1", KbID: "kb-1", Name: &name, ParserConfig: entity.JSONMap{},
	}).Error; err != nil {
		t.Fatalf("seed document: %v", err)
	}
	settings := []entity.DocumentVersion{
		{DocumentID: "doc-1", DatasetVersion: 0, DatasetID: "kb-1",
			Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "general",
			ParserConfig: entity.JSONMap{
				"Parser:HipSignsRhyme": map[string]interface{}{
					"pdf": map[string]interface{}{
						"pages": []interface{}{[]interface{}{1, 100000}},
					},
				},
			}},
		{DocumentID: "doc-1", DatasetVersion: 1, DatasetID: "kb-1",
			Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "qa",
			ParserConfig: entity.JSONMap{
				"Parser:HipSignsRhyme": map[string]interface{}{
					"pdf": map[string]interface{}{"pages": []interface{}{}},
				},
			}},
	}
	if err := db.Create(&settings).Error; err != nil {
		t.Fatalf("seed settings states: %v", err)
	}
	version := &entity.DatasetVersion{
		DatasetID: "kb-1", Version: 1, Kind: entity.DatasetVersionKindDocumentConfig,
		CreatedBy: "tenant-1", CreateTime: &anchor,
		Changes: entity.DatasetVersionChanges{
			{DocID: "doc-1", Entity: entity.DatasetVersionChangeEntityDocument,
				Action: entity.DatasetVersionChangeActionUpdate, Name: name},
		},
	}
	if err := db.Create(version).Error; err != nil {
		t.Fatalf("seed version: %v", err)
	}

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	detail, err := testDatasetUpdateService(t).GetDatasetVersion(
		ctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: 1})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(detail.DocumentConfigChanges) != 1 {
		t.Fatalf("groups = %#v, want the document the version touched", detail.DocumentConfigChanges)
	}
	fields := make([]string, 0, len(detail.DocumentConfigChanges[0].Changes))
	for _, change := range detail.DocumentConfigChanges[0].Changes {
		fields = append(fields, change.Field)
	}
	want := []string{"parser_id", "parser_config.Parser:HipSignsRhyme.pdf.pages"}
	if strings.Join(fields, ",") != strings.Join(want, ",") {
		t.Fatalf("fields = %v, want %v", fields, want)
	}
	if change := detail.DocumentConfigChanges[0].Changes[0]; change.Before != "general" || change.After != "qa" {
		t.Fatalf("parser_id = %#v, want general -> qa", change)
	}
}

// TestDocumentConfigVersionReportsEveryParameterOfAParserSwitch pins the other
// half: the switch line names the method, and every parameter the newer state
// carries is listed as it stands, whether or not the old template declared it.
func TestDocumentConfigVersionReportsEveryParameterOfAParserSwitch(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	anchor := int64(1762764000000)
	name := "012.txt"
	if err := db.Create(&entity.Document{
		ID: "doc-1", KbID: "kb-1", Name: &name, ParserConfig: entity.JSONMap{},
	}).Error; err != nil {
		t.Fatalf("seed document: %v", err)
	}
	settings := []entity.DocumentVersion{
		{DocumentID: "doc-1", DatasetVersion: 0, DatasetID: "kb-1",
			Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "general",
			ParserConfig: entity.JSONMap{
				"Parser:HipSignsRhyme": map[string]interface{}{
					"pdf": map[string]interface{}{
						"parse_method": "DeepDOC",
						"pages":        []interface{}{[]interface{}{1, 100000}},
					},
				},
			}},
		{DocumentID: "doc-1", DatasetVersion: 1, DatasetID: "kb-1",
			Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "qa",
			ParserConfig: entity.JSONMap{
				"Parser:HipSignsRhyme": map[string]interface{}{
					"pdf": map[string]interface{}{
						"parse_method": "VLM",
						"pages":        []interface{}{},
					},
				},
			}},
	}
	if err := db.Create(&settings).Error; err != nil {
		t.Fatalf("seed settings states: %v", err)
	}
	version := &entity.DatasetVersion{
		DatasetID: "kb-1", Version: 1, Kind: entity.DatasetVersionKindDocumentConfig,
		CreatedBy: "tenant-1", CreateTime: &anchor,
		Changes: entity.DatasetVersionChanges{
			{DocID: "doc-1", Entity: entity.DatasetVersionChangeEntityDocument,
				Action: entity.DatasetVersionChangeActionUpdate, Name: name},
		},
	}
	if err := db.Create(version).Error; err != nil {
		t.Fatalf("seed version: %v", err)
	}

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	detail, err := testDatasetUpdateService(t).GetDatasetVersion(
		ctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: 1})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(detail.DocumentConfigChanges) != 1 {
		t.Fatalf("groups = %#v, want the document the version touched", detail.DocumentConfigChanges)
	}
	byField := make(map[string]DatasetConfigChange)
	for _, change := range detail.DocumentConfigChanges[0].Changes {
		byField[change.Field] = change
	}
	// Both parameters are carried by the newer state, so both are listed: the one
	// that moved, and the one the switch emptied.
	method, ok := byField["parser_config.Parser:HipSignsRhyme.pdf.parse_method"]
	if !ok {
		t.Fatalf("changes = %#v, want the parameter the switch moved",
			detail.DocumentConfigChanges[0].Changes)
	}
	if method.Before != "DeepDOC" || method.After != "VLM" {
		t.Fatalf("parse_method = %#v, want DeepDOC -> VLM", method)
	}
	if _, ok := byField["parser_config.Parser:HipSignsRhyme.pdf.pages"]; !ok {
		t.Fatalf("changes = %#v, want the parameter the newer state carries emptied",
			detail.DocumentConfigChanges[0].Changes)
	}
}

// TestDocumentConfigVersionReportsAPipelineToBuiltInSwitch covers the direction
// that comes back: the document ran a pipeline and is put back on a built-in
// method. A pipeline row carries no method of its own, so that side of the pair is
// absent - and it still has to be reported, or the panel would show the pipeline
// disappearing without saying what replaced it.
func TestDocumentConfigVersionReportsAPipelineToBuiltInSwitch(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	anchor := int64(1762764000000)
	name := "012.txt"
	if err := db.Create(&entity.Document{
		ID: "doc-1", KbID: "kb-1", Name: &name, ParserConfig: entity.JSONMap{},
	}).Error; err != nil {
		t.Fatalf("seed document: %v", err)
	}
	canvas := "canvas-7f3c"
	emptyPipeline := ""
	settings := []entity.DocumentVersion{
		// The row still carries the method the document last ran under, which is not
		// part of its selection while a pipeline parses it.
		{DocumentID: "doc-1", DatasetVersion: 0, DatasetID: "kb-1",
			Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "naive",
			PipelineID:   &canvas,
			ParserConfig: entity.JSONMap{"chunk_token_num": 512}},
		{DocumentID: "doc-1", DatasetVersion: 1, DatasetID: "kb-1",
			Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "paper",
			PipelineID:   &emptyPipeline,
			ParserConfig: entity.JSONMap{"chunk_token_num": 512}},
	}
	if err := db.Create(&settings).Error; err != nil {
		t.Fatalf("seed settings states: %v", err)
	}
	version := &entity.DatasetVersion{
		DatasetID: "kb-1", Version: 1, Kind: entity.DatasetVersionKindDocumentConfig,
		CreatedBy: "tenant-1", CreateTime: &anchor,
		Changes: entity.DatasetVersionChanges{
			{DocID: "doc-1", Entity: entity.DatasetVersionChangeEntityDocument,
				Action: entity.DatasetVersionChangeActionUpdate, Name: name},
		},
	}
	if err := db.Create(version).Error; err != nil {
		t.Fatalf("seed version: %v", err)
	}

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	detail, err := testDatasetUpdateService(t).GetDatasetVersion(
		ctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: 1})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(detail.DocumentConfigChanges) != 1 {
		t.Fatalf("groups = %#v, want the document the version touched", detail.DocumentConfigChanges)
	}
	byField := make(map[string]DatasetConfigChange)
	fields := make([]string, 0, len(detail.DocumentConfigChanges[0].Changes))
	for _, change := range detail.DocumentConfigChanges[0].Changes {
		byField[change.Field] = change
		fields = append(fields, change.Field)
	}
	if want := []string{"parse_type", "parser_id", "pipeline_id"}; strings.Join(fields, ",") != strings.Join(want, ",") {
		t.Fatalf("fields = %v, want %v", fields, want)
	}
	if mode := byField["parse_type"]; mode.Before != "pipeline" || mode.After != "builtin" {
		t.Fatalf("parse_type = %#v, want the switch back to the built-in method", mode)
	}
	if method := byField["parser_id"]; method.Before != "" || method.After != "paper" {
		t.Fatalf("parser_id = %#v, want the method the document was put back on", method)
	}
	if pipeline := byField["pipeline_id"]; pipeline.Before != canvas || pipeline.After != "" {
		t.Fatalf("pipeline_id = %#v, want the pipeline the document left", pipeline)
	}
}

// TestDocumentConfigVersionSeparatesTwoEditsInOneMillisecond pins why the states
// are addressed by version instead of by clock: two settings writes in the same
// millisecond are two states, and a diff that answered both sides of the pair
// from one clock-keyed row would report no change at all.
func TestDocumentConfigVersionSeparatesTwoEditsInOneMillisecond(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	anchor := int64(1762764000000)
	// Both writes land in the same millisecond, which is the case a clock-keyed
	// record cannot represent.
	name := "handbook.pdf"
	if err := db.Create(&entity.Document{
		ID: "doc-1", KbID: "kb-1", Name: &name, ParserConfig: entity.JSONMap{},
	}).Error; err != nil {
		t.Fatalf("seed document: %v", err)
	}
	states := []entity.DocumentVersion{
		{DocumentID: "doc-1", DatasetVersion: 0, DatasetID: "kb-1", Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "naive",
			ParserConfig: entity.JSONMap{"chunk_token_num": 512}},
		{DocumentID: "doc-1", DatasetVersion: 1, DatasetID: "kb-1", Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "naive",
			ParserConfig: entity.JSONMap{"chunk_token_num": 1024}},
	}
	if err := db.Create(&states).Error; err != nil {
		t.Fatalf("seed document versions: %v", err)
	}
	version := &entity.DatasetVersion{
		DatasetID: "kb-1", Version: 1, Kind: entity.DatasetVersionKindDocumentConfig,
		CreatedBy: "tenant-1", CreateTime: &anchor,
		Changes: entity.DatasetVersionChanges{
			{DocID: "doc-1", Entity: entity.DatasetVersionChangeEntityDocument,
				Action: entity.DatasetVersionChangeActionUpdate, Name: name},
		},
	}
	if err := db.Create(version).Error; err != nil {
		t.Fatalf("seed version: %v", err)
	}

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	detail, err := testDatasetUpdateService(t).GetDatasetVersion(
		ctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: 1})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(detail.DocumentConfigChanges) != 1 {
		t.Fatalf("groups = %#v, want the edit the two versions record", detail.DocumentConfigChanges)
	}
	group := detail.DocumentConfigChanges[0]
	if len(group.Changes) != 1 || group.Changes[0].Field != "parser_config.chunk_token_num" {
		t.Fatalf("changes = %#v, want the one parameter that moved", group.Changes)
	}
	if before, _ := group.Changes[0].Before.(float64); before != 512 {
		t.Fatalf("before = %v, want the state version 0 recorded", group.Changes[0].Before)
	}
	if after, _ := group.Changes[0].After.(float64); after != 1024 {
		t.Fatalf("after = %v, want the state this version recorded", group.Changes[0].After)
	}
}

// TestDatasetConfigChangesReportsOneNestedParameter pins what a single parser
// parameter edit reads as: the parameter's own line, not the whole parser_config
// blob.
func TestDatasetConfigChangesReportsOneNestedParameter(t *testing.T) {
	before := entity.JSONMap{
		"parser_config": map[string]interface{}{
			"chunk_token_num": 512,
			"delimiter":       "\n",
		},
	}
	after := entity.JSONMap{
		"parser_config": map[string]interface{}{
			"chunk_token_num": 1024,
			"delimiter":       "\n",
		},
	}

	changes := datasetConfigFieldChanges(before, after, []string{"parser_config"})
	if len(changes) != 1 || changes[0].Field != "parser_config.chunk_token_num" {
		t.Fatalf("changes = %#v, want the one parameter that moved", changes)
	}
	// Values are normalized through JSON, so the numbers arrive as float64.
	if got, ok := changes[0].Before.(float64); !ok || got != 512 {
		t.Fatalf("before = %v, want the old value", changes[0].Before)
	}
	if got, ok := changes[0].After.(float64); !ok || got != 1024 {
		t.Fatalf("after = %v, want the new value", changes[0].After)
	}

	// The same snapshot on both sides records nothing, nested or not.
	if got := datasetConfigFieldChanges(before, before, []string{"parser_config"}); len(got) != 0 {
		t.Fatalf("identical snapshots = %#v, want no change", got)
	}
}

// TestDatasetConfigChangesReadsTheNewerSnapshot pins the display rule. Saving
// re-derives parser_config from the parser template, so a parameter the newer row
// carries is listed ("— → value" when the older row had none), while a parameter
// it no longer carries is silent ("value → —"). A top-level field that first
// appears (a pipeline binding) is a real change, and so is a field whose type
// changed.
func TestDatasetConfigChangesReadsTheNewerSnapshot(t *testing.T) {
	before := entity.JSONMap{
		"parser_config": map[string]interface{}{
			"Extractor:AutoExtractDefault": map[string]interface{}{
				"top_p": 0.3,
				"keywords": map[string]interface{}{
					"top_n": 0,
				},
				// The template dropped these two keys.
				"preprocess": []interface{}{"main_content"},
				"summary":    map[string]interface{}{"enabled": false},
				// Only the defaults below are new on the other side.
				"questions": map[string]interface{}{"top_n": 0},
			},
		},
	}
	after := entity.JSONMap{
		"pipeline_id": "canvas-1",
		"parser_config": map[string]interface{}{
			"Extractor:AutoExtractDefault": map[string]interface{}{
				"top_p":                   0.3,
				"frequencyPenaltyEnabled": true,
				"keywords": map[string]interface{}{
					"top_n":   1,
					"enabled": true,
				},
				"summary": map[string]interface{}{
					"enabled":       false,
					"system_prompt": "Act as a precise summarizer.",
				},
				"questions": map[string]interface{}{
					"top_n":         0,
					"enabled":       false,
					"system_prompt": "Propose {{ topn }} questions.",
				},
			},
			"Parser:HipSignsRhyme": map[string]interface{}{
				"doc": map[string]interface{}{"order_index": 0},
			},
		},
	}

	changes := datasetConfigFieldChanges(before, after, []string{"pipeline_id", "parser_config"})
	got := make([]string, 0, len(changes))
	for _, change := range changes {
		got = append(got, change.Field)
	}
	// What the newer snapshot carries: the parameters the older one never stored
	// arrive as "— → value", the one that moved keeps its values, the component the
	// template added is listed parameter by parameter - and the keys only the older
	// snapshot has (the template dropped them) are silent.
	want := []string{
		"pipeline_id",
		"parser_config.Extractor:AutoExtractDefault.frequencyPenaltyEnabled",
		"parser_config.Extractor:AutoExtractDefault.keywords.enabled",
		"parser_config.Extractor:AutoExtractDefault.keywords.top_n",
		"parser_config.Extractor:AutoExtractDefault.questions.enabled",
		"parser_config.Extractor:AutoExtractDefault.questions.system_prompt",
		"parser_config.Extractor:AutoExtractDefault.summary.system_prompt",
		"parser_config.Parser:HipSignsRhyme.doc.order_index",
	}
	if len(got) != len(want) {
		t.Fatalf("changes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("changes = %v, want %v", got, want)
		}
	}

	// A field whose type changed is not comparable key by key, so it is
	// reported whole instead of vanishing.
	typed := datasetConfigFieldChanges(
		entity.JSONMap{"similarity_threshold": 0.2},
		entity.JSONMap{"similarity_threshold": map[string]interface{}{"value": 0.2}},
		[]string{"similarity_threshold"},
	)
	if len(typed) != 1 || typed[0].Field != "similarity_threshold" {
		t.Fatalf("type change = %#v, want the field reported whole", typed)
	}
}

// TestDatasetConfigChangesReportsEveryValueTheNewerRowCarries: absent/nil is a
// filled-in default and "" is a value the row carried - both are reported when
// the newer row carries them.
func TestDatasetConfigChangesReportsEveryValueTheNewerRowCarries(t *testing.T) {
	before := entity.JSONMap{
		"parser_config": map[string]interface{}{
			"Extractor:AutoExtractDefault": map[string]interface{}{
				"keywords": map[string]interface{}{
					"enabled": false,
					"top_n":   0,
					// The key is present but holds no value: absent.
					"system_prompt": nil,
				},
				// The key is not there at all: absent too.
				"questions": map[string]interface{}{},
				"summary": map[string]interface{}{
					// An empty string is a value the row carried.
					"system_prompt": "",
				},
			},
		},
	}
	after := entity.JSONMap{
		"parser_config": map[string]interface{}{
			"Extractor:AutoExtractDefault": map[string]interface{}{
				"keywords": map[string]interface{}{
					"enabled":       true,
					"top_n":         21,
					"system_prompt": "Role: You are a text analyzer.",
				},
				"questions": map[string]interface{}{
					"system_prompt": "Propose {{ topn }} questions.",
				},
				"summary": map[string]interface{}{
					"system_prompt": "Act as a precise summarizer.",
				},
			},
		},
	}

	changes := datasetConfigFieldChanges(before, after, []string{"parser_config"})
	got := make([]string, 0, len(changes))
	for _, change := range changes {
		got = append(got, change.Field)
	}
	want := []string{
		"parser_config.Extractor:AutoExtractDefault.keywords.enabled",
		"parser_config.Extractor:AutoExtractDefault.keywords.system_prompt",
		"parser_config.Extractor:AutoExtractDefault.keywords.top_n",
		"parser_config.Extractor:AutoExtractDefault.questions.system_prompt",
		"parser_config.Extractor:AutoExtractDefault.summary.system_prompt",
	}
	if len(got) != len(want) {
		t.Fatalf("changes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("changes = %v, want %v", got, want)
		}
	}
	// The prompt the older row held with nothing in it is still a value it
	// carried: the line shows "" → text rather than "— → text".
	if before, _ := changes[4].Before.(string); before != "" {
		t.Fatalf("summary before = %#v, want the empty string the row carried", changes[4].Before)
	}
	if before := changes[1].Before; before != nil {
		t.Fatalf("keywords before = %#v, want the nil the older row carried", before)
	}
}

// restoreConfigVersion runs the configuration half of a restore the way the
// content service does, so these tests keep covering the primitives it composes:
// plan under the row lock (with the caller's expected head), apply, record.
// The live path is datasetcontent.Restore, which cannot be exercised here
// - it needs the document, chunk and storage services - so the sequence is
// spelled out once, in this file, instead of being duplicated per test.
// errDryRunPreview ends a preview transaction once the plan has been read. A
// preview writes nothing, so the transaction it needs for the row lock has to be
// rolled back; the sentinel never reaches a caller.
var errDryRunPreview = errors.New("dry run")

func restoreConfigVersion(ctx context.Context, svc *DatasetService, req *RestoreDatasetVersionRequest) (*RestoreDatasetVersionResponse, error) {
	if req == nil {
		return nil, datasetVersionError{code: common.CodeArgumentError, message: "request is required"}
	}
	datasetID, err := datasetVersionTarget(ctx, req.DatasetID, permission.ResourcePermissionManage, permission.ActionWrite)
	if err != nil {
		return nil, err
	}
	target, err := dao.NewDatasetVersionDAO().GetByDatasetAndVersion(ctx, dao.DB, datasetID, req.Version)
	if err != nil {
		if errors.Is(err, dao.ErrDatasetVersionNotFound) {
			return nil, datasetVersionError{code: common.CodeNotFound, message: fmt.Sprintf("dataset version %d not found", req.Version)}
		}
		return nil, fmt.Errorf("failed to load dataset version: %w", err)
	}
	kb, err := svc.kbDAO.GetByID(ctx, dao.DB, datasetID)
	if err != nil || kb == nil {
		return nil, datasetVersionError{code: common.CodeNotFound, message: "dataset not found"}
	}
	if req.ExpectedVersion != nil && kb.Version != *req.ExpectedVersion {
		return nil, datasetVersionError{
			code:    common.CodeConflict,
			message: fmt.Sprintf("dataset version changed: expected %d, current is %d", *req.ExpectedVersion, kb.Version),
		}
	}
	if req.DryRun {
		// The preview runs the plan the commit runs, in a transaction that is rolled
		// back: the plan is what validates the recorded name, embedding model and
		// pipeline, and a preview that skipped it would promise a restore the commit
		// then refuses.
		previewErr := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if _, _, planErr := svc.PlanConfigRestoreTx(ctx, tx, datasetID, req.ExpectedVersion, target.Config); planErr != nil {
				return planErr
			}
			return errDryRunPreview
		})
		if previewErr != nil && !errors.Is(previewErr, errDryRunPreview) {
			return nil, previewErr
		}
		return &RestoreDatasetVersionResponse{
			Version:    NoDatasetVersion,
			PreVersion: kb.Version,
			Applied:    false,
		}, nil
	}

	actor := permission.UserIDFromContext(ctx)
	var (
		preVersion, newVersion int64
		pagerank               *int64
		restoredKB             *entity.Knowledgebase
	)
	err = dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		plan, lockedKB, planErr := svc.PlanConfigRestoreTx(ctx, tx, datasetID, req.ExpectedVersion, target.Config)
		if planErr != nil {
			return planErr
		}
		pagerank = plan.PagerankValue
		restoredKB = lockedKB
		if lockedKB != nil {
			preVersion = lockedKB.Version
		}
		if planErr = svc.ApplyConfigRestoreTx(ctx, tx, datasetID, plan); planErr != nil {
			return planErr
		}
		newVersion, planErr = datasetversion.RecordVersionWithChanges(
			ctx, tx, datasetID, entity.DatasetVersionKindRestore, actor, target.Version, nil, nil)
		return planErr
	})
	if err != nil {
		var coded datasetVersionError
		if errors.As(err, &coded) {
			return nil, coded
		}
		return nil, fmt.Errorf("failed to restore dataset version: %w", err)
	}
	if pagerank != nil {
		if mirrorErr := svc.MirrorConfigPagerank(ctx, restoredKB, datasetID, *pagerank); mirrorErr != nil {
			return nil, fmt.Errorf("dataset configuration was restored, but refreshing chunk pagerank failed: %w", mirrorErr)
		}
	}
	return &RestoreDatasetVersionResponse{
		Version:    newVersion,
		PreVersion: preVersion,
		Applied:    true,
	}, nil
}

// TestRestoreDatasetVersionAppliesConfig pins the restore contract: the target
// state is written back, and the restored state is appended as a new version
// that names its source.
func TestRestoreDatasetVersionAppliesConfig(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	svc := testDatasetUpdateService(t)
	description := "changed"
	if _, code, err := svc.UpdateDataset(ctx, "kb-1", service.UpdateDatasetRequest{Description: &description}); err != nil {
		t.Fatalf("update: code=%d err=%v", code, err)
	}

	resp, err := restoreConfigVersion(ctx, svc, &RestoreDatasetVersionRequest{
		DatasetID: "kb-1",
		Version:   0,
	})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !resp.Applied || resp.Version != 2 || resp.PreVersion != 1 {
		t.Fatalf("restore response = %+v, want applied v2 from pre v1", resp)
	}

	var kb entity.Knowledgebase
	if err := db.Where("id = ?", "kb-1").Take(&kb).Error; err != nil {
		t.Fatalf("reload dataset: %v", err)
	}
	if got := stringValueOf(kb.Description); got != "" {
		t.Fatalf("description = %q, want the baseline (empty)", got)
	}

	rows := listDatasetVersions(t, "kb-1")
	if len(rows) != 3 {
		t.Fatalf("versions = %d, want 3 (baseline, config, restore)", len(rows))
	}
	if rows[2].Kind != entity.DatasetVersionKindRestore || rows[2].RestoredFrom != 0 {
		t.Fatalf("v2 = %s from %d, want restore from v0", rows[2].Kind, rows[2].RestoredFrom)
	}
	// The restore puts the baseline's configuration back, so it lands on the same
	// content-addressed snapshot row as version 0 instead of storing a copy.
	if rows[2].ConfigHash == nil || *rows[2].ConfigHash != *rows[0].ConfigHash {
		t.Fatalf("v2 config_hash = %v, want the restored (baseline) configuration", rows[2].ConfigHash)
	}
	// The read path still resolves the pointer: the restore version reports the
	// configuration it restored to.
	restoredDetail, err := svc.GetDatasetVersion(ctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: 2})
	if err != nil {
		t.Fatalf("get restored version: %v", err)
	}
	if got := restoredDetail.Config["description"]; got != "" {
		t.Fatalf("restored snapshot description = %v, want empty", got)
	}
}

// TestGetDatasetVersionRestoreReportsItsEmptyDocuments covers what a restore
// leaves behind: a document it could not fill keeps the restore's marker and no
// chunks, and the version detail has to carry both so the page can name it. A
// document the dataset no longer holds answers nothing at all - unknown content is
// not the same as empty, and calling it empty would ask for a re-parse that has
// nothing to parse.
func TestGetDatasetVersionRestoreReportsItsEmptyDocuments(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	svc := testDatasetUpdateService(t)

	// A restore version whose set holds three documents: one the restore left
	// empty, one it filled, and one the dataset no longer holds.
	documentsHash := "restore-set"
	if err := dao.NewDatasetDocumentSetSnapshotDAO().Upsert(ctx, db, "kb-1", documentsHash,
		`["doc-empty","doc-filled","doc-gone"]`); err != nil {
		t.Fatalf("seed document set: %v", err)
	}
	restoredFrom, version := int64(4), int64(5)
	if err := db.Create(&entity.DatasetVersion{
		DatasetID: "kb-1", Version: version, Kind: entity.DatasetVersionKindRestore,
		RestoredFrom: restoredFrom, DocumentsHash: &documentsHash,
	}).Error; err != nil {
		t.Fatalf("seed restore version: %v", err)
	}

	emptyName, filledName := "empty.pdf", "filled.pdf"
	if err := db.Create(&[]entity.Document{
		// The replay left no chunks, so the document has to be re-parsed.
		{ID: "doc-empty", KbID: "kb-1", Name: &emptyName, ParserConfig: entity.JSONMap{},
			ChunkNum: 0, RestoredVersion: &restoredFrom},
		// The replay brought its content back: nothing to do.
		{ID: "doc-filled", KbID: "kb-1", Name: &filledName, ParserConfig: entity.JSONMap{},
			ChunkNum: 12, RestoredVersion: &restoredFrom},
	}).Error; err != nil {
		t.Fatalf("seed documents: %v", err)
	}

	detail, err := svc.GetDatasetVersion(ctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: version})
	if err != nil {
		t.Fatalf("get restore version: %v", err)
	}
	byID := make(map[string]DatasetVersionDocument, len(detail.Documents))
	for _, document := range detail.Documents {
		byID[document.ID] = document
	}
	if len(byID) != 3 {
		t.Fatalf("documents = %d, want the three the set recorded", len(byID))
	}

	empty := byID["doc-empty"]
	if empty.ChunkNum == nil || *empty.ChunkNum != 0 {
		t.Fatalf("doc-empty chunk_num = %v, want 0 so the page names it", empty.ChunkNum)
	}
	if empty.RestoredVersion == nil || *empty.RestoredVersion != restoredFrom {
		t.Fatalf("doc-empty restored_version = %v, want the source version %d", empty.RestoredVersion, restoredFrom)
	}

	filled := byID["doc-filled"]
	if filled.ChunkNum == nil || *filled.ChunkNum != 12 {
		t.Fatalf("doc-filled chunk_num = %v, want the 12 it holds", filled.ChunkNum)
	}

	// The document the dataset no longer holds reports neither: its content state
	// is unknown, and the page must not count it as empty.
	gone := byID["doc-gone"]
	if gone.ChunkNum != nil || gone.RestoredVersion != nil {
		t.Fatalf("doc-gone = %+v, want no content state", gone)
	}

	// A version of any other kind carries no content state either: the fields
	// describe what a restore left, not what the documents hold today.
	if err := db.Model(&entity.DatasetVersion{}).
		Where("dataset_id = ? AND version = ?", "kb-1", version).
		Update("kind", entity.DatasetVersionKindUpload).Error; err != nil {
		t.Fatalf("retag version: %v", err)
	}
	upload, err := svc.GetDatasetVersion(ctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: version})
	if err != nil {
		t.Fatalf("get retagged version: %v", err)
	}
	for _, document := range upload.Documents {
		if document.ChunkNum != nil || document.RestoredVersion != nil {
			t.Fatalf("upload version document %s carries content state %+v", document.ID, document)
		}
	}
}

// nestedParserConfigChunkerParam reads one GeneralChunker parameter out of a
// component-scoped parser_config. Every key of the stored config is a component
// id ({"GeneralChunker:SixApplesFall": {"chunk_token_size": ...}}); a flat key
// would mean the column was edited by hand or written before the component
// scoping, and the runtime would ignore it.
func nestedParserConfigChunkerParam(t *testing.T, config entity.JSONMap, param string) float64 {
	t.Helper()
	const chunkerID = "GeneralChunker:SixApplesFall"
	params, ok := config[chunkerID].(map[string]interface{})
	if !ok {
		t.Fatalf("parser_config[%q] = %#v, want the component-scoped chunker params; config = %#v", chunkerID, config[chunkerID], config)
	}
	value, ok := params[param].(float64)
	if !ok {
		t.Fatalf("parser_config[%q][%q] = %#v, want a number", chunkerID, param, params[param])
	}
	return value
}

// TestRestoreDatasetVersionRestoresNestedParserConfig pins the write side of the
// nested diff: the preview names one parameter under its component id, but a
// restore writes columns, so that entry has to be reduced back to
// "parser_config" or the parameter keeps the value the version was supposed to
// replace.
//
// The submitted config is component-scoped, the shape the API accepts: a flat key
// is dropped at the boundary (ValidateParserConfig), which would turn the update
// into a no-op instead of moving the parameter.
func TestRestoreDatasetVersionRestoresNestedParserConfig(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	// The baseline seeds the chunker's chunk_token_size = 128; move it to 1024.
	seed := entity.JSONMap{
		"GeneralChunker:SixApplesFall": map[string]interface{}{"chunk_token_size": float64(128)},
	}
	if err := db.Model(&entity.Knowledgebase{}).Where("id = ?", "kb-1").
		Update("parser_config", seed).Error; err != nil {
		t.Fatalf("seed parser_config: %v", err)
	}

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	svc := testDatasetUpdateService(t)

	submitted := entity.JSONMap{
		"GeneralChunker:SixApplesFall": map[string]interface{}{"chunk_token_size": float64(1024)},
	}
	if _, code, err := svc.UpdateDataset(ctx, "kb-1", service.UpdateDatasetRequest{
		ParserConfig: submitted,
	}); err != nil {
		t.Fatalf("update: code=%d err=%v", code, err)
	}
	var changed entity.Knowledgebase
	if err := db.Where("id = ?", "kb-1").Take(&changed).Error; err != nil {
		t.Fatalf("reload changed dataset: %v", err)
	}
	if got := nestedParserConfigChunkerParam(t, changed.ParserConfig, "chunk_token_size"); got != 1024 {
		t.Fatalf("chunk_token_size = %v after update, want 1024", got)
	}

	if _, err := restoreConfigVersion(ctx, svc, &RestoreDatasetVersionRequest{
		DatasetID: "kb-1",
		Version:   0,
	}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	var restored entity.Knowledgebase
	if err := db.Where("id = ?", "kb-1").Take(&restored).Error; err != nil {
		t.Fatalf("reload restored dataset: %v", err)
	}
	if got := nestedParserConfigChunkerParam(t, restored.ParserConfig, "chunk_token_size"); got != 128 {
		t.Fatalf("chunk_token_size = %v after restore, want the baseline 128", got)
	}
}

// TestRestoreDatasetVersionRestoresParserMethod pins the cheapest way back from a
// parser-method switch: General -> Paper -> restore the General version. The
// component ids parser_config is keyed by come from the template, so the two
// states share no chunker or tokenizer at all. A restore driven by the leaf-level
// diff would see "no key both sides carry moved", write only parser_id, and leave
// the dataset on the General method holding Paper's component ids - which the
// runtime ignores.
func TestRestoreDatasetVersionRestoresParserMethod(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	general := entity.JSONMap{
		"GeneralChunker:SixApplesFall": map[string]interface{}{"chunk_token_size": float64(128)},
	}
	if err := db.Model(&entity.Knowledgebase{}).Where("id = ?", "kb-1").
		Updates(map[string]interface{}{"parser_id": "naive", "parser_config": general}).Error; err != nil {
		t.Fatalf("seed general state: %v", err)
	}

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	svc := testDatasetUpdateService(t)

	builtIn := 1
	paperID := "paper"
	if _, code, err := svc.UpdateDataset(ctx, "kb-1", service.UpdateDatasetRequest{
		ParseType:    &builtIn,
		ParserID:     &paperID,
		ParserConfig: entity.JSONMap{"TitleChunker:SparklySchoolsTravel": map[string]interface{}{"chunk_token_size": float64(256)}},
	}); err != nil {
		t.Fatalf("switch to paper: code=%d err=%v", code, err)
	}
	var switched entity.Knowledgebase
	if err := db.Where("id = ?", "kb-1").Take(&switched).Error; err != nil {
		t.Fatalf("reload switched dataset: %v", err)
	}
	if switched.ParserID != "paper" {
		t.Fatalf("parser_id = %q after switch, want paper", switched.ParserID)
	}
	if _, ok := switched.ParserConfig["GeneralChunker:SixApplesFall"]; ok {
		t.Fatalf("parser_config still holds the general template's chunker: %#v", switched.ParserConfig)
	}

	if _, err := restoreConfigVersion(ctx, svc, &RestoreDatasetVersionRequest{DatasetID: "kb-1", Version: 0}); err != nil {
		t.Fatalf("restore: %v", err)
	}

	var restored entity.Knowledgebase
	if err := db.Where("id = ?", "kb-1").Take(&restored).Error; err != nil {
		t.Fatalf("reload restored dataset: %v", err)
	}
	if restored.ParserID != "naive" {
		t.Fatalf("parser_id = %q after restore, want the baseline naive", restored.ParserID)
	}
	if _, ok := restored.ParserConfig["TitleChunker:SparklySchoolsTravel"]; ok {
		t.Fatalf("parser_config keeps paper's chunker after restoring general: %#v", restored.ParserConfig)
	}
	if got := nestedParserConfigChunkerParam(t, restored.ParserConfig, "chunk_token_size"); got != 128 {
		t.Fatalf("chunk_token_size = %v after restore, want the baseline 128", got)
	}
}

// TestRestoreDatasetVersionRestoresNameAndAvatar pins that the dataset's
// identity is part of what a version records: a restore puts the name and the
// avatar back like any other setting.
func TestRestoreDatasetVersionRestoresNameAndAvatar(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	svc := testDatasetUpdateService(t)

	name := "Renamed"
	avatar := "data:image/png;base64,iVBORw0KGgo="
	if _, code, err := svc.UpdateDataset(ctx, "kb-1", service.UpdateDatasetRequest{Name: &name, Avatar: &avatar}); err != nil {
		t.Fatalf("update: code=%d err=%v", code, err)
	}

	if _, err := restoreConfigVersion(ctx, svc, &RestoreDatasetVersionRequest{DatasetID: "kb-1", Version: 0}); err != nil {
		t.Fatalf("restore: %v", err)
	}

	var kb entity.Knowledgebase
	if err := db.Where("id = ?", "kb-1").Take(&kb).Error; err != nil {
		t.Fatalf("reload dataset: %v", err)
	}
	if got := kb.Name; got != "Dataset" {
		t.Fatalf("name = %q, want the baseline name", got)
	}
	if got := stringValueOf(kb.Avatar); got != "" {
		t.Fatalf("avatar = %q, want the baseline (empty)", got)
	}
}

// TestRestoreDatasetVersionClearsAvatarMissingFromOldSnapshot keeps a version
// written before the avatar joined the snapshot restorable: a missing key means
// the dataset had no avatar, not that the field should be left alone.
func TestRestoreDatasetVersionClearsAvatarMissingFromOldSnapshot(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	svc := testDatasetUpdateService(t)

	avatar := "data:image/png;base64,iVBORw0KGgo="
	if _, code, err := svc.UpdateDataset(ctx, "kb-1", service.UpdateDatasetRequest{Avatar: &avatar}); err != nil {
		t.Fatalf("update: code=%d err=%v", code, err)
	}

	// Strip the key from the baseline, the way a snapshot written before the
	// field existed reads.
	rewriteVersionConfig(t, "kb-1", 0, func(snapshot entity.JSONMap) {
		delete(snapshot, "avatar")
	})

	if _, err := restoreConfigVersion(ctx, svc, &RestoreDatasetVersionRequest{DatasetID: "kb-1", Version: 0}); err != nil {
		t.Fatalf("restore: %v", err)
	}

	var kb entity.Knowledgebase
	if err := db.Where("id = ?", "kb-1").Take(&kb).Error; err != nil {
		t.Fatalf("reload dataset: %v", err)
	}
	if got := stringValueOf(kb.Avatar); got != "" {
		t.Fatalf("avatar = %q, want the missing key read as no avatar", got)
	}
}

// TestRestoreDatasetVersionRefusesTakenName keeps the tenant's uniqueness rule:
// restoring a name another dataset already uses is refused before anything is
// written, with the colliding name in the message.
func TestRestoreDatasetVersionRefusesTakenName(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	svc := testDatasetUpdateService(t)

	renamed := "Renamed"
	if _, code, err := svc.UpdateDataset(ctx, "kb-1", service.UpdateDatasetRequest{Name: &renamed}); err != nil {
		t.Fatalf("update: code=%d err=%v", code, err)
	}

	// Another dataset now holds the name the baseline version recorded.
	other := &entity.Knowledgebase{
		ID:           "kb-2",
		TenantID:     "tenant-1",
		Name:         "Dataset",
		EmbdID:       "BAAI/bge-large-zh-v1.5@Builtin",
		CreatedBy:    "tenant-1",
		Permission:   string(entity.TenantPermissionMe),
		ParserID:     string(entity.ParserTypeNaive),
		ParserConfig: entity.JSONMap{},
		Status:       sptr(string(entity.StatusValid)),
	}
	if err := db.Create(other).Error; err != nil {
		t.Fatalf("insert other dataset: %v", err)
	}

	if _, err := restoreConfigVersion(ctx, svc, &RestoreDatasetVersionRequest{DatasetID: "kb-1", Version: 0}); err == nil {
		t.Fatal("restore onto a taken name must be refused")
	} else if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("restore error = %v, want the name collision", err)
	}

	var kb entity.Knowledgebase
	if err := db.Where("id = ?", "kb-1").Take(&kb).Error; err != nil {
		t.Fatalf("reload dataset: %v", err)
	}
	if got := kb.Name; got != "Renamed" {
		t.Fatalf("name = %q, want the refused restore to leave it untouched", got)
	}
}

// TestDatasetVersionPageSizeClamps keeps one request from asking for an
// unbounded page: a limit past the ceiling is cut to it, and a smaller one is
// left alone so a client can still page finely.
func TestDatasetVersionPageSizeClamps(t *testing.T) {
	if got := datasetVersionPageSize(1000000); got != maxDatasetVersionPageSize {
		t.Fatalf("page size = %d, want the ceiling %d", got, maxDatasetVersionPageSize)
	}
	if got := datasetVersionPageSize(7); got != 7 {
		t.Fatalf("page size = %d, want the requested 7", got)
	}
	if got := datasetVersionPageSize(0); got != 0 {
		t.Fatalf("page size = %d, want the store default (0)", got)
	}
}

// TestRestoreDatasetVersionDryRunReportsTheRefusal keeps the preview honest: it
// has to answer "would this be refused", not only "what would it write". A
// preview that only diffs the configuration shows a plan for a restore whose
// commit then fails on a name another dataset holds.
func TestRestoreDatasetVersionDryRunReportsTheRefusal(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	svc := testDatasetUpdateService(t)

	renamed := "Renamed"
	if _, code, err := svc.UpdateDataset(ctx, "kb-1", service.UpdateDatasetRequest{Name: &renamed}); err != nil {
		t.Fatalf("update: code=%d err=%v", code, err)
	}
	if err := db.Create(&entity.Knowledgebase{
		ID: "kb-2", TenantID: "tenant-1", Name: "Dataset",
		EmbdID: "BAAI/bge-large-zh-v1.5@Builtin", CreatedBy: "tenant-1",
		Permission:   string(entity.TenantPermissionMe),
		ParserID:     string(entity.ParserTypeNaive),
		ParserConfig: entity.JSONMap{},
		Status:       sptr(string(entity.StatusValid)),
	}).Error; err != nil {
		t.Fatalf("insert other dataset: %v", err)
	}

	if _, err := restoreConfigVersion(ctx, svc, &RestoreDatasetVersionRequest{
		DatasetID: "kb-1", Version: 0, DryRun: true,
	}); err == nil {
		t.Fatal("the preview must report the refusal the commit would hit")
	} else if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("preview error = %v, want the name collision", err)
	}

	// The preview wrote nothing: the dataset keeps the name it had.
	var kb entity.Knowledgebase
	if err := db.Where("id = ?", "kb-1").Take(&kb).Error; err != nil {
		t.Fatalf("reload dataset: %v", err)
	}
	if got := kb.Name; got != "Renamed" {
		t.Fatalf("name = %q, the preview must not write", got)
	}
}

// TestDatasetConfigRestoreColumnsComparesWholeColumns pins what a restore writes:
// a parser_config whose component ids were swapped wholesale shares no leaf with
// the stored one, so the reported diff stays silent about it, but the column still
// has to be written or the dataset keeps the other template's component ids.
func TestDatasetConfigRestoreColumnsComparesWholeColumns(t *testing.T) {
	before := entity.JSONMap{
		"description": "Dataset",
		"parser_id":   "general",
		"parser_config": map[string]interface{}{
			"GeneralChunker:SixApplesFall": map[string]interface{}{"chunk_token_size": float64(128)},
		},
	}
	stored := entity.JSONMap{
		"description": "Dataset",
		"parser_id":   "general",
		"parser_config": map[string]interface{}{
			"TitleChunker:SparklySchoolsTravel": map[string]interface{}{"chunk_token_size": float64(128)},
		},
	}
	columns := datasetConfigRestoreColumns(before, stored)
	if _, ok := columns["parser_config"]; !ok {
		t.Fatalf("columns = %#v, want parser_config written when the component set differs", columns)
	}
	if _, ok := columns["description"]; ok {
		t.Fatalf("columns = %#v, want an unchanged column left alone", columns)
	}

	// A parameter only the live row carries has to be written too, so the restore
	// deletes it instead of leaving a value the version never had.
	stored["parser_config"] = map[string]interface{}{
		"GeneralChunker:SixApplesFall": map[string]interface{}{"chunk_token_size": float64(128)},
	}
	before["parser_config"] = map[string]interface{}{
		"GeneralChunker:SixApplesFall": map[string]interface{}{
			"chunk_token_size": float64(128),
			"delimiters":       []interface{}{"\n"},
		},
	}
	columns = datasetConfigRestoreColumns(before, stored)
	if _, ok := columns["parser_config"]; !ok {
		t.Fatalf("columns = %#v, want parser_config written when the version drops a parameter", columns)
	}
}

// TestRestoreDatasetVersionDryRunWritesNothing keeps the preview side effect
// free.
func TestRestoreDatasetVersionDryRunWritesNothing(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	svc := testDatasetUpdateService(t)
	description := "changed"
	if _, code, err := svc.UpdateDataset(ctx, "kb-1", service.UpdateDatasetRequest{Description: &description}); err != nil {
		t.Fatalf("update: code=%d err=%v", code, err)
	}

	resp, err := restoreConfigVersion(ctx, svc, &RestoreDatasetVersionRequest{
		DatasetID: "kb-1",
		Version:   0,
		DryRun:    true,
	})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if resp.Applied {
		t.Fatal("dry run must not report an applied restore")
	}
	if rows := listDatasetVersions(t, "kb-1"); len(rows) != 2 {
		t.Fatalf("versions = %d, want 2 (no writes from a dry run)", len(rows))
	}

	var kb entity.Knowledgebase
	if err := db.Where("id = ?", "kb-1").Take(&kb).Error; err != nil {
		t.Fatalf("reload dataset: %v", err)
	}
	if got := stringValueOf(kb.Description); got != "changed" {
		t.Fatalf("description = %q, want it untouched", got)
	}
}

// TestRestoreDatasetVersionRejectsStaleExpectedVersion pins the CAS: an editor
// holding an older version cannot overwrite a newer one.
func TestRestoreDatasetVersionRejectsStaleExpectedVersion(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	svc := testDatasetUpdateService(t)
	description := "changed"
	if _, code, err := svc.UpdateDataset(ctx, "kb-1", service.UpdateDatasetRequest{Description: &description}); err != nil {
		t.Fatalf("update: code=%d err=%v", code, err)
	}

	stale := int64(0)
	_, err := restoreConfigVersion(ctx, svc, &RestoreDatasetVersionRequest{
		DatasetID:       "kb-1",
		Version:         0,
		ExpectedVersion: &stale,
	})
	if err == nil {
		t.Fatal("expected the stale version to be rejected")
	}
	var coded interface{ Code() common.ErrorCode }
	if !errors.As(err, &coded) || coded.Code() != common.CodeConflict {
		t.Fatalf("error = %v, want CodeConflict", err)
	}
	if rows := listDatasetVersions(t, "kb-1"); len(rows) != 2 {
		t.Fatalf("versions = %d, want 2 (a rejected restore writes nothing)", len(rows))
	}
}

// TestRestoreDatasetVersionRestoresEmbeddingModel pins that moving the model back
// needs no rebuild: vectors are stored per dimension, so the ones the other model
// produced stay in their column and are used again, and the tenant pointer follows
// the id instead of staying on the model the restore replaced.
func TestRestoreDatasetVersionRestoresEmbeddingModel(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	svc := testDatasetUpdateService(t)
	description := "changed"
	if _, code, err := svc.UpdateDataset(ctx, "kb-1", service.UpdateDatasetRequest{Description: &description}); err != nil {
		t.Fatalf("update: code=%d err=%v", code, err)
	}
	// The dataset has moved on to another model and holds chunks parsed under it.
	replacedModel := "aabbccdd11223344aabbccdd11223344"
	if err := db.Model(&entity.Knowledgebase{}).Where("id = ?", "kb-1").Updates(map[string]interface{}{
		"chunk_num":      7,
		"embd_id":        "other-embedding@Builtin",
		"tenant_embd_id": replacedModel,
	}).Error; err != nil {
		t.Fatalf("seed dataset: %v", err)
	}

	if _, err := restoreConfigVersion(ctx, svc, &RestoreDatasetVersionRequest{DatasetID: "kb-1", Version: 0}); err != nil {
		t.Fatalf("restore should move the model back even with chunks: %v", err)
	}

	var restored entity.Knowledgebase
	if err := db.Where("id = ?", "kb-1").Take(&restored).Error; err != nil {
		t.Fatalf("reload restored dataset: %v", err)
	}
	if restored.EmbdID != "BAAI/bge-large-zh-v1.5@Builtin" {
		t.Fatalf("embd_id = %q after restore, want the baseline model", restored.EmbdID)
	}
	// The version recorded no tenant pointer, so the stale model the restore
	// replaced is cleared rather than left behind.
	if restored.TenantEmbdID != nil {
		t.Fatalf("tenant_embd_id = %q after restore, want it cleared", *restored.TenantEmbdID)
	}
}

// TestApplyRestoreEmbeddingSwitchMovesModelBeforeContent pins the ordering a
// content restore needs: the model is switched before any chunk is rebuilt, so the
// recomputed vectors belong to the model the dataset is returned to.
func TestApplyRestoreEmbeddingSwitchMovesModelBeforeContent(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	svc := testDatasetUpdateService(t)
	stored := entity.JSONMap{
		"embd_id":        "other-embedding@Builtin",
		"tenant_embd_id": "aabbccdd11223344aabbccdd11223344",
	}
	change, err := svc.ApplyRestoreEmbeddingSwitch(ctx, "kb-1", stored)
	if err != nil {
		t.Fatalf("switch: %v", err)
	}
	if change == nil || change.Field != "embd_id" || change.After != "other-embedding@Builtin" {
		t.Fatalf("change = %#v, want the moved embedding model", change)
	}
	var switched entity.Knowledgebase
	if err := db.Where("id = ?", "kb-1").Take(&switched).Error; err != nil {
		t.Fatalf("reload dataset: %v", err)
	}
	if switched.EmbdID != "other-embedding@Builtin" {
		t.Fatalf("embd_id = %q, want the recorded model", switched.EmbdID)
	}
	if switched.TenantEmbdID == nil || *switched.TenantEmbdID != "aabbccdd11223344aabbccdd11223344" {
		t.Fatalf("tenant_embd_id = %#v, want the recorded pointer", switched.TenantEmbdID)
	}

	// A second call has nothing to move.
	if change, err := svc.ApplyRestoreEmbeddingSwitch(ctx, "kb-1", stored); err != nil || change != nil {
		t.Fatalf("second switch = (%#v, %v), want nil", change, err)
	}

	// A model the tenant cannot resolve is refused before the column moves.
	stored["embd_id"] = "missing-model@missing-provider"
	if _, err := svc.ApplyRestoreEmbeddingSwitch(ctx, "kb-1", stored); err == nil {
		t.Fatal("expected an unresolvable model to be refused")
	}
	var unchanged entity.Knowledgebase
	if err := db.Where("id = ?", "kb-1").Take(&unchanged).Error; err != nil {
		t.Fatalf("reload dataset: %v", err)
	}
	if unchanged.EmbdID != "other-embedding@Builtin" {
		t.Fatalf("embd_id = %q, want the refused switch to leave it untouched", unchanged.EmbdID)
	}
}

// TestRestoreDatasetVersionRefusesUnavailableEmbeddingModel keeps a restore from
// pointing the dataset at a model the tenant can no longer resolve.
func TestRestoreDatasetVersionRefusesUnavailableEmbeddingModel(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	svc := testDatasetUpdateService(t)
	description := "changed"
	if _, code, err := svc.UpdateDataset(ctx, "kb-1", service.UpdateDatasetRequest{Description: &description}); err != nil {
		t.Fatalf("update: code=%d err=%v", code, err)
	}
	// Rewrite the recorded model as one that no longer resolves, as if the model
	// the version was written under had been deleted from the tenant.
	rewriteVersionConfig(t, "kb-1", 0, func(snapshot entity.JSONMap) {
		snapshot["embd_id"] = "missing-model@missing-provider"
	})

	_, err := restoreConfigVersion(ctx, svc, &RestoreDatasetVersionRequest{DatasetID: "kb-1", Version: 0})
	if err == nil {
		t.Fatal("expected an unresolvable embedding model to be refused")
	}
	var coded interface{ Code() common.ErrorCode }
	if !errors.As(err, &coded) || coded.Code() != common.CodeDataError {
		t.Fatalf("error = %v, want CodeDataError", err)
	}
	if rows := listDatasetVersions(t, "kb-1"); len(rows) != 2 {
		t.Fatalf("versions = %d, want 2 (a refused restore writes nothing)", len(rows))
	}
	var unchanged entity.Knowledgebase
	if err := db.Where("id = ?", "kb-1").Take(&unchanged).Error; err != nil {
		t.Fatalf("reload dataset: %v", err)
	}
	if unchanged.EmbdID != "BAAI/bge-large-zh-v1.5@Builtin" {
		t.Fatalf("embd_id = %q, want the refused restore to leave it untouched", unchanged.EmbdID)
	}
}

// TestSeedDatasetBaselineVersionIsIdempotent keeps the seeding path safe to
// call more than once.
func TestSeedBaselineVersionIsIdempotent(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	svc := testDatasetUpdateService(t)
	kb, err := svc.kbDAO.GetByID(t.Context(), dao.DB, "kb-1")
	if err != nil || kb == nil {
		t.Fatalf("load dataset: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := db.Transaction(func(tx *gorm.DB) error {
			return svc.seedDatasetBaselineVersionTxEE(t.Context(), tx, kb, "tenant-1")
		}); err != nil {
			t.Fatalf("seed #%d: %v", i+1, err)
		}
	}

	rows := listDatasetVersions(t, "kb-1")
	if len(rows) != 1 || rows[0].Version != 0 || rows[0].Kind != entity.DatasetVersionKindBaseline {
		t.Fatalf("versions = %#v, want a single baseline row", rows)
	}
}

// TestDatasetVersionActorResolvesNickname pins the display-name resolution: a
// version stores the actor's user id, and the read paths name the user instead
// so the history does not show a raw id. An actor with no user row — a deleted
// user, or a system-written version — stays empty rather than echoing the id
// into the name field.
func TestDatasetVersionActorResolvesNickname(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	if err := db.Create(&entity.User{
		ID: "actor-1", Nickname: "Alice", Email: "alice@test.local",
		IsAuthenticated: "1", IsActive: "1", IsAnonymous: "0",
	}).Error; err != nil {
		t.Fatalf("insert actor: %v", err)
	}
	for _, row := range []*entity.DatasetVersion{
		{
			DatasetID: "kb-1", Version: 0,
			Kind: entity.DatasetVersionKindBaseline, CreatedBy: "actor-1",
		},
		{
			DatasetID: "kb-1", Version: 1,
			Kind: entity.DatasetVersionKindDatasetConfig, CreatedBy: "no-such-user",
		},
	} {
		if err := dao.NewDatasetVersionDAO().Create(t.Context(), db, row); err != nil {
			t.Fatalf("insert version %d: %v", row.Version, err)
		}
	}

	svc := testDatasetUpdateService(t)
	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	list, err := svc.ListDatasetVersions(ctx, &ListDatasetVersionsRequest{DatasetID: "kb-1"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	names := make(map[int64]string, len(list.Versions))
	for _, item := range list.Versions {
		names[item.Version] = item.CreatedByName
	}
	if names[0] != "Alice" {
		t.Fatalf("v0 created_by_name = %q, want the nickname", names[0])
	}
	if names[1] != "" {
		t.Fatalf("v1 created_by_name = %q, want empty for an unresolvable actor", names[1])
	}

	detail, err := svc.GetDatasetVersion(ctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: 0})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if detail.CreatedByName != "Alice" {
		t.Fatalf("detail created_by_name = %q, want the nickname", detail.CreatedByName)
	}
	if detail.CreatedBy != "actor-1" {
		t.Fatalf("detail created_by = %q, want the raw id kept for reference", detail.CreatedBy)
	}
}

// TestLanguageOnlyUpdateRecordsOnlyLanguage pins what a single-field PATCH is
// allowed to record. The settings form drops an untouched parser_config from the
// payload, so the version a language-only edit produces differs from the state
// before it by the language alone, instead of also carrying the parser_config the
// form's round trip had re-serialised.
func TestLanguageOnlyUpdateRecordsOnlyLanguage(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	// A stored config the DSL rebuild would rewrite (a param the clean-up drops,
	// and defaults it would fill in), so an unwanted rebuild cannot hide behind an
	// unchanged snapshot.
	stored := entity.JSONMap{
		"GeneralChunker:SixApplesFall": map[string]interface{}{
			"preprocess":       []interface{}{"main_content"},
			"chunk_token_size": float64(256),
		},
	}
	if err := db.Model(&entity.Knowledgebase{}).Where("id = ?", "kb-1").Update("parser_config", stored).Error; err != nil {
		t.Fatalf("seed parser config: %v", err)
	}

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	svc := testDatasetUpdateService(t)
	language := "Chinese"
	if _, code, err := svc.UpdateDataset(ctx, "kb-1", service.UpdateDatasetRequest{Language: &language}); err != nil {
		t.Fatalf("update: code=%d err=%v", code, err)
	}

	persisted, err := dao.NewKnowledgebaseDAO().GetByID(ctx, db, "kb-1")
	if err != nil {
		t.Fatalf("get updated dataset: %v", err)
	}
	if !datasetJSONEqual(persisted.ParserConfig, stored) {
		t.Fatalf("parser_config = %#v, want it untouched by a language-only edit", persisted.ParserConfig)
	}

	baseline, err := svc.GetDatasetVersion(ctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: 0})
	if err != nil {
		t.Fatalf("get baseline: %v", err)
	}
	head, err := svc.GetDatasetVersion(ctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: 1})
	if err != nil {
		t.Fatalf("get head: %v", err)
	}
	changes := datasetConfigFieldChanges(
		entity.JSONMap(baseline.Config), entity.JSONMap(head.Config), datasetVersionComparedFields)
	if len(changes) != 1 || changes[0].Field != "language" {
		t.Fatalf("changes = %#v, want the language alone", changes)
	}
}

// TestDatasetVersionKeyIsDatasetAndVersion pins the table's identity: a version
// is addressed by (dataset_id, version) and nothing else, so that pair is the
// primary key. There is no surrogate id, and the same number can never exist
// twice for one dataset while another dataset may hold it independently.
func TestDatasetVersionKeyIsDatasetAndVersion(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	seedDatasetVersion(t, "kb-1", 0, entity.DatasetVersionKindBaseline, entity.JSONMap{"name": "Dataset"})

	if err := db.Create(&entity.DatasetVersion{
		DatasetID: "kb-1", Version: 0, Kind: entity.DatasetVersionKindDatasetConfig,
	}).Error; err == nil {
		t.Fatal("a duplicate (dataset_id, version) must be rejected by the primary key")
	}

	if err := db.Create(&entity.DatasetVersion{
		DatasetID: "kb-2", Version: 0, Kind: entity.DatasetVersionKindDatasetConfig,
	}).Error; err != nil {
		t.Fatalf("another dataset must be able to hold version 0: %v", err)
	}
}

// seedDatasetVersion inserts one history row directly, so a test can pin the
// rules without going through the writers that would produce it. A non-nil config
// is stored as its own snapshot row, exactly as a writer would leave it.
func seedDatasetVersion(t *testing.T, datasetID string, version int64, kind string, config entity.JSONMap) *string {
	t.Helper()
	row := &entity.DatasetVersion{
		DatasetID: datasetID,
		Version:   version,
		Kind:      kind,
	}
	if config != nil {
		hash, err := datasetversion.ConfigHash(config)
		if err != nil {
			t.Fatalf("hash the seeded configuration of version %d: %v", version, err)
		}
		if err := dao.NewDatasetConfigSnapshotDAO().Upsert(t.Context(), dao.DB, datasetID, hash, config); err != nil {
			t.Fatalf("seed the configuration of version %d: %v", version, err)
		}
		row.ConfigHash = &hash
	}
	if err := dao.NewDatasetVersionDAO().Create(t.Context(), dao.DB, row); err != nil {
		t.Fatalf("seed version %d: %v", version, err)
	}
	return row.ConfigHash
}

// TestDeleteDatasetVersionDropsHistoryOnly pins what a version delete does: the
// row and its change set go, the remaining versions keep their numbers, and the
// dataset itself is not rewritten.
func TestDeleteDatasetVersionDropsHistoryOnly(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")
	if err := db.Model(&entity.Knowledgebase{}).Where("id = ?", "kb-1").Update("version", 2).Error; err != nil {
		t.Fatalf("pin current version: %v", err)
	}

	seedDatasetVersion(t, "kb-1", 0, entity.DatasetVersionKindBaseline, entity.JSONMap{"name": "Dataset"})
	seedDatasetVersion(t, "kb-1", 1, entity.DatasetVersionKindDatasetConfig, entity.JSONMap{"name": "Dataset", "language": "English"})
	seedDatasetVersion(t, "kb-1", 2, entity.DatasetVersionKindDatasetConfig, entity.JSONMap{"name": "Dataset", "language": "Chinese"})
	if err := db.Model(&entity.DatasetVersion{}).
		Where("dataset_id = ? AND version = ?", "kb-1", 1).
		Update("changes", entity.DatasetVersionChanges{{
			Entity: "document", Action: "add", DocID: "doc-1",
		}}).Error; err != nil {
		t.Fatalf("seed change set: %v", err)
	}

	svc := testDatasetUpdateService(t)
	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	resp, err := svc.DeleteDatasetVersion(ctx, &DeleteDatasetVersionRequest{DatasetID: "kb-1", Version: 1})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if resp.DeletedVersion != 1 || resp.CurrentVersion != 2 {
		t.Fatalf("response = %+v, want deleted 1 with current 2", resp)
	}

	list, err := svc.ListDatasetVersions(ctx, &ListDatasetVersionsRequest{DatasetID: "kb-1"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	kept := make([]int64, 0, len(list.Versions))
	for _, item := range list.Versions {
		kept = append(kept, item.Version)
	}
	if len(kept) != 2 || kept[0] != 2 || kept[1] != 0 {
		t.Fatalf("versions = %v, want [2 0]: the numbers keep their gaps", kept)
	}

	// The change set lived in the row, so it went with the version: a page that
	// still linked to it must not resolve to anything.
	if _, err := dao.NewDatasetVersionDAO().GetByDatasetAndVersion(ctx, db, "kb-1", 1); !errors.Is(err, dao.ErrDatasetVersionNotFound) {
		t.Fatalf("version 1 still resolves after the delete: %v", err)
	}

	// Deleting history must not move the dataset: its version still points at 2.
	kb, err := dao.NewKnowledgebaseDAO().GetByID(ctx, db, "kb-1")
	if err != nil {
		t.Fatalf("get dataset: %v", err)
	}
	if kb.Version != 2 {
		t.Fatalf("dataset version = %d, want it untouched at 2", kb.Version)
	}
}

// TestDeleteDatasetVersionRefusesProtectedVersions pins the versions the dataset
// depends on: the current one and the baseline.
func TestDeleteDatasetVersionRefusesProtectedVersions(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")
	if err := db.Model(&entity.Knowledgebase{}).Where("id = ?", "kb-1").Update("version", 2).Error; err != nil {
		t.Fatalf("pin current version: %v", err)
	}
	seedDatasetVersion(t, "kb-1", 0, entity.DatasetVersionKindBaseline, entity.JSONMap{"name": "Dataset"})
	seedDatasetVersion(t, "kb-1", 1, entity.DatasetVersionKindDatasetConfig, entity.JSONMap{"name": "Dataset"})
	seedDatasetVersion(t, "kb-1", 2, entity.DatasetVersionKindDatasetConfig, entity.JSONMap{"name": "Dataset"})

	svc := testDatasetUpdateService(t)
	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	for _, tc := range []struct {
		name    string
		version int64
		code    common.ErrorCode
	}{
		{name: "current", version: 2, code: common.CodeConflict},
		{name: "baseline", version: 0, code: common.CodeConflict},
		{name: "missing", version: 9, code: common.CodeNotFound},
	} {
		_, err := svc.DeleteDatasetVersion(ctx, &DeleteDatasetVersionRequest{DatasetID: "kb-1", Version: tc.version})
		var coded interface{ Code() common.ErrorCode }
		if !errors.As(err, &coded) || coded.Code() != tc.code {
			t.Fatalf("%s: error = %v, want %v", tc.name, err, tc.code)
		}
	}
}

// TestDeleteDatasetVersionKeepsTheConfigurationSnapshot pins the pointer's
// consequence for deletes: the version that recorded a configuration can go while
// a later version still reads it, because the snapshot lives in its own table and
// the delete only takes the pointer. The snapshot is reclaimed only once nothing
// points at it.
func TestDeleteDatasetVersionKeepsTheConfigurationSnapshot(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")
	if err := db.Model(&entity.Knowledgebase{}).Where("id = ?", "kb-1").Update("version", 3).Error; err != nil {
		t.Fatalf("pin current version: %v", err)
	}
	seedDatasetVersion(t, "kb-1", 0, entity.DatasetVersionKindBaseline, entity.JSONMap{"name": "Dataset"})
	renamedHash := seedDatasetVersion(t, "kb-1", 1, entity.DatasetVersionKindDatasetConfig, entity.JSONMap{"name": "Renamed"})
	// A content version written while v1 held the configuration: it points at the
	// same snapshot instead of copying it.
	if err := db.Create(&entity.DatasetVersion{
		DatasetID: "kb-1", Version: 2, Kind: entity.DatasetVersionKindChunk, ConfigHash: renamedHash,
	}).Error; err != nil {
		t.Fatalf("seed the content version: %v", err)
	}
	seedDatasetVersion(t, "kb-1", 3, entity.DatasetVersionKindDatasetConfig, entity.JSONMap{"name": "Dataset"})

	svc := testDatasetUpdateService(t)
	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")

	if _, err := svc.DeleteDatasetVersion(ctx, &DeleteDatasetVersionRequest{DatasetID: "kb-1", Version: 1}); err != nil {
		t.Fatalf("deleting the version that recorded the configuration: %v", err)
	}
	// The version that ran under it still reads the same snapshot: that is the
	// whole point of moving the configuration out of the version row.
	row, err := dao.NewDatasetVersionDAO().GetByDatasetAndVersion(ctx, db, "kb-1", 2)
	if err != nil {
		t.Fatalf("load the content version: %v", err)
	}
	if got := fmt.Sprint(row.Config["name"]); got != "Renamed" {
		t.Fatalf("content version config name = %q, want the snapshot it ran under", got)
	}
	if got := countDatasetConfigSnapshots(t, db, "kb-1"); got != 2 {
		t.Fatalf("stored snapshots = %d, want both (baseline and rename)", got)
	}

	if _, err := svc.DeleteDatasetVersion(ctx, &DeleteDatasetVersionRequest{DatasetID: "kb-1", Version: 2}); err != nil {
		t.Fatalf("deleting the content version: %v", err)
	}
	// Nothing points at the rename any more, so the delete reclaimed it.
	if got := countDatasetConfigSnapshots(t, db, "kb-1"); got != 1 {
		t.Fatalf("stored snapshots after the last reference went = %d, want 1", got)
	}
}

// rewriteVersionConfig replaces the snapshot a version points at, which is how a
// test writes a configuration the current writers would no longer produce - a row
// from before a field existed, or a model the tenant has since lost. The content
// changes, so the version is repointed at the new hash; the old row is left for
// the prune to collect.
func rewriteVersionConfig(t *testing.T, datasetID string, version int64, mutate func(entity.JSONMap)) {
	t.Helper()

	var row entity.DatasetVersion
	if err := dao.DB.Where("dataset_id = ? AND version = ?", datasetID, version).Take(&row).Error; err != nil {
		t.Fatalf("load version %d: %v", version, err)
	}
	if row.ConfigHash == nil {
		t.Fatalf("version %d records no configuration", version)
	}
	snapshot, err := dao.NewDatasetConfigSnapshotDAO().Get(t.Context(), dao.DB, datasetID, *row.ConfigHash)
	if err != nil {
		t.Fatalf("load the configuration of version %d: %v", version, err)
	}
	mutate(snapshot)
	hash, err := datasetversion.ConfigHash(snapshot)
	if err != nil {
		t.Fatalf("identify the rewritten configuration: %v", err)
	}
	if err := dao.NewDatasetConfigSnapshotDAO().Upsert(t.Context(), dao.DB, datasetID, hash, snapshot); err != nil {
		t.Fatalf("store the rewritten configuration: %v", err)
	}
	if err := dao.DB.Model(&entity.DatasetVersion{}).
		Where("dataset_id = ? AND version = ?", datasetID, version).
		Update("config_hash", hash).Error; err != nil {
		t.Fatalf("repoint version %d at its rewritten configuration: %v", version, err)
	}
}

// countDatasetConfigSnapshots is how many configuration snapshots the dataset
// stores: one per distinct content, not one per version.
func countDatasetConfigSnapshots(t *testing.T, db *gorm.DB, datasetID string) int64 {
	t.Helper()

	var count int64
	if err := db.Model(&entity.DatasetConfigSnapshot{}).Where("dataset_id = ?", datasetID).Count(&count).Error; err != nil {
		t.Fatalf("count configuration snapshots: %v", err)
	}
	return count
}

// TestDeleteDatasetVersionKeepsTheSetOthersName pins what storing the document set
// by content changed: a content version stays deletable even when later versions
// name the set it wrote, and nothing is copied onto a survivor first. The set stays
// because a version still names it - and it would go only when the last one did.
func TestDeleteDatasetVersionKeepsTheSetOthersName(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")
	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	if err := db.Model(&entity.Knowledgebase{}).Where("id = ?", "kb-1").Update("version", 3).Error; err != nil {
		t.Fatalf("pin current version: %v", err)
	}

	// The set is seeded the way the writers leave it: one row keyed by content,
	// named by every version that describes it.
	documentSet := []byte(`["doc-1"]`)
	hash := documentset.Hash(documentSet)
	if err := dao.NewDatasetDocumentSetSnapshotDAO().Upsert(ctx, db, "kb-1", hash, documentset.Encode(documentSet)); err != nil {
		t.Fatalf("seed the document set: %v", err)
	}
	rows := []*entity.DatasetVersion{
		{DatasetID: "kb-1", Version: 0, Kind: entity.DatasetVersionKindBaseline, DocumentsHash: &hash},
		{DatasetID: "kb-1", Version: 1, Kind: entity.DatasetVersionKindUpload, DocumentsHash: &hash},
		{DatasetID: "kb-1", Version: 2, Kind: entity.DatasetVersionKindChunk, DocumentsHash: &hash},
		{DatasetID: "kb-1", Version: 3, Kind: entity.DatasetVersionKindDatasetConfig, DocumentsHash: &hash},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seed version %d: %v", row.Version, err)
		}
	}

	svc := testDatasetUpdateService(t)
	if _, err := svc.DeleteDatasetVersion(ctx, &DeleteDatasetVersionRequest{DatasetID: "kb-1", Version: 1}); err != nil {
		t.Fatalf("delete the version that wrote the set: %v", err)
	}

	// The surviving versions still resolve the same text, from the same row.
	versionDAO := dao.NewDatasetVersionDAO()
	for _, version := range []int64{2, 3} {
		row, err := versionDAO.GetByDatasetAndVersion(ctx, db, "kb-1", version)
		if err != nil {
			t.Fatalf("load version %d: %v", version, err)
		}
		if row.Documents != documentset.Encode(documentSet) {
			t.Fatalf("v%d document set = %q, want the text the deleted version wrote", version, row.Documents)
		}
		if row.DocumentsHash == nil || *row.DocumentsHash != hash {
			t.Fatalf("v%d documents_hash = %v, want %s", version, row.DocumentsHash, hash[:8])
		}
	}
	// Deleting a version takes only its pointer: the set is still named, so the
	// sweep keeps it. This is the step the old design had to do by copying the set.
	var stored int64
	if err := db.Model(&entity.DatasetDocumentSetSnapshot{}).Where("dataset_id = ?", "kb-1").Count(&stored).Error; err != nil {
		t.Fatalf("count the stored sets: %v", err)
	}
	if stored != 1 {
		t.Fatalf("stored document sets = %d, want 1", stored)
	}
}

// TestConfigurationVersionInheritsTheDocumentSet covers the same rule on the
// state writer: a configuration change leaves every document row alone, so it
// points at the version that recorded the set rather than copying it.
func TestConfigurationVersionInheritsTheDocumentSet(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")
	name := "manual.pdf"
	if err := db.Create(&entity.Document{
		ID: "doc-1", KbID: "kb-1", Name: &name, ChunkNum: 3, ParserConfig: entity.JSONMap{},
	}).Error; err != nil {
		t.Fatalf("seed document: %v", err)
	}

	svc := testDatasetUpdateService(t)
	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	kb, err := dao.NewKnowledgebaseDAO().GetByID(ctx, db, "kb-1")
	if err != nil || kb == nil {
		t.Fatalf("load dataset: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, appendErr := svc.appendDatasetVersionTxEE(ctx, tx, kb, 0, entity.DatasetVersionKindBaseline, "user-1", 0, nil)
		return appendErr
	}); err != nil {
		t.Fatalf("record the baseline: %v", err)
	}
	// The configuration change inherits the hash the baseline wrote, the same way a
	// parse or a chunk edit would.
	baseline, err := dao.NewDatasetVersionDAO().GetByDatasetAndVersion(ctx, db, "kb-1", 0)
	if err != nil {
		t.Fatalf("load the baseline: %v", err)
	}
	if baseline.DocumentsHash == nil {
		t.Fatal("the baseline names no document set")
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, appendErr := svc.appendDatasetVersionTxEE(ctx, tx, kb, 1, entity.DatasetVersionKindDatasetConfig, "user-1", 0, baseline.DocumentsHash)
		return appendErr
	}); err != nil {
		t.Fatalf("record the configuration version: %v", err)
	}

	var stored []*entity.DatasetVersion
	if err := db.Where("dataset_id = ?", "kb-1").Order("version").Find(&stored).Error; err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(stored) != 2 {
		t.Fatalf("versions = %d, want 2", len(stored))
	}
	if stored[0].DocumentsHash == nil {
		t.Fatalf("baseline = %#v, want a document set of its own", stored[0])
	}
	if stored[1].DocumentsHash == nil || *stored[1].DocumentsHash != *stored[0].DocumentsHash {
		t.Fatalf("configuration documents_hash = %v, want the baseline's", stored[1].DocumentsHash)
	}
	// One row, named twice: the configuration change stores nothing.
	var sets int64
	if err := db.Model(&entity.DatasetDocumentSetSnapshot{}).Where("dataset_id = ?", "kb-1").Count(&sets).Error; err != nil {
		t.Fatalf("count the stored sets: %v", err)
	}
	if sets != 1 {
		t.Fatalf("stored document sets = %d, want 1", sets)
	}

	resolved, err := dao.NewDatasetVersionDAO().GetByDatasetAndVersion(ctx, db, "kb-1", 1)
	if err != nil {
		t.Fatalf("load the configuration version: %v", err)
	}
	if len(resolved.Documents) == 0 {
		t.Fatal("the configuration version resolved to no document set")
	}
	entries, err := datasetversion.ParseDocuments(resolved.Documents)
	if err != nil || len(entries) != 1 || entries[0] != "doc-1" {
		t.Fatalf("resolved entries = %#v, %v, want doc-1", entries, err)
	}
}

// connectorBindingsOf decodes a snapshot's data-source bindings, failing when the
// snapshot does not record them.
func connectorBindingsOf(t *testing.T, snapshot entity.JSONMap) []entity.JSONMap {
	t.Helper()
	bindings, recorded, err := datasetversion.ParseConnectorBindings(snapshot)
	if err != nil {
		t.Fatalf("parse snapshot bindings: %v", err)
	}
	if !recorded {
		t.Fatalf("snapshot records no data-source bindings: %#v", snapshot)
	}
	return bindings
}

// TestUpdateDatasetRecordsConnectorBindings pins the record side: a relink is a
// configuration change, the version it produces records the bindings it left
// behind, and the baseline seeded from the state before it records the ones it
// replaced instead of the ones the change just wrote.
func TestUpdateDatasetRecordsConnectorBindings(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")
	insertDatasetUpdateConnector(t, "connector-1", "tenant-1")

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	svc := testDatasetUpdateService(t)
	link := []service.DatasetConnectorRequest{{ID: "connector-1", AutoParse: "0"}}
	if _, code, err := svc.UpdateDataset(ctx, "kb-1", service.UpdateDatasetRequest{Connectors: &link}); err != nil {
		t.Fatalf("update: code=%d err=%v", code, err)
	}

	rows := listDatasetVersions(t, "kb-1")
	if len(rows) != 2 {
		t.Fatalf("versions = %d, want 2 (baseline, config)", len(rows))
	}
	if got := connectorBindingsOf(t, rows[0].Config); len(got) != 0 {
		t.Fatalf("baseline bindings = %#v, want the state before the relink", got)
	}
	bindings := connectorBindingsOf(t, rows[1].Config)
	if len(bindings) != 1 || bindings[0]["id"] != "connector-1" || bindings[0]["auto_parse"] != "0" {
		t.Fatalf("config bindings = %#v, want connector-1 with auto_parse 0", bindings)
	}

	detail, err := svc.GetDatasetVersion(ctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: 1})
	if err != nil {
		t.Fatalf("get version: %v", err)
	}
	named := false
	for _, change := range detail.ConfigChanges {
		if change.Field == "connectors" {
			named = true
		}
	}
	if !named {
		t.Fatalf("config changes = %#v, want the relink named", detail.ConfigChanges)
	}
}

// TestRestoreDatasetVersionRestoresConnectorBindings pins the restore side: a
// binding a later change removed comes back with the parse switch the version
// recorded, and the preview names the relink alone.
func TestRestoreDatasetVersionRestoresConnectorBindings(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")
	insertDatasetUpdateConnector(t, "connector-1", "tenant-1")

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	svc := testDatasetUpdateService(t)

	link := []service.DatasetConnectorRequest{{ID: "connector-1", AutoParse: "0"}}
	if _, code, err := svc.UpdateDataset(ctx, "kb-1", service.UpdateDatasetRequest{Connectors: &link}); err != nil {
		t.Fatalf("link: code=%d err=%v", code, err)
	}
	unlink := []service.DatasetConnectorRequest{}
	if _, code, err := svc.UpdateDataset(ctx, "kb-1", service.UpdateDatasetRequest{Connectors: &unlink}); err != nil {
		t.Fatalf("unlink: code=%d err=%v", code, err)
	}

	if _, err := restoreConfigVersion(ctx, svc, &RestoreDatasetVersionRequest{DatasetID: "kb-1", Version: 1, DryRun: true}); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if _, err := restoreConfigVersion(ctx, svc, &RestoreDatasetVersionRequest{DatasetID: "kb-1", Version: 1}); err != nil {
		t.Fatalf("restore: %v", err)
	}

	var links []entity.Connector2Kb
	if err := db.Where("kb_id = ?", "kb-1").Find(&links).Error; err != nil {
		t.Fatalf("list bindings: %v", err)
	}
	if len(links) != 1 || links[0].ConnectorID != "connector-1" || links[0].AutoParse != "0" {
		t.Fatalf("bindings = %#v, want connector-1 back with auto_parse 0", links)
	}
}

// TestDeleteDatasetVersionReclaimsASetNothingNames covers what the delete of a
// set's last namer now leaves behind. There is no state where a version points at
// a row that is gone - it either names a set or names nothing - so the sweep
// reclaims the row, and the versions that named nothing keep reading as
// "not recorded" instead of failing on a dangling pointer.
func TestDeleteDatasetVersionReclaimsASetNothingNames(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")
	if err := db.Model(&entity.Knowledgebase{}).Where("id = ?", "kb-1").Update("version", 3).Error; err != nil {
		t.Fatalf("pin current version: %v", err)
	}

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	documentSet := []byte(`["doc-1"]`)
	hash := documentset.Hash(documentSet)
	if err := dao.NewDatasetDocumentSetSnapshotDAO().Upsert(ctx, db, "kb-1", hash, documentset.Encode(documentSet)); err != nil {
		t.Fatalf("seed the document set: %v", err)
	}
	rows := []*entity.DatasetVersion{
		// The only version naming the set.
		{DatasetID: "kb-1", Version: 1, Kind: entity.DatasetVersionKindUpload, DocumentsHash: &hash},
		// Two versions that name nothing: the state a row written before the
		// column existed, or one whose read failed, is in.
		{DatasetID: "kb-1", Version: 2, Kind: entity.DatasetVersionKindChunk},
		{DatasetID: "kb-1", Version: 3, Kind: entity.DatasetVersionKindDatasetConfig},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seed version %d: %v", row.Version, err)
		}
	}

	svc := testDatasetUpdateService(t)
	if _, err := svc.DeleteDatasetVersion(ctx, &DeleteDatasetVersionRequest{DatasetID: "kb-1", Version: 1}); err != nil {
		t.Fatalf("delete the last version naming the set: %v", err)
	}

	var stored int64
	if err := db.Model(&entity.DatasetDocumentSetSnapshot{}).Where("dataset_id = ?", "kb-1").Count(&stored).Error; err != nil {
		t.Fatalf("count the stored sets: %v", err)
	}
	if stored != 0 {
		t.Fatalf("stored document sets = %d, want the unreferenced set reclaimed", stored)
	}

	versionDAO := dao.NewDatasetVersionDAO()
	for _, version := range []int64{2, 3} {
		row, err := versionDAO.GetByDatasetAndVersion(ctx, db, "kb-1", version)
		if err != nil {
			t.Fatalf("load version %d: %v", version, err)
		}
		if len(row.Documents) != 0 {
			t.Fatalf("v%d document set = %q, want it unrecorded", version, row.Documents)
		}
	}
}

// TestReadOfAVersionNamingAnEmptySetSucceeds pins the read side: a hash that
// resolves to a row holding no bytes reads as "not recorded" rather than failing,
// so the detail of a version written by a half-finished write still opens. Only a
// hand-written row can be in that state - the writer refuses an empty set.
func TestReadOfAVersionNamingAnEmptySetSucceeds(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	empty := strings.Repeat("0", 64)
	if err := db.Create(&entity.DatasetDocumentSetSnapshot{
		DatasetID: "kb-1", DocumentsHash: empty,
	}).Error; err != nil {
		t.Fatalf("seed an empty document set: %v", err)
	}
	rows := []*entity.DatasetVersion{
		{DatasetID: "kb-1", Version: 1, Kind: entity.DatasetVersionKindUpload, DocumentsHash: &empty},
		{DatasetID: "kb-1", Version: 2, Kind: entity.DatasetVersionKindChunk, DocumentsHash: &empty},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seed version %d: %v", row.Version, err)
		}
	}

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	row, err := dao.NewDatasetVersionDAO().GetByDatasetAndVersion(ctx, db, "kb-1", 2)
	if err != nil {
		t.Fatalf("load a version whose set holds no bytes: %v", err)
	}
	if len(row.Documents) != 0 {
		t.Fatalf("document set = %q, want it unrecorded", row.Documents)
	}
}

// TestDatasetConfigVersionNamesThePipelineAndItsVersion pins the display of a
// dataset moving from its built-in method to a pipeline: the panel names the
// pipeline and the version of it, and clears the built-in method the dataset left
// behind, instead of printing three opaque ids.
func TestDatasetConfigVersionNamesThePipelineAndItsVersion(t *testing.T) {
	db := setupDatasetUpdateTestDB(t)
	pushServiceDB(t, db)
	insertDatasetUpdateKB(t, "kb-1", "tenant-1", "Dataset")

	pipelineID := "abcdef0123456789abcdef0123456789"
	versionID := "version0000000000000000000000aa"
	seedDatasetUpdateCanvas(t, pipelineID, "tenant-1",
		datasetUpdateCanvasDSL("Parser:Shared", "chunk_token_num"))
	seedCanvasVersion(t, db, pipelineID, versionID, 0)
	// The two things a reader needs to tell one pipeline from another, and one
	// version of it from another.
	title, alias := "Contract parser", "v3"
	if err := db.Model(&entity.UserCanvas{}).Where("id = ?", pipelineID).
		Update("title", title).Error; err != nil {
		t.Fatalf("name the canvas: %v", err)
	}
	if err := db.Model(&entity.UserCanvasVersion{}).Where("id = ?", versionID).
		Update("alias", alias).Error; err != nil {
		t.Fatalf("name the canvas version: %v", err)
	}

	ctx := withDatasetTestContext(t.Context(), "tenant-1", "tenant-1")
	parseType := 2
	if _, code, err := testDatasetUpdateService(t).UpdateDataset(ctx, "kb-1", service.UpdateDatasetRequest{
		PipelineID:        &pipelineID,
		ParseType:         &parseType,
		PipelineVersionID: &versionID,
	}); err != nil || code != common.CodeSuccess {
		t.Fatalf("switch to the pipeline: code=%d err=%v", code, err)
	}

	detail, err := testDatasetUpdateService(t).GetDatasetVersion(
		ctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: 1})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	fields := make([]string, 0, len(detail.ConfigChanges))
	byField := make(map[string]DatasetConfigChange, len(detail.ConfigChanges))
	for _, change := range detail.ConfigChanges {
		fields = append(fields, change.Field)
		byField[change.Field] = change
	}
	// The selection comes before the parameters it produced: the switch re-derives
	// parser_config from the canvas, and those lines follow.
	want := []string{"parse_type", "parser_id", "pipeline_id", "pipeline_version_id"}
	if len(fields) < len(want) || strings.Join(fields[:len(want)], ",") != strings.Join(want, ",") {
		t.Fatalf("fields = %v, want them to start with %v", fields, want)
	}
	if mode := byField["parse_type"]; mode.Before != "builtin" || mode.After != "pipeline" {
		t.Fatalf("parse_type = %#v, want the switch to a pipeline", mode)
	}
	// The dataset row keeps the built-in method it last ran under; it is cleared in
	// the panel because the pipeline is what parses the dataset now.
	if method := byField["parser_id"]; method.After != "" {
		t.Fatalf("parser_id = %#v, want the built-in method cleared", method)
	}
	if pipeline := byField["pipeline_id"]; pipeline.Before != nil || pipeline.After != title {
		t.Fatalf("pipeline_id = %#v, want the pipeline's title", pipeline)
	}
	if version := byField["pipeline_version_id"]; version.Before != nil || version.After != alias {
		t.Fatalf("pipeline_version_id = %#v, want the version's alias", version)
	}

	// An alias is a label the user may never have given the version. Without one it
	// still names itself with its title rather than falling back to the id.
	if err := db.Model(&entity.UserCanvasVersion{}).Where("id = ?", versionID).
		Updates(map[string]any{"alias": "", "title": "Contract parser v3"}).Error; err != nil {
		t.Fatalf("retitle the canvas version: %v", err)
	}
	detail, err = testDatasetUpdateService(t).GetDatasetVersion(
		ctx, &DatasetVersionRequest{DatasetID: "kb-1", Version: 1})
	if err != nil {
		t.Fatalf("get after the retitle: %v", err)
	}
	for _, change := range detail.ConfigChanges {
		if change.Field == "pipeline_version_id" && change.After != "Contract parser v3" {
			t.Fatalf("pipeline_version_id after = %v, want the version title", change.After)
		}
	}
}

// TestDatasetConfigDisplayChangesReadsTheNewerSnapshot is the dataset half of the
// document rule: a parameter the newer snapshot carries is listed - including the
// ones a save re-derived from the template, which read as "— → value" - while one
// only the older snapshot carries is not.
func TestDatasetConfigDisplayChangesReadsTheNewerSnapshot(t *testing.T) {
	t.Run("added parameter", func(t *testing.T) {
		before := entity.JSONMap{"parser_config": map[string]interface{}{
			"Parser:Shared": map[string]interface{}{"order_index": 1},
		}}
		after := entity.JSONMap{"parser_config": map[string]interface{}{
			"Parser:Shared": map[string]interface{}{
				"order_index": 1,
				"pages":       []interface{}{[]interface{}{1, 100000}},
			},
		}}

		changes := datasetConfigDisplayChanges(context.Background(), before, after)
		var page *DatasetConfigChange
		for i := range changes {
			if changes[i].Field == "parser_config.Parser:Shared.pages" {
				page = &changes[i]
			}
		}
		if page == nil {
			t.Fatalf("changes = %#v, want the parameter the newer snapshot carries", changes)
		}
		if page.Before != nil || page.After == nil {
			t.Fatalf("pages = %#v, want an empty before side and the value set", *page)
		}
	})

	t.Run("empty parameter with no older value", func(t *testing.T) {
		// The page prints an empty string and an absent value the same way, so the
		// pair would read "— → —": a parameter the save defaulted to "" is not a
		// change a reader can act on.
		before := entity.JSONMap{"parser_config": map[string]interface{}{
			"Parser:Shared": map[string]interface{}{"order_index": 1},
		}}
		after := entity.JSONMap{"parser_config": map[string]interface{}{
			"Parser:Shared": map[string]interface{}{
				"order_index": 1,
				"vlm":         map[string]interface{}{"system_prompt": ""},
			},
		}}

		changes := datasetConfigDisplayChanges(context.Background(), before, after)
		if len(changes) != 0 {
			t.Fatalf("changes = %#v, want the empty pair left out", changes)
		}
	})

	t.Run("parameter cleared to empty", func(t *testing.T) {
		// The value it moved away from is worth a line: the reader cleared it.
		before := entity.JSONMap{"parser_config": map[string]interface{}{
			"Parser:Shared": map[string]interface{}{
				"vlm": map[string]interface{}{"system_prompt": "Describe the page."},
			},
		}}
		after := entity.JSONMap{"parser_config": map[string]interface{}{
			"Parser:Shared": map[string]interface{}{
				"vlm": map[string]interface{}{"system_prompt": ""},
			},
		}}

		changes := datasetConfigDisplayChanges(context.Background(), before, after)
		if len(changes) != 1 || changes[0].Field != "parser_config.Parser:Shared.vlm.system_prompt" {
			t.Fatalf("changes = %#v, want the cleared parameter reported", changes)
		}
	})

	t.Run("dropped parameter", func(t *testing.T) {
		before := entity.JSONMap{"parser_config": map[string]interface{}{
			"Parser:Shared": map[string]interface{}{
				"order_index": 1,
				"pages":       []interface{}{[]interface{}{1, 100000}},
			},
		}}
		after := entity.JSONMap{"parser_config": map[string]interface{}{
			"Parser:Shared": map[string]interface{}{"order_index": 1},
		}}

		changes := datasetConfigDisplayChanges(context.Background(), before, after)
		if len(changes) != 0 {
			t.Fatalf("changes = %#v, want a parameter the newer snapshot lost left out", changes)
		}
	})
}
