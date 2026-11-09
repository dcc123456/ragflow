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
	"sync"

	agentruntime "ragflow/internal/agent/runtime"
)

func withKnowledgeCompileLLMProgress(
	ctx context.Context,
	report func(context.Context, string, string),
) (context.Context, func()) {
	llmCtx, cancel := context.WithCancel(ctx)
	var reportMu sync.Mutex
	progressCtx := agentruntime.WithProgressMessageCallback(llmCtx, func(component, message string) {
		reportMu.Lock()
		defer reportMu.Unlock()
		if llmCtx.Err() != nil {
			return
		}
		report(ctx, component, message)
	})

	return progressCtx, func() {
		cancel()
		reportMu.Lock()
		reportMu.Unlock()
		if ctx.Err() == nil {
			report(ctx, "Compiler", "[INFO] Active LLM requests: 0")
		}
	}
}
