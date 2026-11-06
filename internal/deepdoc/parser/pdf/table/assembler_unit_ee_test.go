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
	"fmt"
	"math"
	"testing"

	pdf "ragflow/internal/deepdoc/parser/pdf/type"
)

func mkCell(x0, y0, x1, y1, score float64) pdf.TSRCell {
	return pdf.TSRCell{X0: x0, Y0: y0, X1: x1, Y1: y1, Score: score}
}

func cellKey(c pdf.TSRCell) string {
	return fmt.Sprintf("%.2f,%.2f", math.Round(c.X0*100)/100, math.Round(c.Y0*100)/100)
}

func findCell(cells []pdf.TSRCell, x0, y0 float64) (pdf.TSRCell, bool) {
	k := fmt.Sprintf("%.2f,%.2f", math.Round(x0*100)/100, math.Round(y0*100)/100)
	for _, c := range cells {
		if cellKey(c) == k {
			return c, true
		}
	}
	return pdf.TSRCell{}, false
}

// TestVHAssembleRowSpan exercises the row-direction crossBoders branch
// (RowSpan > 1), which the parity golden never covers (0 rowspan cells).
// Layout: 2 columns x 3 rows, the left column's top two cells are merged
// into a single wire box spanning rows 0-1, so it must report RowSpan=2.
// Non-spanning cells report 0 (matching the Python tsr.py reference, where
// 0 means "single cell" and >=2 means a real span).
func TestVHAssembleRowSpan(t *testing.T) {
	wire := []pdf.TSRCell{
		mkCell(0, 0, 100, 100, 0.9),     // merged across rows 0-1, col 0
		mkCell(100, 0, 200, 50, 0.9),    // col 1, row 0
		mkCell(100, 50, 200, 100, 0.9),  // col 1, row 1
		mkCell(0, 100, 100, 150, 0.9),   // col 0, row 2
		mkCell(100, 100, 200, 150, 0.9), // col 1, row 2
	}
	got := VHTableAssembler{}.Assemble(wire, 150)
	if len(got) != 5 {
		t.Fatalf("rowspan: expected 5 cells, got %d: %+v", len(got), got)
	}
	merged, ok := findCell(got, 0, 0)
	if !ok {
		t.Fatalf("rowspan: merged cell @(0,0) missing: %+v", got)
	}
	if merged.RowSpan != 2 {
		t.Errorf("rowspan: merged cell RowSpan got %d want 2", merged.RowSpan)
	}
	if merged.ColSpan != 0 {
		t.Errorf("rowspan: merged cell ColSpan got %d want 0 (no column span)", merged.ColSpan)
	}
	for _, c := range got {
		if cellKey(c) == cellKey(merged) {
			continue
		}
		if c.RowSpan != 0 {
			t.Errorf("rowspan: cell @%s has RowSpan=%d want 0", cellKey(c), c.RowSpan)
		}
		if c.ColSpan != 0 {
			t.Errorf("rowspan: cell @%s has ColSpan=%d want 0", cellKey(c), c.ColSpan)
		}
	}
}

// TestVHAssembleColSpan exercises the column-direction crossBoders branch
// (ColSpan > 1). Layout: 3 columns x 2 rows, the top-left cell merges the
// first two columns, so it must report ColSpan=2; all other cells report 0.
func TestVHAssembleColSpan(t *testing.T) {
	wire := []pdf.TSRCell{
		mkCell(0, 0, 200, 50, 0.9),     // merged across cols 0-1, row 0
		mkCell(200, 0, 300, 50, 0.9),   // col 2, row 0
		mkCell(0, 50, 100, 100, 0.9),   // col 0, row 1
		mkCell(100, 50, 200, 100, 0.9), // col 1, row 1
		mkCell(200, 50, 300, 100, 0.9), // col 2, row 1
	}
	got := VHTableAssembler{}.Assemble(wire, 100)
	if len(got) != 5 {
		t.Fatalf("colspan: expected 5 cells, got %d: %+v", len(got), got)
	}
	merged, ok := findCell(got, 0, 0)
	if !ok {
		t.Fatalf("colspan: merged cell @(0,0) missing: %+v", got)
	}
	if merged.ColSpan != 2 {
		t.Errorf("colspan: merged cell ColSpan got %d want 2", merged.ColSpan)
	}
	if merged.RowSpan != 0 {
		t.Errorf("colspan: merged cell RowSpan got %d want 0 (no row span)", merged.RowSpan)
	}
	for _, c := range got {
		if cellKey(c) == cellKey(merged) {
			continue
		}
		if c.ColSpan != 0 {
			t.Errorf("colspan: cell @%s has ColSpan=%d want 0", cellKey(c), c.ColSpan)
		}
		if c.RowSpan != 0 {
			t.Errorf("colspan: cell @%s has RowSpan=%d want 0", cellKey(c), c.RowSpan)
		}
	}
}

// TestVHAssembleDegenerate covers the early-return and single-box paths:
// empty wire returns nil, a lone box returns one cell with spans 0x0.
func TestVHAssembleDegenerate(t *testing.T) {
	if got := (VHTableAssembler{}).Assemble(nil, 100); got != nil {
		t.Errorf("empty wire: expected nil, got %+v", got)
	}
	got := VHTableAssembler{}.Assemble([]pdf.TSRCell{mkCell(10, 20, 110, 70, 0.8)}, 100)
	if len(got) != 1 {
		t.Fatalf("single box: expected 1 cell, got %d: %+v", len(got), got)
	}
	if got[0].RowSpan != 0 || got[0].ColSpan != 0 {
		t.Errorf("single box: spans got %d/%d want 0/0", got[0].RowSpan, got[0].ColSpan)
	}
}
