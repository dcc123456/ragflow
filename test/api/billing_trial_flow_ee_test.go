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
	"github.com/stripe/stripe-go/v87"
	"github.com/stripe/stripe-go/v87/setupintent"
	"testing"
	"time"
)

func TestTrial01aNoInvoiceDuringTrial(t *testing.T) {
	tenant := newTenant(t)
	customerID := tenant.ensureCustomer()
	iter := listCustomerSubscriptions(customerID)
	for iter.Next() {
		row := iter.Subscription()
		if row != nil && row.Status != stripe.SubscriptionStatusCanceled {
			t.Fatalf("trial tenant must not hold a live subscription, got %s", row.ID)
		}
	}
	if plan, _ := tenant.mustSubscription()["plan_name"].(string); plan != "Trial" {
		t.Fatalf("new tenant should be Trial, got %q", plan)
	}
}

func TestTrial01bNoInvoiceAfterStarterCycle(t *testing.T) {
	tenant := newTenant(t)
	tenant.attachTestCard()
	tenant.ensureSubscriptionAtPrice("Starter")
	tenant.waitForPlan("Starter", 150*time.Second)

	invoices := listCustomerInvoices(t, tenant.ensureCustomer())
	paid := 0
	for _, invoice := range invoices {
		if invoice.Status == "paid" && invoice.AmountPaid > 0 {
			paid++
		}
	}
	if paid != 1 {
		t.Fatalf("expected exactly one paid invoice for the first cycle, got %d", paid)
	}
}

// TestMember03RemoveToEnableAcceptance ports MEMBER-03: freeing a seat lets a
// previously rejected member join.

func TestTrial02FirstUpgradeViaSetupIntent(t *testing.T) {
	tenant := newTenant(t)
	tenant.ensureCustomer()
	data := tenant.post("/billing/setup-intents", map[string]interface{}{})
	clientSecret, _ := data["client_secret"].(string)
	setupID, _ := data["setup_intent_id"].(string)
	if clientSecret == "" || setupID == "" {
		t.Fatalf("setup intent not created: %v", data)
	}
	confirmed, err := setupintent.Confirm(setupID, &stripe.SetupIntentConfirmParams{
		PaymentMethod: stripe.String("pm_card_visa"),
		ReturnURL:     stripe.String(successURL),
	})
	if err != nil {
		t.Fatalf("confirm setup intent: %v", err)
	}
	if confirmed.Status != stripe.SetupIntentStatusSucceeded {
		t.Fatalf("setup intent status = %s", confirmed.Status)
	}
	result := tenant.post("/billing/subscription", map[string]interface{}{
		"payment_type":          "subscription",
		"subscription_price_id": planPrice(t, "Starter"),
		"setup_intent_id":       setupID,
		"session_success_url":   successURL,
		"session_cancel_url":    cancelURL,
	})
	if url, _ := result["redirect_to"].(string); url == "" {
		t.Fatalf("expected a checkout redirect, got %v", result)
	}
}

// TestFileUpload01HappyPathOnTrial ports FILE-UPLOAD-BILLING-01: a Trial tenant
// may upload within its storage quota.
