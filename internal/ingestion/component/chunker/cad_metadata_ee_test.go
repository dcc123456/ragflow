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
	"math"
	"strings"
	"testing"

	"ragflow/internal/agent/runtime"
	"ragflow/internal/ingestion/component/schema"
	"ragflow/internal/parser/cadsource"
)

func TestCADInvalidSheetGeometryRetainsProvenance(t *testing.T) {
	for name, box := range map[string][4]float64{
		"nan":               {math.NaN(), 0, 100, 100},
		"infinity":          {0, 0, math.Inf(1), 100},
		"negative_infinity": {math.Inf(-1), 0, 100, 100},
		"inverted":          {100, 100, 0, 0},
		"span_overflow":     {-math.MaxFloat64, 0, math.MaxFloat64, 100},
	} {
		t.Run(name, func(t *testing.T) {
			sheet := cadTestSheet("s1", box)
			sheet.Viewport = box
			text := cadTestText("PUMP", "s1", 10, 20, 5)
			text.Bounds = [4]float64{10, 20, 40, 25}
			chunks, diagnostics := buildCADChunks(cadTestDrawing([]cadsource.Sheet{sheet}, []cadsource.Text{text}), nil, "drawing")
			if len(chunks) != 1 {
				t.Fatalf("lost source text: %+v", chunks)
			}
			docs := cadTestChunkDocs(t, chunks)
			extra, _ := docs[0].ToMap()["extra"].(string)
			var payload struct {
				CAD struct {
					SheetBounds [4]float64      `json:"sheet_bounds"`
					Viewport    [4]float64      `json:"viewport"`
					SourceIDs   []string        `json:"source_ids"`
					Sources     []cadSourceFact `json:"sources"`
				} `json:"cad"`
			}
			if err := json.Unmarshal([]byte(extra), &payload); err != nil {
				t.Fatalf("provenance silently lost: %q: %v", extra, err)
			}
			want := [4]float64{10, 20, 40, 25}
			if payload.CAD.SheetBounds != want || payload.CAD.Viewport != want {
				t.Fatalf("geometry was not rebuilt from source bounds: %+v", payload.CAD)
			}
			if len(payload.CAD.SourceIDs) != 1 || len(payload.CAD.Sources) != 1 {
				t.Fatalf("source provenance lost: %+v", payload.CAD)
			}
			if !cadHasDiagnostic(diagnostics, "cad_sheet_bounds_invalid") || !cadHasDiagnostic(diagnostics, "cad_viewport_invalid") {
				t.Fatalf("missing geometry diagnostics: %+v", diagnostics)
			}
		})
	}
}

func TestCADMetadataOverflowFailsTaskWithDiagnostic(t *testing.T) {
	sheet := cadTestSheet("s1", [4]float64{0, 0, 100, 100})
	sheet.Viewport = sheet.Bounds
	doc := cadTestDrawing([]cadsource.Sheet{sheet}, []cadsource.Text{cadTestText("OVERFLOW", "s1", 0, 0, 1e308)})
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var messages []string
	ctx := runtime.WithProgressMessageCallback(t.Context(), func(component, message string) { messages = append(messages, component+": "+message) })
	upstream := schema.ChunkerFromUpstream{File: &schema.ChunkerFileMeta{Extra: map[string]json.RawMessage{"cad": raw}}}
	drawing, err := decodeCADDrawing(ctx, upstream)
	if err != nil || drawing == nil {
		t.Fatalf("decodeCADDrawing: %v", err)
	}
	out, _, err := chunkCADDrawing(ctx, upstream, drawing, nil)
	if err == nil || out != nil {
		t.Fatalf("invalid metadata reported success: error=%v output=%v", err, out)
	}
	if !strings.Contains(strings.Join(messages, "\n"), "cad_metadata_serialize_failed") {
		t.Fatalf("serialization failure not reported: %v", messages)
	}
}

func TestCADRepairedSheetDoesNotExcludeNumberedContent(t *testing.T) {
	sheet := cadTestSheet("s1", [4]float64{math.NaN(), 0, 100, 100})
	var texts []cadsource.Text
	for i, number := range []string{"1", "2", "3", "4", "5", "6"} {
		texts = append(texts, cadTestText(number, "s1", 10+float64(i)*10, 0, 1))
	}
	chunks, diagnostics := buildCADChunks(cadTestDrawing([]cadsource.Sheet{sheet}, texts), nil, "drawing")
	if len(cadTestSourceIDs(chunks)) != 6 || cadHasDiagnostic(diagnostics, "cad_sheet_grid_excluded") {
		t.Fatalf("derived text extents treated as a detected frame: %+v", diagnostics)
	}
}

func TestCADChunkDocsRejectsInvalidProvenance(t *testing.T) {
	for name, bad := range map[string]cadChunk{
		"rotation":       {Sources: []cadSourceFact{{ID: "text:1", Rotation: math.NaN()}}},
		"source_bounds":  {Sources: []cadSourceFact{{ID: "text:1", Bounds: [4]float64{0, 0, math.Inf(1), 10}}}},
		"context_bounds": {ContextSources: []cadSourceFact{{ID: "legend:1", Bounds: [4]float64{0, 0, 10, math.Inf(-1)}}}},
		"chunk_bounds":   {Bounds: [4]float64{0, 0, math.Inf(1), 10}},
	} {
		t.Run(name, func(t *testing.T) {
			bad.SheetID, bad.RegionID = "s2", "R3"
			docs, err := cadChunkDocs([]cadChunk{{Content: "GOOD"}, bad})
			if err == nil || docs != nil || !strings.Contains(err.Error(), "sheet=s2 region=R3") {
				t.Fatalf("invalid provenance emitted partial results or lost failure location: docs=%+v error=%v", docs, err)
			}
		})
	}
}

func TestCADChunkDocsOmitsUnknownSourceBounds(t *testing.T) {
	docs := cadTestChunkDocs(t, []cadChunk{{Content: "PUMP", Sources: []cadSourceFact{
		{ID: "unknown"}, {ID: "located", Bounds: [4]float64{0, 0, 10, 10}},
	}}})
	extra := docs[0].ToMap()["extra"].(string)
	var payload struct {
		CAD struct {
			Sources []map[string]json.RawMessage `json:"sources"`
		} `json:"cad"`
	}
	if err := json.Unmarshal([]byte(extra), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.CAD.Sources) != 2 || string(payload.CAD.Sources[0]["id"]) != `"unknown"` {
		t.Fatalf("source identity lost: %s", extra)
	}
	if _, exists := payload.CAD.Sources[0]["bounds"]; exists {
		t.Fatalf("unknown bounds serialized as a declared box: %s", extra)
	}
	if string(payload.CAD.Sources[1]["bounds"]) != "[0,0,10,10]" {
		t.Fatalf("valid bounds at origin omitted: %s", extra)
	}
}

func TestCADTitleMetadataNormalizesLabelPunctuation(t *testing.T) {
	content := cadTitleContent([]cadTextItem{
		{text: "SCALE:", x: 10, y: 80, height: 5},
		{text: "SHEET", x: 200, y: 80, height: 5},
		{text: "1/1", x: 10, y: 70, height: 5},
		{text: "1", x: 200, y: 70, height: 5},
		{text: "DWG NO.", x: 10, y: 50, height: 5},
		{text: "DP24572-A", x: 40, y: 50, height: 5},
	})
	fields := cadTitleMetadata(content)
	if fields["scale"] != "1/1" || fields["sheet"] != "1" || fields["drawing_no"] != "DP24572-A" {
		t.Fatalf("title metadata = %+v (content %q)", fields, content)
	}
}

func TestCADChunkMetadataMergesTitleBlocksPerSheet(t *testing.T) {
	chunks := []cadChunk{
		{Kind: cadKindTitleBlock, SheetID: "s1", Content: "SCALE: 1:50"},
		{Kind: cadKindTitleBlock, SheetID: "s1", Content: "SCALE: 1:100\nSHEET: 3"},
	}
	document, bySheet := cadChunkMetadata(chunks)
	if len(document) != 0 {
		t.Fatalf("document metadata = %+v, want none", document)
	}
	sheet := bySheet["s1"]
	if sheet["scale"] != "1:50" || sheet["sheet"] != "3" {
		t.Fatalf("merged sheet metadata = %+v", sheet)
	}
}

func TestCADTitleMetadataFullWidthColon(t *testing.T) {
	content := cadTitleContent([]cadTextItem{
		{text: "图号：M0000506", x: 10, y: 80, height: 5},
		{text: "图名：制冷详情", x: 10, y: 70, height: 5},
	})
	if strings.Contains(content, "：") {
		t.Fatalf("full-width separator survived normalization: %q", content)
	}
	fields := cadTitleMetadata(content)
	if fields["drawing_no"] != "M0000506" || fields["drawing_title"] != "制冷详情" {
		t.Fatalf("title metadata = %+v (content %q)", fields, content)
	}
	if direct := cadTitleMetadata("图号：M0000506"); direct["drawing_no"] != "M0000506" {
		t.Fatalf("direct full-width parse = %+v", direct)
	}
}
