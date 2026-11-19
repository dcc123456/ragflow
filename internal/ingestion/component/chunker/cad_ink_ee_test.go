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

func cadTestInkPage(width, height int, rects [][4]int) *cadInkPage {
	ink := make([]bool, width*height)
	for _, rect := range rects {
		for y := rect[1]; y < rect[3]; y++ {
			if y < 0 || y >= height {
				continue
			}
			for x := rect[0]; x < rect[2]; x++ {
				if x < 0 || x >= width {
					continue
				}
				ink[y*width+x] = true
			}
		}
	}
	prefix := make([]uint32, (width+1)*(height+1))
	row := width + 1
	for y := 0; y < height; y++ {
		var rowSum uint32
		for x := 0; x < width; x++ {
			if ink[y*width+x] {
				rowSum++
			}
			prefix[(y+1)*row+x+1] = prefix[y*row+x+1] + rowSum
		}
	}
	return &cadInkPage{
		key:      cadInkKey{LayoutID: "model", SheetID: "s1"},
		viewport: [4]float64{0, 0, 100, 100},
		width:    width,
		height:   height,
		prefix:   prefix,
	}
}

func cadTestInkIndex(page *cadInkPage) *cadInkIndex {
	return &cadInkIndex{
		pages:        map[cadInkKey]*cadInkPage{page.key: page},
		modelBySheet: map[string]*cadInkPage{page.key.SheetID: page},
	}
}

func cadTestDrawingWithInk(texts []cadsource.Text, page *cadInkPage) (*cadsource.Drawing, *cadInkIndex) {
	drawing := cadTestDrawing(
		[]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 100, 100})},
		texts,
	)
	return drawing, cadTestInkIndex(page)
}

func TestCADInkSplitsAtBlankGutter(t *testing.T) {
	page := cadTestInkPage(100, 100, [][4]int{{10, 10, 40, 90}, {60, 10, 90, 90}})
	drawing, cfg := cadTestDrawingWithInk([]cadsource.Text{
		cadTestText("A", "s1", 12, 50, 5),
		cadTestText("B", "s1", 62, 50, 5),
	}, page)
	chunks, _ := buildCADChunks(drawing, cfg, "test")
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d, want 2: %+v", len(chunks), chunks)
	}
	if !strings.Contains(chunks[0].Content, "A") || strings.Contains(chunks[0].Content, "B") {
		t.Fatalf("first chunk = %q, want only A", chunks[0].Content)
	}
	if !strings.Contains(chunks[1].Content, "B") || strings.Contains(chunks[1].Content, "A") {
		t.Fatalf("second chunk = %q, want only B", chunks[1].Content)
	}
}

func TestCADInkDoesNotCutThroughStroke(t *testing.T) {
	page := cadTestInkPage(100, 100, [][4]int{{10, 10, 40, 90}, {40, 48, 60, 52}, {60, 10, 90, 90}})
	drawing, cfg := cadTestDrawingWithInk([]cadsource.Text{
		cadTestText("A", "s1", 12, 50, 5),
		cadTestText("B", "s1", 62, 50, 5),
	}, page)
	chunks, _ := buildCADChunks(drawing, cfg, "test")
	if len(chunks) != 1 {
		t.Fatalf("chunks = %d, want one block when ink crosses the gap: %+v", len(chunks), chunks)
	}
}

func TestCADInkNoGutterKeepsLargeBlock(t *testing.T) {
	page := cadTestInkPage(100, 100, [][4]int{{0, 0, 100, 100}})
	texts := make([]cadsource.Text, 0, 120)
	for i := 0; i < 120; i++ {
		texts = append(texts, cadTestText(fmt.Sprintf("L%03d", i), "s1", 10, float64(10+i%80), 2))
	}
	drawing, cfg := cadTestDrawingWithInk(texts, page)
	chunks, _ := buildCADChunks(drawing, cfg, "test")
	if len(chunks) != 1 {
		t.Fatalf("chunks = %d, want one block without a legal gutter: %+v", len(chunks), chunks)
	}
	if len(cadTestTexts(chunks)) != len(texts) {
		t.Fatalf("text lines = %d, want %d", len(cadTestTexts(chunks)), len(texts))
	}
}

func TestCADInkMergesOrphanLabel(t *testing.T) {
	page := cadTestInkPage(100, 100, [][4]int{{10, 10, 40, 90}, {70, 45, 73, 55}})
	texts := []cadsource.Text{
		cadTestText("A", "s1", 12, 80, 5),
		cadTestText("B", "s1", 12, 60, 5),
		cadTestText("C", "s1", 12, 40, 5),
		cadTestText("D", "s1", 12, 20, 5),
		cadTestText("STEAM", "s1", 70, 50, 5),
	}
	drawing, cfg := cadTestDrawingWithInk(texts, page)
	chunks, _ := buildCADChunks(drawing, cfg, "test")
	if len(chunks) != 1 {
		t.Fatalf("chunks = %d, want orphan label folded into the block: %+v", len(chunks), chunks)
	}
	if !strings.Contains(chunks[0].Content, "STEAM") {
		t.Fatalf("merged chunk lost label: %q", chunks[0].Content)
	}
}

func TestCADInkBoundsIncludeGraphics(t *testing.T) {
	page := cadTestInkPage(100, 100, [][4]int{{10, 10, 90, 90}})
	drawing, cfg := cadTestDrawingWithInk([]cadsource.Text{
		cadTestText("A", "s1", 50, 50, 5),
	}, page)
	chunks, _ := buildCADChunks(drawing, cfg, "test")
	if len(chunks) != 1 {
		t.Fatalf("chunks = %d, want 1", len(chunks))
	}
	b := chunks[0].Bounds
	if b[0] > 10 || b[1] > 10 || b[2] < 90 || b[3] < 90 {
		t.Fatalf("chunk bounds %v do not contain the block ink", b)
	}
}

func TestCADLocalTextBoundsKeepNotesAwayFromTitle(t *testing.T) {
	notes := []cadTextBox{
		cadLayoutTestBox("NOTE", 10, 20, 2),
		cadLayoutTestBox("KEEP CLEAR", 10, 16, 2),
	}
	title := []cadTextBox{cadLayoutTestBox("SCALE: 1:50", 80, 80, 2)}
	page := cadTestInkPage(100, 100, [][4]int{{5, 5, 95, 95}})
	noteBounds := cadLocalTextBounds(notes, page)
	titleBounds := cadLocalTextBounds(title, page)
	if noteBounds[2] >= titleBounds[0] || titleBounds[2] <= noteBounds[0] {
		t.Fatalf("local bounds overlap: notes=%v title=%v", noteBounds, titleBounds)
	}
	if noteBounds[2] > 30 || noteBounds[3] > 25 {
		t.Fatalf("notes bounds expanded into unrelated ink: %v", noteBounds)
	}
}

func TestCADInkUnavailableKeepsSheetWhole(t *testing.T) {
	drawing := cadTestDrawing(
		[]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 100, 100})},
		[]cadsource.Text{
			cadTestText("A", "s1", 10, 80, 5),
			cadTestText("B", "s1", 90, 20, 5),
		},
	)
	chunks, diagnostics := buildCADChunks(drawing, nil, "test")
	if len(chunks) != 1 {
		t.Fatalf("chunks = %d, want one block without ink: %+v", len(chunks), chunks)
	}
	if !cadHasDiagnostic(diagnostics, "cad_ink_unavailable") {
		t.Fatalf("diagnostics = %+v, want cad_ink_unavailable", diagnostics)
	}
}
