//go:build cgo && ee && integration

package native

// GPU-device integration test: proves a CUDA EP session can be created and run
// from the Go backend when the binary is linked with the full CPU static lib
// and the two CUDA EP provider .so files sit next to the binary.
//
// This test is OFF by default (integration build tag + env gates) so the
// default unit run and CPU-only CI never need a GPU. To run on a GPU machine:
//
//   MODEL_DIR=/path/to/de-fused-ee-weights \
//   DEEPDOC_DEVICE=gpu RAGFLOW_GPU_TEST=1 \
//   go test -tags 'cgo static integration' -run TestGPUDevice -v ./internal/deepdoc/native/
//
// MODEL_DIR must hold the DE-FUSED .ort weights (ee_ort_regen/gpu_export):
// det/rec with Conv+HardSigmoid as separate primitives. The CPU weights (with
// com.microsoft.FusedConv) cannot run on the CUDA EP.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func requireGPUEnv(t *testing.T) string {
	t.Helper()
	if os.Getenv("RAGFLOW_GPU_TEST") != "1" {
		t.Skip("set RAGFLOW_GPU_TEST=1 to run GPU device tests (needs CUDA + provider .so)")
	}
	if os.Getenv("DEEPDOC_DEVICE") == "" || getDevice().UseGPU == false {
		// The test sets the device itself; require the opt-in marker only.
		t.Log("DEEPDOC_DEVICE not set in env; test will set device=gpu explicitly")
	}
	modelDir := os.Getenv("MODEL_DIR")
	if modelDir == "" {
		t.Skip("set MODEL_DIR to the de-fused GPU .ort weights")
	}
	if _, err := os.Stat(filepath.Join(modelDir, "det.ee.cpu.ort")); err != nil {
		t.Skipf("det.ee.cpu.ort not found in MODEL_DIR: %v", err)
	}
	return modelDir
}

// TestGPUDeviceSession creates a CUDA EP session for the det model and runs it
// once on a dummy image. It fails loudly if the binary was linked with the
// minimal CPU lib (which cannot load the CUDA EP) or if provider .so files are
// missing — no silent CPU fallback.
func TestGPUDeviceSession(t *testing.T) {
	modelDir := requireGPUEnv(t)

	orig := getDevice()
	defer SetDevice(orig)

	SetDevice(DeviceConfig{UseGPU: true, DeviceID: 0, EPLibraryPath: ""})
	if err := InitORT(); err != nil {
		t.Fatalf("InitORT: %v", err)
	}
	if err := RegisterExecutionProvider(); err != nil {
		t.Fatalf("RegisterExecutionProvider (CUDA EP .so) failed: %v", err)
	}

	// det model: input "x" shape [1,3,H,W], output [1,1,H,W].
	shape := []int64{1, 3, 640, 640}
	outLen := int(prod(shape))
	in := make([]float32, prod(shape))
	for i := range in {
		in[i] = 0.5
	}
	_ = outLen
	sess, err := NewSession(filepath.Join(modelDir, "det.ee.cpu.ort"), "x", shape, "sigmoid_0.tmp_0")
	if err != nil {
		t.Fatalf("NewSession on CUDA EP failed (full CPU lib + provider .so required): %v", err)
	}
	defer sess.Destroy()
	if sess.isPoisoned() {
		t.Fatal("session poisoned right after creation")
	}

	// Single Run on CUDA EP; a wrong shape or missing CUDA kernel fails here.
	if _, err := sess.Run(context.Background(), in); err != nil {
		t.Fatalf("CUDA EP Run failed: %v", err)
	}
	t.Log("CUDA EP session created and ran successfully")
}
