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
