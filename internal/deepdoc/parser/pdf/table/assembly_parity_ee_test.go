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
	"encoding/json"
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	pdf "ragflow/internal/deepdoc/parser/pdf/type"
)

type goldenCell struct {
	X0      float64 `json:"x0"`
	Top     float64 `json:"top"`
	X1      float64 `json:"x1"`
	Bottom  float64 `json:"bottom"`
	Score   float64 `json:"score"`
	ColSpan int     `json:"colspan"`
	RowSpan int     `json:"rowspan"`
}

// TestVHAssemblyParity proves the Go VHTableAssembler reproduces the
// 0.26.4 deepdoc/vision/tsr.py edge-clustering output for every corpus
// fixture, using the committed v/h wire golden as input and the
// committed vh_assembly.golden.json (produced by gen_vh_golden.py running the
// real 0.26.4 tsr.py) as the expected output. This is the offline,
// deterministic equivalence guarantee between Go EE and Python EE assembly.
func TestVHAssemblyParity(t *testing.T) {
	corpus := filepath.Join("..", "..", "..", "native", "testdata", "tsr_ee_parity", "corpus")
	goldRaw, err := os.ReadFile(filepath.Join(corpus, "vh_assembly.golden.json"))
	if err != nil {
		t.Skipf("corpus golden unavailable: %v", err)
	}
	var gold map[string][]goldenCell
	if err := json.Unmarshal(goldRaw, &gold); err != nil {
		t.Fatalf("parse corpus golden: %v", err)
	}
	var totalDrift float64
	for name, exp := range gold {
		wireRaw, err := os.ReadFile(filepath.Join(corpus, name+".golden.json"))
		if err != nil {
			t.Fatalf("read wire golden %s: %v", name, err)
		}
		var wire struct {
			Bboxes [][]float64 `json:"bboxes"`
		}
		if err := json.Unmarshal(wireRaw, &wire); err != nil {
			t.Fatalf("parse wire golden %s: %v", name, err)
		}
		cells := make([]pdf.TSRCell, 0, len(wire.Bboxes))
		for _, b := range wire.Bboxes {
			cells = append(cells, pdf.TSRCell{X0: b[0], Y0: b[1], X1: b[2], Y1: b[3], Score: b[4]})
		}
		f, err := os.Open(filepath.Join(corpus, name+".png"))
		if err != nil {
			t.Fatalf("open png %s: %v", name, err)
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatalf("decode png %s: %v", name, err)
		}
		imgH := float64(img.Bounds().Dy())
		got := VHTableAssembler{}.Assemble(cells, imgH)

		expCells := make([]pdf.TSRCell, len(exp))
		for i, e := range exp {
			expCells[i] = pdf.TSRCell{X0: e.X0, Y0: e.Top, X1: e.X1, Y1: e.Bottom, Score: e.Score, ColSpan: e.ColSpan, RowSpan: e.RowSpan}
		}

		// Match cells by rounded coordinate key instead of list index:
		// Python stores cells in its own internal order and Go sorts by
		// (Y0,X0); ties on shared coordinates make index alignment fragile.
		key := func(c pdf.TSRCell) string {
			return fmt.Sprintf("%.2f,%.2f", math.Round(c.X0*100)/100, math.Round(c.Y0*100)/100)
		}
		expByKey := map[string]pdf.TSRCell{}
		for _, e := range expCells {
			expByKey[key(e)] = e
		}
		if len(expByKey) != len(expCells) {
			t.Logf("%s: WARNING %d exp cells collapse to %d coord keys (ties)", name, len(expCells), len(expByKey))
		}

		var drift float64
		matched := 0
		for _, g := range got {
			e, ok := expByKey[key(g)]
			if !ok {
				t.Errorf("%s: got cell (%.2f,%.2f) has no matching golden cell", name, g.X0, g.Y0)
				continue
			}
			matched++
			d := math.Max(math.Max(absD(g.X0, e.X0), absD(g.Y0, e.Y0)), math.Max(absD(g.X1, e.X1), absD(g.Y1, e.Y1)))
			if d > drift {
				drift = d
			}
			if g.ColSpan != e.ColSpan || g.RowSpan != e.RowSpan {
				t.Errorf("%s cell@(%.2f,%.2f): span got=%d/%d exp=%d/%d", name, g.X0, g.Y0, g.ColSpan, g.RowSpan, e.ColSpan, e.RowSpan)
			}
			if absD(g.Score, e.Score) > 1e-3 {
				t.Errorf("%s cell@(%.2f,%.2f): score got=%v exp=%v", name, g.X0, g.Y0, g.Score, e.Score)
			}
		}
		if matched != len(expCells) {
			t.Errorf("%s: matched %d of %d exp cells", name, matched, len(expCells))
		}
		if drift > 1.0 {
			t.Errorf("%s: coord drift %.2f exceeds 1px", name, drift)
		}
		totalDrift += drift
		t.Logf("%s: %d cells drift=%.3f", name, len(got), drift)
	}
	t.Logf("TOTAL drift=%.3f", totalDrift)
}

func absD(a, b float64) float64 {
	return math.Abs(a - b)
}
