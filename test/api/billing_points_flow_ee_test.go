//go:build integration && ee

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

package api

import (
	"fmt"
	checkoutsession "github.com/stripe/stripe-go/v87/checkout/session"
	"strings"
	"testing"
	"time"
)

func TestPoint03InvalidInputsRejected(t *testing.T) {
	tenant := newTenant(t)
	before := tenant.get("/billing/points/balance")

	cases := []map[string]interface{}{
		{"quantity": 0, "session_success_url": successURL, "session_cancel_url": cancelURL},
		{"quantity": -1, "session_success_url": successURL, "session_cancel_url": cancelURL},
		{"quantity": 1.5, "session_success_url": successURL, "session_cancel_url": cancelURL},
		{"quantity": 1},
	}
	for _, body := range cases {
		code, message := tenant.postExpectError("/billing/points/checkout", body)
		if code == 0 {
			t.Fatalf("invalid input accepted: %v (%s)", body, message)
		}
	}
	after := tenant.get("/billing/points/balance")
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatalf("balance changed: before=%v after=%v", before, after)
	}
}

// TestPoint04CanceledCheckoutNoMutation ports POINT-04: creating a points
// checkout without paying must not credit points.

func TestPoint04CanceledCheckoutNoMutation(t *testing.T) {
	tenant := newTenant(t)
	data := tenant.post("/billing/points/checkout", map[string]interface{}{
		"quantity":            1,
		"session_success_url": successURL,
		"session_cancel_url":  cancelURL,
	})
	if url, _ := data["checkout_url"].(string); !strings.Contains(url, "checkout.stripe.com") {
		t.Fatalf("expected a checkout redirect, got %v", data)
	}
	beforeCount, beforeTotal := paidInvoiceSnapshot(t, tenant.ensureCustomer())
	if amount, ok := data["amount_cents"].(float64); ok && amount > 0 {
		t.Fatalf("points checkout should not report a settled amount before payment: %v", data)
	}
	sessionID, _ := data["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("points checkout returned no session id: %v", data)
	}
	session, err := checkoutsession.Get(sessionID, nil)
	if err != nil {
		t.Fatalf("get checkout session: %v", err)
	}
	unitAmount := int64(100) // points price is $1 per unit
	if session.AmountTotal != unitAmount {
		t.Fatalf("points session total = %d, want %d for one unit", session.AmountTotal, unitAmount)
	}
	if got := session.Metadata["points_amount"]; got != "100" {
		t.Fatalf("points session metadata points_amount = %q, want 100", got)
	}
	if got := session.Metadata["payment_type"]; got != "points_recharge" {
		t.Fatalf("points session metadata payment_type = %q", got)
	}
	balance := tenant.get("/billing/points/balance")
	addon, _ := balance["addon_points"].(map[string]interface{})
	if limit, _ := addon["limit"].(float64); limit != 0 {
		t.Fatalf("points were credited before payment: %v", addon)
	}
	if afterCount, afterTotal := paidInvoiceSnapshot(t, tenant.ensureCustomer()); afterCount != beforeCount || afterTotal != beforeTotal {
		t.Fatalf("abandoned points checkout charged the customer: before=%d/%d after=%d/%d", beforeCount, beforeTotal, afterCount, afterTotal)
	}
}

// TestPoint05WebhookReplayIdempotent ports POINT-05: replaying the same
// successful points session credits once.

func TestPoint05WebhookReplayIdempotent(t *testing.T) {
	tenant := newTenant(t)
	sessionID := fmt.Sprintf("cs_test_points_%d", time.Now().UnixNano())
	payload := pointsCheckoutEvent(tenant.tenantID, sessionID, 100)
	for attempt := 0; attempt < 2; attempt++ {
		eventID := fmt.Sprintf("evt_points_%d_%d", time.Now().UnixNano(), attempt)
		postRawWebhook(t, strings.Replace(payload, "__EVENT_ID__", eventID, 1))
	}
	balance := tenant.get("/billing/points/balance")
	addon, _ := balance["addon_points"].(map[string]interface{})
	if limit, _ := addon["limit"].(float64); limit != 100 {
		t.Fatalf("points should be credited exactly once, got %v", addon)
	}
}

// TestTrial01aNoInvoiceDuringTrial ports TRIAL-01a: a Trial tenant has no live
// Stripe subscription and therefore no invoice.
