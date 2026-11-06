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

// BillingSubscription is the single main subscription of a tenant.
type BillingSubscription struct {
	ID        string `gorm:"column:id;primaryKey;size:32" json:"id"`
	TenantID  string `gorm:"column:tenant_id;size:32;not null;uniqueIndex:uni_billing_subscription_tenant" json:"tenant_id"`
	ProductID string `gorm:"column:product_id;size:32;not null" json:"product_id"`
	PlanName  string `gorm:"column:plan_name;size:255;not null;default:trial;index" json:"plan_name"`
	OrderID   string `gorm:"column:order_id;size:128;not null" json:"order_id"`
	Status    string `gorm:"column:status;size:16" json:"status"`

	CustomerID         string  `gorm:"column:customer_id;size:255;not null;default:'';index" json:"customer_id"`
	PriceID            string  `gorm:"column:price_id;size:128;not null" json:"price_id"`
	SubscriptionID     string  `gorm:"column:subscription_id;size:255;not null;default:'';index" json:"subscription_id"`
	SubscriptionStatus string  `gorm:"column:subscription_status;size:255;not null;default:'';index" json:"subscription_status"`
	InvoiceID          *string `gorm:"column:invoice_id;size:255;index" json:"invoice_id,omitempty"`
	InvoiceURL         *string `gorm:"column:invoice_url;size:512" json:"invoice_url,omitempty"`
	InvoicePDFURL      *string `gorm:"column:invoice_pdf_url;size:512" json:"invoice_pdf_url,omitempty"`

	StartTime            *time.Time `gorm:"column:start_time;index" json:"start_time,omitempty"`
	EndTime              *time.Time `gorm:"column:end_time;index" json:"end_time,omitempty"`
	RenewTime            *time.Time `gorm:"column:renew_time" json:"renew_time,omitempty"`
	LastEventAt          *time.Time `gorm:"column:last_event_at" json:"last_event_at,omitempty"`
	OriginalSubscription *string    `gorm:"column:original_subscription_id;size:32" json:"original_subscription_id,omitempty"`

	AddonSubscriptionItemID *string `gorm:"column:addon_subscription_item_id;size:255" json:"addon_subscription_item_id,omitempty"`
	AddonStorageBytes       *int64  `gorm:"column:addon_storage_bytes" json:"addon_storage_bytes,omitempty"`
	TargetStorageBytes      *int64  `gorm:"column:target_storage_bytes" json:"target_storage_bytes,omitempty"`
	TargetPlanName          *string `gorm:"column:target_plan_name;size:255;index" json:"target_plan_name,omitempty"`
	ScheduleID              *string `gorm:"column:schedule_id;size:255" json:"schedule_id,omitempty"`
	BaseModel
}

// TableName specifies the billing subscription table.
func (BillingSubscription) TableName() string {
	return "billing_subscription"
}
