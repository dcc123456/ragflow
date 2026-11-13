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

package service

import (
	"context"
	"fmt"
	"strings"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/dao/transaction"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
)

// pickGroupModel resolves a gp-<group_id> model reference: the group must
// belong to the requesting tenant, and the returned model is the
// highest-positioned active member of the requested type. The downstream
// checks in resolveModelIdentity (provider/instance ownership, status,
// resource permissions) still apply to the picked model.
func (s *ModelFactory) pickGroupModel(ctx context.Context, tenantID, groupID string, requestedType entity.ModelType) (*entity.TenantModel, error) {
	group, err := dao.NewTenantModelGroupDAO().GetByID(ctx, dao.DB, groupID)
	if err != nil {
		return nil, fmt.Errorf("%w: model group %s lookup failed: %w", errModelConfigUnavailable, groupID, err)
	}
	if group == nil || group.TenantID != tenantID {
		return nil, fmt.Errorf("%w: model group %s not found", errModelConfigUnavailable, groupID)
	}
	model, err := transaction.PickModelFromGroup(ctx, dao.DB, group.ID, requestedType)
	if err != nil {
		return nil, fmt.Errorf("%w: picking model from group %q failed: %w", errModelConfigUnavailable, group.GroupName, err)
	}
	return model, nil
}

// MarkGroupMemberInactive deactivates the group's currently active,
// highest-positioned member of the requested type, so that the next resolve
// of the same gp- reference picks the next member. Call it after an
// invocation failure against a model resolved from a group reference and
// before re-resolving.
//
// It reports whether modelRef is a group reference; false means the reference
// points at a single model and no state changed. Marking relies on re-picking
// the failed member from current table state, so a concurrent flip between
// resolve and this call may mark a different member.
func (s *ModelFactory) MarkGroupMemberInactive(ctx context.Context, modelType entity.ModelType, modelRef string) (bool, error) {
	groupID, ok := common.ParseGroupModelRef(modelRef)
	if !ok {
		return false, nil
	}
	tenantID := strings.TrimSpace(permission.TenantIDFromContext(ctx))
	if tenantID == "" {
		return true, fmt.Errorf("%w: tenant id is required", errModelConfigUnavailable)
	}
	group, err := dao.NewTenantModelGroupDAO().GetByID(ctx, dao.DB, groupID)
	if err != nil {
		return true, fmt.Errorf("%w: model group %s lookup failed: %w", errModelConfigUnavailable, groupID, err)
	}
	if group == nil || group.TenantID != tenantID {
		return true, fmt.Errorf("%w: model group %s not found", errModelConfigUnavailable, groupID)
	}
	model, err := transaction.PickModelFromGroup(ctx, dao.DB, group.ID, modelType)
	if err != nil {
		return true, fmt.Errorf("%w: picking model from group %q failed: %w", errModelConfigUnavailable, group.GroupName, err)
	}
	if model == nil {
		// Nothing active of the requested type remains; nothing to flip.
		return true, nil
	}
	if _, err := dao.NewTenantModelGroupMapDAO().SetModelStatus(ctx, dao.DB, group.ID, model.ID, entity.ModelGroupMapStatusInactive); err != nil {
		return true, fmt.Errorf("%w: marking model %s inactive in group %q failed: %w", errModelConfigUnavailable, model.ID, group.GroupName, err)
	}
	return true, nil
}
