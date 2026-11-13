//go:build ee

package common

import "testing"

// TestSubjectForTierEE resolves the three enterprise lanes: high, medium and the
// legacy default (low) lane. This guards the enterprise edition's three-tier
// mapping — a regression that drops a lane or mis-maps a subject would fail here.
func TestSubjectForTierEE(t *testing.T) {
	cases := []struct {
		tier    Tier
		want    string
		wantErr bool
	}{
		{TierHigh, TaskSubjectHigh, false},
		{TierMedium, TaskSubjectMedium, false},
		{TierDefault, TaskSubject, false},
	}
	for _, c := range cases {
		got, err := SubjectForTier(c.tier)
		if c.wantErr {
			if err == nil {
				t.Fatalf("SubjectForTier(%q): expected error, got %q", c.tier, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("SubjectForTier(%q): unexpected error: %v", c.tier, err)
		}
		if got != c.want {
			t.Fatalf("SubjectForTier(%q) = %q, want %q", c.tier, got, c.want)
		}
	}
}

// TestSubjectForTierEEUnknown is the adversarial case: an unknown tier must
// error rather than silently falling back to the default lane. Failing to do so
// would let a mis-tiered publish vanish into the wrong lane.
func TestSubjectForTierEEUnknown(t *testing.T) {
	_, err := SubjectForTier(Tier("bogus"))
	if err == nil {
		t.Fatalf("SubjectForTier(bogus): expected error, got nil")
	}
}
