//go:build ee

package document

import (
	"errors"
	"strings"
	"testing"

	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/storage"
)

func useMemoryStorage(t *testing.T) storage.Storage {
	t.Helper()
	factory := storage.GetStorageFactory()
	original := factory.GetStorage()
	store := storage.NewMemoryStorage()
	factory.SetStorage(store)
	t.Cleanup(func() { factory.SetStorage(original) })
	return store
}

func stringPtr(value string) *string { return &value }

func TestPreparePreviewPresentationEEServesCachedPDF(t *testing.T) {
	store := useMemoryStorage(t)
	doc := &entity.Document{ID: "doc1", KbID: "kb1", Name: stringPtr("drawing.dwg")}
	key := cadPreviewObjectKey(doc.ID)
	if err := store.Put(t.Context(), doc.KbID, key, []byte("%PDF-1.7 preview")); err != nil {
		t.Fatal(err)
	}

	data, contentType, handled, err := preparePreviewPresentationEE(t.Context(), doc, "drawing.dwg")
	if err != nil {
		t.Fatal(err)
	}
	if !handled || contentType != "application/pdf" || string(data) != "%PDF-1.7 preview" {
		t.Fatalf("handled=%v contentType=%q data=%q", handled, contentType, data)
	}
}

func TestPreparePreviewPresentationEENotReady(t *testing.T) {
	useMemoryStorage(t)
	doc := &entity.Document{ID: "doc1", KbID: "kb1", Name: stringPtr("drawing.dwg")}

	_, _, handled, err := preparePreviewPresentationEE(t.Context(), doc, "drawing.dwg")
	if !handled || !errors.Is(err, ErrPreviewNotReady) {
		t.Fatalf("handled=%v err=%v, want ErrPreviewNotReady", handled, err)
	}
}

func TestPreparePreviewPresentationEEIgnoresNonCAD(t *testing.T) {
	useMemoryStorage(t)
	doc := &entity.Document{ID: "doc1", KbID: "kb1", Name: stringPtr("drawing.pdf")}

	_, _, handled, err := preparePreviewPresentationEE(t.Context(), doc, "drawing.pdf")
	if handled || err != nil {
		t.Fatalf("handled=%v err=%v, want unhandled", handled, err)
	}
}

func TestCleanupPreviewArtifactsEERemovesDocScopedObjects(t *testing.T) {
	store := useMemoryStorage(t)
	doc := &entity.Document{
		ID:        "doc1",
		KbID:      "kb1",
		Name:      stringPtr("drawing.dwg"),
		Thumbnail: stringPtr(cadThumbnailObjectKey("doc1")),
	}
	for _, key := range []string{cadPreviewObjectKey(doc.ID), cadThumbnailObjectKey(doc.ID)} {
		if err := store.Put(t.Context(), doc.KbID, key, []byte("data")); err != nil {
			t.Fatal(err)
		}
	}

	cleanupPreviewArtifactsEE(t.Context(), doc)

	for _, key := range []string{cadPreviewObjectKey(doc.ID), cadThumbnailObjectKey(doc.ID)} {
		if _, err := store.Get(t.Context(), doc.KbID, key); !storage.IsNotFound(err) {
			t.Fatalf("object %s still present: %v", key, err)
		}
	}
}

func insertCADPreviewDocument(t *testing.T, id, kbID string) *entity.Document {
	t.Helper()
	doc := &entity.Document{
		ID:           id,
		KbID:         kbID,
		UserID:       "user-1",
		ParserID:     "cad",
		ParserConfig: entity.JSONMap{},
		SourceType:   "local",
		Type:         "cad",
		CreatedBy:    "user-1",
		Name:         stringPtr("drawing.dwg"),
		Suffix:       "dwg",
		Status:       stringPtr("1"),
		Thumbnail:    stringPtr(cadThumbnailObjectKey(id)),
	}
	if err := dao.DB.Create(doc).Error; err != nil {
		t.Fatalf("insert CAD preview document: %v", err)
	}
	return doc
}

// The document row and its file2document mappings are dropped in one
// transaction, so a mapping failure rolls the whole delete back: the document
// still exists and a retry can make progress. Nothing is cleaned up on that
// path - in particular the preview artifacts stay, because they still belong to
// a live document.
func TestDeleteDocumentFullRollsBackWhenFileMappingDeleteFails(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	insertTestKB(t, "kb-1", "tenant-1", 1, 0, 0)
	doc := insertCADPreviewDocument(t, "doc-1", "kb-1")
	insertTestFile2Document(t, "f2d-1", "file-1", doc.ID)

	store := useMemoryStorage(t)
	keys := []string{cadPreviewObjectKey(doc.ID), cadThumbnailObjectKey(doc.ID)}
	for _, key := range keys {
		if err := store.Put(t.Context(), doc.KbID, key, []byte("data")); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec(`CREATE TRIGGER fail_file2document_delete
		BEFORE DELETE ON file2document
		BEGIN
			SELECT RAISE(ABORT, 'file2document delete failed');
		END`).Error; err != nil {
		t.Fatalf("create file2document trigger: %v", err)
	}

	err := testDocumentService(t).deleteDocumentFull(t.Context(), doc.ID)
	if err == nil || !strings.Contains(err.Error(), "delete the file mappings") {
		t.Fatalf("deleteDocumentFull error = %v, want a file mapping failure", err)
	}
	var remaining entity.Document
	if findErr := db.First(&remaining, "id = ?", doc.ID).Error; findErr != nil {
		t.Fatalf("document row was not rolled back: %v", findErr)
	}
	for _, key := range keys {
		if _, getErr := store.Get(t.Context(), doc.KbID, key); getErr != nil {
			t.Fatalf("preview object %s was removed although the delete rolled back: %v", key, getErr)
		}
	}
}

func TestClearDocumentParseResultsCleansPreviewArtifacts(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	insertTestKB(t, "kb-1", "tenant-1", 1, 0, 0)
	doc := insertCADPreviewDocument(t, "doc-1", "kb-1")

	store := useMemoryStorage(t)
	for _, key := range []string{cadPreviewObjectKey(doc.ID), cadThumbnailObjectKey(doc.ID)} {
		if err := store.Put(t.Context(), doc.KbID, key, []byte("data")); err != nil {
			t.Fatal(err)
		}
	}

	if err := testDocumentService(t).clearDocumentParseResults(t.Context(), doc, "tenant-1", "superseded"); err != nil {
		t.Fatalf("clearDocumentParseResults: %v", err)
	}
	for _, key := range []string{cadPreviewObjectKey(doc.ID), cadThumbnailObjectKey(doc.ID)} {
		if _, err := store.Get(t.Context(), doc.KbID, key); !storage.IsNotFound(err) {
			t.Fatalf("preview object %s survived re-parse cleanup: %v", key, err)
		}
	}
	var got entity.Document
	if err := db.First(&got, "id = ?", doc.ID).Error; err != nil {
		t.Fatalf("reload document: %v", err)
	}
	if got.Thumbnail != nil {
		t.Fatalf("thumbnail = %v, want nil", *got.Thumbnail)
	}
}
