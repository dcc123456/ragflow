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

// BillingPointAccount is the per-tenant points balance.
type BillingPointAccount struct {
	ID                   string `gorm:"column:id;primaryKey;size:32" json:"id"`
	TenantID             string `gorm:"column:tenant_id;size:32;not null;uniqueIndex:uni_billing_point_account_tenant" json:"tenant_id"`
	ConsumedPlanPoints   int64  `gorm:"column:consumed_plan_points;not null;default:0" json:"consumed_plan_points"`
	AddonPurchasedPoints int64  `gorm:"column:addon_purchased_points;not null;default:0" json:"addon_purchased_points"`
	ConsumedAddonPoints  int64  `gorm:"column:consumed_addon_points;not null;default:0" json:"consumed_addon_points"`
	HeldPoints           int64  `gorm:"column:held_points;not null;default:0" json:"held_points"`
	BaseModel
}

// TableName specifies the billing point account table.
func (BillingPointAccount) TableName() string {
	return "billing_point_account"
}

// BillingPointLedger is the immutable points movement log.
type BillingPointLedger struct {
	ID             string  `gorm:"column:id;primaryKey;size:32" json:"id"`
	TenantID       string  `gorm:"column:tenant_id;size:32;not null;index" json:"tenant_id"`
	EventType      string  `gorm:"column:event_type;size:32;not null" json:"event_type"`
	Points         int64   `gorm:"column:points;not null" json:"points"`
	Source         string  `gorm:"column:source;size:16;not null;default:plan" json:"source"`
	IdempotencyKey string  `gorm:"column:idempotency_key;size:128;not null;uniqueIndex:uni_billing_point_ledger_idem" json:"idempotency_key"`
	RelatedHoldID  *string `gorm:"column:related_hold_id;size:32;index" json:"related_hold_id,omitempty"`
	Description    *string `gorm:"column:description;type:text" json:"description,omitempty"`
	Metadata       JSONMap `gorm:"column:metadata" json:"metadata,omitempty"`
	BaseModel
}

// TableName specifies the billing point ledger table.
func (BillingPointLedger) TableName() string {
	return "billing_point_ledger"
}

// BillingPointHold is a points reservation for an in-progress parse.
type BillingPointHold struct {
	ID             string     `gorm:"column:id;primaryKey;size:32" json:"id"`
	TenantID       string     `gorm:"column:tenant_id;size:32;not null;index" json:"tenant_id"`
	DocID          string     `gorm:"column:doc_id;size:32;not null;index" json:"doc_id"`
	Points         int64      `gorm:"column:points;not null" json:"points"`
	PlanPoints     int64      `gorm:"column:plan_points;not null;default:0" json:"plan_points"`
	AddonPoints    int64      `gorm:"column:addon_points;not null;default:0" json:"addon_points"`
	Status         string     `gorm:"column:status;size:32;not null;default:held;index" json:"status"`
	IdempotencyKey string     `gorm:"column:idempotency_key;size:128;not null;uniqueIndex:uni_billing_point_hold_idem" json:"idempotency_key"`
	ExpiredAt      *time.Time `gorm:"column:expired_at" json:"expired_at,omitempty"`
	BaseModel
}

// TableName specifies the billing point hold table.
func (BillingPointHold) TableName() string {
	return "billing_point_hold"
}
