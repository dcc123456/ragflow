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
	"time"

	"ragflow/internal/entity"

	"gorm.io/gorm"
)

// UserCountRow holds total/active/new user counts.
type UserCountRow struct {
	TotalUsers     int64 `gorm:"column:total_users"`
	ActiveUsers30d int64 `gorm:"column:active_users_30d"`
	NewUsers30d    int64 `gorm:"column:new_users_30d"`
}

// UserCountTotalActiveNew returns total, active (last N days) and new (last N days) user counts.
func (dao *UserDAO) UserCountTotalActiveNew(ctx context.Context, db *gorm.DB, days int) (UserCountRow, error) {
	var row UserCountRow
	err := db.WithContext(ctx).Raw(`
		SELECT
			COUNT(*) AS total_users,
			SUM(CASE WHEN last_login_time >= DATE_SUB(NOW(), INTERVAL ? DAY) THEN 1 ELSE 0 END) AS active_users_30d,
			SUM(CASE WHEN create_time >= DATE_SUB(NOW(), INTERVAL ? DAY) THEN 1 ELSE 0 END) AS new_users_30d
		FROM user
		WHERE status != ? AND is_anonymous = ?
	`, days, days, "0", "0").Scan(&row).Error
	return row, err
}

// UserInactiveByPlanRow holds per-plan inactive user counts.
type UserInactiveByPlanRow struct {
	PlanName      string `gorm:"column:plan_name"`
	InactiveCount int64  `gorm:"column:inactive_count"`
}

// ListInactiveByPlan returns inactive user counts grouped by plan.
func (dao *UserDAO) ListInactiveByPlan(ctx context.Context, db *gorm.DB, days int) ([]UserInactiveByPlanRow, error) {
	var rows []UserInactiveByPlanRow
	err := db.WithContext(ctx).Raw(`
		SELECT COALESCE(sub.plan_name, 'Free') AS plan_name, COUNT(DISTINCT u.id) AS inactive_count
		FROM user u
		LEFT JOIN billing_subscription sub ON u.id = sub.tenant_id AND sub.subscription_status = 'active'
		WHERE u.status != ? AND u.is_anonymous = ?
			AND (u.last_login_time IS NULL OR u.last_login_time < DATE_SUB(NOW(), INTERVAL ? DAY))
		GROUP BY COALESCE(sub.plan_name, 'Free')
		ORDER BY plan_name
	`, "0", "0", days).Scan(&rows).Error
	return rows, err
}

// UserStorageRow holds per-user storage usage.
type UserStorageRow struct {
	Email         string     `gorm:"column:email"`
	PlanName      string     `gorm:"column:plan_name"`
	DocCount      int64      `gorm:"column:doc_count"`
	TotalSize     int64      `gorm:"column:total_size"`
	StorageLimit  int64      `gorm:"column:storage_limit"`
	LastLoginTime *time.Time `gorm:"column:last_login_time"`
}

// ListStorageRows returns the top users by storage usage.
func (dao *UserDAO) ListStorageRows(ctx context.Context, db *gorm.DB, top int) ([]UserStorageRow, error) {
	var rows []UserStorageRow
	err := db.WithContext(ctx).Raw(`
		SELECT u.email AS email,
			COALESCE(sub.plan_name, 'Free') AS plan_name,
			COALESCE(ds.doc_count, 0) AS doc_count,
			COALESCE(ds.total_size, 0) AS total_size,
			COALESCE(sub.target_storage_bytes, 0) AS storage_limit,
			u.last_login_time AS last_login_time
		FROM user u
		LEFT JOIN billing_subscription sub ON u.id = sub.tenant_id AND sub.subscription_status = 'active'
		LEFT JOIN (
			SELECT kb.tenant_id, COUNT(d.id) AS doc_count, COALESCE(SUM(d.size), 0) AS total_size
			FROM knowledgebase kb
			LEFT JOIN document d ON d.kb_id = kb.id
			WHERE kb.status = ?
			GROUP BY kb.tenant_id
		) ds ON ds.tenant_id = u.id
		WHERE u.status != ? AND u.is_anonymous = ?
		ORDER BY total_size DESC
		LIMIT ?
	`, string(entity.StatusValid), "0", "0", top).Scan(&rows).Error
	return rows, err
}

// UserDocRow holds per-user document counts.
type UserDocRow struct {
	Email         string     `gorm:"column:email"`
	PlanName      string     `gorm:"column:plan_name"`
	DocCount      int64      `gorm:"column:doc_count"`
	LastLoginTime *time.Time `gorm:"column:last_login_time"`
}

// ListDocumentsRows returns the top users by document count.
func (dao *UserDAO) ListDocumentsRows(ctx context.Context, db *gorm.DB, top int) ([]UserDocRow, error) {
	var rows []UserDocRow
	err := db.WithContext(ctx).Raw(`
		SELECT u.email AS email,
			COALESCE(sub.plan_name, 'Free') AS plan_name,
			COALESCE(ds.doc_count, 0) AS doc_count,
			u.last_login_time AS last_login_time
		FROM user u
		LEFT JOIN billing_subscription sub ON u.id = sub.tenant_id AND sub.subscription_status = 'active'
		LEFT JOIN (
			SELECT kb.tenant_id, COUNT(d.id) AS doc_count
			FROM knowledgebase kb
			LEFT JOIN document d ON d.kb_id = kb.id
			WHERE kb.status = ?
			GROUP BY kb.tenant_id
		) ds ON ds.tenant_id = u.id
		WHERE u.status != ? AND u.is_anonymous = ?
		ORDER BY doc_count DESC
		LIMIT ?
	`, string(entity.StatusValid), "0", "0", top).Scan(&rows).Error
	return rows, err
}

// UserWithPlanRow holds user id, email, plan and last login.
type UserWithPlanRow struct {
	ID            string     `gorm:"column:id"`
	Email         string     `gorm:"column:email"`
	PlanName      string     `gorm:"column:plan_name"`
	LastLoginTime *time.Time `gorm:"column:last_login_time"`
}

// ListUsersWithPlan returns users (by tenant id) with their plan assignment.
func (dao *UserDAO) ListUsersWithPlan(ctx context.Context, db *gorm.DB, tenantIDs []string) ([]UserWithPlanRow, error) {
	var rows []UserWithPlanRow
	err := db.WithContext(ctx).Raw(`
		SELECT u.id AS id, u.email AS email,
			COALESCE(sub.plan_name, 'Free') AS plan_name,
			u.last_login_time AS last_login_time
		FROM user u
		LEFT JOIN billing_subscription sub ON u.id = sub.tenant_id AND sub.subscription_status = 'active'
		WHERE u.id IN ?
	`, tenantIDs).Scan(&rows).Error
	return rows, err
}

// UserQuotaRow holds per-user quota usage.
type UserQuotaRow struct {
	Email         string     `gorm:"column:email"`
	PlanName      string     `gorm:"column:plan_name"`
	StorageUsed   int64      `gorm:"column:storage_used"`
	StorageLimit  int64      `gorm:"column:storage_limit"`
	LastLoginTime *time.Time `gorm:"column:last_login_time"`
}

// ListQuotaRows returns users whose storage usage percentage meets or exceeds the quota threshold.
func (dao *UserDAO) ListQuotaRows(ctx context.Context, db *gorm.DB, top, quotaThreshold int) ([]UserQuotaRow, error) {
	var rows []UserQuotaRow
	err := db.WithContext(ctx).Raw(`
		SELECT u.email AS email,
			COALESCE(sub.plan_name, 'Free') AS plan_name,
			COALESCE(ds.total_size, 0) AS storage_used,
			COALESCE(sub.target_storage_bytes, 0) AS storage_limit,
			u.last_login_time AS last_login_time
		FROM user u
		LEFT JOIN billing_subscription sub ON u.id = sub.tenant_id AND sub.subscription_status = 'active'
		LEFT JOIN (
			SELECT kb.tenant_id, COALESCE(SUM(d.size), 0) AS total_size
			FROM knowledgebase kb
			LEFT JOIN document d ON d.kb_id = kb.id
			WHERE kb.status = ?
			GROUP BY kb.tenant_id
		) ds ON ds.tenant_id = u.id
		WHERE u.status != ? AND u.is_anonymous = ?
			AND COALESCE(sub.target_storage_bytes, 0) > 0
			AND COALESCE(ds.total_size, 0) * 100 / sub.target_storage_bytes >= ?
		ORDER BY storage_used DESC
		LIMIT ?
	`, string(entity.StatusValid), "0", "0", quotaThreshold, top).Scan(&rows).Error
	return rows, err
}

// UserPlanRow holds per-user plan assignment.
type UserPlanRow struct {
	Email         string     `gorm:"column:email"`
	PlanName      string     `gorm:"column:plan_name"`
	LastLoginTime *time.Time `gorm:"column:last_login_time"`
}

// ListPlanRows returns active users of a given plan within the last N days.
func (dao *UserDAO) ListPlanRows(ctx context.Context, db *gorm.DB, top int, planName string, days int) ([]UserPlanRow, error) {
	var rows []UserPlanRow
	err := db.WithContext(ctx).Raw(`
		SELECT u.email AS email,
			COALESCE(sub.plan_name, 'Free') AS plan_name,
			u.last_login_time AS last_login_time
		FROM user u
		LEFT JOIN billing_subscription sub ON u.id = sub.tenant_id AND sub.subscription_status = 'active'
		WHERE u.status != ? AND u.is_anonymous = ?
			AND COALESCE(sub.plan_name, 'Free') = ?
			AND u.last_login_time >= DATE_SUB(NOW(), INTERVAL ? DAY)
		ORDER BY u.last_login_time DESC
		LIMIT ?
	`, "0", "0", planName, days, top).Scan(&rows).Error
	return rows, err
}

// UserPlanQuotaRow holds per-user plan quota usage.
type UserPlanQuotaRow struct {
	Email         string     `gorm:"column:email"`
	PlanName      string     `gorm:"column:plan_name"`
	StorageUsed   int64      `gorm:"column:storage_used"`
	StorageLimit  int64      `gorm:"column:storage_limit"`
	LastLoginTime *time.Time `gorm:"column:last_login_time"`
}

// ListPlanQuotaRows returns active users of a plan whose storage usage percentage meets or exceeds the threshold.
func (dao *UserDAO) ListPlanQuotaRows(ctx context.Context, db *gorm.DB, top int, planName string, quotaThreshold, days int) ([]UserPlanQuotaRow, error) {
	var rows []UserPlanQuotaRow
	err := db.WithContext(ctx).Raw(`
		SELECT u.email AS email,
			COALESCE(sub.plan_name, 'Free') AS plan_name,
			COALESCE(ds.total_size, 0) AS storage_used,
			COALESCE(sub.target_storage_bytes, 0) AS storage_limit,
			u.last_login_time AS last_login_time
		FROM user u
		LEFT JOIN billing_subscription sub ON u.id = sub.tenant_id AND sub.subscription_status = 'active'
		LEFT JOIN (
			SELECT kb.tenant_id, COALESCE(SUM(d.size), 0) AS total_size
			FROM knowledgebase kb
			LEFT JOIN document d ON d.kb_id = kb.id
			WHERE kb.status = ?
			GROUP BY kb.tenant_id
		) ds ON ds.tenant_id = u.id
		WHERE u.status != ? AND u.is_anonymous = ?
			AND COALESCE(sub.plan_name, 'Free') = ?
			AND u.last_login_time >= DATE_SUB(NOW(), INTERVAL ? DAY)
			AND COALESCE(sub.target_storage_bytes, 0) > 0
			AND COALESCE(ds.total_size, 0) * 100 / sub.target_storage_bytes >= ?
		ORDER BY storage_used DESC
		LIMIT ?
	`, string(entity.StatusValid), "0", "0", planName, days, quotaThreshold, top).Scan(&rows).Error
	return rows, err
}

// PlanQuotaHitRow holds per-plan quota hit statistics.
type PlanQuotaHitRow struct {
	PlanName   string `gorm:"column:plan_name"`
	TotalUsers int64  `gorm:"column:total_users"`
	HitUsers   int64  `gorm:"column:hit_users"`
}

// GetPlanQuotaHitRate returns per-plan counts of users and users hitting the given quota percentage.
func (dao *UserDAO) GetPlanQuotaHitRate(ctx context.Context, db *gorm.DB, quota int) ([]PlanQuotaHitRow, error) {
	var rows []PlanQuotaHitRow
	err := db.WithContext(ctx).Raw(`
		SELECT COALESCE(sub.plan_name, 'Free') AS plan_name,
			COUNT(DISTINCT u.id) AS total_users,
			SUM(CASE WHEN COALESCE(sub.target_storage_bytes, 0) > 0
				AND COALESCE(ds.total_size, 0) * 100 / sub.target_storage_bytes >= ? THEN 1 ELSE 0 END) AS hit_users
		FROM user u
		LEFT JOIN billing_subscription sub ON u.id = sub.tenant_id AND sub.subscription_status = 'active'
		LEFT JOIN (
			SELECT kb.tenant_id, COALESCE(SUM(d.size), 0) AS total_size
			FROM knowledgebase kb
			LEFT JOIN document d ON d.kb_id = kb.id
			WHERE kb.status = ?
			GROUP BY kb.tenant_id
		) ds ON ds.tenant_id = u.id
		WHERE u.status != ? AND u.is_anonymous = ?
		GROUP BY COALESCE(sub.plan_name, 'Free')
	`, quota, string(entity.StatusValid), "0", "0").Scan(&rows).Error
	return rows, err
}
