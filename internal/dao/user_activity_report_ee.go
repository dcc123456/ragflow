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

package dao

import (
	"context"
	"time"

	"ragflow/internal/entity"
	"ragflow/internal/server/config"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func userActivityReportQuery(ctx context.Context, db *gorm.DB, plan string, cutoff time.Time, inactive bool) *gorm.DB {
	// Billing persists the current plan independently of the payment status.
	query := db.WithContext(ctx).Table("? AS u", clause.Table{Name: "user"}).
		Joins("LEFT JOIN billing_subscription sub ON u.id = sub.tenant_id").
		Where("u.status = ? AND u.is_anonymous = ?", string(entity.StatusValid), "0")
	if plan != "" {
		query = query.Where("COALESCE(sub.plan_name, ?) = ?", config.BillingPlanTrialName, plan)
	}
	if inactive {
		return query.Where("(u.last_login_time IS NULL OR u.last_login_time < ?)", cutoff)
	}
	return query.Where("u.last_login_time >= ?", cutoff)
}

// UserInactiveByPlanRow contains an inactivity count for a plan.
type UserInactiveByPlanRow struct {
	PlanName      string
	InactiveCount int64
}

// ListInactiveByPlan groups users who have not logged in since the cutoff.
func (dao *UserDAO) ListInactiveByPlan(ctx context.Context, db *gorm.DB, plan string, cutoff time.Time) ([]UserInactiveByPlanRow, error) {
	plans := userActivityReportQuery(ctx, db, plan, cutoff, true).
		Select("COALESCE(sub.plan_name, ?) AS plan_name", config.BillingPlanTrialName)
	rows := make([]UserInactiveByPlanRow, 0)
	err := db.WithContext(ctx).Table("(?) AS report_users", plans).
		Select("plan_name, COUNT(*) AS inactive_count").Group("plan_name").Order("plan_name ASC").Find(&rows).Error
	return rows, err
}

// UserPlanRow contains a recently logged-in user's plan assignment.
type UserPlanRow struct {
	Email         string
	PlanName      string
	LastLoginTime *time.Time
}

// ListPlanRows returns recent users of a plan and the total before pagination.
func (dao *UserDAO) ListPlanRows(ctx context.Context, db *gorm.DB, plan string, cutoff time.Time, offset, limit int) ([]UserPlanRow, int64, error) {
	query := userActivityReportQuery(ctx, db, plan, cutoff, false)
	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	query = query.Select("u.email, COALESCE(sub.plan_name, ?) AS plan_name, u.last_login_time", config.BillingPlanTrialName).
		Order("u.last_login_time DESC").Order("u.id ASC")
	if offset > 0 {
		query = query.Offset(offset)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	rows := make([]UserPlanRow, 0)
	err := query.Find(&rows).Error
	return rows, total, err
}
