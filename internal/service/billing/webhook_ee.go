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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"

	"github.com/stripe/stripe-go/v87/invoice"
	"github.com/stripe/stripe-go/v87/subscription"
	"gorm.io/gorm"
)

// ErrEventInFlight reports that another delivery currently owns the event.
var ErrEventInFlight = errors.New("billing event is already being processed")

// StripeEvent is the subset of a Stripe event the Go handler needs.
type StripeEvent struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Created json.Number     `json:"created"`
	Data    StripeEventData `json:"data"`
}

// StripeEventData wraps the event object payload.
type StripeEventData struct {
	Object json.RawMessage `json:"object"`
}

// EventType identifies the handled Stripe event classes.
const (
	EventCheckoutSessionCompleted = "checkout.session.completed"
	EventInvoicePaid              = "invoice.paid"
	EventInvoicePaymentFailed     = "invoice.payment_failed"
	EventInvoiceActionRequired    = "invoice.payment_action_required"
	EventSubscriptionCreated      = "customer.subscription.created"
	EventSubscriptionUpdated      = "customer.subscription.updated"
	EventSubscriptionDeleted      = "customer.subscription.deleted"
	EventPaymentIntentSucceeded   = "payment_intent.succeeded"
)

// focusedEvents is the subscription set registered with Stripe.
var focusedEvents = []string{
	EventInvoicePaid,
	EventInvoicePaymentFailed,
	EventInvoiceActionRequired,
	EventSubscriptionUpdated,
	EventSubscriptionDeleted,
	EventSubscriptionCreated,
	EventCheckoutSessionCompleted,
	EventPaymentIntentSucceeded,
}

// FocusedEvents returns the Stripe event types this server registers for.
func FocusedEvents() []string {
	return append([]string(nil), focusedEvents...)
}

// WebhookSecret returns the persisted Stripe webhook signing secret.
func (s *Service) WebhookSecret(ctx context.Context) (string, error) {
	settingsDAO := dao.NewSystemSettingsDAO()
	rows, err := settingsDAO.GetByName(ctx, s.db, "billing_webhook_secret")
	if err != nil {
		return "", err
	}
	if len(rows) == 0 || strings.TrimSpace(rows[0].Value) == "" {
		return "", errors.New("billing_webhook_secret is not persisted")
	}
	return strings.TrimSpace(rows[0].Value), nil
}

// CheckoutSession is the subset of a completed Checkout session we consume.
type CheckoutSession struct {
	ID            string            `json:"id"`
	Mode          string            `json:"mode"`
	CustomerID    string            `json:"customer"`
	AmountTotal   int64             `json:"amount_total"`
	Currency      string            `json:"currency"`
	PaymentIntent string            `json:"payment_intent"`
	Metadata      map[string]string `json:"metadata"`
}

// HandleWebhookEvent records and dispatches one Stripe event.
// It returns nil when the event was accepted, including duplicate delivery.
func (s *Service) HandleWebhookEvent(ctx context.Context, payload []byte) error {
	var event StripeEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("decode stripe event: %w", err)
	}
	if event.ID == "" || event.Type == "" {
		return errors.New("stripe event is missing id or type")
	}

	createdSeconds, _ := strconv.ParseInt(event.Created.String(), 10, 64)
	var created *time.Time
	if createdSeconds > 0 {
		value := time.Unix(createdSeconds, 0).UTC()
		created = &value
	}
	payloadMap := entity.JSONMap{}
	if err := json.Unmarshal(payload, &payloadMap); err != nil {
		return fmt.Errorf("decode stripe event payload: %w", err)
	}
	var objectID *string
	if object, err := extractObjectID(event.Data.Object); err == nil && object != "" {
		objectID = &object
	}

	receivedAt := time.Now().UTC().Truncate(time.Millisecond)
	row := &entity.BillingWebhookEvent{
		ID:                  common.GenerateUUID(),
		EventID:             event.ID,
		EventType:           event.Type,
		ObjectID:            objectID,
		Payload:             payloadMap,
		CreatedAt:           created,
		ReceivedAt:          receivedAt,
		ProcessingStatus:    "processing",
		ProcessingStartedAt: &receivedAt,
	}
	inserted, err := s.webhookDAO.CreateIfAbsent(ctx, s.db, row)
	if err != nil {
		return err
	}
	lease := receivedAt
	if !inserted {
		claimedLease, claimed, claimErr := s.webhookDAO.ClaimProcessing(ctx, s.db, event.ID)
		if claimErr != nil {
			return claimErr
		}
		lease = claimedLease
		if !claimed {
			existing, lookupErr := s.webhookDAO.GetByEventID(ctx, s.db, event.ID)
			if lookupErr == nil && existing != nil && existing.ProcessingStatus == "processing" {
				return ErrEventInFlight
			}
			return nil
		}
	}

	handled, handlerErr := s.dispatchEvent(ctx, event)
	if handlerErr != nil {
		if err := s.webhookDAO.MarkFailed(ctx, s.db, event.ID, handlerErr.Error(), lease); err != nil {
			return err
		}
		return handlerErr
	}
	status := "unhandled"
	if handled {
		status = "completed"
	}
	if err := s.webhookDAO.MarkCompleted(ctx, s.db, event.ID, status, lease); err != nil {
		return err
	}
	if createdSeconds > 0 {
		_ = s.saveCheckpoint(ctx, createdSeconds)
	}
	return nil
}

func (s *Service) dispatchEvent(ctx context.Context, event StripeEvent) (bool, error) {
	switch event.Type {
	case EventCheckoutSessionCompleted:
		return s.handleCheckoutSessionCompleted(ctx, event)
	case EventInvoicePaid:
		return s.handleInvoicePaid(ctx, event)
	case EventInvoicePaymentFailed, EventInvoiceActionRequired:
		return s.handleInvoiceNotPaid(ctx, event)
	case EventPaymentIntentSucceeded:
		return s.handlePaymentIntentSucceeded(ctx, event)
	case EventSubscriptionCreated, EventSubscriptionUpdated, EventSubscriptionDeleted:
		return s.handleSubscriptionEvent(ctx, event)
	default:
		return false, nil
	}
}

func (s *Service) handleCheckoutSessionCompleted(ctx context.Context, event StripeEvent) (bool, error) {
	var session CheckoutSession
	if err := json.Unmarshal(event.Data.Object, &session); err != nil {
		return false, fmt.Errorf("decode checkout session: %w", err)
	}
	if session.Mode != "payment" || session.Metadata["payment_type"] != "points_recharge" {
		return false, nil
	}
	tenantID := strings.TrimSpace(session.Metadata["tenant_id"])
	points, _ := strconv.ParseInt(session.Metadata["points_amount"], 10, 64)
	if tenantID == "" || points <= 0 {
		return false, fmt.Errorf("invalid points recharge metadata for session %s", session.ID)
	}

	existing, err := s.orderDAO.GetByOrderID(ctx, s.db, session.ID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	if existing == nil {
		order := &entity.BillingPaymentOrder{
			ID:                 common.GenerateUUID(),
			TenantID:           tenantID,
			CustomerID:         session.CustomerID,
			PaymentType:        "usage_based",
			ProductNames:       entity.JSONSlice{"points_recharge"},
			ProductQuantities:  entity.JSONSlice{points},
			ProductAmountCents: entity.JSONSlice{session.AmountTotal},
			AmountCents:        session.AmountTotal,
			Currency:           defaultCurrency(session.Currency),
			PaymentMethod:      "card",
			OrderID:            session.ID,
			PaymentChannel:     "stripe",
			PaymentStatus:      "success",
			StripeStatus:       "complete",
			Paid:               true,
			Captured:           true,
			OrderCreatedAt:     ptrTime(eventCreatedAt(event)),
		}
		if session.PaymentIntent != "" {
			order.PaymentIntentID = &session.PaymentIntent
		}
		if err := s.orderDAO.UpsertByOrderID(ctx, s.db, order); err != nil {
			return false, err
		}
	}

	metadata := map[string]interface{}{"session_id": session.ID}
	if err := s.Recharge(ctx, tenantID, points, "checkout:"+session.ID, "Stripe checkout recharge", metadata); err != nil {
		return false, err
	}
	return true, nil
}

func extractObjectID(raw json.RawMessage) (string, error) {
	var object struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &object); err != nil {
		return "", err
	}
	return object.ID, nil
}

func eventCreatedAt(event StripeEvent) time.Time {
	if at, ok := eventTimestamp(event); ok {
		return at
	}
	return time.Now().UTC()
}

// eventTimestamp returns the Stripe event creation time when it is usable for
// ordering. Events without a valid timestamp cannot be ordered.
func eventTimestamp(event StripeEvent) (time.Time, bool) {
	seconds, err := strconv.ParseInt(event.Created.String(), 10, 64)
	if err != nil || seconds <= 0 {
		return time.Time{}, false
	}
	return time.Unix(seconds, 0).UTC(), true
}

func defaultCurrency(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "usd"
	}
	return value
}

func ptrTime(value time.Time) *time.Time {
	return &value
}

// SubscriptionObject is the subset of a Stripe subscription we converge.
// InvoiceRef accepts a Stripe expandable invoice reference, which is either an
// invoice id or an inline invoice object.
type InvoiceRef struct {
	ID     string
	Object *InvoiceObject
}

// UnmarshalJSON decodes both reference forms.
func (ref *InvoiceRef) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		ref.ID, ref.Object = "", nil
		return nil
	}
	if trimmed[0] == '"' {
		var id string
		if err := json.Unmarshal(trimmed, &id); err != nil {
			return err
		}
		ref.ID, ref.Object = id, nil
		return nil
	}
	var object InvoiceObject
	if err := json.Unmarshal(trimmed, &object); err != nil {
		return err
	}
	ref.ID, ref.Object = object.ID, &object
	return nil
}

type SubscriptionObject struct {
	ID                 string            `json:"id"`
	Customer           string            `json:"customer"`
	Status             string            `json:"status"`
	CurrentPeriodStart int64             `json:"current_period_start"`
	CurrentPeriodEnd   int64             `json:"current_period_end"`
	CancelAtPeriodEnd  bool              `json:"cancel_at_period_end"`
	Metadata           map[string]string `json:"metadata"`
	LatestInvoice      *InvoiceRef       `json:"latest_invoice"`
	Items              struct {
		Data []struct {
			ID                 string `json:"id"`
			Quantity           int64  `json:"quantity"`
			CurrentPeriodStart int64  `json:"current_period_start"`
			CurrentPeriodEnd   int64  `json:"current_period_end"`
			Price              struct {
				ID string `json:"id"`
			} `json:"price"`
		} `json:"data"`
	} `json:"items"`
}

// InvoiceObject is the subset of a Stripe invoice we converge.
type InvoiceObject struct {
	ID               string `json:"id"`
	Customer         string `json:"customer"`
	Subscription     string `json:"subscription"`
	Status           string `json:"status"`
	AmountDue        int64  `json:"amount_due"`
	AmountPaid       int64  `json:"amount_paid"`
	Currency         string `json:"currency"`
	HostedInvoiceURL string `json:"hosted_invoice_url"`
	InvoicePDF       string `json:"invoice_pdf"`
	PaymentIntent    string `json:"payment_intent"`
	Paid             bool   `json:"paid"`
	Lines            struct {
		Data []InvoiceLineObject `json:"data"`
	} `json:"lines"`
	Parent struct {
		Type                string `json:"type"`
		SubscriptionDetails struct {
			Subscription string `json:"subscription"`
		} `json:"subscription_details"`
	} `json:"parent"`
}

// InvoiceLineObject is the subset of an invoice line item that the payment
// order's product columns are built from.
type InvoiceLineObject struct {
	Amount   int64 `json:"amount"`
	Quantity int64 `json:"quantity"`
	Pricing  struct {
		PriceDetails struct {
			Price string `json:"price"`
		} `json:"price_details"`
	} `json:"pricing"`
	Price struct {
		ID string `json:"id"`
	} `json:"price"`
}

// invoiceProductColumns aggregates invoice line items into the product columns
// of a payment order: one entry per line, with the price id resolved to its
// configured plan name. The web client renders these as the products,
// quantities and amounts of a billing-history row.
func (s *Service) invoiceProductColumns(invoice InvoiceObject) (names, quantities, amounts, priceIDs entity.JSONSlice) {
	names = entity.JSONSlice{}
	quantities = entity.JSONSlice{}
	amounts = entity.JSONSlice{}
	priceIDs = entity.JSONSlice{}
	for _, line := range invoice.Lines.Data {
		priceID := strings.TrimSpace(line.Pricing.PriceDetails.Price)
		if priceID == "" {
			priceID = strings.TrimSpace(line.Price.ID)
		}
		name, ok := s.ResolvePlanName(priceID)
		if !ok || name == "" {
			name = "UNKNOWN"
		}
		names = append(names, name)
		quantities = append(quantities, line.Quantity)
		amounts = append(amounts, line.Amount)
		priceIDs = append(priceIDs, priceID)
	}
	return names, quantities, amounts, priceIDs
}

// ResolvePlanName maps a Stripe price id to a configured plan.
func (s *Service) ResolvePlanName(priceID string) (string, bool) {
	if s == nil {
		return "", false
	}
	plan, ok := s.cfg.PriceIDToPlan[strings.TrimSpace(priceID)]
	return plan, ok
}

func (s *Service) handleSubscriptionEvent(ctx context.Context, event StripeEvent) (bool, error) {
	var object SubscriptionObject
	if err := json.Unmarshal(event.Data.Object, &object); err != nil {
		return false, fmt.Errorf("decode subscription: %w", err)
	}
	if event.Type == EventSubscriptionDeleted {
		return s.convergeSubscriptionToTrial(ctx, object, event)
	}
	if len(object.Items.Data) == 0 {
		return false, nil
	}
	priceID := object.Items.Data[0].Price.ID
	planName, ok := s.ResolvePlanName(priceID)
	if !ok {
		return false, fmt.Errorf("unknown stripe price %s for subscription %s", priceID, object.ID)
	}
	tenantID := strings.TrimSpace(object.Metadata["tenant_id"])
	if tenantID == "" {
		existing, err := s.subscriptionDAO.GetByCustomerID(ctx, s.db, object.Customer)
		if err != nil {
			return false, fmt.Errorf("resolve tenant for customer %s: %w", object.Customer, err)
		}
		tenantID = existing.TenantID
	}

	current, err := s.Subscription(ctx, tenantID)
	if err != nil {
		return false, err
	}
	periodStart, periodEnd := subscriptionPeriod(object)
	if periodStart <= 0 || periodEnd <= 0 || periodEnd <= periodStart {
		// Unusable cycle snapshot: applying it would reset consumption against
		// an epoch period.
		return false, nil
	}
	if current != nil && current.SubscriptionID != "" && current.SubscriptionID != object.ID {
		switch current.SubscriptionStatus {
		case "canceled", "incomplete_expired", "paused":
		default:
			return false, nil
		}
	}
	if current != nil && current.SubscriptionID == object.ID && current.StartTime != nil && periodStart < current.StartTime.Unix() {
		return false, nil
	}
	if current != nil && current.SubscriptionID == object.ID && current.LastEventAt != nil {
		if eventAt, ok := eventTimestamp(event); ok && eventAt.Before(*current.LastEventAt) {
			return false, nil
		}
	}
	periodChanged := current == nil ||
		!samePeriod(current.StartTime, periodStart) ||
		!samePeriod(current.EndTime, periodEnd)

	plan, _ := s.cfg.Plan(planName)
	productID := ""
	if latest, err := s.productDAO.GetLatestByName(ctx, s.db, planName); err == nil {
		productID = latest.ID
	}
	start := time.Unix(periodStart, 0).UTC()
	end := time.Unix(periodEnd, 0).UTC()
	row := &entity.BillingSubscription{
		TenantID:           tenantID,
		ProductID:          productID,
		PlanName:           planName,
		Status:             "active",
		CustomerID:         object.Customer,
		PriceID:            priceID,
		SubscriptionID:     object.ID,
		SubscriptionStatus: object.Status,
		StartTime:          &start,
		EndTime:            &end,
	}
	if current != nil {
		row.ID = current.ID
		row.OrderID = current.OrderID
		row.InvoiceID = current.InvoiceID
		row.InvoiceURL = current.InvoiceURL
		row.InvoicePDFURL = current.InvoicePDFURL
		row.AddonSubscriptionItemID = current.AddonSubscriptionItemID
		row.AddonStorageBytes = current.AddonStorageBytes
		row.TargetStorageBytes = current.TargetStorageBytes
		row.ScheduleID = current.ScheduleID
		if !periodChanged {
			row.TargetPlanName = current.TargetPlanName
			if current.PlanName != planName {
				row.TargetPlanName = nil
			}
		} else {
			row.TargetPlanName = nil
		}
	} else {
		row.ID = common.GenerateUUID()
	}
	if eventAt, ok := eventTimestamp(event); ok {
		row.LastEventAt = &eventAt
	}
	if row.ID == "" {
		row.ID = common.GenerateUUID()
	}
	if !SubscriptionEntitled(object.Status) && object.Status != "" && object.Status != "trialing" {
		// past_due/unpaid keep the plan but carry the payment state; only
		// canceled/incomplete_expired fall back to Trial.
		if object.Status == "canceled" || object.Status == "incomplete_expired" || object.Status == "paused" {
			return s.convergeSubscriptionToTrial(ctx, object, event)
		}
	}
	if plan != nil {
		row.PlanName = plan.Name
	}
	if SubscriptionEntitled(object.Status) && current != nil && current.PlanName != "" && current.PlanName != row.PlanName {
		latest := s.latestInvoice(ctx, object.LatestInvoice)
		if latest == nil || (latest.Status != "paid" && latest.AmountDue > 0) {
			// An upgrade that cannot be verified as paid keeps the previously
			// paid entitlement until payment converges.
			row.PlanName = current.PlanName
			row.TargetPlanName = &planName
		}
	}
	if !SubscriptionEntitled(object.Status) && object.Status != "" {
		switch object.Status {
		case "canceled", "incomplete_expired", "paused":
			// handled by convergeSubscriptionToTrial above
		default:
			// past_due/unpaid/incomplete keep the previously entitled plan
			// until payment converges; only the price/status move forward.
			if current != nil && current.PlanName != "" {
				row.PlanName = current.PlanName
			} else if trial := s.TrialPlan(); trial != nil {
				row.PlanName = trial.Name
			}
		}
	}
	if addonItemID, addonBytes := s.StorageAddonFromSubscription(&object); addonItemID != "" {
		row.AddonSubscriptionItemID = &addonItemID
		row.AddonStorageBytes = &addonBytes
		if row.TargetStorageBytes != nil && *row.TargetStorageBytes == addonBytes {
			row.TargetStorageBytes = nil
		}
	} else {
		// The authoritative subscription no longer carries a storage item, so
		// the paid add-on quota must be revoked locally.
		row.AddonSubscriptionItemID = nil
		row.AddonStorageBytes = nil
		row.TargetStorageBytes = nil
	}
	if periodChanged {
		// Reset and cycle transition commit together so a retry cannot reset
		// consumption that accumulated after the first attempt.
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if _, resetErr := s.resetPlanConsumedPoints(ctx, tx, tenantID); resetErr != nil {
				return resetErr
			}
			return s.subscriptionDAO.UpsertSubscription(ctx, tx, row)
		})
		if err != nil {
			return false, err
		}
	} else if err := s.subscriptionDAO.UpsertSubscription(ctx, s.db, row); err != nil {
		return false, err
	}
	s.syncTenantTokenLimits(ctx, tenantID)
	return true, nil
}

func (s *Service) convergeSubscriptionToTrial(ctx context.Context, object SubscriptionObject, event StripeEvent) (bool, error) {
	tenantID := strings.TrimSpace(object.Metadata["tenant_id"])
	if tenantID == "" {
		existing, err := s.subscriptionDAO.GetByCustomerID(ctx, s.db, object.Customer)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return false, nil
			}
			return false, err
		}
		tenantID = existing.TenantID
	}
	current, err := s.Subscription(ctx, tenantID)
	if err != nil {
		return false, err
	}
	if current != nil && current.SubscriptionID != "" && object.ID != "" && current.SubscriptionID != object.ID {
		return false, nil
	}
	if current != nil && current.LastEventAt != nil {
		if eventAt, ok := eventTimestamp(event); ok && eventAt.Before(*current.LastEventAt) {
			return false, nil
		}
	}
	trial := s.TrialPlan()
	if trial == nil {
		return false, fmt.Errorf("trial plan is not configured")
	}
	productID := ""
	if latest, err := s.productDAO.GetLatestByName(ctx, s.db, trial.Name); err == nil {
		productID = latest.ID
	}
	row := &entity.BillingSubscription{
		TenantID:           tenantID,
		ProductID:          productID,
		PlanName:           trial.Name,
		Status:             "active",
		CustomerID:         object.Customer,
		SubscriptionStatus: "canceled",
	}
	if current != nil {
		row.ID = current.ID
		row.OrderID = current.OrderID
		row.PriceID = current.PriceID
		row.SubscriptionID = current.SubscriptionID
		row.StartTime = current.StartTime
		row.EndTime = current.EndTime
		row.LastEventAt = current.LastEventAt
		if eventAt, ok := eventTimestamp(event); ok {
			row.LastEventAt = &eventAt
		}
	} else {
		row.ID = common.GenerateUUID()
	}
	if row.ID == "" {
		row.ID = common.GenerateUUID()
	}
	if err := s.subscriptionDAO.UpsertSubscription(ctx, s.db, row); err != nil {
		return false, err
	}
	if _, err := s.ResetPlanConsumedPoints(ctx, tenantID); err != nil {
		return false, err
	}
	s.syncTenantTokenLimits(ctx, tenantID)
	return true, nil
}

func (s *Service) handleInvoicePaid(ctx context.Context, event StripeEvent) (bool, error) {
	var invoice InvoiceObject
	if err := json.Unmarshal(event.Data.Object, &invoice); err != nil {
		return false, fmt.Errorf("decode invoice: %w", err)
	}
	subscriptionID := invoice.Subscription
	if subscriptionID == "" && invoice.Parent.SubscriptionDetails.Subscription != "" {
		subscriptionID = invoice.Parent.SubscriptionDetails.Subscription
	}
	tenantID := ""
	if subscriptionID != "" {
		existing, err := s.subscriptionDAO.GetByCustomerID(ctx, s.db, invoice.Customer)
		if err == nil {
			tenantID = existing.TenantID
			if existing.SubscriptionID == "" || existing.SubscriptionID == subscriptionID {
				urls := existing
				urls.InvoiceID = stringPtr(invoice.ID)
				urls.InvoiceURL = stringPtr(invoice.HostedInvoiceURL)
				urls.InvoicePDFURL = stringPtr(invoice.InvoicePDF)
				if err := s.subscriptionDAO.UpsertSubscription(ctx, s.db, urls); err != nil {
					return false, err
				}
			}
		}
	}
	if tenantID == "" {
		return false, nil
	}
	currentSub, _ := s.subscriptionDAO.GetByCustomerID(ctx, s.db, invoice.Customer)
	if currentSub != nil && subscriptionID != "" && currentSub.SubscriptionID != "" && currentSub.SubscriptionID != subscriptionID {
		// Historical invoice for a retired subscription: record the order but
		// never change current entitlement.
		subscriptionID = ""
	}
	if existing, err := s.orderDAO.GetByOrderID(ctx, s.db, invoice.ID); err == nil && existing != nil {
		existing.PaymentStatus = "success"
		existing.StripeStatus = invoice.Status
		existing.Paid = true
		existing.Captured = true
		if invoice.AmountPaid > 0 {
			existing.AmountCents = invoice.AmountPaid
		}
		if invoice.HostedInvoiceURL != "" {
			existing.ReceiptURL = &invoice.HostedInvoiceURL
		}
		if invoice.InvoicePDF != "" {
			existing.ReceiptPDFURL = &invoice.InvoicePDF
		}
		if names, quantities, amounts, priceIDs := s.invoiceProductColumns(invoice); len(names) > 0 {
			existing.ProductNames = names
			existing.ProductQuantities = quantities
			existing.ProductAmountCents = amounts
			existing.PriceIDs = priceIDs
		}
		if err := s.orderDAO.UpsertByOrderID(ctx, s.db, existing); err != nil {
			return false, err
		}
		if subscriptionID != "" {
			if err := s.convergePaidSubscription(ctx, invoice.Customer, subscriptionID, invoice); err != nil {
				return false, err
			}
		}
		return true, nil
	}
	order := &entity.BillingPaymentOrder{
		ID:             common.GenerateUUID(),
		TenantID:       tenantID,
		CustomerID:     invoice.Customer,
		PaymentType:    "subscription",
		AmountCents:    invoice.AmountPaid,
		Currency:       defaultCurrency(invoice.Currency),
		PaymentMethod:  "card",
		OrderID:        invoice.ID,
		PaymentChannel: "stripe",
		PaymentStatus:  "success",
		StripeStatus:   invoice.Status,
		Paid:           true,
		Captured:       true,
		OrderCreatedAt: ptrTime(eventCreatedAt(event)),
	}
	order.ProductNames, order.ProductQuantities, order.ProductAmountCents, order.PriceIDs = s.invoiceProductColumns(invoice)
	order.IsProrated = true
	if subscriptionID != "" {
		order.PaymentSubscriptionID = &subscriptionID
	}
	if invoice.PaymentIntent != "" {
		order.PaymentIntentID = &invoice.PaymentIntent
	}
	if invoice.HostedInvoiceURL != "" {
		order.ReceiptURL = &invoice.HostedInvoiceURL
	}
	if invoice.InvoicePDF != "" {
		order.ReceiptPDFURL = &invoice.InvoicePDF
	}
	if err := s.orderDAO.UpsertByOrderID(ctx, s.db, order); err != nil {
		return false, err
	}
	return true, s.convergePaidSubscription(ctx, invoice.Customer, subscriptionID, invoice)
}

// convergePaidSubscription applies a paid invoice belonging to the tenant's
// current subscription. Invoices for retired subscriptions are ignored so a
// historical payment cannot restore or alter current entitlement.
func (s *Service) convergePaidSubscription(ctx context.Context, customerID, subscriptionID string, invoice InvoiceObject) error {
	if subscriptionID == "" {
		return nil
	}
	row, err := s.subscriptionDAO.GetByCustomerID(ctx, s.db, customerID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if row.SubscriptionID != "" && row.SubscriptionID != subscriptionID {
		return nil
	}
	// Only the subscription's current invoice may converge entitlement; a paid
	// historical invoice records its order but must not re-enable features.
	if err := s.EnsureStripe(); err == nil {
		stripeSub, subErr := subscription.Get(subscriptionID, nil)
		if subErr != nil {
			// The authoritative invoice identity is unknown: record the order
			// without granting entitlement.
			return nil
		}
		if stripeSub.LatestInvoice != nil && stripeSub.LatestInvoice.ID != "" && stripeSub.LatestInvoice.ID != invoice.ID {
			return nil
		}
	}
	row.SubscriptionStatus = "active"
	trialFallback := false
	if trial := s.TrialPlan(); trial != nil && row.TargetPlanName != nil {
		trialFallback = strings.EqualFold(*row.TargetPlanName, trial.Name)
	}
	if row.TargetPlanName != nil && row.ScheduleID == nil && !trialFallback {
		if target, ok := s.cfg.Plan(*row.TargetPlanName); ok && len(target.PriceIDs) > 0 {
			row.PlanName = target.Name
			row.PriceID = target.PriceIDs[0]
			if product, productErr := s.productDAO.GetLatestByName(ctx, s.db, target.Name); productErr == nil {
				row.ProductID = product.ID
			}
		}
		row.TargetPlanName = nil
	}
	row.InvoiceID = stringPtr(invoice.ID)
	row.InvoiceURL = stringPtr(invoice.HostedInvoiceURL)
	row.InvoicePDFURL = stringPtr(invoice.InvoicePDF)
	return s.subscriptionDAO.UpsertSubscription(ctx, s.db, row)
}

// latestInvoice resolves an expandable latest_invoice reference. Stripe sends
// an id unless the request expanded the field, so the invoice is fetched when
// only an id is present.
func (s *Service) latestInvoice(ctx context.Context, ref *InvoiceRef) *InvoiceObject {
	if ref == nil {
		return nil
	}
	if ref.Object != nil {
		return ref.Object
	}
	if ref.ID == "" {
		return nil
	}
	if err := s.EnsureStripe(); err != nil {
		return nil
	}
	row, err := invoice.Get(ref.ID, nil)
	if err != nil {
		return nil
	}
	resolved := &InvoiceObject{
		ID:               row.ID,
		Status:           string(row.Status),
		AmountDue:        row.AmountDue,
		AmountPaid:       row.AmountPaid,
		Currency:         string(row.Currency),
		HostedInvoiceURL: row.HostedInvoiceURL,
		InvoicePDF:       row.InvoicePDF,
		Paid:             string(row.Status) == "paid",
	}
	if row.Customer != nil {
		resolved.Customer = row.Customer.ID
	}
	if row.Parent != nil {
		resolved.Parent.Type = string(row.Parent.Type)
		if row.Parent.SubscriptionDetails != nil && row.Parent.SubscriptionDetails.Subscription != nil {
			resolved.Parent.SubscriptionDetails.Subscription = row.Parent.SubscriptionDetails.Subscription.ID
		}
	}
	return resolved
}

func subscriptionPeriod(object SubscriptionObject) (int64, int64) {
	if object.CurrentPeriodStart != 0 || object.CurrentPeriodEnd != 0 {
		return object.CurrentPeriodStart, object.CurrentPeriodEnd
	}
	if len(object.Items.Data) > 0 {
		return object.Items.Data[0].CurrentPeriodStart, object.Items.Data[0].CurrentPeriodEnd
	}
	return 0, 0
}

func samePeriod(existing *time.Time, epoch int64) bool {
	if existing == nil {
		return epoch == 0
	}
	return existing.Unix() == epoch
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// handleInvoiceNotPaid converges a failed or action-required invoice onto the
// tenant's subscription and records a failed order.
func (s *Service) handleInvoiceNotPaid(ctx context.Context, event StripeEvent) (bool, error) {
	var invoice InvoiceObject
	if err := json.Unmarshal(event.Data.Object, &invoice); err != nil {
		return false, fmt.Errorf("decode invoice: %w", err)
	}
	subscriptionID := invoice.Subscription
	if subscriptionID == "" {
		subscriptionID = invoice.Parent.SubscriptionDetails.Subscription
	}
	if invoice.Customer == "" {
		return false, nil
	}
	row, err := s.subscriptionDAO.GetByCustomerID(ctx, s.db, invoice.Customer)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if subscriptionID != "" && row.SubscriptionID != "" && row.SubscriptionID != subscriptionID {
		// Failure for a retired subscription must not affect current state.
		return true, nil
	}
	if existing, orderErr := s.orderDAO.GetByOrderID(ctx, s.db, invoice.ID); orderErr == nil && existing != nil && existing.Paid {
		return true, nil
	}
	status := "past_due"
	if event.Type == EventInvoiceActionRequired {
		status = "incomplete"
	}
	row.SubscriptionStatus = status
	row.InvoiceID = stringPtr(invoice.ID)
	row.InvoiceURL = stringPtr(invoice.HostedInvoiceURL)
	row.InvoicePDFURL = stringPtr(invoice.InvoicePDF)
	if err := s.subscriptionDAO.UpsertSubscription(ctx, s.db, row); err != nil {
		return false, err
	}
	s.syncTenantTokenLimits(ctx, row.TenantID)
	if existing, err := s.orderDAO.GetByOrderID(ctx, s.db, invoice.ID); err == nil && existing != nil {
		existing.PaymentStatus = "failed"
		existing.StripeStatus = invoice.Status
		if err := s.orderDAO.UpsertByOrderID(ctx, s.db, existing); err != nil {
			return false, err
		}
		return true, nil
	}
	order := &entity.BillingPaymentOrder{
		ID:             common.GenerateUUID(),
		TenantID:       row.TenantID,
		CustomerID:     invoice.Customer,
		PaymentType:    "subscription",
		AmountCents:    invoice.AmountDue,
		Currency:       defaultCurrency(invoice.Currency),
		PaymentMethod:  "card",
		OrderID:        invoice.ID,
		PaymentChannel: "stripe",
		PaymentStatus:  "failed",
		StripeStatus:   invoice.Status,
		OrderCreatedAt: ptrTime(eventCreatedAt(event)),
	}
	if subscriptionID != "" {
		order.PaymentSubscriptionID = &subscriptionID
	}
	return true, s.orderDAO.UpsertByOrderID(ctx, s.db, order)
}

// handlePaymentIntentSucceeded covers one-off points recharges.
func (s *Service) handlePaymentIntentSucceeded(ctx context.Context, event StripeEvent) (bool, error) {
	var intent struct {
		ID       string            `json:"id"`
		Metadata map[string]string `json:"metadata"`
	}
	if err := json.Unmarshal(event.Data.Object, &intent); err != nil {
		return false, fmt.Errorf("decode payment intent: %w", err)
	}
	if intent.Metadata["payment_type"] != "points_recharge" {
		return false, nil
	}
	// Points purchases are credited once by checkout.session.completed, which
	// owns the session-level idempotency key. The PaymentIntent is audit-only.
	return true, nil
}
