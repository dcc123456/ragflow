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

import "testing"

func TestNormalizePaymentState(t *testing.T) {
	zero := int64(0)
	hundred := int64(100)
	cases := []struct {
		name      string
		invoice   string
		intent    string
		sub       string
		amountDue *int64
		scheduled bool
		want      PaymentState
	}{
		{name: "scheduled wins", invoice: "paid", scheduled: true, want: PaymentStateScheduled},
		{name: "void invoice fails", invoice: "void", intent: "succeeded", want: PaymentStateFailed},
		{name: "canceled intent fails", intent: "canceled", want: PaymentStateFailed},
		{name: "incomplete_expired fails", sub: "incomplete_expired", want: PaymentStateFailed},
		{name: "paid invoice", invoice: "paid", want: PaymentStatePaid},
		{name: "succeeded intent", intent: "succeeded", want: PaymentStatePaid},
		{name: "zero amount due is paid", invoice: "open", amountDue: &zero, want: PaymentStatePaid},
		{name: "requires action intent", intent: "requires_action", want: PaymentStateRequiresAction},
		{name: "requires payment method", intent: "requires_payment_method", want: PaymentStateRequiresAction},
		{name: "open invoice with amount", invoice: "open", amountDue: &hundred, want: PaymentStateRequiresAction},
		{name: "past due", sub: "past_due", want: PaymentStateRequiresAction},
		{name: "processing intent", intent: "processing", want: PaymentStatePending},
		{name: "open invoice processing", invoice: "open", intent: "processing", amountDue: &hundred, want: PaymentStatePending},
		{name: "draft invoice", invoice: "draft", amountDue: &hundred, want: PaymentStatePending},
		{name: "active subscription", sub: "active", want: PaymentStatePaid},
		{name: "empty is pending", want: PaymentStatePending},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizePaymentState(tc.invoice, tc.intent, tc.sub, tc.amountDue, tc.scheduled)
			if got != tc.want {
				t.Fatalf("NormalizePaymentState(%q,%q,%q)=%q, want %q", tc.invoice, tc.intent, tc.sub, got, tc.want)
			}
		})
	}
}

func TestSubscriptionPaymentAttention(t *testing.T) {
	paid := SubscriptionPaymentAttention("active", "https://invoice.example/ignored")
	if paid.PaymentState != PaymentStatePaid || paid.PaymentRequired || paid.PaymentRecoverable || paid.PaymentRecoveryURL != "" {
		t.Fatalf("active subscription attention = %+v", paid)
	}

	recoverable := SubscriptionPaymentAttention("past_due", "https://invoice.example/recover")
	if recoverable.PaymentState != PaymentStateRequiresAction || !recoverable.PaymentRequired || !recoverable.PaymentRecoverable {
		t.Fatalf("past_due attention = %+v", recoverable)
	}
	if recoverable.PaymentRecoveryURL != "https://invoice.example/recover" {
		t.Fatalf("recovery url = %q", recoverable.PaymentRecoveryURL)
	}

	failed := SubscriptionPaymentAttention("incomplete_expired", "https://invoice.example/stale")
	if failed.PaymentState != PaymentStateFailed || !failed.PaymentRequired || failed.PaymentRecoverable || failed.PaymentRecoveryURL != "" {
		t.Fatalf("incomplete_expired attention = %+v", failed)
	}
}

func TestSubscriptionEntitled(t *testing.T) {
	for _, status := range []string{"active", "trialing", "ACTIVE", " trialing "} {
		if !SubscriptionEntitled(status) {
			t.Fatalf("%q should be entitled", status)
		}
	}
	for _, status := range []string{"", "past_due", "canceled", "incomplete"} {
		if SubscriptionEntitled(status) {
			t.Fatalf("%q should not be entitled", status)
		}
	}
}
