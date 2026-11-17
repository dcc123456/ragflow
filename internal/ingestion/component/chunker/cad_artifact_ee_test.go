//go:build ee

package chunker

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"ragflow/internal/entity"
	"ragflow/internal/ingestion/component/schema"
	"ragflow/internal/storage"
)

const cadImageTestDXF = `0
SECTION
2
HEADER
9
$ACADVER
1
AC1015
0
ENDSEC
0
SECTION
2
TABLES
0
TABLE
2
LAYER
5
2
0
LAYER
5
10
2
Wall
62
3
0
ENDTAB
0
ENDSEC
0
SECTION
2
ENTITIES
0
TEXT
5
8E
8
Wall
10
1.0
20
2.0
30
0.0
40
3.0
50
15.0
1
hello dxf
0
ENDSEC
0
EOF
`

func TestCADChunkImageMeta(t *testing.T) {
	encoded, _ := json.Marshal(`{"cad":{"layout_id":"model","space":"model","bbox":[0,0,20,10]}}`)
	ck := schema.ChunkDoc{Extra: map[string]json.RawMessage{"extra": encoded}}
	meta, ok := cadChunkImageMeta(ck)
	if !ok || meta.layoutID != "model" || meta.bbox != [4]float64{0, 0, 20, 10} {
		t.Fatalf("meta = %+v ok=%v", meta, ok)
	}
	bad, _ := json.Marshal(`{"cad":{"layout_id":"model","bbox":[0,0,0,0]}}`)
	if _, ok := cadChunkImageMeta(schema.ChunkDoc{Extra: map[string]json.RawMessage{"extra": bad}}); ok {
		t.Fatal("zero bbox accepted")
	}
}

func TestRenderCADChunkImage(t *testing.T) {
	doc, err := parseCADDocument("drawing.dxf", []byte(cadImageTestDXF))
	if err != nil {
		t.Fatal(err)
	}
	image, err := renderCADChunkImage(doc, cadChunkImageProvenance{
		layoutID: "model",
		bbox:     [4]float64{0, 0, 20, 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(image, cadChunkImageDataPrefix) || len(image) <= len(cadChunkImageDataPrefix) {
		t.Fatalf("image payload = %q", image)
	}
}

func TestGenerateCADPreviewArtifacts(t *testing.T) {
	factory := storage.GetStorageFactory()
	original := factory.GetStorage()
	store := storage.NewMemoryStorage()
	factory.SetStorage(store)
	t.Cleanup(func() { factory.SetStorage(original) })

	doc, err := parseCADDocument("drawing.dxf", []byte(cadImageTestDXF))
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string]any{"kb_id": "kb1", "doc_id": "doc1"}
	if err := generateCADPreviewArtifacts(t.Context(), nil, inputs, schema.ChunkerFromUpstream{}, doc); err != nil {
		t.Fatal(err)
	}

	pdf, err := store.Get(t.Context(), "kb1", cadPreviewKey("doc1"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Fatalf("preview is not a PDF: %q", pdf[:min(16, len(pdf))])
	}
	thumb, err := store.Get(t.Context(), "kb1", cadThumbnailKey("doc1"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(thumb, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatal("thumbnail is not a PNG")
	}
}

func TestGenerateCADPreviewArtifactsUpdatesThumbnail(t *testing.T) {
	factory := storage.GetStorageFactory()
	original := factory.GetStorage()
	factory.SetStorage(storage.NewMemoryStorage())
	t.Cleanup(func() { factory.SetStorage(original) })

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&entity.Document{}); err != nil {
		t.Fatal(err)
	}
	name := "drawing.dxf"
	row := &entity.Document{
		ID:           "doc1",
		KbID:         "kb1",
		UserID:       "user1",
		ParserID:     "cad",
		ParserConfig: entity.JSONMap{},
		SourceType:   "local",
		Type:         "cad",
		CreatedBy:    "user1",
		Name:         &name,
		Suffix:       "dxf",
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	doc, err := parseCADDocument(name, []byte(cadImageTestDXF))
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string]any{"kb_id": "kb1", "doc_id": "doc1"}
	if err := generateCADPreviewArtifacts(t.Context(), db, inputs, schema.ChunkerFromUpstream{}, doc); err != nil {
		t.Fatal(err)
	}

	var got entity.Document
	if err := db.First(&got, "id = ?", "doc1").Error; err != nil {
		t.Fatal(err)
	}
	want := cadThumbnailKey("doc1")
	if got.Thumbnail == nil || *got.Thumbnail != want {
		t.Fatalf("thumbnail = %v, want %q", got.Thumbnail, want)
	}
}
