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

func (m *ModelProviderService) listReadableModelProvidersForUserEE(ctx context.Context, tenantID, userID string) ([]*entity.TenantModelProvider, error) {
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
		Where("tenant_model_provider.user_id = ?", userID).
		Find(&providers).Error
	return providers, err
}

func (m *ModelProviderService) CreateModelGroup(ctx context.Context, userID, groupName string) error {
	tenantID, err := requireCurrentTenantID(ctx)
	if err != nil {
		return err
	}
	var tenantModelGroup = &entity.TenantModelGroup{
		ID:        common.GenerateUUID(),
		UserID:    userID,
		TenantID:  tenantID,
		GroupName: groupName,
		Status:    "active",
		Extra:     "{}",
	}
	return m.modelGroupDAO.Create(ctx, dao.DB, tenantModelGroup)
}

func (m *ModelProviderService) DeleteModelGroup(ctx context.Context, groupName string) error {
	tenantID, err := requireCurrentTenantID(ctx)
	if err != nil {
		return err
	}
	group, err := m.modelGroupDAO.GetByGroupName(ctx, dao.DB, tenantID, groupName)
	if err != nil {
		return err
	}
	if group == nil {
		return fmt.Errorf("model group not found")
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
	ID        string   `json:"id"`
	ModelID   string   `json:"model_id"`
	Position  int64    `json:"position"`
	Provider  string   `json:"provider"`
	Instance  string   `json:"instance"`
	Model     string   `json:"model"`
	ModelType []string `json:"model_type"`
	Status    string   `json:"status"`
	GroupName string   `json:"group_name"`
	GroupID   string   `json:"group_id"`
}

func (m *ModelProviderService) ShowModelGroup(ctx context.Context, groupName string) ([]*ShowModelGroupResponse, error) {
	tenantID, err := requireCurrentTenantID(ctx)
	if err != nil {
		return nil, err
	}
	group, err := m.modelGroupDAO.GetByGroupName(ctx, dao.DB, tenantID, groupName)
	if err != nil {
		return nil, err
	}
	if group == nil {
		return nil, fmt.Errorf("model group not found")
	}

	modelGroupMaps, err := m.modelGroupMapDAO.ListByGroupID(ctx, dao.DB, group.ID)
	if err != nil {
		return nil, err
	}

	var response []*ShowModelGroupResponse

	for _, modelGroupMap := range modelGroupMaps {
		showModelGroupResponse := &ShowModelGroupResponse{
			ID:        modelGroupMap.ID,
			ModelID:   modelGroupMap.ModelID,
			Position:  modelGroupMap.Position,
			Status:    modelGroupMap.Status,
			GroupID:   modelGroupMap.GroupID,
			GroupName: group.GroupName,
		}
		var tenantModel *entity.TenantModel
		tenantModel, err = m.modelDAO.GetByID(ctx, dao.DB, modelGroupMap.ModelID)
		if err != nil {
			return nil, err
		}
		if tenantModel == nil {
			return nil, fmt.Errorf("model not found")
		}
		showModelGroupResponse.Model = tenantModel.ModelName
		showModelGroupResponse.ModelType = entity.ModelType(tenantModel.ModelType).HumanReadable()

		var tenantModelInstance *entity.TenantModelInstance
		tenantModelInstance, err = m.modelInstanceDAO.GetByID(ctx, dao.DB, tenantModel.InstanceID)
		if err != nil {
			return nil, err
		}
		if tenantModelInstance == nil {
			return nil, fmt.Errorf("model instance not found")
		}

		showModelGroupResponse.Instance = tenantModelInstance.InstanceName

		var tenantModelProvider *entity.TenantModelProvider
		tenantModelProvider, err = m.modelProviderDAO.GetByID(ctx, dao.DB, tenantModelInstance.ProviderID)
		if err != nil {
			return nil, err
		}
		if tenantModelProvider == nil {
			return nil, fmt.Errorf("model provider not found")
		}

		showModelGroupResponse.Provider = tenantModelProvider.ProviderName
		response = append(response, showModelGroupResponse)
	}

	return response, nil
}

type ListModelGroupResponse struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	GroupName string `json:"group_name"`
	Status    string `json:"status"`
}

// ListModelGroup returns the metadata of every model group the caller owns in
// the current tenant.
func (m *ModelProviderService) ListModelGroup(ctx context.Context, userID string) ([]*ListModelGroupResponse, error) {
	tenantID, err := requireCurrentTenantID(ctx)
	if err != nil {
		return nil, err
	}
	groups, err := m.modelGroupDAO.ListByTenantIDAndUserID(ctx, dao.DB, tenantID, userID)
	if err != nil {
		return nil, err
	}

	var response []*ListModelGroupResponse

	for _, group := range groups {
		groupResponse := &ListModelGroupResponse{
			ID:        group.ID,
			TenantID:  group.TenantID,
			GroupName: group.GroupName,
			Status:    group.Status,
		}
		response = append(response, groupResponse)
	}

	return response, nil
}

func (m *ModelProviderService) AddModelToGroupByName(ctx context.Context, userID, providerName, instanceName, modelName, groupName string) error {
	tenantModel, err := m.GetModelEntityByName(ctx, userID, providerName, instanceName, modelName)
	if err != nil {
		return err
	}
	return m.AddModelToGroupByID(ctx, tenantModel.ID, groupName)
}

func (m *ModelProviderService) AddModelToGroupByID(ctx context.Context, modelID, groupName string) error {
	tenantID, err := requireCurrentTenantID(ctx)
	if err != nil {
		return err
	}
	group, err := m.modelGroupDAO.GetByGroupName(ctx, dao.DB, tenantID, groupName)
	if err != nil {
		return err
	}
	if group == nil {
		return fmt.Errorf("model group not found")
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

func (m *ModelProviderService) RemoveModelFromGroupByName(ctx context.Context, userID, providerName, instanceName, modelName string, groupName string) error {
	tenantModel, err := m.GetModelEntityByName(ctx, userID, providerName, instanceName, modelName)
	if err != nil {
		return err
	}
	tenantID, err := requireCurrentTenantID(ctx)
	if err != nil {
		return err
	}
	group, err := m.modelGroupDAO.GetByGroupName(ctx, dao.DB, tenantID, groupName)
	if err != nil {
		return err
	}
	if group == nil {
		return fmt.Errorf("model group not found")
	}
	affected, err := m.modelGroupMapDAO.DeleteByModelIDs(ctx, dao.DB, group.ID, []string{tenantModel.ID})
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("model %s is not in group %s", modelName, groupName)
	}
	return nil
}

// SetModelStatusInGroup switches one group member's group-local availability
// between active and inactive. Only the membership row's status changes:
// position ordering and tenant_model.status are left alone, so the model keeps
// its slot in the group and stays usable outside it.
func (m *ModelProviderService) SetModelStatusInGroupByName(ctx context.Context, userID, providerName, instanceName, modelName, groupName, status string) error {
	tenantModel, err := m.GetModelEntityByName(ctx, userID, providerName, instanceName, modelName)
	if err != nil {
		return err
	}
	tenantID, err := requireCurrentTenantID(ctx)
	if err != nil {
		return err
	}
	group, err := m.modelGroupDAO.GetByGroupName(ctx, dao.DB, tenantID, groupName)
	if err != nil {
		return err
	}
	if group == nil {
		return fmt.Errorf("model group not found")
	}
	found, err := m.modelGroupMapDAO.SetModelStatus(ctx, dao.DB, group.ID, tenantModel.ID, status)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("model %s is not in group %s", modelName, groupName)
	}
	return nil
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
