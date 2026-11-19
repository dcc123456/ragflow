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

package clickhouse

import (
	"testing"
	"time"
)

// TestParseMonitorWindow_AllowsExactly90Days pins the boundary: a window of
// exactly 90 days must be accepted (off-by-one guard).
func TestParseMonitorWindow_AllowsExactly90Days(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(90 * 24 * time.Hour)
	if _, _, err := ParseMonitorWindow(from.Format(time.RFC3339), to.Format(time.RFC3339)); err != nil {
		t.Fatalf("exactly 90 days should be allowed: %v", err)
	}
}

// TestParseMonitorWindow_RejectsOver90Days is the adversarial case: a window
// one second beyond the cap must be rejected.
func TestParseMonitorWindow_RejectsOver90Days(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(90*24*time.Hour + time.Second)
	if _, _, err := ParseMonitorWindow(from.Format(time.RFC3339), to.Format(time.RFC3339)); err == nil {
		t.Fatal("window longer than 90 days must be rejected")
	}
}

// TestParseMonitorWindow_RejectsOver90DaysAcrossTimezone guards the UTC-normalisation
// path: input offsets must not let a >90-day UTC span slip through.
func TestParseMonitorWindow_RejectsOver90DaysAcrossTimezone(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*3600)
	from := time.Date(2026, 1, 1, 8, 0, 0, 0, loc) // 2026-01-01T00:00:00Z
	to := time.Date(2026, 4, 1, 8, 0, 1, 0, loc)   // 2026-04-01T00:00:01Z, 90d+1s UTC
	if _, _, err := ParseMonitorWindow(from.Format(time.RFC3339), to.Format(time.RFC3339)); err == nil {
		t.Fatal("UTC span over 90 days must be rejected regardless of input timezone")
	}
}

func TestParseMonitorWindow_AllowsJustUnder90Days(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(90*24*time.Hour - time.Second)
	if _, _, err := ParseMonitorWindow(from.Format(time.RFC3339), to.Format(time.RFC3339)); err != nil {
		t.Fatalf("window just under 90 days should be allowed: %v", err)
	}
}
