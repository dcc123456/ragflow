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
	"encoding/base64"
	"encoding/json"
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

// SyncLoginTokenRateLimit writes the per-minute limit of a login token.
// Login tokens expire with the session.
func (s *Service) SyncLoginTokenRateLimit(ctx context.Context, tenantID, accessToken string) {
	if s == nil || !s.cfg.Enabled || accessToken == "" {
		return
	}
	client := kvrocks.Get()
	if client == nil {
		return
	}
	rpm := s.PlanRateLimit(s.tenantPlanName(ctx, tenantID))
	if ok := client.Set(ctx, rateLimitTokenPrefix+loginTokenKey(accessToken), strconv.FormatInt(rpm, 10), loginTokenTTL); !ok {
		billingLogger.Warn("sync login token rate limit failed", zap.String("tenant_id", tenantID))
	}
}

// loginTokenKey mirrors the Lua limiter's JWT normalisation: the first JWT
// segment is base64url(JSON-encoded token), so the stored key must use the same
// encoding or every session lookup misses.
func loginTokenKey(accessToken string) string {
	encoded, err := json.Marshal(accessToken)
	if err != nil {
		return accessToken
	}
	return base64.RawURLEncoding.EncodeToString(encoded)
}

// SyncAPITokenRateLimit writes the per-minute limit of API/Beta tokens.
// API tokens deliberately have no TTL, matching the Python contract.
func (s *Service) SyncAPITokenRateLimit(ctx context.Context, tenantID string, tokens ...string) {
	if s == nil || !s.cfg.Enabled {
		return
	}
	client := kvrocks.Get()
	if client == nil {
		return
	}
	rpm := s.PlanRateLimit(s.tenantPlanName(ctx, tenantID))
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
				client.Delete(ctx, key)
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
	subscriptions, err := s.subscriptionDAO.ListActive(ctx, s.db)
	if err != nil {
		return err
	}
	for _, subscription := range subscriptions {
		if !strings.EqualFold(subscription.SubscriptionStatus, "active") && subscription.SubscriptionStatus != "" {
			continue
		}
		plan := strings.ToLower(strings.TrimSpace(subscription.PlanName))
		if plan != "starter" && plan != "pro" {
			continue
		}
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

func (s *Service) tenantPlanName(ctx context.Context, tenantID string) string {
	subscription, err := s.Subscription(ctx, tenantID)
	if err != nil || subscription == nil || subscription.PlanName == "" {
		return configTrialName()
	}
	return subscription.PlanName
}

func configTrialName() string {
	return "Trial"
}
