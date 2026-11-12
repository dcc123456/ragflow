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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"

	"github.com/gin-gonic/gin"
)

func newAdminEETestEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("user_id", "system-admin")
		c.Next()
	})
	RegisterEERouter(engine.Group("/api/v1/admin"), NewRouter(NewHandler(NewService())))
	return engine
}

func TestAdminTenantList(t *testing.T) {
	db := testutil.SetupTestDB(t, &entity.Tenant{}, &entity.UserTenant{}, &entity.User{}, &entity.Role{}, &entity.PermissionChangeLog{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))

	status := "1"
	tenantName := "Acme"
	roleID := int32(7)
	if err := db.Create(&entity.Role{ID: roleID, RoleName: "system-role"}).Error; err != nil {
		t.Fatalf("create system role: %v", err)
	}
	if err := db.Create(&entity.Tenant{
		ID: "tenant-acme", Name: &tenantName, ParserIDs: "", RoleID: &roleID, Status: &status,
	}).Error; err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if err := db.Create(&entity.Tenant{
		ID: "tenant-other", ParserIDs: "", Status: &status,
	}).Error; err != nil {
		t.Fatalf("create second tenant: %v", err)
	}
	nickname := "Tenant Owner"
	user := entity.User{ID: "owner-user", Nickname: nickname, Email: "owner@example.com", IsAuthenticated: "1", IsActive: "1", IsAnonymous: "0", Status: &status}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create tenant owner: %v", err)
	}
	if err := db.Create(&entity.UserTenant{ID: "owner-membership", UserID: user.ID, TenantID: "tenant-acme", Role: "owner", Status: &status}).Error; err != nil {
		t.Fatalf("create owner membership: %v", err)
	}

	engine := newAdminEETestEngine()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants?page=1&page_size=10", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /admin/tenants status = %d, body = %s", response.Code, response.Body.String())
	}
	for _, expected := range []string{`"total":2`, `"tenant_id":"tenant-acme"`, `"owner_email":"owner@example.com"`, `"role_id":7`, `"role_name":"system-role"`} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("GET /admin/tenants response = %s, missing %s", response.Body.String(), expected)
		}
	}
}

func TestAdminPermissionLogListFilters(t *testing.T) {
	db := testutil.SetupTestDB(t, &entity.PermissionChangeLog{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))

	logs := []entity.PermissionChangeLog{
		{ID: "log-match", TenantID: "tenant-1", OperatorID: "membership-1", TargetType: "user", TargetID: "user-2", ResourceType: "dataset", ResourceID: "dataset-1", OldPermission: 1, NewPermission: 3, ActionType: "grant", EventType: "permission_changed", ScopeType: "tenant"},
		{ID: "log-other", TenantID: "tenant-2", OperatorID: "membership-2", TargetType: "group", TargetID: "group-2", ResourceType: "dataset", ResourceID: "dataset-2", OldPermission: 0, NewPermission: 1, ActionType: "grant", EventType: "permission_changed", ScopeType: "tenant"},
	}
	if err := db.Create(&logs).Error; err != nil {
		t.Fatalf("create permission logs: %v", err)
	}

	engine := newAdminEETestEngine()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/permission-logs?tenant_id=tenant-1&resource_type=dataset&page=1&page_size=10", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /admin/permission-logs status = %d, body = %s", response.Code, response.Body.String())
	}
	for _, expected := range []string{`"total":1`, `"id":"log-match"`, `"resource_id":"dataset-1"`} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("GET /admin/permission-logs response = %s, missing %s", response.Body.String(), expected)
		}
	}
	if strings.Contains(response.Body.String(), `"id":"log-other"`) {
		t.Fatalf("GET /admin/permission-logs returned a row outside the tenant filter: %s", response.Body.String())
	}
}

func TestAdminTenantListKeywordAndStatus(t *testing.T) {
	db := testutil.SetupTestDB(t, &entity.Tenant{}, &entity.UserTenant{}, &entity.User{}, &entity.Role{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))
	status := "1"
	name := "Searchable Tenant"
	if err := db.Create(&entity.Tenant{ID: "tenant-search", Name: &name, ParserIDs: "", Status: &status}).Error; err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	engine := newAdminEETestEngine()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants?keyword=Searchable&status=1", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"tenant_id":"tenant-search"`) {
		t.Fatalf("GET /admin/tenants keyword filter = (%d, %s)", response.Code, response.Body.String())
	}

}
