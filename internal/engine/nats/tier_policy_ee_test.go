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
	"testing"

	"ragflow/internal/common"
)

// TestEnterpriseTierPolicyHasThreeLanes guards the enterprise build's three
// physically isolated lanes (high/medium/default) and the medium "primary" tier
// used by legacy single-consumer reads. The community build
// (tier_policy_community test) asserts the opposite: exactly one lane. Keeping
// both guards prevents a future refactor from silently collapsing or expanding
// the lane set.
func TestEnterpriseTierPolicyHasThreeLanes(t *testing.T) {
	if len(tierStreams) != 3 {
		t.Fatalf("enterprise tierStreams has %d entries, want 3 (high/medium/default); got %v", len(tierStreams), tierStreams)
	}
	for _, tier := range []common.Tier{common.TierHigh, common.TierMedium, common.TierDefault} {
		if _, ok := tierStreams[tier]; !ok {
			t.Fatalf("enterprise tierStreams missing %q lane", tier)
		}
	}
	if defaultTier() != common.TierMedium {
		t.Fatalf("enterprise defaultTier() = %q, want TierMedium", defaultTier())
	}
}
