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

package handler

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/service/billing"

	"github.com/gin-gonic/gin"
	"github.com/stripe/stripe-go/v87/webhook"
)

// BillingHandler serves the billing API.
type BillingHandler struct{}

// NewBillingHandler creates a billing handler.
func NewBillingHandler() *BillingHandler {
	return &BillingHandler{}
}

func billingServiceOrDisabled(c *gin.Context) (*billing.Service, bool) {
	service := billing.Get()
	if service == nil || !service.Enabled() {
		common.RespondError(c, common.CodeDataError, "Billing is disabled.")
		return nil, false
	}
	return service, true
}

func billingTenant(c *gin.Context) (string, bool) {
	user, code, message := GetUser(c)
	if code != common.CodeSuccess {
		common.RespondError(c, code, message)
		return "", false
	}
	tenantID := c.Query("tenant_id")
	if tenantID == "" {
		tenantID = user.ID
	}
	if tenantID != user.ID {
		common.RespondError(c, common.CodeAuthenticationError, "no authorization")
		return "", false
	}
	return tenantID, true
}

func billingFailure(c *gin.Context, err error) {
	if errors.Is(err, billing.ErrInvalidArgument) {
		common.RespondError(c, common.CodeArgumentError, err.Error())
		return
	}
	common.RespondError(c, common.CodeServerError, err.Error())
}

// Status handles GET /billing/status. It returns a bare JSON object:
// the web client reads billing_enabled at the top level.
func (h *BillingHandler) Status(c *gin.Context) {
	service := billing.Get()
	enabled := service != nil && service.Enabled()
	c.JSON(http.StatusOK, gin.H{"billing_enabled": enabled})
}

// Health handles GET /billing/downgrade-guard/health.
func (h *BillingHandler) Health(c *gin.Context) {
	common.RespondSuccess(c, gin.H{
		"enabled":        billingGuardEnabled(),
		"pool_size":      0,
		"daily_scan_ok":  false,
		"last_scan_date": "",
	}, "")
}

// Plans handles GET /billing/plans. The route is guard-only: no login.
func (h *BillingHandler) Plans(c *gin.Context) {
	service, ok := billingServiceOrDisabled(c)
	if !ok {
		return
	}
	payload, err := service.PlansPayload(c.Request.Context())
	if err != nil {
		billingFailure(c, err)
		return
	}
	common.RespondSuccess(c, payload, "")
}

// CurrentSubscription handles GET /billing/subscription.
func (h *BillingHandler) CurrentSubscription(c *gin.Context) {
	service, ok := billingServiceOrDisabled(c)
	if !ok {
		return
	}
	tenantID, ok := billingTenant(c)
	if !ok {
		return
	}
	payload, err := service.CurrentPlanPayload(c.Request.Context(), tenantID)
	if err != nil {
		billingFailure(c, err)
		return
	}
	common.RespondSuccess(c, payload, "")
}

// SubscriptionOverview handles GET /billing/subscription/overview.
func (h *BillingHandler) SubscriptionOverview(c *gin.Context) {
	service, ok := billingServiceOrDisabled(c)
	if !ok {
		return
	}
	tenantID, ok := billingTenant(c)
	if !ok {
		return
	}
	payload, err := service.OverviewPayload(c.Request.Context(), tenantID)
	if err != nil {
		billingFailure(c, err)
		return
	}
	common.RespondSuccess(c, payload, "")
}

// Storage handles GET /billing/storage.
func (h *BillingHandler) Storage(c *gin.Context) {
	service, ok := billingServiceOrDisabled(c)
	if !ok {
		return
	}
	tenantID, ok := billingTenant(c)
	if !ok {
		return
	}
	payload, err := service.StoragePayload(c.Request.Context(), tenantID)
	if err != nil {
		billingFailure(c, err)
		return
	}
	common.RespondSuccess(c, payload, "")
}

// Addons handles GET /billing/addons.
func (h *BillingHandler) Addons(c *gin.Context) {
	service, ok := billingServiceOrDisabled(c)
	if !ok {
		return
	}
	payload, err := service.AddonsPayload(c.Request.Context())
	if err != nil {
		billingFailure(c, err)
		return
	}
	common.RespondSuccess(c, payload, "")
}

// AddonOverview handles GET /billing/addons/overview.
func (h *BillingHandler) AddonOverview(c *gin.Context) {
	service, ok := billingServiceOrDisabled(c)
	if !ok {
		return
	}
	tenantID, ok := billingTenant(c)
	if !ok {
		return
	}
	payload, err := service.AddonOverviewPayload(c.Request.Context(), tenantID)
	if err != nil {
		billingFailure(c, err)
		return
	}
	common.RespondSuccess(c, payload, "")
}

// PointsBalance handles GET /billing/points/balance and /points/overview.
func (h *BillingHandler) PointsBalance(c *gin.Context) {
	service, ok := billingServiceOrDisabled(c)
	if !ok {
		return
	}
	tenantID, ok := billingTenant(c)
	if !ok {
		return
	}
	payload, err := service.PointsBalancePayload(c.Request.Context(), tenantID)
	if err != nil {
		billingFailure(c, err)
		return
	}
	common.RespondSuccess(c, payload, "")
}

// PointsPrice handles GET /billing/points/price.
func (h *BillingHandler) PointsPrice(c *gin.Context) {
	service, ok := billingServiceOrDisabled(c)
	if !ok {
		return
	}
	if _, ok := billingTenant(c); !ok {
		return
	}
	common.RespondSuccess(c, service.PointsPricePayload(), "")
}

// PointsLedger handles GET /billing/points/ledger.
func (h *BillingHandler) PointsLedger(c *gin.Context) {
	service, ok := billingServiceOrDisabled(c)
	if !ok {
		return
	}
	tenantID, ok := billingTenant(c)
	if !ok {
		return
	}
	page := billingPositiveInt(c, "page", 1, 1, 1<<30)
	pageSize := billingPositiveInt(c, "page_size", 20, 1, 100)
	payload, err := service.LedgerPage(c.Request.Context(), tenantID, (page-1)*pageSize, pageSize)
	if err != nil {
		billingFailure(c, err)
		return
	}
	common.RespondSuccess(c, payload, "")
}

// PointsHolds handles GET /billing/points/holds.
func (h *BillingHandler) PointsHolds(c *gin.Context) {
	service, ok := billingServiceOrDisabled(c)
	if !ok {
		return
	}
	tenantID, ok := billingTenant(c)
	if !ok {
		return
	}
	page := billingPositiveInt(c, "page", 1, 1, 1<<30)
	pageSize := billingPositiveInt(c, "page_size", 20, 1, 100)
	payload, err := service.HoldPage(c.Request.Context(), tenantID, (page-1)*pageSize, pageSize)
	if err != nil {
		billingFailure(c, err)
		return
	}
	common.RespondSuccess(c, payload, "")
}

// SpendOverview handles GET /billing/spend-overview.
func (h *BillingHandler) SpendOverview(c *gin.Context) {
	service, ok := billingServiceOrDisabled(c)
	if !ok {
		return
	}
	tenantID, ok := billingTenant(c)
	if !ok {
		return
	}
	start, ok := billingTimeQuery(c, "start")
	if !ok {
		billingFailure(c, fmt.Errorf("%w: start must be a unix timestamp or an ISO date", billing.ErrInvalidArgument))
		return
	}
	end, ok := billingTimeQuery(c, "end")
	if !ok {
		billingFailure(c, fmt.Errorf("%w: end must be a unix timestamp or an ISO date", billing.ErrInvalidArgument))
		return
	}
	if start != nil && end != nil && end.Before(*start) {
		billingFailure(c, fmt.Errorf("%w: end must not be before start", billing.ErrInvalidArgument))
		return
	}
	page := billingPositiveInt(c, "page", 1, 1, 1<<30)
	pageSize := billingPositiveInt(c, "page_size", 20, 1, 100)
	payload, err := service.SpendHistory(c.Request.Context(), tenantID, page, pageSize, start, end)
	if err != nil {
		billingFailure(c, err)
		return
	}
	common.RespondSuccess(c, payload, "")
}

// SpendMetrics handles GET /billing/spend-metrics.
func (h *BillingHandler) SpendMetrics(c *gin.Context) {
	service, ok := billingServiceOrDisabled(c)
	if !ok {
		return
	}
	tenantID, ok := billingTenant(c)
	if !ok {
		return
	}
	payload, err := service.SpendMetricsPayload(c.Request.Context(), tenantID)
	if err != nil {
		billingFailure(c, err)
		return
	}
	common.RespondSuccess(c, payload, "")
}

// DeepdocUsage handles GET /billing/usages/deepdoc.
func (h *BillingHandler) DeepdocUsage(c *gin.Context) {
	service, ok := billingServiceOrDisabled(c)
	if !ok {
		return
	}
	tenantID, ok := billingTenant(c)
	if !ok {
		return
	}
	payload, err := service.DeepdocUsagePayload(c.Request.Context(), tenantID)
	if err != nil {
		billingFailure(c, err)
		return
	}
	common.RespondSuccess(c, payload, "")
}

func billingGuardEnabled() bool {
	value := strings.TrimSpace(os.Getenv("DOWNGRADE_GUARD_ENABLED"))
	if value == "" {
		return true
	}
	return !strings.EqualFold(value, "false")
}

// billingTimeQuery parses a start/end query argument. Like the Python side it
// accepts a unix timestamp in seconds or an ISO date/datetime; the returned
// ok flag is false for anything unparsable so the caller can answer with an
// argument error.
func billingTimeQuery(c *gin.Context, name string) (*time.Time, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return nil, true
	}
	if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil {
		value := time.Unix(seconds, 0).UTC()
		return &value, true
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		if value, err := time.Parse(layout, raw); err == nil {
			utc := value.UTC()
			return &utc, true
		}
	}
	return nil, false
}

func billingPositiveInt(c *gin.Context, name string, fallback, min, max int) int {
	raw := c.Query(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < min {
		return fallback
	}
	if value > max {
		return max
	}
	return value
}

// Webhook handles POST /billing/webhooks/stripe.
// It verifies the Stripe signature unless the configured URL is a local
// forwarding target, then records and dispatches the event.
func (h *BillingHandler) Webhook(c *gin.Context) {
	service := billing.Get()
	if service == nil || !service.Enabled() {
		c.JSON(http.StatusOK, gin.H{"success": true})
		return
	}
	payload, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read body"})
		return
	}
	if !billingIsLocalWebhook(service.Config().WebhookURL) {
		secret, secretErr := service.WebhookSecret(c.Request.Context())
		if secretErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": secretErr.Error()})
			return
		}
		if _, verifyErr := webhook.ConstructEventWithOptions(payload, c.GetHeader("Stripe-Signature"), secret, webhook.ConstructEventOptions{
			IgnoreAPIVersionMismatch: true,
		}); verifyErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": verifyErr.Error()})
			return
		}
	}
	if err := service.HandleWebhookEvent(c.Request.Context(), payload); err != nil {
		// Stripe only retries non-2xx responses.
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func billingIsLocalWebhook(webhookURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(webhookURL))
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

type billingCheckoutRequest struct {
	TenantID            string           `json:"tenant_id"`
	PaymentType         string           `json:"payment_type"`
	SubscriptionPriceID string           `json:"subscription_price_id"`
	AddonPriceID        string           `json:"addon_price_id"`
	SetupIntentID       string           `json:"setup_intent_id"`
	Quantity            billingFlexInt64 `json:"quantity"`
	SessionSuccessURL   string           `json:"session_success_url"`
	SessionCancelURL    string           `json:"session_cancel_url"`
}

// CheckoutSubscription handles POST/PATCH /billing/subscription.
func (h *BillingHandler) CheckoutSubscription(c *gin.Context) {
	service, ok := billingServiceOrDisabled(c)
	if !ok {
		return
	}
	if _, ok := billingTenant(c); !ok {
		return
	}
	var req billingCheckoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, common.CodeArgumentError, err.Error())
		return
	}
	user, _, _ := GetUser(c)
	tenantID, ok := billingRequestTenant(c, req.TenantID)
	if !ok {
		return
	}
	ctx := c.Request.Context()

	if req.PaymentType == "addon" {
		result, err := service.CreateAddonCheckout(ctx, tenantID, req.AddonPriceID, int64(req.Quantity), req.SessionSuccessURL, req.SessionCancelURL)
		if err != nil {
			billingFailure(c, err)
			return
		}
		common.RespondSuccess(c, result, "")
		return
	}
	if req.SubscriptionPriceID == "" {
		common.RespondError(c, common.CodeArgumentError, "subscription_price_id is required")
		return
	}
	subscription, _ := service.Subscription(ctx, tenantID)
	reusable := subscription != nil && subscription.SubscriptionID != ""
	if reusable {
		switch subscription.SubscriptionStatus {
		case "canceled", "incomplete_expired", "paused":
			reusable = false
		}
	}
	if reusable {
		result, err := service.ChangePlan(ctx, tenantID, req.SubscriptionPriceID, req.SetupIntentID)
		if err != nil {
			billingFailure(c, err)
			return
		}
		common.RespondSuccess(c, result, "")
		return
	}
	_ = user
	result, err := service.CreateSubscriptionCheckout(ctx, tenantID, req.SubscriptionPriceID, req.SetupIntentID, req.SessionSuccessURL, req.SessionCancelURL)
	if err != nil {
		billingFailure(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

// AddonCheckout handles POST /billing/addon-purchases.
func (h *BillingHandler) AddonCheckout(c *gin.Context) {
	service, ok := billingServiceOrDisabled(c)
	if !ok {
		return
	}
	var req billingCheckoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, common.CodeArgumentError, err.Error())
		return
	}
	tenantID, ok := billingRequestTenant(c, req.TenantID)
	if !ok {
		return
	}
	result, err := service.CreateAddonCheckout(c.Request.Context(), tenantID, req.AddonPriceID, int64(req.Quantity), req.SessionSuccessURL, req.SessionCancelURL)
	if err != nil {
		billingFailure(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

// PreviewSubscription handles POST /billing/subscription/preview.
func (h *BillingHandler) PreviewSubscription(c *gin.Context) {
	service, ok := billingServiceOrDisabled(c)
	if !ok {
		return
	}
	var req struct {
		TenantID   string `json:"tenant_id"`
		NewPriceID string `json:"new_price_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, common.CodeArgumentError, err.Error())
		return
	}
	tenantID, ok := billingRequestTenant(c, req.TenantID)
	if !ok {
		return
	}
	if req.NewPriceID == "" {
		common.RespondError(c, common.CodeArgumentError, "new_price_id is required")
		return
	}
	result, err := service.PreviewPlanChange(c.Request.Context(), tenantID, req.NewPriceID)
	if err != nil {
		billingFailure(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

// SetStorageTarget handles PATCH /billing/storage.
func (h *BillingHandler) SetStorageTarget(c *gin.Context) {
	service, ok := billingServiceOrDisabled(c)
	if !ok {
		return
	}
	var req struct {
		TenantID           string `json:"tenant_id"`
		TargetStorageBytes int64  `json:"target_storage_bytes"`
		SetupIntentID      string `json:"setup_intent_id"`
		SessionSuccessURL  string `json:"session_success_url"`
		SessionCancelURL   string `json:"session_cancel_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, common.CodeArgumentError, err.Error())
		return
	}
	tenantID, ok := billingRequestTenant(c, req.TenantID)
	if !ok {
		return
	}
	result, err := service.SetStorageTarget(c.Request.Context(), tenantID, req.TargetStorageBytes, req.SetupIntentID, req.SessionSuccessURL, req.SessionCancelURL)
	if err != nil {
		billingFailure(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

// SetupIntent handles POST /billing/setup-intents.
func (h *BillingHandler) SetupIntent(c *gin.Context) {
	service, ok := billingServiceOrDisabled(c)
	if !ok {
		return
	}
	var req struct {
		TenantID  string `json:"tenant_id"`
		ReturnURL string `json:"return_url"`
	}
	_ = c.ShouldBindJSON(&req)
	tenantID, ok := billingRequestTenant(c, req.TenantID)
	if !ok {
		return
	}
	result, err := service.CreateSetupIntent(c.Request.Context(), tenantID, req.ReturnURL)
	if err != nil {
		billingFailure(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

// PortalSession handles POST /billing/portal-sessions.
func (h *BillingHandler) PortalSession(c *gin.Context) {
	service, ok := billingServiceOrDisabled(c)
	if !ok {
		return
	}
	var req struct {
		TenantID  string `json:"tenant_id"`
		ReturnURL string `json:"return_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, common.CodeArgumentError, err.Error())
		return
	}
	tenantID, ok := billingRequestTenant(c, req.TenantID)
	if !ok {
		return
	}
	result, err := service.CreatePortalSession(c.Request.Context(), tenantID, req.ReturnURL)
	if err != nil {
		billingFailure(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

// PointsCheckout handles POST /billing/points/checkout.
func (h *BillingHandler) PointsCheckout(c *gin.Context) {
	service, ok := billingServiceOrDisabled(c)
	if !ok {
		return
	}
	var req struct {
		TenantID          string           `json:"tenant_id"`
		Quantity          billingFlexInt64 `json:"quantity"`
		SessionSuccessURL string           `json:"session_success_url"`
		SessionCancelURL  string           `json:"session_cancel_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, common.CodeArgumentError, err.Error())
		return
	}
	tenantID, ok := billingRequestTenant(c, req.TenantID)
	if !ok {
		return
	}
	result, err := service.CreatePointsCheckout(c.Request.Context(), tenantID, int64(req.Quantity), req.SessionSuccessURL, req.SessionCancelURL)
	if err != nil {
		billingFailure(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

// CheckoutSession handles GET /billing/checkouts/:session-id.
func (h *BillingHandler) CheckoutSession(c *gin.Context) {
	service, ok := billingServiceOrDisabled(c)
	if !ok {
		return
	}
	tenantID, ok := billingTenant(c)
	if !ok {
		return
	}
	result, err := service.SessionStatus(c.Request.Context(), tenantID, c.Param("session-id"))
	if err != nil {
		billingFailure(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

// SubscriptionPaymentStatus handles GET /billing/subscriptions/:subscription-id/payment-status.
func (h *BillingHandler) SubscriptionPaymentStatus(c *gin.Context) {
	service, ok := billingServiceOrDisabled(c)
	if !ok {
		return
	}
	tenantID, ok := billingTenant(c)
	if !ok {
		return
	}
	result, err := service.SubscriptionPaymentStatus(c.Request.Context(), tenantID, c.Param("subscription-id"))
	if err != nil {
		billingFailure(c, err)
		return
	}
	common.RespondSuccess(c, result, "")
}

func billingRequestTenant(c *gin.Context, requested string) (string, bool) {
	user, code, message := GetUser(c)
	if code != common.CodeSuccess {
		common.RespondError(c, code, message)
		return "", false
	}
	tenantID := requested
	if tenantID == "" {
		tenantID = c.Query("tenant_id")
	}
	if tenantID == "" {
		tenantID = user.ID
	}
	if tenantID != user.ID {
		common.RespondError(c, common.CodeAuthenticationError, "no authorization")
		return "", false
	}
	return tenantID, true
}

// billingFlexInt64 accepts both JSON numbers and numeric strings, matching the
// pricing UI which sends quantity as "1" in some flows.
type billingFlexInt64 int64

func (v *billingFlexInt64) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "" || text == "null" {
		*v = 0
		return nil
	}
	if strings.HasPrefix(text, "\"") && strings.HasSuffix(text, "\"") {
		text = strings.Trim(text, "\"")
	}
	parsed, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		value, floatErr := strconv.ParseFloat(text, 64)
		if floatErr != nil || math.IsNaN(value) || math.IsInf(value, 0) || value != math.Trunc(value) {
			return fmt.Errorf("quantity must be an integer: %w", err)
		}
		if value >= 9223372036854775808.0 || value < -9223372036854775808.0 {
			return fmt.Errorf("quantity is out of range")
		}
		parsed = int64(value)
	}
	*v = billingFlexInt64(parsed)
	return nil
}

// billingQuotaUser resolves the caller for a quota check.
func billingQuotaUser(c *gin.Context) (*entity.User, common.ErrorCode, error) {
	user, code, message := GetUser(c)
	if code != common.CodeSuccess {
		return nil, code, errors.New(message)
	}
	return user, common.CodeSuccess, nil
}

// billingEnforceApps rejects a new application when the plan quota is full.
func billingEnforceApps(c *gin.Context, tenantID string) bool {
	service := billing.Get()
	if service == nil || !service.Enabled() {
		return true
	}
	err := service.CheckResources(c.Request.Context(), tenantID, billing.ResourceDelta{Apps: 1})
	if err == nil {
		return true
	}
	var insufficient *billing.InsufficientResourceError
	if errors.As(err, &insufficient) {
		common.RespondError(c, common.CodeBillingAppsInsufficient, err.Error(), common.ResponseOptions{Detail: gin.H{
			"resource": "apps", "current": insufficient.Current, "limit": insufficient.Limit,
		}})
		return false
	}
	common.RespondError(c, common.CodeServerError, err.Error())
	return false
}

// billingEnforceStorage rejects a write that would exceed the storage quota.
func billingEnforceStorage(c *gin.Context, tenantID string, size int64) bool {
	service := billing.Get()
	if service == nil || !service.Enabled() || size <= 0 {
		return true
	}
	err := service.CheckResources(c.Request.Context(), tenantID, billing.ResourceDelta{Storage: size})
	if err == nil {
		return true
	}
	var insufficient *billing.InsufficientResourceError
	if errors.As(err, &insufficient) {
		common.RespondError(c, common.CodeBillingStorageInsufficient, err.Error(), common.ResponseOptions{Detail: gin.H{
			"resource": "storage", "current": insufficient.Current, "limit": insufficient.Limit,
		}})
		return false
	}
	common.RespondError(c, common.CodeServerError, err.Error())
	return false
}
