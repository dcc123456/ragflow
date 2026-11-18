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
	"context"
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	"ragflow/internal/engine/kvrocks"

	"go.uber.org/zap"
)

const (
	rateLimitTokenPrefix = "billing:ratelimit:token:"
	apiAuthPlanPrefix    = "billing:api_auth_plan:"
	loginTokenTTL        = 24 * time.Hour
	apiAuthPlanTTL       = 24 * time.Hour
)

// SyncLoginTokenRateLimit writes the Starter/Pro limit of a login token.
// Login tokens expire with the session; other plans use the Lua default.
func (s *Service) SyncLoginTokenRateLimit(ctx context.Context, tenantID, accessToken string) {
	if s == nil || !s.cfg.Enabled || accessToken == "" {
		return
	}
	client := kvrocks.Get()
	if client == nil {
		return
	}
	rpm, err := s.tenantTokenRateLimit(ctx, tenantID)
	if err != nil {
		billingLogger.Warn("resolve login token rate limit failed", zap.String("tenant_id", tenantID), zap.Error(err))
		return
	}
	if rpm == 0 {
		s.DeleteRateLimitKeys(ctx, loginTokenKey(accessToken))
		return
	}
	if ok := client.Set(ctx, rateLimitTokenPrefix+loginTokenKey(accessToken), strconv.FormatInt(rpm, 10), loginTokenTTL); !ok {
		billingLogger.Warn("sync login token rate limit failed", zap.String("tenant_id", tenantID))
	}
}

// loginTokenKey matches the second segment of a signed v2 login token.
func loginTokenKey(accessToken string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(accessToken))
}

// SyncAPITokenRateLimit writes the Starter/Pro limit of API/Beta tokens.
// API tokens have no TTL; other plans use the Lua default.
func (s *Service) SyncAPITokenRateLimit(ctx context.Context, tenantID string, tokens ...string) {
	if s == nil || !s.cfg.Enabled {
		return
	}
	client := kvrocks.Get()
	if client == nil {
		return
	}
	rpm, err := s.tenantTokenRateLimit(ctx, tenantID)
	if err != nil {
		billingLogger.Warn("resolve api token rate limit failed", zap.String("tenant_id", tenantID), zap.Error(err))
		return
	}
	if rpm == 0 {
		s.DeleteRateLimitKeys(ctx, tokens...)
		return
	}
	value := strconv.FormatInt(rpm, 10)
	for _, token := range tokens {
		if token == "" {
			continue
		}
		if ok := client.Set(ctx, rateLimitTokenPrefix+token, value, 0); !ok {
			billingLogger.Warn("sync api token rate limit failed", zap.String("tenant_id", tenantID))
		}
	}
}

// DeleteRateLimitKeys removes token rate-limit entries.
func (s *Service) DeleteRateLimitKeys(ctx context.Context, tokens ...string) {
	keys := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token != "" {
			keys = append(keys, rateLimitTokenPrefix+token)
		}
	}
	if len(keys) > 0 {
		if client := kvrocks.Get(); client != nil {
			for _, key := range keys {
				if err := client.GetClient().Del(ctx, key).Err(); err != nil {
					billingLogger.Warn("delete token rate limit failed", zap.Error(err))
				}
			}
		}
	}
}

// SyncAPITokenAccess applies the tenant plan and the per-token rate limits to
// a freshly issued set of API tokens.
func (s *Service) SyncAPITokenAccess(ctx context.Context, tenantID string, tokens ...string) {
	s.SyncAPITokenRateLimit(ctx, tenantID, tokens...)
	s.SyncAPIAuthPlan(ctx, tenantID)
}

// SyncAPIAuthPlan caches whether the tenant plan may use API keys.
func (s *Service) SyncAPIAuthPlan(ctx context.Context, tenantID string) {
	if s == nil || !s.cfg.Enabled {
		return
	}
	allowed, err := s.APIAuthAllowed(ctx, tenantID)
	if err != nil {
		billingLogger.Warn("sync api auth plan failed", zap.String("tenant_id", tenantID), zap.Error(err))
		return
	}
	value := "0"
	if allowed {
		value = "1"
	}
	client := kvrocks.Get()
	if client == nil {
		return
	}
	if ok := client.Set(ctx, apiAuthPlanPrefix+tenantID, value, apiAuthPlanTTL); !ok {
		billingLogger.Warn("write api auth plan failed", zap.String("tenant_id", tenantID))
	}
}

// SyncAllAPITokenRateLimit refreshes rate-limit keys for every API token.
func (s *Service) SyncAllAPITokenRateLimit(ctx context.Context) error {
	if s == nil || !s.cfg.Enabled {
		return nil
	}
	subscriptions, err := s.subscriptionDAO.ListActivePaid(ctx, s.db)
	if err != nil {
		return err
	}
	for _, subscription := range subscriptions {
		var tokens []string
		rows, err := s.db.WithContext(ctx).Table("api_token").
			Select("token, beta").
			Where("tenant_id = ?", subscription.TenantID).
			Rows()
		if err != nil {
			continue
		}
		for rows.Next() {
			var token string
			var beta *string
			if scanErr := rows.Scan(&token, &beta); scanErr != nil {
				continue
			}
			tokens = append(tokens, token)
			if beta != nil && *beta != "" {
				tokens = append(tokens, *beta)
			}
		}
		rows.Close()
		s.SyncAPITokenAccess(ctx, subscription.TenantID, tokens...)
	}
	return nil
}

// syncTenantTokenLimits refreshes rate-limit keys for every existing token of
// a tenant after a plan or payment-state change.
func (s *Service) syncTenantTokenLimits(ctx context.Context, tenantID string) {
	if s == nil || !s.cfg.Enabled {
		return
	}
	var tokens []string
	rows, err := s.db.WithContext(ctx).Table("api_token").
		Select("token, beta").
		Where("tenant_id = ?", tenantID).
		Rows()
	if err == nil {
		for rows.Next() {
			var token string
			var beta *string
			if scanErr := rows.Scan(&token, &beta); scanErr != nil {
				continue
			}
			tokens = append(tokens, token)
			if beta != nil && *beta != "" {
				tokens = append(tokens, *beta)
			}
		}
		rows.Close()
	}
	s.SyncAPITokenRateLimit(ctx, tenantID, tokens...)
	// Refresh login access tokens belonging to the tenant as well; they carry
	// a 24h TTL that would otherwise keep the old RPM after a plan change.
	userRows, userErr := s.db.WithContext(ctx).Table("user").
		Select("user.access_token").
		Joins("INNER JOIN user_tenant ON user_tenant.user_id = user.id").
		Where("user_tenant.tenant_id = ? AND user.access_token IS NOT NULL", tenantID).
		Rows()
	if userErr == nil {
		for userRows.Next() {
			var accessToken *string
			if scanErr := userRows.Scan(&accessToken); scanErr == nil && accessToken != nil && *accessToken != "" {
				s.SyncLoginTokenRateLimit(ctx, tenantID, *accessToken)
			}
		}
		userRows.Close()
	}
	s.SyncAPIAuthPlan(ctx, tenantID)
}

// tenantTokenRateLimit returns zero when Lua should use its hardcoded default.
func (s *Service) tenantTokenRateLimit(ctx context.Context, tenantID string) (int64, error) {
	subscription, err := s.Subscription(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	if subscription == nil || !strings.EqualFold(strings.TrimSpace(subscription.SubscriptionStatus), "active") {
		return 0, nil
	}
	switch strings.ToLower(strings.TrimSpace(subscription.PlanName)) {
	case "starter":
		return s.PlanRateLimit("Starter"), nil
	case "pro":
		return s.PlanRateLimit("Pro"), nil
	default:
		return 0, nil
	}
}
