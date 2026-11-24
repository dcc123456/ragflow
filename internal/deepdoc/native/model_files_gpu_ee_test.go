//go:build cgo && ee && cuda

package native

import "testing"

// TestModelFileNamingGPU locks the EE GPU weight-suffix convention: a CUDA EE
// build must produce *.ee.gpu.ort. The expected suffix is written literally so
// the test locks the convention rather than mirroring the implementation.
func TestModelFileNamingGPU(t *testing.T) {
	for _, base := range []string{"det", "layout", "tsr", "rec"} {
		if got, want := eeModelFile(base), base+".ee.gpu.ort"; got != want {
			t.Fatalf("eeModelFile(%q) = %q, want %q", base, got, want)
		}
	}
	if got, want := detModel(), "det.ee.gpu.ort"; got != want {
		t.Fatalf("detModel() = %q, want %q", got, want)
	}
}
