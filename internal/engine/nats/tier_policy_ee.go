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

import "ragflow/internal/common"

var tierOrder = []common.Tier{common.TierDefault}

var tierStreams = map[common.Tier]tierStreamSpec{
	common.TierDefault: {"RAGFLOW_TASKS", []string{common.TaskSubject}, "RAGFLOW_CONSUMER"},
}

func defaultTier() common.Tier { return common.TierDefault }
