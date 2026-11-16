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
	"fmt"

	"ragflow/internal/common"
)

// tierOrder is the strict drain priority. PullMessage always tries high first,
// then medium, then the legacy default lane; a full default lane can never block
// a high publish.
var tierOrder = []common.Tier{common.TierHigh, common.TierMedium, common.TierDefault}

// tierStreams are the three physically isolated enterprise lanes. Each lane
// owns a single tier subject; the streams do not overlap, so a full default
// lane can never reject a high-priority publish. The default lane reuses the
// bare community/OSS subject "tasks.RAGFLOW" (TaskSubject): the open-source
// edition publishes here for its single queue, so an OSS split needs no rename
// and OSS and EE can share one NATS server. Every publisher resolves a tenant
// tier and publishes to the matching TaskSubjectHigh/Medium/Default via
// SubjectForTier.
var tierStreams = map[common.Tier]tierStreamSpec{
	common.TierHigh:    {"RAGFLOW_TASKS_HIGH", []string{common.TaskSubjectHigh}, "RAGFLOW_CONSUMER_HIGH"},
	common.TierMedium:  {"RAGFLOW_TASKS_MEDIUM", []string{common.TaskSubjectMedium}, "RAGFLOW_CONSUMER_MEDIUM"},
	common.TierDefault: {"RAGFLOW_TASKS", []string{common.TaskSubject}, "RAGFLOW_CONSUMER"},
}

// defaultTier is the lane surfaced through the legacy single-consumer "primary"
// handle (n.stream / n.consumer) that pre-tiering reads used. The enterprise
// build designates the medium lane as primary; the community build
// (tier_policy_community.go) uses the single default lane.
func defaultTier() common.Tier { return common.TierMedium }

// ingestorLanes returns the ordered subset of tierOrder an ingestor of the
// given tier is allowed to consume, in priority (drain) order. A high ingestor
// owns only the high lane; a medium ingestor drains high then medium; a low
// ingestor drains all three lanes. An unknown tier is an error rather than a
// silent fallback, so a misconfigured tier cannot consume unintended lanes.
func ingestorLanes(tier common.Tier) ([]common.Tier, error) {
	switch tier {
	case common.TierHigh:
		return []common.Tier{common.TierHigh}, nil
	case common.TierMedium:
		return []common.Tier{common.TierHigh, common.TierMedium}, nil
	case common.TierDefault:
		return []common.Tier{common.TierHigh, common.TierMedium, common.TierDefault}, nil
	default:
		return nil, fmt.Errorf("unknown ingestor tier %q", tier)
	}
}
