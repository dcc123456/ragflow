//go:build ee

package chunker

import (
	"bytes"
	"testing"
)

const testDXF = `0
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

func TestBuildPreviewFromDXF(t *testing.T) {
	doc, err := parseCADDocument("drawing.dxf", []byte(testDXF))
	if err != nil {
		t.Fatal(err)
	}
	result, err := buildCADPreview(t.Context(), doc, 512)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.PDF) == 0 || !bytes.HasPrefix(result.PDF, []byte("%PDF")) {
		t.Fatalf("preview is not a PDF: %q", result.PDF[:min(16, len(result.PDF))])
	}
	if len(result.Thumbnail) == 0 || !bytes.HasPrefix(result.Thumbnail, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("thumbnail is not a PNG")
	}
	if got := bytes.Count(result.PDF, []byte("/Type /Page")); got < 2 {
		t.Fatalf("PDF page objects = %d, want at least 2", got)
	}
}

func TestPreviewKeysAreDocumentScoped(t *testing.T) {
	if got := cadPreviewKey("doc1"); got != "cad-preview/doc1/preview-v1.pdf" {
		t.Fatalf("PreviewKey = %q", got)
	}
	if got := cadThumbnailKey("doc1"); got != "cad-preview/doc1/thumb.png" {
		t.Fatalf("ThumbnailKey = %q", got)
	}
	if cadPreviewKey("doc1") == cadPreviewKey("doc2") {
		t.Fatal("preview keys do not include the document id")
	}
}

func TestIsCADSourceFile(t *testing.T) {
	for _, name := range []string{"a.dwg", "b.DXF", "c.dxfb"} {
		if !isCADSourceFile(name) {
			t.Errorf("%s not recognized as CAD", name)
		}
	}
	if isCADSourceFile("a.pdf") {
		t.Fatal("pdf recognized as CAD")
	}
}

func TestPageGeometry(t *testing.T) {
	page := cadPreviewPage{paperSize: [2]float64{420, 297}, width: 2048, height: 1448}
	w, h := cadPreviewPointSize(page, nil)
	if w < 1189 || w > 1192 || h < 840 || h > 843 {
		t.Fatalf("A3 point size = %.2f x %.2f", w, h)
	}
	dw, dh := cadPreviewFit(100, 100, 200, 100)
	if dw != 100 || dh != 50 {
		t.Fatalf("fit = %.1f x %.1f", dw, dh)
	}
}
