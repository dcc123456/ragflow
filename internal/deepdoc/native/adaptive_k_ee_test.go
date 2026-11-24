//go:build cgo && ee

package native

import "testing"

func TestComputeAdaptiveK(t *testing.T) {
	// 12GB card, ~1.1GB weights, 0.90 ratio, 560MB/run -> should be ~16, not 8.
	k12 := computeAdaptiveK(12288, 1150, 0.90, 560, 2, 64)
	if k12 < 12 || k12 > 20 {
		t.Fatalf("12GB K = %d, expected ~12-20 (adaptive, larger than fixed 8)", k12)
	}
	// 24GB card should roughly double.
	k24 := computeAdaptiveK(24576, 1150, 0.90, 560, 2, 64)
	if k24 <= k12 {
		t.Fatalf("24GB K=%d should exceed 12GB K=%d", k24, k12)
	}
	// 8GB card should be smaller.
	k8 := computeAdaptiveK(8192, 1150, 0.90, 560, 2, 64)
	if k8 >= k12 {
		t.Fatalf("8GB K=%d should be < 12GB K=%d", k8, k12)
	}
	// cap respected.
	if got := computeAdaptiveK(1024*1024, 1150, 0.90, 560, 2, 64); got != 64 {
		t.Fatalf("huge card K=%d, expected cap 64", got)
	}
	// tiny card floors at min.
	if got := computeAdaptiveK(1024, 1150, 0.90, 560, 2, 64); got != 2 {
		t.Fatalf("tiny card K=%d, expected min 2", got)
	}
	t.Logf("adaptive K: 8GB=%d 12GB=%d 24GB=%d", k8, k12, k24)
}
