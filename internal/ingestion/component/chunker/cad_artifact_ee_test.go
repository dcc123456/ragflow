//go:build ee

package chunker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"ragflow/internal/entity"
	"ragflow/internal/ingestion/component/schema"
	"ragflow/internal/ingestion/task/indexdoc"
	"ragflow/internal/parser/cadsource"
	"ragflow/internal/storage"
)

type cadThumbnailFailStore struct{ storage.Storage }

func (s cadThumbnailFailStore) Put(ctx context.Context, bucket, key string, data []byte, tenantID ...string) error {
	if key == cadThumbnailKey("doc1") {
		return errors.New("thumbnail upload failed")
	}
	return s.Storage.Put(ctx, bucket, key, data, tenantID...)
}

func TestCADArtifactAttachesPreviewPositionsWhenThumbnailFails(t *testing.T) {
	factory := storage.GetStorageFactory()
	original := factory.GetStorage()
	store := storage.NewMemoryStorage()
	factory.SetStorage(cadThumbnailFailStore{Storage: store})
	t.Cleanup(func() { factory.SetStorage(original) })
	if err := store.Put(t.Context(), "kb1", "source.dxf", []byte(cadImageTestDXF)); err != nil {
		t.Fatal(err)
	}
	drawing, err := cadsource.Extract(t.Context(), "drawing.dxf", []byte(cadImageTestDXF))
	if err != nil {
		t.Fatal(err)
	}
	component := &cadChunkerComponent{}
	out, err := component.Invoke(t.Context(), nil, map[string]any{
		"name": "drawing.dxf", "bucket": "kb1", "path": "source.dxf", "kb_id": "kb1", "doc_id": "doc1",
		"output_format": "json", "json": []any{}, "file": map[string]any{"name": "drawing.dxf", "cad": drawing},
	})
	if err != nil {
		t.Fatal(err)
	}
	chunks, ok := schema.GetChunks(out)
	if !ok || len(chunks) != 1 {
		t.Fatalf("chunks=%+v", out)
	}
	var positions [][]float64
	if err := json.Unmarshal(chunks[0].Positions, &positions); err != nil {
		t.Fatalf("chunk has no preview highlight position: %v", err)
	}
	if len(positions) != 1 || len(positions[0]) != 5 || positions[0][0] != 1 || positions[0][2] <= positions[0][1] || positions[0][4] <= positions[0][3] {
		t.Fatalf("invalid highlight position: %v", positions)
	}
	wire := indexdoc.NormalizeChunks(out)
	if _, err := indexdoc.ProcessChunksForPipeline(wire, "doc1", "drawing.dxf", time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	stored, ok := wire[0]["position_int"].([][]int)
	if !ok || len(stored) != 1 || stored[0][0] != 1 || stored[0][2] <= stored[0][1] || stored[0][4] <= stored[0][3] {
		t.Fatalf("highlight lost at index boundary: %+v", wire[0])
	}
	if _, err := store.Get(t.Context(), "kb1", cadPreviewKey("doc1")); err != nil {
		t.Fatal(err)
	}
}

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
	if err := generateCADPreviewArtifacts(t.Context(), nil, inputs, schema.ChunkerFromUpstream{}, doc, []cadsource.Sheet{{ID: "s0", Layout: "model", Source: "fallback"}}, nil); err != nil {
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
	if err := generateCADPreviewArtifacts(t.Context(), db, inputs, schema.ChunkerFromUpstream{}, doc, []cadsource.Sheet{{ID: "s0", Layout: "model", Source: "fallback"}}, nil); err != nil {
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
