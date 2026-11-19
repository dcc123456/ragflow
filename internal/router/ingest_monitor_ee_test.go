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

package router

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestEERouterRegistersIngestMonitorRoutes pins that the monitoring endpoints
// are wired onto the authorised EE router group by RegisterEEAuthRouter under
// the /api/v1/admin/ingestion prefix.
func TestEERouterRegistersIngestMonitorRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	authorized := engine.Group("")
	RegisterEEAuthRouter(authorized, &Router{})

	want := map[string]bool{
		"GET /api/v1/admin/ingestion/overview":         false,
		"GET /api/v1/admin/ingestion/timeseries":       false,
		"GET /api/v1/admin/ingestion/filters":          false,
		"GET /api/v1/admin/ingestion/running/tasks":    false,
		"GET /api/v1/admin/ingestion/running/overview": false,
		"GET /api/v1/admin/ingestion/breakdown":        false,
		"GET /api/v1/admin/ingestion/fail-rate":        false,
		"GET /api/v1/admin/ingestion/events":           false,
		"GET /api/v1/admin/ingestion/timeline":         false,
	}
	for _, r := range engine.Routes() {
		key := r.Method + " " + r.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
		// Adversarial: the legacy /ingest-monitor prefix must be gone, not
		// merely shadowed by the new prefix.
		if strings.Contains(r.Path, "/api/v1/admin/ingestion-monitor/") {
			t.Errorf("legacy ingest-monitor route still registered: %s %s", r.Method, r.Path)
		}
	}
	for key, found := range want {
		if !found {
			t.Errorf("route not registered: %s", key)
		}
	}
}
