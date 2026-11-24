//go:build cgo && ee && !cuda

package native

import "testing"

// TestModelFileNamingCPU locks the EE CPU weight-suffix convention: a CPU EE
// build must produce *.ee.cpu.ort. The expected suffix is written literally
// (not derived from eeModelSuffix) so the test genuinely locks the convention
// rather than mirroring the implementation.
func TestModelFileNamingCPU(t *testing.T) {
	for _, base := range []string{"det", "layout", "tsr", "rec"} {
		if got, want := eeModelFile(base), base+".ee.cpu.ort"; got != want {
			t.Fatalf("eeModelFile(%q) = %q, want %q", base, got, want)
		}
	}
	if got, want := detModel(), "det.ee.cpu.ort"; got != want {
		t.Fatalf("detModel() = %q, want %q", got, want)
	}
}
