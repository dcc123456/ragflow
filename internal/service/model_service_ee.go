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
	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
	permissionresponse "ragflow/internal/permission/response"

	"go.uber.org/zap"
)

func (m *ModelProviderService) CreateModelGroup(ctx context.Context, userID, groupName string) error {
	var tenantModelGroup = &entity.TenantModelGroup{
		ID:        common.GenerateUUID(),
		UserID:    userID,
		GroupName: groupName,
		Status:    "active",
		Extra:     "{}",
	}
	//tenantModelGroup.UpdateCreateDateAndTime()
	return m.modelGroupDAO.Create(ctx, dao.DB, tenantModelGroup)
}

func (m *ModelProviderService) DeleteModelGroup(ctx context.Context, userID, groupName string) error {
	group, err := m.modelGroupDAO.GetByGroupName(ctx, dao.DB, userID, groupName)
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
	ID        string `json:"id"`
	ModelID   string `json:"model_id"`
	Position  int64  `json:"position"`
	Provider  string `json:"provider"`
	Instance  string `json:"instance"`
	Model     string `json:"model"`
	GroupName string `json:"group_name"`
	GroupID   string `json:"group_id"`
}

func (m *ModelProviderService) ShowModelGroup(ctx context.Context, userID, groupName string) ([]*ShowModelGroupResponse, error) {
	group, err := m.modelGroupDAO.GetByGroupName(ctx, dao.DB, userID, groupName)
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
	GroupName string `json:"group_name"`
	Status    string `json:"status"`
}

func (m *ModelProviderService) ListModelGroup(ctx context.Context, userID string) ([]*ListModelGroupResponse, error) {
	groups, err := m.modelGroupDAO.ListByUserID(ctx, dao.DB, userID)
	if err != nil {
		return nil, err
	}

	var response []*ListModelGroupResponse

	for _, group := range groups {
		groupResponse := &ListModelGroupResponse{
			ID:        group.ID,
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
	return m.AddModelToGroupByID(ctx, userID, tenantModel.ID, groupName)
}

func (m *ModelProviderService) AddModelToGroupByID(ctx context.Context, userID, modelID, groupName string) error {
	group, err := m.modelGroupDAO.GetByGroupName(ctx, dao.DB, userID, groupName)
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
	group, err := m.modelGroupDAO.GetByGroupName(ctx, dao.DB, userID, groupName)
	if err != nil {
		return err
	}
	if group == nil {
		return fmt.Errorf("model group not found")
	}
	_, err = m.modelGroupMapDAO.DeleteByModelIDs(ctx, dao.DB, group.ID, []string{tenantModel.ID})
	if err != nil {
		return err
	}
	return nil
}

func (m *ModelProviderService) RemoveModelFromGroupByID(ctx context.Context, groupModelID string) error {
	_, err := m.modelGroupMapDAO.DeleteByIDs(ctx, dao.DB, []string{groupModelID})
	if err != nil {
		return err
	}
	return nil
}

func (m *ModelProviderService) GetModelEntityByName(ctx context.Context, userID, providerName, instanceName, modelName string) (*entity.TenantModel, error) {
	ownedTenantID, err := requireCurrentTenantID(ctx, userID)
	if err != nil {
		if permissionresponse.IsPermissionError(err) {
			_, normalizedErr := normalizeModelPermissionError(err)
			return nil, normalizedErr
		}
		return nil, err
	}
	provider, err := m.findProviderByIDOrNameInTenant(ctx, ownedTenantID, providerName)
	if err != nil {
		return nil, err
	}
	if err = checkModelPermission(ctx, userID, permission.ResourceKindModelProvider, provider.ID, permission.OperationRead); err != nil {
		_, normalizedErr := normalizeModelPermissionError(err)
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
	if err = checkModelPermission(ctx, userID, permission.ResourceKindModelInstance, instance.ID, permission.OperationRead); err != nil {
		_, normalizedErr := normalizeModelPermissionError(err)
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
