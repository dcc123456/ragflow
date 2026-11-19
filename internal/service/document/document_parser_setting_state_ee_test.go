//go:build ee

package document

import (
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/service"
	"ragflow/internal/service/datasetversion"
)

// TestRestoreParserSettingsAtVersion pins the replay: a document's parse settings
// come back from the newest state at or before the version, and a document with
// no state that early keeps what it has.
func TestRestoreParserSettingsAtVersion(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	if err := db.AutoMigrate(&entity.DocumentVersion{}); err != nil {
		t.Fatal(err)
	}
	insertTestKB(t, "kb-1", "tenant-1", 1, 0, 0)
	insertNamedTestDoc(t, "doc-1", "kb-1", "report.txt", 0, 0)

	if err := dao.NewDocumentVersionDAO().CreateBatch(t.Context(), db, []*entity.DocumentVersion{
		{DocumentID: "doc-1", DatasetVersion: 1, DatasetID: "kb-1",
			Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "naive",
			ParserConfig: entity.JSONMap{"marker": "old"}},
		{DocumentID: "doc-1", DatasetVersion: 2, DatasetID: "kb-1",
			Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "general",
			ParserConfig: entity.JSONMap{"marker": "new"}},
	}); err != nil {
		t.Fatal(err)
	}
	// The live row carries the newer settings.
	if err := db.Model(&entity.Document{}).Where("id = ?", "doc-1").Updates(map[string]any{
		"parser_id": "general", "parser_config": entity.JSONMap{"marker": "new"},
	}).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := RestoreParserSettingsAtEE(t.Context(), db, "kb-1", 1); err != nil {
		t.Fatalf("restore version 1: %v", err)
	}
	doc, err := dao.NewDocumentDAO().GetByID(t.Context(), db, "doc-1")
	if err != nil {
		t.Fatal(err)
	}
	if doc.ParserID != "naive" || doc.ParserConfig["marker"] != "old" {
		t.Fatalf("settings = %s %v, want the state version 1 recorded", doc.ParserID, doc.ParserConfig)
	}

	// Before the first state the document predates the history: nothing to put
	// back, so it keeps what it has.
	if _, err := RestoreParserSettingsAtEE(t.Context(), db, "kb-1", 0); err != nil {
		t.Fatalf("restore version 0: %v", err)
	}
	doc, err = dao.NewDocumentDAO().GetByID(t.Context(), db, "doc-1")
	if err != nil {
		t.Fatal(err)
	}
	if doc.ParserID != "naive" || doc.ParserConfig["marker"] != "old" {
		t.Fatalf("settings = %s %v, want them untouched", doc.ParserID, doc.ParserConfig)
	}

	// The replay writes no state of its own: the restore's version is allocated
	// after it runs, and the caller records what it put back under that number.
	var states int64
	if err := db.Model(&entity.DocumentVersion{}).Where("document_id = ?", "doc-1").Count(&states).Error; err != nil {
		t.Fatal(err)
	}
	if states != 2 {
		t.Fatalf("states after the replay = %d, want the two it read", states)
	}
}

// TestInsertDocumentRecordsParserSettingsBaseline pins the first entry: the
// settings a document is created with are recorded, so a restore to a version
// from before its first edit has something to put back.
func TestInsertDocumentRecordsParserSettingsBaseline(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	if err := db.AutoMigrate(&entity.DocumentVersion{}); err != nil {
		t.Fatal(err)
	}
	insertTestKB(t, "kb-1", "tenant-1", 0, 0, 0)

	name := "report.txt"
	doc := &entity.Document{
		ID: "doc-1", KbID: "kb-1", Name: &name, CreatedBy: "user-1",
		ParserID: "naive", ParserConfig: entity.JSONMap{"marker": "first"},
	}
	// The baseline is parked for the version the creating flow records, which is
	// what lets it be written by number instead of by clock.
	ctx := datasetversion.WithStateSink(documentTestContext(t.Context()))
	if err := testDocumentService(t).InsertDocument(ctx, doc); err != nil {
		t.Fatalf("insert: %v", err)
	}
	// Nothing is written while the flow still holds its transaction open.
	var pending int64
	if err := db.Model(&entity.DocumentVersion{}).Count(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatalf("rows before the version = %d, want the state parked", pending)
	}

	datasetversion.FlushStates(ctx, db, "kb-1", 3)

	var rows []entity.DocumentVersion
	if err := db.Where("document_id = ?", "doc-1").Order("kind").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	// The creation is two states of the one version: the settings the document
	// runs under, and the values it holds - none, which is what a restore to the
	// version that added it puts back.
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want the creation settings and the creation values", len(rows))
	}
	var settings *entity.DocumentVersion
	var values *entity.DocumentVersion
	for i := range rows {
		switch rows[i].Kind {
		case entity.DatasetVersionKindDocumentConfig:
			settings = &rows[i]
		case entity.DatasetVersionKindDocumentMetadata:
			values = &rows[i]
		}
	}
	if settings == nil || settings.DatasetVersion != 3 ||
		settings.ParserID != "naive" || settings.ParserConfig["marker"] != "first" {
		t.Fatalf("settings row = %+v, want the creation settings under version 3", settings)
	}
	if values == nil || values.DatasetVersion != 3 {
		t.Fatalf("values row = %+v, want the creation values under version 3", values)
	}
}

// TestApplyDatasetParserConfigRecordsAVersion pins the timeline contract:
// applying the dataset's settings to existing documents changes what their next
// parse runs under, so it appends one document_config version naming the
// documents it moved and writes their settings into their own history.
func TestApplyDatasetParserConfigRecordsAVersion(t *testing.T) {
	db := setupConcurrentServiceTestDB(t)
	pushServiceDB(t, db)
	insertTestKB(t, "kb-1", "tenant-1", 2, 0, 0)
	if err := db.Model(&entity.Knowledgebase{}).Where("id = ?", "kb-1").Updates(map[string]any{
		"parser_id": "general", "parser_config": generalParserConfig(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	insertNamedTestDoc(t, "doc-1", "kb-1", "report.txt", 0, 0)
	insertNamedTestDoc(t, "doc-2", "kb-1", "report.txt", 0, 0)
	// doc-2 already runs the dataset's settings, so only doc-1 is a change.
	if err := db.Model(&entity.Document{}).Where("id = ?", "doc-2").Updates(map[string]any{
		"parser_id": "general", "parser_config": generalParserConfig(),
	}).Error; err != nil {
		t.Fatal(err)
	}

	svc := testDocumentService(t)
	result, code, err := svc.ApplyDatasetParserConfig(documentTestContext(t.Context()), "kb-1")
	if err != nil || code != common.CodeSuccess {
		t.Fatalf("apply: code=%d err=%v", code, err)
	}
	if result.UpdatedDocuments != 1 {
		t.Fatalf("updated = %d, want 1", result.UpdatedDocuments)
	}

	var versions []entity.DatasetVersion
	if err := db.Where("dataset_id = ? AND kind = ?", "kb-1", entity.DatasetVersionKindDocumentConfig).
		Find(&versions).Error; err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 {
		t.Fatalf("document_config versions = %d, want 1", len(versions))
	}
	if len(versions[0].Changes) != 1 || versions[0].Changes[0].DocID != "doc-1" {
		t.Fatalf("changes = %+v, want only doc-1", versions[0].Changes)
	}

	// The moved document gets a state under the version, so a replay of it
	// survives a later edit in the same millisecond; the unchanged one gets
	// nothing, because nothing about it moved.
	var states []entity.DocumentVersion
	if err := db.Order("document_id").Find(&states).Error; err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].DocumentID != "doc-1" {
		t.Fatalf("version-keyed states = %+v, want only doc-1", states)
	}
	if states[0].DatasetVersion != versions[0].Version {
		t.Fatalf("state version = %d, want the version that recorded it (%d)",
			states[0].DatasetVersion, versions[0].Version)
	}
	if states[0].ParserID != "general" {
		t.Fatalf("state parser = %q, want the dataset's parser", states[0].ParserID)
	}
}

// TestUpdateDatasetDocumentRecordsParserSettingsVersion pins the timeline
// contract for a single document: moving its parse settings appends a
// document_config version naming it and writes the settings into its history,
// while a save that resends the settings it already runs under appends nothing,
// and a rename appends a document_rename version of its own.
func TestUpdateDatasetDocumentRecordsParserSettingsVersion(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	insertTestKB(t, "kb-1", "tenant-1", 1, 0, 0)
	insertNamedTestDoc(t, "doc-1", "kb-1", "report.txt", 0, 0)
	svc := testDocumentService(t)

	_, code, err := svc.UpdateDatasetDocument(documentTestContext(t.Context()), "kb-1", "doc-1",
		&UpdateDatasetDocumentRequest{
			ParserConfig: map[string]any{"GeneralChunker:SixApplesFall": map[string]any{"chunk_token_size": 256}},
		}, map[string]bool{"parser_config": true})
	if err != nil || code != common.CodeSuccess {
		t.Fatalf("update: code=%d err=%v", code, err)
	}

	var versions []entity.DatasetVersion
	if err := db.Where("dataset_id = ? AND kind = ?", "kb-1", entity.DatasetVersionKindDocumentConfig).
		Find(&versions).Error; err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 {
		t.Fatalf("document_config versions = %d, want 1", len(versions))
	}
	if len(versions[0].Changes) != 1 || versions[0].Changes[0].DocID != "doc-1" {
		t.Fatalf("changes = %+v, want only doc-1", versions[0].Changes)
	}
	// The same change is recorded against the version it took, so a restore can
	// read the state by number instead of by millisecond.
	var states []entity.DocumentVersion
	if err := db.Where("document_id = ?", "doc-1").Find(&states).Error; err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("version-keyed states = %d rows, want 1", len(states))
	}
	if states[0].DatasetVersion != versions[0].Version {
		t.Fatalf("state version = %d, want the version that recorded it (%d)",
			states[0].DatasetVersion, versions[0].Version)
	}
	if _, ok := states[0].ParserConfig["GeneralChunker:SixApplesFall"]; !ok {
		t.Fatalf("state parser_config = %v, want the submitted configuration", states[0].ParserConfig)
	}

	// A save that resends the settings the document already runs under changes
	// nothing: the form submits every field it shows, so a second click on Save
	// must not append a version or a settings row.
	var saved entity.Document
	if err := db.Where("id = ?", "doc-1").Take(&saved).Error; err != nil {
		t.Fatalf("reload document: %v", err)
	}
	builtin := int(service.ParseModeBuiltIn)
	if _, code, err := svc.UpdateDatasetDocument(documentTestContext(t.Context()), "kb-1", "doc-1",
		&UpdateDatasetDocumentRequest{
			ParseType:    &builtin,
			ParserID:     &saved.ParserID,
			ParserConfig: saved.ParserConfig,
		}, map[string]bool{"parser_config": true}); err != nil || code != common.CodeSuccess {
		t.Fatalf("resave: code=%d err=%v", code, err)
	}
	var unchanged int64
	if err := db.Model(&entity.DatasetVersion{}).
		Where("dataset_id = ? AND kind = ?", "kb-1", entity.DatasetVersionKindDocumentConfig).
		Count(&unchanged).Error; err != nil {
		t.Fatal(err)
	}
	if unchanged != 1 {
		t.Fatalf("document_config versions after an unchanged save = %d, want 1", unchanged)
	}
	if err := db.Model(&entity.DocumentVersion{}).
		Where("document_id = ?", "doc-1").Count(&unchanged).Error; err != nil {
		t.Fatal(err)
	}
	if unchanged != 1 {
		t.Fatalf("version-keyed states after an unchanged save = %d rows, want 1", unchanged)
	}

	// A rename does not touch the parse settings, so it is not a settings
	// change.
	insertTestFile(t, "file-1", "folder-1", "report.txt", new("report.txt"))
	insertTestFile2Document(t, "f2d-1", "file-1", "doc-1")
	if _, code, err := svc.UpdateDatasetDocument(documentTestContext(t.Context()), "kb-1", "doc-1",
		&UpdateDatasetDocumentRequest{Name: new("renamed.txt")}, map[string]bool{"name": true}); err != nil || code != common.CodeSuccess {
		t.Fatalf("rename: code=%d err=%v", code, err)
	}
	var after int64
	if err := db.Model(&entity.DatasetVersion{}).
		Where("dataset_id = ? AND kind = ?", "kb-1", entity.DatasetVersionKindDocumentConfig).
		Count(&after).Error; err != nil {
		t.Fatal(err)
	}
	if after != 1 {
		t.Fatalf("document_config versions after rename = %d, want 1", after)
	}

	// The rename is a version of its own kind: the timeline says who renamed which
	// file, and the entry carries the new name so it reads "Renamed document
	// renamed.txt" instead of only naming the document.
	var renames []entity.DatasetVersion
	if err := db.Where("dataset_id = ? AND kind = ?", "kb-1", entity.DatasetVersionKindDocumentRename).
		Find(&renames).Error; err != nil {
		t.Fatal(err)
	}
	if len(renames) != 1 {
		t.Fatalf("document_rename versions = %d, want 1", len(renames))
	}
	if len(renames[0].Changes) != 1 {
		t.Fatalf("changes = %+v, want the renamed document", renames[0].Changes)
	}
	rename := renames[0].Changes[0]
	if rename.DocID != "doc-1" || rename.Action != entity.DatasetVersionChangeActionRename ||
		rename.Name != "renamed.txt" {
		t.Fatalf("change = %+v, want doc-1 renamed to renamed.txt", rename)
	}
}

// TestUpdateDatasetDocumentRecordsMetadataVersion pins the third kind of document
// edit: the metadata values live in the metadata store, which a restore does not
// rebuild, so a version is the only place that says whose metadata moved.
func TestUpdateDatasetDocumentRecordsMetadataVersion(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	insertTestKB(t, "kb-1", "tenant-1", 1, 0, 0)
	insertNamedTestDoc(t, "doc-1", "kb-1", "doc.txt", 0, 0)
	svc := testDocumentService(t)

	if _, code, err := svc.UpdateDatasetDocument(documentTestContext(t.Context()), "kb-1", "doc-1",
		&UpdateDatasetDocumentRequest{MetaFields: map[string]any{"author": "alice"}},
		map[string]bool{"meta_fields": true}); err != nil || code != common.CodeSuccess {
		t.Fatalf("update metadata: code=%d err=%v", code, err)
	}

	var versions []entity.DatasetVersion
	if err := db.Where("dataset_id = ? AND kind = ?", "kb-1", entity.DatasetVersionKindDocumentMetadata).
		Find(&versions).Error; err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 {
		t.Fatalf("document_metadata versions = %d, want 1", len(versions))
	}
	if len(versions[0].Changes) != 1 {
		t.Fatalf("changes = %+v, want the edited document", versions[0].Changes)
	}
	change := versions[0].Changes[0]
	if change.DocID != "doc-1" || change.Action != entity.DatasetVersionChangeActionUpdate ||
		change.Name != "doc.txt" {
		t.Fatalf("change = %+v, want doc-1 with its name", change)
	}

	// A settings save that carries no metadata leaves the metadata timeline alone.
	var before int64
	if err := db.Model(&entity.DatasetVersion{}).
		Where("dataset_id = ? AND kind = ?", "kb-1", entity.DatasetVersionKindDocumentMetadata).
		Count(&before).Error; err != nil {
		t.Fatal(err)
	}
	if _, code, err := svc.UpdateDatasetDocument(documentTestContext(t.Context()), "kb-1", "doc-1",
		&UpdateDatasetDocumentRequest{Name: new("doc.txt")},
		map[string]bool{"name": true}); err != nil || code != common.CodeSuccess {
		t.Fatalf("save without metadata: code=%d err=%v", code, err)
	}
	var after int64
	if err := db.Model(&entity.DatasetVersion{}).
		Where("dataset_id = ? AND kind = ?", "kb-1", entity.DatasetVersionKindDocumentMetadata).
		Count(&after).Error; err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("document_metadata versions after a save without metadata = %d, want %d", after, before)
	}
}

// TestRestoreParserSettingsDistinguishesStatesInOneMillisecond pins the reason
// the states are addressed by version: two edits that land in one millisecond are
// two states, so only the version numbers can still tell them apart.
func TestRestoreParserSettingsDistinguishesStatesInOneMillisecond(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	insertTestKB(t, "kb-1", "tenant-1", 1, 0, 0)
	insertNamedTestDoc(t, "doc-1", "kb-1", "report.txt", 0, 0)

	const sameMillisecond = 100
	// Both states share the moment and are told apart by their numbers.
	if err := dao.NewDocumentVersionDAO().CreateBatch(t.Context(), db,
		[]*entity.DocumentVersion{
			{
				DocumentID: "doc-1", DatasetVersion: 5, DatasetID: "kb-1", Kind: entity.DatasetVersionKindDocumentConfig,
				ParserID: "naive", ParserConfig: entity.JSONMap{"marker": "first"},
			},
			{
				DocumentID: "doc-1", DatasetVersion: 6, DatasetID: "kb-1", Kind: entity.DatasetVersionKindDocumentConfig,
				ParserID: "general", ParserConfig: entity.JSONMap{"marker": "second"},
			},
			// A later change, so restoring version 6 has a document to move back.
			{
				DocumentID: "doc-1", DatasetVersion: 7, DatasetID: "kb-1", Kind: entity.DatasetVersionKindDocumentConfig,
				ParserID: "general", ParserConfig: entity.JSONMap{"marker": "third"},
			},
		}); err != nil {
		t.Fatal(err)
	}
	// The live row carries the latest settings, as it does after the last edit.
	if err := db.Model(&entity.Document{}).Where("id = ?", "doc-1").Updates(map[string]any{
		"parser_id": "general", "parser_config": entity.JSONMap{"marker": "third"},
	}).Error; err != nil {
		t.Fatal(err)
	}

	// Restoring version 5 means undoing the two later edits, which a clock-keyed
	// record could no longer express.
	if _, err := RestoreParserSettingsAtEE(t.Context(), db, "kb-1", 5); err != nil {
		t.Fatalf("restore version 5: %v", err)
	}
	doc, err := dao.NewDocumentDAO().GetByID(t.Context(), db, "doc-1")
	if err != nil {
		t.Fatal(err)
	}
	if doc.ParserID != "naive" || doc.ParserConfig["marker"] != "first" {
		t.Fatalf("settings at version 5 = %s %v, want the first edit", doc.ParserID, doc.ParserConfig)
	}

	// And version 6 is the second edit, not the third.
	if _, err := RestoreParserSettingsAtEE(t.Context(), db, "kb-1", 6); err != nil {
		t.Fatalf("restore version 6: %v", err)
	}
	doc, err = dao.NewDocumentDAO().GetByID(t.Context(), db, "doc-1")
	if err != nil {
		t.Fatal(err)
	}
	if doc.ParserID != "general" || doc.ParserConfig["marker"] != "second" {
		t.Fatalf("settings at version 6 = %s %v, want the second edit", doc.ParserID, doc.ParserConfig)
	}
}
