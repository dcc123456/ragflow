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
	"strings"
	"testing"

	pdf "ragflow/internal/deepdoc/parser/pdf/type"
)

// TestConstructTableVH_Span verifies EE post-processing emits rowspan/colspan
// from the assembled cell's own ColSpan/RowSpan (mirroring tsr.py, which reads
// c['colspan']/c['rowspan'] per cell), with no dense-grid / CalSpans path.
func TestConstructTableVH_Span(t *testing.T) {
	cells := []pdf.TSRCell{
		{X0: 0, Y0: 0, X1: 20, Y1: 20, ColSpan: 2, RowSpan: 2, Text: "A"},
		{X0: 20, Y0: 0, X1: 40, Y1: 10, Text: "B"},
		{X0: 20, Y0: 10, X1: 40, Y1: 20, Text: "C"},
		{X0: 0, Y0: 20, X1: 20, Y1: 40, Text: "D"},
	}
	html := ConstructTableVH(cells, nil, "")
	if !strings.Contains(html, "rowspan=2") {
		t.Fatalf("expected rowspan=2 in EE HTML, got: %s", html)
	}
	if !strings.Contains(html, "colspan=2") {
		t.Fatalf("expected colspan=2 in EE HTML, got: %s", html)
	}
}

// TestConstructTableVH_NoSpuriousSpan is adversarial: plain cells must not emit
// any rowspan/colspan attributes.
func TestConstructTableVH_NoSpuriousSpan(t *testing.T) {
	cells := []pdf.TSRCell{
		{X0: 0, Y0: 0, X1: 10, Y1: 10, Text: "A"},
		{X0: 10, Y0: 0, X1: 20, Y1: 10, Text: "B"},
		{X0: 0, Y0: 10, X1: 10, Y1: 20, Text: "C"},
		{X0: 10, Y0: 10, X1: 20, Y1: 20, Text: "D"},
	}
	html := ConstructTableVH(cells, nil, "")
	if strings.Contains(html, "rowspan") || strings.Contains(html, "colspan") {
		t.Fatalf("plain cells must not emit span attributes, got: %s", html)
	}
}

// TestConstructTableVH_Overlay verifies the IoU>=0.85 text overlay matches a
// box to its cell the way tsr.py find_overlapped_with_threshold does.
func TestConstructTableVH_Overlay(t *testing.T) {
	cells := []pdf.TSRCell{
		{X0: 0, Y0: 0, X1: 100, Y1: 50, Text: ""},
		{X0: 0, Y0: 50, X1: 100, Y1: 100, Text: ""},
	}
	boxes := []pdf.TextBox{
		{X0: 2, Top: 2, X1: 98, Bottom: 48, Text: "head"},
		{X0: 5, Top: 55, X1: 95, Bottom: 95, Text: "body"},
	}
	html := ConstructTableVH(cells, boxes, "")
	if !strings.Contains(html, "head") || !strings.Contains(html, "body") {
		t.Fatalf("expected overlaid text in EE HTML, got: %s", html)
	}
}

// TestConstructTableVH_CoordinateSpace guards the regression where the EE
// V/H renderer received crop-space item.Cells but page-global text boxes,
// so the IoU>=0.85 overlay matched nothing and every cell rendered empty
// (the 82.7%-empty-cell report). The dispatch must map boxes into the crop
// frame (BoxToCropSpace) before overlay, so a far-from-origin table crop
// (large CropOff) still fills its cells.
func TestConstructTableVH_CoordinateSpace(t *testing.T) {
	// Crop-frame assembled cells (what the VH assembler emits).
	cells := []pdf.TSRCell{
		{X0: 0, Y0: 0, X1: 100, Y1: 50, Text: ""},
		{X0: 0, Y0: 50, X1: 100, Y1: 100, Text: ""},
	}
	item := &pdf.TableItem{
		RenderVH: true,
		Cells:    cells,
		Scale:    2.0,
		CropOffX: 1000,
		CropOffY: 2000,
	}
	// Page-global boxes whose crop-frame interiors are (2,2)-(98,48) and
	// (2,52)-(98,98): without the crop-space map they sit at page ~1000 and
	// match no crop-space cell, dropping all text.
	boxes := []pdf.TextBox{
		{X0: 501, Top: 1001, X1: 549, Bottom: 1024, Text: "alpha"},
		{X0: 501, Top: 1026, X1: 549, Bottom: 1049, Text: "beta"},
	}
	html := ConstructTable(cells, boxes, "", item)
	if !strings.Contains(html, "alpha") || !strings.Contains(html, "beta") {
		t.Fatalf("EE renderer dropped text across coordinate spaces; got: %s", html)
	}
}

// TestConstructTableVH_CrossPageUnifiedGrid guards the cross-page merge
// regression: a table merged across pages has item.Cells as a raw per-page
// concat in mixed crop frames (page2 cells NOT Y-shifted), which would
// scramble the ragged V/H renderer (columns doubled, rows interleaved).
// When MergeTablesAcrossPages marks the item GridUnified, ConstructTable
// must route to the grid path and render the unified, text-bearing
// item.Grid instead of item.Cells.
func TestConstructTableVH_CrossPageUnifiedGrid(t *testing.T) {
	item := &pdf.TableItem{
		RenderVH:    true,
		GridUnified: true,
		// Unified crop frame: page2 rows Y-shifted below page1.
		Grid: [][]pdf.TSRCell{
			{{X0: 0, Y0: 0, X1: 100, Y1: 20, Text: "alpha"}},
			{{X0: 0, Y0: 20, X1: 100, Y1: 40, Text: "beta"}},
			{{X0: 0, Y0: 200, X1: 100, Y1: 220, Text: "gamma"}},
			{{X0: 0, Y0: 220, X1: 100, Y1: 240, Text: "delta"}},
		},
		// Broken mixed-frame cells: page2 cells are NOT shifted, so if the
		// V/H path consumed them the two pages would collide and scramble.
		Cells: []pdf.TSRCell{
			{X0: 0, Y0: 0, X1: 100, Y1: 20},
			{X0: 0, Y0: 20, X1: 100, Y1: 40},
			{X0: 0, Y0: 0, X1: 100, Y1: 20},
			{X0: 0, Y0: 20, X1: 100, Y1: 40},
		},
	}
	html := ConstructTable(nil, nil, "", item)
	for _, want := range []string{"alpha", "beta", "gamma", "delta"} {
		if !strings.Contains(html, want) {
			t.Fatalf("merged unified grid path dropped %q; got: %s", want, html)
		}
	}
}

// TestConstructTableVH_NoPhantomColumn proves the streaming V/H renderer
// drops a spurious TSR-split phantom column instead of emitting an empty
// <td></td>. The grid has two x-bands (a stray vertical line split one
// logical column); the body value lands only in band A, band B is empty,
// and the header spans both. The dense OSS renderer would emit <td></td>
// for band B on every body row; streaming skips it (mirroring Python's
// construct_table, which never builds a fixed grid).
func TestConstructTableVH_NoPhantomColumn(t *testing.T) {
	grid := [][]pdf.TSRCell{
		{
			{X0: 0, Y0: 0, X1: 200, Y1: 20, Text: "HDR", ColSpan: 2, RowSpan: 1},
			{X0: 100, Y0: 0, X1: 200, Y1: 20}, // covered by HDR colspan
		},
		{
			{X0: 0, Y0: 20, X1: 100, Y1: 40, Text: "BODY"}, // band A body
			{X0: 100, Y0: 20, X1: 200, Y1: 40},             // band B = phantom (empty)
		},
	}
	item := &pdf.TableItem{RenderVH: true, Grid: grid, GridUnified: true}
	html := ConstructTable(nil, nil, "", item)
	if strings.Contains(html, "<td></td>") {
		t.Fatalf("phantom empty <td></td> emitted: %s", html)
	}
	if !strings.Contains(html, "HDR") || !strings.Contains(html, "BODY") {
		t.Fatalf("missing content: %s", html)
	}
	if !strings.Contains(html, "colspan=2") {
		t.Fatalf("expected header colspan=2: %s", html)
	}
}
