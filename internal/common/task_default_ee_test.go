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

// TestDefaultTier pins the enterprise default ingestion lane. The admin pull
// endpoint and the memory wake-up fallback publish to SubjectForTier(
// DefaultTier()); the EE build must resolve those to the medium lane so the
// shared paths honour the contract ("admin uses the medium subject").
func TestDefaultTier(t *testing.T) {
	const want = TierMedium
	if got := DefaultTier(); got != want {
		t.Fatalf("DefaultTier() = %q, want %q", got, want)
	}
	subject, err := SubjectForTier(DefaultTier())
	if err != nil {
		t.Fatalf("SubjectForTier(DefaultTier()) returned error: %v", err)
	}
	if subject == "" {
		t.Fatalf("SubjectForTier(DefaultTier()) returned empty subject")
	}
	if subject != TaskSubjectMedium {
		t.Fatalf("DefaultTier() subject = %q, want %q", subject, TaskSubjectMedium)
	}
}

// TestEnterpriseTierConstants guards the three enterprise lane names and their
// subjects so a refactor cannot silently rename or drop a lane.
func TestEnterpriseTierConstants(t *testing.T) {
	if TierHigh != "high" || TierMedium != "medium" || TierDefault != "default" {
		t.Fatalf("tier constant values drifted: high=%q medium=%q default=%q", TierHigh, TierMedium, TierDefault)
	}
	if TaskSubjectHigh != "tasks.RAGFLOW.high" || TaskSubjectMedium != "tasks.RAGFLOW.medium" {
		t.Fatalf("tier subject constants drifted: high=%q medium=%q", TaskSubjectHigh, TaskSubjectMedium)
	}
}
