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

package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"ragflow/internal/entity"
	"ragflow/internal/server/config"

	"github.com/stripe/stripe-go/v87"
)

func newWebhookTestService(t *testing.T) *Service {
	t.Helper()
	service := newTestService(t)
	if service.cfg.PriceIDToPlan == nil {
		service.cfg.PriceIDToPlan = map[string]string{}
	}
	service.cfg.PlansByName["Starter"] = &config.BillingPlanInfo{
		Name: "Starter", Priority: 2, QuotaPoints: 100, QuotaApps: 10,
	}
	service.cfg.PriceIDToPlan["price_starter"] = "Starter"
	service.cfg.PriceIDToPlan["price_trial"] = "Trial"
	return service
}

func mustEvent(t *testing.T, id, eventType string, object interface{}) []byte {
	t.Helper()
	payload := map[string]interface{}{
		"id":      id,
		"type":    eventType,
		"created": time.Now().Unix(),
		"data":    map[string]interface{}{"object": object},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return raw
}

func TestResolvePlanName(t *testing.T) {
	service := newWebhookTestService(t)
	if name, ok := service.ResolvePlanName("price_starter"); !ok || name != "Starter" {
		t.Fatalf("resolve = %q %v", name, ok)
	}
	if _, ok := service.ResolvePlanName("price_unknown"); ok {
		t.Fatal("unknown price must not resolve")
	}
}

func TestSubscriptionUpdatedConvergesPlanAndResetsPoints(t *testing.T) {
	service := newWebhookTestService(t)
	ctx := context.Background()

	// Consume plan points first so the cycle reset is observable.
	if _, err := service.Hold(ctx, "tenant-w", "doc-w", 4, "hold:doc-w", nil); err != nil {
		t.Fatalf("hold: %v", err)
	}

	start := time.Now().Unix()
	end := start + 30*24*3600
	event := mustEvent(t, "evt_sub_1", EventSubscriptionUpdated, map[string]interface{}{
		"id":                   "sub_1",
		"customer":             "cus_1",
		"status":               "active",
		"current_period_start": start,
		"current_period_end":   end,
		"metadata":             map[string]string{"tenant_id": "tenant-w"},
		"items": map[string]interface{}{
			"data": []interface{}{
				map[string]interface{}{"id": "si_1", "quantity": 1, "price": map[string]string{"id": "price_starter"}},
			},
		},
	})
	if err := service.HandleWebhookEvent(ctx, event); err != nil {
		t.Fatalf("handle: %v", err)
	}
	subscription, err := service.Subscription(ctx, "tenant-w")
	if err != nil || subscription == nil {
		t.Fatalf("subscription: %v %v", subscription, err)
	}
	if subscription.PlanName != "Starter" || subscription.PriceID != "price_starter" || subscription.SubscriptionID != "sub_1" {
		t.Fatalf("subscription = %+v", subscription)
	}
	if subscription.StartTime == nil || subscription.StartTime.Unix() != start {
		t.Fatalf("start time = %v", subscription.StartTime)
	}
}

func TestSubscriptionDeletedFallsBackToTrial(t *testing.T) {
	service := newWebhookTestService(t)
	ctx := context.Background()

	update := mustEvent(t, "evt_sub_2", EventSubscriptionUpdated, map[string]interface{}{
		"id": "sub_2", "customer": "cus_2", "status": "active",
		"current_period_start": time.Now().Unix(), "current_period_end": time.Now().Unix() + 100,
		"metadata": map[string]string{"tenant_id": "tenant-x"},
		"items": map[string]interface{}{"data": []interface{}{
			map[string]interface{}{"id": "si_2", "quantity": 1, "price": map[string]string{"id": "price_starter"}},
		}},
	})
	if err := service.HandleWebhookEvent(ctx, update); err != nil {
		t.Fatalf("update: %v", err)
	}

	deleted := mustEvent(t, "evt_sub_2_del", EventSubscriptionDeleted, map[string]interface{}{
		"id": "sub_2", "customer": "cus_2", "status": "canceled",
		"metadata": map[string]string{"tenant_id": "tenant-x"},
	})
	if err := service.HandleWebhookEvent(ctx, deleted); err != nil {
		t.Fatalf("delete: %v", err)
	}
	subscription, _ := service.Subscription(ctx, "tenant-x")
	if subscription == nil || subscription.PlanName != "Trial" || subscription.SubscriptionStatus != "canceled" {
		t.Fatalf("subscription after delete = %+v", subscription)
	}
}

func TestInvoicePaidRecordsOrder(t *testing.T) {
	service := newWebhookTestService(t)
	ctx := context.Background()

	update := mustEvent(t, "evt_sub_3", EventSubscriptionUpdated, map[string]interface{}{
		"id": "sub_3", "customer": "cus_3", "status": "active",
		"current_period_start": time.Now().Unix(), "current_period_end": time.Now().Unix() + 100,
		"metadata": map[string]string{"tenant_id": "tenant-y"},
		"items": map[string]interface{}{"data": []interface{}{
			map[string]interface{}{"id": "si_3", "quantity": 1, "price": map[string]string{"id": "price_starter"}},
		}},
	})
	if err := service.HandleWebhookEvent(ctx, update); err != nil {
		t.Fatalf("update: %v", err)
	}

	invoice := mustEvent(t, "evt_inv_1", EventInvoicePaid, map[string]interface{}{
		"id": "in_1", "customer": "cus_3", "subscription": "sub_3", "status": "paid",
		"amount_due": 2900, "amount_paid": 2900, "currency": "usd",
		"hosted_invoice_url": "https://invoice.example/in_1", "paid": true,
	})
	if err := service.HandleWebhookEvent(ctx, invoice); err != nil {
		t.Fatalf("invoice: %v", err)
	}
	order, err := service.orderDAO.GetByOrderID(ctx, service.db, "in_1")
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	if order.AmountCents != 2900 || !order.Paid || order.TenantID != "tenant-y" {
		t.Fatalf("order = %+v", order)
	}
	subscription, _ := service.Subscription(ctx, "tenant-y")
	if subscription == nil || subscription.InvoiceID == nil || *subscription.InvoiceID != "in_1" {
		t.Fatalf("subscription invoice = %+v", subscription)
	}
}

// jsonNumber reads a JSON array element as a number: values written as int64
// come back as float64 after a database round trip.
func jsonNumber(t *testing.T, value interface{}) float64 {
	t.Helper()
	switch typed := value.(type) {
	case int64:
		return float64(typed)
	case float64:
		return typed
	case int:
		return float64(typed)
	}
	t.Fatalf("not a number: %T", value)
	return 0
}

func TestInvoicePaidRecordsProductColumns(t *testing.T) {
	service := newWebhookTestService(t)
	ctx := context.Background()

	update := mustEvent(t, "evt_sub_prod", EventSubscriptionUpdated, map[string]interface{}{
		"id": "sub_prod", "customer": "cus_prod", "status": "active",
		"current_period_start": time.Now().Unix(), "current_period_end": time.Now().Unix() + 100,
		"metadata": map[string]string{"tenant_id": "tenant-prod"},
		"items": map[string]interface{}{"data": []interface{}{
			map[string]interface{}{"id": "si_prod", "quantity": 1, "price": map[string]string{"id": "price_starter"}},
		}},
	})
	if err := service.HandleWebhookEvent(ctx, update); err != nil {
		t.Fatalf("update: %v", err)
	}

	invoice := mustEvent(t, "evt_inv_prod", EventInvoicePaid, map[string]interface{}{
		"id": "in_prod", "customer": "cus_prod", "subscription": "sub_prod", "status": "paid",
		"amount_due": 3400, "amount_paid": 3400, "currency": "usd", "paid": true,
		"hosted_invoice_url": "https://invoice.example/in_prod",
		"invoice_pdf":        "https://invoice.example/in_prod.pdf",
		"lines": map[string]interface{}{"data": []interface{}{
			map[string]interface{}{
				"amount": 2900, "quantity": 1,
				"pricing": map[string]interface{}{"price_details": map[string]string{"price": "price_starter"}},
			},
			map[string]interface{}{
				"amount": 500, "quantity": 1,
				"price": map[string]string{"id": "price_retired"},
			},
		}},
	})
	if err := service.HandleWebhookEvent(ctx, invoice); err != nil {
		t.Fatalf("invoice: %v", err)
	}
	order, err := service.orderDAO.GetByOrderID(ctx, service.db, "in_prod")
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	if len(order.ProductNames) != 2 || order.ProductNames[0] != "Starter" || order.ProductNames[1] != "UNKNOWN" {
		t.Fatalf("product names = %v, want [Starter UNKNOWN]", order.ProductNames)
	}
	if len(order.ProductQuantities) != 2 || jsonNumber(t, order.ProductQuantities[1]) != 1 {
		t.Fatalf("product quantities = %v", order.ProductQuantities)
	}
	if len(order.ProductAmountCents) != 2 || jsonNumber(t, order.ProductAmountCents[0]) != 2900 {
		t.Fatalf("product amounts = %v", order.ProductAmountCents)
	}
	if len(order.PriceIDs) != 2 || order.PriceIDs[1] != "price_retired" {
		t.Fatalf("price ids = %v", order.PriceIDs)
	}
	if order.ReceiptPDFURL == nil || *order.ReceiptPDFURL != "https://invoice.example/in_prod.pdf" {
		t.Fatalf("receipt pdf = %v", order.ReceiptPDFURL)
	}
}

func TestCheckoutSessionRecordsPointsOrderColumns(t *testing.T) {
	service := newWebhookTestService(t)
	ctx := context.Background()

	event := mustEvent(t, "evt_cs_points", EventCheckoutSessionCompleted, map[string]interface{}{
		"id": "cs_points", "object": "checkout.session", "mode": "payment",
		"customer": "cus_points", "amount_total": 100, "currency": "usd",
		"payment_intent": "pi_points",
		"metadata": map[string]string{
			"payment_type":  "points_recharge",
			"tenant_id":     "tenant-points",
			"points_amount": "100",
		},
	})
	if err := service.HandleWebhookEvent(ctx, event); err != nil {
		t.Fatalf("checkout session: %v", err)
	}
	order, err := service.orderDAO.GetByOrderID(ctx, service.db, "cs_points")
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	if len(order.ProductNames) != 1 || order.ProductNames[0] != "points_recharge" {
		t.Fatalf("product names = %v, want [points_recharge]", order.ProductNames)
	}
	if len(order.ProductQuantities) != 1 || jsonNumber(t, order.ProductQuantities[0]) != 100 {
		t.Fatalf("product quantities = %v, want [100]", order.ProductQuantities)
	}
	if len(order.ProductAmountCents) != 1 || jsonNumber(t, order.ProductAmountCents[0]) != 100 {
		t.Fatalf("product amounts = %v, want [100]", order.ProductAmountCents)
	}
}

func TestDuplicateEventIsIgnored(t *testing.T) {
	service := newWebhookTestService(t)
	ctx := context.Background()
	event := mustEvent(t, "evt_dup", EventInvoicePaid, map[string]interface{}{
		"id": "in_dup", "customer": "cus_none", "status": "paid", "paid": true,
	})
	if err := service.HandleWebhookEvent(ctx, event); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := service.HandleWebhookEvent(ctx, event); err != nil {
		t.Fatalf("second: %v", err)
	}
	row, err := service.webhookDAO.GetByEventID(ctx, service.db, "evt_dup")
	if err != nil {
		t.Fatalf("event row: %v", err)
	}
	if row.ProcessingStatus != "unhandled" {
		t.Fatalf("status = %s", row.ProcessingStatus)
	}
	_ = fmt.Sprintf("%v", row)
}

func TestRemoveStorageItemClearsAddon(t *testing.T) {
	service := newWebhookTestService(t)
	ctx := context.Background()
	addonItem := "si_storage"
	addonBytes := int64(500)
	if err := service.subscriptionDAO.UpsertSubscription(ctx, service.db, &entity.BillingSubscription{
		ID:                      "sub-1",
		TenantID:                "tenant-s",
		PlanName:                "Starter",
		CustomerID:              "cus_s",
		SubscriptionID:          "sub_1",
		SubscriptionStatus:      "active",
		AddonSubscriptionItemID: &addonItem,
		AddonStorageBytes:       &addonBytes,
	}); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
	event := mustEvent(t, "evt_storage_remove", EventSubscriptionUpdated, map[string]interface{}{
		"id": "sub_1", "customer": "cus_s", "status": "active",
		"current_period_start": time.Now().Unix(), "current_period_end": time.Now().Unix() + 100,
		"metadata": map[string]string{"tenant_id": "tenant-s"},
		"items": map[string]interface{}{"data": []interface{}{
			map[string]interface{}{"id": "si_plan", "quantity": 1, "price": map[string]string{"id": "price_starter"}},
		}},
	})
	if err := service.HandleWebhookEvent(ctx, event); err != nil {
		t.Fatalf("handle: %v", err)
	}
	row, _ := service.Subscription(ctx, "tenant-s")
	if row.AddonStorageBytes != nil || row.AddonSubscriptionItemID != nil {
		t.Fatalf("addon should be cleared, got %+v", row)
	}
}

func TestInvoiceParentSubscriptionIsResolved(t *testing.T) {
	service := newWebhookTestService(t)
	ctx := context.Background()
	target := "Starter"
	if err := service.subscriptionDAO.UpsertSubscription(ctx, service.db, &entity.BillingSubscription{
		ID: "sub-parent", TenantID: "tenant-p", PlanName: "Starter",
		CustomerID: "cus_p", SubscriptionID: "sub_p", SubscriptionStatus: "active",
		TargetPlanName: &target,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	event := mustEvent(t, "evt_parent", EventInvoicePaid, map[string]interface{}{
		"id": "in_parent", "customer": "cus_p", "status": "paid", "paid": true,
		"amount_paid": 2900, "currency": "usd",
		"parent": map[string]interface{}{
			"type":                 "subscription_details",
			"subscription_details": map[string]string{"subscription": "sub_p"},
		},
	})
	if err := service.HandleWebhookEvent(ctx, event); err != nil {
		t.Fatalf("handle: %v", err)
	}
	order, err := service.orderDAO.GetByOrderID(ctx, service.db, "in_parent")
	if err != nil || order == nil {
		t.Fatalf("order missing: %v %v", order, err)
	}
	if order.PaymentSubscriptionID == nil || *order.PaymentSubscriptionID != "sub_p" {
		t.Fatalf("order subscription = %v", order.PaymentSubscriptionID)
	}
}

// stubStripeBackend routes the stripe-go package at a local handler so tests
// can exercise SDK failure paths without network access.
func stubStripeBackend(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	previous := stripe.GetBackend(stripe.APIBackend)
	stripe.SetBackend(stripe.APIBackend, stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{
		URL:               stripe.String(server.URL),
		MaxNetworkRetries: stripe.Int64(0),
		LeveledLogger:     &stripe.LeveledLogger{Level: stripe.LevelNull},
	}))
	t.Cleanup(func() {
		stripe.SetBackend(stripe.APIBackend, previous)
		server.Close()
	})
}

// enableStripe marks the service as configured without touching the network:
// the SDK backend is replaced by the caller before the handler runs.
func enableStripe(t *testing.T, service *Service) {
	t.Helper()
	service.cfg.StripeAPIKey = "sk_test_stub"
	// Consume the one-time initializer so EnsureStripe keeps the stubbed
	// backend instead of installing a client pointed at Stripe's API.
	service.stripeOnce.Do(func() {})
}

func testSubscriptionObject(subscriptionID, tenantID, status string, periodStart, periodEnd int64) map[string]interface{} {
	return map[string]interface{}{
		"id": subscriptionID, "customer": "cus_" + subscriptionID, "status": status,
		"current_period_start": periodStart, "current_period_end": periodEnd,
		"metadata": map[string]string{"tenant_id": tenantID},
		"items": map[string]interface{}{"data": []interface{}{
			map[string]interface{}{"id": "si_" + subscriptionID, "quantity": 1, "price": map[string]string{"id": "price_starter"}},
		}},
	}
}

// A subscription snapshot without items used to be acknowledged as unhandled:
// nothing was written, yet the event reached a terminal status and Stripe
// never retried. It must fail so the retry drain can pick it up.
func TestSubscriptionEventWithoutItemsFails(t *testing.T) {
	service := newWebhookTestService(t)
	ctx := context.Background()
	object := testSubscriptionObject("sub_noitems", "tenant-noitems", "active", time.Now().Unix(), time.Now().Unix()+100)
	object["items"] = map[string]interface{}{"data": []interface{}{}}
	event := mustEvent(t, "evt_noitems", EventSubscriptionCreated, object)

	err := service.HandleWebhookEvent(ctx, event)
	if err == nil {
		t.Fatal("a subscription event without items must fail")
	}
	row, rowErr := service.webhookDAO.GetByEventID(ctx, service.db, "evt_noitems")
	if rowErr != nil {
		t.Fatalf("event row: %v", rowErr)
	}
	if row.ProcessingStatus != "failed" {
		t.Fatalf("status = %s, want failed", row.ProcessingStatus)
	}
	retryable, listErr := service.webhookDAO.ListRetryable(ctx, service.db, "", 10)
	if listErr != nil || len(retryable) != 1 || retryable[0].EventID != "evt_noitems" {
		t.Fatalf("retryable = %v %v", retryable, listErr)
	}
}

// A snapshot without a usable billing period cannot be written. Acknowledging
// it silently loses the upgrade; it must fail and stay retryable.
func TestSubscriptionEventWithoutBillingPeriodFails(t *testing.T) {
	service := newWebhookTestService(t)
	ctx := context.Background()
	object := testSubscriptionObject("sub_noperiod", "tenant-noperiod", "active", 0, 0)
	event := mustEvent(t, "evt_noperiod", EventSubscriptionUpdated, object)

	err := service.HandleWebhookEvent(ctx, event)
	if err == nil {
		t.Fatal("a subscription event without a billing period must fail")
	}
	row, rowErr := service.webhookDAO.GetByEventID(ctx, service.db, "evt_noperiod")
	if rowErr != nil {
		t.Fatalf("event row: %v", rowErr)
	}
	if row.ProcessingStatus != "failed" {
		t.Fatalf("status = %s, want failed", row.ProcessingStatus)
	}
	if subscription, _ := service.Subscription(ctx, "tenant-noperiod"); subscription != nil {
		t.Fatalf("no subscription may be written, got %+v", subscription)
	}
}

// A price missing from the plan catalogue must fail the event instead of
// keeping the locally stored plan: the old fallback left a tenant on Starter
// forever when Stripe reported an unmapped (legacy) price.
func TestSubscriptionUnknownPriceFailsWithoutStalePlanFallback(t *testing.T) {
	service := newWebhookTestService(t)
	ctx := context.Background()
	staleStart := time.Now().Add(-48 * time.Hour).Unix()
	if err := service.subscriptionDAO.UpsertSubscription(ctx, service.db, &entity.BillingSubscription{
		ID: "sub-stale", TenantID: "tenant-stale", PlanName: "Starter",
		CustomerID: "cus_stale", SubscriptionID: "sub_stale", SubscriptionStatus: "active",
		StartTime: ptrTime(time.Unix(staleStart, 0).UTC()),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	event := mustEvent(t, "evt_unknownprice", EventSubscriptionUpdated, map[string]interface{}{
		"id": "sub_stale", "customer": "cus_stale", "status": "active",
		"current_period_start": time.Now().Unix(), "current_period_end": time.Now().Unix() + 100,
		"metadata": map[string]string{"tenant_id": "tenant-stale"},
		"items": map[string]interface{}{"data": []interface{}{
			map[string]interface{}{"id": "si_stale", "quantity": 1, "price": map[string]string{"id": "price_legacy_zero"}},
		}},
	})

	err := service.HandleWebhookEvent(ctx, event)
	if err == nil || !strings.Contains(err.Error(), "unknown stripe price") {
		t.Fatalf("err = %v, want unknown price failure", err)
	}
	row, rowErr := service.webhookDAO.GetByEventID(ctx, service.db, "evt_unknownprice")
	if rowErr != nil {
		t.Fatalf("event row: %v", rowErr)
	}
	if row.ProcessingStatus != "failed" {
		t.Fatalf("status = %s, want failed", row.ProcessingStatus)
	}
	subscription, _ := service.Subscription(ctx, "tenant-stale")
	if subscription == nil || subscription.PlanName != "Starter" || subscription.PriceID != "" {
		t.Fatalf("local row must stay untouched, got %+v", subscription)
	}
}

// A permanently failing event must not hold the replay watermark back: the
// failed row is stored locally, so the drain owns its retries and the later
// events must still be replayed.
func TestReplayAdvancesPastFailedEvent(t *testing.T) {
	service := newWebhookTestService(t)
	ctx := context.Background()
	enableStripe(t, service)
	if err := service.db.AutoMigrate(&entity.SystemSettings{}); err != nil {
		t.Fatalf("migrate system settings: %v", err)
	}

	now := time.Now().Unix()
	badObject := map[string]interface{}{
		"id": "sub_replay_bad", "customer": "cus_replay_bad", "status": "active",
		"current_period_start": now, "current_period_end": now + 100,
		"metadata": map[string]string{"tenant_id": "tenant-replay-bad"},
		"items": map[string]interface{}{"data": []interface{}{
			map[string]interface{}{"id": "si_bad", "quantity": 1, "price": map[string]string{"id": "price_legacy_zero"}},
		}},
	}
	goodObject := testSubscriptionObject("sub_replay_good", "tenant-replay-good", "active", now, now+100)
	stubStripeBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/events") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"object":   "list",
			"has_more": false,
			"data": []interface{}{
				map[string]interface{}{
					"id": "evt_replay_bad", "object": "event", "type": EventSubscriptionUpdated,
					"created": now - 20, "data": map[string]interface{}{"object": badObject},
				},
				map[string]interface{}{
					"id": "evt_replay_good", "object": "event", "type": EventSubscriptionUpdated,
					"created": now - 10, "data": map[string]interface{}{"object": goodObject},
				},
			},
		})
	})

	if err := service.ReplayUndeliveredEvents(ctx); err == nil {
		t.Fatal("the failed event must still be reported")
	}
	badRow, badErr := service.webhookDAO.GetByEventID(ctx, service.db, "evt_replay_bad")
	if badErr != nil {
		t.Fatalf("failed event row: %v", badErr)
	}
	if badRow.ProcessingStatus != "failed" {
		t.Fatalf("failed event status = %s", badRow.ProcessingStatus)
	}
	goodRow, goodErr := service.webhookDAO.GetByEventID(ctx, service.db, "evt_replay_good")
	if goodErr != nil {
		t.Fatalf("later event must still be replayed: %v", goodErr)
	}
	if goodRow.ProcessingStatus != "completed" {
		t.Fatalf("later event status = %s, want completed", goodRow.ProcessingStatus)
	}
	subscription, _ := service.Subscription(ctx, "tenant-replay-good")
	if subscription == nil || subscription.PlanName != "Starter" {
		t.Fatalf("later event must converge: %+v", subscription)
	}
	checkpoint, _ := service.settingValue(ctx, checkpointKey)
	if checkpoint == "" {
		t.Fatal("checkpoint must advance past the replayed events")
	}
	value, parseErr := strconv.ParseInt(checkpoint, 10, 64)
	if parseErr != nil || value < now-10 {
		t.Fatalf("checkpoint = %q, want >= %d", checkpoint, now-10)
	}
}

// A paid invoice whose subscription cannot be read must fail the event: the
// order is recorded, but acknowledging it would leave a paying tenant on the
// old plan with no retry.
func TestInvoicePaidSubscriptionRetrieveFailureMarksEventFailed(t *testing.T) {
	service := newWebhookTestService(t)
	ctx := context.Background()
	enableStripe(t, service)
	stubStripeBackend(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"type":"api_error","message":"stripe unavailable"}}`))
	})

	if err := service.subscriptionDAO.UpsertSubscription(ctx, service.db, &entity.BillingSubscription{
		ID: "sub-retrieve", TenantID: "tenant-retrieve", PlanName: "Starter",
		CustomerID: "cus_retrieve", SubscriptionID: "sub_retrieve", SubscriptionStatus: "active",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	event := mustEvent(t, "evt_retrieve_fail", EventInvoicePaid, map[string]interface{}{
		"id": "in_retrieve_fail", "customer": "cus_retrieve", "subscription": "sub_retrieve",
		"status": "paid", "amount_due": 2900, "amount_paid": 2900, "currency": "usd", "paid": true,
	})

	err := service.HandleWebhookEvent(ctx, event)
	if err == nil {
		t.Fatal("an unreadable subscription must fail the invoice event")
	}
	row, rowErr := service.webhookDAO.GetByEventID(ctx, service.db, "evt_retrieve_fail")
	if rowErr != nil {
		t.Fatalf("event row: %v", rowErr)
	}
	if row.ProcessingStatus != "failed" {
		t.Fatalf("status = %s, want failed", row.ProcessingStatus)
	}
	subscription, _ := service.Subscription(ctx, "tenant-retrieve")
	if subscription == nil || subscription.SubscriptionStatus != "active" {
		t.Fatalf("entitlement must not converge on a failed retrieve: %+v", subscription)
	}
}
