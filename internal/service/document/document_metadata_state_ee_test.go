//go:build ee

package document

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/engine/history"
	"ragflow/internal/engine/history/historytest"
	"ragflow/internal/entity"
	"ragflow/internal/service"
	"ragflow/internal/service/datasetversion"
)

// documentMetadataStatesOf reads a document's metadata states, oldest first.
func documentMetadataStatesOf(t *testing.T, db *gorm.DB, documentID string) []entity.DocumentVersion {
	t.Helper()
	var rows []entity.DocumentVersion
	if err := db.Where("document_id = ? AND kind = ?", documentID,
		entity.DatasetVersionKindDocumentMetadata).
		Order("dataset_version").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	// The values live in the document engine when a store is installed, so the
	// rows are loaded back before anything compares them.
	pointers := make([]*entity.DocumentVersion, len(rows))
	for i := range rows {
		pointers[i] = &rows[i]
	}
	if err := history.HydrateMetadata(t.Context(), pointers); err != nil {
		t.Fatalf("hydrate the metadata states: %v", err)
	}
	return rows
}

// documentMetadataVersionCount counts the timeline entries a document's value
// changes produced.
func documentMetadataVersionCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	if err := db.Model(&entity.DatasetVersion{}).
		Where("dataset_id = ? AND kind = ?", "kb-1", entity.DatasetVersionKindDocumentMetadata).
		Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

// TestRecordDocumentMetadataHistoryAppendsOnlyRealChanges pins the grain: a write
// that leaves the values as they were records nothing, a write that moves them
// records one version and one state holding the whole map, and a removal is an
// empty map rather than a missing row.
func TestRecordDocumentMetadataHistoryAppendsOnlyRealChanges(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	insertTestKB(t, "kb-1", "tenant-1", 1, 0, 0)
	insertNamedTestDoc(t, "doc-1", "kb-1", "report.txt", 0, 0)

	// An earlier state of the document, under version 0.
	if err := dao.NewDocumentVersionDAO().CreateBatch(t.Context(), db, []*entity.DocumentVersion{{
		DocumentID: "doc-1", DatasetVersion: 0, DatasetID: "kb-1",
		Kind:       entity.DatasetVersionKindDocumentMetadata,
		MetaFields: entity.JSONMap{"author": "alice"},
	}}); err != nil {
		t.Fatal(err)
	}

	// The same values with a nil entry: that is the state the document already
	// holds, so it is not a moment.
	recordDocumentMetadataState(t.Context(), db, "doc-1", "kb-1",
		map[string]interface{}{"author": "alice", "note": nil})
	if rows := documentMetadataStatesOf(t, db, "doc-1"); len(rows) != 1 {
		t.Fatalf("states after an unchanged write = %d, want 1: %+v", len(rows), rows)
	}
	if versions := documentMetadataVersionCount(t, db); versions != 0 {
		t.Fatalf("versions after an unchanged write = %d, want none", versions)
	}

	recordDocumentMetadataState(t.Context(), db, "doc-1", "kb-1",
		map[string]interface{}{"author": "bob"})
	rows := documentMetadataStatesOf(t, db, "doc-1")
	if len(rows) != 2 {
		t.Fatalf("states after a change = %d, want 2: %+v", len(rows), rows)
	}
	if rows[1].MetaFields["author"] != "bob" || rows[1].DatasetID != "kb-1" {
		t.Fatalf("second state = %+v, want bob in kb-1", rows[1])
	}
	if rows[1].DatasetVersion <= rows[0].DatasetVersion {
		t.Fatalf("second state is under version %d, want a number after %d",
			rows[1].DatasetVersion, rows[0].DatasetVersion)
	}
	if versions := documentMetadataVersionCount(t, db); versions != 1 {
		t.Fatalf("versions after a change = %d, want 1", versions)
	}

	// A removal is an empty map, not a missing row: without it, restoring to a
	// version after the removal would read the earlier values and put them back.
	insertNamedTestDoc(t, "doc-3", "kb-1", "notes.md", 0, 0)
	if err := dao.NewDocumentVersionDAO().CreateBatch(t.Context(), db, []*entity.DocumentVersion{{
		DocumentID: "doc-3", DatasetVersion: 0, DatasetID: "kb-1",
		Kind:       entity.DatasetVersionKindDocumentMetadata,
		MetaFields: entity.JSONMap{"author": "alice"},
	}}); err != nil {
		t.Fatal(err)
	}
	recordDocumentMetadataState(t.Context(), db, "doc-3", "kb-1", nil)
	removed := documentMetadataStatesOf(t, db, "doc-3")
	if len(removed) != 2 || len(removed[1].MetaFields) != 0 {
		t.Fatalf("states after a removal = %+v, want an empty map last", removed)
	}
}

// TestUpdateDatasetDocumentRecordsMetadataHistory pins the wiring: a metadata
// edit through the document endpoint records one timeline entry and one state
// holding the values the document was left with, scoped to its dataset - the row
// a later restore reads.
func TestUpdateDatasetDocumentRecordsMetadataHistory(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	insertTestKB(t, "kb-1", "tenant-1", 1, 0, 0)
	insertNamedTestDoc(t, "doc-1", "kb-1", "doc.txt", 0, 0)

	engine := newMetadataDocEngine(map[string]map[string]interface{}{
		"doc-1": {"author": "alice"},
	}, map[string]string{"doc-1": "kb-1"})
	svc := testDocumentService(t)
	svc.docEngine = engine
	svc.metadataSvc = service.NewMetadataServiceForTest(dao.NewKnowledgebaseDAO(), engine)

	if _, code, err := svc.UpdateDatasetDocument(documentTestContext(t.Context()), "kb-1", "doc-1",
		&UpdateDatasetDocumentRequest{MetaFields: map[string]any{"author": "bob"}},
		map[string]bool{"meta_fields": true}); err != nil || code != common.CodeSuccess {
		t.Fatalf("update metadata: code=%d err=%v", code, err)
	}

	rows := documentMetadataStatesOf(t, db, "doc-1")
	if len(rows) != 1 {
		t.Fatalf("metadata states = %d rows, want 1: %+v", len(rows), rows)
	}
	if rows[0].DatasetID != "kb-1" || rows[0].MetaFields["author"] != "bob" {
		t.Fatalf("state = %+v, want bob in kb-1", rows[0])
	}
	// The values the store holds and the row that describes them agree.
	if got := engine.records["doc-1"]["author"]; got != "bob" {
		t.Fatalf("stored author = %#v, want bob", got)
	}
}

// recordingMetadataWriter is the writer a replay test needs: it records what the
// replay asked the store to hold, without a document engine behind it.
type recordingMetadataWriter struct {
	written map[string]map[string]interface{}
	failOn  string
}

func (w *recordingMetadataWriter) writeDocumentMetadataHistory(_ context.Context, documentID string, meta map[string]interface{}) error {
	if w.failOn == documentID {
		return context.Canceled
	}
	if w.written == nil {
		w.written = make(map[string]map[string]interface{})
	}
	w.written[documentID] = meta
	return nil
}

// seedMetadataState writes one metadata state of a document under a version.
func seedMetadataState(t *testing.T, db *gorm.DB, documentID string, datasetVersion int64, at int64, author string) {
	t.Helper()
	if err := dao.NewDocumentVersionDAO().CreateBatch(t.Context(), db, []*entity.DocumentVersion{{
		DocumentID: documentID, DatasetVersion: datasetVersion, DatasetID: "kb-1",
		Kind:       entity.DatasetVersionKindDocumentMetadata,
		MetaFields: entity.JSONMap{"author": author},
	}}); err != nil {
		t.Fatalf("seed metadata state of %s at version %d: %v", documentID, datasetVersion, err)
	}
}

// TestRestoreDocumentMetadataAtVersion pins the replay: a document's values come
// back from the newest state at or before the version, a document with no state
// that early keeps what it has, and a document that is still gone is left out
// instead of failing the replay of the documents that are there.
func TestRestoreDocumentMetadataAtVersion(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	insertTestKB(t, "kb-1", "tenant-1", 1, 0, 0)
	insertNamedTestDoc(t, "doc-1", "kb-1", "report.txt", 0, 0)

	seedMetadataState(t, db, "doc-1", 1, 100, "alice")
	seedMetadataState(t, db, "doc-1", 2, 200, "bob")
	// A change after both, so restoring to either of them has a document to move
	// back - a restore of the head itself has nothing to do.
	seedMetadataState(t, db, "doc-1", 3, 300, "carol")

	writer := &recordingMetadataWriter{}
	if _, err := restoreDocumentMetadataAt(t.Context(), db, "kb-1", 1, nil, false, writer); err != nil {
		t.Fatalf("restore version 1: %v", err)
	}
	if got := writer.written["doc-1"]; got["author"] != "alice" {
		t.Fatalf("written = %v, want the state version 1 recorded", writer.written)
	}

	writer = &recordingMetadataWriter{}
	if _, err := restoreDocumentMetadataAt(t.Context(), db, "kb-1", 2, nil, false, writer); err != nil {
		t.Fatalf("restore version 2: %v", err)
	}
	if got := writer.written["doc-1"]; got["author"] != "bob" {
		t.Fatalf("written = %v, want the state version 2 recorded", writer.written)
	}

	// Restoring the head changes nothing: no state is newer than it.
	writer = &recordingMetadataWriter{}
	if _, err := restoreDocumentMetadataAt(t.Context(), db, "kb-1", 3, nil, false, writer); err != nil {
		t.Fatalf("restore version 3: %v", err)
	}
	if len(writer.written) != 0 {
		t.Fatalf("restore of the head wrote %v, want nothing", writer.written)
	}

	// Before the first state the document predates the history: nothing to put
	// back, so it is left alone.
	writer = &recordingMetadataWriter{}
	if _, err := restoreDocumentMetadataAt(t.Context(), db, "kb-1", 0, nil, false, writer); err != nil {
		t.Fatalf("restore version 0: %v", err)
	}
	if len(writer.written) != 0 {
		t.Fatalf("restore at version 0 wrote %v, want nothing", writer.written)
	}

	// A document that is still gone - removed after its values last moved, with
	// no row left to bring it back - has nowhere to put its values.
	seedMetadataState(t, db, "doc-9", 4, 400, "ghost")
	writer = &recordingMetadataWriter{}
	if _, err := restoreDocumentMetadataAt(t.Context(), db, "kb-1", 1, nil, false, writer); err != nil {
		t.Fatalf("restore version 1 with a removed document: %v", err)
	}
	if _, ok := writer.written["doc-9"]; ok {
		t.Fatalf("restore wrote %v for a document that is gone", writer.written["doc-9"])
	}
	if got := writer.written["doc-1"]; got["author"] != "alice" {
		t.Fatalf("restore wrote %v, want the state version 1 recorded for doc-1", writer.written)
	}
}

// TestRestoreDocumentMetadataRewritesADocumentBroughtBackFromRemoval is the
// case a metadata edit followed by a document delete produces: the state at the
// version is the newest one there is, yet the document's store is empty because
// the removal dropped it. Reading "no newer state" as "the store still holds
// what it held then" left the restored document with no metadata at all.
func TestRestoreDocumentMetadataRewritesADocumentBroughtBackFromRemoval(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	insertTestKB(t, "kb-1", "tenant-1", 1, 0, 0)
	// The document is back: this restore put its row back from the removal.
	insertNamedTestDoc(t, "doc-1", "kb-1", "report.txt", 0, 0)

	// Its values were recorded at version 2 and never moved again - the removal
	// at version 3 is a document version, not a metadata state - so version 4,
	// the restore of version 2 that brought the document back, is the newest
	// state a reader would find.
	seedMetadataState(t, db, "doc-1", 1, 100, "aaa")
	seedMetadataState(t, db, "doc-1", 2, 200, "bbb")

	writer := &recordingMetadataWriter{}
	if _, err := restoreDocumentMetadataAt(t.Context(), db, "kb-1", 4, []string{"doc-1"}, false, writer); err != nil {
		t.Fatalf("restore version 4 of a returned document: %v", err)
	}
	if got := writer.written["doc-1"]; got["author"] != "bbb" {
		t.Fatalf("written = %v, want the values version 2 recorded for the returned document", writer.written)
	}

	// Without the removal the same replay writes nothing: a document that never
	// moved keeps the store it has.
	writer = &recordingMetadataWriter{}
	if _, err := restoreDocumentMetadataAt(t.Context(), db, "kb-1", 4, nil, false, writer); err != nil {
		t.Fatalf("restore version 4 without a returned document: %v", err)
	}
	if len(writer.written) != 0 {
		t.Fatalf("restore wrote %v, want nothing for a document that never moved", writer.written)
	}

	// Restoring the metadata change itself replays the documents it recorded, so
	// a store this replay emptied earlier is filled again from the version.
	writer = &recordingMetadataWriter{}
	if _, err := restoreDocumentMetadataAt(t.Context(), db, "kb-1", 2, nil, true, writer); err != nil {
		t.Fatalf("restore the metadata change at version 2: %v", err)
	}
	if got := writer.written["doc-1"]; got["author"] != "bbb" {
		t.Fatalf("written = %v, want the values version 2 recorded", writer.written)
	}
}

// TestRestoreDocumentMetadataAtVersionStopsOnWriteFailure pins that a failed
// write is reported instead of being recorded as a replay that did not happen.
func TestRestoreDocumentMetadataAtVersionStopsOnWriteFailure(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	insertTestKB(t, "kb-1", "tenant-1", 1, 0, 0)
	insertNamedTestDoc(t, "doc-1", "kb-1", "report.txt", 0, 0)

	// A state at the version being restored, so the replay has something to put
	// back, and a later one, so the document is a candidate.
	seedMetadataState(t, db, "doc-1", 0, 50, "old")
	seedMetadataState(t, db, "doc-1", 1, 100, "alice")
	seedMetadataState(t, db, "doc-1", 2, 200, "bob")

	writer := &recordingMetadataWriter{failOn: "doc-1"}
	if _, err := restoreDocumentMetadataAt(t.Context(), db, "kb-1", 0, nil, false, writer); err == nil {
		t.Fatal("a failed write should be reported")
	}
}

// TestRecordMetadataVersionChangesAppendsOneTimelineRow pins the half of a batch
// metadata edit the versions page reads: one document_metadata version per
// batch, naming every document whose values moved, and no row at all for a batch
// that moved nothing.
func TestRecordMetadataVersionChangesAppendsOneTimelineRow(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	insertTestKB(t, "kb-1", "tenant-1", 1, 0, 0)

	recordMetadataVersionChanges(t.Context(), "kb-1", nil, nil)
	recordMetadataVersionChanges(t.Context(), "kb-1", []entity.DatasetVersionChange{}, nil)

	if versions := documentMetadataVersionCount(t, db); versions != 0 {
		t.Fatalf("versions after a batch that moved nothing = %d, want 0", versions)
	}

	recordMetadataVersionChanges(t.Context(), "kb-1", []entity.DatasetVersionChange{
		{DocID: "doc-1", Entity: entity.DatasetVersionChangeEntityDocument,
			Action: entity.DatasetVersionChangeActionUpdate, Name: "report.txt"},
		{DocID: "doc-2", Entity: entity.DatasetVersionChangeEntityDocument,
			Action: entity.DatasetVersionChangeActionUpdate, Name: "notes.md"},
	}, map[string]map[string]interface{}{
		"doc-1": {"author": "alice"},
		"doc-2": {"author": "bob"},
	})

	var rows []entity.DatasetVersion
	if err := db.Where("dataset_id = ? AND kind = ?", "kb-1",
		entity.DatasetVersionKindDocumentMetadata).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("document_metadata versions = %d, want one row per batch", len(rows))
	}
	if len(rows[0].Changes) != 2 {
		t.Fatalf("changes = %+v, want both documents", rows[0].Changes)
	}
	if first := rows[0].Changes[0]; first.DocID != "doc-1" || first.Name != "report.txt" ||
		first.Action != entity.DatasetVersionChangeActionUpdate {
		t.Fatalf("first change = %+v, want doc-1 updated with its name", first)
	}
	if total := rows[0].ChangesTotal; total != 2 {
		t.Fatalf("changes_total = %d, want 2", total)
	}

	// Each moved document's values are kept under the version, so a replay of it
	// survives a later edit in the same millisecond.
	var states []entity.DocumentVersion
	if err := db.Where("dataset_version = ? AND kind = ?",
		rows[0].Version, entity.DatasetVersionKindDocumentMetadata).
		Order("document_id").Find(&states).Error; err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 {
		t.Fatalf("version-keyed states = %d, want one per moved document", len(states))
	}
	if states[0].DocumentID != "doc-1" || states[1].DocumentID != "doc-2" {
		t.Fatalf("states = %+v, want doc-1 and doc-2", states)
	}
}

// TestRestoreDocumentMetadataDistinguishesStatesInOneMillisecond is the reason
// the states are addressed by version: two edits in one millisecond are two
// states, and each version reads back its own.
func TestRestoreDocumentMetadataDistinguishesStatesInOneMillisecond(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	insertTestKB(t, "kb-1", "tenant-1", 1, 0, 0)
	insertNamedTestDoc(t, "doc-1", "kb-1", "report.txt", 0, 0)

	const sameMillisecond = 100
	seedMetadataState(t, db, "doc-1", 5, sameMillisecond, "alice")
	seedMetadataState(t, db, "doc-1", 6, sameMillisecond, "bob")
	// A later change, so restoring version 6 has a document to move back.
	seedMetadataState(t, db, "doc-1", 7, sameMillisecond, "carol")

	writer := &recordingMetadataWriter{}
	if _, err := restoreDocumentMetadataAt(t.Context(), db, "kb-1", 5, nil, false, writer); err != nil {
		t.Fatalf("restore version 5: %v", err)
	}
	if got := writer.written["doc-1"]; got["author"] != "alice" {
		t.Fatalf("values at version 5 = %v, want the first edit", got)
	}

	writer = &recordingMetadataWriter{}
	if _, err := restoreDocumentMetadataAt(t.Context(), db, "kb-1", 6, nil, false, writer); err != nil {
		t.Fatalf("restore version 6: %v", err)
	}
	if got := writer.written["doc-1"]; got["author"] != "bob" {
		t.Fatalf("values at version 6 = %v, want the second edit", got)
	}
}

// failingMetadataStore refuses every write, which is the shape of an engine that
// is reachable but will not hold the values.
type failingMetadataStore struct {
	historytest.MetadataStore
}

func (s *failingMetadataStore) StashMetadataHistory(context.Context, string, string, []map[string]interface{}) error {
	return errors.New("engine refused the write")
}

// TestMetadataStateRowStashesAtInsert pins the gate every insert path crosses: a
// metadata row written straight through the DAO cannot land with its values still
// inline while a store is installed - the hook puts them in the engine and the
// row keeps the state's name.
func TestMetadataStateRowStashesAtInsert(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	insertTestKB(t, "kb-1", "tenant-1", 1, 0, 0)

	store := historytest.NewMetadataStore()
	history.SetMetadataStore(store)
	t.Cleanup(func() { history.SetMetadataStore(nil) })

	row := &entity.DocumentVersion{
		DocumentID: "doc-1", DatasetVersion: 1, DatasetID: "kb-1",
		Kind:       entity.DatasetVersionKindDocumentMetadata,
		MetaFields: entity.JSONMap{"author": "alice"},
	}
	if err := dao.NewDocumentVersionDAO().CreateBatch(t.Context(), db, []*entity.DocumentVersion{row}); err != nil {
		t.Fatalf("insert a metadata state: %v", err)
	}
	if row.MetaFieldsSnapshotID == "" {
		t.Fatal("the row was written without a state holding its values")
	}
	if ids := store.SnapshotIDs("kb-1"); len(ids) != 1 || ids[0] != row.MetaFieldsSnapshotID {
		t.Fatalf("states in the engine = %v, want %q", ids, row.MetaFieldsSnapshotID)
	}
	if got := documentMetadataStatesOf(t, db, "doc-1"); len(got) != 1 || got[0].MetaFields["author"] != "alice" {
		t.Fatalf("state read back = %+v, want the values that were stashed", got)
	}
}

// TestMetadataStateRowFailsWhenTheStoreRefuses pins the failure policy: a row
// whose values the engine would not take is not written at all, so the history
// never names a state nobody holds.
func TestMetadataStateRowFailsWhenTheStoreRefuses(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	insertTestKB(t, "kb-1", "tenant-1", 1, 0, 0)

	history.SetMetadataStore(&failingMetadataStore{})
	t.Cleanup(func() { history.SetMetadataStore(nil) })

	row := &entity.DocumentVersion{
		DocumentID: "doc-1", DatasetVersion: 1, DatasetID: "kb-1",
		Kind:       entity.DatasetVersionKindDocumentMetadata,
		MetaFields: entity.JSONMap{"author": "alice"},
	}
	if err := dao.NewDocumentVersionDAO().CreateBatch(t.Context(), db, []*entity.DocumentVersion{row}); err == nil {
		t.Fatal("the row was written although the engine refused its values")
	}
	var count int64
	if err := db.Model(&entity.DocumentVersion{}).Where("document_id = ?", "doc-1").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rows after the refused write = %d, want 0", count)
	}
}

// TestMetadataStateRowFailsWithoutStore pins what an engine outside the
// supported set gets: versioning supports Elasticsearch and Infinity, and a
// deployment whose engine keeps no states is told so when a value changes rather
// than given a row that would replay an empty map.
func TestMetadataStateRowFailsWithoutStore(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	insertTestKB(t, "kb-1", "tenant-1", 1, 0, 0)

	history.SetMetadataStore(nil)

	err := recordDocumentVersionMetadata(t.Context(), db, 1, "doc-1", "kb-1",
		map[string]interface{}{"author": "alice"})
	if !errors.Is(err, history.ErrMetadataUnavailable) {
		t.Fatalf("error = %v, want %v", err, history.ErrMetadataUnavailable)
	}
	var count int64
	if err := db.Model(&entity.DocumentVersion{}).Where("document_id = ?", "doc-1").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rows after the refused write = %d, want 0", count)
	}
}

// TestMetadataStateFailsWhenTheVersionCannotBeRecorded pins the policy the
// history writes now follow: a state whose version cannot be stored is an error,
// not a warning. The change that produced it fails with it, so a metadata edit
// never succeeds while the timeline silently misses the version it made.
func TestMetadataStateFailsWhenTheVersionCannotBeRecorded(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)

	// No knowledgebase row: the version has no dataset to attach to, which is
	// the failure a broken history write surfaces as.
	err := recordDocumentMetadataState(t.Context(), db, "doc-1", "kb-missing",
		map[string]interface{}{"author": "alice"})
	if err == nil {
		t.Fatal("a metadata state with no version to anchor it was recorded")
	}

	var count int64
	if countErr := db.Model(&entity.DocumentVersion{}).Where("document_id = ?", "doc-1").Count(&count).Error; countErr != nil {
		t.Fatal(countErr)
	}
	if count != 0 {
		t.Fatalf("rows after the refused write = %d, want 0", count)
	}
}

// TestParkDocumentMetadataStateRecordsTheEmptyCreationState pins the wiring: a
// created document's first state is the empty map, recorded under the version the
// flow opens for it. Without it a restore to the version that added the document
// would find nothing to put back.
func TestParkDocumentMetadataStateRecordsTheEmptyCreationState(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	insertTestKB(t, "kb-1", "tenant-1", 1, 0, 0)

	ctx := datasetversion.WithStateSink(t.Context())
	parkDocumentMetadataState(ctx, []*entity.Document{{ID: "doc-1", KbID: "kb-1"}})
	if err := datasetversion.FlushStates(ctx, db, "kb-1", 1); err != nil {
		t.Fatalf("flush the creation state: %v", err)
	}

	rows := documentMetadataStatesOf(t, db, "doc-1")
	if len(rows) != 1 {
		t.Fatalf("states = %+v, want the creation state", rows)
	}
	if rows[0].DatasetVersion != 1 || len(rows[0].MetaFields) != 0 {
		t.Fatalf("creation state = %+v, want an empty map under version 1", rows[0])
	}
}

// TestRestoreDocumentMetadataClearsValuesAddedAfterTheVersion pins the scenario
// the creation state exists for: a document added at version 1 and given metadata
// at version 3, restored to version 1, goes back to the empty map it was created
// with instead of keeping the newer values.
func TestRestoreDocumentMetadataClearsValuesAddedAfterTheVersion(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	insertTestKB(t, "kb-1", "tenant-1", 1, 0, 0)
	insertNamedTestDoc(t, "doc-1", "kb-1", "report.txt", 0, 0)

	// What the insert records: the document's values at creation, which are none.
	if err := dao.NewDocumentVersionDAO().CreateBatch(t.Context(), db, []*entity.DocumentVersion{{
		DocumentID: "doc-1", DatasetVersion: 1, DatasetID: "kb-1",
		Kind:       entity.DatasetVersionKindDocumentMetadata,
		MetaFields: entity.JSONMap{},
	}}); err != nil {
		t.Fatal(err)
	}
	seedMetadataState(t, db, "doc-1", 3, 300, "carol")

	writer := &recordingMetadataWriter{}
	if _, err := restoreDocumentMetadataAt(t.Context(), db, "kb-1", 1, nil, false, writer); err != nil {
		t.Fatalf("restore version 1: %v", err)
	}
	meta, ok := writer.written["doc-1"]
	if !ok {
		t.Fatalf("restore wrote nothing for doc-1, want the empty creation state: %v", writer.written)
	}
	if len(meta) != 0 {
		t.Fatalf("restore wrote %v, want an empty map", meta)
	}
}

// TestRestoreDocumentMetadataClearsWhenTheCreationStateWasNotRecorded pins the
// histories written before creation states existed: the document's settings are
// recorded at the version, so the history knew it then, and the values it held -
// never recorded that early - were none.
func TestRestoreDocumentMetadataClearsWhenTheCreationStateWasNotRecorded(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	insertTestKB(t, "kb-1", "tenant-1", 1, 0, 0)
	insertNamedTestDoc(t, "doc-1", "kb-1", "report.txt", 0, 0)

	if err := dao.NewDocumentVersionDAO().CreateBatch(t.Context(), db, []*entity.DocumentVersion{{
		DocumentID: "doc-1", DatasetVersion: 1, DatasetID: "kb-1",
		Kind: entity.DatasetVersionKindDocumentConfig, ParserID: "naive",
	}}); err != nil {
		t.Fatal(err)
	}
	seedMetadataState(t, db, "doc-1", 3, 300, "carol")

	writer := &recordingMetadataWriter{}
	if _, err := restoreDocumentMetadataAt(t.Context(), db, "kb-1", 1, nil, false, writer); err != nil {
		t.Fatalf("restore version 1: %v", err)
	}
	if meta, ok := writer.written["doc-1"]; !ok || len(meta) != 0 {
		t.Fatalf("restore wrote %v, want an empty map for doc-1", writer.written)
	}
}
