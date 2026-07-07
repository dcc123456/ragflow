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
	"ragflow/internal/entity"
	"time"
)

type EnterpriseUserDAO struct{}

func NewEnterpriseUserDAO() *EnterpriseUserDAO {
	return &EnterpriseUserDAO{}
}

type InactiveByPlanRow struct {
	PlanName      string `gorm:"column:plan_name"`
	InactiveCount int64  `gorm:"column:inactive_count"`
}

func (dao *EnterpriseUserDAO) ListInactiveByPlan(days int) ([]InactiveByPlanRow, error) {
	cutoff := time.Now().AddDate(0, 0, -days)
	var rows []InactiveByPlanRow
	err := DB.Raw(`
		SELECT
			COALESCE(sub.plan_name, 'Trial') AS plan_name,
			COUNT(DISTINCT u.id) AS inactive_count
		FROM user u
		LEFT JOIN (
			SELECT bs.tenant_id, bs.plan_name
			FROM billing_subscription bs
			WHERE bs.subscription_status = 'active'
			GROUP BY bs.tenant_id, bs.plan_name
		) sub ON u.id = sub.tenant_id
		WHERE u.status != '0'
			AND u.is_anonymous = '0'
			AND (u.last_login_time < ? OR u.last_login_time IS NULL)
		GROUP BY sub.plan_name
		ORDER BY inactive_count DESC
	`, cutoff).Scan(&rows).Error
	return rows, err
}

type UserStorageRow struct {
	UserID    string `gorm:"column:user_id"`
	Email     string `gorm:"column:email"`
	Nickname  string `gorm:"column:nickname"`
	DocCount  int64  `gorm:"column:doc_count"`
	TotalSize int64  `gorm:"column:total_size"`
}

func (dao *EnterpriseUserDAO) ListStorageRows(top int) ([]UserStorageRow, error) {
	var rows []UserStorageRow
	err := DB.Raw(`
		SELECT
			u.id AS user_id,
			u.email,
			u.nickname,
			COALESCE(SUM(ds.doc_count), 0) AS doc_count,
			COALESCE(SUM(ds.total_size), 0) AS total_size
		FROM user u
		INNER JOIN user_tenant ut ON u.id = ut.user_id AND ut.status = '1'
		INNER JOIN (
			SELECT
				kb.tenant_id,
				COUNT(d.id) AS doc_count,
				COALESCE(SUM(d.size), 0) AS total_size
			FROM knowledgebase kb
			INNER JOIN document d ON d.kb_id = kb.id
			WHERE kb.status = '1'
			GROUP BY kb.tenant_id
		) ds ON ut.tenant_id = ds.tenant_id
		WHERE u.status != '0'
		GROUP BY u.id, u.email, u.nickname
		HAVING SUM(ds.total_size) > 0
		ORDER BY total_size DESC
		LIMIT ?
	`, top).Scan(&rows).Error
	return rows, err
}

type UserDocRow struct {
	UserID   string `gorm:"column:user_id"`
	Email    string `gorm:"column:email"`
	Nickname string `gorm:"column:nickname"`
	DocCount int64  `gorm:"column:doc_count"`
}

func (dao *EnterpriseUserDAO) ListDocumentsRows(top int) ([]UserDocRow, error) {
	var rows []UserDocRow
	err := DB.Raw(`
		SELECT
			u.id AS user_id,
			u.email,
			u.nickname,
			COALESCE(SUM(ds.doc_count), 0) AS doc_count
		FROM user u
		INNER JOIN user_tenant ut ON u.id = ut.user_id AND ut.status = '1'
		INNER JOIN (
			SELECT
				kb.tenant_id,
				COUNT(d.id) AS doc_count
			FROM knowledgebase kb
			INNER JOIN document d ON d.kb_id = kb.id
			WHERE kb.status = '1'
			GROUP BY kb.tenant_id
		) ds ON ut.tenant_id = ds.tenant_id
		WHERE u.status != '0'
		GROUP BY u.id, u.email, u.nickname
		HAVING SUM(ds.doc_count) > 0
		ORDER BY doc_count DESC
		LIMIT ?
	`, top).Scan(&rows).Error
	return rows, err
}

type UserQuotaRow struct {
	UserID       string `gorm:"column:user_id"`
	Email        string `gorm:"column:email"`
	Nickname     string `gorm:"column:nickname"`
	PlanName     string `gorm:"column:plan_name"`
	StorageUsed  int64  `gorm:"column:storage_used"`
	StorageLimit int64  `gorm:"column:storage_limit"`
}

func (dao *EnterpriseUserDAO) GetFreeQuotaStorage() int64 {
	var freeQuotaStorage int64
	if err := DB.Model(&entity.BillingProduct{}).
		Select("quota_storage").
		Where("name = ?", "Free").
		Order("version DESC").
		Limit(1).
		Scan(&freeQuotaStorage).Error; err != nil {
		freeQuotaStorage = 0
	}
	return freeQuotaStorage
}

func (dao *EnterpriseUserDAO) ListQuotaRows(top, quotaThreshold int) ([]UserQuotaRow, error) {
	freeQuotaStorage := dao.GetFreeQuotaStorage()

	var rows []UserQuotaRow
	err := DB.Raw(`
		SELECT
			u.id AS user_id,
			u.email,
			u.nickname,
			COALESCE(sub.plan_name, 'Free') AS plan_name,
			CAST(COALESCE(ds.total_size, 0) AS SIGNED) AS storage_used,
			CAST(COALESCE(sub.storage_limit, ?) AS SIGNED) AS storage_limit
		FROM user u
		INNER JOIN (
			SELECT kb.tenant_id, COALESCE(SUM(d.size), 0) AS total_size
			FROM knowledgebase kb
			INNER JOIN document d ON d.kb_id = kb.id
			WHERE kb.status = '1'
			GROUP BY kb.tenant_id
		) ds ON u.id = ds.tenant_id
		LEFT JOIN (
			SELECT bs.tenant_id,
				bs.plan_name,
				MAX(bp.quota_storage + COALESCE(bs.addon_storage_bytes, 0)) AS storage_limit
			FROM billing_subscription bs
			INNER JOIN billing_product bp ON bs.plan_name = bp.name
			WHERE bs.subscription_status = 'active'
			GROUP BY bs.tenant_id, bs.plan_name
		) sub ON u.id = sub.tenant_id
		WHERE u.status != '0'
			AND CAST(COALESCE(ds.total_size, 0) AS SIGNED) > 0
			AND (CAST(COALESCE(sub.storage_limit, ?) AS SIGNED) = 0
				OR CAST(COALESCE(ds.total_size, 0) AS SIGNED) * 100 / NULLIF(CAST(COALESCE(sub.storage_limit, ?) AS SIGNED), 0) >= ?)
		ORDER BY storage_used DESC
		LIMIT ?
	`, freeQuotaStorage, freeQuotaStorage, freeQuotaStorage, quotaThreshold, top).Scan(&rows).Error
	return rows, err
}

type UserPlanRow struct {
	Email         string     `gorm:"column:email"`
	PlanName      string     `gorm:"column:plan_name"`
	LastLoginTime *time.Time `gorm:"column:last_login_time"`
}

func (dao *EnterpriseUserDAO) ListPlanRows(top int, planName string, days int) ([]UserPlanRow, error) {
	cutoff := time.Now().AddDate(0, 0, -days)
	var rows []UserPlanRow
	err := DB.Raw(`
		SELECT
			u.email,
			COALESCE(sub.plan_name, 'Free') AS plan_name,
			u.last_login_time
		FROM user u
		LEFT JOIN (
			SELECT bs.tenant_id, bs.plan_name
			FROM billing_subscription bs
			WHERE bs.subscription_status = 'active'
			GROUP BY bs.tenant_id, bs.plan_name
		) sub ON u.id = sub.tenant_id
		WHERE u.status != '0'
			AND u.is_anonymous = '0'
			AND u.last_login_time >= ?
			AND COALESCE(sub.plan_name, 'Free') = ?
		ORDER BY u.last_login_time DESC
		LIMIT ?
	`, cutoff, planName, top).Scan(&rows).Error
	return rows, err
}

type UserPlanQuotaRow struct {
	Email         string     `gorm:"column:email"`
	PlanName      string     `gorm:"column:plan_name"`
	StorageUsed   int64      `gorm:"column:storage_used"`
	StorageLimit  int64      `gorm:"column:storage_limit"`
	LastLoginTime *time.Time `gorm:"column:last_login_time"`
}

func (dao *EnterpriseUserDAO) ListPlanQuotaRows(top int, planName string, quotaThreshold, days int) ([]UserPlanQuotaRow, error) {
	freeQuotaStorage := dao.GetFreeQuotaStorage()
	cutoff := time.Now().AddDate(0, 0, -days)

	var rows []UserPlanQuotaRow
	err := DB.Raw(`
		SELECT
			u.email,
			COALESCE(sub.plan_name, 'Free') AS plan_name,
			CAST(COALESCE(ds.total_size, 0) AS SIGNED) AS storage_used,
			CAST(COALESCE(sub.storage_limit, ?) AS SIGNED) AS storage_limit,
			u.last_login_time
		FROM user u
		INNER JOIN (
			SELECT kb.tenant_id, COALESCE(SUM(d.size), 0) AS total_size
			FROM knowledgebase kb
			INNER JOIN document d ON d.kb_id = kb.id
			WHERE kb.status = '1'
			GROUP BY kb.tenant_id
		) ds ON u.id = ds.tenant_id
		LEFT JOIN (
			SELECT bs.tenant_id,
				bs.plan_name,
				MAX(bp.quota_storage + COALESCE(bs.addon_storage_bytes, 0)) AS storage_limit
			FROM billing_subscription bs
			INNER JOIN billing_product bp ON bs.plan_name = bp.name
			WHERE bs.subscription_status = 'active'
			GROUP BY bs.tenant_id, bs.plan_name
		) sub ON u.id = sub.tenant_id
		WHERE u.status != '0'
			AND u.is_anonymous = '0'
			AND u.last_login_time >= ?
			AND COALESCE(sub.plan_name, 'Free') = ?
			AND CAST(COALESCE(ds.total_size, 0) AS SIGNED) > 0
			AND (CAST(COALESCE(sub.storage_limit, ?) AS SIGNED) = 0
				OR CAST(COALESCE(ds.total_size, 0) AS SIGNED) * 100 / NULLIF(CAST(COALESCE(sub.storage_limit, ?) AS SIGNED), 0) >= ?)
		ORDER BY storage_used DESC
		LIMIT ?
	`, freeQuotaStorage, cutoff, planName, freeQuotaStorage, freeQuotaStorage, quotaThreshold, top).Scan(&rows).Error
	return rows, err
}

type ProductRow struct {
	Name                     string `gorm:"column:name"`
	QuotaStorage             int64  `gorm:"column:quota_storage"`
	QuotaApps                int    `gorm:"column:quota_apps"`
	APIRequestLimitPerMinute *int64 `gorm:"column:api_request_limit_per_minute"`
}

func (dao *EnterpriseUserDAO) ListBillingProducts() ([]ProductRow, error) {
	var products []ProductRow
	err := DB.Table("billing_product").
		Select("name, quota_storage, quota_apps, api_request_limit_per_minute").
		Order("priority ASC, version DESC").
		Scan(&products).Error
	return products, err
}

type QuotaUsageRow struct {
	PlanName       string  `gorm:"column:plan_name"`
	UserCount      int64   `gorm:"column:user_count"`
	AvgStorageUsed float64 `gorm:"column:avg_storage_used"`
	AvgAppsUsed    float64 `gorm:"column:avg_apps_used"`
	TotalAPITokens int64   `gorm:"column:total_api_tokens"`
}

func (dao *EnterpriseUserDAO) ListQuotaUsage() ([]QuotaUsageRow, error) {
	var rows []QuotaUsageRow
	err := DB.Raw(`
		SELECT
			COALESCE(sub.plan_name, 'Trial') AS plan_name,
			COUNT(DISTINCT u.id) AS user_count,
			AVG(COALESCE(ds.total_size, 0)) AS avg_storage_used,
			AVG(COALESCE(uc.app_count, 0)) AS avg_apps_used,
			SUM(COALESCE(at.token_count, 0)) AS total_api_tokens
		FROM user u
		LEFT JOIN (
			SELECT tenant_id, plan_name FROM billing_subscription
			WHERE subscription_status = 'active'
			GROUP BY tenant_id, plan_name
		) sub ON u.id = sub.tenant_id
		LEFT JOIN (
			SELECT kb.tenant_id, SUM(d.size) AS total_size
			FROM knowledgebase kb
			INNER JOIN document d ON d.kb_id = kb.id
			WHERE kb.status = '1'
			GROUP BY kb.tenant_id
		) ds ON u.id = ds.tenant_id
		LEFT JOIN (
			SELECT user_id, COUNT(*) AS app_count
			FROM user_canvas
			WHERE canvas_category = 'agent_canvas'
			GROUP BY user_id
		) uc ON u.id = uc.user_id
		LEFT JOIN (
			SELECT tenant_id, COUNT(*) AS token_count
			FROM api_token
			GROUP BY tenant_id
		) at ON u.id = at.tenant_id
		WHERE u.status != '0' AND u.is_anonymous = '0'
		GROUP BY COALESCE(sub.plan_name, 'Trial')
		ORDER BY plan_name
	`).Scan(&rows).Error
	return rows, err
}
