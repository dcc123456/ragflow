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
	"strings"
	"testing"

	"ragflow/internal/parser/cadsource"
)

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
	chunks, _ := buildCADChunks(drawing, nil, "title")
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
	chunks, _ := buildCADChunks(drawing, nil, "notes")
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
	chunks, _ := buildCADChunks(drawing, nil, "notes")
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
	chunks, diagnostics := buildCADChunks(drawing, nil, "unowned")
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
	chunks, diagnostics := buildCADChunks(drawing, nil, "attrs")
	attributeChunks := 0
	for _, chunk := range chunks {
		if chunk.Kind != cadKindAttributes {
			continue
		}
		attributeChunks++
		if lines := 1 + strings.Count(chunk.Content, "\n"); lines > cadAttributeMaxLines {
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
