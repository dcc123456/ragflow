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

package dao

import (
	"context"

	"ragflow/internal/entity"

	"gorm.io/gorm"
)

type BillingSubscriptionDAO struct{}

func NewBillingSubscriptionDAO() *BillingSubscriptionDAO {
	return &BillingSubscriptionDAO{}
}

// CountActiveSubscriptions counts all active billing subscriptions.
func (dao *BillingSubscriptionDAO) CountActiveSubscriptions(ctx context.Context, db *gorm.DB) (int64, error) {
	var total int64
	err := db.WithContext(ctx).Model(&entity.BillingSubscription{}).
		Where("subscription_status = ?", "active").
		Count(&total).Error
	return total, err
}

type QuotaUsageRow struct {
	PlanName       string  `gorm:"column:plan_name"`
	UserCount      int64   `gorm:"column:user_count"`
	AvgStorageUsed float64 `gorm:"column:avg_storage_used"`
	AvgAppsUsed    float64 `gorm:"column:avg_apps_used"`
	TotalAPITokens int64   `gorm:"column:total_api_tokens"`
}

func (dao *BillingSubscriptionDAO) ListQuotaUsage(ctx context.Context, db *gorm.DB) ([]QuotaUsageRow, error) {
	var rows []QuotaUsageRow
	err := db.WithContext(ctx).Raw(`
		WITH active_users AS (
			SELECT DISTINCT u.id, sub.plan_name
			FROM user u
			INNER JOIN billing_subscription sub
				ON u.id = sub.tenant_id AND sub.subscription_status = 'active'
			WHERE u.status != '0' AND u.is_anonymous = '0'
		),
		user_counts AS (
			SELECT plan_name, COUNT(*) AS user_count
			FROM active_users GROUP BY plan_name
		),
		storage_stats AS (
			SELECT au.plan_name, AVG(COALESCE(ds.total_size, 0)) AS avg_storage_used
			FROM active_users au
			LEFT JOIN (
				SELECT kb.tenant_id, SUM(d.size) AS total_size
				FROM knowledgebase kb
				INNER JOIN document d ON d.kb_id = kb.id
				WHERE kb.status = '1'
				GROUP BY kb.tenant_id
			) ds ON au.id = ds.tenant_id
			GROUP BY au.plan_name
		),
		app_stats AS (
			SELECT au.plan_name, AVG(COALESCE(uc.app_count, 0)) AS avg_apps_used
			FROM active_users au
			LEFT JOIN (
				SELECT user_id, COUNT(*) AS app_count
				FROM user_canvas
				WHERE canvas_category = 'agent_canvas'
				GROUP BY user_id
			) uc ON au.id = uc.user_id
			GROUP BY au.plan_name
		),
		token_stats AS (
			SELECT au.plan_name, SUM(COALESCE(at.token_count, 0)) AS total_api_tokens
			FROM active_users au
			LEFT JOIN (
				SELECT tenant_id, COUNT(*) AS token_count
				FROM api_token
				GROUP BY tenant_id
			) at ON au.id = at.tenant_id
			GROUP BY au.plan_name
		)
		SELECT
			uc.plan_name,
			uc.user_count,
			s.avg_storage_used,
			a.avg_apps_used,
			t.total_api_tokens
		FROM user_counts uc
		LEFT JOIN storage_stats s ON uc.plan_name = s.plan_name
		LEFT JOIN app_stats a ON uc.plan_name = a.plan_name
		LEFT JOIN token_stats t ON uc.plan_name = t.plan_name
		ORDER BY uc.plan_name
	`).Scan(&rows).Error
	return rows, err
}
