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

import "fmt"

// subjectForTier maps an enterprise tier to its JetStream subject: high and
// medium get their own priority lanes, and the legacy default lane reuses the
// shared TaskSubject so an OSS split needs no rename. An unknown tier is an
// error, never a silent fallback to the default lane.
func subjectForTier(tier Tier) (string, error) {
	switch tier {
	case TierHigh:
		return TaskSubjectHigh, nil
	case TierMedium:
		return TaskSubjectMedium, nil
	case TierDefault:
		return TaskSubject, nil
	default:
		return "", fmt.Errorf("unknown task tier %q", tier)
	}
}
