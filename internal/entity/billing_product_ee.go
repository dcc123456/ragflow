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

package entity

import "time"

// BillingProduct is a purchasable plan or add-on.
type BillingProduct struct {
	ID                       string  `gorm:"column:id;primaryKey;size:32" json:"id"`
	Name                     string  `gorm:"column:name;size:255;not null;index" json:"name"`
	Priority                 int     `gorm:"column:priority;not null" json:"priority"`
	QuotaApps                int64   `gorm:"column:quota_apps;not null" json:"quota_apps"`
	QuotaMembers             int64   `gorm:"column:quota_members;not null" json:"quota_members"`
	QuotaStorage             int64   `gorm:"column:quota_storage;not null" json:"quota_storage"`
	TaskPriority             string  `gorm:"column:task_priority;not null" json:"task_priority"`
	PriceIDs                 string  `gorm:"column:price_ids;type:text;not null" json:"price_ids"`
	Description              *string `gorm:"column:description;type:text" json:"description,omitempty"`
	ProductType              string  `gorm:"column:product_type;not null" json:"product_type"`
	Version                  int     `gorm:"column:version;not null" json:"version"`
	QuotaPoints              *int64  `gorm:"column:quota_points" json:"quota_points,omitempty"`
	APIRequestLimitPerMinute *int64  `gorm:"column:api_request_limit_per_minute" json:"api_request_limit_per_minute,omitempty"`
	BaseModel
}

// TableName specifies the billing product table.
func (BillingProduct) TableName() string {
	return "billing_product"
}

// BillingPricePoint is the metered unit price for a product.
type BillingPricePoint struct {
	ID                   string     `gorm:"column:id;primaryKey;size:32" json:"id"`
	ProductID            string     `gorm:"column:product_id;size:32;index" json:"product_id"`
	ProductName          string     `gorm:"column:product_name;size:255;not null" json:"product_name"`
	PriceType            string     `gorm:"column:price_type;size:32;not null;index" json:"price_type"`
	BillingFrequency     string     `gorm:"column:billing_frequency;size:32;not null" json:"billing_frequency"`
	IncludedFreeAmount   *int64     `gorm:"column:included_free_amount" json:"included_free_amount,omitempty"`
	Unit                 *string    `gorm:"column:unit;size:32" json:"unit,omitempty"`
	UnitQuantity         *int64     `gorm:"column:unit_quantity" json:"unit_quantity,omitempty"`
	PriceAmount          *int64     `gorm:"column:price_amount" json:"price_amount,omitempty"`
	PriceCurrency        *string    `gorm:"column:price_currency;size:3" json:"price_currency,omitempty"`
	ConsumingPointAmount int64      `gorm:"column:consuming_point_amount;not null;default:0" json:"consuming_point_amount"`
	EffectiveTime        time.Time  `gorm:"column:effective_time;not null" json:"effective_time"`
	ExpiryTime           *time.Time `gorm:"column:expiry_time" json:"expiry_time,omitempty"`
	BaseModel
}

// TableName specifies the billing price point table.
func (BillingPricePoint) TableName() string {
	return "billing_pricepoint"
}

// BillingPurchasedProductOverview is the remaining amount of a purchased add-on.
type BillingPurchasedProductOverview struct {
	ID            string     `gorm:"column:id;primaryKey;size:32" json:"id"`
	TenantID      string     `gorm:"column:tenant_id;size:32;not null;index" json:"tenant_id"`
	ProductID     string     `gorm:"column:product_id;size:32;not null;index" json:"product_id"`
	ProductName   string     `gorm:"column:product_name;size:255;not null" json:"product_name"`
	Quantity      int64      `gorm:"column:quantity;not null" json:"quantity"`
	EffectiveTime time.Time  `gorm:"column:effective_time;not null" json:"effective_time"`
	ExpiryTime    *time.Time `gorm:"column:expiry_time" json:"expiry_time,omitempty"`
	BaseModel
}

// TableName specifies the purchased product overview table.
func (BillingPurchasedProductOverview) TableName() string {
	return "billing_purchased_product_overview"
}
