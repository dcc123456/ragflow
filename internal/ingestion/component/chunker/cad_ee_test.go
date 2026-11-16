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
	"context"
	"encoding/json"
	"strings"
	"testing"

	"ragflow/internal/agent/runtime"
	"ragflow/internal/ingestion/component/schema"
	"ragflow/internal/parser/cadsource"
	"ragflow/internal/parser/parser"
	"ragflow/internal/utility"
)

func cadTestSheet(id string, bounds [4]float64) cadsource.Sheet {
	return cadsource.Sheet{ID: id, Index: 1, Bounds: bounds}
}

func cadTestText(text, sheet string, x, y, height float64) cadsource.Text {
	return cadsource.Text{
		Text: text, SheetID: sheet, X: x, Y: y, Height: height,
		Origin: cadsource.OriginSheetTexts, Confidence: 1,
	}
}

func cadTestDrawing(sheets []cadsource.Sheet, texts []cadsource.Text, attributes ...cadsource.Attribute) *cadsource.Drawing {
	return &cadsource.Drawing{
		Sheets:     sheets,
		Texts:      texts,
		Attributes: attributes,
		Diagnostics: cadsource.Diagnostics{
			SheetDetection: "detected",
		},
	}
}

func cadTestTexts(chunks []cadChunk) []string {
	var lines []string
	for _, chunk := range chunks {
		lines = append(lines, strings.Split(chunk.Content, "\n")...)
	}
	return lines
}

func cadTestSourceIDs(chunks []cadChunk) map[string]int {
	counts := make(map[string]int)
	for _, chunk := range chunks {
		for _, id := range chunk.SourceIDs {
			if id == "" {
				continue
			}
			counts[id]++
		}
	}
	return counts
}

// TestCADChunkEvidenceGates locks the gating table: a semantic kind without its
// minimum evidence must be rejected, and a region must always be accepted.
func TestCADChunkEvidenceGates(t *testing.T) {
	cases := []struct {
		name     string
		kind     cadChunkKind
		evidence []cadEvidence
		want     bool
	}{
		{name: "region always", kind: cadKindRegion, want: true},
		{name: "title needs layout evidence", kind: cadKindTitleBlock, want: false},
		{name: "title with derived evidence", kind: cadKindTitleBlock,
			evidence: []cadEvidence{{Level: cadEvidenceDerived, Source: "layout", Method: "title_block"}}, want: true},
		{name: "title with inferred evidence", kind: cadKindTitleBlock,
			evidence: []cadEvidence{{Level: cadEvidenceInferred, Source: "layout", Method: "title_block"}}, want: false},
		{name: "notes needs header evidence", kind: cadKindNotes, want: false},
		{name: "notes with derived evidence", kind: cadKindNotes,
			evidence: []cadEvidence{{Level: cadEvidenceDerived, Source: "layout", Method: "notes"}}, want: true},
		{name: "attributes need tag value", kind: cadKindAttributes, want: false},
		{name: "attributes with derived evidence", kind: cadKindAttributes,
			evidence: []cadEvidence{{Level: cadEvidenceDerived, Source: "attrib", Method: "tag_value"}}, want: true},
		{name: "object needs insert identity", kind: cadKindObject,
			evidence: []cadEvidence{{Level: cadEvidenceDeclared, Source: "insert", Method: "owner_handle"}}, want: false},
		{name: "object with declared insert and attribs", kind: cadKindObject,
			evidence: []cadEvidence{
				{Level: cadEvidenceDeclared, Source: "insert", Method: "owner_handle"},
				{Level: cadEvidenceDeclared, Source: "attrib", Method: "tag_value"},
			}, want: true},
		{name: "object rejects inferred attribs", kind: cadKindObject,
			evidence: []cadEvidence{
				{Level: cadEvidenceDeclared, Source: "insert", Method: "owner_handle"},
				{Level: cadEvidenceInferred, Source: "attrib", Method: "tag_value"},
			}, want: false},
		{name: "table needs cells or grid", kind: cadKindTable, want: false},
		{name: "table with native cells", kind: cadKindTable,
			evidence: []cadEvidence{{Level: cadEvidenceDeclared, Source: "table", Method: "native_cells"}}, want: true},
		{name: "table with derived text grid", kind: cadKindTable,
			evidence: []cadEvidence{{Level: cadEvidenceDerived, Source: "table", Method: "text_grid"}}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cadEvidenceSatisfies(tc.kind, tc.evidence); got != tc.want {
				t.Fatalf("cadEvidenceSatisfies(%s, %+v) = %v, want %v", tc.kind, tc.evidence, got, tc.want)
			}
		})
	}
}

// TestCADChunkerSplitsRegionsBySpace verifies that clearly separated text
// clusters become separate region chunks instead of one sheet-wide chunk.
func TestCADChunkerSplitsRegionsBySpace(t *testing.T) {
	drawing := cadTestDrawing(
		[]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 1000, 600})},
		[]cadsource.Text{
			cadTestText("A1", "s1", 10, 500, 10),
			cadTestText("A2", "s1", 20, 500, 10),
			cadTestText("A3", "s1", 30, 500, 10),
			cadTestText("B1", "s1", 700, 100, 10),
			cadTestText("B2", "s1", 710, 100, 10),
			cadTestText("B3", "s1", 720, 100, 10),
		},
	)
	chunks, diagnostics := buildCADChunks(drawing, defaultCADChunkerConfig(), "M-102")
	if len(diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diagnostics)
	}
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d, want 2: %+v", len(chunks), chunks)
	}
	if chunks[0].Kind != cadKindRegion || chunks[1].Kind != cadKindRegion {
		t.Fatalf("kinds = %s/%s, want region/region", chunks[0].Kind, chunks[1].Kind)
	}
	if !strings.Contains(chunks[0].Content, "A1") || strings.Contains(chunks[0].Content, "B1") {
		t.Fatalf("first region content = %q, want only cluster A", chunks[0].Content)
	}
	if !strings.Contains(chunks[1].Content, "B1") || strings.Contains(chunks[1].Content, "A1") {
		t.Fatalf("second region content = %q, want only cluster B", chunks[1].Content)
	}
	if len(cadTestTexts(chunks)) != 6 {
		t.Fatalf("text lines = %d, want 6", len(cadTestTexts(chunks)))
	}
}

// TestCADChunkerDeterministic verifies that input order does not change chunk
// order, region ids, or content.
func TestCADChunkerDeterministic(t *testing.T) {
	drawing := cadTestDrawing(
		[]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 1000, 600})},
		[]cadsource.Text{
			cadTestText("A1", "s1", 10, 500, 10),
			cadTestText("A2", "s1", 20, 500, 10),
			cadTestText("A3", "s1", 30, 500, 10),
			cadTestText("B1", "s1", 700, 100, 10),
			cadTestText("B2", "s1", 710, 100, 10),
			cadTestText("B3", "s1", 720, 100, 10),
		},
	)
	first, _ := buildCADChunks(drawing, defaultCADChunkerConfig(), "M-102")
	firstJSON, err := json.Marshal(cadChunkDocs(first))
	if err != nil {
		t.Fatalf("marshal first: %v", err)
	}

	reversed := make([]cadsource.Text, len(drawing.Texts))
	for i := range drawing.Texts {
		reversed[len(drawing.Texts)-1-i] = drawing.Texts[i]
	}
	drawing.Texts = reversed
	second, _ := buildCADChunks(drawing, defaultCADChunkerConfig(), "M-102")
	secondJSON, err := json.Marshal(cadChunkDocs(second))
	if err != nil {
		t.Fatalf("marshal second: %v", err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("chunks are not deterministic:\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}
}

// TestCADChunkerSheetFallbackClusters verifies that a fallback sheet (no
// detected frame) still uses text extents and spatial regions.
func TestCADChunkerSheetFallbackClusters(t *testing.T) {
	drawing := cadTestDrawing(
		[]cadsource.Sheet{{ID: "s0", Name: "整图"}},
		[]cadsource.Text{
			cadTestText("A1", "s0", 10, 500, 10),
			cadTestText("A2", "s0", 20, 500, 10),
			cadTestText("A3", "s0", 30, 500, 10),
			cadTestText("B1", "s0", 700, 100, 10),
			cadTestText("B2", "s0", 710, 100, 10),
			cadTestText("B3", "s0", 720, 100, 10),
		},
	)
	drawing.Diagnostics.SheetDetection = "fallback"
	chunks, _ := buildCADChunks(drawing, defaultCADChunkerConfig(), "M-102")
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d, want 2: %+v", len(chunks), chunks)
	}
}

// TestCADChunkerNoHeightWholeSheet verifies the conservative fallback: without
// heights and without a reliable line pitch, the sheet stays one chunk.
func TestCADChunkerNoHeightWholeSheet(t *testing.T) {
	drawing := cadTestDrawing(
		[]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 1000, 600})},
		[]cadsource.Text{
			cadTestText("A1", "s1", 10, 500, 0),
			cadTestText("A2", "s1", 700, 500, 0),
			cadTestText("A3", "s1", 300, 500, 0),
		},
	)
	chunks, diagnostics := buildCADChunks(drawing, defaultCADChunkerConfig(), "M-102")
	if len(chunks) != 1 {
		t.Fatalf("chunks = %d, want 1: %+v", len(chunks), chunks)
	}
	if !cadHasEvidence(chunks[0].Evidence, "text_layout", "whole_sheet_fallback", cadEvidenceInferred) {
		t.Fatalf("evidence = %+v, want whole_sheet_fallback", chunks[0].Evidence)
	}
	if !cadHasDiagnostic(diagnostics, "cad_scale_unreliable") {
		t.Fatalf("diagnostics = %+v, want cad_scale_unreliable", diagnostics)
	}
}

// TestCADChunkerTitleBlock verifies bottom-right field labels become a
// title_block chunk and the rest stays a region.
func TestCADChunkerTitleBlock(t *testing.T) {
	drawing := cadTestDrawing(
		[]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 1000, 600})},
		[]cadsource.Text{
			cadTestText("主视图", "s1", 50, 500, 10),
			cadTestText("φ25", "s1", 60, 500, 10),
			cadTestText("Ra1.6", "s1", 70, 500, 10),
			cadTestText("图号: M-102", "s1", 800, 50, 10),
			cadTestText("比例 1:2", "s1", 800, 30, 10),
			cadTestText("材料 45钢", "s1", 800, 10, 10),
		},
	)
	chunks, _ := buildCADChunks(drawing, defaultCADChunkerConfig(), "M-102")
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d, want 2: %+v", len(chunks), chunks)
	}
	if chunks[1].Kind != cadKindTitleBlock {
		t.Fatalf("second kind = %s, want title_block (chunks=%+v)", chunks[1].Kind, chunks)
	}
	if !strings.Contains(chunks[1].Content, "图号") {
		t.Fatalf("title block content = %q", chunks[1].Content)
	}
}

// TestCADChunkerNotes verifies a technical-notes region is separated without
// duplicating its text in another chunk.
func TestCADChunkerNotes(t *testing.T) {
	drawing := cadTestDrawing(
		[]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 1000, 600})},
		[]cadsource.Text{
			cadTestText("技术要求", "s1", 10, 500, 10),
			cadTestText("1. 未注公差按 GB/T 1804-m", "s1", 10, 480, 10),
			cadTestText("2. 表面粗糙度 Ra1.6", "s1", 10, 460, 10),
			cadTestText("3. 热处理调质", "s1", 10, 440, 10),
			cadTestText("主视图", "s1", 600, 500, 10),
			cadTestText("φ30", "s1", 610, 500, 10),
			cadTestText("120", "s1", 620, 500, 10),
		},
	)
	chunks, _ := buildCADChunks(drawing, defaultCADChunkerConfig(), "M-102")
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d, want 2: %+v", len(chunks), chunks)
	}
	notes := 0
	for _, chunk := range chunks {
		if chunk.Kind == cadKindNotes {
			notes++
			if !strings.Contains(chunk.Content, "技术要求") {
				t.Fatalf("notes content = %q", chunk.Content)
			}
		}
	}
	if notes != 1 {
		t.Fatalf("notes chunks = %d, want 1: %+v", notes, chunks)
	}
	lines := cadTestTexts(chunks)
	if len(lines) != 7 {
		t.Fatalf("text lines = %d, want 7 (no duplication): %+v", len(lines), lines)
	}
}

// TestCADChunkerAttributesGroupedByOwner verifies attributes from different
// owners never share a chunk, duplicates are collapsed, and the bare value is
// still kept in the region text.
func TestCADChunkerAttributesGroupedByOwner(t *testing.T) {
	drawing := cadTestDrawing(
		[]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 1000, 600})},
		[]cadsource.Text{
			cadTestText("45钢", "s1", 10, 500, 10),
			cadTestText("V-101", "s1", 20, 500, 10),
			cadTestText("DN100", "s1", 30, 500, 10),
		},
		cadsource.Attribute{Tag: "MATERIAL", Value: "45钢", OwnerHandle: 0x8E},
		cadsource.Attribute{Tag: "MATERIAL", Value: "45钢", OwnerHandle: 0x8E},
		cadsource.Attribute{Tag: "DN", Value: "100", OwnerHandle: 0x8E},
		cadsource.Attribute{Tag: "TAG", Value: "V-101", OwnerHandle: 0xA1},
		cadsource.Attribute{Tag: "SPEC", Value: "DN100", OwnerHandle: 0xA1},
	)
	chunks, _ := buildCADChunks(drawing, defaultCADChunkerConfig(), "M-102")
	if len(chunks) != 3 {
		t.Fatalf("chunks = %d, want 3 (region + 2 attribute owners): %+v", len(chunks), chunks)
	}
	var attributeChunks []cadChunk
	for _, chunk := range chunks {
		if chunk.Kind == cadKindAttributes {
			attributeChunks = append(attributeChunks, chunk)
		}
	}
	if len(attributeChunks) != 2 {
		t.Fatalf("attribute chunks = %d, want 2: %+v", len(attributeChunks), attributeChunks)
	}
	if strings.Count(attributeChunks[0].Content, "MATERIAL: 45钢") != 1 {
		t.Fatalf("duplicate attribute kept: %q", attributeChunks[0].Content)
	}
	if !strings.Contains(attributeChunks[0].Content, "DN: 100") {
		t.Fatalf("owner 8E chunk = %q", attributeChunks[0].Content)
	}
	if !strings.Contains(attributeChunks[1].Content, "TAG: V-101") {
		t.Fatalf("owner A1 chunk = %q", attributeChunks[1].Content)
	}
	if !strings.Contains(chunks[0].Content, "45钢") {
		t.Fatalf("region lost the bare attribute value: %q", chunks[0].Content)
	}
}

// TestCADChunkerZeroText verifies an empty drawing yields no chunks and a
// diagnostic, while the chunker still claims the document.
func TestCADChunkerZeroText(t *testing.T) {
	drawing := cadTestDrawing(
		[]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 1000, 600})},
		nil,
	)
	chunks, diagnostics := buildCADChunks(drawing, defaultCADChunkerConfig(), "M-102")
	if len(chunks) != 0 {
		t.Fatalf("chunks = %d, want 0", len(chunks))
	}
	if !cadHasDiagnostic(diagnostics, "cad_no_text") {
		t.Fatalf("diagnostics = %+v, want cad_no_text", diagnostics)
	}

	raw, err := json.Marshal(drawing)
	if err != nil {
		t.Fatalf("marshal drawing: %v", err)
	}
	upstream := schema.ChunkerFromUpstream{
		Name: "empty.dxf",
		File: &schema.ChunkerFileMeta{Extra: map[string]json.RawMessage{"cad": raw}},
	}
	var messages []string
	ctx := runtime.WithProgressMessageCallback(context.Background(), func(_, message string) {
		messages = append(messages, message)
	})
	out, handled, err := chunkCAD(ctx, upstream)
	if err != nil {
		t.Fatalf("chunkCAD: %v", err)
	}
	if !handled {
		t.Fatal("chunkCAD did not claim the CAD payload")
	}
	chunksOut, _ := out["chunks"].([]map[string]any)
	if len(chunksOut) != 0 {
		t.Fatalf("emitted chunks = %d, want 0", len(chunksOut))
	}
	if !containsMessage(messages, "cad_no_text") {
		t.Fatalf("progress messages = %+v, want cad_no_text", messages)
	}
}

// TestCADChunkerOversizeSplits verifies an oversized region is split without
// losing or duplicating any line.
func TestCADChunkerOversizeSplits(t *testing.T) {
	texts := make([]cadsource.Text, 0, 120)
	for i := 0; i < 120; i++ {
		texts = append(texts, cadTestText("L"+itoa(i), "s1", 10, float64(1000-i*10), 10))
	}
	drawing := cadTestDrawing(
		[]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 1000, 600})},
		texts,
	)
	chunks, _ := buildCADChunks(drawing, defaultCADChunkerConfig(), "M-102")
	if len(chunks) < 2 {
		t.Fatalf("chunks = %d, want >= 2: %+v", len(chunks), chunks)
	}
	lines := cadTestTexts(chunks)
	if len(lines) != 120 {
		t.Fatalf("lines = %d, want 120", len(lines))
	}
	seen := make(map[string]bool, len(lines))
	for _, line := range lines {
		if seen[line] {
			t.Fatalf("duplicate line %q", line)
		}
		seen[line] = true
	}
}

// TestCADChunkerNoCrossSheetLeak verifies texts never move between sheets.
func TestCADChunkerNoCrossSheetLeak(t *testing.T) {
	drawing := cadTestDrawing(
		[]cadsource.Sheet{
			cadTestSheet("s1", [4]float64{0, 0, 1000, 600}),
			cadTestSheet("s2", [4]float64{0, 0, 1000, 600}),
		},
		[]cadsource.Text{
			cadTestText("A1", "s1", 10, 500, 10),
			cadTestText("A2", "s1", 20, 500, 10),
			cadTestText("A3", "s1", 30, 500, 10),
			cadTestText("B1", "s2", 10, 500, 10),
			cadTestText("B2", "s2", 20, 500, 10),
			cadTestText("B3", "s2", 30, 500, 10),
		},
	)
	chunks, _ := buildCADChunks(drawing, defaultCADChunkerConfig(), "M-102")
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d, want 2: %+v", len(chunks), chunks)
	}
	for _, chunk := range chunks {
		switch chunk.SheetID {
		case "s1":
			if strings.Contains(chunk.Content, "B1") {
				t.Fatalf("sheet s1 chunk leaked B text: %q", chunk.Content)
			}
		case "s2":
			if strings.Contains(chunk.Content, "A1") {
				t.Fatalf("sheet s2 chunk leaked A text: %q", chunk.Content)
			}
		default:
			t.Fatalf("unexpected sheet id %q", chunk.SheetID)
		}
	}
}

// TestCADChunkerChunkIDsUnique verifies identical text on two sheets still
// produces distinct chunk ids.
func TestCADChunkerChunkIDsUnique(t *testing.T) {
	drawing := cadTestDrawing(
		[]cadsource.Sheet{
			cadTestSheet("s1", [4]float64{0, 0, 1000, 600}),
			cadTestSheet("s2", [4]float64{0, 0, 1000, 600}),
		},
		[]cadsource.Text{
			cadTestText("重复文字", "s1", 10, 500, 10),
			cadTestText("重复文字2", "s1", 20, 500, 10),
			cadTestText("重复文字3", "s1", 30, 500, 10),
			cadTestText("重复文字", "s2", 10, 500, 10),
			cadTestText("重复文字2", "s2", 20, 500, 10),
			cadTestText("重复文字3", "s2", 30, 500, 10),
		},
	)
	chunks, _ := buildCADChunks(drawing, defaultCADChunkerConfig(), "M-102")
	seen := make(map[string]bool, len(chunks))
	for _, chunk := range chunks {
		doc := cadChunkDocs([]cadChunk{chunk})[0]
		id := canonicalChunkID("doc-1", doc)
		if seen[id] {
			t.Fatalf("duplicate chunk id %s for content %q", id, doc.Text)
		}
		seen[id] = true
	}
	if len(seen) != len(chunks) {
		t.Fatalf("ids = %d, chunks = %d", len(seen), len(chunks))
	}
}

// TestCADChunkerSourceCoverage verifies every non-empty text appears exactly
// once across the emitted chunks.
func TestCADChunkerSourceCoverage(t *testing.T) {
	drawing := cadTestDrawing(
		[]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 1000, 600})},
		[]cadsource.Text{
			cadTestText("A1", "s1", 10, 500, 10),
			cadTestText("A2", "s1", 20, 500, 10),
			cadTestText("A3", "s1", 30, 500, 10),
			cadTestText("B1", "s1", 700, 100, 10),
			cadTestText("B2", "s1", 710, 100, 10),
			cadTestText("B3", "s1", 720, 100, 10),
		},
	)
	chunks, diagnostics := buildCADChunks(drawing, defaultCADChunkerConfig(), "M-102")
	counts := cadTestSourceIDs(chunks)
	if len(counts) != 6 {
		t.Fatalf("source ids = %d, want 6: %+v", len(counts), counts)
	}
	for id, count := range counts {
		if count != 1 {
			t.Fatalf("source %s appears %d times, want 1", id, count)
		}
	}
	if cadHasDiagnostic(diagnostics, "cad_text_unassigned") {
		t.Fatalf("diagnostics = %+v, want no cad_text_unassigned", diagnostics)
	}
}

// TestCADChunkerMissingPayload verifies the fallback contract: no structured
// payload means the generic text path keeps the document.
func TestCADChunkerMissingPayload(t *testing.T) {
	out, handled, err := chunkCAD(context.Background(), schema.ChunkerFromUpstream{Name: "drawing.dxf"})
	if err != nil {
		t.Fatalf("chunkCAD: %v", err)
	}
	if handled || out != nil {
		t.Fatalf("handled = %v, out = %+v; want false/nil", handled, out)
	}
}

// TestCADChunkerInvalidPayload verifies a malformed payload falls back instead
// of failing the run.
func TestCADChunkerInvalidPayload(t *testing.T) {
	upstream := schema.ChunkerFromUpstream{
		Name: "drawing.dxf",
		File: &schema.ChunkerFileMeta{Extra: map[string]json.RawMessage{
			"cad": json.RawMessage("{not json"),
		}},
	}
	var messages []string
	ctx := runtime.WithProgressMessageCallback(context.Background(), func(_, message string) {
		messages = append(messages, message)
	})
	_, handled, err := chunkCAD(ctx, upstream)
	if err != nil {
		t.Fatalf("chunkCAD: %v", err)
	}
	if handled {
		t.Fatal("malformed payload must not be handled")
	}
	if !containsMessage(messages, "cad_payload_invalid") {
		t.Fatalf("messages = %+v, want cad_payload_invalid", messages)
	}
}

// TestCADChunkerFromParsedDXF drives the real parser output through the chunker
// so the File["cad"] handoff is covered end to end.
func TestCADChunkerFromParsedDXF(t *testing.T) {
	p, _, err := parser.GetParser(utility.FileTypeCAD, []byte(cadFixtureDXF))
	if err != nil {
		t.Fatalf("GetParser: %v", err)
	}
	result := p.ParseWithResult(context.Background(), "drawing.dxf", []byte(cadFixtureDXF))
	if result.Err != nil {
		t.Fatalf("ParseWithResult: %v", result.Err)
	}
	raw, err := json.Marshal(result.File)
	if err != nil {
		t.Fatalf("marshal parser file: %v", err)
	}
	var meta schema.ChunkerFileMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("unmarshal ChunkerFileMeta: %v", err)
	}
	upstream := schema.ChunkerFromUpstream{
		Name:     "drawing.dxf",
		FileType: "cad",
		File:     &meta,
	}
	out, handled, err := chunkCAD(context.Background(), upstream)
	if err != nil {
		t.Fatalf("chunkCAD: %v", err)
	}
	if !handled {
		t.Fatal("chunkCAD did not claim the parsed CAD document")
	}
	chunks, _ := out["chunks"].([]map[string]any)
	if len(chunks) == 0 {
		t.Fatal("no chunks emitted")
	}
	found := false
	for _, chunk := range chunks {
		if text, _ := chunk["text"].(string); strings.Contains(text, "hello dxf") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no chunk contains the parsed text: %+v", chunks)
	}
}

func containsMessage(messages []string, needle string) bool {
	for _, message := range messages {
		if strings.Contains(message, needle) {
			return true
		}
	}
	return false
}

func cadHasDiagnostic(diagnostics []cadDiagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	pos := len(digits)
	for value > 0 {
		pos--
		digits[pos] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[pos:])
}

// cadFixtureDXF is a synthetic R2000 DXF with one TEXT "hello dxf" on layer
// "Wall" at world (1.0, 2.0), mirroring the parser's own fixture.
const cadFixtureDXF = `0
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
