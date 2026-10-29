//go:build cgo && ee

package native

import (
	"os"
	"testing"

	"ragflow/internal/common"
)

// CmpTolScoreDLA is a relaxed score tolerance used ONLY for the DLA (layout)
// equivalence row against the Python EE docker golden. It lives in an EE-only
// test helper (not the shared OSS golden.go) so the EE fork never forks that
// OSS file.
//
// The geometric gate stays strict: the caller still applies the coord tolerance,
// so any real coordinate drift, missing, or extra box is caught. Only the
// auxiliary confidence value is allowed to vary, because Go links ORT 1.29.0 and
// runs a pure-Go bilinearResize while the docker runs ORT 1.23.2 with cv2; on a
// couple of borderline detections the raw score wiggles by up to ~0.16 at
// identical coordinates — not a different region or a coordinate error. The 0.4
// encode gate in dlaPostprocess still trims low-confidence boxes identically;
// this tolerance just keeps the equivalence proof from being brittle to that
// cross-runtime score jitter.
const CmpTolScoreDLA = 0.20

// eeTestEnabled skips an EE-only test unless RUN_EE_TESTS=1 is set. EE tests
// need the EE weights/testdata, so they are opt-in even under the `integration`
// build tag; this keeps the default test pass (and OSS tests in this package)
// free of EE-specific global state and model loads.
func eeTestEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv("RUN_EE_TESTS") == "" {
		t.Skip("set RUN_EE_TESTS=1 to run EE tests")
	}
}

// eeModelDir returns the EE weight directory from EE_MODEL_DIR. EE tests use a
// dedicated variable so they never read the OSS MODEL_DIR (which holds the OSS
// *.ort weights); the two weight sets are different files by design. There is no
// fallback to MODEL_DIR — OSS and EE model directories are fully separate.
func eeModelDir() string {
	return os.Getenv("EE_MODEL_DIR")
}

// eeActivate switches the in-process DeepDoc backend to the EE weights and
// behaviour for the duration of t, restoring the OSS defaults afterwards so
// OSS tests in this package are unaffected. It is the runtime replacement for
// the old deepdoc_ee_cpu build-tag switch.
func eeActivate(t *testing.T) {
	t.Helper()
	eeTestEnabled(t)

	oldDet := modelFileDet
	oldLayout := modelFileLayout
	oldRec := modelFileRec
	oldThresh := detBoxThresh
	oldDLA := dlaPostprocessFn
	oldModels := common.DeepDocModelFiles
	t.Cleanup(func() {
		modelFileDet = oldDet
		modelFileLayout = oldLayout
		modelFileRec = oldRec
		detBoxThresh = oldThresh
		dlaPostprocessFn = oldDLA
		common.DeepDocModelFiles = oldModels
	})
	InitCPUEE()
}
