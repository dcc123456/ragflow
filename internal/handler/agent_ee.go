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

package handler

import (
	"ragflow/internal/common"
	"ragflow/internal/entity"

	"github.com/gin-gonic/gin"
)

// billingQuotaUser resolves the caller that the plan quota applies to.
func billingQuotaUser(c *gin.Context) (*entity.User, bool) {
	user, code, message := GetUser(c)
	if code != common.CodeSuccess {
		common.ErrorWithCode(c, code, message)
		return nil, false
	}
	return user, true
}

// billingEnforceApps admits every new application: the community edition has
// no plan quota.
func billingEnforceApps(_ *gin.Context, _ string) bool { return true }

// billingEnforceStorage admits every upload: the community edition has no
// storage quota.
func billingEnforceStorage(_ *gin.Context, _ string, _ int64) bool { return true }
