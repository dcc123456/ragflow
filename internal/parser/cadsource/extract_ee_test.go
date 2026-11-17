//go:build ee

package cadsource

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

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

func TestExtractMinimalDXF(t *testing.T) {
	doc, err := Extract(context.Background(), "drawing.dxf", []byte(minimalDXF))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if doc.Source.Format != "dxf" {
		t.Errorf("Source.Format = %q, want dxf", doc.Source.Format)
	}
	if doc.Source.Version == "" {
		t.Error("Source.Version is empty")
	}
	if doc.Diagnostics.SheetDetection != "fallback" {
		t.Errorf("SheetDetection = %q, want fallback", doc.Diagnostics.SheetDetection)
	}
	if len(doc.Sheets) != 1 || doc.Sheets[0].ID != "s0" {
		t.Fatalf("Sheets = %+v, want one fallback sheet s0", doc.Sheets)
	}
	if len(doc.Texts) != 1 {
		t.Fatalf("Texts = %+v, want one text", doc.Texts)
	}
	text := doc.Texts[0]
	if text.Text != "hello dxf" || text.X != 1 || text.Y != 2 {
		t.Errorf("text = %+v, want 'hello dxf' at (1,2)", text)
	}
	if text.Layer != "Wall" {
		t.Errorf("Layer = %q, want Wall", text.Layer)
	}
	if text.SheetID != "s0" {
		t.Errorf("SheetID = %q, want s0", text.SheetID)
	}
	if text.Origin != OriginSheetTexts || text.Confidence != 1 {
		t.Errorf("origin/confidence = %q/%v, want %q/1", text.Origin, text.Confidence, OriginSheetTexts)
	}
}

func TestDrawingJSONRoundTrip(t *testing.T) {
	source, err := Extract(context.Background(), "drawing.dxf", []byte(minimalDXF))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	raw, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("marshal drawing: %v", err)
	}
	var decoded Drawing
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal drawing: %v", err)
	}
	if decoded.Source.Format != source.Source.Format {
		t.Errorf("source = %+v, want %+v", decoded.Source, source.Source)
	}
	if len(decoded.Texts) != len(source.Texts) {
		t.Fatalf("texts = %d, want %d", len(decoded.Texts), len(source.Texts))
	}
	if decoded.Texts[0].Text != "hello dxf" || decoded.Texts[0].SheetID != "s0" {
		t.Errorf("text = %+v", decoded.Texts[0])
	}
	if decoded.Diagnostics.SheetDetection != "fallback" {
		t.Errorf("sheet detection = %q, want fallback", decoded.Diagnostics.SheetDetection)
	}
}

func TestExtractSeparatesModelAndPaperText(t *testing.T) {
	// This fixture is generated here; no downloaded drawing is redistributed.
	paper := "0\nTEXT\n5\n90\n330\n42\n67\n1\n8\nWall\n10\n1.0\n20\n2.0\n40\n3.0\n1\npaper note\n"
	dxf := strings.Replace(minimalDXF, "0\nENDSEC\n0\nEOF\n", paper+"0\nENDSEC\n0\nEOF\n", 1)
	doc, err := Extract(t.Context(), "layouts.dxf", []byte(dxf))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Texts) != 2 {
		t.Fatalf("model/paper text missing or duplicated: %+v", doc.Texts)
	}
	seen := map[string]string{}
	for _, text := range doc.Texts {
		seen[text.Text] = text.SheetID
		if text.Handle == 0 || text.Bounds[2] <= text.Bounds[0] || text.Bounds[3] <= text.Bounds[1] {
			t.Fatalf("source identity or native bounds missing: %+v", text)
		}
	}
	if seen["hello dxf"] != "s0" || seen["paper note"] != "p42" {
		t.Fatalf("overlapping coordinate spaces mixed: %v", seen)
	}
	for _, sheet := range doc.Sheets {
		if sheet.ID == "p42" && sheet.Layout == "paper:42" {
			return
		}
	}
	t.Fatal("paper layout metadata missing")
}

func TestExtractWithoutEntityDump(t *testing.T) {
	doc, err := extract(context.Background(), "drawing.dxf", []byte(minimalDXF), false)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(doc.Texts) == 0 {
		t.Fatal("Texts is empty without entity dump")
	}
	if len(doc.Tables) != 0 || len(doc.Dimensions) != 0 {
		t.Errorf("dump-derived data present with IncludeEntityDump=false: tables=%d dimensions=%d", len(doc.Tables), len(doc.Dimensions))
	}
}

func TestParseDumpRows(t *testing.T) {
	rows, err := parseDumpRows(`[
		{"handle": 1, "type": "MULTILEADER", "dxf": {"layer_handle": 10}},
		{"handle": 2, "type": "DIMENSION", "dxf": {"layer_handle": 11, "user_text": "x"}},
		{"handle": 3, "type": "UNKNOWN", "dxf": {"dxfname": "ACAD_TABLE"}}
	]`)
	if err != nil {
		t.Fatalf("parseDumpRows: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	if rows[0].Handle != 1 || rows[0].Type != "MULTILEADER" {
		t.Errorf("row[0] = %+v", rows[0])
	}
	if !isTableRow(rows[2]) {
		t.Error("row[2] must be recognized as ACAD_TABLE")
	}
}

func TestApplyDumpRows(t *testing.T) {
	doc := &Drawing{
		Sheets:      []Sheet{{ID: "s0", Index: 0, Name: "整图"}},
		Diagnostics: Diagnostics{Counts: map[string]int{}, SheetDetection: "fallback"},
	}
	rows := []dumpRow{
		{
			Handle: 1,
			Type:   "MULTILEADER",
			DXF: map[string]any{
				"layer_handle":                 10,
				"ctx.content.txt.default_text": "4×M10",
				"ctx.content.txt.location":     map[string]any{"X": 1.0, "Y": 2.0, "Z": 0.0},
				"ctx.content.txt.rotation":     0.0,
				"ctx.content.txt.height":       3.0,
			},
		},
		{
			Handle: 2,
			Type:   "DIMENSION",
			DXF: map[string]any{
				"layer_handle":    11,
				"user_text":       "φ25±0.02",
				"act_measurement": 25,
				"text_midpt":      map[string]any{"X": 3.0, "Y": 4.0, "Z": 0.0},
			},
		},
		{Handle: 3, Type: "UNKNOWN", DXF: map[string]any{"dxfname": "ACAD_TABLE"}},
		{Handle: 4, Type: "IMAGE", DXF: map[string]any{}},
		{Handle: 5, Type: "PROXY_ENTITY", DXF: map[string]any{}},
	}

	applyDumpRows(doc, nil, rows)

	if doc.Diagnostics.Counts["MULTILEADER"] != 1 {
		t.Errorf("MULTILEADER count = %d, want 1", doc.Diagnostics.Counts["MULTILEADER"])
	}
	if doc.Diagnostics.Counts["DIMENSION"] != 1 {
		t.Errorf("DIMENSION count = %d, want 1", doc.Diagnostics.Counts["DIMENSION"])
	}
	if doc.Diagnostics.Counts["ACAD_TABLE"] != 1 {
		t.Errorf("ACAD_TABLE count = %d, want 1", doc.Diagnostics.Counts["ACAD_TABLE"])
	}
	if doc.Diagnostics.Counts["IMAGE"] != 1 || doc.Diagnostics.Counts["PROXY_ENTITY"] != 1 {
		t.Errorf("image/proxy counts = %d/%d, want 1/1", doc.Diagnostics.Counts["IMAGE"], doc.Diagnostics.Counts["PROXY_ENTITY"])
	}
	if len(doc.Tables) != 1 || doc.Tables[0].Status != "unsupported" {
		t.Errorf("Tables = %+v, want one unsupported table", doc.Tables)
	}
	if len(doc.Dimensions) != 1 {
		t.Fatalf("Dimensions = %d, want 1", len(doc.Dimensions))
	}
	if doc.Dimensions[0].Measurement == nil || *doc.Dimensions[0].Measurement != 25 {
		t.Errorf("measurement = %v, want 25", doc.Dimensions[0].Measurement)
	}

	var mleader *Text
	for i := range doc.Texts {
		if doc.Texts[i].Text == "4×M10" {
			mleader = &doc.Texts[i]
			break
		}
	}
	if mleader == nil {
		t.Fatalf("MLEADER text not extracted: %+v", doc.Texts)
	}
	if mleader.Kind != KindMLeader || mleader.Origin != OriginDump {
		t.Errorf("MLEADER text kind/origin = %q/%q", mleader.Kind, mleader.Origin)
	}

	for _, warning := range []string{"table_unsupported", "image_entities", "proxy_entities"} {
		found := false
		for _, got := range doc.Diagnostics.Warnings {
			if got == warning {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing warning %q in %v", warning, doc.Diagnostics.Warnings)
		}
	}
}

func TestDimensionMatchesRenderedText(t *testing.T) {
	doc := &Drawing{
		Sheets: []Sheet{{ID: "s0", Index: 0, Name: "整图"}},
		Texts: []Text{{
			Text: "φ25±0.02", Kind: KindUnknown, Origin: OriginSheetTexts,
			SheetID: "s0", X: 3, Y: 4, Height: 3, Confidence: 1,
		}},
		Diagnostics: Diagnostics{Counts: map[string]int{}, SheetDetection: "fallback"},
	}
	rows := []dumpRow{{
		Handle: 7,
		Type:   "DIMENSION",
		DXF: map[string]any{
			"user_text":  "φ25±0.02",
			"text_midpt": map[string]any{"X": 3.0, "Y": 4.0, "Z": 0.0},
		},
	}}

	applyDumpRows(doc, nil, rows)

	if len(doc.Texts) != 1 {
		t.Fatalf("Texts = %d, want the existing text reused (no duplicate)", len(doc.Texts))
	}
	if doc.Texts[0].Kind != KindDimension {
		t.Errorf("matched text kind = %q, want %q", doc.Texts[0].Kind, KindDimension)
	}
	if doc.Texts[0].Handle != 7 {
		t.Errorf("matched text handle = %d, want 7", doc.Texts[0].Handle)
	}
	if doc.Dimensions[0].RenderedText != "φ25±0.02" {
		t.Errorf("RenderedText = %q", doc.Dimensions[0].RenderedText)
	}
}

func TestModelDimensionDoesNotMatchPaperText(t *testing.T) {
	for _, paperText := range []string{"COMPANY NAME", "100"} {
		for _, positioned := range []bool{true, false} {
			t.Run(paperText+map[bool]string{true: "/positioned", false: "/unpositioned"}[positioned], func(t *testing.T) {
				doc := &Drawing{
					Sheets: []Sheet{
						{ID: "s0", Layout: "model", Bounds: [4]float64{0, 0, 1000, 1000}},
						{ID: "p42", Layout: "paper:42", Bounds: [4]float64{0, 0, 200, 200}},
					},
					Texts:       []Text{{Text: paperText, Kind: KindUnknown, Origin: OriginSheetTexts, SheetID: "p42", X: 10, Y: 10, Height: 5, Handle: 7}},
					Diagnostics: Diagnostics{SheetDetection: "fallback", Counts: map[string]int{}},
				}
				fields := map[string]any{"user_text": "100"}
				if positioned {
					fields["text_midpt"] = map[string]any{"X": 10.0, "Y": 10.0, "Z": 0.0}
				}
				applyDimensionRow(doc, dumpRow{Handle: 99, Type: "DIMENSION", DXF: fields}, "")
				if len(doc.Texts) != 2 || doc.Texts[0].Kind != KindUnknown || doc.Texts[0].SheetID != "p42" {
					t.Fatalf("model dimension consumed paper text: %+v", doc.Texts)
				}
				if doc.Texts[1].Text != "100" || doc.Texts[1].SheetID != "s0" || doc.Dimensions[0].RenderedText != "100" {
					t.Fatalf("model dimension not retained: texts=%+v dimension=%+v", doc.Texts, doc.Dimensions[0])
				}
			})
		}
	}
}

func TestFinalizeZeroTextWarns(t *testing.T) {
	doc := &Drawing{
		Source:      Source{EntityCount: 100},
		Diagnostics: Diagnostics{Counts: map[string]int{}},
	}
	doc.finalize()

	joined := strings.Join(doc.Diagnostics.Warnings, ",")
	for _, want := range []string{"zero_text", "low_coverage", "paper_space_suspected"} {
		if !strings.Contains(joined, want) {
			t.Errorf("warnings = %v, want %q", doc.Diagnostics.Warnings, want)
		}
	}
}

func TestPointFromAny(t *testing.T) {
	fromMap := pointFromAny(map[string]any{"X": 1.0, "Y": 2.0, "Z": 3.0})
	if fromMap == nil || fromMap.X != 1 || fromMap.Y != 2 || fromMap.Z != 3 {
		t.Errorf("map point = %+v", fromMap)
	}
	fromSlice := pointFromAny([]any{4.0, 5.0})
	if fromSlice == nil || fromSlice.X != 4 || fromSlice.Y != 5 || fromSlice.Z != 0 {
		t.Errorf("slice point = %+v", fromSlice)
	}
	if pointFromAny(nil) != nil || pointFromAny("bad") != nil {
		t.Error("invalid point must be nil")
	}
}

func TestDimensionAmbiguousSameTextIsNotForced(t *testing.T) {
	doc := &Drawing{
		Sheets: []Sheet{{ID: "s0", Layout: "model", Bounds: [4]float64{0, 0, 100, 100}}},
		Texts: []Text{
			{Text: "100", Kind: KindUnknown, Origin: OriginSheetTexts, SheetID: "s0", X: 10, Y: 10, Height: 5},
			{Text: "100", Kind: KindUnknown, Origin: OriginSheetTexts, SheetID: "s0", X: 80, Y: 80, Height: 5},
		},
		Diagnostics: Diagnostics{SheetDetection: "detected", Counts: map[string]int{}},
	}
	applyDimensionRow(doc, dumpRow{Handle: 7, Type: "DIMENSION", DXF: map[string]any{"user_text": "100"}}, "")
	if len(doc.Texts) != 2 {
		t.Fatalf("ambiguous dimension created/removed text: %+v", doc.Texts)
	}
	if doc.Dimensions[0].RenderedText != "" {
		t.Fatalf("ambiguous dimension forced a match: %+v", doc.Dimensions[0])
	}
	if !hasCADWarning(doc.Diagnostics.Warnings, "dimension_ambiguous") {
		t.Fatalf("missing ambiguity diagnostic: %+v", doc.Diagnostics.Warnings)
	}
}

func TestDimensionAmbiguousAdjacentCandidates(t *testing.T) {
	doc := &Drawing{
		Sheets: []Sheet{{ID: "s0", Layout: "model", Bounds: [4]float64{0, 0, 100, 100}}},
		Texts: []Text{
			{Text: "ROOM", Kind: KindUnknown, Origin: OriginSheetTexts, SheetID: "s0", X: 0, Y: 0, Height: 10},
			{Text: "PIPE", Kind: KindUnknown, Origin: OriginSheetTexts, SheetID: "s0", X: 20, Y: 0, Height: 10},
		},
		Diagnostics: Diagnostics{SheetDetection: "detected", Counts: map[string]int{}},
	}
	applyDimensionRow(doc, dumpRow{Handle: 8, Type: "DIMENSION", DXF: map[string]any{
		"user_text":  "100",
		"text_midpt": map[string]any{"X": 10.0, "Y": 0.0},
	}}, "")
	if doc.Dimensions[0].RenderedText != "100" {
		t.Fatalf("ambiguity erased declared dimension text: %+v", doc.Dimensions[0])
	}
	if len(doc.Texts) != 3 || doc.Texts[0].Kind != KindUnknown || doc.Texts[1].Kind != KindUnknown {
		t.Fatalf("ambiguity altered unrelated candidates or erased declaration: %+v", doc.Texts)
	}
	if !hasCADWarning(doc.Diagnostics.Warnings, "dimension_ambiguous") {
		t.Fatalf("missing ambiguity diagnostic: %+v", doc.Diagnostics.Warnings)
	}
}

func hasCADWarning(warnings []string, prefix string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, prefix) {
			return true
		}
	}
	return false
}

func TestUnassignedSheetBoundsCoverMembers(t *testing.T) {
	doc := &Drawing{
		Sheets: []Sheet{{ID: "s1", Index: 1, Layout: "model", Bounds: [4]float64{0, 0, 100, 100}}},
		Texts: []Text{
			{Text: "outside one", SheetID: "s0", X: 800, Y: 800, Height: 5, Bounds: [4]float64{800, 800, 850, 805}},
			{Text: "outside two", SheetID: "s0", X: 900, Y: 900, Height: 5, Bounds: [4]float64{900, 900, 950, 905}},
		},
	}
	ensureUnassignedSheet(doc)
	for _, sheet := range doc.Sheets {
		if sheet.ID == "s0" {
			if sheet.Bounds != [4]float64{800, 800, 950, 905} || sheet.Layout != "model" || sheet.Source != "unassigned" {
				t.Fatalf("unassigned view bounds do not cover its members: %+v", sheet)
			}
			return
		}
	}
	t.Fatal("missing unassigned sheet")
}
