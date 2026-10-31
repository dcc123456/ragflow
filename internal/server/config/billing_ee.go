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
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

const (
	// BillingPlanTrialName is the free plan every tenant falls back to.
	BillingPlanTrialName = "Trial"
	// StorageProductName is the add-on product that backs storage purchases.
	StorageProductName = "storage"
	// BytesPerGB is the decimal gigabyte used by the billing rules.
	BytesPerGB int64 = 1000 * 1000 * 1000
)

var storageSizePattern = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*([kmgt]b?)?$`)

// ParseStorageSize converts a human readable storage size into bytes using
// decimal units, matching the Python billing parser.
func ParseStorageSize(size string) (int64, error) {
	size = strings.TrimSpace(strings.ToLower(size))
	if size == "" {
		return 0, nil
	}
	match := storageSizePattern.FindStringSubmatch(size)
	if match == nil {
		return 0, fmt.Errorf("invalid storage size format: %q", size)
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid storage size %q: %w", size, err)
	}
	unit := match[2]
	if unit == "" {
		unit = "b"
	}
	multipliers := map[string]int64{
		"b":  1,
		"k":  1000,
		"kb": 1000,
		"m":  1000 * 1000,
		"mb": 1000 * 1000,
		"g":  1000 * 1000 * 1000,
		"gb": 1000 * 1000 * 1000,
		"t":  1000 * 1000 * 1000 * 1000,
		"tb": 1000 * 1000 * 1000 * 1000,
	}
	multiplier, ok := multipliers[unit]
	if !ok {
		return 0, fmt.Errorf("unknown unit %q in storage size", unit)
	}
	return int64(value * float64(multiplier)), nil
}

// BillingPricePointConfig is one metered unit defined in service_conf.yaml.
type BillingPricePointConfig struct {
	ProductName          string `mapstructure:"product_name"`
	PriceType            string `mapstructure:"price_type"`
	BillingFrequency     string `mapstructure:"billing_frequency"`
	Unit                 string `mapstructure:"unit"`
	UnitQuantity         int64  `mapstructure:"unit_quantity"`
	PriceAmount          int64  `mapstructure:"price_amount"`
	PriceCurrency        string `mapstructure:"price_currency"`
	ConsumingPointAmount int64  `mapstructure:"consuming_point_amount"`
	IncludedFreeAmount   int64  `mapstructure:"included_free_amount"`
}

// BillingPointsRechargeConfig describes the one-off points purchase price.
type BillingPointsRechargeConfig struct {
	PriceID       string `mapstructure:"price_id"`
	PointsPerUnit int64  `mapstructure:"points_per_unit"`
}

// BillingPlanConfig mirrors one entry of billing.billing_plans.
type BillingPlanConfig struct {
	Name                     string `mapstructure:"name"`
	Priority                 *int   `mapstructure:"priority"`
	TaskPriority             string `mapstructure:"task_priority"`
	PriceIDs                 string `mapstructure:"price_ids"`
	ProductType              string `mapstructure:"product_type"`
	QuotaApps                int64  `mapstructure:"quota_apps"`
	QuotaMembers             int64  `mapstructure:"quota_members"`
	QuotaStorage             any    `mapstructure:"quota_storage"`
	QuotaPoints              int64  `mapstructure:"quota_points"`
	APIRequestLimitPerMinute *int64 `mapstructure:"api_request_limit_per_minute"`

	// QuotaStorageBytes is the normalized quota_storage value.
	QuotaStorageBytes int64 `mapstructure:"-"`
	// PriceIDList is price_ids split on whitespace.
	PriceIDList []string `mapstructure:"-"`
}

// BillingPlanInfo is the normalized, lookup-friendly form of a plan.
type BillingPlanInfo struct {
	Name                     string
	Priority                 int
	TaskPriority             string
	PriceIDs                 []string
	ProductType              string
	QuotaApps                int64
	QuotaMembers             int64
	QuotaStorage             int64
	QuotaPoints              int64
	APIRequestLimitPerMinute int64
}

// BillingConfig is the parsed billing section plus derived indexes.
type BillingConfig struct {
	Enabled              bool
	StripeAPIKey         string
	StripePublishableKey string
	StripeAPIVersion     string
	WebhookURL           string
	PricePoints          []BillingPricePointConfig
	PointsRecharge       BillingPointsRechargeConfig
	Plans                []BillingPlanConfig

	PlansByName     map[string]*BillingPlanInfo
	PriceIDToPlan   map[string]string
	PriorityToPlans map[int][]string
}

// Plan returns the normalized plan info for name.
func (c BillingConfig) Plan(name string) (*BillingPlanInfo, bool) {
	info, ok := c.PlansByName[name]
	return info, ok
}

// TrialPlan returns the configured Trial plan.
func (c BillingConfig) TrialPlan() *BillingPlanInfo {
	if info, ok := c.PlansByName[BillingPlanTrialName]; ok {
		return info
	}
	return nil
}

// StorageBytesPerUnit returns the byte size of one purchasable storage unit.
func (c BillingConfig) StorageBytesPerUnit() int64 {
	if info, ok := c.PlansByName[StorageProductName]; ok {
		return info.QuotaStorage
	}
	return 0
}

func billingEnabledFromEnv() bool {
	value := strings.TrimSpace(os.Getenv("BILLING_ENABLED"))
	if value == "" {
		return false
	}
	enabled, err := strconv.Atoi(value)
	return err == nil && enabled != 0
}

// ParseBillingConfig parses the billing section and validates the plan set.
func (c *Config) ParseBillingConfig(v *viper.Viper) error {
	parsed := BillingConfig{
		Enabled:         billingEnabledFromEnv(),
		PlansByName:     map[string]*BillingPlanInfo{},
		PriceIDToPlan:   map[string]string{},
		PriorityToPlans: map[int][]string{},
	}

	if v.IsSet("billing") {
		var raw struct {
			StripeAPIKey         string                      `mapstructure:"stripe_api_key"`
			StripePublishableKey string                      `mapstructure:"stripe_publishable_key"`
			StripeAPIVersion     string                      `mapstructure:"stripe_api_version"`
			WebhookURL           string                      `mapstructure:"webhook_url"`
			PricePoints          []BillingPricePointConfig   `mapstructure:"price_point"`
			PointsRecharge       BillingPointsRechargeConfig `mapstructure:"points_recharge"`
			Plans                []BillingPlanConfig         `mapstructure:"billing_plans"`
		}
		if err := v.UnmarshalKey("billing", &raw); err != nil {
			return fmt.Errorf("unmarshal billing config: %w", err)
		}
		parsed.StripeAPIKey = raw.StripeAPIKey
		parsed.StripePublishableKey = raw.StripePublishableKey
		parsed.StripeAPIVersion = raw.StripeAPIVersion
		parsed.WebhookURL = raw.WebhookURL
		parsed.PricePoints = raw.PricePoints
		parsed.PointsRecharge = raw.PointsRecharge
		parsed.Plans = raw.Plans
	}

	for i := range parsed.Plans {
		plan := &parsed.Plans[i]
		if plan.Priority == nil {
			return fmt.Errorf("billing plan %q is missing required priority in billing.billing_plans", plan.Name)
		}
		info := &BillingPlanInfo{
			Name:         plan.Name,
			Priority:     *plan.Priority,
			TaskPriority: plan.TaskPriority,
			ProductType:  plan.ProductType,
			QuotaApps:    plan.QuotaApps,
			QuotaMembers: plan.QuotaMembers,
			QuotaPoints:  plan.QuotaPoints,
		}
		if info.TaskPriority == "" {
			info.TaskPriority = "low"
		}
		if plan.APIRequestLimitPerMinute != nil {
			info.APIRequestLimitPerMinute = *plan.APIRequestLimitPerMinute
		}
		for _, priceID := range strings.Fields(plan.PriceIDs) {
			info.PriceIDs = append(info.PriceIDs, priceID)
			parsed.PriceIDToPlan[priceID] = plan.Name
		}
		storageBytes, err := normalizeQuotaStorage(plan.QuotaStorage)
		if err != nil {
			return fmt.Errorf("billing plan %q: %w", plan.Name, err)
		}
		info.QuotaStorage = storageBytes
		plan.QuotaStorageBytes = storageBytes
		plan.PriceIDList = info.PriceIDs

		parsed.PlansByName[plan.Name] = info
		parsed.PriorityToPlans[info.Priority] = append(parsed.PriorityToPlans[info.Priority], plan.Name)
	}

	c.billing = parsed
	return nil
}

// GetBillingConfig returns the parsed billing configuration.
func (c *Config) GetBillingConfig() BillingConfig {
	return c.billing
}

func normalizeQuotaStorage(value any) (int64, error) {
	switch typed := value.(type) {
	case nil:
		return 0, nil
	case string:
		return ParseStorageSize(typed)
	case int:
		return int64(typed), nil
	case int64:
		return typed, nil
	case float64:
		return int64(typed), nil
	default:
		return 0, fmt.Errorf("unsupported quota_storage type %T", value)
	}
}
