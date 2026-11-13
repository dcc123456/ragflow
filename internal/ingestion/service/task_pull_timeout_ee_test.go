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

package service

import (
	"testing"
	"time"
)

// TestTaskPullRequestTimeoutCoversThreeLaneProbe guards against the
// three-lane starvation regression. PullMessages drains high -> medium ->
// default, and each lane waits up to its tier window (tierFetchWindow = 1s in
// the engine) before giving up. With the outer taskPullRequestTimeout bounded
// below the sum of the lane windows, an empty high lane consumes the whole
// budget and the lower lanes are never reached, so normal (medium/default)
// ingestion tasks are not pulled. The budget must therefore cover all three
// lanes. A wrong value (1s, or even 2s = high+medium) fails this test and the
// behavioural burst test.
func TestTaskPullRequestTimeoutCoversThreeLaneProbe(t *testing.T) {
	const minBudget = 3 * time.Second
	if taskPullRequestTimeout < minBudget {
		t.Fatalf("taskPullRequestTimeout = %v, want >= %v to cover the three-lane probe (each lane waits up to the 1s tier window)", taskPullRequestTimeout, minBudget)
	}
}
