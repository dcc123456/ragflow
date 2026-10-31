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
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/server/config"
	"ragflow/internal/service/billing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const webhookTestSecret = "whsec_test_secret"

func setupWebhookEngine(t *testing.T, webhookURL string) *gin.Engine {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&entity.BillingWebhookEvent{},
		&entity.BillingSubscription{},
		&entity.BillingPaymentOrder{},
		&entity.BillingPointAccount{},
		&entity.BillingPointLedger{},
		&entity.BillingPointHold{},
		&entity.SystemSettings{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create(&entity.SystemSettings{
		Name: "billing_webhook_secret", Source: "billing", DataType: "string", Value: webhookTestSecret,
	}).Error; err != nil {
		t.Fatalf("seed secret: %v", err)
	}
	previousDB := dao.DB
	t.Cleanup(func() { dao.DB = previousDB })
	dao.DB = db
	cfg := config.BillingConfig{
		Enabled:          true,
		WebhookURL:       webhookURL,
		StripeAPIVersion: "2026-04-22.dahlia",
		PlansByName: map[string]*config.BillingPlanInfo{
			"Trial": {Name: "Trial", Priority: 1, QuotaPoints: 10},
		},
	}
	service := billing.New(cfg, db)
	if err := service.Seed(context.Background()); err != nil {
		t.Fatalf("billing seed: %v", err)
	}
	t.Cleanup(billing.SetDefault(service))
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/api/v1/billing/webhooks/stripe", NewBillingHandler().Webhook)
	return engine
}

func signPayload(payload []byte, secret string) string {
	timestamp := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.%s", timestamp, payload)
	return fmt.Sprintf("t=%d,v1=%s", timestamp, hex.EncodeToString(mac.Sum(nil)))
}

func postWebhook(engine *gin.Engine, payload []byte, signature string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/billing/webhooks/stripe", bytes.NewReader(payload))
	if signature != "" {
		request.Header.Set("Stripe-Signature", signature)
	}
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

func webhookPayload(t *testing.T) []byte {
	t.Helper()
	payload, err := json.Marshal(map[string]interface{}{
		"id":          "evt_signed_" + fmt.Sprint(time.Now().UnixNano()),
		"object":      "event",
		"type":        "invoice.paid",
		"created":     time.Now().Unix(),
		"api_version": "2025-08-27.basil",
		"data": map[string]interface{}{"object": map[string]interface{}{
			"id": "in_signed", "customer": "cus_missing", "status": "paid", "paid": true,
		}},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return payload
}

func TestWebhookAcceptsConfiguredAPIVersion(t *testing.T) {
	engine := setupWebhookEngine(t, "https://billing.example.com/v1/billing/webhooks/stripe")
	payload := webhookPayload(t)
	response := postWebhook(engine, payload, signPayload(payload, webhookTestSecret))
	if response.Code != http.StatusOK {
		t.Fatalf("signed webhook status = %d body=%s", response.Code, response.Body.String())
	}
}

func TestWebhookRejectsInvalidSignature(t *testing.T) {
	engine := setupWebhookEngine(t, "https://billing.example.com/v1/billing/webhooks/stripe")
	payload := webhookPayload(t)
	response := postWebhook(engine, payload, "t=1,v1=deadbeef")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid signature status = %d", response.Code)
	}
}

func TestWebhookPublicURLWithLocalhostQueryStillVerifies(t *testing.T) {
	engine := setupWebhookEngine(t, "https://billing.example.com/v1/billing/webhooks/stripe?next=localhost")
	payload := webhookPayload(t)
	response := postWebhook(engine, payload, "t=1,v1=deadbeef")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("public url with localhost query must verify signatures, got %d", response.Code)
	}
}

func TestWebhookLocalForwardSkipsVerification(t *testing.T) {
	engine := setupWebhookEngine(t, "http://127.0.0.1/v1/billing/webhooks/stripe")
	payload := webhookPayload(t)
	response := postWebhook(engine, payload, "")
	if response.Code != http.StatusOK {
		t.Fatalf("local webhook status = %d body=%s", response.Code, response.Body.String())
	}
}
