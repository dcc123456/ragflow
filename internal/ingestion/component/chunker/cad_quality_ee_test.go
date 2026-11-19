//
//  Copyright (c) 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//
//  This file is part of the RAGFlow Enterprise Edition and is licensed,
//  not sold, under the RAGFlow Enterprise Commercial License Agreement.
//  Unauthorized copying, modification, distribution, or disclosure of this
//  file, via any medium, is strictly prohibited.
//
//  Portions of this file may incorporate code from RAGFlow Community Edition,
//  which is licensed under the Apache License, Version 2.0.
//
//  For commercial licensing inquiries: sales@infiniflow.ai
//

//go:build ee

package chunker

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"ragflow/internal/ingestion/component/schema"
	"ragflow/internal/parser/cadsource"
	"ragflow/internal/storage"
)

func TestCADDeviceLabelsAreNotTitleBlock(t *testing.T) {
	var texts []cadsource.Text
	for i, text := range []string{"LT", "LT", "002A", "002B", "Laser", "US", "002C", "002D"} {
		texts = append(texts, cadTestText(text, "s1", 800, float64(10+i*5), 5))
	}
	chunks, _ := buildCADChunks(cadTestDrawing([]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 1000, 1000})}, texts), nil, "drawing")
	for _, chunk := range chunks {
		if chunk.Kind == cadKindTitleBlock {
			t.Fatal("device labels claimed as title block without field evidence")
		}
	}
}

func TestCADNotesDoNotConsumeAdjacentDetail(t *testing.T) {
	var boxes []cadTextBox
	for _, text := range []cadTextItem{{text: "NOTES", x: 10, y: 100, height: 5}, {text: "1. Insulate pipe", x: 10, y: 90, height: 5}, {text: "2. Seal joints", x: 10, y: 80, height: 5}, {text: "SECTION A-A", x: 300, y: 85, height: 5}} {
		boxes = append(boxes, cadTextBoxFor(text, 5))
	}
	before, notes, ok := cadSplitNotesPart(boxes)
	if !ok {
		t.Fatal("notes not recognized")
	}
	if strings.Contains(strings.Join(cadItemTexts(cadBoxesToItems(notes)), "\n"), "SECTION A-A") {
		t.Fatal("adjacent detail absorbed into notes")
	}
	if len(before)+len(notes) != 4 {
		t.Fatal("notes segmentation lost content")
	}
}

func cadItemTexts(items []cadTextItem) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.text
	}
	return out
}

func TestCADChunkRetainsSpatialSources(t *testing.T) {
	chunk := cadTestChunkFromItems("drawing", cadTestSheet("s1", [4]float64{0, 0, 1000, 600}), "R1", cadKindRegion, []cadTextItem{{text: "TAG 1", x: 10, y: 30, height: 5, sourceID: "source:1", handle: 42}})
	docs := cadTestChunkDocs(t, []cadChunk{chunk})
	wire := docs[0].ToMap()
	var extra struct {
		CAD map[string]json.RawMessage `json:"cad"`
	}
	raw, ok := wire["extra"]
	if !ok {
		t.Fatal("missing persisted extra column")
	}
	serialized, ok := raw.(string)
	if !ok {
		t.Fatalf("extra must remain a JSON string for existing chunk stores, got %T", raw)
	}
	if err := json.Unmarshal([]byte(serialized), &extra); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"kind", "sheet_id", "bbox", "source_ids"} {
		if len(extra.CAD[key]) == 0 {
			t.Errorf("missing CAD provenance %q", key)
		}
	}
}

func TestCADUnownedPlaceholderAttributes(t *testing.T) {
	doc := cadTestDrawing(nil, nil, cadsource.Attribute{Tag: "ITEM", Value: "XXX"}, cadsource.Attribute{Tag: "TAG", Value: "FC 095"})
	chunks, _ := cadAttributeChunks(doc, "drawing")
	var bodies []string
	for _, chunk := range chunks {
		bodies = append(bodies, chunk.Content)
	}
	body := strings.Join(bodies, "\n")
	if strings.Contains(body, "XXX") || !strings.Contains(body, "FC 095") {
		t.Fatalf("placeholder filtering lost useful values or kept placeholders: %s", body)
	}
}

func TestCADLegendRowsStayTogether(t *testing.T) {
	texts := []cadsource.Text{cadTestText("TAG NUMBER", "s1", 10, 400, 5), cadTestText("DESCRIPTION", "s1", 100, 400, 5)}
	for i := 0; i < 8; i++ {
		y := float64(380 - i*20)
		texts = append(texts, cadTestText(fmt.Sprintf("FC %03d", 95+i), "s1", 10, y, 5), cadTestText("FLOW CONTROLLER (CP)", "s1", 100, y, 5))
	}
	chunks, _ := buildCADChunks(cadTestDrawing([]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 300, 500})}, texts), nil, "legend")
	for _, chunk := range chunks {
		if strings.Contains(chunk.Content, "FC 095") && !strings.Contains(chunk.Content, "FLOW CONTROLLER (CP)") {
			t.Fatalf("legend row separated: %s", chunk.Content)
		}
	}
}

func TestCADFrameGridNeedsOrderedSequence(t *testing.T) {
	var texts []cadsource.Text
	for i := 0; i < 8; i++ {
		texts = append(texts, cadTestText(string(rune('A'+i)), "s1", 0, float64(500-i*50), 5))
	}
	texts = append(texts, cadTestText("MOTOR A", "s1", 100, 400, 5), cadTestText("220", "s1", 100, 350, 5))
	chunks, diags := buildCADChunks(cadTestDrawing([]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 1000, 600})}, texts), nil, "drawing")
	all := strings.Join(cadTestTexts(chunks), "\n")
	if !strings.Contains(all, "MOTOR A") || !strings.Contains(all, "220") {
		t.Fatal("grid filter removed real annotation")
	}
	for _, line := range strings.Split(all, "\n") {
		if line == "A" || line == "H" {
			t.Fatalf("frame grid indexed as content: %s", all)
		}
	}
	if !cadHasDiagnostic(diags, "cad_sheet_grid_excluded") {
		t.Fatal("grid exclusion not accounted for")
	}
}

// zeroTextDXF is a synthetic R2000 DXF with one LINE and no text: it decodes
// cleanly but carries nothing searchable, like the NIST single-part solids.
const zeroTextDXF = `0
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
ENTITIES
0
LINE
5
8F
8
0
10
0.0
20
0.0
30
0.0
11
100.0
21
100.0
31
0.0
0
ENDSEC
0
EOF
`

func TestCADComponentWarnsOnNoSearchableContent(t *testing.T) {
	component := &cadChunkerComponent{}
	out, err := component.Invoke(t.Context(), nil, map[string]any{"name": "geometry.dxf", "output_format": "json", "json": []any{}, "file": map[string]any{"name": "geometry.dxf", "cad": cadTestDrawing(nil, nil)}})
	if err != nil {
		t.Fatalf("zero-text CAD ingestion failed: %v", err)
	}
	chunks, _ := schema.GetChunks(out)
	if len(chunks) != 0 {
		t.Fatalf("zero-text CAD ingestion emitted %d chunks, want 0", len(chunks))
	}
	warnings, _ := out["warnings"].([]string)
	if len(warnings) != 1 || !strings.Contains(strings.ToLower(warnings[0]), "no searchable text") {
		t.Fatalf("zero-text CAD ingestion reported no warning: %v", out["warnings"])
	}
}

// TestCADZeroTextKeepsPreviewArtifacts proves a geometry-only drawing stays
// retrievable: one summary chunk per sheet plus the stored preview PDF and
// thumbnail, instead of the old task failure.
func TestCADZeroTextKeepsPreviewArtifacts(t *testing.T) {
	factory := storage.GetStorageFactory()
	original := factory.GetStorage()
	store := storage.NewMemoryStorage()
	factory.SetStorage(store)
	t.Cleanup(func() { factory.SetStorage(original) })
	if err := store.Put(t.Context(), "kb1", "geometry.dxf", []byte(zeroTextDXF)); err != nil {
		t.Fatal(err)
	}
	drawing, err := cadsource.Extract(t.Context(), "geometry.dxf", []byte(zeroTextDXF))
	if err != nil {
		t.Fatal(err)
	}
	if len(drawing.Texts) != 0 {
		t.Fatalf("fixture has %d texts, want 0", len(drawing.Texts))
	}
	component := &cadChunkerComponent{}
	out, err := component.Invoke(t.Context(), nil, map[string]any{
		"name": "geometry.dxf", "bucket": "kb1", "path": "geometry.dxf", "kb_id": "kb1", "doc_id": "doc1",
		"output_format": "json", "json": []any{}, "file": map[string]any{"name": "geometry.dxf", "cad": drawing},
	})
	if err != nil {
		t.Fatalf("zero-text CAD ingestion failed: %v", err)
	}
	chunks, ok := schema.GetChunks(out)
	if !ok || len(chunks) != 1 {
		t.Fatalf("geometry-only CAD ingestion emitted %d chunks, want 1 summary: %+v", len(chunks), out)
	}
	if !strings.Contains(chunks[0].Text, "geometry summary") || !strings.Contains(chunks[0].Text, "entities: 1") {
		t.Fatalf("summary chunk misses geometry facts: %q", chunks[0].Text)
	}
	if _, hasWarnings := out["warnings"]; hasWarnings {
		t.Fatalf("summary chunk path must not set the zero-chunk warning: %v", out["warnings"])
	}
	for _, key := range []string{cadPreviewKey("doc1"), cadThumbnailKey("doc1")} {
		data, err := store.Get(t.Context(), "kb1", key)
		if err != nil || len(data) == 0 {
			t.Fatalf("preview artifact %q missing: data=%d err=%v", key, len(data), err)
		}
	}
}

func TestCADGeometrySummaryChunkReportsDecodedFacts(t *testing.T) {
	measurement := 80.0
	doc := cadTestDrawing(
		[]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 1000, 600})},
		nil,
	)
	doc.Source = cadsource.Source{Format: "dwg", Version: "AC1015", EntityCount: 1182}
	doc.Dimensions = []cadsource.Dimension{
		{Handle: 7, SheetID: "s1", Layer: "DIM", Measurement: &measurement},
		{Handle: 8, SheetID: "s1", Layer: "DIM", UserText: "TYP"},
	}
	doc.Tables = []cadsource.Table{{Handle: 9, Status: "unsupported"}}
	doc.Diagnostics.Counts = map[string]int{"PROXY_ENTITY": 5, "DIMENSION": 2, "ACAD_TABLE": 1}
	chunks, diags := buildCADChunks(doc, nil, "fans")
	if len(chunks) != 1 {
		t.Fatalf("geometry summary chunks = %d, want 1", len(chunks))
	}
	body := chunks[0].Content
	for _, want := range []string{
		"geometry summary (no text annotations in this sheet)",
		"entities: 1182 (dwg AC1015); skipped during decode: 0",
		"sheet bounds:",
		"dimension: 80 (layer DIM)",
		"dimension: TYP (layer DIM)",
		"tables: 1 (content not extracted)",
		"proxy entities: 5",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("summary misses %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "DIMENSION") || strings.Contains(body, "ACAD_TABLE") {
		t.Errorf("summary double-counts special entities:\n%s", body)
	}
	if !cadHasDiagnostic(diags, "cad_no_text") {
		t.Error("zero-text drawing lost its cad_no_text diagnostic")
	}
}

func TestCADTitleFieldExtractionDoesNotTakeLegendRows(t *testing.T) {
	items := []cadTextItem{{text: "FCV 095", x: 10, y: 114, height: 1.5}, {text: "FLOW CONTROL VALVE", x: 40, y: 114, height: 1.5}, {text: "FLOW INSTRUMENT FAMILY", x: 10, y: 108, height: 1.5}, {text: "NAMA", x: 10, y: 100, height: 1.5}, {text: "NIM", x: 10, y: 96, height: 1.5}, {text: "TANGGAL", x: 10, y: 90, height: 1.5}}
	rest, titles := cadExtractTitleFields(items, cadBounds{minX: 0, minY: 0, maxX: 100, maxY: 200})
	if len(rest) < 2 || len(titles) != 1 {
		t.Fatalf("title segmentation: rest=%v titles=%v", rest, titles)
	}
	for _, part := range titles {
		for _, item := range part {
			if strings.Contains(item.text, "FCV") || strings.Contains(item.text, "CONTROL VALVE") {
				t.Fatal("legend row swallowed by title rectangle")
			}
		}
	}
}

func TestCADChunkUsesNativeTextBounds(t *testing.T) {
	text := cadTestText("ROTATED", "s1", 10, 20, 5)
	text.Bounds = [4]float64{5, 20, 10, 60}
	doc := cadTestDrawing([]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 100, 100})}, []cadsource.Text{text})
	chunks, _ := buildCADChunks(doc, nil, "drawing")
	if len(chunks) != 1 {
		t.Fatalf("chunks = %+v, want one block", chunks)
	}
	bounds := chunks[0].Bounds
	if bounds[0] > text.Bounds[0] || bounds[1] > text.Bounds[1] || bounds[2] < text.Bounds[2] || bounds[3] < text.Bounds[3] {
		t.Fatalf("chunk bounds %v do not contain native text bounds %v", bounds, text.Bounds)
	}
}

func TestCADFrameGridPreservesInteriorAnnotations(t *testing.T) {
	for _, letters := range []bool{false, true} {
		t.Run(fmt.Sprintf("letters=%t", letters), func(t *testing.T) {
			texts := []cadsource.Text{cadTestText("INSTALL COMPONENTS IN ORDER", "s1", 300, 550, 5)}
			for i := 0; i < 6; i++ {
				text := fmt.Sprint(i + 1)
				x, y := 300+float64(i)*50, 500.0
				if letters {
					text, x, y = string(rune('A'+i)), 300, 500-float64(i)*50
				}
				texts = append(texts, cadTestText(text, "s1", x, y, 5))
			}
			chunks, diagnostics := buildCADChunks(cadTestDrawing([]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 1000, 1000})}, texts), nil, "assembly")
			if len(cadTestSourceIDs(chunks)) != 7 || cadHasDiagnostic(diagnostics, "cad_sheet_grid_excluded") {
				t.Fatalf("interior annotations removed: chunks=%+v diagnostics=%+v", chunks, diagnostics)
			}
		})
	}
}

func TestCADFrameGridPreservesFallbackBoundaryLabels(t *testing.T) {
	var texts []cadsource.Text
	for i := 0; i < 6; i++ {
		texts = append(texts, cadTestText(fmt.Sprint(i+1), "s0", float64(i)*50, 0, 5))
	}
	sheet := cadsource.Sheet{ID: "s0", Layout: "model", Bounds: [4]float64{0, 0, 250, 5}}
	chunks, diagnostics := buildCADChunks(cadTestDrawing([]cadsource.Sheet{sheet}, texts), nil, "numbered parts")
	if len(cadTestSourceIDs(chunks)) != 6 || cadHasDiagnostic(diagnostics, "cad_sheet_grid_excluded") {
		t.Fatalf("fallback text bounds treated as a detected frame: chunks=%+v diagnostics=%+v", chunks, diagnostics)
	}
}

func TestCADMaterialScheduleIsNotTitleBlock(t *testing.T) {
	for _, stacked := range []bool{false, true} {
		t.Run(fmt.Sprintf("stacked=%t", stacked), func(t *testing.T) {
			var texts []cadsource.Text
			for i, label := range []string{"MATERIAL", "WEIGHT", "DATE"} {
				x, y := 100+float64(i)*100, 800.0
				if stacked {
					x, y = 100, 800-float64(i)*20
				}
				texts = append(texts, cadTestText(label, "s1", x, y, 5))
			}
			texts = append(texts, cadTestText("S355 STEEL", "s1", 100, 790, 5))
			chunks, _ := buildCADChunks(cadTestDrawing([]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 1000, 1000})}, texts), nil, "schedule")
			for _, chunk := range chunks {
				if chunk.Kind == cadKindTitleBlock {
					t.Fatalf("material schedule classified as title block: %s", chunk.Content)
				}
			}
			if len(cadTestSourceIDs(chunks)) != 4 {
				t.Fatal("material schedule lost source text")
			}
		})
	}
}

func TestCADChunkRetainsTextFacts(t *testing.T) {
	text := cadTestText("ROTATED", "s1", 10, 20, 5)
	text.Kind = cadsource.KindDimension
	text.Origin = cadsource.OriginDump
	text.Layer = "DIM"
	text.Rotation = 30
	text.Bounds = [4]float64{5, 20, 15, 25}
	sheet := cadTestSheet("s1", [4]float64{0, 0, 100, 100})
	sheet.Layout = "model"
	sheet.Space = "model"
	sheet.Viewport = [4]float64{-2, -2, 102, 102}
	doc := cadTestDrawing([]cadsource.Sheet{sheet}, []cadsource.Text{text})
	chunks, _ := buildCADChunks(doc, nil, "drawing")
	if len(chunks) != 1 {
		t.Fatalf("chunks = %d", len(chunks))
	}
	wire := cadTestChunkDocs(t, chunks)[0].ToMap()
	extra, _ := wire["extra"].(string)
	var payload struct {
		CAD struct {
			LayoutID string          `json:"layout_id"`
			Space    string          `json:"space"`
			Viewport [4]float64      `json:"viewport"`
			Sources  []cadSourceFact `json:"sources"`
		} `json:"cad"`
	}
	if err := json.Unmarshal([]byte(extra), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.CAD.LayoutID != "model" || payload.CAD.Space != "model" {
		t.Fatalf("layout provenance = %q/%q", payload.CAD.LayoutID, payload.CAD.Space)
	}
	if payload.CAD.Viewport != sheet.Viewport {
		t.Fatalf("render viewport lost in persistence: %v", payload.CAD.Viewport)
	}
	if len(payload.CAD.Sources) != 1 {
		t.Fatalf("sources = %+v", payload.CAD.Sources)
	}
	fact := payload.CAD.Sources[0]
	if fact.Kind != string(cadsource.KindDimension) || fact.Origin != string(cadsource.OriginDump) || fact.Layer != "DIM" || fact.Rotation != 30 {
		t.Fatalf("text facts lost: %+v", fact)
	}
	if fact.Bounds != text.Bounds {
		t.Fatalf("source bounds = %v, want %v", fact.Bounds, text.Bounds)
	}
}

func TestCADTitleChunkKeepsLayoutSpace(t *testing.T) {
	sheet := cadsource.Sheet{
		ID:     "p42",
		Layout: "paper:42",
		Space:  "paper",
		Bounds: [4]float64{0, 0, 420, 297},
	}
	part := []cadTextItem{{text: "NAMA", x: 300, y: 20, height: 5, sourceID: "text:1"}}
	chunks := cadTitleChunks("drawing", sheet, [][]cadTextItem{part}, nil, 5)
	if len(chunks) != 1 || chunks[0].Layout != "paper:42" || chunks[0].Space != "paper" {
		t.Fatalf("title chunk metadata = %+v", chunks)
	}
}

func TestCADLegendTagFormats(t *testing.T) {
	items := []cadTextItem{
		{text: "TAG NUMBER", x: 10, y: 400, height: 5},
		{text: "DESCRIPTION", x: 100, y: 400, height: 5},
	}
	rows := []struct{ tag, description string }{
		{"095A", "RECIPROCATING COMPRESSOR"},
		{"147A", "MOTOR"},
		{"PS147", "PRESSURE SWITCH (FIELD)"},
		{"1", "GATE"},
		{"V-147A", "VALVE"},
	}
	for i, row := range rows {
		y := float64(380 - i*20)
		items = append(items,
			cadTextItem{text: row.tag, x: 10, y: y, height: 5},
			cadTextItem{text: row.description, x: 100, y: y, height: 5},
		)
	}
	out, _ := cadJoinLegendRows(items)
	joined := map[string]string{}
	for _, item := range out {
		if parts := strings.Split(item.text, "\t"); len(parts) == 2 {
			joined[parts[0]] = parts[1]
		}
	}
	for _, row := range rows {
		if joined[row.tag] != row.description {
			t.Errorf("tag %q joined = %q, want %q", row.tag, joined[row.tag], row.description)
		}
	}
}

func TestCADFrameGridAllowsMissingLabels(t *testing.T) {
	sheet := cadsource.Sheet{ID: "s1", Index: 1, Layout: "model", Space: "model", Source: "polyline", Bounds: [4]float64{0, 0, 100, 100}}
	items := []cadTextItem{
		{text: "A", x: 0, y: 95, height: 2},
		{text: "B", x: 0, y: 87, height: 2},
		{text: "C", x: 0, y: 79, height: 2},
		{text: "E", x: 0, y: 71, height: 2},
		{text: "F", x: 0, y: 63, height: 2},
		{text: "G", x: 0, y: 55, height: 2},
		{text: "MOTOR", x: 50, y: 75, height: 2},
	}
	out, removed := cadFilterFrameGrid(items, sheet)
	if removed != 6 {
		t.Fatalf("removed = %d, want 6", removed)
	}
	if len(out) != 1 || out[0].text != "MOTOR" {
		t.Fatalf("interior annotation removed: %+v", out)
	}
}

func TestCADLegendKeywordsIncludeAliases(t *testing.T) {
	chunks := []cadChunk{{Content: "FC 095\nPS147"}}
	cadAddLegendContext(chunks, map[string]cadLegendEntry{
		"FC 095": {Tag: "FC 095", Description: "FLOW CONTROLLER (CP)", Sources: []cadSourceFact{{ID: "text:1"}}},
		"PS147":  {Tag: "PS147", Description: "PRESSURE SWITCH (FIELD)", Sources: []cadSourceFact{{ID: "text:2"}}},
	})
	keywords := strings.Join(chunks[0].Keywords, ",")
	for _, want := range []string{"FC 095", "FC095", "PS147", "PS 147"} {
		if !strings.Contains(keywords, want) {
			t.Errorf("keywords %q missing %q", keywords, want)
		}
	}
	if len(chunks[0].ContextSources) != 2 {
		t.Fatalf("context sources = %+v", chunks[0].ContextSources)
	}
	wire := cadTestChunkDocs(t, chunks)[0].ToMap()
	if got, _ := wire["keywords"].(string); !strings.Contains(got, "FC095") {
		t.Fatalf("persisted keywords = %q", got)
	}
}

func TestCADLegendContextMatchesEquipmentAliases(t *testing.T) {
	for _, tc := range []struct{ body, tag, description string }{
		{"095A", "095A", "RECIPROCATING COMPRESSOR"},
		{"PS\n147", "PS147", "PRESSURE SWITCH (FIELD)"},
		{"PS147", "PS 147", "PRESSURE SWITCH (FIELD)"},
	} {
		t.Run(tc.body, func(t *testing.T) {
			chunks := []cadChunk{{Content: tc.body}}
			cadAddLegendContext(chunks, map[string]cadLegendEntry{tc.tag: {Tag: tc.tag, Description: tc.description, Sources: []cadSourceFact{{ID: "legend:1"}}}})
			if !strings.Contains(chunks[0].Context, tc.description) || len(chunks[0].ContextSources) != 1 || len(chunks[0].Keywords) == 0 {
				t.Fatalf("equipment tag did not retain its legend evidence: %+v", chunks[0])
			}
		})
	}
}

func TestCADResidualRegionPreservesNumberedContent(t *testing.T) {
	sheet := cadsource.Sheet{ID: "s2", Index: 2, Layout: "model", Source: "residual", Bounds: [4]float64{0, 0, 500, 200}}
	var texts []cadsource.Text
	for i := 1; i <= 6; i++ {
		texts = append(texts, cadTestText(fmt.Sprint(i), "s2", float64(i)*50, 0, 5))
	}
	chunks, diagnostics := buildCADChunks(cadTestDrawing([]cadsource.Sheet{sheet}, texts), nil, "residual")
	if len(cadTestSourceIDs(chunks)) != 6 || cadHasDiagnostic(diagnostics, "cad_sheet_grid_excluded") {
		t.Fatalf("residual content treated as frame grid: %+v", diagnostics)
	}
}

func TestCADGridMarginPreservesInteriorOnExtendedFrame(t *testing.T) {
	sheet := cadTestSheet("s1", [4]float64{0, 0, 3000, 1000})
	sheet.Source = "polyline"
	var texts []cadsource.Text
	for i := 1; i <= 6; i++ {
		texts = append(texts, cadTestText(fmt.Sprint(i), "s1", 1000+float64(i)*30, 70, 2))
	}
	chunks, diagnostics := buildCADChunks(cadTestDrawing([]cadsource.Sheet{sheet}, texts), nil, "extended frame")
	if len(cadTestSourceIDs(chunks)) != 6 || cadHasDiagnostic(diagnostics, "cad_sheet_grid_excluded") {
		t.Fatalf("long-axis margin removed interior annotations: %+v", diagnostics)
	}
}

func TestCADDeclaredDimensionReachesChunks(t *testing.T) {
	text := func(handle, x int, value string) string {
		return fmt.Sprintf("0\nTEXT\n5\n%X\n8\n0\n10\n%d\n20\n0\n40\n10\n1\n%s\n", handle, x, value)
	}
	dxf := "0\nSECTION\n2\nHEADER\n9\n$ACADVER\n1\nAC1015\n0\nENDSEC\n0\nSECTION\n2\nENTITIES\n" + text(1, 0, "ROOM") + text(2, 20, "PIPE") + "0\nDIMENSION\n5\n3\n8\n0\n70\n0\n1\n100\n10\n10\n20\n-20\n11\n10\n21\n0\n13\n0\n23\n-20\n14\n100\n24\n-20\n42\n100\n0\nENDSEC\n0\nEOF\n"
	doc, err := cadsource.Extract(t.Context(), "dimension.dxf", []byte(dxf))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Dimensions) != 1 || doc.Dimensions[0].UserText != "100" {
		t.Fatalf("fixture dimension was not decoded: %+v", doc.Dimensions)
	}
	chunks, _ := buildCADChunks(doc, nil, "dimension")
	for _, chunk := range chunks {
		if strings.Contains(chunk.Content, "100") {
			return
		}
	}
	t.Fatalf("declared dimension lost after ambiguous text matching: %+v", doc)
}

func TestCADLegendContextPreservesTagBoundaries(t *testing.T) {
	for _, body := range []string{"XPS147", "PS147A", "1\n120\n40", "11\tGATE"} {
		chunks := []cadChunk{{Content: body}}
		cadAddLegendContext(chunks, map[string]cadLegendEntry{
			"PS147": {Tag: "PS147", Description: "PRESSURE SWITCH"},
			"1":     {Tag: "1", Description: "GATE"},
		})
		if len(chunks[0].Keywords) != 0 || chunks[0].Context != "" {
			t.Fatalf("unrelated content inherited legend: %+v", chunks[0])
		}
	}
}

func TestCADLegendRowStaysOutsideTitleBlock(t *testing.T) {
	items := []cadTextItem{
		{text: "NAMA", x: 80, y: 10, height: 1},
		{text: "NIM", x: 80, y: 7, height: 1},
		{text: "43\tROTARY", x: 80, y: 16, height: 1, legendRow: true},
	}
	remaining, _ := cadExtractTitleFields(items, cadBounds{minX: 0, minY: 0, maxX: 100, maxY: 100})
	if len(remaining) != 1 || remaining[0].text != "43\tROTARY" {
		t.Fatalf("title absorbed atomic legend row: %+v", remaining)
	}
}

func TestCADLegendConflictingAliasesHaveNoInferredContext(t *testing.T) {
	chunks := []cadChunk{{Content: "PS147"}}
	cadAddLegendContext(chunks, map[string]cadLegendEntry{
		"PS147":  {Description: "PRESSURE SWITCH"},
		"PS 147": {Description: "OTHER EQUIPMENT"},
	})
	if strings.Contains(chunks[0].Context, "Legend:") || len(chunks[0].Keywords) > 0 {
		t.Fatalf("conflicting aliases propagated as fact: %+v", chunks[0])
	}
}

func TestCADTitleContentPairsFields(t *testing.T) {
	content := cadTitleContent([]cadTextItem{
		{text: "SCALA", x: 10, y: 80, height: 5},
		{text: "FORMATO", x: 60, y: 80, height: 5},
		{text: "1:50", x: 10, y: 70, height: 5},
		{text: "A3", x: 60, y: 70, height: 5},
		{text: "NAMA", x: 10, y: 50, height: 5},
		{text: "ALICE", x: 40, y: 51, height: 5},
	})
	for _, want := range []string{"SCALA: 1:50", "FORMATO: A3", "NAMA: ALICE"} {
		if !strings.Contains(content, want) {
			t.Errorf("title content %q missing %q", content, want)
		}
	}
}

func TestCADNumericLegendRowKeepsKeywordsAfterFormatting(t *testing.T) {
	texts := []cadsource.Text{
		cadTestText("TAG NUMBER", "s1", 10, 400, 5),
		cadTestText("DESCRIPTION", "s1", 100, 400, 5),
		cadTestText("43", "s1", 10, 380, 5),
		cadTestText("ROTARY", "s1", 100, 380, 5),
	}
	chunks, _ := buildCADChunks(cadTestDrawing([]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 300, 500})}, texts), nil, "legend")
	if len(chunks) != 1 || len(chunks[0].Keywords) == 0 || chunks[0].Keywords[0] != "43" {
		t.Fatalf("numeric legend row lost keywords after formatting: %+v", chunks)
	}
}

func TestCADTitleContentHandlesInlineColonValues(t *testing.T) {
	content := cadTitleContent([]cadTextItem{
		{text: "SCALE: 1:50", x: 10, y: 80, height: 5},
		{text: "ACME CORP", x: 50, y: 80, height: 5},
		{text: "REV:", x: 10, y: 60, height: 5},
		{text: "B", x: 30, y: 60, height: 5},
	})
	if strings.Contains(content, "SCALE: 1:50: ACME CORP") {
		t.Fatalf("inline field:value item was re-paired: %q", content)
	}
	if !strings.Contains(content, "REV: B") || strings.Contains(content, "REV:: B") {
		t.Fatalf("trailing colon not normalized: %q", content)
	}
}

func TestCADOversizeChunkReportsDiagnostic(t *testing.T) {
	chunks := []cadChunk{{
		Kind: cadKindRegion, SheetID: "s1", RegionID: "R1",
		SubjectID: "region:s1:R1",
		Content:   strings.Repeat("A", cadChunkOversizeRunes+1),
	}}
	out, diagnostics := cadValidateAndEmit(chunks, nil, "drawing")
	if len(out) != 1 {
		t.Fatalf("oversize chunk must stay in the output: %d", len(out))
	}
	if !cadHasDiagnostic(diagnostics, "cad_chunk_oversize") {
		t.Fatalf("diagnostics = %+v, want cad_chunk_oversize", diagnostics)
	}
}
