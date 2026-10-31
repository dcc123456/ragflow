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
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/stripe/stripe-go/v87"
	"github.com/stripe/stripe-go/v87/customer"
	"github.com/stripe/stripe-go/v87/invoice"
	"github.com/stripe/stripe-go/v87/invoiceitem"
	"github.com/stripe/stripe-go/v87/paymentmethod"
	"github.com/stripe/stripe-go/v87/subscription"
)

const billingTestPassword = "QaBilling#2026x"

const (
	successURL = "http://127.0.0.1:9222/price?ok=1"
	cancelURL  = "http://127.0.0.1:9222/price?cancel=1"
	oneGB      = int64(1000000000)
)

// billingTenant is an isolated tenant owned by one test: its own user, Stripe
// customer and API token, so flows never interfere with each other.
type billingTenant struct {
	t          *testing.T
	cfg        *ServerTestConfig
	email      string
	tenantID   string
	customerID string
	subID      string
}

// stripeEnabled reports whether the suite may talk to stripe.com.
func stripeEnabled(t *testing.T) bool {
	t.Helper()
	if os.Getenv("BILLING_STRIPE_E2E") != "1" {
		t.Skip("set BILLING_STRIPE_E2E=1 to run the billing flows against real Stripe")
	}
	stripe.Key = stripeSecretKey(t)
	return true
}

// stripeSecretKey resolves the test key from the environment or the repo config.
func stripeSecretKey(t *testing.T) string {
	t.Helper()
	if key := os.Getenv("BILLING_STRIPE_API_KEY"); key != "" {
		return key
	}
	raw := repoServiceConfig(t)
	if match := regexp.MustCompile(`stripe_api_key:\s*['"]?(sk_test_[A-Za-z0-9]+)`).FindStringSubmatch(raw); len(match) == 2 {
		return match[1]
	}
	t.Skip("no Stripe test key available (BILLING_STRIPE_API_KEY or conf/service_conf.yaml)")
	return ""
}

func repoServiceConfig(t *testing.T) string {
	t.Helper()
	for _, path := range []string{"../../conf/service_conf.yaml", "conf/service_conf.yaml"} {
		if data, err := os.ReadFile(path); err == nil {
			return string(data)
		}
	}
	t.Skip("conf/service_conf.yaml not found")
	return ""
}

// planPriceIDs reads plan -> first price id from the service configuration.
func planPriceIDs(t *testing.T) map[string]string {
	t.Helper()
	raw := repoServiceConfig(t)
	prices := map[string]string{}
	re := regexp.MustCompile(`(?s)-\s*name:\s*"([^"]+)".*?price_ids:\s*['"]?([A-Za-z0-9_]+)`)
	for _, match := range re.FindAllStringSubmatch(raw, -1) {
		prices[match[1]] = match[2]
	}
	if len(prices) == 0 {
		t.Skip("no billing plans configured")
	}
	return prices
}

func planPrice(t *testing.T, plan string) string {
	t.Helper()
	price, ok := planPriceIDs(t)[plan]
	if !ok {
		t.Skipf("%s price id not configured", plan)
	}
	return price
}

// newTenant registers a fresh user, logs in and returns its tenant context.
func newTenant(t *testing.T) *billingTenant {
	t.Helper()
	if !stripeEnabled(t) {
		t.Skip("billing flows disabled")
	}
	email := fmt.Sprintf("billing-%d-%s@infiniflow.org", time.Now().UnixNano(), strings.ToLower(randSuffix(t)))
	body := map[string]interface{}{"email": email, "nickname": "billing", "password": billingTestPassword}
	resp := rawJSON(t, http.MethodPost, TestConfig.HostAddress+"/api/v1/users", body, "")
	if resp.status != http.StatusOK {
		t.Fatalf("register %s: status=%d body=%s", email, resp.status, resp.body)
	}
	login := rawJSON(t, http.MethodPost, TestConfig.HostAddress+"/api/v1/auth/login",
		map[string]interface{}{"email": email, "password": billingTestPassword}, "")
	token := login.header.Get("Authorization")
	if token == "" {
		t.Fatalf("login %s: %s", email, login.body)
	}
	cfg := &ServerTestConfig{HostAddress: TestConfig.HostAddress, APIVersion: "v1", AuthToken: token, Email: email, Password: billingTestPassword}
	tenant := &billingTenant{t: t, cfg: cfg, email: email}
	tenant.tenantID = loginTenantID(t, login.body)
	if tenant.tenantID == "" {
		if id, _ := tenant.mustSubscription()["tenant_id"].(string); id != "" {
			tenant.tenantID = id
		}
	}
	if tenant.tenantID == "" {
		t.Fatalf("cannot resolve tenant id for %s", email)
	}
	t.Cleanup(tenant.cancelSubscription)
	return tenant
}

func randSuffix(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
}

// mustSubscription returns the GET /billing/subscription data object.
func (b *billingTenant) mustSubscription() map[string]interface{} {
	b.t.Helper()
	resp := b.get("/billing/subscription")
	return resp
}

// get performs an authenticated GET and returns the data object.
func (b *billingTenant) get(path string) map[string]interface{} {
	b.t.Helper()
	httpResp, err := b.cfg.GetJSON(path, nil, nil)
	if err != nil {
		b.t.Fatalf("GET %s: %v", path, err)
	}
	payload := decodeEnvelope(b.t, httpResp)
	if code := int(payload.Code); code != 0 {
		b.t.Fatalf("GET %s failed: code=%d message=%s", path, code, payload.Message)
	}
	if payload.Data == nil {
		b.t.Fatalf("GET %s returned empty data: %s", path, payload.Raw)
	}
	return payload.Data
}

// post performs an authenticated POST and returns the data object.
func (b *billingTenant) post(path string, body map[string]interface{}) map[string]interface{} {
	b.t.Helper()
	httpResp, err := b.cfg.PostJSON(path, body, nil)
	if err != nil {
		b.t.Fatalf("POST %s: %v", path, err)
	}
	payload := decodeEnvelope(b.t, httpResp)
	if code := int(payload.Code); code != 0 {
		b.t.Fatalf("POST %s failed: code=%d message=%s", path, code, payload.Message)
	}
	return payload.Data
}

// postExpectError performs a POST and returns the business code and message.
func (b *billingTenant) postExpectError(path string, body map[string]interface{}) (int, string) {
	b.t.Helper()
	httpResp, err := b.cfg.PostJSON(path, body, nil)
	if err != nil {
		b.t.Fatalf("POST %s: %v", path, err)
	}
	payload := decodeEnvelope(b.t, httpResp)
	return int(payload.Code), payload.Message
}

// patch performs an authenticated PATCH and returns the data object.
func (b *billingTenant) patch(path string, body map[string]interface{}) map[string]interface{} {
	b.t.Helper()
	httpResp, err := b.cfg.PatchJSON(path, body, nil)
	if err != nil {
		b.t.Fatalf("PATCH %s: %v", path, err)
	}
	payload := decodeEnvelope(b.t, httpResp)
	if code := int(payload.Code); code != 0 {
		b.t.Fatalf("PATCH %s failed: code=%d message=%s", path, code, payload.Message)
	}
	return payload.Data
}

func (b *billingTenant) patchExpectError(path string, body map[string]interface{}) (int, string) {
	b.t.Helper()
	httpResp, err := b.cfg.PatchJSON(path, body, nil)
	if err != nil {
		b.t.Fatalf("PATCH %s: %v", path, err)
	}
	payload := decodeEnvelope(b.t, httpResp)
	return int(payload.Code), payload.Message
}

// ensureCustomer provisions the Stripe customer through the preview endpoint
// (which calls EnsureCustomer without creating a checkout session).
func (b *billingTenant) ensureCustomer() string {
	b.t.Helper()
	if b.customerID != "" {
		return b.customerID
	}
	if id, _ := b.mustSubscription()["customer_id"].(string); id != "" {
		b.customerID = id
		return id
	}
	b.post("/billing/subscription", map[string]interface{}{
		"payment_type":          "subscription",
		"subscription_price_id": planPrice(b.t, "Starter"),
		"session_success_url":   "http://127.0.0.1:9222/price?ok=1",
		"session_cancel_url":    "http://127.0.0.1:9222/price?cancel=1",
	})
	id, _ := b.mustSubscription()["customer_id"].(string)
	if id == "" {
		b.t.Fatalf("tenant has no Stripe customer: %v", b.mustSubscription())
	}
	b.customerID = id
	return id
}

// attachTestCard attaches tok_visa and makes it the customer default so
// proration charges succeed without an interactive flow.
func (b *billingTenant) attachTestCard() {
	b.t.Helper()
	customerID := b.ensureCustomer()
	current, err := customer.Get(customerID, nil)
	if err != nil {
		b.t.Fatalf("get customer: %v", err)
	}
	if current.InvoiceSettings != nil && current.InvoiceSettings.DefaultPaymentMethod != nil {
		return
	}
	pm, err := paymentmethod.New(&stripe.PaymentMethodParams{
		Type: stripe.String(string(stripe.PaymentMethodTypeCard)),
		Card: &stripe.PaymentMethodCardParams{Token: stripe.String("tok_visa")},
	})
	if err != nil {
		b.t.Fatalf("create payment method: %v", err)
	}
	if _, err := paymentmethod.Attach(pm.ID, &stripe.PaymentMethodAttachParams{Customer: stripe.String(customerID)}); err != nil {
		b.t.Fatalf("attach payment method: %v", err)
	}
	if _, err := customer.Update(customerID, &stripe.CustomerParams{
		InvoiceSettings: &stripe.CustomerInvoiceSettingsParams{DefaultPaymentMethod: stripe.String(pm.ID)},
	}); err != nil {
		b.t.Fatalf("set default payment method: %v", err)
	}
}

// ensureSubscriptionAtPrice creates (or re-prices) a live subscription so the
// tenant starts on the given plan without going through Checkout.
func (b *billingTenant) ensureSubscriptionAtPrice(plan string) *stripe.Subscription {
	b.t.Helper()
	priceID := planPrice(b.t, plan)
	customerID := b.ensureCustomer()
	iter := subscription.List(&stripe.SubscriptionListParams{Customer: stripe.String(customerID)})
	for iter.Next() {
		row := iter.Subscription()
		if row == nil || row.Status == stripe.SubscriptionStatusCanceled {
			continue
		}
		if len(row.Items.Data) == 0 || row.Items.Data[0].Price == nil {
			continue
		}
		if row.Items.Data[0].Price.ID == priceID {
			b.subID = row.ID
			return row
		}
		updated, err := subscription.Update(row.ID, &stripe.SubscriptionParams{
			Items:             []*stripe.SubscriptionItemsParams{{ID: stripe.String(row.Items.Data[0].ID), Price: stripe.String(priceID)}},
			ProrationBehavior: stripe.String("none"),
		})
		if err != nil {
			b.t.Fatalf("re-price subscription: %v", err)
		}
		b.subID = updated.ID
		return updated
	}
	if err := iter.Err(); err != nil {
		b.t.Fatalf("list subscriptions: %v", err)
	}
	created, err := subscription.New(&stripe.SubscriptionParams{
		Customer:          stripe.String(customerID),
		Items:             []*stripe.SubscriptionItemsParams{{Price: stripe.String(priceID)}},
		PaymentBehavior:   stripe.String("allow_incomplete"),
		ProrationBehavior: stripe.String("none"),
	})
	if err != nil {
		b.t.Fatalf("create subscription: %v", err)
	}
	b.subID = created.ID
	return created
}

func (b *billingTenant) cancelSubscription() {
	if b.subID == "" {
		return
	}
	live, err := subscription.Get(b.subID, nil)
	if err != nil || live.Status == stripe.SubscriptionStatusCanceled {
		return
	}
	if _, err := subscription.Cancel(b.subID, nil); err != nil {
		b.t.Logf("cleanup: cancel %s: %v", b.subID, err)
	}
}

// waitForPlan polls until the local subscription converges to the plan.
func (b *billingTenant) waitForPlan(plan string, timeout time.Duration) {
	b.t.Helper()
	deadline := time.Now().Add(timeout)
	last := ""
	for time.Now().Before(deadline) {
		last, _ = b.mustSubscription()["plan_name"].(string)
		if last == plan {
			return
		}
		time.Sleep(2 * time.Second)
	}
	b.t.Fatalf("plan did not converge to %s (last %q); is `stripe listen` forwarding to %s?", plan, last, TestConfig.HostAddress)
}

// subscriptionStatus returns the Stripe subscription for assertions.
func (b *billingTenant) subscriptionStatus() *stripe.Subscription {
	b.t.Helper()
	live, err := subscription.Get(b.subID, nil)
	if err != nil {
		b.t.Fatalf("get subscription: %v", err)
	}
	return live
}

// listCustomerSubscriptions returns the customer's subscriptions.
func listCustomerSubscriptions(customerID string) *subscription.Iter {
	return subscription.List(&stripe.SubscriptionListParams{Customer: stripe.String(customerID)})
}

// assertSubscriptionHasStorageItem verifies the add-on item quantity.
func assertSubscriptionHasStorageItem(t *testing.T, tenant *billingTenant, want int64) error {
	t.Helper()
	if tenant.subID == "" {
		return fmt.Errorf("tenant has no subscription")
	}
	live, err := subscription.Get(tenant.subID, nil)
	if err != nil {
		return fmt.Errorf("get subscription: %w", err)
	}
	storagePrice := planPriceIDs(t)["storage"]
	for _, item := range live.Items.Data {
		if item.Price != nil && item.Price.ID == storagePrice {
			if item.Quantity != want {
				return fmt.Errorf("storage quantity = %d, want %d", item.Quantity, want)
			}
			return nil
		}
	}
	if want == 0 {
		return nil
	}
	return fmt.Errorf("storage item missing on %s", tenant.subID)
}

// detachPaymentMethod removes a payment method from its customer.
func detachPaymentMethod(pmID string) error {
	_, err := paymentmethod.Detach(pmID, nil)
	return err
}

// loginTenantID reads the tenant id from the login payload.
func loginTenantID(t *testing.T, body string) string {
	t.Helper()
	var payload struct {
		Code float64 `json:"code"`
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return ""
	}
	return payload.Data.ID
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type envelope struct {
	Code    float64                `json:"code"`
	Message string                 `json:"message"`
	Data    map[string]interface{} `json:"data"`
	Raw     string                 `json:"-"`
}

func decodeEnvelope(t *testing.T, resp *http.Response) envelope {
	t.Helper()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var payload envelope
	payload.Raw = string(raw)
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode envelope: %v body=%s", err, string(raw))
	}
	return payload
}

// jsonUnmarshal decodes a raw body into out.
func jsonUnmarshal(body string, out interface{}) error {
	return json.Unmarshal([]byte(body), out)
}

// uploadMultipart posts one file as multipart/form-data.
func uploadMultipart(t *testing.T, url, token, name string, content []byte) rawResponse {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatalf("multipart: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("multipart write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, &buf)
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 120 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return rawResponse{status: resp.StatusCode, body: string(raw), header: resp.Header}
}

type rawResponse struct {
	status int
	body   string
	header http.Header
}

func rawJSON(t *testing.T, method, url string, body map[string]interface{}, token string) rawResponse {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return rawResponse{status: resp.StatusCode, body: string(raw), header: resp.Header}
}
func stripeTestKey(t *testing.T) string {
	t.Helper()
	if key := os.Getenv("BILLING_STRIPE_API_KEY"); key != "" {
		return key
	}
	if key := readConfigValue(t, `stripe_api_key:\s*['"]?(sk_test_[A-Za-z0-9]+)`); key != "" {
		return key
	}
	t.Skip("BILLING_STRIPE_API_KEY is not set and conf/service_conf.yaml has no key")
	return ""
}

// loadPlanPriceIDs reads plan -> price id from the service configuration.

func loadPlanPriceIDs(t *testing.T) map[string]string {
	t.Helper()
	raw := readConfigFile(t)
	prices := map[string]string{}
	re := regexp.MustCompile(`(?s)-\s*name:\s*"([^"]+)".*?price_ids:\s*['"]?([A-Za-z0-9_]+)`)
	for _, match := range re.FindAllStringSubmatch(raw, -1) {
		prices[match[1]] = match[2]
	}
	if len(prices) == 0 {
		t.Skip("no billing plans found in conf/service_conf.yaml")
	}
	return prices
}

// trialPriceID asks the running server for the Trial price id, which is a local
// placeholder rather than a Stripe price.

func trialPriceID(t *testing.T) string {
	t.Helper()
	resp, err := TestConfig.GetJSON("/billing/plans", nil, nil)
	if err != nil {
		t.Fatalf("get plans: %v", err)
	}
	defer resp.Body.Close()
	var payload struct {
		Data []struct {
			Name     string `json:"name"`
			PriceIDs string `json:"price_ids"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode plans: %v", err)
	}
	for _, plan := range payload.Data {
		if plan.Name == "Trial" && plan.PriceIDs != "" {
			return plan.PriceIDs
		}
	}
	t.Fatal("Trial plan has no price id")
	return ""
}

func readConfigFile(t *testing.T) string {
	t.Helper()
	for _, path := range []string{"../../conf/service_conf.yaml", "conf/service_conf.yaml"} {
		if data, err := os.ReadFile(path); err == nil {
			return string(data)
		}
	}
	t.Skip("conf/service_conf.yaml not found")
	return ""
}

func readConfigValue(t *testing.T, pattern string) string {
	t.Helper()
	raw := readConfigFile(t)
	match := regexp.MustCompile(pattern).FindStringSubmatch(raw)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

// ensureCustomer returns the tenant's Stripe customer, creating one through the
// checkout endpoint when the tenant has none yet.

func ensureCustomer(t *testing.T) string {
	t.Helper()
	if id := currentCustomerID(t); id != "" {
		return id
	}
	// Creating a checkout session provisions the customer as a side effect.
	postBilling(t, "/billing/subscription", map[string]interface{}{
		"payment_type":          "subscription",
		"subscription_price_id": planPriceOrSkip(t, "Starter"),
		"session_success_url":   "http://127.0.0.1:9222/price?ok=1",
		"session_cancel_url":    "http://127.0.0.1:9222/price?cancel=1",
	})
	if id := currentCustomerID(t); id != "" {
		return id
	}
	t.Fatal("tenant has no Stripe customer after checkout provisioning")
	return ""
}

func planPriceOrSkip(t *testing.T, plan string) string {
	t.Helper()
	prices := loadPlanPriceIDs(t)
	price, ok := prices[plan]
	if !ok {
		t.Skipf("%s price id not configured", plan)
	}
	return price
}

func currentCustomerID(t *testing.T) string {
	t.Helper()
	resp, err := TestConfig.GetJSON("/billing/subscription", nil, nil)
	if err != nil {
		t.Fatalf("get subscription: %v", err)
	}
	defer resp.Body.Close()
	var payload struct {
		Data struct {
			CustomerID string `json:"customer_id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ""
	}
	return payload.Data.CustomerID
}

// ensureDefaultPaymentMethod attaches a test card so subscription updates can
// charge prorations without an interactive flow.

func ensureDefaultPaymentMethod(t *testing.T, customerID string) {
	t.Helper()
	current, err := customer.Get(customerID, nil)
	if err != nil {
		t.Fatalf("get customer: %v", err)
	}
	if current.InvoiceSettings != nil && current.InvoiceSettings.DefaultPaymentMethod != nil {
		return
	}
	pm, err := paymentmethod.New(&stripe.PaymentMethodParams{
		Type: stripe.String(string(stripe.PaymentMethodTypeCard)),
		Card: &stripe.PaymentMethodCardParams{Token: stripe.String("tok_visa")},
	})
	if err != nil {
		t.Fatalf("create payment method: %v", err)
	}
	if _, err := paymentmethod.Attach(pm.ID, &stripe.PaymentMethodAttachParams{Customer: stripe.String(customerID)}); err != nil {
		t.Fatalf("attach payment method: %v", err)
	}
	if _, err := customer.Update(customerID, &stripe.CustomerParams{
		InvoiceSettings: &stripe.CustomerInvoiceSettingsParams{DefaultPaymentMethod: stripe.String(pm.ID)},
	}); err != nil {
		t.Fatalf("set default payment method: %v", err)
	}
}

// ensureSubscriptionAtPrice returns an active subscription on the given price,
// creating one (or re-pricing the existing one) through the Stripe API.

func ensureSubscriptionAtPrice(t *testing.T, customerID, priceID string) *stripe.Subscription {
	t.Helper()
	iter := subscription.List(&stripe.SubscriptionListParams{Customer: stripe.String(customerID)})
	for iter.Next() {
		row := iter.Subscription()
		if row == nil || row.Status == stripe.SubscriptionStatusCanceled {
			continue
		}
		if len(row.Items.Data) > 0 && row.Items.Data[0].Price != nil && row.Items.Data[0].Price.ID == priceID {
			return row
		}
		return moveSubscriptionToPrice(t, row, priceID)
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("list subscriptions: %v", err)
	}
	created, err := subscription.New(&stripe.SubscriptionParams{
		Customer:          stripe.String(customerID),
		Items:             []*stripe.SubscriptionItemsParams{{Price: stripe.String(priceID)}},
		PaymentBehavior:   stripe.String("allow_incomplete"),
		ProrationBehavior: stripe.String("none"),
		Expand:            []*string{stripe.String("latest_invoice")},
	})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	return created
}

func moveSubscriptionToPrice(t *testing.T, row *stripe.Subscription, priceID string) *stripe.Subscription {
	t.Helper()
	item := row.Items.Data[0]
	updated, err := subscription.Update(row.ID, &stripe.SubscriptionParams{
		Items:             []*stripe.SubscriptionItemsParams{{ID: stripe.String(item.ID), Price: stripe.String(priceID)}},
		ProrationBehavior: stripe.String("none"),
	})
	if err != nil {
		t.Fatalf("re-price subscription: %v", err)
	}
	return updated
}

// postBilling calls a billing endpoint and returns its data object, failing on
// any non-zero business code.

func postBilling(t *testing.T, path string, body map[string]interface{}) map[string]interface{} {
	t.Helper()
	resp, err := TestConfig.PostJSON(path, body, nil)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	var payload struct {
		Code    float64                `json:"code"`
		Message string                 `json:"message"`
		Data    map[string]interface{} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	if int(payload.Code) != 0 {
		t.Fatalf("POST %s failed: code=%v message=%s", path, payload.Code, payload.Message)
	}
	return payload.Data
}

// waitForPlan polls the subscription endpoint until the local plan converges,
// which only happens when webhook forwarding is running.

func waitForPlan(t *testing.T, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	last := ""
	for time.Now().Before(deadline) {
		resp, err := TestConfig.GetJSON("/billing/subscription", nil, nil)
		if err == nil {
			var payload struct {
				Data struct {
					PlanName string `json:"plan_name"`
				} `json:"data"`
			}
			if json.NewDecoder(resp.Body).Decode(&payload) == nil {
				last = payload.Data.PlanName
				if last == want {
					return
				}
			}
			resp.Body.Close()
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("plan did not converge to %s (last %q); is `stripe listen` forwarding to %s?", want, last, TestConfig.HostAddress)
}

func pointsCheckoutEvent(tenantID, sessionID string, points int) string {
	event := map[string]interface{}{
		"id":          "__EVENT_ID__",
		"object":      "event",
		"type":        "checkout.session.completed",
		"created":     time.Now().Unix(),
		"api_version": "2026-03-25.dahlia",
		"data": map[string]interface{}{"object": map[string]interface{}{
			"id":             sessionID,
			"object":         "checkout.session",
			"mode":           "payment",
			"payment_status": "paid",
			"amount_total":   points,
			"currency":       "usd",
			"metadata": map[string]string{
				"tenant_id":     tenantID,
				"payment_type":  "points_recharge",
				"points_amount": fmt.Sprintf("%d", points),
			},
		}},
	}
	raw, _ := json.Marshal(event)
	return string(raw)
}

func postRawWebhook(t *testing.T, payload string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, TestConfig.HostAddress+"/v1/billing/webhooks/stripe", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("webhook request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("webhook post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("webhook status %d", resp.StatusCode)
	}
}

// detachDefaultPaymentMethod removes the customer default so proration charges
// cannot be collected.

func detachDefaultPaymentMethod(customerID string) error {
	row, err := customer.Get(customerID, nil)
	if err != nil {
		return err
	}
	if row.InvoiceSettings == nil || row.InvoiceSettings.DefaultPaymentMethod == nil {
		return nil
	}
	pmID := row.InvoiceSettings.DefaultPaymentMethod.ID
	if _, err := customer.Update(customerID, &stripe.CustomerParams{
		InvoiceSettings: &stripe.CustomerInvoiceSettingsParams{DefaultPaymentMethod: stripe.String("")},
	}); err != nil {
		return err
	}
	return detachPaymentMethod(pmID)
}

func registerMember(t *testing.T, prefix string) string {
	t.Helper()
	email := fmt.Sprintf("%s-member-%d@infiniflow.org", prefix, time.Now().UnixNano())
	resp := rawJSON(t, http.MethodPost, TestConfig.HostAddress+"/api/v1/users", map[string]interface{}{
		"email": email, "nickname": prefix, "password": billingTestPassword,
	}, "")
	if resp.status != http.StatusOK {
		t.Fatalf("register member %s: %d %s", email, resp.status, resp.body)
	}
	return email
}

// createDataset creates an app counted by the app quota.

func (b *billingTenant) createDataset(name string) {
	b.t.Helper()
	b.post("/datasets", map[string]interface{}{"name": fmt.Sprintf("%s-%d", name, time.Now().UnixNano()), "parse_type": 1, "parser_id": "naive"})
}

func (b *billingTenant) createDatasetExpectError(name string) (int, string) {
	b.t.Helper()
	return b.postExpectError("/datasets", map[string]interface{}{"name": fmt.Sprintf("%s-%d", name, time.Now().UnixNano()), "parse_type": 1, "parser_id": "naive"})
}

// uploadFile posts a multipart file to /files.

func (b *billingTenant) uploadFile(name string, content []byte) rawResponse {
	b.t.Helper()
	return uploadMultipart(b.t, TestConfig.HostAddress+"/api/v1/files", b.cfg.AuthToken, name, content)
}

// appsLimit reads the app quota of a plan from the running server.

func appsLimit(t *testing.T, plan string) int64 {
	t.Helper()
	resp := rawJSON(t, http.MethodGet, TestConfig.HostAddress+"/api/v1/billing/plans", nil, TestConfig.AuthToken)
	var payload struct {
		Data []struct {
			Name    string `json:"name"`
			Feature struct {
				QuotaApps int64 `json:"quota_apps"`
			} `json:"feature"`
		} `json:"data"`
	}
	if err := jsonUnmarshal(resp.body, &payload); err != nil {
		t.Fatalf("decode plans: %v", err)
	}
	for _, item := range payload.Data {
		if item.Name == plan {
			return item.Feature.QuotaApps
		}
	}
	t.Fatalf("%s plan not found", plan)
	return 0
}

// TestPlan02RenewalFailureRecovery ports PLAN-02: renewal failure surfaces the
// payment-required banner and the invoice can be recovered.
//
// Stripe only emits invoice.payment_failed for an automatic collection attempt,
// so the flow runs on a Stripe test clock: the customer is created inside the
// clock, linked to the tenant, charged once, then advanced a month with a
// declining card. Without a database DSN the case falls back to the initial
// invoice path.

func (b *billingTenant) attachFailingDefault() {
	b.t.Helper()
	pm, err := paymentmethod.New(&stripe.PaymentMethodParams{
		Type: stripe.String(string(stripe.PaymentMethodTypeCard)),
		Card: &stripe.PaymentMethodCardParams{Token: stripe.String("tok_chargeCustomerFail")},
	})
	if err != nil {
		b.t.Fatalf("create failing payment method: %v", err)
	}
	if _, err := paymentmethod.Attach(pm.ID, &stripe.PaymentMethodAttachParams{Customer: stripe.String(b.customerID)}); err != nil {
		b.t.Fatalf("attach failing payment method: %v", err)
	}
	if _, err := customer.Update(b.customerID, &stripe.CustomerParams{
		InvoiceSettings: &stripe.CustomerInvoiceSettingsParams{DefaultPaymentMethod: stripe.String(pm.ID)},
	}); err != nil {
		b.t.Fatalf("set failing default: %v", err)
	}
}

// attachCardTo attaches tok_visa to an arbitrary customer.

func (b *billingTenant) attachCardTo(customerID string) error {
	pm, err := paymentmethod.New(&stripe.PaymentMethodParams{
		Type: stripe.String(string(stripe.PaymentMethodTypeCard)),
		Card: &stripe.PaymentMethodCardParams{Token: stripe.String("tok_visa")},
	})
	if err != nil {
		return err
	}
	if _, err := paymentmethod.Attach(pm.ID, &stripe.PaymentMethodAttachParams{Customer: stripe.String(customerID)}); err != nil {
		return err
	}
	_, err = customer.Update(customerID, &stripe.CustomerParams{
		InvoiceSettings: &stripe.CustomerInvoiceSettingsParams{DefaultPaymentMethod: stripe.String(pm.ID)},
	})
	return err
}

// attachFailingCardTo makes a declining card the customer default.

func (b *billingTenant) attachFailingCardTo(customerID string) error {
	pm, err := paymentmethod.New(&stripe.PaymentMethodParams{
		Type: stripe.String(string(stripe.PaymentMethodTypeCard)),
		Card: &stripe.PaymentMethodCardParams{Token: stripe.String("tok_chargeCustomerFail")},
	})
	if err != nil {
		return err
	}
	if _, err := paymentmethod.Attach(pm.ID, &stripe.PaymentMethodAttachParams{Customer: stripe.String(customerID)}); err != nil {
		return err
	}
	_, err = customer.Update(customerID, &stripe.CustomerParams{
		InvoiceSettings: &stripe.CustomerInvoiceSettingsParams{DefaultPaymentMethod: stripe.String(pm.ID)},
	})
	return err
}

// linkCustomerToTenant points the tenant's billing row at the clock customer.

func linkCustomerToTenant(dsn, tenantID, customerID string) error {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec("UPDATE billing_subscription SET customer_id = ? WHERE tenant_id = ?", customerID, tenantID)
	return err
}

func waitForCondition(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("%s never became true", what)
}

// TestTrial01bNoInvoiceAfterStarterCycle ports TRIAL-01b: the first paid cycle
// produces exactly one invoice (no invoice for the Trial period).

func (b *billingTenant) removeMember(email string) {
	b.t.Helper()
	list := rawJSON(b.t, http.MethodGet, TestConfig.HostAddress+"/api/v1"+fmt.Sprintf("/tenants/%s/users", b.tenantID), nil, b.cfg.AuthToken)
	var payload struct {
		Data []struct {
			UserID string `json:"user_id"`
			Email  string `json:"email"`
		} `json:"data"`
	}
	if err := jsonUnmarshal(list.body, &payload); err != nil {
		b.t.Fatalf("decode members: %v", err)
	}
	rows := payload.Data
	userID := ""
	for _, row := range rows {
		if row.Email == email {
			userID = row.UserID
			break
		}
	}
	if userID == "" {
		b.t.Fatalf("member %s not found in list", email)
	}
	resp := rawJSON(b.t, http.MethodDelete, TestConfig.HostAddress+"/api/v1"+fmt.Sprintf("/tenants/%s/users", b.tenantID),
		map[string]interface{}{"user_id": userID}, b.cfg.AuthToken)
	if resp.status != http.StatusOK {
		b.t.Fatalf("remove member %s: %d %s", email, resp.status, resp.body)
	}
}

// membersLimit reads the member quota of a plan.

func membersLimit(t *testing.T, plan string) int64 {
	t.Helper()
	resp := rawJSON(t, http.MethodGet, TestConfig.HostAddress+"/api/v1/billing/plans", nil, TestConfig.AuthToken)
	var payload struct {
		Data []struct {
			Name    string `json:"name"`
			Feature struct {
				QuotaMembers int64 `json:"quota_members"`
			} `json:"feature"`
		} `json:"data"`
	}
	if err := jsonUnmarshal(resp.body, &payload); err != nil {
		t.Fatalf("decode plans: %v", err)
	}
	for _, item := range payload.Data {
		if item.Name == plan {
			return item.Feature.QuotaMembers
		}
	}
	t.Fatalf("%s plan not found", plan)
	return 0
}

// failingPMID is captured for the subscription default in PLAN-02.

var failingPMID string

// useGoodCard restores a chargeable card on the customer and subscription.

func useGoodCard(t *testing.T, customerID string) error {
	t.Helper()
	pm, err := paymentmethod.New(&stripe.PaymentMethodParams{
		Type: stripe.String(string(stripe.PaymentMethodTypeCard)),
		Card: &stripe.PaymentMethodCardParams{Token: stripe.String("tok_visa")},
	})
	if err != nil {
		return err
	}
	if _, err := paymentmethod.Attach(pm.ID, &stripe.PaymentMethodAttachParams{Customer: stripe.String(customerID)}); err != nil {
		return err
	}
	if _, err := customer.Update(customerID, &stripe.CustomerParams{
		InvoiceSettings: &stripe.CustomerInvoiceSettingsParams{DefaultPaymentMethod: stripe.String(pm.ID)},
	}); err != nil {
		return err
	}
	iter := subscription.List(&stripe.SubscriptionListParams{Customer: stripe.String(customerID)})
	for iter.Next() {
		row := iter.Subscription()
		if row != nil && row.Status != stripe.SubscriptionStatusCanceled {
			_, err = subscription.Update(row.ID, &stripe.SubscriptionParams{DefaultPaymentMethod: stripe.String(pm.ID)})
			return err
		}
	}
	return iter.Err()
}

// useFailingCard replaces the customer default with a card that declines.

func useFailingCard(t *testing.T, customerID string) error {
	t.Helper()
	pm, err := paymentmethod.New(&stripe.PaymentMethodParams{
		Type: stripe.String(string(stripe.PaymentMethodTypeCard)),
		Card: &stripe.PaymentMethodCardParams{Token: stripe.String("tok_chargeCustomerFail")},
	})
	if err != nil {
		return err
	}
	if _, err := paymentmethod.Attach(pm.ID, &stripe.PaymentMethodAttachParams{Customer: stripe.String(customerID)}); err != nil {
		return err
	}
	failingPMID = pm.ID
	if _, err = customer.Update(customerID, &stripe.CustomerParams{
		InvoiceSettings: &stripe.CustomerInvoiceSettingsParams{DefaultPaymentMethod: stripe.String(pm.ID)},
	}); err != nil {
		return err
	}
	return nil
}

// paidInvoiceSnapshot returns the number and total of paid invoices, failing on
// any invoice that still expects money.
func paidInvoiceSnapshot(t *testing.T, customerID string) (int, int64) {
	t.Helper()
	count, total := 0, int64(0)
	for _, row := range listCustomerInvoices(t, customerID) {
		if row.Status == stripe.InvoiceStatusOpen && row.AmountDue > 0 {
			t.Fatalf("unexpected open invoice %s (%d due)", row.ID, row.AmountDue)
		}
		if row.Status == stripe.InvoiceStatusPaid && row.AmountPaid > 0 {
			count++
			total += row.AmountPaid
		}
	}
	return count, total
}

// createRenewalInvoice adds a charge to the subscription and finalises it so
// Stripe attempts (and fails) the payment.

func createRenewalInvoice(t *testing.T, customerID, subscriptionID string) error {
	t.Helper()
	if _, err := invoiceitem.New(&stripe.InvoiceItemParams{
		Customer:     stripe.String(customerID),
		Subscription: stripe.String(subscriptionID),
		Amount:       stripe.Int64(500),
		Currency:     stripe.String("usd"),
		Description:  stripe.String("renewal charge"),
	}); err != nil {
		return err
	}
	_, err := invoice.New(&stripe.InvoiceParams{
		Customer:     stripe.String(customerID),
		Subscription: stripe.String(subscriptionID),
		AutoAdvance:  stripe.Bool(true),
	})
	return err
}

// listCustomerInvoices returns the customer's invoices.

func listCustomerInvoices(t *testing.T, customerID string) []*stripe.Invoice {
	t.Helper()
	iter := invoice.List(&stripe.InvoiceListParams{Customer: stripe.String(customerID)})
	out := []*stripe.Invoice{}
	for iter.Next() {
		if row := iter.Invoice(); row != nil {
			out = append(out, row)
		}
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("list invoices: %v", err)
	}
	return out
}

// rawDelete issues a DELETE and ignores the payload.

func (b *billingTenant) rawDelete(path string, body map[string]interface{}) {
	b.t.Helper()
	httpResp, err := b.cfg.DeleteJSON(path, body, nil)
	if err != nil {
		b.t.Fatalf("DELETE %s: %v", path, err)
	}
	httpResp.Body.Close()
}
