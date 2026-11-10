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

package knowledge_compile

import (
	"context"

	"go.uber.org/zap"

	agentruntime "ragflow/internal/agent/runtime"
	"ragflow/internal/common"
)

// SubmitCompilerLLMJobs tracks the first LLM fan-out while submitting jobs to
// the shared compiler pool. Later batches feed the task's periodic heartbeat.
func SubmitCompilerLLMJobs(ctx context.Context, jobs []CompilerJob) error {
	if len(jobs) == 0 {
		return nil
	}
	stats := compilerPool.Stats()
	limit := stats.DesiredWorkers
	if limit <= 0 {
		limit = 1
	}
	expectedCalls := len(jobs)
	if expectedCalls > limit {
		expectedCalls = limit
	}
	finishBatch := agentruntime.BeginIOProgressBatch(ctx, "Compiler", "LLM call", expectedCalls)
	defer finishBatch()
	return runCompilerJobs(ctx, jobs)
}

func withKnowledgeCompileLLMProgress(
	ctx context.Context,
	tenant, kb string,
) (context.Context, func()) {
	messageCallback := func(component, message string) {
		common.Info("component progress detail",
			zap.String("component", component),
			zap.String("dataset_id", kb),
			zap.String("tenant_id", tenant),
			zap.String("message", message),
		)
	}
	ctx = agentruntime.WithProgressMessageCallback(ctx, messageCallback)
	return agentruntime.WithIOProgress(ctx)
}
