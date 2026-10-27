//go:build cgo

package infnative

import (
	"testing"

	"ragflow/internal/deepdoc/native"
)

// TestTSRCellsFromBoxesEE_OSSBoxMapping proves the OSS 6-class RunTSR output
// (native.TSRBox: X0/X1/Top/Bottom/Label/Score) maps into the shared
// doctype.TSRCell contract with the geometry remap Top->Y0, Bottom->Y1, and
// that the EE override additionally carries the detection Score. This is a pure
// function so the mapping is unit-tested without ONNX Runtime or models (the
// real RunTSR path is covered by TestAnalyzerTSRGolden when MODEL_DIR is
// present).
func TestTSRCellsFromBoxesEE_OSSBoxMapping(t *testing.T) {
	boxes := []native.TSRBox{
		{Label: "table", Score: 0.91, X0: 10, X1: 90, Top: 5, Bottom: 50},
		{Label: "table column", Score: 0.87, X0: 12, X1: 88, Top: 8, Bottom: 48},
		{Label: "table row", Score: 0.83, X0: 14, X1: 86, Top: 9, Bottom: 47},
	}
	cells := tsrCellsFromBoxesEE(native.TSRResult{W: 100, H: 60, Boxes: boxes})
	if len(cells) != len(boxes) {
		t.Fatalf("cell count = %d, want %d", len(cells), len(boxes))
	}
	for i, b := range boxes {
		c := cells[i]
		if c.X0 != float64(b.X0) || c.X1 != float64(b.X1) {
			t.Errorf("box %d x coords: got (%v,%v) want (%v,%v)", i, c.X0, c.X1, b.X0, b.X1)
		}
		if c.Y0 != float64(b.Top) || c.Y1 != float64(b.Bottom) {
			t.Errorf("box %d y remap (Top->Y0, Bottom->Y1): got (%v,%v) want (%v,%v)", i, c.Y0, c.Y1, b.Top, b.Bottom)
		}
		if c.Label != b.Label {
			t.Errorf("box %d label: got %q want %q", i, c.Label, b.Label)
		}
		if c.Score != float64(b.Score) {
			t.Errorf("box %d score: got %v want %v", i, c.Score, b.Score)
		}
	}
}

// TestTSRCellsFromBoxesEE_Empty proves a TSR result with no boxes yields an empty
// (not nil-panicking, not erroring) cell slice — the no-table page contract.
func TestTSRCellsFromBoxesEE_Empty(t *testing.T) {
	if cells := tsrCellsFromBoxesEE(native.TSRResult{}); len(cells) != 0 {
		t.Fatalf("cells = %d, want 0", len(cells))
	}
}
