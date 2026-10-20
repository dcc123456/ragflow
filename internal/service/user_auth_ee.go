//
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
//

package service

import (
	"context"
	"fmt"

	"ragflow/internal/dao"
	"ragflow/internal/entity"
)

func resolveUserAuthTenant(ctx context.Context, user *entity.User, tenantID string) (string, error) {
	if user == nil {
		return "", fmt.Errorf("user is required")
	}
	if user.IsSuperuser != nil && *user.IsSuperuser {
		return "", nil
	}
	if tenantID == "" {
		return "", fmt.Errorf("tenant context is required")
	}
	if _, err := dao.NewUserTenantDAO().FilterByUserIDAndTenantID(ctx, dao.DB, user.ID, tenantID); err != nil {
		return "", fmt.Errorf("user is not an active member of tenant %q", tenantID)
	}
	return tenantID, nil
}
