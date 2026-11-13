//go:build ee

package parser

import (
	"context"
	"strings"
	"testing"

	"ragflow/internal/parser/cadsource"
	"ragflow/internal/utility"
)

// minimalDXF is a synthetic R2000 DXF (mirrors go-cad's own sample): one TEXT
// "hello dxf" on layer "Wall" at world (1.0, 2.0). It exercises the real
// go-cad extraction path end to end.
const minimalDXF = `0
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

// TestGetFileTypeCADResolvesRegisteredExtension verifies that init() registers
// the CAD extensions and GetFileType routes them to the CAD type. This is the
// gate that actually feeds parser.GetParser, so a wrong/missing registration
// silently drops the file to OTHER.
func TestGetFileTypeCADResolvesRegisteredExtension(t *testing.T) {
	registered := []string{"drawing.dwg", "DRAWING.DWG", "drawing.dxf", "drawing.dxfb"}
	for _, name := range registered {
		t.Run(name, func(t *testing.T) {
			if got := utility.GetFileType(name); got != utility.FileTypeCAD {
				t.Errorf("GetFileType(%q) = %q, want %q (extension must be registered)", name, got, utility.FileTypeCAD)
			}
		})
	}
}

// TestGetFileTypeCADDoesNotOverMatch guards against accidentally widening the
// match: an unrelated extension that merely contains a CAD substring must stay
// OTHER. This passes trivially before registration and must stay true after.
func TestGetFileTypeCADDoesNotOverMatch(t *testing.T) {
	for _, name := range []string{"drawing.dwgx", "drawing.cad", "notes.dxf.bak"} {
		if got := utility.GetFileType(name); got == utility.FileTypeCAD {
			t.Errorf("GetFileType(%q) = %q, must not over-match", name, got)
		}
	}
}

// TestGetParserCADReturnsProducer verifies the parser factory registration:
// GetParser for the CAD type must return a usable producer without error.
func TestGetParserCADReturnsProducer(t *testing.T) {
	p, ft, err := GetParser(utility.FileTypeCAD, []byte(minimalDXF))
	if err != nil {
		t.Fatalf("GetParser(%q): %v (factory must be registered)", utility.FileTypeCAD, err)
	}
	if ft != utility.FileTypeCAD {
		t.Errorf("effective type = %q, want %q", ft, utility.FileTypeCAD)
	}
	if p == nil {
		t.Fatal("GetParser returned nil producer")
	}
	var _ ParseResultProducer = p
}

// TestCADParserProducesText drives the registered producer through
// ParseWithResult and confirms the extracted drawing text reaches the parser
// payload as a text DocItem, while the structured document travels on File.
func TestCADParserProducesText(t *testing.T) {
	p, _, err := GetParser(utility.FileTypeCAD, []byte(minimalDXF))
	if err != nil {
		t.Fatalf("GetParser(%q): %v", utility.FileTypeCAD, err)
	}
	res := p.ParseWithResult(context.Background(), "drawing.dxf", []byte(minimalDXF))
	if res.Err != nil {
		t.Fatalf("ParseWithResult: %v", res.Err)
	}
	if !res.Produced() {
		t.Fatal("ParseWithResult produced no payload")
	}
	if _, ok := res.File["cad"].(*cadsource.Drawing); !ok {
		t.Fatalf("File[\"cad\"] = %T, want *cadsource.Drawing", res.File["cad"])
	}
	found := false
	for _, it := range res.JSON {
		if s, ok := it.ToMap()["text"].(string); ok && strings.Contains(s, "hello dxf") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a text item containing %q, got %d items: %+v", "hello dxf", len(res.JSON), res.JSON)
	}
}

// TestCADTextItemsCarrySpatialMetadata verifies the flat DocItem projection
// keeps the CAD context the downstream chunker needs.
func TestCADTextItemsCarrySpatialMetadata(t *testing.T) {
	doc, err := cadsource.Extract(context.Background(), "drawing.dxf", []byte(minimalDXF))
	if err != nil {
		t.Fatalf("cadsource.Extract: %v", err)
	}
	items := CADTextItems(doc)
	if len(items) == 0 {
		t.Fatal("CADTextItems returned no items")
	}
	var target map[string]any
	for _, item := range items {
		m := item.ToMap()
		if m["text"] == "hello dxf" {
			target = m
			break
		}
	}
	if target == nil {
		t.Fatalf("no projected item for 'hello dxf': %+v", items)
	}
	if target["cad_x"] != 1.0 || target["cad_y"] != 2.0 {
		t.Errorf("projected coordinates = (%v, %v), want (1, 2)", target["cad_x"], target["cad_y"])
	}
	if target["cad_layer"] != "Wall" {
		t.Errorf("projected layer = %v, want Wall", target["cad_layer"])
	}
	if target["cad_sheet_id"] != "s0" {
		t.Errorf("projected sheet = %v, want s0", target["cad_sheet_id"])
	}
}
