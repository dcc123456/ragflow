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
	"fmt"
	"strings"
)

// parseIngestorTier normalizes and validates an ingestor-tier CLI/env string
// for the full lane set. Accepted spellings are "", "low", "medium" and "high"
// (case-insensitive, whitespace-trimmed); empty/absent defaults to low. The
// removed "default" spelling and any unknown value are rejected rather than
// silently falling back to a lane, so a misconfiguration fails at startup.
func parseIngestorTier(s string) (Tier, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "low":
		return TierDefault, nil
	case "medium":
		return TierMedium, nil
	case "high":
		return TierHigh, nil
	default:
		return "", fmt.Errorf("invalid ingestor tier %q: expected low, medium or high", s)
	}
}
