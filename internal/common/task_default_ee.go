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

// Enterprise ingestion lanes. TierDefault is the shared, community-compatible
// lane (the bare tasks.RAGFLOW subject); TierHigh and TierMedium are the
// enterprise priority lanes. The community build (task_default_community.go)
// knows only TierDefault, so the OSS tree carries no high/medium lane names.
const (
	// TierHigh is the highest-priority enterprise ingestion lane.
	TierHigh Tier = "high"
	// TierMedium is the normal-paid-traffic enterprise ingestion lane.
	TierMedium Tier = "medium"
)

// TaskSubjectHigh / TaskSubjectMedium are the enterprise priority-lane subjects.
// TierDefault reuses the shared TaskSubject (tasks.RAGFLOW) so an OSS split
// needs no rename and OSS and EE can share one NATS server.
const (
	// TaskSubjectHigh is the JetStream subject of the high-priority lane.
	TaskSubjectHigh = "tasks.RAGFLOW.high"
	// TaskSubjectMedium is the JetStream subject of the medium-priority lane.
	TaskSubjectMedium = "tasks.RAGFLOW.medium"
)

// DefaultTier is the lane the shared admin / memory-wake-up paths publish to in
// the enterprise build: the medium lane (normal paid traffic). The community
// build (task_default_community.go) returns TierDefault for its single lane.
func DefaultTier() Tier {
	return TierMedium
}

// DefaultTierSubject is the JetStream subject of DefaultTier.
func DefaultTierSubject() string {
	return TaskSubjectMedium
}
