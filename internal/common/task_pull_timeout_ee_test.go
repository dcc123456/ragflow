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

import (
	"testing"
	"time"
)

// TestHandlerPullTimeoutCoversThreeLaneProbe guards the admin manual-pull
// budget the same way TestTaskPullRequestTimeoutCoversThreeLaneProbe guards the
// worker budget: PullMessageFromQueue drains the three lanes under a single
// outer deadline, so the budget must cover all three lane windows or the lower
// lanes are starved. Under-budget values (1s, or 2s = high+medium) fail.
func TestHandlerPullTimeoutCoversThreeLaneProbe(t *testing.T) {
	const minBudget = 3 * time.Second
	if HandlerPullTimeout < minBudget {
		t.Fatalf("HandlerPullTimeout = %v, want >= %v to cover the three-lane probe", HandlerPullTimeout, minBudget)
	}
}
