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

package nats

import (
	"reflect"
	"testing"

	"ragflow/internal/common"
)

// TestIngestorLanesEE pins the enterprise ingestor-lane mapping. In priority
// (drain) order: a high ingestor consumes only [high]; a medium ingestor
// consumes [high, medium]; a low ingestor consumes all three lanes. Using
// reflect.DeepEqual locks the ORDER, not just the set, since drain priority is
// part of the contract.
func TestIngestorLanesEE(t *testing.T) {
	cases := []struct {
		tier    common.Tier
		want    []common.Tier
		wantErr bool
	}{
		{common.TierHigh, []common.Tier{common.TierHigh}, false},
		{common.TierMedium, []common.Tier{common.TierHigh, common.TierMedium}, false},
		{common.TierDefault, []common.Tier{common.TierHigh, common.TierMedium, common.TierDefault}, false},
		{common.Tier("bogus"), nil, true},
	}
	for _, c := range cases {
		got, err := ingestorLanes(c.tier)
		if c.wantErr {
			if err == nil {
				t.Errorf("ingestorLanes(%q): expected error, got lanes %v", c.tier, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ingestorLanes(%q): unexpected error: %v", c.tier, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("ingestorLanes(%q) = %v, want %v", c.tier, got, c.want)
		}
	}
}

// TestIngestorLanesEELowIncludesDefault is the adversarial fixture for the
// low-tier rule: a wrong implementation that maps low to only [high, medium]
// would satisfy a "low consumes high+medium" assertion but silently drop the
// default lane. This test fails unless low explicitly includes TierDefault.
func TestIngestorLanesEELowIncludesDefault(t *testing.T) {
	lanes, err := ingestorLanes(common.TierDefault)
	if err != nil {
		t.Fatalf("ingestorLanes(default): unexpected error: %v", err)
	}
	found := false
	for _, lane := range lanes {
		if lane == common.TierDefault {
			found = true
		}
	}
	if !found {
		t.Fatalf("ingestorLanes(default) = %v, want it to include TierDefault (low lane)", lanes)
	}
}

// TestIngestorLanesEEHighIsExclusive is the adversarial counterpart: the high
// tier must NOT include medium or default lanes, otherwise high ingestors would
// drain lower-priority work they are not allowed to consume.
func TestIngestorLanesEEHighIsExclusive(t *testing.T) {
	lanes, err := ingestorLanes(common.TierHigh)
	if err != nil {
		t.Fatalf("ingestorLanes(high): unexpected error: %v", err)
	}
	if !reflect.DeepEqual(lanes, []common.Tier{common.TierHigh}) {
		t.Fatalf("ingestorLanes(high) = %v, want exactly [high]", lanes)
	}
}
