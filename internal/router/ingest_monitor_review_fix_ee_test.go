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
	"os"
	"strings"
	"testing"
)

// TestIngestMonitorEEFilesHaveLicenseHeader pins that every source file added
// by the ingestion-monitor change carries the enterprise license header at
// the top of the file, ahead of the build constraint.
func TestIngestMonitorEEFilesHaveLicenseHeader(t *testing.T) {
	files := []string{
		"../engine/clickhouse/ingest_monitor_dto_ee_test.go",
		"../engine/clickhouse/ingest_monitor_ee.go",
		"../engine/clickhouse/ingest_monitor_ee_integration_test.go",
		"../engine/clickhouse/ingest_monitor_ee_test.go",
		"../engine/clickhouse/ingest_monitor_scope_ee_test.go",
		"../handler/ingest_monitor_ee.go",
		"../handler/ingest_monitor_ee_test.go",
		"ingest_monitor_ee_test.go",
		"ingest_monitor_review_fix_ee_test.go",
		"../engine/clickhouse/ingest_monitor_window_limit_ee_test.go",
		"../engine/clickhouse/ingest_monitor_failrate_sql_ee_test.go",
		"../engine/clickhouse/ingest_monitor_filter_scope_ee_integration_test.go",
		"../handler/ingest_monitor_param_error_ee_test.go",
	}
	const (
		copyrightMarker = "Copyright (c) 2026 InfiniFlow. All rights reserved."
		licenseMarker   = "RAGFlow Enterprise Commercial License Agreement"
	)
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: read file: %v", path, err)
			continue
		}
		lines := strings.Split(string(src), "\n")
		copyrightAt, licenseAt, buildTagAt := -1, -1, -1
		for i, line := range lines {
			if copyrightAt < 0 && strings.Contains(line, copyrightMarker) {
				copyrightAt = i
			}
			if licenseAt < 0 && strings.Contains(line, licenseMarker) {
				licenseAt = i
			}
			if buildTagAt < 0 && strings.HasPrefix(strings.TrimSpace(line), "//go:build") {
				buildTagAt = i
			}
		}
		// Adversarial: the license must be at the top, before the build tag —
		// a header appended at the bottom of the file must not pass.
		if copyrightAt < 0 || licenseAt < 0 {
			t.Errorf("%s: missing enterprise license header", path)
			continue
		}
		if buildTagAt >= 0 && (copyrightAt > buildTagAt || licenseAt > buildTagAt) {
			t.Errorf("%s: license header must appear before the //go:build directive", path)
		}
	}
}
