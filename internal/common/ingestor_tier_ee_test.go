//
//  Copyright (c) 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//
//  This file is part of the RAGFlow Enterprise Edition and is licensed,
//  not sold, under the RAGFlow Enterprise Commercial License Agreement.
//  Unauthorized copying, modification, distribution, or disclosure of this
//  file, via any medium, is strictly prohibited.
//
//  Portions of this file may incorporate code from RAGFlow Community Edition,
//  which is licensed under the Apache License, Version 2.0.
//
//  For commercial licensing inquiries: sales@infiniflow.ai
//

//go:build ee

package common

import "testing"

// TestParseIngestorTierEE pins the enterprise ingestor-tier parsing contract:
// the full high/medium/low set is accepted (unlike the single-lane build),
// empty/absent defaults to low, parsing is case-insensitive and trims spaces,
// and the removed "default" spelling plus any unknown value are rejected.
func TestParseIngestorTierEE(t *testing.T) {
	cases := []struct {
		in      string
		want    Tier
		wantErr bool
	}{
		{"", TierDefault, false},
		{"  ", TierDefault, false},
		{"low", TierDefault, false},
		{"LOW", TierDefault, false},
		{" Low ", TierDefault, false},
		{"medium", TierMedium, false},
		{"MEDIUM", TierMedium, false},
		{"  medium  ", TierMedium, false},
		{"high", TierHigh, false},
		{"HIGH", TierHigh, false},
		{" high ", TierHigh, false},
		{"default", "", true}, // removed spelling, must be rejected
		{"DEFAULT", "", true},
		{"bogus", "", true},
	}
	for _, c := range cases {
		got, err := parseIngestorTier(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseIngestorTier(%q): expected error, got tier %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseIngestorTier(%q): unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseIngestorTier(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestParseIngestorTierEEDistinguishesLanes is the adversarial fixture that
// makes a single-lane stub falsifiable: a wrong implementation that collapses
// every accepted value to TierDefault (the other edition's behavior) still
// passes a happy-path low-only test, but fails here because high/medium must
// resolve to their distinct tier constants.
func TestParseIngestorTierEEDistinguishesLanes(t *testing.T) {
	high, errHigh := parseIngestorTier("high")
	medium, errMedium := parseIngestorTier("medium")
	low, errLow := parseIngestorTier("low")
	if errHigh != nil || errMedium != nil || errLow != nil {
		t.Fatalf("all three lanes must be accepted: high=%v medium=%v low=%v", errHigh, errMedium, errLow)
	}
	if high == medium || medium == low || high == low {
		t.Fatalf("high/medium/low must map to distinct tiers; got high=%q medium=%q low=%q", high, medium, low)
	}
	if high != TierHigh || medium != TierMedium || low != TierDefault {
		t.Fatalf("wrong tier mapping: high=%q medium=%q low=%q", high, medium, low)
	}
}
