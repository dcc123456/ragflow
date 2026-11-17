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
	"fmt"
	"math"
	"strings"
	"testing"

	"ragflow/internal/parser/cadsource"
)

func TestCADHeightBands(t *testing.T) {
	bands := cadHeightBands([]cadTextItem{
		{text: "paper-1", height: 2.5},
		{text: "paper-2", height: 2.5},
		{text: "plan-1", height: 110},
		{text: "plan-2", height: 150},
	}, 4)
	if len(bands) != 2 {
		t.Fatalf("bands = %d, want 2: %+v", len(bands), bands)
	}
	if len(bands[0]) != 2 || len(bands[1]) != 2 {
		t.Fatalf("band sizes = %d/%d, want 2/2", len(bands[0]), len(bands[1]))
	}

	single := cadHeightBands([]cadTextItem{
		{text: "a", height: 2.5},
		{text: "b", height: 3},
		{text: "c", height: 4},
	}, 4)
	if len(single) != 1 || len(single[0]) != 3 {
		t.Fatalf("single band = %+v, want one band of 3", single)
	}

	withZero := cadHeightBands([]cadTextItem{
		{text: "a", height: 2.5},
		{text: "b", height: 2.5},
		{text: "z", height: 0},
	}, 4)
	if len(withZero) != 1 || len(withZero[0]) != 3 {
		t.Fatalf("zero-height band = %+v, want one band of 3", withZero)
	}
}

func TestCADNotesHeader(t *testing.T) {
	for _, text := range []string{"NOTE", "Notes:", "note 1:", "技术要求", "General notes"} {
		if !cadIsNotesHeader(text) {
			t.Errorf("cadIsNotesHeader missed %q", text)
		}
	}
	for _, text := range []string{"footnote: x", "This note is not a header", "notebook"} {
		if cadIsNotesHeader(text) {
			t.Errorf("cadIsNotesHeader matched %q", text)
		}
	}
}

func TestCADTargetRegions(t *testing.T) {
	cfg := defaultCADChunkerConfig()
	cases := []struct {
		texts int
		want  int
	}{
		{texts: 1, want: 1},
		{texts: 10, want: 1},
		{texts: 12, want: 1},
		{texts: 13, want: 2},
		{texts: 24, want: 2},
		{texts: 59, want: 5},
		{texts: 1000, want: cfg.MaxRegions},
	}
	for _, tc := range cases {
		if got := cadTargetRegions(tc.texts, cfg); got != tc.want {
			t.Errorf("cadTargetRegions(%d) = %d, want %d", tc.texts, got, tc.want)
		}
	}
}

// TestCADRegionCapDiagnostic locks the cap diagnostic to the one case that
// deserves it: a band whose natural target exceeds MaxRegions. A band that
// simply reaches its natural target of MaxRegions was never clipped.
func TestCADRegionCapDiagnostic(t *testing.T) {
	diagnostics := func(texts int) []cadDiagnostic {
		items := make([]cadsource.Text, 0, texts)
		for i := 0; i < texts; i++ {
			items = append(items, cadTestText(fmt.Sprintf("T%03d", i), "s1",
				float64(i%24)*100, float64(i/24)*20, 10))
		}
		drawing := cadTestDrawing(
			[]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 2400, 1000})},
			items,
		)
		_, out := buildCADChunks(drawing, defaultCADChunkerConfig(), "cap")
		return out
	}
	hasCap := func(diagnostics []cadDiagnostic) bool {
		for _, diagnostic := range diagnostics {
			if diagnostic.Code == "cad_region_cap" {
				return true
			}
		}
		return false
	}

	cfg := defaultCADChunkerConfig()
	if got := cadNaturalRegions(cfg.MaxRegions*cfg.TextsPerRegion, cfg); got != cfg.MaxRegions {
		t.Fatalf("cadNaturalRegions(%d) = %d, want %d", cfg.MaxRegions*cfg.TextsPerRegion, got, cfg.MaxRegions)
	}
	if hasCap(diagnostics(cfg.MaxRegions * cfg.TextsPerRegion)) {
		t.Fatalf("cad_region_cap fired at the natural target %d", cfg.MaxRegions)
	}
	if !hasCap(diagnostics(cfg.MaxRegions*cfg.TextsPerRegion + 1)) {
		t.Fatal("cad_region_cap missing when MaxRegions clips the natural target")
	}
}

// TestCADSplitBoxesTargetCap guards the old maxParts breach: the recursive
// threshold split could return more parts than requested.
func TestCADSplitBoxesTargetCap(t *testing.T) {
	xs := []float64{0, 110, 310, 610, 1010, 1510}
	boxes := make([]cadTextBox, len(xs))
	for i, x := range xs {
		boxes[i] = cadTextBox{minX: x, maxX: x + 1, minY: 0, maxY: 1}
	}
	parts := splitCADBoxesTarget(boxes, 50, 4)
	if len(parts) > 4 {
		t.Fatalf("splitCADBoxesTarget = %d parts, want <= 4", len(parts))
	}
	if total := countCADBoxes(parts); total != len(boxes) {
		t.Fatalf("target split lost text: %d boxes, want %d", total, len(boxes))
	}
}

func TestCADChunkerMixedScaleBands(t *testing.T) {
	drawing := cadTestDrawing(
		[]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 1000, 600})},
		[]cadsource.Text{
			cadTestText("paper-1", "s1", 10, 500, 2),
			cadTestText("paper-2", "s1", 20, 500, 2),
			cadTestText("paper-3", "s1", 30, 500, 2),
			cadTestText("paper-4", "s1", 40, 500, 2),
			cadTestText("plan-1", "s1", 600, 100, 100),
			cadTestText("plan-2", "s1", 610, 100, 100),
			cadTestText("plan-3", "s1", 620, 100, 100),
			cadTestText("plan-4", "s1", 630, 100, 100),
			cadTestText("plan-5", "s1", 640, 100, 100),
		},
	)
	chunks, diagnostics := buildCADChunks(drawing, defaultCADChunkerConfig(), "mixed")
	if !cadHasDiagnostic(diagnostics, "cad_scale_mixed") {
		t.Fatalf("diagnostics = %+v, want cad_scale_mixed", diagnostics)
	}
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d, want 2 (one per scale band): %+v", len(chunks), chunks)
	}
	for _, chunk := range chunks {
		hasPaper := strings.Contains(chunk.Content, "paper-")
		hasPlan := strings.Contains(chunk.Content, "plan-")
		if hasPaper && hasPlan {
			t.Fatalf("chunk mixes scale bands: %q", chunk.Content)
		}
	}
}

// TestCADChunkerLocalizedTitleFields recognizes an Italian title block from
// its scale and format labels while retaining its accompanying text.
func TestCADChunkerLocalizedTitleFields(t *testing.T) {
	var texts []cadsource.Text
	for i := 0; i < 13; i++ {
		text := fmt.Sprintf("riga %02d", i)
		if i == 0 {
			text = "SCALA"
		}
		if i == 1 {
			text = "FORMATO"
		}
		texts = append(texts, cadTestText(text, "s1", 50, float64(10+i*5), 5))
	}
	drawing := cadTestDrawing(
		[]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 1000, 600})},
		texts,
	)
	chunks, _ := buildCADChunks(drawing, defaultCADChunkerConfig(), "title")
	if len(chunks) != 1 || chunks[0].Kind != cadKindTitleBlock {
		t.Fatalf("chunks = %+v, want one localized title_block", chunks)
	}
}

// TestCADChunkerBottomNotesNotTitleBlock guards the structural rule against
// claiming a notes list: a bottom block of short lines that starts with a
// notes header must stay notes, not become a title block.
func TestCADChunkerBottomNotesNotTitleBlock(t *testing.T) {
	texts := []cadsource.Text{
		cadTestText("主视图", "s1", 600, 500, 10),
		cadTestText("φ30", "s1", 610, 495, 10),
		cadTestText("120", "s1", 620, 490, 10),
		cadTestText("技术要求", "s1", 20, 60, 5),
	}
	for i := 0; i < 8; i++ {
		texts = append(texts, cadTestText(fmt.Sprintf("%d. 未注公差按 GB/T 1804-m", i+1), "s1", 20, float64(52-i*5), 5))
	}
	drawing := cadTestDrawing(
		[]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 1000, 600})},
		texts,
	)
	chunks, _ := buildCADChunks(drawing, defaultCADChunkerConfig(), "notes")
	if len(cadTestTexts(chunks)) != len(texts) {
		t.Fatalf("notes split lost or duplicated text: %+v", chunks)
	}
	notes, titles := 0, 0
	for _, chunk := range chunks {
		switch chunk.Kind {
		case cadKindNotes:
			notes++
		case cadKindTitleBlock:
			titles++
		}
	}
	if notes != 1 || titles != 0 {
		t.Fatalf("notes=%d title_block=%d, want 1/0: %+v", notes, titles, chunks)
	}
}

func TestCADChunkerNotesAfterTable(t *testing.T) {
	var texts []cadsource.Text
	for i := 0; i < 5; i++ {
		texts = append(texts, cadTestText(fmt.Sprintf("row %d", i), "s1", 10, float64(500-i*10), 10))
	}
	texts = append(texts, cadTestText("NOTE", "s1", 10, 400, 10))
	for i := 0; i < 3; i++ {
		texts = append(texts, cadTestText(fmt.Sprintf("nota %d", i), "s1", 10, float64(380-i*10), 10))
	}
	drawing := cadTestDrawing(
		[]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 1000, 600})},
		texts,
	)
	chunks, _ := buildCADChunks(drawing, defaultCADChunkerConfig(), "notes")
	if len(cadTestTexts(chunks)) != len(texts) {
		t.Fatalf("notes split lost or duplicated text: %+v", chunks)
	}
	notes := 0
	for _, chunk := range chunks {
		if chunk.Kind != cadKindNotes {
			continue
		}
		notes++
		if !strings.Contains(chunk.Content, "NOTE") {
			t.Fatalf("notes chunk = %q, want the NOTE header", chunk.Content)
		}
	}
	if notes != 1 {
		t.Fatalf("notes chunks = %d, want 1: %+v", notes, chunks)
	}
}

func TestCADChunkerLoneTextStaysSeparateWhenFar(t *testing.T) {
	texts := make([]cadsource.Text, 0, 14)
	for i := 0; i < 13; i++ {
		texts = append(texts, cadTestText(fmt.Sprintf("label %d", i), "s1", 10, float64(500-i*10), 10))
	}
	texts = append(texts, cadTestText("drawing title", "s1", 900, 50, 10))
	drawing := cadTestDrawing(
		[]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 1000, 600})},
		texts,
	)
	chunks, _ := buildCADChunks(drawing, defaultCADChunkerConfig(), "lone")
	if len(chunks) != 2 {
		t.Fatalf("chunks = %+v, want the far title kept separate", chunks)
	}
	found := false
	for _, chunk := range chunks {
		if strings.Contains(chunk.Content, "drawing title") {
			found = true
			if chunk.Bounds[2]-chunk.Bounds[0] > 200 {
				t.Fatalf("far title stretched a region bbox: %+v", chunk.Bounds)
			}
		}
	}
	if !found {
		t.Fatal("drawing title disappeared")
	}
}

func TestCADChunkerFallbackBudget(t *testing.T) {
	texts := make([]cadsource.Text, 0, 250)
	for i := 0; i < 250; i++ {
		// Growing vertical gaps defeat the line-pitch scale estimate, so the
		// sheet takes the whole-sheet fallback.
		texts = append(texts, cadTestText("LINE", "s1", float64(i), math.Pow(2, float64(i%40)), 0))
	}
	drawing := cadTestDrawing(
		[]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 1000, 600})},
		texts,
	)
	chunks, diagnostics := buildCADChunks(drawing, defaultCADChunkerConfig(), "fallback")
	if !cadHasDiagnostic(diagnostics, "cad_scale_unreliable") {
		t.Fatalf("diagnostics = %+v, want cad_scale_unreliable", diagnostics)
	}
	if len(chunks) < 2 {
		t.Fatalf("chunks = %d, want the fallback split by budget", len(chunks))
	}
	for _, chunk := range chunks {
		if lines := 1 + strings.Count(chunk.Content, "\n"); lines > defaultCADChunkerConfig().MaxLines {
			t.Fatalf("fallback chunk %s has %d lines", chunk.RegionID, lines)
		}
	}
	if len(cadTestTexts(chunks)) != len(texts) {
		t.Fatalf("fallback lost text: %d lines, want %d", len(cadTestTexts(chunks)), len(texts))
	}
}

func TestCADChunkerUnownedAttributes(t *testing.T) {
	drawing := cadTestDrawing(
		[]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 1000, 600})},
		[]cadsource.Text{
			cadTestText("45钢", "s1", 10, 500, 10),
			cadTestText("V-101", "s1", 20, 500, 10),
			cadTestText("DN100", "s1", 30, 500, 10),
		},
		cadsource.Attribute{Tag: "MATERIAL", Value: "45钢", OwnerHandle: 0},
		cadsource.Attribute{Tag: "DN", Value: "100", OwnerHandle: 0},
	)
	chunks, diagnostics := buildCADChunks(drawing, defaultCADChunkerConfig(), "unowned")
	if !cadHasDiagnostic(diagnostics, "cad_attrib_owner_missing") {
		t.Fatalf("diagnostics = %+v, want cad_attrib_owner_missing", diagnostics)
	}
	attributeChunks := 0
	for _, chunk := range chunks {
		if chunk.Kind != cadKindAttributes {
			continue
		}
		attributeChunks++
		if !strings.Contains(chunk.Context, "unowned") {
			t.Fatalf("attribute context = %q, want unowned", chunk.Context)
		}
		if len(chunk.Evidence) != 1 || chunk.Evidence[0].Level != cadEvidenceDerived {
			t.Fatalf("attribute evidence = %+v, want derived", chunk.Evidence)
		}
	}
	if attributeChunks != 1 {
		t.Fatalf("attribute chunks = %d, want 1", attributeChunks)
	}
}

func TestCADChunkerAttributeBudget(t *testing.T) {
	drawing := cadTestDrawing(
		[]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 1000, 600})},
		[]cadsource.Text{
			cadTestText("A", "s1", 10, 500, 10),
			cadTestText("B", "s1", 20, 500, 10),
			cadTestText("C", "s1", 30, 500, 10),
		},
	)
	for i := 0; i < 150; i++ {
		drawing.Attributes = append(drawing.Attributes, cadsource.Attribute{
			Tag: "TAG", Value: fmt.Sprintf("V%03d", i), OwnerHandle: 0,
		})
	}
	chunks, diagnostics := buildCADChunks(drawing, defaultCADChunkerConfig(), "attrs")
	attributeChunks := 0
	for _, chunk := range chunks {
		if chunk.Kind != cadKindAttributes {
			continue
		}
		attributeChunks++
		if lines := 1 + strings.Count(chunk.Content, "\n"); lines > defaultCADChunkerConfig().MaxLines {
			t.Fatalf("attribute chunk %s has %d lines", chunk.RegionID, lines)
		}
	}
	if attributeChunks < 2 {
		t.Fatalf("attribute chunks = %d, want the budget to split 150 lines", attributeChunks)
	}
	found := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "cad_attrib_owner_missing" {
			found = true
			if diagnostic.Count != 150 {
				t.Fatalf("owner-missing count = %d, want 150", diagnostic.Count)
			}
		}
	}
	if !found {
		t.Fatalf("diagnostics = %+v, want cad_attrib_owner_missing", diagnostics)
	}
}

func countCADBoxes(parts [][]cadTextBox) int {
	total := 0
	for _, part := range parts {
		total += len(part)
	}
	return total
}
