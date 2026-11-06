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
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/dao"

	"github.com/stripe/stripe-go/v87"
	"github.com/stripe/stripe-go/v87/event"
	"github.com/stripe/stripe-go/v87/webhookendpoint"

	"go.uber.org/zap"
)

const (
	legacyCheckpointKey = "billing_webhook_event_checkpoint"
	checkpointKey       = "billing_webhook_checkpoint"
	webhookIDKey        = "billing_webhook_id"
	webhookSecretKey    = "billing_webhook_secret"
	checkpointRewindSec = int64(600)
)

// RegisterWebhook reconciles the Stripe webhook endpoint with configuration.
// Loopback URLs skip registration (they only work with `stripe listen`).
func (s *Service) RegisterWebhook(ctx context.Context) error {
	webhookURL := strings.TrimSpace(s.cfg.WebhookURL)
	if webhookURL == "" || isLoopbackURL(webhookURL) {
		return nil
	}
	if err := s.EnsureStripe(); err != nil {
		return err
	}

	settingsDAO := dao.NewSystemSettingsDAO()
	storedID, _ := s.settingValue(ctx, webhookIDKey)
	storedSecret, _ := s.settingValue(ctx, webhookSecretKey)
	if storedID != "" {
		endpoint, err := webhookendpoint.Get(storedID, nil)
		if err == nil && endpoint != nil && endpoint.URL == webhookURL && sameEventSet(endpoint.EnabledEvents) {
			if storedSecret != "" {
				return nil
			}
			if endpoint.Secret != "" {
				return settingsDAO.SaveOrCreate(ctx, s.db, webhookSecretKey, endpoint.Secret, "billing", "string")
			}
		}
		if err == nil && endpoint != nil && endpoint.URL != webhookURL {
			if _, deleteErr := webhookendpoint.Del(storedID, nil); deleteErr != nil {
				return fmt.Errorf("delete stale webhook endpoint: %w", deleteErr)
			}
		}
	}

	created, err := webhookendpoint.New(&stripe.WebhookEndpointParams{
		URL:           stripe.String(webhookURL),
		EnabledEvents: stripe.StringSlice(FocusedEvents()),
	})
	if err != nil {
		return fmt.Errorf("register webhook endpoint: %w", err)
	}
	if err := settingsDAO.SaveOrCreate(ctx, s.db, webhookIDKey, created.ID, "billing", "string"); err != nil {
		return err
	}
	if created.Secret != "" {
		if err := settingsDAO.SaveOrCreate(ctx, s.db, webhookSecretKey, created.Secret, "billing", "string"); err != nil {
			return err
		}
	}
	return nil
}

// ReplayUndeliveredEvents replays focused events after the checkpoint in
// chronological order, then drains locally stored retryable events.
func (s *Service) ReplayUndeliveredEvents(ctx context.Context) error {
	if s == nil || !s.cfg.Enabled {
		return nil
	}
	since := s.replayStart(ctx)
	var drainErr error
	if err := s.drainRetryableEvents(ctx); err != nil {
		common.Warn("billing retryable event drain failed", zap.Error(err))
		drainErr = fmt.Errorf("drain retryable events: %w", err)
	}
	if err := s.EnsureStripe(); err != nil {
		return err
	}

	iterator := event.List(&stripe.EventListParams{
		CreatedRange: &stripe.RangeQueryParams{GreaterThanOrEqual: since},
		Limit:        stripe.Int64(100),
	})
	var pending []*stripe.Event
	for iterator.Next() {
		stripeEvent := iterator.Event()
		if stripeEvent == nil || !isFocusedEvent(string(stripeEvent.Type)) {
			continue
		}
		pending = append(pending, stripeEvent)
	}
	if err := iterator.Err(); err != nil {
		return fmt.Errorf("list stripe events: %w", err)
	}
	// Oldest first so a failure leaves a safe, contiguous watermark.
	sort.SliceStable(pending, func(i, j int) bool {
		if pending[i].Created == pending[j].Created {
			return pending[i].ID < pending[j].ID
		}
		return pending[i].Created < pending[j].Created
	})
	for _, stripeEvent := range pending {
		raw, err := json.Marshal(stripeEvent)
		if err != nil {
			continue
		}
		if err := s.HandleWebhookEvent(ctx, raw); err != nil {
			// Do not advance past a failed event; it will be retried on the
			// next startup and is also stored locally for the retry drain.
			common.Warn("billing webhook replay failed", zap.String("event_id", stripeEvent.ID), zap.Error(err))
			break
		}
		if stripeEvent.Created > 0 {
			if err := s.saveCheckpoint(ctx, stripeEvent.Created); err != nil {
				return fmt.Errorf("save billing checkpoint: %w", err)
			}
		}
	}
	return drainErr
}

func (s *Service) replayStart(ctx context.Context) int64 {
	since := time.Now().Unix() - checkpointRewindSec
	if raw, err := s.settingValue(ctx, checkpointKey); err == nil && raw != "" {
		if parsed, parseErr := strconv.ParseInt(raw, 10, 64); parseErr == nil && parsed > 0 {
			return parsed - checkpointRewindSec
		}
	}
	// Migrate the Python checkpoint (JSON with a created field) when present.
	if raw, err := s.settingValue(ctx, legacyCheckpointKey); err == nil && raw != "" {
		var legacy struct {
			Created int64 `json:"created"`
		}
		if json.Unmarshal([]byte(raw), &legacy) == nil && legacy.Created > 0 {
			_ = s.saveCheckpoint(ctx, legacy.Created)
			return legacy.Created - checkpointRewindSec
		}
		if parsed, parseErr := strconv.ParseInt(raw, 10, 64); parseErr == nil && parsed > 0 {
			_ = s.saveCheckpoint(ctx, parsed)
			return parsed - checkpointRewindSec
		}
	}
	return since
}

func (s *Service) drainRetryableEvents(ctx context.Context) error {
	cursor := ""
	var firstErr error
	for {
		rows, err := s.webhookDAO.ListRetryable(ctx, s.db, cursor, 100)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		cursor = rows[len(rows)-1].ID
		for _, row := range rows {
			raw, marshalErr := json.Marshal(row.Payload)
			if marshalErr != nil {
				continue
			}
			if err := s.HandleWebhookEvent(ctx, raw); err != nil {
				if !errors.Is(err, ErrEventInFlight) {
					common.Warn("billing retryable event failed", zap.String("event_id", row.EventID), zap.Error(err))
					if firstErr == nil {
						firstErr = err
					}
				}
				continue
			}
		}
		if len(rows) < 100 {
			break
		}
	}
	return firstErr
}

func (s *Service) settingValue(ctx context.Context, name string) (string, error) {
	settingsDAO := dao.NewSystemSettingsDAO()
	rows, err := settingsDAO.GetByName(ctx, s.db, name)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", nil
	}
	return strings.TrimSpace(rows[0].Value), nil
}

func (s *Service) saveCheckpoint(ctx context.Context, created int64) error {
	current, err := s.settingValue(ctx, checkpointKey)
	if err == nil && current != "" {
		if parsed, parseErr := strconv.ParseInt(current, 10, 64); parseErr == nil && parsed >= created {
			return nil
		}
	}
	settingsDAO := dao.NewSystemSettingsDAO()
	return settingsDAO.SaveOrCreate(ctx, s.db, checkpointKey, strconv.FormatInt(created, 10), "billing", "string")
}

func isFocusedEvent(eventType string) bool {
	for _, focused := range focusedEvents {
		if focused == eventType {
			return true
		}
	}
	return false
}

func sameEventSet(enabled []string) bool {
	if len(enabled) != len(focusedEvents) {
		return false
	}
	set := map[string]bool{}
	for _, item := range enabled {
		set[item] = true
	}
	for _, focused := range focusedEvents {
		if !set[focused] {
			return false
		}
	}
	return true
}

func isLoopbackURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
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

// StorageAddonFromSubscription extracts the storage add-on item of a subscription.
func (s *Service) StorageAddonFromSubscription(object *SubscriptionObject) (string, int64) {
	storagePlan, ok := s.cfg.Plan("storage")
	if !ok || len(storagePlan.PriceIDs) == 0 {
		return "", 0
	}
	unit := storagePlan.QuotaStorage
	for index, item := range object.Items.Data {
		if index == 0 {
			continue
		}
		if item.Price.ID == storagePlan.PriceIDs[0] {
			return item.ID, item.Quantity * unit
		}
	}
	return "", 0
}
