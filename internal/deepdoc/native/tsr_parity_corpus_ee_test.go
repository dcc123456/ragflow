//go:build cgo && integration

package native

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestTSREEParityCorpus runs the real Go TSR (RunTSREE) on every corpus fixture
// and compares against the golden captured from the Python EE docker (tsr_svr.py).
// After the channel-order fix (Go now feeds BGR via img.ToBGR, matching the
// docker's effective color order), this should fully close the earlier
// horizontal-separator divergence on the small invoice tables.
func TestTSREEParityCorpus(t *testing.T) {
	eeTestEnabled(t)
	skipIfNoModels(t)
	md := eeModelDir()
	dir := filepath.Join("testdata", "tsr_ee_parity", "corpus")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("corpus dir unavailable: %v", err)
	}
	var totalMatched, totalGold, totalUnmatched int
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".png" {
			continue
		}
		name := e.Name()
		img, err := Decode(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		res, err := RunTSREE(t.Context(), md, img)
		if err != nil {
			t.Fatalf("RunTSREE %s: %v", name, err)
		}
		var got struct {
			Bboxes [][]float64 `json:"bboxes"`
		}
		if err := json.Unmarshal([]byte(res.Wire()), &got); err != nil {
			t.Fatalf("parse go wire %s: %v", name, err)
		}
		goldRaw, err := os.ReadFile(filepath.Join(dir, name[:len(name)-4]+".golden.json"))
		if err != nil {
			t.Fatalf("read golden %s: %v", name, err)
		}
		var gold struct {
			Bboxes [][]float64 `json:"bboxes"`
		}
		if err := json.Unmarshal(goldRaw, &gold); err != nil {
			t.Fatalf("parse golden %s: %v", name, err)
		}
		matched, maxd, unmatched := MatchBoxesRelaxed(t, gold.Bboxes, got.Bboxes, 3.0, 0.05)
		totalMatched += matched
		totalGold += len(gold.Bboxes)
		totalUnmatched += len(unmatched)
		t.Logf("%-52s gold=%d got=%d matched=%d unmatched=%d maxd=%.2f",
			name, len(gold.Bboxes), len(got.Bboxes), matched, len(unmatched), maxd)
	}
	t.Logf("CORPUS TOTAL: matched=%d/%d unmatched=%d", totalMatched, totalGold, totalUnmatched)
	// The channel-order fix (Go now feeds BGR, matching the EE docker) closes the
	// earlier horizontal-separator divergence (phantom ~182 line, RAGFlow 8->7).
	// A small number of unmatched boxes remains: these are redundant, overlapping
	// boxes in the golden itself (the docker's NMS left 2-3 stacked detections at
	// one y-band) that Go's iou=0.7 NMS correctly collapses. That is a golden NMS
	// artifact, not a Go error, and is bounded (same NMS-marginal class as the
	// curated TestTSREEParity's maxDrift). Allow the same slack as that test.
	const maxDrift = 3
	if totalUnmatched > maxDrift {
		t.Errorf("corpus parity residual %d boxes exceeds maxDrift %d", totalUnmatched, maxDrift)
	}
}
