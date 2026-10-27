//go:build cgo && integration

package native

// EE 2-class TSR integration tests. These live in their own file (per the
// migration plan) because the OSS 6-class TableStructureRecognizer (RunTSR) is
// retired dead code in EE — the EE deepdoc service's /predict/tsr endpoint
// serves only RunTSREE, the 2-class YOLOv8 separator model (tsr.ee.cpu.ort). The tests
// below exercise the EE path end-to-end against the real tsr.ee.cpu.ort weights:
//
//   - session-pool stability (reuse does not contaminate output),
//   - concurrency safety (parallel RunTSREE == serial baseline),
//   - the defining EE behavioral contract: exactly the 2 separator classes
//     (v=vertical, h=horizontal) with the known distribution 283/17 over 300
//     boxes, and
//   - the model snapshot lock so the OSS 6-class tsr.ort can never silently
//     replace the EE 2-class one and paint CI green.
//
// Coordinate accuracy vs the Python EE service is covered by
// tsr_parity_ee_test.go (TestTSREEParity); these tests assert EE-specific
// invariants that do NOT require the golden fixture's exact coordinates, so a
// model/export regression fails loud even if the box positions happen to look
// plausible.
//
// Requires EE_MODEL_DIR (//go:build integration); skips otherwise.

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"testing"
)

// eeTSRFixture is the tracked EE 2-class TSR fixture (a PNG so Go's stdlib png
// decoder and the EE service's cv2/libpng see bit-identical pixels — no JPG
// decode divergence). See tsr_parity_ee_test.go for the full rationale.
const (
	eeTSRDir = "testdata/tsr_ee_parity"
	eeTSRImg = "tsr_ee_table.png"
	eeTSGold = "tsr_ee_table.golden.json"
)

// decodeEETSRFixture decodes the shared EE 2-class TSR fixture image.
func decodeEETSRFixture(t *testing.T) *Image {
	t.Helper()
	img, err := Decode(filepath.Join(eeTSRDir, eeTSRImg))
	if err != nil {
		t.Fatalf("decode %s: %v", eeTSRImg, err)
	}
	return img
}

// TestTSREESessionReuse proves the shared fixed-shape model-session pool is
// stable across repeated RunTSREE calls on the same crop: two back-to-back runs
// must produce byte-identical Wire() output (no stale input tensor, no
// cross-call contamination), proving pooling is a no-behavior-change over the
// old per-call NewSession path for the EE TSR model.
func TestTSREESessionReuse(t *testing.T) {
	eeTestEnabled(t)
	skipIfNoModels(t)
	img := decodeEETSRFixture(t)

	r1, err := RunTSREE(t.Context(), eeModelDir(), img)
	if err != nil {
		t.Fatalf("RunTSREE #1: %v", err)
	}
	r2, err := RunTSREE(t.Context(), eeModelDir(), img)
	if err != nil {
		t.Fatalf("RunTSREE #2: %v", err)
	}
	if r1.Wire() != r2.Wire() {
		t.Fatalf("EE TSR output changed across pooled runs (session reuse not stable)")
	}
	t.Logf("EE TSR session reuse: 2 consecutive runs wire-identical (%d boxes)", len(r1.Boxes))
}

// TestTSREEConcurrencyConsistent drives RunTSREE once serially to establish a
// baseline wire, then 8 times concurrently from separate goroutines, and asserts
// every concurrent result is byte-identical to the serial baseline. This is the
// EE counterpart to TestInferenceConcurrencyConsistent: it proves the shared
// session pool is concurrency-safe for the EE TSR model (no data race, no
// cross-call tensor contamination under parallel load). It reuses the same
// runConcurrentMatchesSerial helper the OSS tasks use.
func TestTSREEConcurrencyConsistent(t *testing.T) {
	eeTestEnabled(t)
	skipIfNoModels(t)
	md := eeModelDir()
	img := decodeEETSRFixture(t)

	runConcurrentMatchesSerial(t, md, img, "TSR-EE",
		func(ctx context.Context, m string, im *Image) (string, error) {
			r, e := RunTSREE(ctx, m, im)
			if e != nil {
				return "", e
			}
			return r.Wire(), nil
		})
}

// eeTSRWire is the parsed EE TSR Wire() shape for assertions.
type eeTSRWire struct {
	Bboxes [][]float64 `json:"bboxes"`
}

// parseEETSRWire runs RunTSREE and parses its Wire() into the {bboxes:[...]}
// form, failing the test on any decode/parse error.
func parseEETSRWire(t *testing.T, md string, img *Image) eeTSRWire {
	t.Helper()
	res, err := RunTSREE(t.Context(), md, img)
	if err != nil {
		t.Fatalf("RunTSREE: %v", err)
	}
	var got eeTSRWire
	if err := json.Unmarshal([]byte(res.Wire()), &got); err != nil {
		t.Fatalf("parse Go wire: %v", err)
	}
	return got
}

// TestTSREEBehavioralContract is the defining EE 2-class invariant, checked
// WITHOUT the golden's exact coordinates:
//
//   - exactly 300 separators are emitted (ultralytics max_det=300 truncation),
//   - the two classes are v (0, vertical) and h (1, horizontal) ONLY — no other
//     class id may appear (this is what makes it "2-class", not the OSS 6-class
//     schema),
//   - class distribution is v=283 / h=17 (±2 slack; the 2 NMS-divergent boxes
//     are class-stable, so this is a hard signature of the EE weights),
//   - every box has a positive score, finite coordinates inside the image
//     bounds, and x0<=x1 / y0<=y1 (no degenerate or flipped geometry).
//
// A model/export regression (e.g. someone drops the OSS 6-class tsr.ort export
// back in, or the class head changes) breaks this contract even if box positions
// still look plausible — so it guards the EE-2-class property directly.
func TestTSREEBehavioralContract(t *testing.T) {
	eeTestEnabled(t)
	skipIfNoModels(t)
	md := eeModelDir()
	img := decodeEETSRFixture(t)
	got := parseEETSRWire(t, md, img)

	n := len(got.Bboxes)
	if n < 298 || n > 300 {
		t.Fatalf("EE TSR box count = %d, expected 298..300 (max_det=300 truncation)", n)
	}

	v, h := 0, 0
	boundsBad, flipped, scoreBad, nan := 0, 0, 0, 0
	for _, b := range got.Bboxes {
		if len(b) != 6 {
			t.Fatalf("box has %d fields, expected 6 [x0,y0,x1,y1,score,cls]: %v", len(b), b)
		}
		x0, y0, x1, y1, score, cls := b[0], b[1], b[2], b[3], b[4], b[5]
		if math.IsNaN(x0) || math.IsNaN(y0) || math.IsNaN(x1) || math.IsNaN(y1) ||
			math.IsNaN(score) || math.IsNaN(cls) {
			nan++
			continue
		}
		switch int(cls) {
		case 0:
			v++
		case 1:
			h++
		default:
			t.Errorf("EE TSR emitted class id %d — only 0 (v) / 1 (h) allowed (not 2-class)", int(cls))
		}
		if score <= 0 {
			scoreBad++
		}
		if x0 < -1 || y0 < -1 || x1 > float64(img.W)+1 || y1 > float64(img.H)+1 {
			boundsBad++
		}
		if x0 > x1 || y0 > y1 {
			flipped++
		}
	}
	if nan > 0 {
		t.Errorf("%d boxes had NaN fields", nan)
	}
	if scoreBad > 0 {
		t.Errorf("%d boxes had non-positive score", scoreBad)
	}
	if boundsBad > 0 {
		t.Errorf("%d boxes fell outside image bounds (W=%d H=%d)", boundsBad, img.W, img.H)
	}
	if flipped > 0 {
		t.Errorf("%d boxes had flipped geometry (x0>x1 or y0>y1)", flipped)
	}
	if v < 281 || v > 285 {
		t.Errorf("EE TSR vertical count = %d, expected ~283 (±2)", v)
	}
	if h < 15 || h > 19 {
		t.Errorf("EE TSR horizontal count = %d, expected ~17 (±2)", h)
	}
	t.Logf("EE TSR behavioral contract OK: %d boxes, v=%d h=%d (img %dx%d), all in-bounds/positive/non-flipped",
		n, v, h, img.W, img.H)
}
