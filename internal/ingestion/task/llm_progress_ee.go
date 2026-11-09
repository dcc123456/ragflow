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

package task

import (
	"context"
	"fmt"
	"sync"
	"time"

	agentruntime "ragflow/internal/agent/runtime"
)

type kcLLMProgressReporter interface {
	begin(ctx context.Context) func()
	report(ctx context.Context, message string)
}

type activeKCLLMProgressReporter struct {
	mu     sync.Mutex
	active int
	cancel context.CancelFunc
}

func newKCLLMProgressReporter() kcLLMProgressReporter {
	return &activeKCLLMProgressReporter{}
}

func (r *activeKCLLMProgressReporter) begin(ctx context.Context) func() {
	if r == nil {
		return func() {}
	}

	r.mu.Lock()
	r.active++
	if r.cancel == nil {
		agentruntime.ReportProgressMessage(
			ctx,
			"Compiler",
			fmt.Sprintf("[INFO] Active LLM requests: %d", r.active),
		)
		reporterCtx, cancel := context.WithCancel(ctx)
		r.cancel = cancel
		go r.reportConcurrency(reporterCtx, ctx)
	}
	r.mu.Unlock()

	return func() {
		r.mu.Lock()
		if r.active > 0 {
			r.active--
		}
		r.mu.Unlock()
	}
}

func (r *activeKCLLMProgressReporter) report(ctx context.Context, message string) {
	agentruntime.ReportProgressMessage(ctx, "Compiler", message)
}

func (r *activeKCLLMProgressReporter) reportConcurrency(reporterCtx, progressCtx context.Context) {
	ticker := time.NewTicker(kcLLMConcurrencyReportInterval)
	defer ticker.Stop()

	for {
		select {
		case <-reporterCtx.Done():
			r.mu.Lock()
			r.cancel = nil
			r.mu.Unlock()
			return
		case <-ticker.C:
			r.mu.Lock()
			if r.active > 0 {
				r.report(progressCtx, fmt.Sprintf("[INFO] Active LLM requests: %d", r.active))
				r.mu.Unlock()
				continue
			}
			if r.cancel != nil {
				r.cancel()
				r.cancel = nil
			}
			r.report(progressCtx, "[INFO] Active LLM requests: 0")
			r.mu.Unlock()
			return
		}
	}
}

const kcLLMConcurrencyReportInterval = 15 * time.Second
