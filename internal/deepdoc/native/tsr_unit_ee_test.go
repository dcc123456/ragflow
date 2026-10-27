//go:build cgo

package native

// EE 2-class TSR postprocess / wire unit tests (model-free). These exercise the
// EE YOLOv8 separator decoder logic directly (tsrEEPostprocess, TSREEResult.Wire)
// so the EE-specific invariants — exactly the 2 separator classes v/h, the
// score-descending order, the un-letterbox remap, and bound clamping — are
// covered without the tsr.onnx weights. The end-to-end weight-accuracy checks
// (golden parity, session reuse, concurrency) live in tsr_ee_*_integration_test.

import (
	"testing"
)

// buildEEOut lays out a YOLOv8-style [1,6,8400] tensor with the given anchors
// set at the front (cx,cy,w,h at features 0..3, class probs at 4,5). Every
// unset anchor stays at 0 confidence (below tsrEEConfThr) so only the supplied
// anchors survive decoding.
func buildEEOut(anns []eeAnchor) []float32 {
	out := make([]float32, 6*tsrEECandidates)
	for _, a := range anns {
		out[0*tsrEECandidates+a.idx] = a.cx
		out[1*tsrEECandidates+a.idx] = a.cy
		out[2*tsrEECandidates+a.idx] = a.w
		out[3*tsrEECandidates+a.idx] = a.h
		out[4*tsrEECandidates+a.idx] = a.p0
		out[5*tsrEECandidates+a.idx] = a.p1
	}
	return out
}

type eeAnchor struct {
	idx          int
	cx, cy, w, h float32
	p0, p1       float32
}

// identityBox is a no-op letterbox (ratio 1, no padding) so model-space coords
// equal source-space coords, making the expected geometry easy to assert.
var identityBox = tsrEELetterbox{ratio: 1, dw: 0, dh: 0}

// TestTSREEPostprocessHappy decodes two separators (one v, one h) and verifies
// class selection, score, and the xywh->xyxy geometry — and that the result is
// sorted by score DESCENDING.
func TestTSREEPostprocessHappy(t *testing.T) {
	out := buildEEOut([]eeAnchor{
		{idx: 0, cx: 100, cy: 50, w: 40, h: 20, p0: 0.9, p1: 0.1},  // class 0 = v
		{idx: 1, cx: 200, cy: 80, w: 60, h: 30, p0: 0.1, p1: 0.95}, // class 1 = h
	})
	res := tsrEEPostprocess(out, identityBox)
	if len(res.Boxes) != 2 {
		t.Fatalf("boxes = %d, want 2", len(res.Boxes))
	}
	// Score-descending order: h (0.95) precedes v (0.9).
	h := res.Boxes[0]
	v := res.Boxes[1]
	if h.Label != "h" || v.Label != "v" {
		t.Fatalf("class order/label: got [%s,%s], want [h,v]", h.Label, v.Label)
	}
	if h.Score != 0.95 || v.Score != 0.9 {
		t.Fatalf("scores: got [%v,%v], want [0.95,0.9]", h.Score, v.Score)
	}
	// xywh (cx,cy,w,h) -> xyxy with identity letterbox.
	if h.X0 != 170 || h.Top != 65 || h.X1 != 230 || h.Bottom != 95 {
		t.Errorf("h geometry: got (%v,%v,%v,%v) want (170,65,230,95)", h.X0, h.Top, h.X1, h.Bottom)
	}
	if v.X0 != 80 || v.Top != 40 || v.X1 != 120 || v.Bottom != 60 {
		t.Errorf("v geometry: got (%v,%v,%v,%v) want (80,40,120,60)", v.X0, v.Top, v.X1, v.Bottom)
	}
}

// TestTSREEPostprocessBelowConf proves anchors under tsrEEConfThr are dropped,
// yielding an empty (not erroring) result — the no-separator page contract.
func TestTSREEPostprocessBelowConf(t *testing.T) {
	out := buildEEOut([]eeAnchor{
		{idx: 0, cx: 100, cy: 50, w: 40, h: 20, p0: 0.1, p1: 0.10}, // class1 wins but below 0.25
	})
	res := tsrEEPostprocess(out, identityBox)
	if len(res.Boxes) != 0 {
		t.Fatalf("boxes = %d, want 0 (all below conf threshold)", len(res.Boxes))
	}
}

// TestTSREEPostprocessClassSelection proves the higher of the two class
// probabilities wins (p1 > p0 => class 1 "h"), independent of absolute magnitude.
func TestTSREEPostprocessClassSelection(t *testing.T) {
	out := buildEEOut([]eeAnchor{
		{idx: 0, cx: 50, cy: 50, w: 10, h: 10, p0: 0.4, p1: 0.6}, // p1 higher => h
	})
	res := tsrEEPostprocess(out, identityBox)
	if len(res.Boxes) != 1 || res.Boxes[0].Label != "h" {
		t.Fatalf("class selection: got %+v, want single h", res.Boxes)
	}
}

// TestTSREEWire proves the EE wire format {"bboxes":[[x0,y0,x1,y1,score,cls]...]}
// with cls in {0,1} and the Top/Bottom -> y0/y1 remap.
func TestTSREEWire(t *testing.T) {
	r := TSREEResult{Boxes: []TSREEBox{{Label: "v", Score: 0.7, X0: 1.2, X1: 9.8, Top: 3.4, Bottom: 5.6}}, W: 1000, H: 1000}
	// "v" -> class 0; within bounds so clamp is a no-op.
	want := `{"bboxes":[[1.2,3.4,9.8,5.6,0.7,0]]}`
	if got := r.Wire(); got != want {
		t.Fatalf("Wire() = %s, want %s", got, want)
	}
}

// TestTSREEWireClamps proves out-of-bounds boxes are clamped into [0,W]/[0,H].
func TestTSREEWireClamps(t *testing.T) {
	r := TSREEResult{Boxes: []TSREEBox{
		{Label: "h", Score: 0.7, X0: -5, X1: 9.8, Top: 3.4, Bottom: 5000},
	}, W: 10, H: 10}
	want := `{"bboxes":[[0,3.4,9.8,10,0.7,1]]}`
	if got := r.Wire(); got != want {
		t.Fatalf("Wire() = %s, want %s", got, want)
	}
}
