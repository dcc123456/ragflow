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
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
	"github.com/stripe/stripe-go/v87"
	"github.com/stripe/stripe-go/v87/customer"
	"github.com/stripe/stripe-go/v87/invoice"
	"github.com/stripe/stripe-go/v87/subscription"
	"github.com/stripe/stripe-go/v87/subscriptionschedule"
	"github.com/stripe/stripe-go/v87/testhelpers/testclock"
	"os"
	"testing"
	"time"
)

func TestBillingStripeFlows(t *testing.T) {
	if os.Getenv("BILLING_STRIPE_E2E") != "1" {
		t.Skip("set BILLING_STRIPE_E2E=1 to run against the real Stripe test API")
	}
	key := stripeTestKey(t)
	stripe.Key = key
	prices := loadPlanPriceIDs(t)
	proPrice, ok := prices["Pro"]
	if !ok {
		t.Fatal("Pro price id not configured")
	}
	starterPrice, ok := prices["Starter"]
	if !ok {
		t.Fatal("Starter price id not configured")
	}
	trialPrice := trialPriceID(t)

	customerID := ensureCustomer(t)
	ensureDefaultPaymentMethod(t, customerID)
	sub := ensureSubscriptionAtPrice(t, customerID, proPrice)
	t.Cleanup(func() {
		live, err := subscription.Get(sub.ID, nil)
		if err != nil || live.Status == stripe.SubscriptionStatusCanceled {
			return
		}
		if _, err := subscription.Cancel(sub.ID, nil); err != nil {
			t.Logf("cleanup: cancel subscription %s: %v", sub.ID, err)
		}
	})

	waitForPlan(t, "Pro", 150*time.Second)

	// 1) Pro -> Starter is a scheduled change, current plan stays Pro.
	data := postBilling(t, "/billing/subscription", map[string]interface{}{
		"payment_type":          "subscription",
		"subscription_price_id": starterPrice,
		"session_success_url":   "http://127.0.0.1:9222/price?ok=1",
		"session_cancel_url":    "http://127.0.0.1:9222/price?cancel=1",
	})
	if scheduled, _ := data["scheduled"].(bool); !scheduled {
		t.Fatalf("downgrade should be scheduled, got %v", data)
	}
	if got, _ := data["target_plan_name"].(string); got != "Starter" {
		t.Fatalf("target plan = %q, want Starter", got)
	}

	// 2) Regression: falling back to Trial while a schedule owns the
	// subscription used to fail with 500 (Stripe forbids updating cancellation
	// behaviour directly). The fallback must release the schedule first.
	data = postBilling(t, "/billing/subscription", map[string]interface{}{
		"payment_type":          "subscription",
		"subscription_price_id": trialPrice,
	})
	if scheduled, _ := data["scheduled"].(bool); !scheduled {
		t.Fatalf("trial fallback should be scheduled, got %v", data)
	}
	if got, _ := data["target_plan_name"].(string); got != "Trial" {
		t.Fatalf("target plan = %q, want Trial", got)
	}
	live, err := subscription.Get(sub.ID, nil)
	if err != nil {
		t.Fatalf("retrieve subscription: %v", err)
	}
	if !live.CancelAtPeriodEnd {
		t.Fatalf("trial fallback must set cancel_at_period_end on %s", sub.ID)
	}
	schedules := subscriptionschedule.List(&stripe.SubscriptionScheduleListParams{Customer: stripe.String(customerID)})
	for schedules.Next() {
		row := schedules.SubscriptionSchedule()
		if row != nil && row.Status == "active" && row.Subscription != nil && row.Subscription.ID == sub.ID {
			t.Fatalf("schedule %s still active after trial fallback", row.ID)
		}
	}
	if err := schedules.Err(); err != nil {
		t.Fatalf("list schedules: %v", err)
	}

	// 3) Deleting the subscription converges the tenant back to Trial and drops
	// the storage add-on.
	if _, err := subscription.Cancel(sub.ID, nil); err != nil {
		t.Fatalf("cancel subscription: %v", err)
	}
	waitForPlan(t, "Trial", 150*time.Second)
}

// stripeTestKey resolves the Stripe secret key from the environment or the
// repository service configuration.

func TestPlan02RenewalFailureRecovery(t *testing.T) {
	tenant := newTenant(t)
	if dsn := os.Getenv("RAGFLOW_TEST_MYSQL_DSN"); dsn != "" {
		t.Run("test_clock", func(t *testing.T) {
			runClockRenewalFailure(t, tenant, dsn)
		})
		return
	}
	customerID := tenant.ensureCustomer()
	if err := useFailingCard(t, customerID); err != nil {
		t.Fatalf("attach failing card: %v", err)
	}
	tenant.attachFailingDefault()
	created := tenant.ensureSubscriptionAtPrice("Starter")

	waitForCondition(t, 90*time.Second, "payment required banner", func() bool {
		required, _ := tenant.mustSubscription()["payment_required"].(bool)
		return required
	})

	if err := useGoodCard(t, customerID); err != nil {
		t.Fatalf("attach good card: %v", err)
	}
	live, err := subscription.Get(created.ID, nil)
	if err != nil {
		t.Fatalf("get subscription: %v", err)
	}
	if live.LatestInvoice == nil || live.LatestInvoice.ID == "" {
		t.Fatalf("subscription has no latest invoice")
	}
	if _, err := invoice.Pay(live.LatestInvoice.ID, &stripe.InvoicePayParams{}); err != nil {
		t.Fatalf("pay invoice: %v", err)
	}
	tenant.waitForPlan("Starter", 90*time.Second)
	if required, _ := tenant.mustSubscription()["payment_required"].(bool); required {
		t.Fatalf("banner should clear after payment: %v", tenant.mustSubscription())
	}
}

// runClockRenewalFailure advances a Stripe test clock to force a real renewal.

func runClockRenewalFailure(t *testing.T, tenant *billingTenant, dsn string) {
	// The tenant needs a billing row before the clock customer can be linked.
	tenant.ensureCustomer()
	clock, base := createTestClock(t)
	cus := createClockCustomer(t, clock.ID, tenant.tenantID)
	if err := tenant.attachCardTo(cus.ID); err != nil {
		t.Fatalf("attach card: %v", err)
	}
	if err := linkCustomerToTenant(dsn, tenant.tenantID, cus.ID); err != nil {
		t.Fatalf("link customer: %v", err)
	}
	created, err := subscription.New(&stripe.SubscriptionParams{
		Customer:          stripe.String(cus.ID),
		Items:             []*stripe.SubscriptionItemsParams{{Price: stripe.String(planPrice(t, "Starter"))}},
		PaymentBehavior:   stripe.String("error_if_incomplete"),
		ProrationBehavior: stripe.String("none"),
	})
	if err != nil {
		t.Fatalf("create clock subscription: %v", err)
	}
	t.Cleanup(func() {
		if _, err := subscription.Cancel(created.ID, nil); err != nil {
			t.Logf("cleanup: cancel clock subscription: %v", err)
		}
	})
	tenant.subID = created.ID
	tenant.waitForPlan("Starter", 90*time.Second)

	// Renewal with a declining card.
	if err := tenant.attachFailingCardTo(cus.ID); err != nil {
		t.Fatalf("attach failing card: %v", err)
	}
	advanceClock(t, clock.ID, base.Add(35*24*time.Hour))
	waitForCondition(t, 180*time.Second, "payment required banner", func() bool {
		required, _ := tenant.mustSubscription()["payment_required"].(bool)
		return required
	})
}

// attachFailingDefault makes the declining card the subscription default so the
// first invoice cannot be collected.

func TestPlan03StarterToProViaAPI(t *testing.T) {
	tenant := newTenant(t)
	tenant.attachTestCard()
	tenant.ensureSubscriptionAtPrice("Starter")
	tenant.waitForPlan("Starter", 150*time.Second)

	data := tenant.post("/billing/subscription", map[string]interface{}{
		"payment_type":          "subscription",
		"subscription_price_id": planPrice(t, "Pro"),
		"session_success_url":   successURL,
		"session_cancel_url":    cancelURL,
	})
	if state, _ := data["payment_state"].(string); state != "paid" {
		t.Fatalf("expected paid upgrade, got %v", data)
	}
	if amount, ok := data["amount_cents"].(float64); !ok || amount <= 0 {
		t.Fatalf("upgrade should report a prorated amount, got %v", data["amount_cents"])
	}
	tenant.waitForPlan("Pro", 150*time.Second)
}

// TestPlan05UpgradeWithoutReusableCard ports PLAN-05: without a reusable card
// the upgrade cannot be charged, the tenant keeps Starter and Stripe reports an
// actionable payment state.

func TestPlan05UpgradeWithoutReusableCard(t *testing.T) {
	tenant := newTenant(t)
	tenant.attachTestCard()
	tenant.ensureSubscriptionAtPrice("Starter")
	tenant.waitForPlan("Starter", 150*time.Second)

	customerID := tenant.ensureCustomer()
	if err := detachDefaultPaymentMethod(customerID); err != nil {
		t.Fatalf("detach payment method: %v", err)
	}
	data := tenant.post("/billing/subscription", map[string]interface{}{
		"payment_type":          "subscription",
		"subscription_price_id": planPrice(t, "Pro"),
		"session_success_url":   successURL,
		"session_cancel_url":    cancelURL,
	})
	if flag, _ := data["requires_payment_method_setup"].(bool); !flag {
		t.Fatalf("upgrade without a card must ask for a payment method, got %v", data)
	}
	plan, _ := tenant.mustSubscription()["plan_name"].(string)
	if plan != "Starter" {
		t.Fatalf("tenant must keep Starter until payment converges, got %q", plan)
	}
}

// TestStorage03UpgradeImmediate ports STORAGE-03: increasing the add-on applies
// immediately and reports a prorated charge.\n
// TestPlan01CancelLifecycleWithClock ports PLAN-01's cancellation leg with a
// Stripe test clock: cancel -> observe -> +1 day -> observe -> period end + 1h
// -> observe the fallback to Trial.
func TestPlan01CancelLifecycleWithClock(t *testing.T) {
	if !stripeEnabled(t) {
		t.Skip("billing flows disabled")
	}
	dsn := os.Getenv("RAGFLOW_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("RAGFLOW_TEST_MYSQL_DSN is required to bind the tenant to a test clock")
	}
	tenant := newTenant(t)
	tenant.ensureCustomer()
	clock, clockBase := createTestClock(t)
	cus := createClockCustomer(t, clock.ID, tenant.tenantID)
	if err := tenant.attachCardTo(cus.ID); err != nil {
		t.Fatalf("attach card: %v", err)
	}
	if err := linkCustomerToTenant(dsn, tenant.tenantID, cus.ID); err != nil {
		t.Fatalf("link customer: %v", err)
	}
	sub, err := subscription.New(&stripe.SubscriptionParams{
		Customer:          stripe.String(cus.ID),
		Items:             []*stripe.SubscriptionItemsParams{{Price: stripe.String(planPrice(t, "Starter"))}},
		PaymentBehavior:   stripe.String("error_if_incomplete"),
		ProrationBehavior: stripe.String("none"),
	})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	tenant.subID = sub.ID
	tenant.waitForPlan("Starter", 150*time.Second)
	baseInvoices, baseTotal := invoiceSnapshot(t, cus.ID)
	baseOrders, baseOrderTotal := orderSnapshot(t, dsn, tenant.tenantID)
	if baseInvoices != 1 {
		t.Fatalf("expected one paid invoice after the first charge, got %d", baseInvoices)
	}
	if baseOrders != 1 || baseOrderTotal != baseTotal {
		t.Fatalf("local orders (%d / %d) do not match Stripe (%d / %d)", baseOrders, baseOrderTotal, baseInvoices, baseTotal)
	}
	t.Logf("baseline: paid invoices=%d total=%d, local orders=%d total=%d", baseInvoices, baseTotal, baseOrders, baseOrderTotal)

	// 1) Cancel: the plan stays Starter until the period end, the Trial fallback
	// is recorded and Stripe marks cancel_at_period_end.
	tenant.post("/billing/subscription", map[string]interface{}{
		"payment_type":          "subscription",
		"subscription_price_id": trialPriceID(t),
	})
	assertCancelState(t, tenant, sub.ID, "Starter", true)
	assertNoExtraCharge(t, "after cancellation", cus.ID, dsn, tenant.tenantID, baseInvoices, baseTotal, baseOrders, baseOrderTotal)

	// 2) Advance one day: still entitled, still scheduled, no fallback yet.
	advanceClock(t, clock.ID, clockBase.Add(24*time.Hour))
	assertCancelState(t, tenant, sub.ID, "Starter", true)
	assertNoExtraCharge(t, "one day after cancellation", cus.ID, dsn, tenant.tenantID, baseInvoices, baseTotal, baseOrders, baseOrderTotal)

	// 3) Advance to one hour past the period end: Stripe cancels the subscription
	// and the tenant converges to Trial with the add-on cleared.
	periodEnd := sub.Items.Data[0].CurrentPeriodEnd
	advanceClock(t, clock.ID, time.Unix(periodEnd, 0).Add(time.Hour))
	tenant.waitForPlan("Trial", 180*time.Second)
	subRow := tenant.mustSubscription()
	if target, ok := subRow["pending_subscription_change"]; ok && target != nil {
		t.Fatalf("pending change should be cleared after the fallback: %v", target)
	}
	if addon, ok := subRow["addon_storage_bytes"].(float64); ok && addon != 0 {
		t.Fatalf("add-on storage should be cleared, got %v", addon)
	}
	live, err := subscription.Get(sub.ID, nil)
	if err != nil {
		t.Fatalf("get subscription: %v", err)
	}
	if live.Status != stripe.SubscriptionStatusCanceled {
		t.Fatalf("stripe subscription status = %s, want canceled", live.Status)
	}
	// Cancelling at the period end must not bill the next cycle.
	assertNoExtraCharge(t, "after the period end", cus.ID, dsn, tenant.tenantID, baseInvoices, baseTotal, baseOrders, baseOrderTotal)
}

// invoiceSnapshot counts paid invoices and sums what was actually charged.
func invoiceSnapshot(t *testing.T, customerID string) (int, int64) {
	t.Helper()
	paid, total := 0, int64(0)
	for _, row := range listCustomerInvoices(t, customerID) {
		if row.Status == stripe.InvoiceStatusPaid && row.AmountPaid > 0 {
			paid++
			total += row.AmountPaid
		}
		if row.Status == stripe.InvoiceStatusOpen && row.AmountDue > 0 {
			t.Fatalf("unexpected open invoice %s (%d due)", row.ID, row.AmountDue)
		}
	}
	return paid, total
}

// orderSnapshot counts the local paid orders of a tenant.
func orderSnapshot(t *testing.T, dsn, tenantID string) (int, int64) {
	t.Helper()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var count int
	var total sql.NullInt64
	if err := db.QueryRow(
		"SELECT COUNT(*), COALESCE(SUM(amount_cents), 0) FROM billing_payment_order WHERE tenant_id = ? AND payment_status = 'success'",
		tenantID,
	).Scan(&count, &total); err != nil {
		t.Fatalf("query orders: %v", err)
	}
	return count, total.Int64
}

// assertNoExtraCharge verifies Stripe and the local order table did not grow.
func assertNoExtraCharge(t *testing.T, stage, customerID, dsn, tenantID string, wantInvoices int, wantTotal int64, wantOrders int, wantOrderTotal int64) {
	t.Helper()
	paid, total := invoiceSnapshot(t, customerID)
	if paid != wantInvoices || total != wantTotal {
		t.Fatalf("%s: invoices changed: paid=%d total=%d (want %d / %d)", stage, paid, total, wantInvoices, wantTotal)
	}
	orders, orderTotal := orderSnapshot(t, dsn, tenantID)
	t.Logf("%s: paid invoices=%d total=%d, local orders=%d total=%d", stage, paid, total, orders, orderTotal)
	if orders != wantOrders || orderTotal != wantOrderTotal {
		t.Fatalf("%s: local orders changed: count=%d total=%d (want %d / %d)", stage, orders, orderTotal, wantOrders, wantOrderTotal)
	}
}

// assertCancelState checks the local plan plus the Stripe cancellation flag.
func assertCancelState(t *testing.T, tenant *billingTenant, subscriptionID, wantPlan string, wantCancel bool) {
	t.Helper()
	row := tenant.mustSubscription()
	if plan, _ := row["plan_name"].(string); plan != wantPlan {
		t.Fatalf("plan = %q, want %q (%v)", plan, wantPlan, row)
	}
	pending, _ := row["pending_subscription_change"].(map[string]interface{})
	if maybeTarget, ok := pending["target_plan_name"].(string); !ok || maybeTarget != "Trial" {
		t.Fatalf("pending target = %v, want Trial (%v)", pending["target_plan_name"], pending)
	}
	live, err := subscription.Get(subscriptionID, nil)
	if err != nil {
		t.Fatalf("get subscription: %v", err)
	}
	if live.CancelAtPeriodEnd != wantCancel {
		t.Fatalf("cancel_at_period_end = %v, want %v", live.CancelAtPeriodEnd, wantCancel)
	}
}

// createTestClock freezes time at the moment the test starts and returns the
// frozen instant as well. Every later checkpoint must be derived from that base
// instead of calling time.Now() again: a slow test would otherwise move the
// target forward and the "one day later" checkpoint would silently drift.
func createTestClock(t *testing.T) (*stripe.TestHelpersTestClock, time.Time) {
	t.Helper()
	base := time.Now().Truncate(time.Second)
	clock, err := testclock.New(&stripe.TestHelpersTestClockParams{FrozenTime: stripe.Int64(base.Unix())})
	if err != nil {
		t.Fatalf("create test clock: %v", err)
	}
	t.Cleanup(func() { deleteTestClock(t, clock.ID) })
	return clock, base
}

// deleteTestClock removes a test clock. Stripe refuses to delete one that is
// still applying an advance, so wait for that to settle first and report a
// failure instead of leaking the clock silently.
func deleteTestClock(t *testing.T, clockID string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		clock, err := testclock.Get(clockID, nil)
		if err != nil {
			t.Logf("cleanup: get test clock %s: %v", clockID, err)
			return
		}
		if clock.Status != stripe.TestHelpersTestClockStatusAdvancing {
			break
		}
		if time.Now().After(deadline) {
			t.Logf("cleanup: test clock %s is still advancing, left in place", clockID)
			return
		}
		time.Sleep(time.Second)
	}
	if _, err := testclock.Del(clockID, nil); err != nil {
		t.Logf("cleanup: delete test clock %s: %v", clockID, err)
	}
}

// createClockCustomer creates a customer bound to the clock.
func createClockCustomer(t *testing.T, clockID, tenantID string) *stripe.Customer {
	t.Helper()
	cus, err := customer.New(&stripe.CustomerParams{
		Metadata:  map[string]string{"tenant_id": tenantID},
		TestClock: stripe.String(clockID),
	})
	if err != nil {
		t.Fatalf("create clock customer: %v", err)
	}
	return cus
}

// advanceClock moves the clock forward and waits until Stripe finished applying
// the change (advance is asynchronous).
func advanceClock(t *testing.T, clockID string, to time.Time) {
	t.Helper()
	if _, err := testclock.Advance(clockID, &stripe.TestHelpersTestClockAdvanceParams{
		FrozenTime: stripe.Int64(to.Unix()),
	}); err != nil {
		t.Fatalf("advance clock: %v", err)
	}
	deadline := time.Now().Add(180 * time.Second)
	for time.Now().Before(deadline) {
		clock, err := testclock.Get(clockID, nil)
		if err == nil && clock.Status == stripe.TestHelpersTestClockStatusReady {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("test clock %s did not become ready", clockID)
}
