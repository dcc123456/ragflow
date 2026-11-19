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
	"errors"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/engine/clickhouse"
	"ragflow/internal/entity"

	"github.com/gin-gonic/gin"
)

// TestIngestMonitorParamErrorsUseClientErrorCode pins that malformed caller
// input maps to a client error code, not a server failure. The adversarial
// sibling test below asserts a genuine store failure still maps to a server error.
func TestIngestMonitorParamErrorsUseClientErrorCode(t *testing.T) {
	super := true
	user := &entity.User{ID: "u-1", IsSuperuser: &super}
	const window = "from=2026-11-01T00:00:00Z&to=2026-11-19T00:00:00Z"

	cases := []struct {
		name   string
		query  string
		invoke func(*IngestMonitorHandler, *gin.Context)
	}{
		{"overview missing from", "to=2026-11-19T00:00:00Z", (*IngestMonitorHandler).Overview},
		{"overview malformed time", "from=bad&to=2026-11-19T00:00:00Z", (*IngestMonitorHandler).Overview},
		{"timeseries invalid time", "from=bad&to=2026-11-19T00:00:00Z", (*IngestMonitorHandler).Timeseries},
		{"running tasks invalid time", "from=bad&to=2026-11-19T00:00:00Z", (*IngestMonitorHandler).RunningTasks},
		{"breakdown invalid time", "from=bad&to=2026-11-19T00:00:00Z", (*IngestMonitorHandler).Breakdown},
		{"failrate invalid time", "from=bad&to=2026-11-19T00:00:00Z", (*IngestMonitorHandler).FailRate},
		{"running overview invalid time", "from=bad&to=2026-11-19T00:00:00Z", (*IngestMonitorHandler).RunningOverview},
		{"events invalid time", "from=bad&to=2026-11-19T00:00:00Z", (*IngestMonitorHandler).Events},
		{"timeline invalid time", "from=bad&to=2026-11-19T00:00:00Z", (*IngestMonitorHandler).Timeline},
		{"timeseries bad interval", window + "&interval=bogus", (*IngestMonitorHandler).Timeseries},
		{"breakdown bad by", window + "&by=bogus", (*IngestMonitorHandler).Breakdown},
		{"failrate bad by", window + "&by=bogus", (*IngestMonitorHandler).FailRate},
		{"events bad sort", window + "&sort_by=bogus", (*IngestMonitorHandler).Events},
		{"timeline missing doc id", window, (*IngestMonitorHandler).Timeline},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeIngestMonitorQuerier{
				overview:    &clickhouse.MonitorOverview{},
				filters:     &clickhouse.FilterOptions{},
				runOverview: &clickhouse.RunningOverview{},
			}
			h := NewIngestMonitorHandlerWith(fake)
			c, rec := newMonitorCtx(user, "my-tenant", tc.query)
			tc.invoke(h, c)
			env := decodeEnvelope(t, rec)
			if env["code"] != float64(common.CodeParamError) {
				t.Errorf("code = %v, want %d (client param error), body=%s",
					env["code"], common.CodeParamError, rec.Body.String())
			}
		})
	}
}

func TestIngestMonitorStoreErrorRemainsServerError(t *testing.T) {
	super := true
	user := &entity.User{ID: "u-1", IsSuperuser: &super}
	fake := &fakeIngestMonitorQuerier{err: errors.New("clickhouse unavailable")}
	h := NewIngestMonitorHandlerWith(fake)
	c, rec := newMonitorCtx(user, "my-tenant", "from=2026-11-01T00:00:00Z&to=2026-11-19T00:00:00Z")
	h.Overview(c)
	env := decodeEnvelope(t, rec)
	if env["code"] != float64(common.CodeServerError) {
		t.Errorf("code = %v, want %d (server error), body=%s",
			env["code"], common.CodeServerError, rec.Body.String())
	}
}
