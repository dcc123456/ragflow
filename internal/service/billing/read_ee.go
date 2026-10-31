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
	"sort"
	"strings"
	"time"

	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/server/config"

	"gorm.io/gorm"
)

// subscriptionState is the resolved plan view of a tenant.
type subscriptionState struct {
	Subscription *entity.BillingSubscription
	PlanName     string
	Plan         *config.BillingPlanInfo
}

func (s *Service) resolveSubscription(ctx context.Context, tenantID string) (*subscriptionState, error) {
	subscription, err := s.Subscription(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	planName := config.BillingPlanTrialName
	if subscription != nil && subscription.PlanName != "" {
		planName = subscription.PlanName
	}
	info, ok := s.cfg.Plan(planName)
	if !ok {
		info = s.TrialPlan()
	}
	return &subscriptionState{Subscription: subscription, PlanName: planName, Plan: info}, nil
}

// CurrentPlanPayload returns the GET /billing/subscription payload.
func (s *Service) CurrentPlanPayload(ctx context.Context, tenantID string) (map[string]interface{}, error) {
	state, err := s.resolveSubscription(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	payload := map[string]interface{}{
		"tenant_id":           tenantID,
		"plan_name":           state.PlanName,
		"subscription_status": "",
		"customer_id":         "",
		"subscription_id":     "",
		"price_id":            "",
		"start_time":          nil,
		"end_time":            nil,
	}
	attention := SubscriptionPaymentAttention("", "")
	if state.Subscription != nil {
		sub := state.Subscription
		payload["subscription_status"] = sub.SubscriptionStatus
		payload["customer_id"] = sub.CustomerID
		payload["subscription_id"] = sub.SubscriptionID
		payload["price_id"] = sub.PriceID
		payload["start_time"] = utcISO(sub.StartTime)
		payload["end_time"] = utcISO(sub.EndTime)
		if sub.InvoiceURL != nil {
			payload["invoice_url"] = *sub.InvoiceURL
		}
		attention = SubscriptionPaymentAttention(sub.SubscriptionStatus, deref(sub.InvoiceURL))
		if sub.TargetPlanName != nil || sub.TargetStorageBytes != nil {
			pending := map[string]interface{}{}
			if sub.TargetPlanName != nil {
				pending["target_plan_name"] = *sub.TargetPlanName
				pending["pending_plan_name"] = *sub.TargetPlanName
			}
			if sub.TargetStorageBytes != nil {
				pending["target_storage_bytes"] = *sub.TargetStorageBytes
			}
			if sub.TargetPlanName != nil {
				if targetPlan, ok := s.cfg.Plan(*sub.TargetPlanName); ok && len(targetPlan.PriceIDs) > 0 {
					pending["pending_price_id"] = targetPlan.PriceIDs[0]
				}
			}
			pending["schedule_id"] = deref(sub.ScheduleID)
			if sub.EndTime != nil {
				pending["effective_at"] = sub.EndTime.UTC().Format(time.RFC3339)
			}
			payload["pending_subscription_change"] = pending
		}
	}
	payload["payment_state"] = string(attention.PaymentState)
	payload["payment_required"] = attention.PaymentRequired
	payload["payment_recoverable"] = attention.PaymentRecoverable
	payload["payment_recovery_url"] = attention.PaymentRecoveryURL
	return payload, nil
}

// OverviewPayload returns the GET /billing/subscription/overview payload.
func (s *Service) OverviewPayload(ctx context.Context, tenantID string) (map[string]interface{}, error) {
	state, err := s.resolveSubscription(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	usage := dao.NewBillingUsageDAO()
	apps, err := usage.TenantApps(ctx, s.db, tenantID)
	if err != nil {
		return nil, err
	}
	members, err := usage.TenantMembers(ctx, s.db, tenantID)
	if err != nil {
		return nil, err
	}
	storageUsed, err := usage.TenantStorageBytes(ctx, s.db, tenantID)
	if err != nil {
		return nil, err
	}
	balance, err := s.GetBalance(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	addonStorage, _, err := s.subscriptionDAO.GetStorageBytesForTenant(ctx, s.db, tenantID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	var plan *config.BillingPlanInfo
	if state.Plan != nil {
		plan = state.Plan
	} else {
		plan = &config.BillingPlanInfo{}
	}

	attention := SubscriptionPaymentAttention("", "")
	status := ""
	invoiceURL := ""
	var subscriptionID, priceID, customerID string
	var start, end *time.Time
	if state.Subscription != nil {
		status = state.Subscription.SubscriptionStatus
		invoiceURL = deref(state.Subscription.InvoiceURL)
		subscriptionID = state.Subscription.SubscriptionID
		priceID = state.Subscription.PriceID
		customerID = state.Subscription.CustomerID
		start = state.Subscription.StartTime
		end = state.Subscription.EndTime
		attention = SubscriptionPaymentAttention(status, invoiceURL)
	}

	payload := map[string]interface{}{
		"customer_id":         customerID,
		"subscription_id":     subscriptionID,
		"price_id":            priceID,
		"plan_name":           state.PlanName,
		"subscription_status": status,
		"billing_cycle": map[string]interface{}{
			"start": utcISO(start),
			"end":   utcISO(end),
		},
		"payment_state":        string(attention.PaymentState),
		"payment_required":     attention.PaymentRequired,
		"payment_recoverable":  attention.PaymentRecoverable,
		"payment_recovery_url": attention.PaymentRecoveryURL,
		"resources": map[string]interface{}{
			"plan_storage":  map[string]interface{}{"used": storageUsed, "limit": plan.QuotaStorage, "unit": "bytes"},
			"addon_storage": map[string]interface{}{"used": int64(0), "limit": addonStorage, "unit": "bytes"},
			"plan_points":   map[string]interface{}{"used": balance.ConsumedPlanPoints, "limit": plan.QuotaPoints, "unit": "points"},
			"addon_points":  map[string]interface{}{"used": balance.ConsumedAddonPoints, "limit": balance.AddonPurchasedPoints, "unit": "points"},
			"members":       map[string]interface{}{"used": members, "limit": plan.QuotaMembers, "unit": "users"},
			"apps":          map[string]interface{}{"used": apps, "limit": plan.QuotaApps, "unit": "applications"},
		},
		"api_request_limits": map[string]interface{}{
			"requests_per_minute": s.PlanRateLimit(state.PlanName),
		},
	}
	if addonStorage > 0 && storageUsed > plan.QuotaStorage {
		resources := payload["resources"].(map[string]interface{})
		resources["plan_storage"] = map[string]interface{}{"used": plan.QuotaStorage, "limit": plan.QuotaStorage, "unit": "bytes"}
		overflow := storageUsed - plan.QuotaStorage
		if overflow > addonStorage {
			overflow = addonStorage
		}
		resources["addon_storage"] = map[string]interface{}{"used": overflow, "limit": addonStorage, "unit": "bytes"}
	}
	return payload, nil
}

// PlansPayload returns the GET /billing/plans payload.
func (s *Service) PlansPayload(ctx context.Context) ([]map[string]interface{}, error) {
	products, err := s.productDAO.ListLatestByType(ctx, s.db, "subscription")
	if err != nil {
		return nil, err
	}
	sort.SliceStable(products, func(i, j int) bool {
		left, right := 9999, 9999
		if plan, ok := s.cfg.Plan(products[i].Name); ok {
			left = plan.Priority
		}
		if plan, ok := s.cfg.Plan(products[j].Name); ok {
			right = plan.Priority
		}
		if left != right {
			return left < right
		}
		return products[i].Name < products[j].Name
	})

	payload := make([]map[string]interface{}, 0, len(products))
	for _, product := range products {
		payload = append(payload, s.productPayload(ctx, product))
	}
	return payload, nil
}

// AddonsPayload returns the GET /billing/addons payload.
func (s *Service) AddonsPayload(ctx context.Context) ([]map[string]interface{}, error) {
	products, err := s.productDAO.ListLatestByType(ctx, s.db, "addon")
	if err != nil {
		return nil, err
	}
	sort.SliceStable(products, func(i, j int) bool { return products[i].Name < products[j].Name })
	payload := make([]map[string]interface{}, 0, len(products))
	for _, product := range products {
		payload = append(payload, s.productPayload(ctx, product))
	}
	return payload, nil
}

func (s *Service) productPayload(ctx context.Context, product entity.BillingProduct) map[string]interface{} {
	usd := float64(-1)
	if priceIDs := splitPriceIDs(product.PriceIDs); len(priceIDs) > 0 {
		if value, ok := s.PriceUSD(priceIDs[0]); ok {
			usd = value
		}
	}
	plan, _ := s.cfg.Plan(product.Name)
	quotaPoints := int64(0)
	apiLimit := product.APIRequestLimitPerMinute
	if plan != nil {
		quotaPoints = plan.QuotaPoints
	}
	return map[string]interface{}{
		"id":          product.ID,
		"name":        product.Name,
		"price":       usd,
		"description": product.Description,
		"price_ids":   product.PriceIDs,
		"feature": map[string]interface{}{
			"quota_apps":       product.QuotaApps,
			"quota_members":    product.QuotaMembers,
			"quota_storage":    product.QuotaStorage,
			"quota_points":     quotaPoints,
			"quota_api_limits": derefInt64(apiLimit),
			"price_per_gb":     pricePerGB(usd, product.QuotaStorage),
		},
	}
}

// StoragePayload returns the GET /billing/storage payload.
func (s *Service) StoragePayload(ctx context.Context, tenantID string) (map[string]interface{}, error) {
	state, err := s.resolveSubscription(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	addonPtr, targetPtr, err := s.subscriptionDAO.GetStorageStateForTenant(ctx, s.db, tenantID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	subscriptionID, priceID, status := "", "", ""
	var start, end *time.Time
	attention := SubscriptionPaymentAttention("", "")
	if state.Subscription != nil {
		subscriptionID = state.Subscription.SubscriptionID
		priceID = state.Subscription.PriceID
		status = state.Subscription.SubscriptionStatus
		start = state.Subscription.StartTime
		end = state.Subscription.EndTime
		attention = SubscriptionPaymentAttention(status, deref(state.Subscription.InvoiceURL))
	}
	unitPrice := float64(0)
	if storagePlan, ok := s.cfg.Plan(config.StorageProductName); ok && storagePlan.QuotaStorage > 0 {
		if usd, found := s.PriceUSD(firstPriceIDFromPlan(storagePlan)); found {
			unitPrice = usd
		}
	}
	return map[string]interface{}{
		"tenant_id":            tenantID,
		"plan_name":            state.PlanName,
		"trial_forbidden":      state.PlanName == config.BillingPlanTrialName,
		"unit_price":           unitPrice,
		"addon_storage_bytes":  derefInt64(addonPtr),
		"target_storage_bytes": targetPtr,
		"subscription_id":      subscriptionID,
		"price_id":             priceID,
		"status":               status,
		"cancel_at_period_end": false,
		"current_period_start": utcISO(start),
		"current_period_end":   utcISO(end),
		"payment_required":     attention.PaymentRequired,
		"payment_recovery_url": attention.PaymentRecoveryURL,
	}, nil
}

// PointsBalancePayload returns the GET /billing/points/balance payload.
func (s *Service) PointsBalancePayload(ctx context.Context, tenantID string) (map[string]interface{}, error) {
	balance, err := s.GetBalance(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"plan_points": map[string]interface{}{
			"used":  maxInt64(0, balance.ConsumedPlanPoints),
			"limit": maxInt64(0, balance.AvailablePlanPoints+balance.ConsumedPlanPoints),
			"unit":  "points",
		},
		"addon_points": map[string]interface{}{
			"used":  maxInt64(0, balance.ConsumedAddonPoints),
			"limit": maxInt64(0, balance.AddonPurchasedPoints),
			"unit":  "points",
		},
	}, nil
}

// PointsPricePayload returns the GET /billing/points/price payload.
func (s *Service) PointsPricePayload() map[string]interface{} {
	priceID := s.cfg.PointsRecharge.PriceID
	pointsPerUnit := s.cfg.PointsRecharge.PointsPerUnit
	if pointsPerUnit == 0 {
		pointsPerUnit = 100
	}
	payload := map[string]interface{}{
		"price_id":        priceID,
		"price_usd":       nil,
		"points_per_unit": pointsPerUnit,
	}
	if usd, ok := s.PriceUSD(priceID); ok {
		payload["price_usd"] = usd
	}
	return payload
}

// LedgerPage returns one page of the points ledger.
func (s *Service) LedgerPage(ctx context.Context, tenantID string, offset, limit int) (map[string]interface{}, error) {
	rows, total, err := s.pointDAO.ListLedger(ctx, s.db, tenantID, offset, limit)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]interface{}{
			"id":              row.ID,
			"tenant_id":       row.TenantID,
			"event_type":      row.EventType,
			"points":          row.Points,
			"source":          row.Source,
			"idempotency_key": row.IdempotencyKey,
			"related_hold_id": row.RelatedHoldID,
			"description":     row.Description,
			"metadata":        row.Metadata,
			"create_time":     row.CreateTime,
			"update_time":     row.UpdateTime,
		})
	}
	return map[string]interface{}{"total": total, "items": items}, nil
}

// HoldPage returns one page of point holds.
func (s *Service) HoldPage(ctx context.Context, tenantID string, offset, limit int) (map[string]interface{}, error) {
	rows, total, err := s.pointDAO.ListHolds(ctx, s.db, tenantID, offset, limit)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]interface{}{
			"id":           row.ID,
			"tenant_id":    row.TenantID,
			"doc_id":       row.DocID,
			"points":       row.Points,
			"plan_points":  row.PlanPoints,
			"addon_points": row.AddonPoints,
			"status":       row.Status,
			"expired_at":   utcISO(row.ExpiredAt),
			"create_time":  row.CreateTime,
		})
	}
	return map[string]interface{}{"total": total, "items": items}, nil
}

// SpendHistory returns one page of payment orders in the shape the web client
// renders as billing history. The envelope and field names mirror the Python
// spend_overview contract: the client reads data.items and expects
// invoice_id/product/product_quantities/product_amount_cents plus an epoch
// millisecond created_at.
func (s *Service) SpendHistory(ctx context.Context, tenantID string, page, pageSize int, start, end *time.Time) (map[string]interface{}, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	orders, total, err := s.orderDAO.ListByTenantPage(ctx, s.db, tenantID, start, end, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]interface{}, 0, len(orders))
	for _, order := range orders {
		status := "pending"
		switch {
		case order.Paid || strings.EqualFold(order.PaymentStatus, "success"):
			status = "paid"
		case strings.EqualFold(order.PaymentStatus, "failed"):
			status = "unpaid"
		}
		productNames := jsonSliceStrings(order.ProductNames)
		product := "UNKNOWN"
		if len(productNames) > 0 {
			product = strings.Join(productNames, ", ")
		}
		invoicePDF := order.ReceiptURL
		if order.ReceiptPDFURL != nil && *order.ReceiptPDFURL != "" {
			invoicePDF = order.ReceiptPDFURL
		}
		createdAt := interface{}(nil)
		if order.OrderCreatedAt != nil {
			createdAt = order.OrderCreatedAt.UnixMilli()
		}
		items = append(items, map[string]interface{}{
			"invoice_id":           order.OrderID,
			"amount":               float64(order.AmountCents) / 100,
			"currency":             strings.ToUpper(order.Currency),
			"status":               status,
			"created_at":           createdAt,
			"hosted_invoice_url":   order.ReceiptURL,
			"invoice_pdf_url":      invoicePDF,
			"product_ids":          order.ProductIDs,
			"product":              product,
			"product_quantities":   order.ProductQuantities,
			"product_amount_cents": order.ProductAmountCents,
		})
	}
	return map[string]interface{}{"total": total, "items": items}, nil
}

// jsonSliceStrings extracts the string elements of a JSON array column.
func jsonSliceStrings(values entity.JSONSlice) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok && text != "" {
			out = append(out, text)
		}
	}
	return out
}

func splitPriceIDs(value string) []string {
	if value == "" {
		return nil
	}
	var out []string
	current := ""
	for _, r := range value {
		if r == ' ' || r == '\t' || r == '\n' {
			if current != "" {
				out = append(out, current)
				current = ""
			}
			continue
		}
		current += string(r)
	}
	if current != "" {
		out = append(out, current)
	}
	return out
}

func firstPriceIDFromPlan(plan *config.BillingPlanInfo) string {
	if plan == nil || len(plan.PriceIDs) == 0 {
		return ""
	}
	return plan.PriceIDs[0]
}

func pricePerGB(priceUSD float64, quotaStorage int64) float64 {
	if priceUSD <= 0 || quotaStorage <= 0 {
		return 0
	}
	quotaGB := float64(quotaStorage) / float64(config.BytesPerGB)
	if quotaGB <= 0 {
		return 0
	}
	return priceUSD / quotaGB
}

func utcISO(value *time.Time) interface{} {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339)
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func derefInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

// SpendMetricsPayload aggregates paid orders by month for the usage charts.
func (s *Service) SpendMetricsPayload(ctx context.Context, tenantID string) (map[string]interface{}, error) {
	orders, err := s.orderDAO.ListByTenant(ctx, s.db, tenantID, 500)
	if err != nil {
		return nil, err
	}
	type bucket struct {
		total float64
	}
	buckets := map[string]*bucket{}
	total := 0.0
	for _, order := range orders {
		if !order.Paid || order.OrderCreatedAt == nil {
			continue
		}
		amount := float64(order.AmountCents) / 100
		total += amount
		month := order.OrderCreatedAt.UTC().Format("2006-01")
		if buckets[month] == nil {
			buckets[month] = &bucket{}
		}
		buckets[month].total += amount
	}
	months := make([]string, 0, len(buckets))
	for month := range buckets {
		months = append(months, month)
	}
	sort.Strings(months)
	categories := make([]string, 0, len(months))
	series := make([]float64, 0, len(months))
	for _, month := range months {
		categories = append(categories, month)
		series = append(series, buckets[month].total)
	}
	return map[string]interface{}{
		"total_spend": total,
		"categories":  categories,
		"series":      series,
	}, nil
}

// DeepdocUsagePayload reports the parse points consumed in the current cycle.
func (s *Service) DeepdocUsagePayload(ctx context.Context, tenantID string) (map[string]interface{}, error) {
	state, err := s.resolveSubscription(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	balance, err := s.GetBalance(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	limit := int64(0)
	if state.Plan != nil {
		limit = state.Plan.QuotaPoints
	}
	payload := map[string]interface{}{
		"plan_name":   state.PlanName,
		"used":        balance.ConsumedPlanPoints,
		"limit":       limit,
		"addon_used":  balance.ConsumedAddonPoints,
		"addon_limit": balance.AddonPurchasedPoints,
		"unit":        "points",
		"cycle_start": nil,
		"cycle_end":   nil,
	}
	if state.Subscription != nil {
		payload["cycle_start"] = utcISO(state.Subscription.StartTime)
		payload["cycle_end"] = utcISO(state.Subscription.EndTime)
	}
	return payload, nil
}

// AddonOverviewPayload lists purchased add-ons and their remaining quantity.
func (s *Service) AddonOverviewPayload(ctx context.Context, tenantID string) ([]map[string]interface{}, error) {
	var rows []entity.BillingPurchasedProductOverview
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Order("product_name ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	payload := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		payload = append(payload, map[string]interface{}{
			"product_name":   row.ProductName,
			"quantity":       row.Quantity,
			"effective_time": utcISO(&row.EffectiveTime),
			"expiry_time":    utcISO(row.ExpiryTime),
		})
	}
	if addonPtr, targetPtr, err := s.subscriptionDAO.GetStorageStateForTenant(ctx, s.db, tenantID); err == nil {
		payload = append(payload, map[string]interface{}{
			"product_name":   "storage",
			"quantity":       derefInt64(addonPtr),
			"target":         targetPtr,
			"effective_time": nil,
			"expiry_time":    nil,
		})
	}
	return payload, nil
}
