//go:build ee

package billing

import (
	"context"
	"strings"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/server/config"
)

func TestTenantTokenRateLimit(t *testing.T) {
	const accessToken = "rate-limit-test-access-token"
	signed, err := common.DumpAccessToken(accessToken, "test-tenant", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := loginTokenKey(accessToken), strings.Split(signed, ".")[1]; got != want {
		t.Fatalf("login rate-limit key = %q, want v2 access-token segment %q", got, want)
	}
	s := newTestService(t)
	s.cfg.PlansByName["Trial"].APIRequestLimitPerMinute = 25
	s.cfg.PlansByName["Starter"] = &config.BillingPlanInfo{Name: "Starter", APIRequestLimitPerMinute: 100}
	s.cfg.PlansByName["Pro"] = &config.BillingPlanInfo{Name: "Pro", APIRequestLimitPerMinute: 1000}
	ctx := context.Background()
	for _, tc := range []struct {
		name, plan, status string
		want               int64
	}{
		{"starter", "Starter", "active", 100},
		{"pro", "Pro", "active", 1000},
		{"normalized", " starter ", "ACTIVE", 100},
		{"trial uses Lua default", "Trial", "active", 0},
		{"free uses Lua default", "Free", "active", 0},
		{"canceled paid plan", "Pro", "canceled", 0},
		{"past due paid plan", "Starter", "past_due", 0},
		{"trialing paid plan", "Starter", "trialing", 0},
		{"missing status", "Pro", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := entity.BillingSubscription{ID: tc.name, TenantID: tc.name, PlanName: tc.plan, SubscriptionStatus: tc.status}
			if err := s.db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			rpm, err := s.tenantTokenRateLimit(ctx, tc.name)
			if err != nil || rpm != tc.want {
				t.Fatalf("rate limit = %d, %v; want %d, nil", rpm, err, tc.want)
			}
		})
	}
	if rpm, err := s.tenantTokenRateLimit(ctx, "no-subscription"); err != nil || rpm != 0 {
		t.Fatalf("missing subscription = %d, %v; want 0, nil", rpm, err)
	}
	if err := s.db.Migrator().DropTable(&entity.BillingSubscription{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.tenantTokenRateLimit(ctx, "database-error"); err == nil {
		t.Fatal("database failure must not be treated as a free plan")
	}
}
