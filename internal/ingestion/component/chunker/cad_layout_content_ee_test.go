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
	"strings"
	"testing"
)

func cadLayoutTestBox(text string, x, y, height float64) cadTextBox {
	return cadTextBoxFor(cadTextItem{text: text, x: x, y: y, height: height}, height)
}

func TestCADLayoutPairsColumnValues(t *testing.T) {
	boxes := []cadTextBox{
		cadLayoutTestBox("OFFICE 01", 10, 80, 5),
		cadLayoutTestBox("OFFICE 02", 60, 80, 5),
		cadLayoutTestBox("26.8 m2", 10, 70, 5),
		cadLayoutTestBox("31.4 m2", 60, 70, 5),
	}
	content := cadLayoutContent(boxes, 5)
	for _, want := range []string{"OFFICE 01: 26.8 m2", "OFFICE 02: 31.4 m2"} {
		if !strings.Contains(content, want) {
			t.Errorf("layout content %q missing %q", content, want)
		}
	}
}

func TestCADLayoutPairsTwoColumnTable(t *testing.T) {
	boxes := []cadTextBox{
		cadLayoutTestBox("Soggiorno-pranzo", 10, 80, 5),
		cadLayoutTestBox("27,72 m²", 200, 80, 5),
		cadLayoutTestBox("Cucina", 10, 70, 5),
		cadLayoutTestBox("15,46 m²", 200, 70, 5),
	}
	content := cadLayoutContent(boxes, 5)
	for _, want := range []string{"Soggiorno-pranzo: 27,72 m²", "Cucina: 15,46 m²"} {
		if !strings.Contains(content, want) {
			t.Errorf("layout content %q missing %q", content, want)
		}
	}
}

func TestCADLayoutGroupsProseColumns(t *testing.T) {
	boxes := []cadTextBox{
		cadLayoutTestBox("ARR. 1 SWSI FOR BELT DRIVE OR", 10, 100, 5),
		cadLayoutTestBox("DIRECT CONNECTION. IMPELLER OVERHUNG,", 10, 90, 5),
		cadLayoutTestBox("TWO BEARINGS ON BASE.", 10, 80, 5),
		cadLayoutTestBox("ARR. 2 SWSI FOR BELT DRIVE OR", 200, 100, 5),
		cadLayoutTestBox("DIRECT CONNECTION. IMPELLER OVERHUNG,", 200, 90, 5),
		cadLayoutTestBox("BEARINGS IN BRACKET SUPPORTED BY", 200, 80, 5),
	}
	content := cadLayoutContent(boxes, 5)
	first := strings.Index(content, "ARR. 1")
	second := strings.Index(content, "ARR. 2")
	body := strings.Index(content, "TWO BEARINGS ON BASE.")
	if first < 0 || second < 0 || body < 0 {
		t.Fatalf("layout content lost text: %q", content)
	}
	if !(first < body && body < second) {
		t.Fatalf("ARR.1 body was interleaved with ARR.2: %q", content)
	}
}

func TestCADLayoutGroupsDimensions(t *testing.T) {
	boxes := []cadTextBox{
		cadLayoutTestBox("11000", 10, 100, 5),
		cadLayoutTestBox("4000", 30, 100, 5),
		cadLayoutTestBox("2860", 50, 100, 5),
		cadLayoutTestBox("1400", 10, 90, 5),
		cadLayoutTestBox("2500", 30, 90, 5),
		cadLayoutTestBox("900", 50, 90, 5),
	}
	content := cadLayoutContent(boxes, 5)
	if !strings.Contains(content, "Dimensions:") || !strings.Contains(content, "11000, 4000, 2860") {
		t.Fatalf("dimension layout = %q", content)
	}
}

func TestCADLayoutFallbackPreservesText(t *testing.T) {
	boxes := []cadTextBox{
		cadLayoutTestBox("PUMP", 10, 80, 5),
		cadLayoutTestBox("147", 40, 70, 5),
		cadLayoutTestBox("NOTE", 70, 60, 5),
	}
	content := cadLayoutContent(boxes, 5)
	for _, want := range []string{"PUMP", "147", "NOTE"} {
		if !strings.Contains(content, want) {
			t.Errorf("layout content %q lost %q", content, want)
		}
	}
}

func TestCADLayoutPairsColumnValuesWithoutSharedBaseline(t *testing.T) {
	boxes := []cadTextBox{
		cadLayoutTestBox("Camera", 10, 100, 5),
		cadLayoutTestBox("S = 9,11 m²", 10, 85, 5),
		cadLayoutTestBox("Bagno", 200, 95, 5),
		cadLayoutTestBox("S = 7,89 m²", 200, 80, 5),
	}
	content := cadLayoutContent(boxes, 5)
	for _, want := range []string{"Camera: S = 9,11 m²", "Bagno: S = 7,89 m²"} {
		if !strings.Contains(content, want) {
			t.Errorf("layout content %q missing %q", content, want)
		}
	}
}

func TestCADLayoutDimensionsKeepSingleItemsAndLabels(t *testing.T) {
	boxes := []cadTextBox{
		cadLayoutTestBox("11000", 10, 100, 5),
		cadLayoutTestBox("4000", 30, 100, 5),
		cadLayoutTestBox("2860", 50, 100, 5),
		cadLayoutTestBox("950", 10, 90, 5),
		cadLayoutTestBox("TYP", 10, 80, 5),
	}
	content := cadLayoutContent(boxes, 5)
	for _, want := range []string{"Dimensions:", "11000, 4000, 2860", "950", "TYP"} {
		if !strings.Contains(content, want) {
			t.Errorf("dimension layout %q lost %q", content, want)
		}
	}
}

func TestCADLayoutColumnPairsDoNotScrambleProse(t *testing.T) {
	boxes := []cadTextBox{
		cadLayoutTestBox("ARR. 1 SWSI FOR BELT DRIVE", 10, 100, 5),
		cadLayoutTestBox("100", 10, 90, 5),
		cadLayoutTestBox("TWO BEARINGS ON BASE.", 10, 80, 5),
		cadLayoutTestBox("ARR. 2 SWSI FOR BELT DRIVE", 200, 100, 5),
		cadLayoutTestBox("200", 200, 90, 5),
		cadLayoutTestBox("BEARINGS IN BRACKET.", 200, 80, 5),
	}
	content := cadLayoutContent(boxes, 5)
	first := strings.Index(content, "ARR. 1")
	second := strings.Index(content, "ARR. 2")
	body := strings.Index(content, "TWO BEARINGS ON BASE.")
	if !(first >= 0 && first < body && body < second) {
		t.Fatalf("prose columns were scrambled by column-pair matching: %q", content)
	}
}
