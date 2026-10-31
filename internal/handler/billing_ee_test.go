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

//go:build ee

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/server/config"
	"ragflow/internal/service/billing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupBillingHandlerTest(t *testing.T) *gin.Engine {
	t.Helper()
	t.Setenv("DOWNGRADE_GUARD_ENABLED", "false")

	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&entity.BillingProduct{},
		&entity.BillingPricePoint{},
		&entity.BillingSubscription{},
		&entity.BillingPointAccount{},
		&entity.BillingPointLedger{},
		&entity.BillingPointHold{},
		&entity.BillingPaymentOrder{},
		&entity.BillingWebhookEvent{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, statement := range []string{
		"CREATE TABLE dialog (id TEXT PRIMARY KEY, tenant_id TEXT)",
		"CREATE TABLE knowledgebase (id TEXT PRIMARY KEY, tenant_id TEXT)",
		"CREATE TABLE user_canvas (id TEXT PRIMARY KEY, user_id TEXT)",
		"CREATE TABLE search (id TEXT PRIMARY KEY, tenant_id TEXT)",
		"CREATE TABLE memory (id TEXT PRIMARY KEY, tenant_id TEXT)",
		"CREATE TABLE user_tenant (id TEXT PRIMARY KEY, tenant_id TEXT, role TEXT, status TEXT)",
		"CREATE TABLE file (id TEXT PRIMARY KEY, tenant_id TEXT, type TEXT, size INTEGER)",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create usage table: %v", err)
		}
	}
	previousDB := dao.DB
	dao.DB = db
	t.Cleanup(func() { dao.DB = previousDB })

	cfg := config.BillingConfig{
		Enabled:          true,
		WebhookURL:       "http://127.0.0.1/v1/billing/webhooks/stripe",
		StripeAPIVersion: "2026-04-22.dahlia",
		PlansByName: map[string]*config.BillingPlanInfo{
			"Trial": {
				Name: "Trial", Priority: 1, QuotaApps: 5, QuotaMembers: 1,
				QuotaStorage: 100 * 1000 * 1000, QuotaPoints: 500, APIRequestLimitPerMinute: 10,
			},
		},
	}
	service := billing.New(cfg, db)
	if err := service.Seed(context.Background()); err != nil {
		t.Fatalf("billing seed: %v", err)
	}
	t.Cleanup(billing.SetDefault(service))

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("user", &entity.User{ID: "tenant-test", Email: "test@example.com"})
	})
	h := NewBillingHandler()
	group := engine.Group("/api/v1/billing")
	group.GET("/status", h.Status)
	group.GET("/plans", h.Plans)
	group.GET("/subscription", h.CurrentSubscription)
	group.GET("/subscription/overview", h.SubscriptionOverview)
	group.GET("/points/balance", h.PointsBalance)
	return engine
}

func doBillingRequest(t *testing.T, engine *gin.Engine, method, path string) (int, map[string]interface{}) {
	t.Helper()
	request := httptest.NewRequest(method, path, nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	payload := map[string]interface{}{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %s %s: %v body=%s", method, path, err, recorder.Body.String())
	}
	return recorder.Code, payload
}

func TestBillingStatusBareJSON(t *testing.T) {
	engine := setupBillingHandlerTest(t)
	status, payload := doBillingRequest(t, engine, http.MethodGet, "/api/v1/billing/status")
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	enabled, ok := payload["billing_enabled"].(bool)
	if !ok || !enabled {
		t.Fatalf("payload = %v", payload)
	}
	if _, wrapped := payload["code"]; wrapped {
		t.Fatalf("status must not use the code envelope: %v", payload)
	}
}

func TestBillingPlansReturnsCatalog(t *testing.T) {
	engine := setupBillingHandlerTest(t)
	status, payload := doBillingRequest(t, engine, http.MethodGet, "/api/v1/billing/plans")
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if code, _ := payload["code"].(float64); int(code) != 0 {
		t.Fatalf("plans code = %v payload=%v", payload["code"], payload)
	}
	if _, ok := payload["data"].([]interface{}); !ok {
		t.Fatalf("plans data must be an array: %v", payload)
	}
}

func TestBillingSubscriptionDefaultsToTrial(t *testing.T) {
	engine := setupBillingHandlerTest(t)
	status, payload := doBillingRequest(t, engine, http.MethodGet, "/api/v1/billing/subscription")
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if code, _ := payload["code"].(float64); int(code) != 0 {
		t.Fatalf("code = %v payload=%v", payload["code"], payload)
	}
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data = %v", payload["data"])
	}
	if data["plan_name"] != "Trial" {
		t.Fatalf("plan_name = %v", data["plan_name"])
	}
	if data["payment_state"] != "pending" {
		t.Fatalf("payment_state = %v", data["payment_state"])
	}
}

func TestBillingOverviewExposesResources(t *testing.T) {
	engine := setupBillingHandlerTest(t)
	status, payload := doBillingRequest(t, engine, http.MethodGet, "/api/v1/billing/subscription/overview")
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data = %v", payload["data"])
	}
	resources, ok := data["resources"].(map[string]interface{})
	if !ok {
		t.Fatalf("resources = %v", data["resources"])
	}
	apps, ok := resources["apps"].(map[string]interface{})
	if !ok || apps["limit"].(float64) != 5 {
		t.Fatalf("apps resource = %v", resources["apps"])
	}
	limits, ok := data["api_request_limits"].(map[string]interface{})
	if !ok || limits["requests_per_minute"].(float64) != 10 {
		t.Fatalf("api limits = %v", data["api_request_limits"])
	}
}

func TestBillingPointsBalanceDefaultsToPlanQuota(t *testing.T) {
	engine := setupBillingHandlerTest(t)
	status, payload := doBillingRequest(t, engine, http.MethodGet, "/api/v1/billing/points/balance")
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data = %v", payload["data"])
	}
	planPoints, ok := data["plan_points"].(map[string]interface{})
	if !ok || planPoints["limit"].(float64) != 500 {
		t.Fatalf("plan_points = %v", data["plan_points"])
	}
}
