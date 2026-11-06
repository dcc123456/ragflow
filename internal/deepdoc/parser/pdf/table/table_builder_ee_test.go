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

package table

import (
	"context"
	"image"
	"testing"

	pdf "ragflow/internal/deepdoc/parser/pdf/type"
)

// fakeTSR is a test double for the EE TSR HTTP client surface that
// DeepDocVHTableBuilder depends on.
type fakeTSR struct {
	cells []pdf.TSRCell
	err   error
}

func (f fakeTSR) TSR(_ context.Context, _ image.Image) ([]pdf.TSRCell, error) {
	return f.cells, f.err
}

func testImg(h int) image.Image {
	return image.NewRGBA(image.Rect(0, 0, 600, h))
}

// spanBothWire mirrors the tsr_ee_span_both fixture: a 3x3 grid whose
// top-left cell merges 2x2, expressed as filled rects (the EE 2-class wire).
func spanBothWire() []pdf.TSRCell {
	b := func(x0, y0, x1, y1 float64) pdf.TSRCell {
		return pdf.TSRCell{X0: x0, Y0: y0, X1: x1, Y1: y1, Label: "v", Score: 0.9}
	}
	return []pdf.TSRCell{
		b(0, 0, 400, 240),     // top-left 2x2 merge
		b(400, 0, 600, 120),   // row0 col2
		b(400, 120, 600, 240), // row1 col2
		b(0, 240, 200, 360),   // row2 col0
		b(200, 240, 400, 360), // row2 col1
		b(400, 240, 600, 360), // row2 col2
	}
}

// TestDeepDocVHTableBuilder_DetectCells verifies the EE path turns 2-class
// wire rects into spanned cells via VHTableAssembler.Assemble.
func TestDeepDocVHTableBuilder_DetectCells(t *testing.T) {
	b := NewDeepDocVHTableBuilder(fakeTSR{cells: spanBothWire()})
	got, err := b.DetectCells(context.Background(), testImg(360))
	if err != nil {
		t.Fatalf("DetectCells err = %v", err)
	}
	if len(got) == 0 {
		t.Fatal("DetectCells returned no cells")
	}
	hasSpan := false
	for _, c := range got {
		if c.ColSpan > 1 || c.RowSpan > 1 {
			hasSpan = true
			break
		}
	}
	if !hasSpan {
		t.Fatal("expected VH Assemble to derive at least one spanned cell")
	}
}

// TestDeepDocVHTableBuilder_DetectCellsClientError is adversarial: a client
// error must propagate, not be swallowed into empty output.
func TestDeepDocVHTableBuilder_DetectCellsClientError(t *testing.T) {
	b := NewDeepDocVHTableBuilder(fakeTSR{err: context.Canceled})
	if _, err := b.DetectCells(context.Background(), testImg(360)); err == nil {
		t.Fatal("client TSR error must propagate")
	}
}

// TestDeepDocVHTableBuilder_GroupCells checks ExpandGrid is wired into
// GroupCells and yields a non-empty 2D grid.
func TestDeepDocVHTableBuilder_GroupCells(t *testing.T) {
	// A plain 2x2 grid (no merge) of filled rects.
	cells := []pdf.TSRCell{
		{X0: 0, Y0: 0, X1: 100, Y1: 100, Label: "v", Score: 0.9},
		{X0: 100, Y0: 0, X1: 200, Y1: 100, Label: "v", Score: 0.9},
		{X0: 0, Y0: 100, X1: 100, Y1: 200, Label: "v", Score: 0.9},
		{X0: 100, Y0: 100, X1: 200, Y1: 200, Label: "v", Score: 0.9},
	}
	b := NewDeepDocVHTableBuilder(fakeTSR{cells: cells})
	grid := b.GroupCells(cells)
	if len(grid) == 0 || len(grid[0]) == 0 {
		t.Fatal("GroupCells returned an empty grid")
	}
}
