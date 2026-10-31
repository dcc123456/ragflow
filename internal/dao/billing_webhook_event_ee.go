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

package dao

import (
	"context"
	"time"

	"ragflow/internal/entity"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BillingWebhookEventDAO is the data access object for billing_webhook_event.
const processingLease = 15 * time.Minute

type BillingWebhookEventDAO struct{}

// NewBillingWebhookEventDAO creates a webhook event DAO.
func NewBillingWebhookEventDAO() *BillingWebhookEventDAO {
	return &BillingWebhookEventDAO{}
}

// CreateIfAbsent inserts the event row, leaving an existing event untouched.
// It reports whether the row was inserted.
func (dao *BillingWebhookEventDAO) CreateIfAbsent(ctx context.Context, db *gorm.DB, event *entity.BillingWebhookEvent) (bool, error) {
	result := db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "event_id"}},
		DoNothing: true,
	}).Create(event)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// GetByEventID returns the audit row of a Stripe event.
func (dao *BillingWebhookEventDAO) GetByEventID(ctx context.Context, db *gorm.DB, eventID string) (*entity.BillingWebhookEvent, error) {
	var event entity.BillingWebhookEvent
	err := db.WithContext(ctx).Where("event_id = ?", eventID).Limit(1).Take(&event).Error
	if err != nil {
		return nil, err
	}
	return &event, nil
}

// ClaimProcessing atomically claims one delivery for processing.
// It returns false when the event is already completed, unhandled, or being
// processed by another delivery.
func (dao *BillingWebhookEventDAO) ClaimProcessing(ctx context.Context, db *gorm.DB, eventID string) (time.Time, bool, error) {
	var current entity.BillingWebhookEvent
	err := db.WithContext(ctx).Where("event_id = ?", eventID).Limit(1).Take(&current).Error
	if err != nil {
		return time.Time{}, false, err
	}
	switch current.ProcessingStatus {
	case "completed", "unhandled":
		return time.Time{}, false, nil
	case "processing":
		// Refuse a live owner, but allow recovery of a crashed processor after
		// the lease window.
		if current.ProcessingStartedAt != nil && time.Since(*current.ProcessingStartedAt) < processingLease {
			return time.Time{}, false, nil
		}
	}
	query := db.WithContext(ctx).Model(&entity.BillingWebhookEvent{}).
		Where("event_id = ? AND processing_status = ?", eventID, current.ProcessingStatus)
	if current.ProcessingStatus == "processing" {
		if current.ProcessingStartedAt != nil {
			// Fence stale reclaims: only one reader may take over this lease.
			query = query.Where("processing_started_at = ?", *current.ProcessingStartedAt)
		} else {
			query = query.Where("processing_started_at IS NULL")
		}
	}
	// Truncate to the column precision (datetime(3)); the lease is later used
	// as an equality fence, so store and compare the same value.
	lease := time.Now().UTC().Truncate(time.Millisecond)
	result := query.Updates(map[string]interface{}{
		"processing_status":     "processing",
		"processing_started_at": lease,
	})
	if result.Error != nil {
		return time.Time{}, false, result.Error
	}
	if result.RowsAffected != 1 {
		return time.Time{}, false, nil
	}
	return lease, true, nil
}

// MarkCompleted marks an event as completed or unhandled. The lease fences the
// write so a stale owner cannot overwrite a successor's result.
func (dao *BillingWebhookEventDAO) MarkCompleted(ctx context.Context, db *gorm.DB, eventID, status string, lease time.Time) error {
	now := time.Now().UTC()
	return db.WithContext(ctx).Model(&entity.BillingWebhookEvent{}).
		Where("event_id = ? AND processing_status = ? AND processing_started_at = ?", eventID, "processing", lease).
		Updates(map[string]interface{}{
			"processing_status": status,
			"processed_at":      now,
			"last_error":        "",
		}).Error
}

// MarkFailed records a handler failure for the lease that owns the event.
func (dao *BillingWebhookEventDAO) MarkFailed(ctx context.Context, db *gorm.DB, eventID, message string, lease time.Time) error {
	now := time.Now().UTC()
	return db.WithContext(ctx).Model(&entity.BillingWebhookEvent{}).
		Where("event_id = ? AND processing_status = ? AND processing_started_at = ?", eventID, "processing", lease).
		Updates(map[string]interface{}{
			"processing_status": "failed",
			"failed_at":         now,
			"last_error":        message,
		}).Error
}

// ListRetryable returns events that are failed or stuck in processing.
func (dao *BillingWebhookEventDAO) ListRetryable(ctx context.Context, db *gorm.DB, afterID string, limit int) ([]entity.BillingWebhookEvent, error) {
	var events []entity.BillingWebhookEvent
	cutoff := time.Now().UTC().Add(-processingLease)
	query := db.WithContext(ctx).
		Where(
			"processing_status = ? OR (processing_status = ? AND (processing_started_at IS NULL OR processing_started_at < ?))",
			"failed", "processing", cutoff,
		).
		Order("id ASC")
	if afterID != "" {
		query = query.Where("id > ?", afterID)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&events).Error
	return events, err
}
