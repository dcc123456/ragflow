//go:build ee

package parser

import (
	"context"
	"strings"
	"testing"

	"ragflow/internal/utility"
)

// eeFileTypeCAD mirrors the production FileTypeCAD constant
// (internal/utility/file_ee.go). It is declared locally so this test compiles
// and asserts the registration contract before the production constant and
// wiring exist. The value MUST equal "cad".
const eeFileTypeCAD = utility.FileType("cad")

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
			if got := utility.GetFileType(name); got != eeFileTypeCAD {
				t.Errorf("GetFileType(%q) = %q, want %q (extension must be registered)", name, got, eeFileTypeCAD)
			}
		})
	}
}

// TestGetFileTypeCADDoesNotOverMatch guards against accidentally widening the
// match: an unrelated extension that merely contains a CAD substring must stay
// OTHER. This passes trivially before registration and must stay true after.
func TestGetFileTypeCADDoesNotOverMatch(t *testing.T) {
	for _, name := range []string{"drawing.dwgx", "drawing.cad", "notes.dxf.bak"} {
		if got := utility.GetFileType(name); got == eeFileTypeCAD {
			t.Errorf("GetFileType(%q) = %q, must not over-match", name, got)
		}
	}
}

// TestGetParserCADReturnsProducer verifies the parser factory registration:
// GetParser for the CAD type must return a usable producer without error. RED:
// the type is unsupported until the factory is registered.
func TestGetParserCADReturnsProducer(t *testing.T) {
	p, ft, err := GetParser(eeFileTypeCAD, []byte(minimalDXF))
	if err != nil {
		t.Fatalf("GetParser(%q): %v (factory must be registered)", eeFileTypeCAD, err)
	}
	if ft != eeFileTypeCAD {
		t.Errorf("effective type = %q, want %q", ft, eeFileTypeCAD)
	}
	if p == nil {
		t.Fatal("GetParser returned nil producer")
	}
	var _ ParseResultProducer = p
}

// TestCADParserProducesText drives the registered producer through
// ParseWithResult and confirms the extracted drawing text reaches the parser
// payload as a text DocItem.
func TestCADParserProducesText(t *testing.T) {
	p, _, err := GetParser(eeFileTypeCAD, []byte(minimalDXF))
	if err != nil {
		t.Fatalf("GetParser(%q): %v", eeFileTypeCAD, err)
	}
	res := p.ParseWithResult(context.Background(), "drawing.dxf", []byte(minimalDXF))
	if res.Err != nil {
		t.Fatalf("ParseWithResult: %v", res.Err)
	}
	if !res.Produced() {
		t.Fatal("ParseWithResult produced no payload")
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

// TestCADGoCadExtraction drives the go-cad wrapper directly and confirms the
// drawing text, world coordinates and layer survive the wrapper. This guards
// the extractCADText contract that CADParser depends on.
func TestCADGoCadExtraction(t *testing.T) {
	texts, err := extractCADText([]byte(minimalDXF), true)
	if err != nil {
		t.Fatalf("extractCADText: %v", err)
	}
	if len(texts) == 0 {
		t.Fatal("expected at least one extracted text, got none")
	}
	var got *cadText
	for i := range texts {
		if texts[i].Text == "hello dxf" {
			got = &texts[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("TEXT 'hello dxf' not extracted; got %d texts: %v", len(texts), texts)
	}
	if got.X != 1.0 || got.Y != 2.0 {
		t.Errorf("coordinate mismatch: want (1.0, 2.0), got (%.4f, %.4f)", got.X, got.Y)
	}
	if got.Layer == 0 {
		t.Errorf("layer handle should be populated (non-zero), got %d", got.Layer)
	}
}
