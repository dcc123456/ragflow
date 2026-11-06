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

import "strings"

// PaymentState is the canonical payment state shown to the client.
type PaymentState string

// Canonical payment states.
const (
	PaymentStatePaid           PaymentState = "paid"
	PaymentStatePending        PaymentState = "pending"
	PaymentStateRequiresAction PaymentState = "requires_action"
	PaymentStateFailed         PaymentState = "failed"
	PaymentStateScheduled      PaymentState = "scheduled"
)

var (
	paymentActionRequiredIntentStatuses = map[string]bool{
		"requires_action":         true,
		"requires_confirmation":   true,
		"requires_payment_method": true,
		"requires_source":         true,
		"requires_source_action":  true,
	}
	paymentFailedInvoiceStatuses = map[string]bool{"uncollectible": true, "void": true}
	paymentFailedIntentStatuses  = map[string]bool{"canceled": true}
	paymentPendingIntentStatuses = map[string]bool{"processing": true}
	paymentFailedSubscription    = map[string]bool{"canceled": true, "incomplete_expired": true, "paused": true}
	paymentRecoverableSub        = map[string]bool{"incomplete": true, "past_due": true, "unpaid": true}
	entitledSubscriptionStatus   = map[string]bool{"active": true, "trialing": true}
)

// NormalizePaymentState ports normalize_billing_payment_state.
func NormalizePaymentState(invoiceStatus, intentStatus, subscriptionStatus string, amountDue *int64, scheduled bool) PaymentState {
	invoice := strings.ToLower(strings.TrimSpace(invoiceStatus))
	intent := strings.ToLower(strings.TrimSpace(intentStatus))
	subscription := strings.ToLower(strings.TrimSpace(subscriptionStatus))

	if scheduled {
		return PaymentStateScheduled
	}
	if paymentFailedInvoiceStatuses[invoice] || paymentFailedIntentStatuses[intent] || paymentFailedSubscription[subscription] {
		return PaymentStateFailed
	}
	if invoice == "paid" || intent == "succeeded" || (amountDue != nil && *amountDue <= 0) {
		return PaymentStatePaid
	}
	if paymentActionRequiredIntentStatuses[intent] {
		return PaymentStateRequiresAction
	}
	if paymentPendingIntentStatuses[intent] {
		return PaymentStatePending
	}
	if invoice == "open" && amountDue != nil && *amountDue > 0 {
		return PaymentStateRequiresAction
	}
	if paymentRecoverableSub[subscription] {
		return PaymentStateRequiresAction
	}
	if entitledSubscriptionStatus[subscription] {
		return PaymentStatePaid
	}
	return PaymentStatePending
}

// PaymentAttention is the recovery contract attached to subscription payloads.
type PaymentAttention struct {
	PaymentState       PaymentState `json:"payment_state"`
	PaymentRequired    bool         `json:"payment_required"`
	PaymentRecoverable bool         `json:"payment_recoverable"`
	PaymentRecoveryURL string       `json:"payment_recovery_url"`
}

// SubscriptionPaymentAttention ports subscription_payment_attention.
func SubscriptionPaymentAttention(subscriptionStatus, invoiceURL string) PaymentAttention {
	status := strings.ToLower(strings.TrimSpace(subscriptionStatus))
	recoverable := paymentRecoverableSub[status]
	recoveryURL := ""
	if recoverable {
		recoveryURL = strings.TrimSpace(invoiceURL)
	}
	return PaymentAttention{
		PaymentState:       NormalizePaymentState("", "", status, nil, false),
		PaymentRequired:    recoverable || paymentFailedSubscription[status],
		PaymentRecoverable: recoverable,
		PaymentRecoveryURL: recoveryURL,
	}
}

// SubscriptionEntitled reports whether a Stripe status grants paid entitlements.
func SubscriptionEntitled(status string) bool {
	return entitledSubscriptionStatus[strings.ToLower(strings.TrimSpace(status))]
}
