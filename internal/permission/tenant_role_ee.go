// Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package permission

import (
	"context"
	"strconv"

	"gorm.io/gorm"
)

type tenantRoleRecord struct {
	ID       int64  `gorm:"column:id"`
	TenantID string `gorm:"column:tenant_id"`
}

func (tenantRoleRecord) TableName() string { return "role" }

func tenantRoleMembershipEnabled(db *gorm.DB) bool {
	if db == nil {
		return false
	}
	migrator := db.Migrator()
	return migrator.HasTable(&tenantRoleRecord{}) && migrator.HasColumn(&tenantRoleRecord{}, "TenantID")
}

func isTenantRoleID(ctx context.Context, db *gorm.DB, tenantID, roleValue string) (bool, error) {
	roleID, err := strconv.ParseInt(roleValue, 10, 64)
	if err != nil || roleID <= 0 || !tenantRoleMembershipEnabled(db) {
		return false, nil
	}
	var count int64
	if err := db.WithContext(ctx).Model(&tenantRoleRecord{}).
		Where("id = ? AND tenant_id = ?", roleID, tenantID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
