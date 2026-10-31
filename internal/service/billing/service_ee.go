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
	"fmt"
	"sync"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/server/config"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

var (
	defaultService   *Service
	defaultServiceMu sync.RWMutex
)

// Service owns billing reads and writes for the Go API server.
type Service struct {
	cfg config.BillingConfig
	db  *gorm.DB

	productDAO      *dao.BillingProductDAO
	subscriptionDAO *dao.BillingSubscriptionDAO
	orderDAO        *dao.BillingOrderDAO
	webhookDAO      *dao.BillingWebhookEventDAO
	pointDAO        *dao.BillingPointDAO
	stripeOnce      sync.Once
}

// New creates a billing service over the given configuration and database.
func New(cfg config.BillingConfig, db *gorm.DB) *Service {
	service := &Service{
		cfg:             cfg,
		db:              db,
		productDAO:      dao.NewBillingProductDAO(),
		subscriptionDAO: dao.NewBillingSubscriptionDAO(),
		orderDAO:        dao.NewBillingOrderDAO(),
		webhookDAO:      dao.NewBillingWebhookEventDAO(),
		pointDAO:        dao.NewBillingPointDAO(),
	}
	service.initStripe()
	return service
}

// Init builds the package-level service from the loaded configuration.
// It seeds the plan catalogue when billing is enabled.
func Init(ctx context.Context, cfg config.BillingConfig) error {
	service := New(cfg, dao.GetDB())
	SetDefault(service)

	if !cfg.Enabled {
		common.Info("Billing is disabled")
		return nil
	}
	if err := service.Seed(ctx); err != nil {
		return err
	}
	common.Info("Billing plan catalogue seeded")
	return nil
}

// Start runs the API-only billing startup steps: webhook endpoint
// reconciliation, undelivered-event replay, token limit sync and the
// downgrade guard. Webhook registration is fail-fast; the rest is background.
func Start(ctx context.Context) error {
	service := Get()
	if service == nil || !service.Enabled() {
		return nil
	}
	if err := service.RegisterWebhook(ctx); err != nil {
		return err
	}
	go func() {
		replayCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := service.ReplayUndeliveredEvents(replayCtx); err != nil {
			common.Warn("Billing undelivered event replay failed", zap.Error(err))
		}
		if err := service.SyncAllAPITokenRateLimit(replayCtx); err != nil {
			common.Warn("Billing API token rate limit sync failed", zap.Error(err))
		}
	}()
	service.StartDowngradeGuard(ctx)
	return nil
}

// Get returns the package-level billing service, or nil before Init.
func Get() *Service {
	defaultServiceMu.RLock()
	defer defaultServiceMu.RUnlock()
	return defaultService
}

// SetDefault installs the package-level service and returns a function that
// restores the previous one.
func SetDefault(service *Service) func() {
	defaultServiceMu.Lock()
	previous := defaultService
	defaultService = service
	defaultServiceMu.Unlock()
	return func() {
		defaultServiceMu.Lock()
		defaultService = previous
		defaultServiceMu.Unlock()
	}
}

// Enabled reports whether billing is configured on this deployment.
func (s *Service) Enabled() bool {
	return s != nil && s.cfg.Enabled
}

// Config returns the parsed billing configuration.
func (s *Service) Config() config.BillingConfig {
	if s == nil {
		return config.BillingConfig{}
	}
	return s.cfg
}

// DB returns the database handle used by the service.
func (s *Service) DB() *gorm.DB {
	if s == nil {
		return nil
	}
	return s.db
}

// Seed upserts the plan catalogue and price points described by configuration.
// Products are versioned: a changed plan inserts a new row with version+1.
func (s *Service) Seed(ctx context.Context) error {
	if s == nil || !s.cfg.Enabled {
		return nil
	}
	for _, plan := range s.cfg.Plans {
		info, ok := s.cfg.Plan(plan.Name)
		if !ok {
			continue
		}
		if err := s.seedPlan(ctx, info); err != nil {
			return fmt.Errorf("seed billing plan %q: %w", plan.Name, err)
		}
	}
	for _, pricePoint := range s.cfg.PricePoints {
		if err := s.seedPricePoint(ctx, pricePoint); err != nil {
			return fmt.Errorf("seed price point %q: %w", pricePoint.ProductName, err)
		}
	}
	return nil
}

func (s *Service) seedPlan(ctx context.Context, plan *config.BillingPlanInfo) error {
	existing, err := s.productDAO.GetLatestByName(ctx, s.db, plan.Name)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	quotaPoints := plan.QuotaPoints
	apiLimit := plan.APIRequestLimitPerMinute
	priceIDs := joinPriceIDs(plan.PriceIDs)

	if existing == nil {
		product := &entity.BillingProduct{
			ID:                       common.GenerateUUID(),
			Name:                     plan.Name,
			Priority:                 plan.Priority,
			QuotaApps:                plan.QuotaApps,
			QuotaMembers:             plan.QuotaMembers,
			QuotaStorage:             plan.QuotaStorage,
			TaskPriority:             plan.TaskPriority,
			PriceIDs:                 priceIDs,
			ProductType:              plan.ProductType,
			Version:                  1,
			QuotaPoints:              &quotaPoints,
			APIRequestLimitPerMinute: &apiLimit,
		}
		return s.productDAO.UpsertProduct(ctx, s.db, product)
	}

	if !planChanged(existing, plan, priceIDs) {
		return nil
	}
	product := &entity.BillingProduct{
		ID:                       common.GenerateUUID(),
		Name:                     plan.Name,
		Priority:                 plan.Priority,
		QuotaApps:                plan.QuotaApps,
		QuotaMembers:             plan.QuotaMembers,
		QuotaStorage:             plan.QuotaStorage,
		TaskPriority:             plan.TaskPriority,
		PriceIDs:                 priceIDs,
		ProductType:              plan.ProductType,
		Version:                  existing.Version + 1,
		QuotaPoints:              &quotaPoints,
		APIRequestLimitPerMinute: &apiLimit,
	}
	return s.productDAO.UpsertProduct(ctx, s.db, product)
}

func planChanged(existing *entity.BillingProduct, plan *config.BillingPlanInfo, priceIDs string) bool {
	if existing.Priority != plan.Priority ||
		existing.QuotaApps != plan.QuotaApps ||
		existing.QuotaMembers != plan.QuotaMembers ||
		existing.QuotaStorage != plan.QuotaStorage ||
		existing.TaskPriority != plan.TaskPriority ||
		existing.PriceIDs != priceIDs ||
		existing.ProductType != plan.ProductType {
		return true
	}
	if existing.QuotaPoints == nil || *existing.QuotaPoints != plan.QuotaPoints {
		return true
	}
	if existing.APIRequestLimitPerMinute == nil || *existing.APIRequestLimitPerMinute != plan.APIRequestLimitPerMinute {
		return true
	}
	return false
}

func (s *Service) seedPricePoint(ctx context.Context, pricePoint config.BillingPricePointConfig) error {
	product, err := s.productDAO.GetLatestByName(ctx, s.db, pricePoint.ProductName)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	existing, err := s.productDAO.GetPricePointByName(ctx, s.db, pricePoint.ProductName)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	unit := pricePoint.Unit
	unitQuantity := pricePoint.UnitQuantity
	priceAmount := pricePoint.PriceAmount
	priceCurrency := pricePoint.PriceCurrency
	included := pricePoint.IncludedFreeAmount

	if existing == nil {
		productID := ""
		if product != nil {
			productID = product.ID
		}
		point := &entity.BillingPricePoint{
			ID:                   common.GenerateUUID(),
			ProductID:            productID,
			ProductName:          pricePoint.ProductName,
			PriceType:            pricePoint.PriceType,
			BillingFrequency:     pricePoint.BillingFrequency,
			IncludedFreeAmount:   &included,
			Unit:                 &unit,
			UnitQuantity:         &unitQuantity,
			PriceAmount:          &priceAmount,
			PriceCurrency:        &priceCurrency,
			ConsumingPointAmount: pricePoint.ConsumingPointAmount,
			EffectiveTime:        time.Now().UTC(),
		}
		return s.productDAO.UpsertPricePoint(ctx, s.db, point)
	}

	if !pricePointChanged(existing, pricePoint) {
		return nil
	}
	point := &entity.BillingPricePoint{
		ID:                   common.GenerateUUID(),
		ProductID:            existing.ProductID,
		ProductName:          pricePoint.ProductName,
		PriceType:            pricePoint.PriceType,
		BillingFrequency:     pricePoint.BillingFrequency,
		IncludedFreeAmount:   &included,
		Unit:                 &unit,
		UnitQuantity:         &unitQuantity,
		PriceAmount:          &priceAmount,
		PriceCurrency:        &priceCurrency,
		ConsumingPointAmount: pricePoint.ConsumingPointAmount,
		EffectiveTime:        time.Now().UTC(),
	}
	return s.productDAO.UpsertPricePoint(ctx, s.db, point)
}

func pricePointChanged(existing *entity.BillingPricePoint, pricePoint config.BillingPricePointConfig) bool {
	if existing.PriceType != pricePoint.PriceType ||
		existing.BillingFrequency != pricePoint.BillingFrequency ||
		existing.ConsumingPointAmount != pricePoint.ConsumingPointAmount {
		return true
	}
	if existing.PriceAmount == nil || *existing.PriceAmount != pricePoint.PriceAmount {
		return true
	}
	if existing.Unit == nil || *existing.Unit != pricePoint.Unit {
		return true
	}
	if existing.UnitQuantity == nil || *existing.UnitQuantity != pricePoint.UnitQuantity {
		return true
	}
	return false
}

// Plan returns the configured plan info.
func (s *Service) Plan(name string) (*config.BillingPlanInfo, bool) {
	if s == nil {
		return nil, false
	}
	return s.cfg.Plan(name)
}

// TrialPlan returns the configured Trial plan.
func (s *Service) TrialPlan() *config.BillingPlanInfo {
	if s == nil {
		return nil
	}
	return s.cfg.TrialPlan()
}

// PlanQuotaPoints returns the monthly point quota of a tenant's plan.
func (s *Service) PlanQuotaPoints(ctx context.Context, tenantID string) (int64, error) {
	subscription, err := s.subscriptionDAO.GetByTenantID(ctx, s.db, tenantID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if trial := s.TrialPlan(); trial != nil {
				return trial.QuotaPoints, nil
			}
			return 0, nil
		}
		return 0, err
	}
	if plan, ok := s.cfg.Plan(subscription.PlanName); ok {
		return plan.QuotaPoints, nil
	}
	if trial := s.TrialPlan(); trial != nil {
		return trial.QuotaPoints, nil
	}
	return 0, nil
}

// Subscription returns the main subscription of a tenant, or nil when absent.
func (s *Service) Subscription(ctx context.Context, tenantID string) (*entity.BillingSubscription, error) {
	subscription, err := s.subscriptionDAO.GetByTenantID(ctx, s.db, tenantID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return subscription, nil
}

// PlanRateLimit returns the per-minute API request limit of a plan name.
func (s *Service) PlanRateLimit(planName string) int64 {
	if plan, ok := s.cfg.Plan(planName); ok && plan.APIRequestLimitPerMinute > 0 {
		return plan.APIRequestLimitPerMinute
	}
	if trial := s.TrialPlan(); trial != nil && trial.APIRequestLimitPerMinute > 0 {
		return trial.APIRequestLimitPerMinute
	}
	return 10
}

func joinPriceIDs(priceIDs []string) string {
	if len(priceIDs) == 0 {
		return ""
	}
	out := priceIDs[0]
	for _, id := range priceIDs[1:] {
		out += " " + id
	}
	return out
}

var billingLogger = zap.L().Named("billing")
