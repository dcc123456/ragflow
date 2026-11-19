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
	"errors"
	"fmt"
	"strings"
	"sync"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/dao/transaction"
	"ragflow/internal/entity"
	modelModule "ragflow/internal/entity/models"
	"ragflow/internal/permission"

	"gorm.io/gorm"
)

func init() {
	usageTemplateAttacher = attachUsageTemplateEE
}

// groupNamesByID caches the display name of every model group a gp- reference
// has resolved through, so the usage template can carry the group name without
// a second lookup.
var groupNamesByID sync.Map

// pickGroupModel resolves a gp-<group_id> reference to the first active model
// in position order that matches the requested type and the caller can use.
func (s *ModelFactory) pickGroupModel(ctx context.Context, tenantID, groupID string, requestedType entity.ModelType) (*entity.TenantModel, error) {
	group, err := dao.NewTenantModelGroupDAO().GetByID(ctx, dao.DB, groupID)
	if err != nil {
		return nil, fmt.Errorf("%w: model group %s lookup failed: %w", errModelConfigUnavailable, groupID, err)
	}
	if group == nil || group.TenantID != tenantID || !strings.EqualFold(group.Status, "active") {
		return nil, fmt.Errorf("%w: model group %s not found", errModelConfigUnavailable, groupID)
	}
	if strings.TrimSpace(permission.UserIDFromContext(ctx)) != "" {
		if err := authorizeModelGroup(ctx, group, false); err != nil {
			return nil, fmt.Errorf("%w: model group %s is not available: %w", errModelConfigUnavailable, groupID, err)
		}
	}
	members, err := dao.NewTenantModelGroupMapDAO().ListByGroupID(ctx, dao.DB, group.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: listing models in group %q failed: %w", errModelConfigUnavailable, group.GroupName, err)
	}
	userID := strings.TrimSpace(permission.UserIDFromContext(ctx))
	owner, err := s.tenantOwnerMembership(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	checker := permission.NewDatabaseChecker(dao.DB)
	var lastPermissionErr error
	for _, member := range members {
		if member.Status != entity.ModelGroupMapStatusActive {
			continue
		}
		model, err := s.modelDAO.GetByID(ctx, dao.DB, member.ModelID)
		if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && model == nil {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%w: model %s lookup failed: %w", errModelConfigUnavailable, member.ModelID, err)
		}
		if !strings.EqualFold(model.Status, "active") || requestedType != 0 && !entity.ModelType(model.ModelType).Has(requestedType) {
			continue
		}
		provider, err := s.modelProviderDAO.GetByID(ctx, dao.DB, model.ProviderID)
		if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && provider == nil {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%w: provider %s lookup failed: %w", errModelConfigUnavailable, model.ProviderID, err)
		}
		instance, err := s.modelInstanceDAO.GetByID(ctx, dao.DB, model.InstanceID)
		if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && instance == nil {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%w: instance %s lookup failed: %w", errModelConfigUnavailable, model.InstanceID, err)
		}
		if provider.TenantID != tenantID || instance.ProviderID != provider.ID || !strings.EqualFold(instance.Status, "active") {
			continue
		}
		if userID != "" {
			if provider.UserID != owner.UserID {
				err = authorizeNonOwnerModelConfiguration(ctx, provider, instance, []*entity.TenantModel{model})
			} else {
				err = checker.CheckResource(ctx, permission.ResourceRef{Kind: permission.ResourceKindModelProvider, ID: provider.ID}, permission.OperationRead)
				if err == nil {
					err = checker.CheckResource(ctx, permission.ResourceRef{Kind: permission.ResourceKindModelInstance, ID: instance.ID}, permission.OperationUse)
				}
				if err == nil {
					err = checker.CheckResource(ctx, permission.ResourceRef{Kind: permission.ResourceKindModel, ID: model.ID}, permission.OperationUse)
				}
			}
			if err != nil {
				lastPermissionErr = err
				continue
			}
		}
		groupNamesByID.Store(groupID, group.GroupName)
		return model, nil
	}
	if lastPermissionErr != nil {
		return nil, fmt.Errorf("%w: no model in group %q is authorized for this user: %w", errModelConfigUnavailable, group.GroupName, lastPermissionErr)
	}
	return nil, fmt.Errorf("%w: model group %q has no available model", errModelConfigUnavailable, group.GroupName)
}

// attachUsageTemplateEE pre-fills the construction-time usage identity
// (tenant, user, provider, instance, model, gp- group) on the model info, so
// every call of the resolved model produces an attributable usage row without
// each call site having to build one.
func attachUsageTemplateEE(ctx context.Context, apiConfig *modelModule.APIConfig, info *modelModule.ModelInfo) {
	if info == nil {
		return
	}
	usage := &common.ModelUsage{}
	populateModelUsage(ctx, usage, info, apiConfig, permission.TenantIDFromContext(ctx))
	if info.GroupID != "" {
		usage.GroupID = info.GroupID
		usage.GroupName = groupNameForUsage(ctx, info.GroupID)
		info.GroupName = usage.GroupName
	}
	info.UsageTemplate = usage
}

// groupNameForUsage resolves the display name of a model group, preferring the
// name cached by pickGroupModel during resolution and falling back to a direct
// lookup.
func groupNameForUsage(ctx context.Context, groupID string) string {
	if name, ok := groupNamesByID.Load(groupID); ok {
		if name, ok := name.(string); ok {
			return name
		}
	}
	group, err := dao.NewTenantModelGroupDAO().GetByID(ctx, dao.DB, groupID)
	if err != nil || group == nil {
		return ""
	}
	return group.GroupName
}

func authorizeNonOwnerModelConfiguration(ctx context.Context, provider *entity.TenantModelProvider, instance *entity.TenantModelInstance, models []*entity.TenantModel) error {
	checker := permission.NewDatabaseChecker(dao.DB)
	if err := checker.CheckResource(ctx, permission.ResourceRef{Kind: permission.ResourceKindModelProvider, ID: provider.ID}, permission.OperationRead); err != nil {
		return err
	}
	if err := checker.CheckResource(ctx, permission.ResourceRef{Kind: permission.ResourceKindModelInstance, ID: instance.ID}, permission.OperationUse); err != nil {
		return err
	}
	for _, model := range models {
		if !strings.EqualFold(model.Status, "active") {
			continue
		}
		if err := checker.CheckResource(ctx, permission.ResourceRef{Kind: permission.ResourceKindModel, ID: model.ID}, permission.OperationUse); err != nil {
			return err
		}
	}
	return nil
}

func authorizeModelMetadataReadEE(ctx context.Context, identity *modelIdentity) error {
	if identity == nil || identity.providerEntity == nil || identity.instanceEntity == nil {
		return fmt.Errorf("%w: incomplete model identity", errModelConfigUnavailable)
	}
	// Identity-less internal callers are already restricted by model identity
	// resolution to the tenant owner's provider configuration.
	if strings.TrimSpace(permission.UserIDFromContext(ctx)) == "" {
		return nil
	}
	checker := permission.NewDatabaseChecker(dao.DB)
	for _, ref := range []permission.ResourceRef{
		{Kind: permission.ResourceKindModelProvider, ID: identity.providerEntity.ID},
		{Kind: permission.ResourceKindModelInstance, ID: identity.instanceEntity.ID},
	} {
		if err := checker.CheckResource(ctx, ref, permission.OperationRead); err != nil {
			return fmt.Errorf("%w: cannot read model metadata: %w", errModelConfigUnavailable, err)
		}
	}
	return nil
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
