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
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"
)

func TestAdminEnterpriseAuditRequests(t *testing.T) {
	db := testutil.SetupTestDB(t, &entity.Role{}, &entity.User{}, &entity.Tenant{}, &entity.SystemSettings{}, &entity.WhiteList{}, &entity.IngestionTask{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))
	gin.SetMode(gin.TestMode)
	h := NewHandler(NewService())
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set("user_id", "audit-admin"); c.Set("tenant_id", "actor-tenant") })
	RegisterEERouter(engine.Group("/api/v1/admin"), NewRouter(h))
	var logs []*common.OperationLog
	common.SetAuditLogSink(func(log *common.OperationLog) error { logs = append(logs, log); return nil })
	t.Cleanup(func() { common.SetAuditLogSink(nil) })
	for _, tc := range []struct{ method, path, action, body, outcome string }{
		{"POST", "/roles", "create_admin_role", "{", "response_error"},
		{"PUT", "/roles/missing", "update_admin_role", "{", "response_error"},
		{"DELETE", "/roles/missing", "delete_admin_role", "", "response_error"},
		{"POST", "/roles/missing/permission", "grant_admin_role_permission", "{", "response_error"},
		{"DELETE", "/roles/missing/permission", "revoke_admin_role_permission", "{", "response_error"},
		{"PATCH", "/roles/missing/default-models", "set_admin_role_default_model", "{", "response_error"},
		{"DELETE", "/roles/missing/default-models", "reset_admin_role_default_model", "{", "response_error"},
		{"PUT", "/providers", "create_admin_model_provider", "{", "response_error"},
		{"DELETE", "/providers/%20", "delete_admin_model_provider", "", "response_error"},
		{"POST", "/providers/missing/instances", "create_admin_model_instance", "{", "response_error"},
		{"DELETE", "/providers/missing/instances", "delete_admin_model_instance", "{", "response_error"},
		{"PUT", "/providers/missing/instances/missing", "update_admin_model_instance", "{", "response_error"},
		{"GET", "/providers/missing/instances/missing/connection", "test_admin_model_instance_connection", "", "response_error"},
		{"POST", "/providers/missing/connection", "test_admin_model_provider_connection", "{", "response_error"},
		{"PATCH", "/providers/missing/instances/missing/models/model", "update_admin_model", "{", "response_error"},
		{"POST", "/providers/missing/instances/missing/models", "create_admin_model", "{", "response_error"},
		{"DELETE", "/providers/missing/instances/missing/models", "delete_admin_models", "{", "response_error"},
		{"POST", "/system/license", "set_admin_license", "{", "response_error"},
		{"PUT", "/system/license/config", "update_admin_license_config", "{", "response_error"},
		{"POST", "/system/soft-fingerprint", "set_admin_soft_fingerprint", "{", "response_error"},
		{"DELETE", "/system/soft-fingerprint", "delete_admin_soft_fingerprint", "", "response_error"},
		{"GET", "/system/license?license=private-audit-sentinel", "test_admin_license", "", "response_error"},
		{"DELETE", "/ingestion/tasks", "request_admin_ingestion_tasks_remove", "", "response_success"},
		{"PUT", "/ingestion/tasks", "request_admin_ingestion_tasks_stop", "", "response_success"},
		{"POST", "/ingestion/tasks/missing/promote", "promote_admin_ingestion_task", "", "response_error"},
		{"DELETE", "/data/orphan", "request_admin_orphan_data_purge", `{"preview":true}`, "response_success"},
		{"DELETE", "/users/missing/data", "request_admin_user_data_purge", "{", "response_error"},
		{"DELETE", "/users/data", "request_admin_users_data_purge", `{"preview":true}`, "response_success"},
		{"POST", "/users/missing/keys", "create_admin_user_api_key", "", "response_error"},
		{"DELETE", "/users/missing/keys/private-audit-sentinel", "delete_admin_user_api_key", "", "response_error"},
		{"POST", "/sensitive-words", "upload_admin_sensitive_words", "{", "response_error"},
		{"POST", "/email/verification", "bind_admin_verification_email", `{"password":"private-audit-sentinel"}`, "response_error"},
		{"PUT", "/tenants/target-tenant/role", "update_admin_tenant_role", "{", "response_error"},
		{"PUT", "/tenants/target-tenant/status", "update_admin_tenant_status", "{", "response_error"},
		{"PUT", "/settings/default-tenant-role", "set_admin_default_tenant_role", "{", "response_error"},
		{"POST", "/whitelist/add", "add_admin_whitelist", "{", "response_error"},
		{"POST", "/whitelist/batch", "import_admin_whitelist", "{", "response_error"},
		{"PUT", "/whitelist/invalid", "update_admin_whitelist", `{"email":"private-audit-sentinel@example.com"}`, "response_error"},
		{"DELETE", "/whitelist/invalid", "delete_admin_whitelist", "", "response_success"},
		{"DELETE", "/whitelist/batch", "delete_admin_whitelist_batch", "{", "response_error"},
		{"POST", "/validate_mail", "test_admin_smtp_connection", `{"password":"private-audit-sentinel"}`, "response_error"},
	} {
		t.Run(tc.action, func(t *testing.T) {
			logs = nil
			req := httptest.NewRequest(tc.method, "/api/v1/admin"+tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, req)
			if len(logs) != 1 {
				t.Fatalf("got %d logs, response %s", len(logs), response.Body.String())
			}
			log := logs[0]
			if tc.action == "promote_admin_ingestion_task" && log.Resources["task_id"] != "missing" {
				t.Fatalf("task identifier lost: %+v", log.Resources)
			}
			if log.Operation != tc.action || log.HTTPMethod != tc.method || log.UserID != "audit-admin" || log.Resources["tenant_id"] != "actor-tenant" || log.Resources["outcome"] != tc.outcome {
				t.Fatalf("unexpected log: %+v resources=%+v", log, log.Resources)
			}
			serialized, err := json.Marshal(log)
			if err != nil {
				t.Fatal(err)
			}
			if log.RequestParams != "" || log.Error != nil {
				t.Fatalf("unexpected private request/error payload: %+v", log)
			}
			if strings.Contains(string(serialized), "private-audit-sentinel") || strings.Contains(log.APIPath, "?") {
				t.Fatalf("private input leaked: %s", serialized)
			}
			if strings.Contains(tc.path, "/tenants/") && log.Resources["target_tenant_id"] != "target-tenant" {
				t.Fatalf("target tenant lost: %+v", log.Resources)
			}
		})
	}
}

func TestAdminEnterpriseAuditWhitelistSuccessAndReads(t *testing.T) {
	setupWhitelistTest(t)
	h := NewHandler(NewService())
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set("user_id", "audit-admin"); c.Set("tenant_id", "actor-tenant") })
	RegisterEERouter(engine.Group("/api/v1/admin"), NewRouter(h))
	var logs []*common.OperationLog
	common.SetAuditLogSink(func(log *common.OperationLog) error { logs = append(logs, log); return nil })
	t.Cleanup(func() { common.SetAuditLogSink(nil) })
	callWhitelist(t, engine, whitelistJSON(http.MethodPost, "/add", `{"email":"audit@example.com"}`), http.StatusOK)
	if len(logs) != 1 || logs[0].Resources["outcome"] != "response_success" {
		t.Fatalf("success logs: %+v", logs)
	}
	logs = nil
	callWhitelist(t, engine, whitelistJSON(http.MethodGet, "", ""), http.StatusOK)
	if len(logs) != 0 {
		t.Fatalf("read produced %d logs", len(logs))
	}
}

func TestAdminEnterpriseAuditTenantRoleRetainsPermissionHistory(t *testing.T) {
	db := testutil.SetupTestDB(t, &entity.Tenant{}, &entity.Role{}, &entity.PermissionChangeLog{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))
	if err := db.Create(&entity.Tenant{ID: "managed-tenant", ParserIDs: ""}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.Role{ID: 7, RoleName: "selected-role"}).Error; err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set("user_id", "audit-admin"); c.Set("tenant_id", "actor-tenant") })
	RegisterEERouter(engine.Group("/api/v1/admin"), NewRouter(NewHandler(NewService())))
	var logs []*common.OperationLog
	common.SetAuditLogSink(func(log *common.OperationLog) error { logs = append(logs, log); return nil })
	t.Cleanup(func() { common.SetAuditLogSink(nil) })
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/tenants/managed-tenant/role", strings.NewReader(`{"role_id":7}`))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, req)
	if len(logs) != 1 || logs[0].Resources["outcome"] != "response_success" || logs[0].Resources["target_tenant_id"] != "managed-tenant" || logs[0].Resources["tenant_id"] != "actor-tenant" {
		t.Fatalf("logs=%+v response=%s", logs, response.Body.String())
	}
	var history []entity.PermissionChangeLog
	if err := db.Find(&history).Error; err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].EventType != "tenant.role.updated" || history[0].ActorUserID == nil || *history[0].ActorUserID != "audit-admin" {
		t.Fatalf("permission history=%+v", history)
	}
}

func TestAdminEnterpriseAuditExcludesAnonymousAndLicenseReads(t *testing.T) {
	var logs []*common.OperationLog
	common.SetAuditLogSink(func(log *common.OperationLog) error { logs = append(logs, log); return nil })
	t.Cleanup(func() { common.SetAuditLogSink(nil) })
	h := NewHandler(NewService())
	anonymous := gin.New()
	RegisterEERouter(anonymous.Group("/api/v1/admin"), NewRouter(h))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/roles", strings.NewReader("{"))
	request.Header.Set("Content-Type", "application/json")
	anonymous.ServeHTTP(httptest.NewRecorder(), request)
	authenticated := gin.New()
	authenticated.Use(func(c *gin.Context) { c.Set("user_id", "audit-admin") })
	RegisterEERouter(authenticated.Group("/api/v1/admin"), NewRouter(h))
	for _, query := range []string{"?check=invalid", "?check=invalid&license=private-audit-sentinel", "?check=1&license=private-audit-sentinel", "?check=t&license=private-audit-sentinel", "?check=T&license=private-audit-sentinel", "?check=true&license=private-audit-sentinel"} {
		authenticated.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/license"+query, nil))
	}
	if len(logs) != 0 {
		t.Fatalf("anonymous/read requests produced %d records", len(logs))
	}
}
