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

package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestParseStorageSize(t *testing.T) {
	cases := []struct {
		input string
		want  int64
		ok    bool
	}{
		{"", 0, true},
		{"100KB", 100 * 1000, true},
		{"300kb", 300 * 1000, true},
		{"5G", 5 * 1000 * 1000 * 1000, true},
		{"1.5GB", 1500 * 1000 * 1000, true},
		{"2TB", 2 * 1000 * 1000 * 1000 * 1000, true},
		{"42", 42, true},
		{"1XB", 0, false},
		{"abc", 0, false},
	}
	for _, tc := range cases {
		got, err := ParseStorageSize(tc.input)
		if tc.ok && err != nil {
			t.Fatalf("ParseStorageSize(%q) error: %v", tc.input, err)
		}
		if !tc.ok && err == nil {
			t.Fatalf("ParseStorageSize(%q) expected error", tc.input)
		}
		if got != tc.want {
			t.Fatalf("ParseStorageSize(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func newBillingViper(t *testing.T, body string) *viper.Viper {
	t.Helper()
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(body)); err != nil {
		t.Fatalf("read config: %v", err)
	}
	return v
}

func TestParseBillingConfigRequiresPriority(t *testing.T) {
	v := newBillingViper(t, `
billing:
  billing_plans:
    - name: Trial
      price_ids: price_trial
      task_priority: low
`)
	cfg := &Config{}
	err := cfg.ParseBillingConfig(v)
	if err == nil || !strings.Contains(err.Error(), "priority") {
		t.Fatalf("expected priority error, got %v", err)
	}
}

func TestParseBillingConfigBuildsIndexes(t *testing.T) {
	v := newBillingViper(t, `
billing:
  stripe_api_key: sk_test
  stripe_api_version: 2026-04-22.dahlia
  webhook_url: http://127.0.0.1/v1/billing/webhooks/stripe
  billing_plans:
    - name: storage
      priority: 0
      quota_storage: 1G
      price_ids: price_storage
      product_type: addon
      task_priority: low
    - name: Trial
      priority: 1
      quota_apps: 5
      quota_members: 1
      quota_storage: 100MB
      quota_points: 500
      price_ids: price_trial
      product_type: subscription
      task_priority: low
      api_request_limit_per_minute: 10
`)
	cfg := &Config{}
	if err := cfg.ParseBillingConfig(v); err != nil {
		t.Fatalf("parse: %v", err)
	}
	billing := cfg.GetBillingConfig()
	if billing.StripeAPIKey != "sk_test" || billing.StripeAPIVersion != "2026-04-22.dahlia" {
		t.Fatalf("stripe config = %+v", billing)
	}
	trial, ok := billing.Plan("Trial")
	if !ok {
		t.Fatal("Trial plan missing")
	}
	if trial.QuotaStorage != 100*1000*1000 || trial.QuotaPoints != 500 || trial.APIRequestLimitPerMinute != 10 {
		t.Fatalf("trial plan = %+v", trial)
	}
	if len(trial.PriceIDs) != 1 || trial.PriceIDs[0] != "price_trial" {
		t.Fatalf("trial price ids = %v", trial.PriceIDs)
	}
	if billing.PriceIDToPlan["price_trial"] != "Trial" {
		t.Fatalf("price index = %v", billing.PriceIDToPlan)
	}
	if billing.PriorityToPlans[1][0] != "Trial" {
		t.Fatalf("priority index = %v", billing.PriorityToPlans)
	}
	if billing.StorageBytesPerUnit() != 1000*1000*1000 {
		t.Fatalf("storage unit = %d", billing.StorageBytesPerUnit())
	}
}
