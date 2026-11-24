//go:build ee

package parser

import (
	"os"
	"strings"
	"testing"

	"ragflow/internal/utility"
)

func TestVisioParserDetectsMislabeledFiles(t *testing.T) {
	vsd, err := os.ReadFile("testdata/Visio11FormatLine.vsd")
	if err != nil {
		t.Fatal(err)
	}
	entries := testVSDXBaseEntries()
	entries["visio/pages/pages.xml"] = `<Pages xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><Page ID="7"><Rel r:id="page"/></Page></Pages>`
	entries["visio/pages/_rels/pages.xml.rels"] = `<Relationships><Relationship Id="page" Target="page1.xml"/></Relationships>`
	entries["visio/pages/page1.xml"] = `<PageContents><Shapes><Shape ID="1"><Text>Visio XML content</Text></Shape></Shapes></PageContents>`
	for _, tc := range []struct {
		filename string
		data     []byte
		format   string
		text     string
	}{
		{"drawing.vsdx", vsd, "vsd", "Very small"},
		{"drawing.vsd", buildTestVSDX(t, entries), "vsdx", "Visio XML content"},
	} {
		t.Run(tc.filename, func(t *testing.T) {
			p, family, err := GetParser(utility.GetFileType(tc.filename), tc.data)
			if err != nil {
				t.Fatal(err)
			}
			if family != utility.FileTypeVisio {
				t.Fatalf("family = %q, want visio", family)
			}
			result := p.ParseWithResult(t.Context(), tc.filename, tc.data)
			if result.Err != nil {
				t.Fatal(result.Err)
			}
			if result.File["format"] != tc.format || result.File["name"] != tc.filename || len(result.JSON) != 1 {
				t.Fatalf("unexpected result: file=%v, items=%d", result.File, len(result.JSON))
			}
			text, _ := result.JSON[0].ToMap()["text"].(string)
			if !strings.Contains(text, tc.text) {
				t.Errorf("expected %q in %q", tc.text, text)
			}
		})
	}
}

func TestVisioParserRejectsUnrelatedOfficeZip(t *testing.T) {
	data := buildTestVSDX(t, map[string]string{
		"[Content_Types].xml": "<Types/>",
		"word/document.xml":   "<document/>",
	})
	p, _, err := GetParser(utility.GetFileType("renamed.vsd"), data)
	if err != nil {
		t.Fatal(err)
	}
	result := p.ParseWithResult(t.Context(), "renamed.vsd", data)
	if result.Err == nil || result.Produced() {
		t.Fatalf("expected a non-Visio error, got %+v", result)
	}
}
