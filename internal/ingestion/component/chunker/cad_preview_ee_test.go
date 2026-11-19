//go:build ee

package chunker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"reflect"
	"strings"
	"testing"

	"ragflow/internal/parser/cadsource"
)

func TestCADPreviewKeepsModelAndPaperPages(t *testing.T) {
	for _, count := range []int{1, 10} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			var paper strings.Builder
			for i := 0; i < count; i++ {
				fmt.Fprintf(&paper, "0\nTEXT\n5\n%X\n330\n42\n67\n1\n8\nWall\n10\n%d\n20\n20\n40\n3\n1\npaper note\n", 0x90+i, 10+i*10)
			}
			dxf := strings.Replace(testDXF, "0\nENDSEC\n0\nEOF\n", paper.String()+"0\nENDSEC\n0\nEOF\n", 1)
			doc, err := parseCADDocument("mixed.dxf", []byte(dxf))
			if err != nil {
				t.Fatal(err)
			}
			drawing, err := cadsource.Extract(t.Context(), "mixed.dxf", []byte(dxf))
			if err != nil {
				t.Fatal(err)
			}
			preview, err := renderCADPreviewPages(t.Context(), doc, drawing.Sheets, 256)
			if err != nil {
				t.Fatal(err)
			}
			pages := preview.Pages
			if len(pages) != 2 || pages[0].layoutID != "paper:42" || pages[1].layoutID != "model" || pages[1].sheetID != "s0" {
				t.Fatalf("want paper:42 then model/s0, got %+v", pages)
			}
		})
	}
}

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
	drawing, err := cadsource.Extract(t.Context(), "drawing.dxf", []byte(testDXF))
	if err != nil {
		t.Fatal(err)
	}
	result, err := buildCADPreview(t.Context(), doc, drawing.Sheets, 512)
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
	w, h := cadPreviewPointSize(page)
	if w < 1189 || w > 1192 || h < 840 || h > 843 {
		t.Fatalf("A3 point size = %.2f x %.2f", w, h)
	}
	dw, dh := cadPreviewFit(100, 100, 200, 100)
	if dw != 100 || dh != 50 {
		t.Fatalf("fit = %.1f x %.1f", dw, dh)
	}
}

func TestCADPreviewPositionUsesViewportAndPaperPlacement(t *testing.T) {
	page := cadPreviewPage{width: 200, height: 100, paperSize: [2]float64{25.4, 25.4}, viewport: [4]float64{-100, -50, 100, 50}}
	for _, tc := range []struct {
		name string
		box  [4]float64
		want []float64
	}{
		{"negative_coordinates_and_y_flip", [4]float64{-50, 0, 50, 25}, []float64{3, 18, 54, 27, 36}},
		{"clipped_to_visible_region", [4]float64{90, -10, 120, 10}, []float64{3, 68, 72, 32, 40}},
		{"outside_viewport", [4]float64{200, 0, 300, 25}, nil},
		{"non_finite", [4]float64{math.NaN(), 0, 20, 20}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := cadPreviewPosition(page, 3, tc.box)
			if ok != (tc.want != nil) || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("position=%v valid=%v want=%v", got, ok, tc.want)
			}
		})
	}
}

func TestCADHighlightResolvesNativeSpaceAndActualPageOrder(t *testing.T) {
	chunks := cadTestChunkDocs(t, []cadChunk{{
		Content: "PUMP", Layout: "model", SheetID: "s1", Bounds: [4]float64{-50, 0, 50, 25}, SourceIDs: []string{"source:1"},
		ContextSources: []cadSourceFact{{ID: "legend:1", Bounds: [4]float64{1000, 0, 1200, 100}}},
	}, {Content: "PAPER PUMP", Layout: "paper:42", SheetID: "p42", Bounds: [4]float64{-50, 0, 50, 25}}})
	original := string(chunks[0].Extra["extra"])
	page := cadPreviewPage{width: 200, height: 100, paperSize: [2]float64{25.4, 25.4}, viewport: [4]float64{-100, -50, 100, 50}}
	paper, other, target, otherPaper := page, page, page, page
	otherPaper.layoutID = "paper:43"
	paper.layoutID = "paper:42"
	other.layoutID = "model"
	other.sheetID = "s2"
	target.layoutID = "model"
	target.sheetID = "s1"
	if diagnostics := attachCADPreviewPositions(chunks, &cadPreviewResult{Pages: []cadPreviewPage{paper, other, target, otherPaper}}); len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	var positions [][]float64
	if err := json.Unmarshal(chunks[0].Positions, &positions); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(positions, [][]float64{{3, 18, 54, 27, 36}}) {
		t.Fatalf("wrong page or context enlarged the highlight: %v", positions)
	}
	if err := json.Unmarshal(chunks[1].Positions, &positions); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(positions, [][]float64{{1, 18, 54, 27, 36}}) {
		t.Fatalf("paper chunk highlighted a different layout: %v", positions)
	}
	if string(chunks[0].Extra["extra"]) != original {
		t.Fatal("highlight changed existing CAD provenance")
	}
}

func TestCADUnmappedChunkDoesNotRetainStalePosition(t *testing.T) {
	chunks := cadTestChunkDocs(t, []cadChunk{{Content: "PUMP", Layout: "model", SheetID: "s2", Bounds: [4]float64{0, 0, 10, 10}}})
	chunks[0].Positions = json.RawMessage(`[[1,0,10,0,10]]`)
	diagnostics := attachCADPreviewPositions(chunks, &cadPreviewResult{Pages: []cadPreviewPage{{layoutID: "model", sheetID: "s1", width: 100, height: 100, viewport: [4]float64{0, 0, 100, 100}}}})
	if len(chunks[0].Positions) != 0 || !cadHasDiagnostic(diagnostics, "cad_preview_unmapped") {
		t.Fatalf("chunk pointed to the wrong sheet: %s diagnostics=%v", chunks[0].Positions, diagnostics)
	}
}

func TestCADPreviewPositionUsesRendererScaleAfterRasterRounding(t *testing.T) {
	page := cadPreviewPage{width: 100, height: 34, viewport: [4]float64{0, 0, 300, 100}}
	got, ok := cadPreviewPosition(page, 1, [4]float64{0, 0, 300, 100})
	if !ok || !reflect.DeepEqual(got, []float64{1, 0, 842, 0, 281}) {
		t.Fatalf("rounded raster height distorted the world scale: %v", got)
	}
}

func TestCADModelPreviewUsesParserViewportWithoutAddingMargin(t *testing.T) {
	dxf := strings.Replace(testDXF, "10\n1.0\n20", "10\n18.0\n20", 1)
	doc, err := parseCADDocument("drawing.dxf", []byte(dxf))
	if err != nil {
		t.Fatal(err)
	}
	// The parser already accounted for both margin and text overhanging the frame.
	sheet := cadsource.Sheet{
		ID: "s1", Layout: "model",
		Bounds: [4]float64{0, 0, 10, 10}, Viewport: [4]float64{-1, -1, 25, 11},
	}
	preview, err := renderCADPreviewPages(t.Context(), doc, []cadsource.Sheet{sheet}, 260)
	if err != nil {
		t.Fatal(err)
	}
	pages := preview.Pages
	if len(pages) != 1 {
		t.Fatalf("render model preview: pages=%d warnings=%v", len(pages), preview.Warnings)
	}
	if pages[0].viewport != sheet.Viewport || pages[0].width != 260 || pages[0].height != 120 {
		t.Fatalf("rendered viewport=%v image=%dx%d, want %v at 260x120", pages[0].viewport, pages[0].width, pages[0].height, sheet.Viewport)
	}
	img, err := png.Decode(bytes.NewReader(pages[0].data))
	if err != nil {
		t.Fatal(err)
	}
	// The drawing's only text starts at X=18, beyond the ownership box.
	ink := false
	for y := 0; y < pages[0].height && !ink; y++ {
		for x := 190; x < pages[0].width; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r < 0xffff || g < 0xffff || b < 0xffff {
				ink = true
				break
			}
		}
	}
	if !ink {
		t.Fatal("text outside the ownership box was clipped")
	}
	chunks := cadTestChunkDocs(t, []cadChunk{{Content: "overhang", Layout: "model", SheetID: "s1", Bounds: [4]float64{18, 2, 22, 4}}})
	if diagnostics := attachCADPreviewPositions(chunks, preview); len(diagnostics) != 0 || len(chunks[0].Positions) == 0 {
		t.Fatalf("lost highlight for text outside the ownership box: %v", diagnostics)
	}
}

func TestCADPreviewExplainsUnmappedChunks(t *testing.T) {
	doc, err := parseCADDocument("drawing.dxf", []byte(testDXF))
	if err != nil {
		t.Fatal(err)
	}
	sheets := []cadsource.Sheet{{ID: "p42", Layout: "paper:42"}, {ID: "s-broken", Layout: "model"}}
	for i := range cadPreviewMaxPages + 1 {
		sheets = append(sheets, cadsource.Sheet{
			ID: fmt.Sprintf("s%d", i), Layout: "model",
			Viewport: [4]float64{0, 0, 100, 100},
		})
	}
	preview, err := renderCADPreviewPages(t.Context(), doc, sheets, 16)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Pages) != cadPreviewMaxPages {
		t.Fatalf("page cap did not retain exactly %d successful pages: %d", cadPreviewMaxPages, len(preview.Pages))
	}
	chunks := cadTestChunkDocs(t, []cadChunk{
		{Content: "missing layout", Layout: "paper:42", SheetID: "p42", Bounds: [4]float64{1, 1, 10, 10}},
		{Content: "render failed", Layout: "model", SheetID: "s-broken", Bounds: [4]float64{1, 1, 10, 10}},
		{Content: "page capped", Layout: "model", SheetID: fmt.Sprintf("s%d", cadPreviewMaxPages), Bounds: [4]float64{1, 1, 10, 10}},
		{Content: "out of bounds", Layout: "model", SheetID: "s0", Bounds: [4]float64{110, 0, 120, 10}},
	})
	diagnostics := attachCADPreviewPositions(chunks, preview)
	reasons := []string{"parser layout not found", "render failed", "preview truncated", "outside the visible page"}
	if len(diagnostics) != len(reasons) {
		t.Fatalf("unexpected diagnostics: %+v", diagnostics)
	}
	for i, reason := range reasons {
		if diagnostics[i].Code != "cad_preview_unmapped" || !strings.Contains(diagnostics[i].Detail, reason) || len(chunks[i].Positions) != 0 {
			t.Errorf("want %q without a highlight, got %+v and %s", reason, diagnostics[i], chunks[i].Positions)
		}
	}
}

func TestCADPreviewDetectsThinInkAndRecordsBlankOmission(t *testing.T) {
	encodeWhite := func(drawPixel bool) []byte {
		img := image.NewRGBA(image.Rect(0, 0, 512, 512))
		for y := 0; y < 512; y++ {
			for x := 0; x < 512; x++ {
				img.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
			}
		}
		if drawPixel {
			img.SetRGBA(1, 1, color.RGBA{R: 0, G: 0, B: 0, A: 255})
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	thinPage := cadPreviewPage{layoutID: "model", sheetID: "s1", data: encodeWhite(true), width: 512, height: 512}
	blankPage := cadPreviewPage{layoutID: "model", sheetID: "s2", data: encodeWhite(false), width: 512, height: 512}
	if !cadPreviewPageHasInk(thinPage) {
		t.Fatal("1px ink at non-strided coordinate missed")
	}
	preview := &cadPreviewResult{
		Pages:   []cadPreviewPage{blankPage, thinPage},
		Omitted: make(map[cadPreviewPageKey]string),
	}
	visible := cadVisiblePreviewPages(preview)
	if len(visible) != 1 || visible[0].sheetID != "s1" {
		t.Fatalf("visible pages = %+v", visible)
	}
	if preview.Omitted[previewPageKey("model", "s2")] != "page rendered blank" {
		t.Fatalf("blank page omission not recorded: %+v", preview.Omitted)
	}
}
