//go:build ee

package chunker

import (
	"encoding/json"
	"strings"
	"testing"

	"ragflow/internal/ingestion/component/schema"
	"ragflow/internal/parser/cadsource"
)

func cadTableTestBox(text string, x, y, height float64) cadTextBox {
	return cadTextBoxFor(cadTextItem{text: text, x: x, y: y, height: height}, height)
}

func cadTableTestItem(text string, x, y, height float64, id string) cadTextBox {
	box := cadTableTestBox(text, x, y, height)
	box.item.sourceID = id
	box.item.sourceIDs = []string{id}
	box.item.facts = []cadSourceFact{{ID: id, Bounds: [4]float64{box.minX, box.minY, box.maxX, box.maxY}}}
	return box
}

func TestCADLegendRowStaysInChunkText(t *testing.T) {
	row := cadTableTestItem("PS147\tPRESSURE SWITCH (FIELD)", 10, 80, 2, "legend:1")
	row.item.legendRow = true
	header := cadTableTestBox("LEGEND", 10, 90, 2)
	box := cadTableTestBox("TAG NUMBER", 10, 85, 2)
	text := cadBuildLayout([]cadTextBox{header, box, row}, 2)
	if !strings.Contains(text, "TAG NUMBER: PS147 | DESCRIPTION: PRESSURE SWITCH (FIELD)") {
		t.Fatalf("legend row missing from block text: %q", text)
	}
}

func TestCADHeaderTableBindsColumnsAndRows(t *testing.T) {
	boxes := []cadTextBox{
		cadTableTestBox("DESIG", 10, 100, 2),
		cadTableTestBox("SIZE", 30, 100, 2),
		cadTableTestBox("REMARKS", 50, 100, 2),
		cadTableTestBox("FCV-2", 10, 90, 2),
		cadTableTestBox(`1 1/2"`, 30, 90, 2),
		cadTableTestBox("SEE 15684", 50, 90, 2),
		cadTableTestBox("FCV-3", 10, 80, 2),
		cadTableTestBox(`2"`, 30, 80, 2),
		cadTableTestBox("THREADED", 50, 80, 2),
	}
	columns, bottom, ok := cadFindTableHeader(boxes, 2)
	if !ok || len(columns) != 3 || bottom != 100 {
		t.Fatalf("header = %+v bottom=%v ok=%v", columns, bottom, ok)
	}
	rest, rows := cadExtractHeaderRows(boxes, 2)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2: %+v", len(rows), rows)
	}
	if got := cadTableRowText(rows[0]); got != `DESIG: FCV-2 | SIZE: 1 1/2" | REMARKS: SEE 15684` {
		t.Fatalf("row 0 = %q", got)
	}
	if got := cadTableRowText(rows[1]); got != `DESIG: FCV-3 | SIZE: 2" | REMARKS: THREADED` {
		t.Fatalf("row 1 = %q", got)
	}
	for _, box := range rest {
		if box.item.text == "FCV-2" || box.item.text == "SEE 15684" {
			t.Fatalf("row cell retained in block text: %+v", rest)
		}
	}
}

func TestCADAreaTablePairsEveryLabelWithItsValue(t *testing.T) {
	boxes := []cadTextBox{
		cadTableTestBox("SUPERFICI UTILI NETTE", 10, 60, 2),
		cadTableTestBox("27,72 m²", 10, 50, 2),
		cadTableTestBox("Soggiorno-pranzo", 10, 45, 2),
		cadTableTestBox("15,46 m²", 10, 40, 2),
		cadTableTestBox("Cucina", 10, 35, 2),
	}
	rest, rows := cadExtractAreaRows(boxes, 2)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2: %+v", len(rows), rows)
	}
	if got := cadTableRowText(rows[0]); got != "ITEM: Soggiorno-pranzo | AREA: 27,72 m²" {
		t.Fatalf("row 0 = %q", got)
	}
	if got := cadTableRowText(rows[1]); got != "ITEM: Cucina | AREA: 15,46 m²" {
		t.Fatalf("row 1 = %q", got)
	}
	for _, box := range rest {
		if box.item.text == "Cucina" || box.item.text == "27,72 m²" {
			t.Fatalf("area cell retained in block text: %+v", rest)
		}
	}
}

func TestCADChunkKeepsAllRows(t *testing.T) {
	sheet := cadsource.Sheet{ID: "s1", Layout: "model", Space: "model", Viewport: [4]float64{0, 0, 100, 100}}
	chunk := cadChunkFromBoxes(
		"drawing", sheet, "R1", cadKindRegion,
		[]cadEvidence{{Level: cadEvidenceDerived, Source: "text_layout", Method: "ink_gutter"}},
		[]cadTextBox{
			cadTableTestBox("VALVE SCHEDULE", 10, 200, 20),
			cadTableTestBox("DESIG", 10, 150, 20),
			cadTableTestBox("REMARKS", 50, 150, 20),
			cadTableTestBox("FCV-2", 10, 100, 20),
			cadTableTestBox("SEE 15684", 50, 100, 20),
			cadTableTestBox("FCV-3", 10, 50, 20),
			cadTableTestBox("THREADED", 50, 50, 20),
		},
		[4]float64{0, 0, 100, 100},
	)
	if !strings.Contains(chunk.Content, "FCV-2") || !strings.Contains(chunk.Content, "SEE 15684") {
		t.Fatalf("chunk lost table row text: %q", chunk.Content)
	}
	docs, err := cadChunkDocs([]cadChunk{chunk})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Mom != "" {
		t.Fatalf("unexpected mom record: %+v", docs)
	}
	var extra string
	if err := json.Unmarshal(docs[0].Extra["extra"], &extra); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		CAD struct {
			Row   bool `json:"row"`
			Cells any  `json:"cells"`
		} `json:"cad"`
	}
	if err := json.Unmarshal([]byte(extra), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.CAD.Row || payload.CAD.Cells != nil {
		t.Fatalf("obsolete row metadata emitted: %+v", payload.CAD)
	}
}

func TestCADTitleMetadataStaysDocumentScoped(t *testing.T) {
	chunks := []cadChunk{
		{
			Kind: cadKindTitleBlock, SheetID: "p0", RegionID: "T1",
			Content: "DRAWING NO: M0000506\nTITLE: REFRIGERATION DETAILS\nSCALE: 1:50\nSHEET: 3\nREV: A",
			Bounds:  [4]float64{0, 0, 10, 10},
		},
		{
			Kind: cadKindRegion, SheetID: "p1", RegionID: "R1",
			Content: "OTHER SHEET", Bounds: [4]float64{0, 0, 10, 10},
		},
		{
			Kind: cadKindAttributes, RegionID: "A1",
			Content: "TAG: 1", Bounds: [4]float64{0, 0, 10, 10},
		},
	}
	docs, err := cadChunkDocs(chunks)
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]string
	if err := json.Unmarshal(docs[0].Extra["metadata"], &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["drawing_no"] != "M0000506" || metadata["drawing_title"] != "REFRIGERATION DETAILS" {
		t.Fatalf("document metadata = %+v", metadata)
	}
	if _, ok := metadata["scale"]; ok {
		t.Fatalf("sheet-local scale leaked into document metadata: %+v", metadata)
	}
	sheetFields := func(doc schema.ChunkDoc) map[string]any {
		t.Helper()
		var encoded string
		if err := json.Unmarshal(doc.Extra["extra"], &encoded); err != nil {
			t.Fatal(err)
		}
		var payload struct {
			CAD map[string]any `json:"cad"`
		}
		if err := json.Unmarshal([]byte(encoded), &payload); err != nil {
			t.Fatal(err)
		}
		return payload.CAD
	}
	if cad := sheetFields(docs[0]); cad["scale"] != "1:50" || cad["sheet"] != "3" || cad["revision"] != "A" {
		t.Fatalf("sheet-local CAD metadata = %+v", cad)
	}
	if cad := sheetFields(docs[1]); cad["scale"] != nil || cad["sheet"] != nil || cad["revision"] != nil {
		t.Fatalf("title-block metadata leaked to another sheet: %+v", cad)
	}
	// Attribute chunks have no sheet id; a single title block still describes
	// the drawing they belong to.
	if cad := sheetFields(docs[2]); cad["scale"] != "1:50" {
		t.Fatalf("sheet-less attribute chunk lost drawing metadata: %+v", cad)
	}
}

func TestCADNativeSectionHeadingIsMarkedOnce(t *testing.T) {
	boxes := []cadTextBox{
		cadTableTestBox("CEILING DOOR DETAIL", 10, 100, 2),
		cadTableTestBox("DOOR WITH GASKET", 10, 90, 2),
	}
	text := cadBuildLayout(boxes, 2)
	if strings.Count(text, "CEILING DOOR DETAIL") != 1 || !strings.HasPrefix(text, "## ") {
		t.Fatalf("section heading = %q", text)
	}
}

func TestCADHeaderTableRetainsSmallHeightCells(t *testing.T) {
	// Cell at x=53 has box height 1 (< scale 5), with column gap 20 ->
	// columnTolerance = max(3, 2.4) = 3, so it matches column REM at x=50.
	boxes := []cadTextBox{
		cadTableTestBox("DESIG", 10, 100, 5),
		cadTableTestBox("SIZE", 30, 100, 5),
		cadTableTestBox("REMARKS", 50, 100, 5),
		cadTableTestBox("FCV-2", 10, 90, 5),
		cadTableTestBox("2\"", 30, 90, 5),
		cadTableTestBox("SMALL TEXT", 53, 90, 1),
		cadTableTestBox("FCV-3", 10, 80, 5),
		cadTableTestBox("3\"", 30, 80, 5),
		cadTableTestBox("OK", 50, 80, 5),
	}
	_, rows := cadExtractHeaderRows(boxes, 5)
	if len(rows) != 2 || !strings.Contains(cadTableRowText(rows[0]), "REMARKS: SMALL TEXT") {
		t.Fatalf("small-height cell dropped from table row: %+v", rows)
	}
}
