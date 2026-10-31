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

package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/server/config"

	"github.com/stripe/stripe-go/v87"
	billingportal "github.com/stripe/stripe-go/v87/billingportal/session"
	checkoutsession "github.com/stripe/stripe-go/v87/checkout/session"
	"github.com/stripe/stripe-go/v87/customer"
	"github.com/stripe/stripe-go/v87/invoice"
	"github.com/stripe/stripe-go/v87/setupintent"
	"github.com/stripe/stripe-go/v87/subscription"
	"github.com/stripe/stripe-go/v87/subscriptionschedule"
)

// withCheckoutSessionPlaceholder makes the redirect page able to learn its
// session id, matching the Python checkout flow.
// ErrInvalidArgument marks caller mistakes that should surface as 400 rather
// than a server error.
var ErrInvalidArgument = errors.New("invalid billing request")

// requireCheckoutURLs rejects checkout requests without redirect URLs; Stripe
// refuses empty success_url/cancel_url with an opaque 400.
func requireCheckoutURLs(successURL, cancelURL string) error {
	if strings.TrimSpace(successURL) == "" || strings.TrimSpace(cancelURL) == "" {
		return fmt.Errorf("%w: session_success_url and session_cancel_url are required", ErrInvalidArgument)
	}
	return nil
}

func withCheckoutSessionPlaceholder(successURL string) string {
	if successURL == "" || strings.Contains(successURL, "{CHECKOUT_SESSION_ID}") {
		return successURL
	}
	separator := "?"
	if strings.Contains(successURL, "?") {
		separator = "&"
	}
	return successURL + separator + "session_id={CHECKOUT_SESSION_ID}"
}

// EnsureStripe prepares the SDK for a request.
func (s *Service) EnsureStripe() error {
	if s == nil || s.cfg.StripeAPIKey == "" {
		return errors.New("billing is not configured")
	}
	s.initStripe()
	return nil
}

// EnsureCustomer returns the Stripe customer of a tenant, creating one when absent.
func (s *Service) EnsureCustomer(ctx context.Context, tenantID string) (string, error) {
	current, err := s.Subscription(ctx, tenantID)
	if err != nil {
		return "", err
	}
	if current != nil && current.CustomerID != "" {
		return current.CustomerID, nil
	}
	if err := s.EnsureStripe(); err != nil {
		return "", err
	}
	created, err := customer.New(&stripe.CustomerParams{
		Metadata: map[string]string{"tenant_id": tenantID},
	})
	if err != nil {
		return "", fmt.Errorf("create stripe customer: %w", err)
	}
	row := &entity.BillingSubscription{
		ID:         common.GenerateUUID(),
		TenantID:   tenantID,
		PlanName:   config.BillingPlanTrialName,
		Status:     "active",
		CustomerID: created.ID,
	}
	if trial := s.TrialPlan(); trial != nil {
		row.PlanName = trial.Name
	}
	if product, err := s.productDAO.GetLatestByName(ctx, s.db, row.PlanName); err == nil {
		row.ProductID = product.ID
	}
	if current != nil {
		row = current
		row.CustomerID = created.ID
	}
	if err := s.subscriptionDAO.UpsertSubscription(ctx, s.db, row); err != nil {
		return "", err
	}
	return created.ID, nil
}

// CreateSubscriptionCheckout starts a Stripe Checkout session for a new plan.
func (s *Service) CreateSubscriptionCheckout(ctx context.Context, tenantID, priceID, setupIntentID, successURL, cancelURL string) (map[string]interface{}, error) {
	if err := requireCheckoutURLs(successURL, cancelURL); err != nil {
		return nil, err
	}
	if err := s.EnsureStripe(); err != nil {
		return nil, err
	}
	customerID, err := s.EnsureCustomer(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(setupIntentID) != "" {
		if err := s.applySetupIntentPaymentMethod(ctx, tenantID, setupIntentID); err != nil {
			return nil, err
		}
	}
	// The success page resolves the purchased plan from session metadata, so
	// the plan name and price must travel with the session (python parity).
	metadata := map[string]string{
		"tenant_id":    tenantID,
		"price_type":   "subscription",
		"product_type": "subscription",
		"price_id":     priceID,
	}
	if planName, ok := s.ResolvePlanName(priceID); ok {
		metadata["product_name"] = planName
	}
	params := &stripe.CheckoutSessionParams{
		Mode:              stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		Customer:          stripe.String(customerID),
		SuccessURL:        stripe.String(withCheckoutSessionPlaceholder(successURL)),
		CancelURL:         stripe.String(cancelURL),
		ClientReferenceID: stripe.String(tenantID),
		Metadata:          metadata,
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{Price: stripe.String(priceID), Quantity: stripe.Int64(1)},
		},
		SubscriptionData: &stripe.CheckoutSessionSubscriptionDataParams{
			Metadata: metadata,
		},
	}
	session, err := checkoutsession.New(params)
	if err != nil {
		return nil, fmt.Errorf("create checkout session: %w", err)
	}
	return map[string]interface{}{"redirect_to": session.URL, "session_id": session.ID}, nil
}

// CreateAddonCheckout starts a Checkout session for an add-on price.
func (s *Service) CreateAddonCheckout(ctx context.Context, tenantID, priceID string, quantity int64, successURL, cancelURL string) (map[string]interface{}, error) {
	if err := requireCheckoutURLs(successURL, cancelURL); err != nil {
		return nil, err
	}
	if quantity <= 0 {
		quantity = 1
	}
	if err := s.EnsureStripe(); err != nil {
		return nil, err
	}
	customerID, err := s.EnsureCustomer(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	session, err := checkoutsession.New(&stripe.CheckoutSessionParams{
		Mode:       stripe.String(string(stripe.CheckoutSessionModePayment)),
		Customer:   stripe.String(customerID),
		SuccessURL: stripe.String(withCheckoutSessionPlaceholder(successURL)),
		CancelURL:  stripe.String(cancelURL),
		Metadata:   map[string]string{"tenant_id": tenantID},
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{Price: stripe.String(priceID), Quantity: stripe.Int64(quantity)},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create addon checkout: %w", err)
	}
	return map[string]interface{}{"redirect_to": session.URL, "session_id": session.ID}, nil
}

// CreatePointsCheckout starts a one-off Checkout session for points.
func (s *Service) CreatePointsCheckout(ctx context.Context, tenantID string, quantity int64, successURL, cancelURL string) (map[string]interface{}, error) {
	if err := requireCheckoutURLs(successURL, cancelURL); err != nil {
		return nil, err
	}
	if quantity <= 0 {
		return nil, fmt.Errorf("quantity must be positive")
	}
	priceID := strings.TrimSpace(s.cfg.PointsRecharge.PriceID)
	if priceID == "" {
		return nil, fmt.Errorf("points recharge is not configured")
	}
	pointsPerUnit := s.cfg.PointsRecharge.PointsPerUnit
	if pointsPerUnit == 0 {
		pointsPerUnit = 100
	}
	if err := s.EnsureStripe(); err != nil {
		return nil, err
	}
	customerID, err := s.EnsureCustomer(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	metadata := map[string]string{
		"tenant_id":     tenantID,
		"payment_type":  "points_recharge",
		"points_amount": fmt.Sprintf("%d", quantity*pointsPerUnit),
	}
	session, err := checkoutsession.New(&stripe.CheckoutSessionParams{
		Mode:       stripe.String(string(stripe.CheckoutSessionModePayment)),
		Customer:   stripe.String(customerID),
		SuccessURL: stripe.String(withCheckoutSessionPlaceholder(successURL)),
		CancelURL:  stripe.String(cancelURL),
		Metadata:   metadata,
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{Price: stripe.String(priceID), Quantity: stripe.Int64(quantity)},
		},
		PaymentIntentData: &stripe.CheckoutSessionPaymentIntentDataParams{Metadata: metadata},
	})
	if err != nil {
		return nil, fmt.Errorf("create points checkout: %w", err)
	}
	return map[string]interface{}{"checkout_url": session.URL, "session_id": session.ID}, nil
}

// CreateSetupIntent returns a SetupIntent client secret for the tenant.
//
// returnURL is where Stripe sends the customer back after a redirect-based
// payment method (3DS, wallets, bank redirects) authenticates or is cancelled.
// Stripe refuses to confirm such a SetupIntent without one, so the intent
// carries the URL as well as the confirmation call.
func (s *Service) CreateSetupIntent(ctx context.Context, tenantID, returnURL string) (map[string]interface{}, error) {
	if err := s.EnsureStripe(); err != nil {
		return nil, err
	}
	customerID, err := s.EnsureCustomer(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	params := &stripe.SetupIntentParams{
		Customer: stripe.String(customerID),
		Usage:    stripe.String("off_session"),
		Metadata: map[string]string{"tenant_id": tenantID},
		AutomaticPaymentMethods: &stripe.SetupIntentAutomaticPaymentMethodsParams{
			Enabled: stripe.Bool(true),
		},
	}
	if url := strings.TrimSpace(returnURL); url != "" {
		params.ReturnURL = stripe.String(url)
	}
	intent, err := setupintent.New(params)
	if err != nil {
		return nil, fmt.Errorf("create setup intent: %w", err)
	}
	return map[string]interface{}{"client_secret": intent.ClientSecret, "setup_intent_id": intent.ID}, nil
}

// CreatePortalSession returns a Stripe Billing Portal URL.
func (s *Service) CreatePortalSession(ctx context.Context, tenantID, returnURL string) (map[string]interface{}, error) {
	if err := s.EnsureStripe(); err != nil {
		return nil, err
	}
	customerID, err := s.EnsureCustomer(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	portal, err := billingportal.New(&stripe.BillingPortalSessionParams{
		Customer:  stripe.String(customerID),
		ReturnURL: stripe.String(returnURL),
	})
	if err != nil {
		return nil, fmt.Errorf("create portal session: %w", err)
	}
	return map[string]interface{}{"redirect_to": portal.URL}, nil
}

// PreviewPlanChange returns the upcoming invoice for a target price.
// PreviewPlanChange previews the prorated amount due today for a plan change.
// It supports tenants without a modifiable subscription by previewing the
// first charge of a brand-new subscription.
func (s *Service) PreviewPlanChange(ctx context.Context, tenantID, priceID string) (map[string]interface{}, error) {
	if err := s.EnsureStripe(); err != nil {
		return nil, err
	}
	customerID, err := s.EnsureCustomer(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	row, err := s.Subscription(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	var current *stripe.Subscription
	modifiable := false
	if row != nil && row.SubscriptionID != "" {
		current, err = subscription.Get(row.SubscriptionID, nil)
		if err != nil {
			return nil, fmt.Errorf("retrieve subscription: %w", err)
		}
		if current != nil && current.Items != nil && len(current.Items.Data) > 0 {
			switch current.Status {
			case stripe.SubscriptionStatusActive, stripe.SubscriptionStatusPastDue:
				modifiable = true
			}
		}
	}
	if modifiable && s.previewAsNewSubscription(row, current, priceID) {
		modifiable = false
	}

	var preview *stripe.Invoice
	if modifiable {
		planItem, storageItem := splitSubscriptionItems(current, s.storagePriceID())
		if planItem == nil || planItem.Price == nil {
			return nil, fmt.Errorf("plan subscription item not found")
		}
		quantity := planItem.Quantity
		if quantity < 1 {
			quantity = 1
		}
		items := []*stripe.InvoiceCreatePreviewSubscriptionDetailsItemParams{
			{ID: stripe.String(planItem.ID), Price: stripe.String(priceID), Quantity: stripe.Int64(quantity)},
		}
		if storageItem != nil && storageItem.Price != nil {
			items = append(items, &stripe.InvoiceCreatePreviewSubscriptionDetailsItemParams{
				ID:       stripe.String(storageItem.ID),
				Price:    stripe.String(storageItem.Price.ID),
				Quantity: stripe.Int64(storageItem.Quantity),
			})
		}
		preview, err = invoice.CreatePreview(&stripe.InvoiceCreatePreviewParams{
			Customer:     stripe.String(customerID),
			Subscription: stripe.String(row.SubscriptionID),
			SubscriptionDetails: &stripe.InvoiceCreatePreviewSubscriptionDetailsParams{
				Items:             items,
				ProrationBehavior: stripe.String("always_invoice"),
			},
		})
	} else {
		preview, err = invoice.CreatePreview(&stripe.InvoiceCreatePreviewParams{
			Customer: stripe.String(customerID),
			SubscriptionDetails: &stripe.InvoiceCreatePreviewSubscriptionDetailsParams{
				Items: []*stripe.InvoiceCreatePreviewSubscriptionDetailsItemParams{
					{Price: stripe.String(priceID), Quantity: stripe.Int64(1)},
				},
				ProrationBehavior: stripe.String("always_invoice"),
			},
		})
	}
	if err != nil {
		return nil, fmt.Errorf("preview invoice: %w", err)
	}

	reusable := false
	if modifiable {
		reusable = s.hasReusablePaymentMethod(current)
	}
	return map[string]interface{}{
		"amount_due_today":            float64(preview.Total) / 100,
		"currency":                    string(preview.Currency),
		"invoice_preview":             preview,
		"has_reusable_payment_method": reusable,
		"stripe_publishable_key":      s.cfg.StripePublishableKey,
	}, nil
}

// previewAsNewSubscription reports whether a Trial tenant moving to a paid plan
// must be previewed as a brand-new subscription instead of a price change.
func (s *Service) previewAsNewSubscription(row *entity.BillingSubscription, current *stripe.Subscription, priceID string) bool {
	if row == nil || !strings.EqualFold(row.PlanName, config.BillingPlanTrialName) {
		return false
	}
	target := s.cfg.PriceIDToPlan[priceID]
	return !strings.EqualFold(target, config.BillingPlanTrialName)
}

// storagePriceID returns the configured storage add-on price id.
func (s *Service) storagePriceID() string {
	plan, ok := s.cfg.Plan(config.StorageProductName)
	if !ok || len(plan.PriceIDs) == 0 {
		return ""
	}
	return plan.PriceIDs[0]
}

// splitSubscriptionItems separates the plan item from the storage add-on item.
func splitSubscriptionItems(sub *stripe.Subscription, storagePriceID string) (*stripe.SubscriptionItem, *stripe.SubscriptionItem) {
	if sub == nil || sub.Items == nil {
		return nil, nil
	}
	var planItem, storageItem *stripe.SubscriptionItem
	for _, item := range sub.Items.Data {
		if item == nil || item.Price == nil {
			continue
		}
		if storagePriceID != "" && item.Price.ID == storagePriceID {
			storageItem = item
			continue
		}
		if planItem == nil {
			planItem = item
		}
	}
	return planItem, storageItem
}

// ChangePlan upgrades immediately or schedules a period-end downgrade.
func (s *Service) ChangePlan(ctx context.Context, tenantID, priceID, setupIntentID string) (map[string]interface{}, error) {
	subscriptionRow, err := s.Subscription(ctx, tenantID)
	if err != nil || subscriptionRow == nil || subscriptionRow.SubscriptionID == "" {
		return nil, fmt.Errorf("no active subscription")
	}
	if err := s.EnsureStripe(); err != nil {
		return nil, err
	}
	targetPlan, ok := s.cfg.PriceIDToPlan[priceID]
	if !ok {
		return nil, fmt.Errorf("unknown price %s", priceID)
	}
	currentPlan, _ := s.cfg.Plan(subscriptionRow.PlanName)
	targetInfo, _ := s.cfg.Plan(targetPlan)

	// Trial has no real Stripe price (its price id is a local placeholder), so
	// falling back to Trial is a period-end cancellation rather than a price
	// change.
	if strings.EqualFold(targetPlan, config.BillingPlanTrialName) || strings.HasPrefix(priceID, "price_trail") {
		if err := s.scheduleTrialFallback(ctx, subscriptionRow); err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"scheduled": true, "target_plan_name": config.BillingPlanTrialName,
			"subscription_id": subscriptionRow.SubscriptionID, "payment_state": "scheduled",
			"scheduled_change": map[string]interface{}{"plan_name": config.BillingPlanTrialName},
		}, nil
	}

	// Downgrade (lower priority) to a lower paid plan takes effect at the end
	// of the current period.
	if currentPlan != nil && targetInfo != nil && targetInfo.Priority < currentPlan.Priority {
		if err := s.scheduleDowngrade(ctx, subscriptionRow, priceID, targetPlan); err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"scheduled": true, "target_plan_name": targetPlan,
			"subscription_id": subscriptionRow.SubscriptionID, "payment_state": "scheduled",
			"scheduled_change": map[string]interface{}{"plan_name": targetPlan},
		}, nil
	}

	if strings.TrimSpace(setupIntentID) != "" {
		if err := s.applySetupIntentPaymentMethod(ctx, tenantID, setupIntentID); err != nil {
			return nil, err
		}
	}
	sub, err := subscription.Get(subscriptionRow.SubscriptionID, nil)
	if err != nil {
		return nil, fmt.Errorf("retrieve subscription: %w", err)
	}
	if len(sub.Items.Data) == 0 {
		return nil, fmt.Errorf("subscription has no items")
	}
	// An upgrade produces a proration invoice; without a reusable payment
	// method Stripe rejects the update outright, so tell the client to collect
	// one first instead of surfacing a Stripe 400.
	if !s.hasReusablePaymentMethod(sub) {
		return map[string]interface{}{
			"requires_payment_method_setup": true,
			"subscription_id":               subscriptionRow.SubscriptionID,
			"plan_name":                     subscriptionRow.PlanName,
			"target_plan_name":              targetPlan,
			"payment_state":                 "requires_payment_method",
		}, nil
	}
	planItem, _ := splitSubscriptionItems(sub, s.storagePriceID())
	if planItem == nil || planItem.Price == nil {
		return nil, fmt.Errorf("plan subscription item not found")
	}
	// Stripe replaces the full item list: keep every non-plan item untouched so
	// the storage add-on is not dropped by a plan change.
	items := make([]*stripe.SubscriptionItemsParams, 0, len(sub.Items.Data))
	for _, item := range sub.Items.Data {
		if item == nil || item.Price == nil {
			continue
		}
		if item.ID == planItem.ID {
			items = append(items, &stripe.SubscriptionItemsParams{ID: stripe.String(item.ID), Price: stripe.String(priceID)})
			continue
		}
		items = append(items, &stripe.SubscriptionItemsParams{ID: stripe.String(item.ID), Quantity: stripe.Int64(item.Quantity)})
	}
	updated, err := subscription.Update(subscriptionRow.SubscriptionID, &stripe.SubscriptionParams{
		Items:             items,
		ProrationBehavior: stripe.String("always_invoice"),
		PaymentBehavior:   stripe.String("pending_if_incomplete"),
		Expand:            []*string{stripe.String("latest_invoice")},
	})
	if err != nil {
		return nil, fmt.Errorf("update subscription: %w", err)
	}
	state := NormalizePaymentState(
		latestInvoiceField(updated.LatestInvoice, "status"),
		latestInvoiceField(updated.LatestInvoice, "payment_intent.status"),
		string(updated.Status),
		latestInvoiceAmountDue(updated.LatestInvoice),
		false,
	)
	result := map[string]interface{}{
		"scheduled":       false,
		"plan_name":       targetPlan,
		"subscription_id": subscriptionRow.SubscriptionID,
		"payment_state":   string(state),
		"invoice_id":      latestInvoiceField(updated.LatestInvoice, "id"),
		"invoice_url":     latestInvoiceField(updated.LatestInvoice, "hosted_invoice_url"),
		"amount_cents":    latestInvoiceAmountDue(updated.LatestInvoice),
		"currency":        strings.ToUpper(latestInvoiceField(updated.LatestInvoice, "currency")),
	}
	if state == PaymentStateRequiresAction {
		result["redirect_to"] = latestInvoiceField(updated.LatestInvoice, "hosted_invoice_url")
	}
	return result, nil
}

func (s *Service) scheduleDowngrade(ctx context.Context, subscriptionRow *entity.BillingSubscription, priceID, targetPlan string) error {
	sub, err := subscription.Get(subscriptionRow.SubscriptionID, nil)
	if err != nil {
		return fmt.Errorf("retrieve subscription: %w", err)
	}
	schedule, created, err := s.ensureSchedule(subscriptionRow)
	if err != nil {
		return err
	}
	release := func() {
		if created {
			_, _ = subscriptionschedule.Release(schedule.ID, nil)
		}
	}
	if schedule.CurrentPhase == nil {
		release()
		return fmt.Errorf("subscription schedule has no current phase")
	}

	planItem, _ := splitSubscriptionItems(sub, s.storagePriceID())
	if planItem == nil {
		release()
		return fmt.Errorf("plan subscription item not found")
	}
	currentItems := make([]*stripe.SubscriptionSchedulePhaseItemParams, 0, len(sub.Items.Data))
	targetItems := make([]*stripe.SubscriptionSchedulePhaseItemParams, 0, len(sub.Items.Data))
	for _, item := range sub.Items.Data {
		if item == nil || item.Price == nil {
			continue
		}
		currentItems = append(currentItems, &stripe.SubscriptionSchedulePhaseItemParams{
			Price:    stripe.String(item.Price.ID),
			Quantity: stripe.Int64(item.Quantity),
		})
		targetPrice := item.Price.ID
		if item.ID == planItem.ID {
			targetPrice = priceID
		}
		targetItems = append(targetItems, &stripe.SubscriptionSchedulePhaseItemParams{
			Price:    stripe.String(targetPrice),
			Quantity: stripe.Int64(item.Quantity),
		})
	}

	start := schedule.CurrentPhase.StartDate
	end := schedule.CurrentPhase.EndDate
	_, err = subscriptionschedule.Update(schedule.ID, &stripe.SubscriptionScheduleParams{
		EndBehavior: stripe.String("release"),
		Phases: []*stripe.SubscriptionSchedulePhaseParams{
			{
				StartDate: stripe.Int64(start),
				EndDate:   stripe.Int64(end),
				Items:     currentItems,
			},
			{
				StartDate: stripe.Int64(end),
				Items:     targetItems,
			},
		},
	})
	if err != nil {
		release()
		return fmt.Errorf("update subscription schedule: %w", err)
	}

	return s.subscriptionDAO.UpdatePendingPlanChange(ctx, s.db, subscriptionRow.TenantID, &targetPlan, stringPtr(schedule.ID))
}

// scheduleTrialFallback cancels the paid subscription at the period end so the
// tenant falls back to Trial when the Stripe deletion webhook arrives.
func (s *Service) scheduleTrialFallback(ctx context.Context, row *entity.BillingSubscription) error {
	// Stripe refuses to update cancellation behaviour while a subscription
	// schedule owns the subscription, so drop any pending downgrade first and
	// turn the fallback into a plain period-end cancellation.
	if err := s.releaseSchedulesForSubscription(ctx, row); err != nil {
		return fmt.Errorf("release schedule before trial fallback: %w", err)
	}
	if _, err := subscription.Update(row.SubscriptionID, &stripe.SubscriptionParams{
		CancelAtPeriodEnd: stripe.Bool(true),
	}); err != nil {
		return fmt.Errorf("schedule trial fallback: %w", err)
	}
	// The schedule is gone and the fallback replaces both the plan and any
	// pending storage target; write only those columns so a webhook that
	// converged this row meanwhile is preserved.
	return s.subscriptionDAO.UpdatePendingTrialFallback(ctx, s.db, row.TenantID, config.BillingPlanTrialName)
}

// SetStorageTarget increases the add-on immediately or defers a decrease.
func (s *Service) SetStorageTarget(ctx context.Context, tenantID string, targetBytes int64, setupIntentID, successURL, cancelURL string) (map[string]interface{}, error) {
	if targetBytes < 0 {
		return nil, fmt.Errorf("%w: target storage must be >= 0", ErrInvalidArgument)
	}
	unit := s.cfg.StorageBytesPerUnit()
	if unit <= 0 {
		return nil, fmt.Errorf("storage add-on plan is not configured")
	}
	if targetBytes%unit != 0 {
		return nil, fmt.Errorf("%w: target storage must be a multiple of %d bytes", ErrInvalidArgument, unit)
	}
	subscriptionRow, err := s.Subscription(ctx, tenantID)
	if err != nil || subscriptionRow == nil {
		return nil, fmt.Errorf("no subscription for tenant")
	}
	if err := s.EnsureStripe(); err != nil {
		return nil, err
	}

	currentAddon := int64(0)
	if subscriptionRow.AddonStorageBytes != nil {
		currentAddon = *subscriptionRow.AddonStorageBytes
	}
	// Trial tenants may keep or cancel an existing add-on but cannot buy or
	// resume one (python parity).
	if strings.EqualFold(subscriptionRow.PlanName, config.BillingPlanTrialName) && targetBytes > currentAddon {
		return nil, fmt.Errorf("%w: Trial plan does not support storage add-on.", ErrInvalidArgument)
	}
	quantity := targetBytes / unit

	if targetBytes < currentAddon {
		if err := s.scheduleStorageQuantityChange(ctx, subscriptionRow, targetBytes/unit); err != nil {
			return nil, err
		}
		if err := s.subscriptionDAO.UpdatePendingStorageTarget(ctx, s.db, tenantID, &targetBytes, false); err != nil {
			return nil, err
		}
		return map[string]interface{}{"scheduled": true, "target_storage_bytes": targetBytes}, nil
	}
	if targetBytes == currentAddon {
		// Reselecting the current quantity cancels a pending decrease, but only
		// when one exists; an independent plan schedule must stay intact.
		if subscriptionRow.TargetStorageBytes != nil {
			if err := s.releaseStorageSchedules(ctx, subscriptionRow); err != nil {
				return nil, err
			}
		}
		if err := s.subscriptionDAO.UpdatePendingStorageTarget(ctx, s.db, tenantID, nil, subscriptionRow.TargetPlanName == nil); err != nil {
			return nil, err
		}
		return map[string]interface{}{"scheduled": false, "addon_storage_bytes": currentAddon}, nil
	}

	storagePlan, ok := s.cfg.Plan(config.StorageProductName)
	if !ok || len(storagePlan.PriceIDs) == 0 {
		return nil, fmt.Errorf("storage price is not configured")
	}
	storagePriceID := storagePlan.PriceIDs[0]
	sub, err := subscription.Get(subscriptionRow.SubscriptionID, nil)
	if err != nil {
		return nil, fmt.Errorf("retrieve subscription: %w", err)
	}
	if strings.TrimSpace(setupIntentID) != "" {
		if err := s.applySetupIntentPaymentMethod(ctx, tenantID, setupIntentID); err != nil {
			return nil, err
		}
		if refreshed, refreshErr := subscription.Get(subscriptionRow.SubscriptionID, nil); refreshErr == nil {
			sub = refreshed
		}
	}
	if !s.hasReusablePaymentMethod(sub) {
		// The increase produces an invoice, so a reusable payment method is
		// required before the mutation. Send the client through setup Checkout
		// and let it resubmit with the confirmed setup intent.
		session, setupErr := s.createStorageSetupCheckout(ctx, tenantID, subscriptionRow, storagePriceID, targetBytes, successURL, cancelURL)
		if setupErr != nil {
			return nil, setupErr
		}
		result := storageChangeResult(currentAddon, targetBytes)
		result["redirect_to"] = session.URL
		result["requires_payment_method_setup"] = true
		return result, nil
	}

	// Stripe replaces the full item list, so the plan item must be preserved.
	items := make([]*stripe.SubscriptionItemsParams, 0, len(sub.Items.Data)+1)
	hasStorageItem := false
	for _, item := range sub.Items.Data {
		if item.Price != nil && item.Price.ID == storagePriceID {
			hasStorageItem = true
			items = append(items, &stripe.SubscriptionItemsParams{ID: stripe.String(item.ID), Quantity: stripe.Int64(quantity)})
			continue
		}
		items = append(items, &stripe.SubscriptionItemsParams{ID: stripe.String(item.ID), Quantity: stripe.Int64(item.Quantity)})
	}
	if !hasStorageItem {
		items = append(items, &stripe.SubscriptionItemsParams{Price: stripe.String(storagePriceID), Quantity: stripe.Int64(quantity)})
	}
	params := &stripe.SubscriptionParams{
		ProrationBehavior:           stripe.String("always_invoice"),
		PaymentBehavior:             stripe.String("pending_if_incomplete"),
		BillingCycleAnchorUnchanged: stripe.Bool(true),
		Items:                       items,
		Expand:                      []*string{stripe.String("latest_invoice")},
	}
	params.SetIdempotencyKey(fmt.Sprintf("billing:%s:storage_change:%s:%d", tenantID, subscriptionRow.SubscriptionID, targetBytes))
	updated, err := subscription.Update(subscriptionRow.SubscriptionID, params)
	if err != nil {
		return nil, fmt.Errorf("update storage quantity: %w", err)
	}

	invoice := updated.LatestInvoice
	state := NormalizePaymentState(
		latestInvoiceField(invoice, "status"),
		latestInvoiceField(invoice, "payment_intent.status"),
		string(updated.Status),
		latestInvoiceAmountDue(invoice),
		false,
	)
	if state == PaymentStatePaid {
		// Store what Stripe now bills rather than the requested target: the
		// authoritative quantity lives on the updated subscription item, and
		// the pending target is gone once the change is live.
		if itemID, addonBytes, ok := s.storageAddonFromStripeSubscription(updated); ok {
			if err := s.subscriptionDAO.UpdateStorageAddon(ctx, s.db, tenantID, itemID, addonBytes, nil); err != nil {
				return nil, err
			}
		}
	}
	result := storageChangeResult(currentAddon, targetBytes)
	result["payment_state"] = string(state)
	result["invoice_id"] = latestInvoiceField(invoice, "id")
	result["invoice_url"] = latestInvoiceField(invoice, "hosted_invoice_url")
	result["invoice_pdf_url"] = latestInvoiceField(invoice, "invoice_pdf")
	result["amount_cents"] = latestInvoiceAmountDue(invoice)
	result["currency"] = strings.ToUpper(latestInvoiceField(invoice, "currency"))
	if state == PaymentStateRequiresAction {
		result["redirect_to"] = latestInvoiceField(invoice, "hosted_invoice_url")
	}
	return result, nil
}

// storageAddonFromStripeSubscription returns the storage item id and byte size
// billed by a Stripe subscription. It mirrors StorageAddonFromSubscription,
// which works on the webhook payload type.
func (s *Service) storageAddonFromStripeSubscription(sub *stripe.Subscription) (*string, int64, bool) {
	storagePlan, ok := s.cfg.Plan(config.StorageProductName)
	if !ok || len(storagePlan.PriceIDs) == 0 || sub == nil {
		return nil, 0, false
	}
	for _, item := range sub.Items.Data {
		if item == nil || item.Price == nil || item.Price.ID != storagePlan.PriceIDs[0] {
			continue
		}
		id := item.ID
		return &id, item.Quantity * storagePlan.QuotaStorage, true
	}
	return nil, 0, false
}

func storageChangeResult(currentAddon, targetBytes int64) map[string]interface{} {
	return map[string]interface{}{
		"addon_storage_bytes":  currentAddon,
		"target_storage_bytes": targetBytes,
		"product_type":         "storage",
	}
}

func latestInvoiceField(invoice *stripe.Invoice, field string) string {
	if invoice == nil {
		return ""
	}
	switch field {
	case "status":
		return string(invoice.Status)
	case "id":
		return invoice.ID
	case "hosted_invoice_url":
		return invoice.HostedInvoiceURL
	case "invoice_pdf":
		return invoice.InvoicePDF
	case "currency":
		return string(invoice.Currency)
	case "payment_intent.status":
		// v82 exposes the invoice payment through the confirmation secret
		// rather than an expanded payment intent.
		if invoice.ConfirmationSecret != nil && invoice.ConfirmationSecret.ClientSecret != "" {
			return "requires_action"
		}
	}
	return ""
}

func latestInvoiceAmountDue(invoice *stripe.Invoice) *int64 {
	if invoice == nil {
		return nil
	}
	amount := invoice.AmountDue
	return &amount
}

// hasReusablePaymentMethod reports whether an invoice can be charged without
// collecting card details again. Subscription-level and customer-level
// defaults are both authoritative.
func (s *Service) hasReusablePaymentMethod(sub *stripe.Subscription) bool {
	if sub == nil {
		return false
	}
	if sub.DefaultPaymentMethod != nil && sub.DefaultPaymentMethod.ID != "" {
		return true
	}
	if sub.Customer == nil || sub.Customer.ID == "" {
		return false
	}
	row, err := customer.Get(sub.Customer.ID, nil)
	if err != nil || row == nil || row.InvoiceSettings == nil {
		return false
	}
	return row.InvoiceSettings.DefaultPaymentMethod != nil && row.InvoiceSettings.DefaultPaymentMethod.ID != ""
}

// applySetupIntentPaymentMethod validates a tenant-owned, confirmed setup
// intent and makes its payment method the customer default.
func (s *Service) applySetupIntentPaymentMethod(ctx context.Context, tenantID, setupIntentID string) error {
	if err := s.EnsureStripe(); err != nil {
		return err
	}
	customerID, err := s.EnsureCustomer(ctx, tenantID)
	if err != nil {
		return err
	}
	intent, err := setupintent.Get(setupIntentID, nil)
	if err != nil {
		return fmt.Errorf("retrieve setup intent: %w", err)
	}
	if intent.Customer == nil || intent.Customer.ID != customerID {
		return fmt.Errorf("setup intent does not belong to tenant")
	}
	if intent.Status != stripe.SetupIntentStatusSucceeded {
		return fmt.Errorf("setup intent is not confirmed")
	}
	if intent.PaymentMethod == nil || intent.PaymentMethod.ID == "" {
		return fmt.Errorf("setup intent has no payment method")
	}
	if _, err := customer.Update(customerID, &stripe.CustomerParams{
		InvoiceSettings: &stripe.CustomerInvoiceSettingsParams{
			DefaultPaymentMethod: stripe.String(intent.PaymentMethod.ID),
		},
	}); err != nil {
		return fmt.Errorf("attach setup intent payment method: %w", err)
	}
	return nil
}

// createStorageSetupCheckout starts a setup-mode Checkout session so the
// tenant can store a reusable payment method before a storage increase.
func (s *Service) createStorageSetupCheckout(ctx context.Context, tenantID string, row *entity.BillingSubscription, storagePriceID string, targetBytes int64, successURL, cancelURL string) (*stripe.CheckoutSession, error) {
	if err := s.EnsureStripe(); err != nil {
		return nil, err
	}
	customerID, err := s.EnsureCustomer(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return checkoutsession.New(&stripe.CheckoutSessionParams{
		Mode:       stripe.String(string(stripe.CheckoutSessionModeSetup)),
		Customer:   stripe.String(customerID),
		SuccessURL: stripe.String(withCheckoutSessionPlaceholder(successURL)),
		CancelURL:  stripe.String(cancelURL),
		Metadata: map[string]string{
			"tenant_id":            tenantID,
			"product_type":         "storage",
			"price_id":             storagePriceID,
			"subscription_id":      row.SubscriptionID,
			"target_storage_bytes": fmt.Sprintf("%d", targetBytes),
		},
	})
}

// ensureSchedule returns the subscription's active schedule, creating one when
// absent. Reusing the live schedule keeps a single pending change alive instead
// of failing with Stripe's "subscription already has a schedule" error.
func (s *Service) ensureSchedule(row *entity.BillingSubscription) (*stripe.SubscriptionSchedule, bool, error) {
	if row.CustomerID != "" {
		iter := subscriptionschedule.List(&stripe.SubscriptionScheduleListParams{
			Customer: stripe.String(row.CustomerID),
		})
		for iter.Next() {
			existing := iter.SubscriptionSchedule()
			if existing == nil || existing.Status != "active" {
				continue
			}
			if existing.Subscription.ID != "" && row.SubscriptionID != "" && existing.Subscription.ID != row.SubscriptionID {
				continue
			}
			full, err := subscriptionschedule.Get(existing.ID, &stripe.SubscriptionScheduleParams{})
			if err != nil {
				return nil, false, fmt.Errorf("retrieve subscription schedule: %w", err)
			}
			return full, false, nil
		}
		if err := iter.Err(); err != nil {
			return nil, false, err
		}
	}
	created, err := subscriptionschedule.New(&stripe.SubscriptionScheduleParams{
		FromSubscription: stripe.String(row.SubscriptionID),
	})
	if err != nil {
		return nil, false, fmt.Errorf("create subscription schedule: %w", err)
	}
	return created, true, nil
}

// releaseStorageSchedules cancels only the storage portion of a pending
// subscription change. Schedules that also carry plan phases keep those phases
// so an independent plan downgrade stays scheduled.
func (s *Service) releaseStorageSchedules(ctx context.Context, row *entity.BillingSubscription) error {
	storagePlan, ok := s.cfg.Plan(config.StorageProductName)
	if !ok || len(storagePlan.PriceIDs) == 0 || row.CustomerID == "" {
		return nil
	}
	storagePriceID := storagePlan.PriceIDs[0]
	currentQuantity := int64(0)
	if row.AddonStorageBytes != nil && s.cfg.StorageBytesPerUnit() > 0 {
		currentQuantity = *row.AddonStorageBytes / s.cfg.StorageBytesPerUnit()
	}
	if err := s.EnsureStripe(); err != nil {
		return err
	}
	iter := subscriptionschedule.List(&stripe.SubscriptionScheduleListParams{
		Customer: stripe.String(row.CustomerID),
	})
	for iter.Next() {
		schedule := iter.SubscriptionSchedule()
		if schedule == nil || schedule.Status != "active" {
			continue
		}
		if schedule.Subscription.ID != "" && row.SubscriptionID != "" && schedule.Subscription.ID != row.SubscriptionID {
			continue
		}
		full, err := subscriptionschedule.Get(schedule.ID, &stripe.SubscriptionScheduleParams{})
		if err != nil {
			return err
		}
		phases, removed := stripStoragePhases(full.Phases, storagePriceID, currentQuantity)
		if !removed {
			continue
		}
		if len(phases) == 0 {
			if _, err := subscriptionschedule.Release(schedule.ID, nil); err != nil {
				return err
			}
			continue
		}
		if _, err := subscriptionschedule.Update(schedule.ID, &stripe.SubscriptionScheduleParams{
			EndBehavior: stripe.String("release"),
			Phases:      phases,
		}); err != nil {
			return err
		}
	}
	return iter.Err()
}

// stripStoragePhases rebuilds phases with the storage item pinned to the
// current paid quantity, cancelling a pending reduction without revoking the
// capacity the tenant already pays for. It reports whether storage was present.
func stripStoragePhases(phases []*stripe.SubscriptionSchedulePhase, storagePriceID string, currentQuantity int64) ([]*stripe.SubscriptionSchedulePhaseParams, bool) {
	removed := false
	out := make([]*stripe.SubscriptionSchedulePhaseParams, 0, len(phases))
	for _, phase := range phases {
		if phase == nil {
			continue
		}
		items := make([]*stripe.SubscriptionSchedulePhaseItemParams, 0, len(phase.Items)+1)
		hasStorage := false
		for _, item := range phase.Items {
			if item == nil || item.Price == nil {
				continue
			}
			if item.Price.ID == storagePriceID {
				removed = true
				hasStorage = true
				items = append(items, &stripe.SubscriptionSchedulePhaseItemParams{
					Price:    stripe.String(item.Price.ID),
					Quantity: stripe.Int64(currentQuantity),
				})
				continue
			}
			items = append(items, &stripe.SubscriptionSchedulePhaseItemParams{
				Price:    stripe.String(item.Price.ID),
				Quantity: stripe.Int64(item.Quantity),
			})
		}
		if currentQuantity > 0 && !hasStorage {
			// A pending reduction removes the item from the future phase; a
			// cancellation must restore the capacity the tenant still pays for.
			items = append(items, &stripe.SubscriptionSchedulePhaseItemParams{
				Price:    stripe.String(storagePriceID),
				Quantity: stripe.Int64(currentQuantity),
			})
			removed = true
		}
		if len(items) == 0 {
			continue
		}
		params := &stripe.SubscriptionSchedulePhaseParams{Items: items}
		if phase.StartDate > 0 {
			params.StartDate = stripe.Int64(phase.StartDate)
		}
		if phase.EndDate > 0 {
			params.EndDate = stripe.Int64(phase.EndDate)
		}
		out = append(out, params)
	}
	return out, removed
}

// releaseSchedulesForSubscription releases active schedules attached to a
// subscription.
func (s *Service) releaseSchedulesForSubscription(ctx context.Context, row *entity.BillingSubscription) error {
	if row.CustomerID == "" {
		return nil
	}
	if err := s.EnsureStripe(); err != nil {
		return err
	}
	iter := subscriptionschedule.List(&stripe.SubscriptionScheduleListParams{
		Customer: stripe.String(row.CustomerID),
	})
	for iter.Next() {
		schedule := iter.SubscriptionSchedule()
		if schedule == nil || schedule.Status != "active" {
			continue
		}
		if schedule.Subscription.ID != "" && row.SubscriptionID != "" && schedule.Subscription.ID != row.SubscriptionID {
			continue
		}
		if _, err := subscriptionschedule.Release(schedule.ID, nil); err != nil {
			return err
		}
	}
	return iter.Err()
}

// scheduleStorageQuantityChange applies a storage quantity reduction at the
// end of the current period via a Stripe subscription schedule.
func (s *Service) scheduleStorageQuantityChange(ctx context.Context, row *entity.BillingSubscription, targetQuantity int64) error {
	storagePlan, ok := s.cfg.Plan(config.StorageProductName)
	if !ok || len(storagePlan.PriceIDs) == 0 {
		return fmt.Errorf("storage price is not configured")
	}
	sub, err := subscription.Get(row.SubscriptionID, nil)
	if err != nil {
		return fmt.Errorf("retrieve subscription: %w", err)
	}
	schedule, created, err := s.ensureSchedule(row)
	if err != nil {
		return err
	}
	release := func() {
		if created {
			_, _ = subscriptionschedule.Release(schedule.ID, nil)
		}
	}
	if schedule.CurrentPhase == nil {
		release()
		return fmt.Errorf("subscription schedule has no current phase")
	}
	currentItems := make([]*stripe.SubscriptionSchedulePhaseItemParams, 0, len(sub.Items.Data))
	targetItems := make([]*stripe.SubscriptionSchedulePhaseItemParams, 0, len(sub.Items.Data))
	for _, item := range sub.Items.Data {
		if item == nil || item.Price == nil {
			continue
		}
		currentItems = append(currentItems, &stripe.SubscriptionSchedulePhaseItemParams{
			Price:    stripe.String(item.Price.ID),
			Quantity: stripe.Int64(item.Quantity),
		})
		if item.Price.ID == storagePlan.PriceIDs[0] {
			if targetQuantity <= 0 {
				continue
			}
			targetItems = append(targetItems, &stripe.SubscriptionSchedulePhaseItemParams{
				Price:    stripe.String(item.Price.ID),
				Quantity: stripe.Int64(targetQuantity),
			})
			continue
		}
		targetItems = append(targetItems, &stripe.SubscriptionSchedulePhaseItemParams{
			Price:    stripe.String(item.Price.ID),
			Quantity: stripe.Int64(item.Quantity),
		})
	}
	start := schedule.CurrentPhase.StartDate
	end := schedule.CurrentPhase.EndDate
	_, err = subscriptionschedule.Update(schedule.ID, &stripe.SubscriptionScheduleParams{
		EndBehavior: stripe.String("release"),
		Phases: []*stripe.SubscriptionSchedulePhaseParams{
			{StartDate: stripe.Int64(start), EndDate: stripe.Int64(end), Items: currentItems},
			{StartDate: stripe.Int64(end), Items: targetItems},
		},
	})
	if err != nil {
		release()
		return fmt.Errorf("update subscription schedule: %w", err)
	}
	row.ScheduleID = stringPtr(schedule.ID)
	return nil
}

// SessionStatus returns the state of a Checkout session after verifying that
// the session belongs to the caller's tenant.
func (s *Service) SessionStatus(ctx context.Context, tenantID, sessionID string) (map[string]interface{}, error) {
	if err := s.EnsureStripe(); err != nil {
		return nil, err
	}
	session, err := checkoutsession.Get(sessionID, &stripe.CheckoutSessionParams{Expand: []*string{stripe.String("invoice")}})
	if err != nil {
		return nil, fmt.Errorf("retrieve checkout session: %w", err)
	}
	metadataTenant := strings.TrimSpace(session.Metadata["tenant_id"])
	if metadataTenant != "" && metadataTenant != tenantID {
		return nil, fmt.Errorf("checkout session does not belong to tenant")
	}
	if metadataTenant == "" {
		if session.Customer == nil {
			return nil, fmt.Errorf("checkout session has no ownership evidence")
		}
		row, lookupErr := s.subscriptionDAO.GetByCustomerID(ctx, s.db, session.Customer.ID)
		if lookupErr != nil || row.TenantID != tenantID {
			return nil, fmt.Errorf("checkout session does not belong to tenant")
		}
	}
	paymentIntentID := ""
	if session.PaymentIntent != nil {
		paymentIntentID = session.PaymentIntent.ID
	}
	invoiceID := ""
	invoiceURL := ""
	invoicePDF := ""
	if session.Invoice != nil {
		invoiceID = session.Invoice.ID
		invoiceURL = session.Invoice.HostedInvoiceURL
		invoicePDF = session.Invoice.InvoicePDF
	}
	customerID := ""
	if session.Customer != nil {
		customerID = session.Customer.ID
	}
	subscriptionID := ""
	if session.Subscription != nil {
		subscriptionID = session.Subscription.ID
	}
	planName := strings.TrimSpace(session.Metadata["product_name"])
	priceID := strings.TrimSpace(session.Metadata["price_id"])
	if planName == "" && priceID != "" {
		if name, found := s.ResolvePlanName(priceID); found {
			planName = name
		}
	}
	if planName == "" && customerID != "" {
		if row, lookupErr := s.subscriptionDAO.GetByCustomerID(ctx, s.db, customerID); lookupErr == nil {
			planName = row.PlanName
			priceID = row.PriceID
			if subscriptionID == "" {
				subscriptionID = row.SubscriptionID
			}
		}
	}
	productType := strings.TrimSpace(session.Metadata["product_type"])
	if productType == "" {
		productType = "subscription"
	}
	return map[string]interface{}{
		"payment_status":    session.PaymentStatus,
		"mode":              session.Mode,
		"amount_cents":      session.AmountTotal,
		"currency":          session.Currency,
		"created":           session.Created,
		"metadata":          session.Metadata,
		"customer_id":       customerID,
		"subscription_id":   subscriptionID,
		"plan_name":         planName,
		"price_id":          priceID,
		"product_type":      productType,
		"payment_intent_id": paymentIntentID,
		"invoice_id":        invoiceID,
		"invoice_url":       invoiceURL,
		"invoice_pdf_url":   invoicePDF,
		"receipt_url":       invoiceURL,
	}, nil
}

// SubscriptionPaymentStatus derives the canonical payment state of a subscription.
func (s *Service) SubscriptionPaymentStatus(ctx context.Context, tenantID, subscriptionID string) (map[string]interface{}, error) {
	row, err := s.Subscription(ctx, tenantID)
	if err != nil || row == nil {
		return nil, fmt.Errorf("no subscription for tenant")
	}
	if row.SubscriptionID != subscriptionID {
		return nil, fmt.Errorf("subscription does not belong to tenant")
	}
	if err := s.EnsureStripe(); err != nil {
		return nil, err
	}
	sub, err := subscription.Get(subscriptionID, &stripe.SubscriptionParams{Expand: []*string{stripe.String("latest_invoice")}})
	if err != nil {
		return nil, fmt.Errorf("retrieve subscription: %w", err)
	}
	invoiceStatus := ""
	var amountDue *int64
	invoiceURL := ""
	invoiceID := ""
	if sub.LatestInvoice != nil {
		invoiceStatus = string(sub.LatestInvoice.Status)
		amountDue = stripe.Int64(sub.LatestInvoice.AmountDue)
		invoiceURL = sub.LatestInvoice.HostedInvoiceURL
		invoiceID = sub.LatestInvoice.ID
	}
	status := string(sub.Status)
	state := NormalizePaymentState(invoiceStatus, "", status, amountDue, row.TargetPlanName != nil)
	remotePlan, remotePrice := "", ""
	if sub.Items != nil && len(sub.Items.Data) > 0 && sub.Items.Data[0].Price != nil {
		remotePrice = sub.Items.Data[0].Price.ID
		if resolved, found := s.ResolvePlanName(remotePrice); found {
			remotePlan = resolved
		}
	}
	converged := remotePlan == "" || strings.EqualFold(row.PlanName, remotePlan)
	attention := SubscriptionPaymentAttention(status, invoiceURL)
	periodStart, periodEnd := int64(0), int64(0)
	if sub.Items != nil && len(sub.Items.Data) > 0 {
		periodStart = sub.Items.Data[0].CurrentPeriodStart
		periodEnd = sub.Items.Data[0].CurrentPeriodEnd
	}
	return map[string]interface{}{
		"subscription_id":      sub.ID,
		"status":               sub.Status,
		"payment_state":        string(state),
		"payment_required":     attention.PaymentRequired,
		"payment_recoverable":  attention.PaymentRecoverable,
		"payment_recovery_url": attention.PaymentRecoveryURL,
		"current_period_start": periodStart,
		"current_period_end":   periodEnd,
		"entitlement_ready":    state == PaymentStatePaid && converged,
		"plan_name":            firstNonEmpty(remotePlan, row.PlanName),
		"price_id":             firstNonEmpty(remotePrice, row.PriceID),
		"remote_plan_name":     remotePlan,
		"remote_price_id":      remotePrice,
		"invoice_id":           invoiceID,
		"invoice_url":          invoiceURL,
		"receipt_url":          invoiceURL,
		"amount_cents":         amountDue,
		"checked_at":           time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// firstNonEmpty returns the first non-empty value, preferring authoritative
// remote state over a possibly stale local row.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
