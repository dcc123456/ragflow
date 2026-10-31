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
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stripe/stripe-go/v87"
	"github.com/stripe/stripe-go/v87/invoice"
	"github.com/stripe/stripe-go/v87/subscription"
)

func TestStorage01FirstAddonProration(t *testing.T) {
	tenant := newTenant(t)
	tenant.attachTestCard()
	tenant.ensureSubscriptionAtPrice("Pro")
	tenant.waitForPlan("Pro", 150*time.Second)

	beforeCount, beforeTotal := paidInvoiceSnapshot(t, tenant.ensureCustomer())
	data := tenant.patch("/billing/storage", map[string]interface{}{"target_storage_bytes": oneGB})
	if scheduled, _ := data["scheduled"].(bool); scheduled {
		t.Fatalf("first purchase must be immediate, got %v", data)
	}
	if amount, ok := data["amount_cents"].(float64); !ok || amount <= 0 {
		t.Fatalf("first add-on should report a prorated amount, got %v", data)
	}
	if state, _ := data["payment_state"].(string); state != "paid" {
		t.Fatalf("first add-on should be paid, got %v", data)
	}
	charged, _ := data["amount_cents"].(float64)
	afterCount, afterTotal := paidInvoiceSnapshot(t, tenant.ensureCustomer())
	if afterCount != beforeCount+1 || afterTotal != beforeTotal+int64(charged) {
		t.Fatalf("storage charge not reflected in Stripe: before=%d/%d after=%d/%d reported=%v",
			beforeCount, beforeTotal, afterCount, afterTotal, data["amount_cents"])
	}
	if lineQty := storageInvoiceQuantity(t, tenant, data, "storage"); lineQty != 1 {
		t.Fatalf("first add-on invoice quantity = %d, want 1", lineQty)
	}
}

// storageInvoiceQuantity returns the storage quantity billed by the invoice of
// a storage response (0 when the invoice has no storage line).
func storageInvoiceQuantity(t *testing.T, tenant *billingTenant, data map[string]interface{}, plan string) int64 {
	t.Helper()
	invoiceID, _ := data["invoice_id"].(string)
	if invoiceID == "" {
		t.Fatalf("response has no invoice id: %v", data)
	}
	row, err := invoice.Get(invoiceID, nil)
	if err != nil {
		t.Fatalf("get invoice %s: %v", invoiceID, err)
	}
	if row.Lines == nil {
		return 0
	}
	var quantity int64
	for _, line := range row.Lines.Data {
		if strings.Contains(strings.ToLower(line.Description), strings.ToLower(plan)) {
			quantity += line.Quantity
		}
	}
	return quantity
}

// TestStorage02StorageWithTrialFallback ports STORAGE-02: falling back to Trial
// drops the add-on together with the plan.

func TestStorage02StorageWithTrialFallback(t *testing.T) {
	tenant := newTenant(t)
	tenant.attachTestCard()
	tenant.ensureSubscriptionAtPrice("Pro")
	tenant.waitForPlan("Pro", 150*time.Second)
	tenant.patch("/billing/storage", map[string]interface{}{"target_storage_bytes": oneGB})

	tenant.post("/billing/subscription", map[string]interface{}{
		"payment_type":          "subscription",
		"subscription_price_id": trialPriceID(t),
	})
	tenant.cancelSubscription()
	tenant.waitForPlan("Trial", 90*time.Second)

	storage := tenant.get("/billing/storage")
	if got, _ := storage["addon_storage_bytes"].(float64); int64(got) != 0 {
		t.Fatalf("add-on must be cleared after falling back to Trial, got %v", storage["addon_storage_bytes"])
	}
}

// TestMember01TrialQuota ports MEMBER-01 (Trial): a Trial tenant cannot invite a
// second member.

func TestStorage03UpgradeImmediate(t *testing.T) {
	tenant := newTenant(t)
	tenant.attachTestCard()
	tenant.ensureSubscriptionAtPrice("Pro")
	tenant.waitForPlan("Pro", 150*time.Second)

	beforeCount, beforeTotal := paidInvoiceSnapshot(t, tenant.ensureCustomer())
	data := tenant.patch("/billing/storage", map[string]interface{}{"target_storage_bytes": 2 * oneGB})
	if scheduled, _ := data["scheduled"].(bool); scheduled {
		t.Fatalf("increase must be immediate, got %v", data)
	}
	charged, _ := data["amount_cents"].(float64)
	afterCount, afterTotal := paidInvoiceSnapshot(t, tenant.ensureCustomer())
	if afterCount != beforeCount+1 || afterTotal != beforeTotal+int64(charged) {
		t.Fatalf("storage increase charge not reflected: before=%d/%d after=%d/%d reported=%v",
			beforeCount, beforeTotal, afterCount, afterTotal, data["amount_cents"])
	}
	// The tenant starts without an add-on, so the proration bills the full
	// target quantity for the remaining period.
	if billed := storageInvoiceQuantity(t, tenant, data, "storage"); billed != 2 {
		t.Fatalf("increase invoice should bill 2 GB, got %d", billed)
	}
	storage := tenant.get("/billing/storage")
	if got, _ := storage["addon_storage_bytes"].(float64); int64(got) != 2*oneGB {
		t.Fatalf("addon storage = %v, want %d", storage["addon_storage_bytes"], 2*oneGB)
	}
	if err := assertSubscriptionHasStorageItem(t, tenant, 2); err != nil {
		t.Fatal(err)
	}
}

// TestStorage04DowngradeAtPeriodEnd ports STORAGE-04: decreasing the add-on is
// scheduled for the end of the period and keeps the current quantity until then.

func TestStorage04DowngradeAtPeriodEnd(t *testing.T) {
	tenant := newTenant(t)
	tenant.attachTestCard()
	tenant.ensureSubscriptionAtPrice("Pro")
	tenant.waitForPlan("Pro", 150*time.Second)
	tenant.patch("/billing/storage", map[string]interface{}{"target_storage_bytes": oneGB})

	beforeCount, beforeTotal := paidInvoiceSnapshot(t, tenant.ensureCustomer())
	data := tenant.patch("/billing/storage", map[string]interface{}{"target_storage_bytes": 0})
	if scheduled, _ := data["scheduled"].(bool); !scheduled {
		t.Fatalf("decrease must be scheduled, got %v", data)
	}
	if afterCount, afterTotal := paidInvoiceSnapshot(t, tenant.ensureCustomer()); afterCount != beforeCount || afterTotal != beforeTotal {
		t.Fatalf("scheduling a decrease must not charge: before=%d/%d after=%d/%d", beforeCount, beforeTotal, afterCount, afterTotal)
	}
	storage := tenant.get("/billing/storage")
	if got, _ := storage["addon_storage_bytes"].(float64); int64(got) != oneGB {
		t.Fatalf("addon must stay at the current quantity, got %v", storage["addon_storage_bytes"])
	}
	if err := assertSubscriptionHasStorageItem(t, tenant, 1); err != nil {
		t.Fatal(err)
	}

	// Re-selecting the current quantity cancels the pending decrease.
	data = tenant.patch("/billing/storage", map[string]interface{}{"target_storage_bytes": oneGB})
	if scheduled, _ := data["scheduled"].(bool); scheduled {
		t.Fatalf("reselection must cancel the decrease, got %v", data)
	}
	storage = tenant.get("/billing/storage")
	if target, ok := storage["target_storage_bytes"]; ok && target != nil {
		t.Fatalf("pending target must be cleared, got %v", target)
	}
}

// TestStorage05PlanChangeKeepsAddon ports STORAGE-05: a plan change preserves the
// storage add-on item.

func TestStorage05PlanChangeKeepsAddon(t *testing.T) {
	tenant := newTenant(t)
	tenant.attachTestCard()
	tenant.ensureSubscriptionAtPrice("Starter")
	tenant.waitForPlan("Starter", 150*time.Second)
	tenant.patch("/billing/storage", map[string]interface{}{"target_storage_bytes": oneGB})

	tenant.post("/billing/subscription", map[string]interface{}{
		"payment_type":          "subscription",
		"subscription_price_id": planPrice(t, "Pro"),
		"session_success_url":   successURL,
		"session_cancel_url":    cancelURL,
	})
	tenant.waitForPlan("Pro", 150*time.Second)
	if err := assertSubscriptionHasStorageItem(t, tenant, 1); err != nil {
		t.Fatalf("plan change dropped the add-on: %v", err)
	}
}

// TestPoint03InvalidInputsRejected ports POINT-03: invalid purchases are
// rejected and do not change the balance.

// TestStorage04DowngradeTimelineWithClock ports STORAGE-04 with a Stripe test
// clockFixture is a tenant pinned to a Stripe test clock. base is the instant
// the clock was frozen at, so every checkpoint is derived from it rather than
// from a fresh time.Now() (which would drift with test latency).
type clockFixture struct {
	t        *testing.T
	tenant   *billingTenant
	customer *stripe.Customer
	clock    *stripe.TestHelpersTestClock
	base     time.Time
	sub      *stripe.Subscription
}

// clockTenantOnPlan builds a clock-bound tenant already subscribed to the plan.
func clockTenantOnPlan(t *testing.T, plan string) *clockFixture {
	t.Helper()
	if !stripeEnabled(t) {
		t.Skip("billing flows disabled")
	}
	dsn := os.Getenv("RAGFLOW_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("RAGFLOW_TEST_MYSQL_DSN is required to bind the tenant to a test clock")
	}
	tenant := newTenant(t)
	tenant.ensureCustomer()
	clock, base := createTestClock(t)
	cus := createClockCustomer(t, clock.ID, tenant.tenantID)
	if err := tenant.attachCardTo(cus.ID); err != nil {
		t.Fatalf("attach card: %v", err)
	}
	if err := linkCustomerToTenant(dsn, tenant.tenantID, cus.ID); err != nil {
		t.Fatalf("link customer: %v", err)
	}
	sub, err := subscription.New(&stripe.SubscriptionParams{
		Customer:          stripe.String(cus.ID),
		Items:             []*stripe.SubscriptionItemsParams{{Price: stripe.String(planPrice(t, plan))}},
		PaymentBehavior:   stripe.String("error_if_incomplete"),
		ProrationBehavior: stripe.String("none"),
	})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	tenant.subID = sub.ID
	tenant.waitForPlan(plan, 150*time.Second)
	return &clockFixture{t: t, tenant: tenant, customer: cus, clock: clock, base: base, sub: sub}
}

// advanceTo moves the clock to an instant on the frozen timeline.
func (f *clockFixture) advanceTo(at time.Time) {
	f.t.Helper()
	advanceClock(f.t, f.clock.ID, at)
}

// renew advances one hour past the current billing period end and returns the
// paid invoice Stripe issued for the new cycle. The period end is read from a
// fresh subscription: the add-on item can shift it, so a stale copy would make
// the clock stop short of the renewal.
func (f *clockFixture) renew() *stripe.Invoice {
	f.t.Helper()
	seen := map[string]bool{}
	for _, row := range listCustomerInvoices(f.t, f.customer.ID) {
		seen[row.ID] = true
	}
	live, err := subscription.Get(f.sub.ID, nil)
	if err != nil {
		f.t.Fatalf("get subscription: %v", err)
	}
	periodEnd := time.Unix(live.Items.Data[0].CurrentPeriodEnd, 0)
	f.advanceTo(periodEnd.Add(time.Hour))

	deadline := time.Now().Add(180 * time.Second)
	for time.Now().Before(deadline) {
		for _, row := range listCustomerInvoices(f.t, f.customer.ID) {
			if !seen[row.ID] && row.Status == stripe.InvoiceStatusPaid {
				f.t.Logf("renewal invoice %s charged %d for the cycle after %s",
					row.ID, row.AmountPaid, periodEnd.Format(time.RFC3339))
				return row
			}
		}
		time.Sleep(2 * time.Second)
	}
	f.t.Fatalf("no renewal invoice appeared after the period end %s", periodEnd.Format(time.RFC3339))
	return nil
}

// TestStorage04DowngradeTimelineWithClock ports STORAGE-04 with a Stripe test
// clock: schedule the decrease, then observe quantity and charges immediately,
// one day later, and one hour after the period end (where the new phase applies).
func TestStorage04DowngradeTimelineWithClock(t *testing.T) {
	f := clockTenantOnPlan(t, "Pro")
	tenant := f.tenant

	// Buy 1 GB, which bills one unit immediately.
	tenant.patch("/billing/storage", map[string]interface{}{"target_storage_bytes": oneGB})
	addCount, addTotal := paidInvoiceSnapshot(t, f.customer.ID)
	if addCount != 2 {
		t.Fatalf("expected the plan invoice plus one add-on invoice, got %d", addCount)
	}

	// Schedule the decrease to zero.
	data := tenant.patch("/billing/storage", map[string]interface{}{"target_storage_bytes": 0})
	if scheduled, _ := data["scheduled"].(bool); !scheduled {
		t.Fatalf("decrease must be scheduled, got %v", data)
	}
	assertStorageStage(t, tenant, f.customer.ID, "right after scheduling", 1, addCount, addTotal)

	// One day later nothing changes: still paying for 1 GB, still scheduled.
	f.advanceTo(f.base.Add(24 * time.Hour))
	assertStorageStage(t, tenant, f.customer.ID, "one day later", 1, addCount, addTotal)

	// One hour past the period end the new phase applies: storage is gone and
	// the renewal invoice bills only the plan.
	renewal := f.renew()
	assertNoStorageLine(t, "renewal invoice", renewal)
	waitForStorageQuantity(t, tenant, 0, 180*time.Second)
	assertStorageStage(t, tenant, f.customer.ID, "after the period end", 0, addCount+1, addTotal+renewal.AmountPaid)
	if row := tenant.get("/billing/storage"); func() bool {
		v, ok := row["addon_storage_bytes"].(float64)
		return ok && v != 0
	}() {
		t.Fatalf("local add-on bytes should be 0 after the decrease: %v", row)
	}
}

// TestStorage01TimelineWithClock extends STORAGE-01 to two checkpoints: the
// immediate add-on charge and the renewal one cycle later, checking both the
// billed quantity and the money.
func TestStorage01TimelineWithClock(t *testing.T) {
	f := clockTenantOnPlan(t, "Pro")
	tenant := f.tenant

	baseCount, baseTotal := paidInvoiceSnapshot(t, f.customer.ID)
	if baseCount != 1 {
		t.Fatalf("expected the plan invoice only, got %d invoices", baseCount)
	}

	data := tenant.patch("/billing/storage", map[string]interface{}{"target_storage_bytes": oneGB})
	charged, ok := data["amount_cents"].(float64)
	if !ok || charged <= 0 {
		t.Fatalf("first add-on should be charged immediately, got %v", data)
	}
	if state, _ := data["payment_state"].(string); state != "paid" {
		t.Fatalf("first add-on should be paid, got %v", data)
	}
	count, total := paidInvoiceSnapshot(t, f.customer.ID)
	if count != baseCount+1 || total != baseTotal+int64(charged) {
		t.Fatalf("add-on charge not reflected in Stripe: invoices=%d/%d want %d/%d",
			count, total, baseCount+1, baseTotal+int64(charged))
	}
	if err := assertSubscriptionHasStorageItem(t, tenant, 1); err != nil {
		t.Fatalf("immediately after purchase: %v", err)
	}
	if qty := storageUnitsBilled(t, newestInvoice(t, f.customer.ID)); qty != 1 {
		t.Fatalf("add-on invoice bills %d GB, want 1", qty)
	}

	renewal := f.renew()

	if err := assertSubscriptionHasStorageItem(t, tenant, 1); err != nil {
		t.Fatalf("after the period end: %v", err)
	}
	if qty := storageUnitsBilled(t, renewal); qty != 1 {
		t.Fatalf("renewal invoice bills %d GB, want 1", qty)
	}
	if renewal.AmountPaid <= 0 {
		t.Fatalf("renewal invoice was not charged: %v", renewal)
	}
	if count, total := paidInvoiceSnapshot(t, f.customer.ID); count != baseCount+2 || total != baseTotal+int64(charged)+renewal.AmountPaid {
		t.Fatalf("renewal charge not reflected in Stripe: invoices=%d/%d", count, total)
	}
}

// TestStorage03TimelineWithClock extends STORAGE-03: buy 1 GB, increase to 2 GB,
// then check the increase is billed as the one-unit delta and the renewal bills
// two units.
func TestStorage03TimelineWithClock(t *testing.T) {
	f := clockTenantOnPlan(t, "Pro")
	tenant := f.tenant

	baseCount, baseTotal := paidInvoiceSnapshot(t, f.customer.ID)
	first := tenant.patch("/billing/storage", map[string]interface{}{"target_storage_bytes": oneGB})
	firstCharge, ok := first["amount_cents"].(float64)
	if !ok || firstCharge <= 0 {
		t.Fatalf("first add-on should be charged immediately, got %v", first)
	}
	if count, total := paidInvoiceSnapshot(t, f.customer.ID); count != baseCount+1 || total != baseTotal+int64(firstCharge) {
		t.Fatalf("first add-on charge not reflected in Stripe: invoices=%d/%d", count, total)
	}

	data := tenant.patch("/billing/storage", map[string]interface{}{"target_storage_bytes": 2 * oneGB})
	increase, ok := data["amount_cents"].(float64)
	if !ok || increase <= 0 {
		t.Fatalf("increase should report a prorated charge: %v", data)
	}
	count, total := paidInvoiceSnapshot(t, f.customer.ID)
	if count != baseCount+2 || total != baseTotal+int64(firstCharge)+int64(increase) {
		t.Fatalf("increase charge not reflected in Stripe: invoices=%d/%d, want %d/%d",
			count, total, baseCount+2, baseTotal+int64(firstCharge)+int64(increase))
	}
	increaseInvoice := newestInvoice(t, f.customer.ID)
	if err := assertSubscriptionHasStorageItem(t, tenant, 2); err != nil {
		t.Fatalf("immediately after increase: %v", err)
	}
	// The proration carries a credit line for the old unit, so only the signed
	// sum gives the units actually billed by this invoice.
	if qty := storageUnitsBilled(t, increaseInvoice); qty != 1 {
		t.Fatalf("increase invoice bills %d GB, want the 1 GB delta", qty)
	}

	renewal := f.renew()

	if err := assertSubscriptionHasStorageItem(t, tenant, 2); err != nil {
		t.Fatalf("after the period end: %v", err)
	}
	if qty := storageUnitsBilled(t, renewal); qty != 2 {
		t.Fatalf("renewal invoice bills %d GB, want 2", qty)
	}
	if count, total := paidInvoiceSnapshot(t, f.customer.ID); count != baseCount+3 ||
		total != baseTotal+int64(firstCharge)+int64(increase)+renewal.AmountPaid {
		t.Fatalf("renewal charge not reflected in Stripe: invoices=%d/%d", count, total)
	}
}

// assertStorageStage checks the billed storage quantity plus the paid invoice
// count and total after a checkpoint.
func assertStorageStage(t *testing.T, tenant *billingTenant, customerID, stage string, wantQty int64, wantCount int, wantTotal int64) {
	t.Helper()
	if err := assertSubscriptionHasStorageItem(t, tenant, wantQty); err != nil {
		t.Fatalf("%s: %v", stage, err)
	}
	count, total := paidInvoiceSnapshot(t, customerID)
	if count != wantCount {
		t.Fatalf("%s: paid invoices = %d, want %d", stage, count, wantCount)
	}
	if total != wantTotal {
		t.Fatalf("%s: paid total = %d, want %d", stage, total, wantTotal)
	}
}

// assertNoStorageLine checks an invoice carries no storage charge.
func assertNoStorageLine(t *testing.T, stage string, row *stripe.Invoice) {
	t.Helper()
	if row == nil || row.Lines == nil {
		return
	}
	for _, line := range row.Lines.Data {
		if strings.Contains(strings.ToLower(line.Description), "storage") {
			t.Fatalf("%s still bills storage: %s", stage, line.Description)
		}
	}
}

// storageUnitsBilled returns the net storage units an invoice bills. Prorations
// come as a credit line for the old quantity plus a charge line for the new
// one, so raw quantities double count and the signed sum is the real delta.
func storageUnitsBilled(t *testing.T, row *stripe.Invoice) int64 {
	t.Helper()
	if row == nil || row.Lines == nil {
		return 0
	}
	var qty int64
	for _, line := range row.Lines.Data {
		if !strings.Contains(strings.ToLower(line.Description), "storage") {
			continue
		}
		if line.Amount < 0 {
			qty -= line.Quantity
		} else {
			qty += line.Quantity
		}
	}
	return qty
}

// newestInvoice returns the most recently created invoice of a customer.
func newestInvoice(t *testing.T, customerID string) *stripe.Invoice {
	t.Helper()
	var newest *stripe.Invoice
	for _, row := range listCustomerInvoices(t, customerID) {
		if newest == nil || row.Created > newest.Created {
			newest = row
		}
	}
	return newest
}

// waitForStorageQuantity polls until the subscription carries the wanted
// storage quantity (0 means the item is gone).
func waitForStorageQuantity(t *testing.T, tenant *billingTenant, want int64, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		live, err := subscription.Get(tenant.subID, nil)
		if err == nil {
			qty := int64(0)
			storagePrice := planPriceIDs(t)["storage"]
			for _, item := range live.Items.Data {
				if item.Price != nil && item.Price.ID == storagePrice {
					qty = item.Quantity
				}
			}
			if qty == want {
				return
			}
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("storage quantity never became %d", want)
}
