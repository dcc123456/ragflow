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

package admin

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/server"
	"ragflow/internal/service"
	"ragflow/internal/utility"

	"github.com/gin-gonic/gin"
)

func TestChatterHeartbeat(t *testing.T) {
	previousStore := GlobalServerStatusStore
	GlobalServerStatusStore = &ServerStatusStore{
		servers: make(map[string]*common.BaseMessage),
		CurrentLicense: &utility.LicenseV2{
			ValidFrom:  time.Now().Add(-time.Hour),
			ValidUntil: time.Now().Add(time.Hour),
		},
		LicenseCode: common.CodeLicenseValid,
	}
	t.Cleanup(func() { GlobalServerStatusStore = previousStore })

	router := gin.New()
	router.POST("/api/v1/admin/reports", (&Handler{service: &Service{}}).Reports)
	httpServer := httptest.NewServer(router)
	t.Cleanup(httpServer.Close)
	host, port, err := net.SplitHostPort(httpServer.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "service_conf.yaml")
	if err = os.WriteFile(configPath, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RAGFLOW_ADMIN_HOST", host)
	t.Setenv("RAGFLOW_ADMIN_HTTP_PORT", port)
	if err = server.Init(configPath); err != nil {
		t.Fatal(err)
	}

	utility.InitServiceQuota(common.ServerTypeChatter)

	client := service.NewAdminClient(common.ServerTypeChatter, "chatter-test", host, 0)
	if err = client.SendHeartbeat(); err != nil {
		t.Fatalf("send Chatter heartbeat: %v", err)
	}
	status, ok := GlobalServerStatusStore.GetStatus("chatter-test")
	if !ok || status.ServerType != common.ServerTypeChatter {
		t.Fatalf("stored Chatter status = %+v, present = %t", status, ok)
	}
	cpu, ok := status.Ext.(map[string]any)
	if !ok || cpu["core_num"] != float64(runtime.NumCPU()) {
		t.Fatalf("stored CPU status = %#v", status.Ext)
	}
}

func TestReportsPreservesHeartbeatResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tt := range []struct {
		name    string
		body    string
		status  int
		code    common.ErrorCode
		message string
		data    map[string]any
	}{
		{
			name:    "valid heartbeat retains nonzero code and data",
			body:    `{"report_type":"heartbeat","server_id":"reports-test","server_type":"api"}`,
			status:  http.StatusOK,
			code:    common.CodeLicenseValid,
			message: "License valid",
			data:    map[string]any{"cpu_num": float64(2), "license_type": "", "max_concurrency": float64(4), "features": map[string]any{"RBAC": true}},
		},
		{
			name:    "service error retains code and message",
			body:    `{"report_type":"heartbeat","server_id":"reports-over-quota","server_type":"api"}`,
			status:  http.StatusOK,
			code:    common.CodeSuccess,
			message: "API server count exceeds quota",
		},
		{
			name:    "unsupported report remains an HTTP error",
			body:    `{"report_type":"metric"}`,
			status:  http.StatusBadRequest,
			code:    common.CodeBadRequest,
			message: "Unsupported report type: metric",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			previousStore := GlobalServerStatusStore
			GlobalServerStatusStore = &ServerStatusStore{
				servers: map[string]*common.BaseMessage{
					"reports-test": {ServerType: common.ServerTypeAPI, Ext: map[string]any{}},
				},
				CurrentLicense: &utility.LicenseV2{
					ValidFrom:  time.Now().Add(-time.Hour),
					ValidUntil: time.Now().Add(time.Hour),
					APIQuota:   utility.APIQuota{Count: 1, CPUNum: 2, MaxConcurrency: 4},
					Features:   map[string]bool{"RBAC": true},
				},
				LicenseCode: common.CodeLicenseValid,
			}
			t.Cleanup(func() { GlobalServerStatusStore = previousStore })

			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/reports", strings.NewReader(tt.body))
			ctx.Request.Header.Set("Content-Type", "application/json")

			(&Handler{service: &Service{}}).Reports(ctx)

			var response struct {
				Code    common.ErrorCode `json:"code"`
				Message string           `json:"message"`
				Data    map[string]any   `json:"data"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if recorder.Code != tt.status || response.Code != tt.code || response.Message != tt.message || !reflect.DeepEqual(response.Data, tt.data) {
				t.Fatalf("status = %d, response = %s; want status %d, code %d, message %q, data %#v", recorder.Code, recorder.Body.String(), tt.status, tt.code, tt.message, tt.data)
			}
		})
	}
}
