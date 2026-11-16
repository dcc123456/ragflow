//go:build ee

//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

package admin

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ragflow/internal/common"

	"github.com/gin-gonic/gin"
)

func TestAdminBaseActionAudit(t *testing.T) {
	h := setupAdminUserDetailTest(t)
	for _, tt := range []struct {
		method, route, path, body, action string
		handler                           gin.HandlerFunc
		resource, value                   string
		success                           bool
	}{
		{"POST", "/logout", "/logout", "", "logout_admin", h.Logout, "", "", false},
		{"POST", "/users", "/users", `{"username":"invalid","password":"private-secret"}`, "create_admin_user", h.CreateUser, "target_username", "invalid", false},
		{"DELETE", "/users/:username", "/users/user@example.com", "", "delete_admin_user", h.DeleteUser, "target_username", "user@example.com", false},
		{"PUT", "/users/:username/password", "/users/user@example.com/password", `{"new_password":"private-secret",`, "update_admin_user_password", h.ChangePassword, "target_username", "user@example.com", false},
		{"PUT", "/users/:username/activate", "/users/user@example.com/activate", `{"activate_status":"invalid"}`, "update_admin_user_status", h.UpdateUserActivateStatus, "target_username", "user@example.com", false},
		{"PUT", "/users/:username/admin", "/users/user@example.com/admin", "", "grant_admin", h.GrantAdmin, "target_username", "user@example.com", false},
		{"DELETE", "/users/:username/admin", "/users/user@example.com/admin", "", "revoke_admin", h.RevokeAdmin, "target_username", "user@example.com", false},
		{"POST", "/users/:username/tokens", "/users/missing@example.com/tokens", "", "create_admin_user_api_token", h.GenerateUserAPIToken, "target_username", "missing@example.com", false},
		{"DELETE", "/users/:username/tokens/:token", "/users/missing@example.com/tokens/private-secret", "", "delete_admin_user_api_token", h.DeleteUserAPIToken, "target_username", "missing@example.com", false},
		{"DELETE", "/services/:service_name", "/services/audit-service", "", "request_admin_service_shutdown", h.ShutdownService, "service_name", "audit-service", true},
		{"PUT", "/services/:service_name", "/services/audit-service", "", "request_admin_service_restart", h.RestartService, "service_name", "audit-service", true},
		{"POST", "/services/:service_name", "/services/audit-service", "", "request_admin_service_start", h.StartService, "service_name", "audit-service", true},
		{"PUT", "/variables", "/variables", `{"var_name":"audit-variable"}`, "update_admin_variable", h.SetVariable, "variable_name", "audit-variable", false},
		{"PUT", "/config/log", "/config/log", `{`, "update_admin_log_level", h.SetLogLevel, "", "", false},
		{"POST", "/queue/messages", "/queue/messages", `{"message":"private-secret",`, "request_admin_queue_publish", h.PublishMessageToQueue, "", "", false},
		{"PUT", "/queue/messages", "/queue/messages", `{"message_count":999999,"ack_policy":"ACK"}`, "request_admin_queue_pull", h.PullMessageFromQueue, "", "", false},
		{"DELETE", "/ingestors", "/ingestors", `{"ingestor_name":"audit-ingestor"}`, "request_admin_ingestor_shutdown", h.ShutdownIngestor, "ingestor_id", "audit-ingestor", true},
		{"POST", "/sandbox/config", "/sandbox/config", `{"provider_type":"unknown-audit-provider","config":{"api_key":"private-secret"}}`, "update_admin_sandbox_config", h.SetSandboxConfig, "provider_type", "unknown-audit-provider", false},
		{"POST", "/sandbox/test", "/sandbox/test", `{"provider_type":"unknown-audit-provider","config":{"api_key":"private-secret"}}`, "test_admin_sandbox_connection", h.TestSandboxConnection, "provider_type", "unknown-audit-provider", false},
	} {
		t.Run(tt.action, func(t *testing.T) {
			var events []*common.OperationLog
			common.SetAuditLogSink(func(event *common.OperationLog) error { events = append(events, event); return nil })
			t.Cleanup(func() { common.SetAuditLogSink(nil) })
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("user_id", "audit-admin")
				c.Set("tenant_id", "audit-tenant")
				c.Set("email", "user@example.com")
			})
			router.Handle(tt.method, "/api/v1/admin"+tt.route, tt.handler)
			req := httptest.NewRequest(tt.method, "/api/v1/admin"+tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			if len(events) != 1 {
				t.Fatalf("events = %d, response = %s", len(events), recorder.Body.String())
			}
			event := events[0]
			if event.Operation != tt.action || event.UserID != "audit-admin" || event.Resources["tenant_id"] != "audit-tenant" || event.APIPath != "/api/v1/admin"+tt.route || event.HTTPMethod != tt.method {
				t.Fatalf("unexpected event: %+v", event)
			}
			want := "response_error"
			if tt.success {
				want = "response_success"
			}
			if event.Resources["outcome"] != want || event.Resources["event_kind"] != "action_request" {
				t.Fatalf("resources = %+v, response = %s", event.Resources, recorder.Body.String())
			}
			if tt.resource != "" && event.Resources[tt.resource] != tt.value {
				t.Fatalf("missing selector %s: %+v", tt.resource, event.Resources)
			}
			encoded, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "private-secret") || event.RequestParams != "" || event.Error != nil {
				t.Fatalf("unsafe audit event: %s", encoded)
			}
		})
	}
}

func TestAdminBaseAuditExcludesAnonymousAndReads(t *testing.T) {
	h := NewHandler(NewService())
	var events []*common.OperationLog
	common.SetAuditLogSink(func(event *common.OperationLog) error { events = append(events, event); return nil })
	t.Cleanup(func() { common.SetAuditLogSink(nil) })
	for _, tt := range []struct {
		method, path, body string
		handler            gin.HandlerFunc
		actor              bool
	}{
		{http.MethodPost, "/api/v1/admin/users", "{", h.CreateUser, false},
		{http.MethodPost, "/api/v1/admin/login", "{", h.Login, true},
		{http.MethodPost, "/api/v1/admin/reports", "{", h.Reports, true},
		{http.MethodGet, "/api/v1/admin/auth", "", h.AuthCheck, true},
		{http.MethodGet, "/api/v1/admin/version", "", h.GetVersion, true},
	} {
		router := gin.New()
		if tt.actor {
			router.Use(func(c *gin.Context) { c.Set("user_id", "audit-admin") })
		}
		router.Handle(tt.method, tt.path, tt.handler)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)))
	}
	if len(events) != 0 {
		t.Fatalf("unexpected audit events: %+v", events)
	}
}

func TestAdminBaseAuditSuccessfulUserActions(t *testing.T) {
	h := setupAdminUserDetailTest(t)
	var events []*common.OperationLog
	common.SetAuditLogSink(func(event *common.OperationLog) error { events = append(events, event); return nil })
	t.Cleanup(func() { common.SetAuditLogSink(nil) })
	for _, tt := range []struct {
		method, route, path, body, action string
		handler                           gin.HandlerFunc
	}{
		{"PUT", "/users/:username/admin", "/users/user@example.com/admin", "", "grant_admin", h.GrantAdmin},
		{"DELETE", "/users/:username/admin", "/users/user@example.com/admin", "", "revoke_admin", h.RevokeAdmin},
		{"PUT", "/users/:username/activate", "/users/user@example.com/activate", `{"activate_status":"off"}`, "update_admin_user_status", h.UpdateUserActivateStatus},
	} {
		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set("user_id", "audit-admin")
			c.Set("tenant_id", "audit-tenant")
			c.Set("email", "admin@example.com")
		})
		router.Handle(tt.method, "/api/v1/admin"+tt.route, tt.handler)
		req := httptest.NewRequest(tt.method, "/api/v1/admin"+tt.path, strings.NewReader(tt.body))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		if len(events) != 1 || events[0].Operation != tt.action || events[0].Resources["outcome"] != "response_success" || events[0].Resources["target_username"] != "user@example.com" || events[0].UserID != "audit-admin" {
			t.Fatalf("events = %+v, response = %s", events, recorder.Body.String())
		}
		events = nil
	}
}

func TestAdminVariableAuditOmitsValue(t *testing.T) {
	h, _ := setupSetVariableHandler(t)
	var events []*common.OperationLog
	common.SetAuditLogSink(func(event *common.OperationLog) error { events = append(events, event); return nil })
	t.Cleanup(func() { common.SetAuditLogSink(nil) })
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("user_id", "audit-admin") })
	router.PUT("/api/v1/admin/variables", h.SetVariable)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/variables", strings.NewReader(`{"var_name":"audit-secret-setting","var_value":"private-secret"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if len(events) != 1 || events[0].Resources["outcome"] != "response_success" || events[0].Resources["variable_name"] != "audit-secret-setting" {
		t.Fatalf("events = %+v, response = %s", events, recorder.Body.String())
	}
	encoded, err := json.Marshal(events[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private-secret") {
		t.Fatalf("setting value leaked: %s", encoded)
	}
}
