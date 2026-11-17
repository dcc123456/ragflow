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

package handler

import (
	"context"
	"strings"
	"sync"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/deepdoc/client"
)

// eeDocAnalyzers contributes the EE HTTP DeepDoc backends. A backend is
// available only when its <METHOD>_URL is set AND the remote /health
// probe succeeds. Every configured backend is probed concurrently with its
// own short timeout, so a hanging backend cannot consume the budget of a
// healthy peer and cause it to be reported unavailable. The handler probes
// fresh on every request, so this runs on each call rather than being
// memoized. The backend list (id/urlEnv/label) is the single source of truth
// shared with the parser dispatch via common.EEDeepDocBackends.
func eeDocAnalyzers(ctx context.Context) []DocAnalyzerInfo {
	if ctx == nil {
		ctx = context.Background()
	}

	out := make([]DocAnalyzerInfo, len(common.EEDeepDocBackends))
	var wg sync.WaitGroup
	for i, m := range common.EEDeepDocBackends {
		url := strings.TrimSpace(common.GetEnv(m.URLEnv))
		out[i] = DocAnalyzerInfo{ID: m.ID, Kind: "ee", Label: m.Label}
		if url == "" {
			continue
		}
		wg.Add(1)
		go func(i int, url string) {
			defer wg.Done()
			probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			out[i].Available = client.New(url).HealthContext(probeCtx)
		}(i, url)
	}
	wg.Wait()
	return out
}
