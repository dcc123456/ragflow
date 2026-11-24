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

package service

import (
	"context"
	"errors"
	"fmt"
	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
	permissionresponse "ragflow/internal/permission/response"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

func (m *ModelProviderService) listModelProvidersForTenantEE(ctx context.Context, query *gorm.DB, tenantID string) ([]*entity.TenantModelProvider, error) {
	var providers []*entity.TenantModelProvider
	err := query.WithContext(ctx).
		Where("tenant_model_provider.tenant_id = ?", tenantID).
		Find(&providers).Error
	return providers, err
}

func (*ModelProviderService) setModelOwnerInfoEE(ctx context.Context, models []map[string]interface{}, providers map[string]*entity.TenantModelProvider) error {
	ownerIDs := make([]string, 0, len(models))
	seenOwnerIDs := make(map[string]struct{}, len(models))
	for _, model := range models {
		providerID, _ := model["provider_id"].(string)
		provider := providers[providerID]
		if provider == nil || provider.UserID == "" {
			continue
		}
		model["user_id"] = provider.UserID
		if _, exists := seenOwnerIDs[provider.UserID]; exists {
			continue
		}
		seenOwnerIDs[provider.UserID] = struct{}{}
		ownerIDs = append(ownerIDs, provider.UserID)
	}

	nicknames, err := dao.NewUserDAO().GetNicknamesByIDs(ctx, dao.DB, ownerIDs)
	if err != nil {
		return fmt.Errorf("resolve model owner nicknames: %w", err)
	}
	for _, model := range models {
		providerID, _ := model["provider_id"].(string)
		provider := providers[providerID]
		if provider == nil || provider.UserID == "" {
			continue
		}
		if nickname, ok := nicknames[provider.UserID]; ok {
			model["nickname"] = nickname
		}
	}
	return nil
}

func queryScopeForAccessUser(ctx context.Context, checker *permission.Checker, query permission.ScopeQuery, accessUserID string) (permission.QueryScope, error) {
	accessUserID = strings.TrimSpace(accessUserID)
	currentUserID := strings.TrimSpace(permission.UserIDFromContext(ctx))
	if accessUserID == "" || accessUserID == currentUserID {
		return checker.QueryScope(ctx, query)
	}

	if query.Kind == permission.ResourceKindModelProvider || query.Kind == permission.ResourceKindModelInstance || query.Kind == permission.ResourceKindModel {
		query.OwnerUserID = accessUserID
		return checker.QueryScope(ctx, query)
	}

	currentScope, err := checker.QueryScope(ctx, query)
	if err != nil {
		return permission.QueryScope{}, err
	}
	accessContext := permission.WithIdentity(ctx, accessUserID, permission.TenantIDFromContext(ctx))
	accessScope, err := checker.QueryScope(accessContext, query)
	if errors.Is(err, permission.ErrPermissionDenied) || errors.Is(err, permission.ErrMembershipNotFound) || errors.Is(err, permission.ErrResourceNotFound) {
		return permission.QueryScope{}, nil
	}
	if err != nil {
		return permission.QueryScope{}, err
	}
	return currentScope.Intersect(accessScope), nil
}

func (m *ModelProviderService) listReadableModelProvidersForUserEE(ctx context.Context, tenantID, _ string) ([]*entity.TenantModelProvider, error) {
	scope, err := permission.NewDatabaseChecker(dao.DB).QueryScope(ctx, permission.ScopeQuery{
		TenantID:  tenantID,
		Kind:      permission.ResourceKindModelProvider,
		Operation: permission.OperationRead,
	})
	if err != nil {
		return nil, err
	}
	var providers []*entity.TenantModelProvider
	err = scope.Apply(dao.DB.Model(&entity.TenantModelProvider{})).
		Where("tenant_model_provider.tenant_id = ?", tenantID).
		Find(&providers).Error
	return providers, err
}

func (m *ModelProviderService) providersForInstanceList(ctx context.Context, providerIDOrName string) ([]*entity.TenantModelProvider, error) {
	tenantID, err := requireCurrentTenantID(ctx)
	if err != nil {
		return nil, err
	}
	providers, err := m.listReadableModelProvidersForUserEE(ctx, tenantID, permission.UserIDFromContext(ctx))
	if err != nil {
		return nil, err
	}

	// Prefer an exact provider ID match. Provider names are shared across
	// owners within a tenant, so name lookups must include every readable row.
	for _, provider := range providers {
		if provider.ID == providerIDOrName {
			return []*entity.TenantModelProvider{provider}, nil
		}
	}
	matched := make([]*entity.TenantModelProvider, 0, len(providers))
	for _, provider := range providers {
		if strings.EqualFold(provider.ProviderName, providerIDOrName) {
			matched = append(matched, provider)
		}
	}
	if len(matched) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return matched, nil
}

func (m *ModelProviderService) CreateModelGroup(ctx context.Context, userID, groupName string) error {
	if err := permission.NewDatabaseChecker(dao.DB).CheckFeature(ctx, "model_group", permission.ActionEnable); err != nil {
		return err
	}
	tenantID, err := requireCurrentTenantID(ctx)
	if err != nil {
		return err
	}
	ctx = permission.WithIdentity(ctx, userID, tenantID)
	tenantModelGroup := &entity.TenantModelGroup{
		ID:        common.GenerateUUID(),
		UserID:    userID,
		TenantID:  tenantID,
		GroupName: groupName,
		Status:    "active",
		Extra:     "{}",
	}
	return dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		membership, err := findTenantMembership(tx, tenantID, userID, true)
		if err != nil {
			return err
		}
		if err := m.modelGroupDAO.Create(ctx, tx, tenantModelGroup); err != nil {
			return err
		}
		resourceID := tenantModelGroup.ID
		memberID := membership.ID
		status := string(entity.StatusValid)
		ownerGrant := entity.Permission{
			ID: common.GenerateUUID(), TenantID: tenantID, ResourceType: "model_group",
			ResourceID: &resourceID, MemberID: &memberID,
			Permission: resourcePermissionMask, Status: &status,
		}
		if err := tx.Create(&ownerGrant).Error; err != nil {
			return fmt.Errorf("create model group owner permission: %w", err)
		}
		return nil
	})
}

func (m *ModelProviderService) DeleteModelGroup(ctx context.Context, groupID string) error {
	tenantID, err := requireCurrentTenantID(ctx)
	if err != nil {
		return err
	}
	group, err := m.modelGroupDAO.GetByIDAndTenantID(ctx, dao.DB, tenantID, groupID)
	if err != nil {
		return err
	}
	if group == nil {
		return fmt.Errorf("model group not found")
	}
	if err := authorizeModelGroup(ctx, group, true); err != nil {
		return err
	}
	count, err := m.modelGroupMapDAO.DeleteByGroupID(ctx, dao.DB, group.ID)
	if err != nil {
		return err
	}
	common.Info("delete model group map success", zap.String("group_id", group.ID), zap.Int64("count", count))
	count, err = m.modelGroupDAO.DeleteByIDs(ctx, dao.DB, []string{group.ID})
	if err != nil {
		return err
	}
	common.Info("delete model group success", zap.String("group_id", group.ID), zap.Int64("count", count))
	return nil
}

type ShowModelGroupResponse struct {
	ID          string   `json:"id"`
	ModelID     string   `json:"model_id"`
	Position    int64    `json:"position"`
	Provider    string   `json:"provider"`
	Instance    string   `json:"instance"`
	Model       string   `json:"model"`
	ModelType   []string `json:"model_type"`
	Status      string   `json:"status"`
	Unavailable bool     `json:"unavailable"`
	Restricted  bool     `json:"restricted"`
	GroupName   string   `json:"group_name"`
	GroupID     string   `json:"group_id"`
}

func (m *ModelProviderService) ShowModelGroup(ctx context.Context, groupID string) ([]*ShowModelGroupResponse, error) {
	tenantID, err := requireCurrentTenantID(ctx)
	if err != nil {
		return nil, err
	}
	group, err := m.modelGroupDAO.GetByIDAndTenantID(ctx, dao.DB, tenantID, groupID)
	if err != nil {
		return nil, err
	}
	if group == nil {
		return nil, fmt.Errorf("model group not found")
	}
	if err := authorizeModelGroup(ctx, group, false); err != nil {
		return nil, err
	}

	modelGroupMaps, err := m.modelGroupMapDAO.ListByGroupID(ctx, dao.DB, group.ID)
	if err != nil {
		return nil, err
	}
	modelIDs := make([]string, 0, len(modelGroupMaps))
	for _, modelGroupMap := range modelGroupMaps {
		modelIDs = append(modelIDs, modelGroupMap.ModelID)
	}
	modelReadScope, err := permission.NewDatabaseChecker(dao.DB).QueryScope(ctx, permission.ScopeQuery{
		TenantID: tenantID, Kind: permission.ResourceKindModel, Operation: permission.OperationRead,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve model group member permissions: %w", err)
	}
	var readableModelIDs []string
	if len(modelIDs) > 0 {
		if err := modelReadScope.Apply(dao.DB.Model(&entity.TenantModel{})).
			Where("tenant_model.id IN ?", modelIDs).
			Pluck("tenant_model.id", &readableModelIDs).Error; err != nil {
			return nil, fmt.Errorf("list readable model group members: %w", err)
		}
	}
	readableModels := make(map[string]struct{}, len(readableModelIDs))
	for _, modelID := range readableModelIDs {
		readableModels[modelID] = struct{}{}
	}

	var response []*ShowModelGroupResponse

	for _, modelGroupMap := range modelGroupMaps {
		if _, canReadModel := readableModels[modelGroupMap.ModelID]; !canReadModel {
			response = append(response, &ShowModelGroupResponse{
				ID:         modelGroupMap.ID,
				Position:   modelGroupMap.Position,
				Status:     modelGroupMap.Status,
				Restricted: true,
				GroupID:    modelGroupMap.GroupID,
				GroupName:  group.GroupName,
			})
			continue
		}
		showModelGroupResponse := &ShowModelGroupResponse{
			ID:        modelGroupMap.ID,
			ModelID:   modelGroupMap.ModelID,
			Model:     modelGroupMap.ModelID,
			Position:  modelGroupMap.Position,
			Status:    modelGroupMap.Status,
			GroupID:   modelGroupMap.GroupID,
			GroupName: group.GroupName,
		}
		var tenantModel *entity.TenantModel
		tenantModel, err = m.modelDAO.GetByID(ctx, dao.DB, modelGroupMap.ModelID)
		if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && tenantModel == nil) {
			showModelGroupResponse.Unavailable = true
			response = append(response, showModelGroupResponse)
			continue
		}
		if err != nil {
			return nil, err
		}
		if !strings.EqualFold(tenantModel.Status, "active") {
			showModelGroupResponse.Unavailable = true
			response = append(response, showModelGroupResponse)
			continue
		}
		showModelGroupResponse.Model = tenantModel.ModelName
		showModelGroupResponse.ModelType = entity.ModelType(tenantModel.ModelType).HumanReadable()
		showModelGroupResponse.Instance = tenantModel.InstanceID
		showModelGroupResponse.Provider = tenantModel.ProviderID

		var tenantModelInstance *entity.TenantModelInstance
		tenantModelInstance, err = m.modelInstanceDAO.GetByID(ctx, dao.DB, tenantModel.InstanceID)
		if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && tenantModelInstance == nil) {
			showModelGroupResponse.Unavailable = true
			response = append(response, showModelGroupResponse)
			continue
		}
		if err != nil {
			return nil, err
		}

		showModelGroupResponse.Instance = tenantModelInstance.InstanceName
		showModelGroupResponse.Provider = tenantModelInstance.ProviderID

		var tenantModelProvider *entity.TenantModelProvider
		tenantModelProvider, err = m.modelProviderDAO.GetByID(ctx, dao.DB, tenantModelInstance.ProviderID)
		if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && tenantModelProvider == nil) {
			showModelGroupResponse.Unavailable = true
			response = append(response, showModelGroupResponse)
			continue
		}
		if err != nil {
			return nil, err
		}

		showModelGroupResponse.Provider = tenantModelProvider.ProviderName
		response = append(response, showModelGroupResponse)
	}

	return response, nil
}

type ListModelGroupResponse struct {
	ID                 string `json:"id"`
	UserID             string `json:"user_id"`
	TenantID           string `json:"tenant_id"`
	Nickname           string `json:"nickname"`
	GroupName          string `json:"group_name"`
	Status             string `json:"status"`
	OperatorPermission int32  `json:"operator_permission"`
}

// ListModelGroup returns model groups readable by the caller. When accessUserID
// is supplied, only groups readable by both users are returned.
func (m *ModelProviderService) ListModelGroup(ctx context.Context, userID string, accessUserIDs ...string) ([]*ListModelGroupResponse, error) {
	if err := permission.NewDatabaseChecker(dao.DB).CheckFeature(ctx, "model_group", permission.ActionEnable); err != nil {
		return nil, err
	}
	tenantID, err := requireCurrentTenantID(ctx)
	if err != nil {
		return nil, err
	}
	ctx = permission.WithIdentity(ctx, userID, tenantID)
	actor, err := loadPermissionActor(ctx, dao.DB, userID, tenantID)
	if err != nil {
		return nil, err
	}
	groupQuery := dao.DB.WithContext(ctx).Model(&entity.TenantModelGroup{}).Where("tenant_id = ?", tenantID)
	if !actor.systemAdmin && (actor.membership == nil || actor.membership.Role != TenantRoleOwner) {
		if actor.membership == nil {
			return nil, common.NewCodedError(common.CodeForbidden, "Tenant membership required")
		}
		var groupIDs []string
		if err := dao.DB.WithContext(ctx).Table("group_member").Select("group_member.group_id").
			Joins("JOIN group_info ON group_info.id = group_member.group_id").
			Where("group_member.member_id = ? AND group_member.status = ? AND group_info.tenant_id = ? AND group_info.status = ?", actor.membership.ID, entity.GroupMemberStatusJoined, tenantID, string(entity.StatusValid)).
			Pluck("group_id", &groupIDs).Error; err != nil {
			return nil, fmt.Errorf("load model group memberships: %w", err)
		}
		targets := dao.DB.Where("member_id = ? AND (group_id IS NULL OR group_id = '')", actor.membership.ID)
		if len(groupIDs) > 0 {
			targets = targets.Or("group_id IN ? AND (member_id IS NULL OR member_id = '')", groupIDs)
		}
		grantedGroups := dao.DB.Model(&entity.Permission{}).Select("resource_id").
			Where("tenant_id = ? AND resource_type = ? AND status = ? AND permission <> 0", tenantID, "model_group", string(entity.StatusValid)).
			Where(targets)
		groupQuery = groupQuery.Where("(user_id = ? OR id IN (?))", userID, grantedGroups)
	}
	var groups []*entity.TenantModelGroup
	if err := groupQuery.Order("create_time DESC, id DESC").Find(&groups).Error; err != nil {
		return nil, fmt.Errorf("list readable model groups: %w", err)
	}
	accessUserID := ""
	if len(accessUserIDs) > 0 {
		accessUserID = strings.TrimSpace(accessUserIDs[0])
	}
	if accessUserID != "" && accessUserID != userID {
		accessCtx := permission.WithIdentity(ctx, accessUserID, tenantID)
		accessibleGroups, accessErr := m.ListModelGroup(accessCtx, accessUserID)
		if accessErr != nil {
			var codedErr *common.CodedError
			if errors.As(accessErr, &codedErr) && (codedErr.Code == common.CodeForbidden || codedErr.Code == common.CodeUnauthorized) {
				return []*ListModelGroupResponse{}, nil
			}
			return nil, fmt.Errorf("check model group access for user %s: %w", accessUserID, accessErr)
		}
		accessibleIDs := make(map[string]struct{}, len(accessibleGroups))
		for _, group := range accessibleGroups {
			accessibleIDs[group.ID] = struct{}{}
		}
		filteredGroups := groups[:0]
		for _, group := range groups {
			if _, ok := accessibleIDs[group.ID]; ok {
				filteredGroups = append(filteredGroups, group)
			}
		}
		groups = filteredGroups
	}

	ownerIDs := make([]string, 0, len(groups))
	seenOwnerIDs := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		if group.UserID == "" {
			continue
		}
		if _, exists := seenOwnerIDs[group.UserID]; exists {
			continue
		}
		seenOwnerIDs[group.UserID] = struct{}{}
		ownerIDs = append(ownerIDs, group.UserID)
	}
	nicknames, err := dao.NewUserDAO().GetNicknamesByIDs(ctx, dao.DB, ownerIDs)
	if err != nil {
		return nil, fmt.Errorf("resolve model group owner nicknames: %w", err)
	}

	var response []*ListModelGroupResponse

	for _, group := range groups {
		operatorPermission := int32(0)
		if actor.systemAdmin || actor.membership != nil && actor.membership.Role == TenantRoleOwner || group.UserID == userID {
			operatorPermission = resourcePermissionMask
		} else {
			operatorPermission, err = loadEffectiveResourcePermissionMask(ctx, dao.DB, actor,
				permissionResource{ID: group.ID, Type: "model_group", Name: group.GroupName, OwnerUserID: group.UserID})
			if err != nil {
				return nil, fmt.Errorf("load model group permissions: %w", err)
			}
		}
		groupResponse := &ListModelGroupResponse{
			ID:                 group.ID,
			UserID:             group.UserID,
			TenantID:           group.TenantID,
			GroupName:          group.GroupName,
			Status:             group.Status,
			OperatorPermission: operatorPermission,
		}
		groupResponse.Nickname = nicknames[group.UserID]
		response = append(response, groupResponse)
	}

	return response, nil
}

func (m *ModelProviderService) AddModelToGroupByName(ctx context.Context, userID, providerName, instanceName, modelName, groupID string) error {
	tenantModel, err := m.GetModelEntityByName(ctx, userID, providerName, instanceName, modelName)
	if err != nil {
		return err
	}
	return m.AddModelToGroupByID(ctx, tenantModel.ID, groupID)
}

func (m *ModelProviderService) AddModelToGroupByID(ctx context.Context, modelID, groupID string) error {
	tenantID, err := requireCurrentTenantID(ctx)
	if err != nil {
		return err
	}
	group, err := m.modelGroupDAO.GetByIDAndTenantID(ctx, dao.DB, tenantID, groupID)
	if err != nil {
		return err
	}
	if group == nil {
		return fmt.Errorf("model group not found")
	}
	if err := authorizeModelGroup(ctx, group, true); err != nil {
		return err
	}

	err = m.modelGroupMapDAO.AddModelToGroupTxn(ctx, dao.DB, group.ID, &entity.TenantModelGroupMap{
		ID:       common.GenerateUUID(),
		GroupID:  group.ID,
		ModelID:  modelID,
		Position: -1,
		Status:   entity.ModelGroupMapStatusActive,
	})
	if err != nil {
		return err
	}
	return nil
}

func (m *ModelProviderService) RemoveModelFromGroupByName(ctx context.Context, userID, providerName, instanceName, modelName, groupID string) error {
	tenantModel, err := m.GetModelEntityByName(ctx, userID, providerName, instanceName, modelName)
	if err != nil {
		return err
	}
	return m.RemoveModelFromGroupByID(ctx, tenantModel.ID, groupID)
}

func (m *ModelProviderService) RemoveModelFromGroupByID(ctx context.Context, modelID, groupID string) error {
	tenantID, err := requireCurrentTenantID(ctx)
	if err != nil {
		return err
	}
	group, err := m.modelGroupDAO.GetByIDAndTenantID(ctx, dao.DB, tenantID, groupID)
	if err != nil {
		return err
	}
	if group == nil {
		return fmt.Errorf("model group not found")
	}
	if err := authorizeModelGroup(ctx, group, true); err != nil {
		return err
	}
	affected, err := m.modelGroupMapDAO.DeleteByModelIDs(ctx, dao.DB, group.ID, []string{modelID})
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("model %s is not in group %s", modelID, groupID)
	}
	return nil
}

// SetModelStatusInGroup switches one group member's group-local availability
// between active and inactive. Only the membership row's status changes:
// position ordering and tenant_model.status are left alone, so the model keeps
// its slot in the group and stays usable outside it.
func (m *ModelProviderService) SetModelStatusInGroupByName(ctx context.Context, userID, providerName, instanceName, modelName, groupID, status string) error {
	tenantModel, err := m.GetModelEntityByName(ctx, userID, providerName, instanceName, modelName)
	if err != nil {
		return err
	}
	tenantID, err := requireCurrentTenantID(ctx)
	if err != nil {
		return err
	}
	group, err := m.modelGroupDAO.GetByIDAndTenantID(ctx, dao.DB, tenantID, groupID)
	if err != nil {
		return err
	}
	if group == nil {
		return fmt.Errorf("model group not found")
	}
	if err := authorizeModelGroup(ctx, group, true); err != nil {
		return err
	}
	found, err := m.modelGroupMapDAO.SetModelStatus(ctx, dao.DB, group.ID, tenantModel.ID, status)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("model %s is not in group %s", modelName, groupID)
	}
	return nil
}

func (m *ModelProviderService) ReorderModelGroupMembers(ctx context.Context, groupID string, modelIDs []string) error {
	tenantID, err := requireCurrentTenantID(ctx)
	if err != nil {
		return err
	}
	group, err := m.modelGroupDAO.GetByIDAndTenantID(ctx, dao.DB, tenantID, groupID)
	if err != nil {
		return err
	}
	if group == nil {
		return common.NewCodedError(common.CodeNotFound, "Model group not found")
	}
	if err := authorizeModelGroup(ctx, group, true); err != nil {
		return err
	}
	if len(modelIDs) == 0 {
		return common.NewCodedError(common.CodeBadRequest, "model_ids are required")
	}
	seen := make(map[string]struct{}, len(modelIDs))
	for i, modelID := range modelIDs {
		modelID = strings.TrimSpace(modelID)
		if modelID == "" {
			return common.NewCodedError(common.CodeBadRequest, "model_ids cannot contain empty IDs")
		}
		if _, exists := seen[modelID]; exists {
			return common.NewCodedError(common.CodeBadRequest, "model_ids cannot contain duplicates")
		}
		seen[modelID] = struct{}{}
		modelIDs[i] = modelID
	}
	if err := m.modelGroupMapDAO.ReorderByModelIDs(ctx, dao.DB, group.ID, modelIDs); err != nil {
		return fmt.Errorf("reorder model group members: %w", err)
	}
	return nil
}

func authorizeModelGroup(ctx context.Context, group *entity.TenantModelGroup, write bool) error {
	if err := permission.NewDatabaseChecker(dao.DB).CheckFeature(ctx, "model_group", permission.ActionEnable); err != nil {
		return err
	}
	userID := strings.TrimSpace(permission.UserIDFromContext(ctx))
	if group == nil || userID == "" {
		return common.NewCodedError(common.CodeUnauthorized, "User and model group are required")
	}
	ctx = permission.WithIdentity(ctx, userID, group.TenantID)
	actor, err := loadPermissionActor(ctx, dao.DB, userID, group.TenantID)
	if err != nil {
		return err
	}
	if actor.systemAdmin || actor.membership != nil && actor.membership.Role == TenantRoleOwner {
		return nil
	}
	resource := permissionResource{ID: group.ID, Type: "model_group", Name: group.GroupName, OwnerUserID: group.UserID}
	if group.UserID == userID {
		hasOwner, err := resourceHasOwnerGrant(dao.DB, group.TenantID, resource)
		if err != nil {
			return err
		}
		if !hasOwner {
			return nil
		}
	}
	mask, err := loadEffectiveResourcePermissionMask(ctx, dao.DB, actor, resource)
	if err != nil {
		return err
	}
	if write && mask&(entity.ResourcePermissionManage|entity.ResourcePermissionOwner) != 0 {
		return nil
	}
	if !write && mask&(entity.ResourcePermissionRead|entity.ResourcePermissionWrite|entity.ResourcePermissionManage|entity.ResourcePermissionOwner) != 0 {
		return nil
	}
	return common.NewCodedError(common.CodeForbidden, "Model group access denied")
}

func (m *ModelProviderService) GetModelEntityByName(ctx context.Context, userID, providerName, instanceName, modelName string) (*entity.TenantModel, error) {
	ctx = permission.WithIdentity(ctx, userID, permission.TenantIDFromContext(ctx))
	ownedTenantID, err := requireCurrentTenantID(ctx)
	if err != nil {
		if permissionresponse.IsPermissionError(err) {
			_, normalizedErr := permissionresponse.NormalizeHidden(err)
			return nil, normalizedErr
		}
		return nil, err
	}
	provider, err := m.findProviderByIDOrNameInCurrentTenant(ctx, providerName, false)
	if err != nil {
		return nil, err
	}
	checker := permission.NewDatabaseChecker(dao.DB)
	ctx = permission.WithIdentity(ctx, userID, ownedTenantID)
	if err = checker.CheckResource(ctx, permission.ResourceRef{Kind: permission.ResourceKindModelProvider, ID: provider.ID}, permission.OperationRead); err != nil {
		_, normalizedErr := permissionresponse.NormalizeHidden(err)
		return nil, normalizedErr
	}

	// Find instance by ID first, then by name
	instance, err := m.modelInstanceDAO.GetByID(ctx, dao.DB, instanceName)
	if err != nil || instance.ProviderID != provider.ID {
		instance, err = m.modelInstanceDAO.GetByProviderIDAndInstanceName(ctx, dao.DB, provider.ID, instanceName)
		if err != nil {
			return nil, err
		}
	}
	if err = checker.CheckResource(ctx, permission.ResourceRef{Kind: permission.ResourceKindModelInstance, ID: instance.ID}, permission.OperationRead); err != nil {
		_, normalizedErr := permissionresponse.NormalizeHidden(err)
		return nil, normalizedErr
	}

	tenantModel, err := m.modelDAO.GetModelByProviderIDAndInstanceIDAndModelName(ctx, dao.DB, provider.ID, instance.ID, modelName)
	if err != nil {
		return nil, err
	}
	if tenantModel == nil {
		return nil, fmt.Errorf("model not found")
	}
	return tenantModel, nil
}
