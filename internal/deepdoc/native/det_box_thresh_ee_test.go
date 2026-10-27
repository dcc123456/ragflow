//go:build cgo

package native

import (
	"os"
	"testing"
)

// TestDetBoxThresh_EE pins the EE det box-score threshold at 0.6, matching the
// Python EE docker: its OCREndpoint builds PaddleOCR(use_angle_cls=True,
// lang="ch") with defaults, and the effective postprocess params measured live
// in the deployed container are det_db_thresh=0.3, det_db_box_thresh=0.6,
// det_db_unclip_ratio=1.5, max_candidates=1000, min_size=3. Every other det
// parameter already matches the Go port; box_thresh was the only divergence
// (Go used the OSS 0.5), which over-detected low-score fragments (~21% extra
// boxes on real pages) versus the EE docker.
//
// The EE threshold is applied by InitCPUEE (the runtime seam override); this
// test activates it, asserts, and restores the OSS defaults so the rest of the
// package's tests are unaffected.
func TestDetBoxThresh_EE(t *testing.T) {
	if os.Getenv("RUN_EE_TESTS") == "" {
		t.Skip("set RUN_EE_TESTS=1 to run EE tests")
	}
	eeActivate(t)
	if detBoxThresh != 0.6 {
		t.Fatalf("EE detBoxThresh = %v, want 0.6 (Python EE docker PaddleOCR default det_db_box_thresh)", detBoxThresh)
	}
}
