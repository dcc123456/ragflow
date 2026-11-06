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

package entity

import "time"

// BillingPaymentOrder records one Stripe payment or invoice.
type BillingPaymentOrder struct {
	ID          string `gorm:"column:id;primaryKey;size:32" json:"id"`
	TenantID    string `gorm:"column:tenant_id;size:32;not null;index" json:"tenant_id"`
	CustomerID  string `gorm:"column:customer_id;size:255;not null;default:'';index" json:"customer_id"`
	PaymentType string `gorm:"column:payment_type;size:32" json:"payment_type"`

	ProductIDs         JSONSlice `gorm:"column:product_ids" json:"product_ids,omitempty"`
	ProductNames       JSONSlice `gorm:"column:product_names" json:"product_names,omitempty"`
	ProductQuantities  JSONSlice `gorm:"column:product_quantities" json:"product_quantities,omitempty"`
	ProductAmountCents JSONSlice `gorm:"column:product_amount_cents" json:"product_amount_cents,omitempty"`
	PriceIDs           JSONSlice `gorm:"column:price_ids" json:"price_ids,omitempty"`
	IsProrated         bool      `gorm:"column:is_prorated;default:false" json:"is_prorated"`

	AmountCents   int64  `gorm:"column:amount_cents;not null;default:0" json:"amount_cents"`
	Currency      string `gorm:"column:currency;size:3;not null" json:"currency"`
	PaymentMethod string `gorm:"column:payment_method;size:32;not null" json:"payment_method"`

	OrderID               string  `gorm:"column:order_id;size:128;not null;index" json:"order_id"`
	PaymentIntentID       *string `gorm:"column:payment_intent_id;size:128" json:"payment_intent_id,omitempty"`
	PaymentSubscriptionID *string `gorm:"column:payment_subscription_id;size:128" json:"payment_subscription_id,omitempty"`
	ReceiptURL            *string `gorm:"column:receipt_url;size:512" json:"receipt_url,omitempty"`
	ReceiptPDFURL         *string `gorm:"column:receipt_pdf_url;size:512" json:"receipt_pdf_url,omitempty"`

	PaymentChannel string `gorm:"column:payment_channel;size:32;not null" json:"payment_channel"`
	PaymentStatus  string `gorm:"column:payment_status;size:32;not null" json:"payment_status"`
	StripeStatus   string `gorm:"column:stripe_status;size:255;not null;default:''" json:"stripe_status"`
	Paid           bool   `gorm:"column:paid;default:false" json:"paid"`
	Captured       bool   `gorm:"column:captured;default:false" json:"captured"`

	Description    *string    `gorm:"column:description;type:text" json:"description,omitempty"`
	OrderCreatedAt *time.Time `gorm:"column:order_created_at" json:"order_created_at,omitempty"`
	PaymentDetail  JSONMap    `gorm:"column:payment_detail" json:"payment_detail,omitempty"`
	BaseModel
}

// TableName specifies the billing payment order table.
func (BillingPaymentOrder) TableName() string {
	return "billing_payment_order"
}

// BillingWebhookEvent is the audit log and state machine row of a Stripe event.
type BillingWebhookEvent struct {
	ID                  string     `gorm:"column:id;primaryKey;size:32" json:"id"`
	EventID             string     `gorm:"column:event_id;size:255;not null;uniqueIndex:uni_billing_webhook_event_event" json:"event_id"`
	EventType           string     `gorm:"column:event_type;size:255;not null;index" json:"event_type"`
	ObjectID            *string    `gorm:"column:object_id;size:255;index" json:"object_id,omitempty"`
	Payload             JSONMap    `gorm:"column:payload;not null" json:"payload"`
	CreatedAt           *time.Time `gorm:"column:created_at" json:"created_at,omitempty"`
	ReceivedAt          time.Time  `gorm:"column:received_at;not null" json:"received_at"`
	ProcessingStatus    string     `gorm:"column:processing_status;size:16;not null;default:completed;index" json:"processing_status"`
	ProcessingStartedAt *time.Time `gorm:"column:processing_started_at" json:"processing_started_at,omitempty"`
	ProcessedAt         *time.Time `gorm:"column:processed_at" json:"processed_at,omitempty"`
	FailedAt            *time.Time `gorm:"column:failed_at" json:"failed_at,omitempty"`
	LastError           *string    `gorm:"column:last_error;type:text" json:"last_error,omitempty"`
	BaseModel
}

// TableName specifies the billing webhook event table.
func (BillingWebhookEvent) TableName() string {
	return "billing_webhook_event"
}
