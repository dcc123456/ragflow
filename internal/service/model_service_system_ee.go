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
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	modelModule "ragflow/internal/entity/models"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

func (m *ModelProviderService) ListSystemModelProviders(ctx context.Context, available bool) ([]map[string]interface{}, error) {
	manager := dao.GetModelProviderManager()
	if available {
		return manager.ListProviders()
	}

	var providers []entity.TenantModelProvider
	if err := dao.DB.WithContext(ctx).
		Where("user_id = ? AND tenant_id = ?", "", "").
		Order("provider_name ASC").
		Find(&providers).Error; err != nil {
		return nil, fmt.Errorf("failed to list system model providers: %w", err)
	}

	catalogProviders, err := manager.ListProviders()
	if err != nil {
		return nil, fmt.Errorf("failed to load model provider catalog: %w", err)
	}
	catalogByName := make(map[string]map[string]interface{}, len(catalogProviders))
	for _, provider := range catalogProviders {
		if name, ok := provider["name"].(string); ok {
			catalogByName[strings.ToLower(name)] = provider
		}
	}

	result := make([]map[string]interface{}, 0, len(providers))
	for _, provider := range providers {
		var item map[string]interface{}
		if catalogItem, ok := catalogByName[strings.ToLower(provider.ProviderName)]; ok {
			item = make(map[string]interface{}, len(catalogItem)+4)
			for key, value := range catalogItem {
				item[key] = value
			}
		} else {
			item = map[string]interface{}{"name": provider.ProviderName}
		}

		var instanceCount int64
		if err := dao.DB.WithContext(ctx).Model(&entity.TenantModelInstance{}).
			Where("provider_id = ?", provider.ID).Count(&instanceCount).Error; err != nil {
			return nil, fmt.Errorf("failed to count instances for system provider %s: %w", provider.ProviderName, err)
		}
		item["id"] = provider.ID
		item["provider_id"] = provider.ID
		item["provider_name"] = provider.ProviderName
		item["has_instance"] = instanceCount > 0
		result = append(result, item)
	}
	return result, nil
}

func (m *ModelProviderService) ListSystemAddedModels(ctx context.Context, modelTypeFilter string) ([]map[string]interface{}, error) {
	modelTypeFilter = strings.ToLower(strings.TrimSpace(modelTypeFilter))
	modelTypeFilterBin := entity.ModelTypeFromString(modelTypeFilter)

	var providers []entity.TenantModelProvider
	if err := dao.DB.WithContext(ctx).
		Where("user_id = ? AND tenant_id = ?", "", "").
		Find(&providers).Error; err != nil {
		return nil, fmt.Errorf("failed to list system model providers: %w", err)
	}
	if len(providers) == 0 {
		return []map[string]interface{}{}, nil
	}

	providerByID := make(map[string]*entity.TenantModelProvider, len(providers))
	providerIDs := make([]string, 0, len(providers))
	for i := range providers {
		providerByID[providers[i].ID] = &providers[i]
		providerIDs = append(providerIDs, providers[i].ID)
	}

	var instances []entity.TenantModelInstance
	if err := dao.DB.WithContext(ctx).
		Where("provider_id IN ?", providerIDs).
		Find(&instances).Error; err != nil {
		return nil, fmt.Errorf("failed to list system model instances: %w", err)
	}
	if len(instances) == 0 {
		return []map[string]interface{}{}, nil
	}

	instanceByID := make(map[string]*entity.TenantModelInstance, len(instances))
	instanceIDs := make([]string, 0, len(instances))
	for i := range instances {
		instanceByID[instances[i].ID] = &instances[i]
		instanceIDs = append(instanceIDs, instances[i].ID)
	}

	var models []entity.TenantModel
	if err := dao.DB.WithContext(ctx).
		Where("provider_id IN ? AND instance_id IN ?", providerIDs, instanceIDs).
		Find(&models).Error; err != nil {
		return nil, fmt.Errorf("failed to list system models: %w", err)
	}

	result := make([]map[string]interface{}, 0, len(models))
	for _, model := range models {
		modelType := entity.ModelType(model.ModelType)
		if modelTypeFilterBin != 0 && !modelType.Has(modelTypeFilterBin) {
			continue
		}
		provider := providerByID[model.ProviderID]
		instance := instanceByID[model.InstanceID]
		if provider == nil || instance == nil {
			continue
		}
		result = append(result, map[string]interface{}{
			"model_id":      model.ID,
			"tenant_id":     "",
			"tenant_name":   "",
			"model_type":    modelType.HumanReadable(),
			"name":          model.ModelName,
			"provider_id":   model.ProviderID,
			"provider_name": provider.ProviderName,
			"instance_id":   model.InstanceID,
			"instance_name": instance.InstanceName,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		providerI, _ := result[i]["provider_name"].(string)
		providerJ, _ := result[j]["provider_name"].(string)
		if providerI != providerJ {
			return providerI < providerJ
		}
		instanceI, _ := result[i]["instance_name"].(string)
		instanceJ, _ := result[j]["instance_name"].(string)
		if instanceI != instanceJ {
			return instanceI < instanceJ
		}
		nameI, _ := result[i]["name"].(string)
		nameJ, _ := result[j]["name"].(string)
		return nameI < nameJ
	})
	return result, nil
}

func (m *ModelProviderService) AddSystemModelProvider(ctx context.Context, providerName string) (map[string]interface{}, error) {
	catalogProvider, err := findCatalogProvider(providerName)
	if err != nil {
		return nil, err
	}

	var provider entity.TenantModelProvider
	err = dao.DB.WithContext(ctx).
		Where("user_id = ? AND tenant_id = ? AND LOWER(provider_name) = LOWER(?)", "", "", catalogProvider.Name).
		Take(&provider).Error
	if err == nil {
		return systemProviderResponse(&provider), nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("failed to find system model provider: %w", err)
	}

	provider = entity.TenantModelProvider{
		ID:           common.GenerateToken(),
		UserID:       "",
		TenantID:     "",
		ProviderName: catalogProvider.Name,
	}
	if err := dao.DB.WithContext(ctx).Create(&provider).Error; err != nil {
		var existing entity.TenantModelProvider
		lookupErr := dao.DB.WithContext(ctx).
			Where("user_id = ? AND tenant_id = ? AND LOWER(provider_name) = LOWER(?)", "", "", catalogProvider.Name).
			Take(&existing).Error
		if lookupErr == nil {
			return systemProviderResponse(&existing), nil
		}
		return nil, fmt.Errorf("failed to add system model provider: %w", err)
	}
	return systemProviderResponse(&provider), nil
}

func (m *ModelProviderService) DeleteSystemModelProviders(ctx context.Context, providerNames []string) (map[string]interface{}, error) {
	requested := normalizedNames(providerNames)
	if len(requested) == 0 {
		return map[string]interface{}{"provider_names": []string{}}, nil
	}

	deleted := make([]string, 0, len(requested))
	err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var providers []entity.TenantModelProvider
		if err := tx.Where("user_id = ? AND tenant_id = ?", "", "").Find(&providers).Error; err != nil {
			return fmt.Errorf("failed to find system model providers: %w", err)
		}

		requestedSet := make(map[string]struct{}, len(requested))
		for _, name := range requested {
			requestedSet[strings.ToLower(name)] = struct{}{}
		}
		found := make(map[string]struct{}, len(requested))
		providerIDs := make([]string, 0, len(requested))
		for _, provider := range providers {
			if _, ok := requestedSet[strings.ToLower(provider.ProviderName)]; ok {
				providerIDs = append(providerIDs, provider.ID)
				deleted = append(deleted, provider.ProviderName)
				found[strings.ToLower(provider.ProviderName)] = struct{}{}
			}
		}
		var missing []string
		for _, name := range requested {
			if _, ok := found[strings.ToLower(name)]; !ok {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			return common.NewCodedError(common.CodeNotFound, fmt.Sprintf("System model providers %q are not configured", missing))
		}
		if len(providerIDs) == 0 {
			return nil
		}

		var instances []entity.TenantModelInstance
		if err := tx.Where("provider_id IN ?", providerIDs).Find(&instances).Error; err != nil {
			return fmt.Errorf("failed to find system provider instances: %w", err)
		}
		instanceIDs := make([]string, 0, len(instances))
		for _, instance := range instances {
			instanceIDs = append(instanceIDs, instance.ID)
		}

		var models []entity.TenantModel
		if len(instanceIDs) > 0 {
			if err := tx.Where("instance_id IN ? AND provider_id IN ?", instanceIDs, providerIDs).Find(&models).Error; err != nil {
				return fmt.Errorf("failed to find system provider models: %w", err)
			}
		}
		modelIDs := modelIDs(models)
		if err := deleteRoleDefaultModels(tx, modelIDs); err != nil {
			return err
		}
		if len(modelIDs) > 0 {
			if err := tx.Unscoped().Where("id IN ?", modelIDs).Delete(&entity.TenantModel{}).Error; err != nil {
				return fmt.Errorf("failed to delete system provider models: %w", err)
			}
		}
		if len(instanceIDs) > 0 {
			if err := tx.Unscoped().Where("id IN ?", instanceIDs).Delete(&entity.TenantModelInstance{}).Error; err != nil {
				return fmt.Errorf("failed to delete system provider instances: %w", err)
			}
		}
		if err := tx.Unscoped().Where("id IN ? AND user_id = ? AND tenant_id = ?", providerIDs, "", "").Delete(&entity.TenantModelProvider{}).Error; err != nil {
			return fmt.Errorf("failed to delete system model providers: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(deleted)
	return map[string]interface{}{"provider_names": deleted}, nil
}

func (m *ModelProviderService) ListSystemModelInstances(ctx context.Context, providerName string) ([]map[string]interface{}, error) {
	provider, _, err := m.systemModelProvider(ctx, providerName)
	if err != nil {
		return nil, err
	}

	var instances []entity.TenantModelInstance
	if err := dao.DB.WithContext(ctx).Where("provider_id = ?", provider.ID).
		Order("create_time DESC").Find(&instances).Error; err != nil {
		return nil, fmt.Errorf("failed to list system model instances: %w", err)
	}

	result := make([]map[string]interface{}, 0, len(instances))
	for _, instance := range instances {
		extra, err := decodeModelExtra(instance.Extra)
		if err != nil {
			return nil, fmt.Errorf("failed to decode instance %s settings: %w", instance.ID, err)
		}
		result = append(result, map[string]interface{}{
			"id":            instance.ID,
			"instance_name": instance.InstanceName,
			"provider_id":   provider.ID,
			"region":        extra["region"],
			"status":        instance.Status,
		})
	}
	return result, nil
}

func (m *ModelProviderService) ShowSystemProviderInstance(ctx context.Context, providerName, instanceName string) (map[string]interface{}, error) {
	provider, _, instance, extra, err := m.systemModelInstance(ctx, providerName, instanceName)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"id":            instance.ID,
		"instance_name": instance.InstanceName,
		"provider_id":   provider.ID,
		"region":        extra["region"],
		"base_url":      extra["base_url"],
		"api_key":       instance.APIKey,
		"status":        instance.Status,
	}, nil
}

func (m *ModelProviderService) ShowSystemProviderInstanceBalance(ctx context.Context, providerName, instanceName string) (map[string]interface{}, error) {
	_, catalogProvider, instance, extra, err := m.systemModelInstance(ctx, providerName, instanceName)
	if err != nil {
		return nil, err
	}
	driver, apiConfig, err := configuredModelDriver(catalogProvider, instance.APIKey, stringSetting(extra, "region"), stringSetting(extra, "base_url"))
	if err != nil {
		return nil, err
	}
	result, err := driver.Balance(ctx, apiConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to read model provider balance: %w", err)
	}
	return result, nil
}

func (m *ModelProviderService) CheckSystemInstanceConnection(ctx context.Context, providerName, instanceName string) (map[string]interface{}, error) {
	_, catalogProvider, instance, extra, err := m.systemModelInstance(ctx, providerName, instanceName)
	if err != nil {
		return nil, err
	}
	if err := m.checkSystemProviderConnection(ctx, catalogProvider, instance.APIKey, stringSetting(extra, "region"), stringSetting(extra, "base_url"), instance.ID, nil); err != nil {
		return nil, err
	}
	return map[string]interface{}{"success": true}, nil
}

func (m *ModelProviderService) CheckSystemProviderConnection(ctx context.Context, providerName string, request *CheckConnectionRequest) (map[string]interface{}, error) {
	if request == nil {
		return nil, common.NewCodedError(common.CodeBadRequest, "Request is required")
	}
	apiKey := NormalizeAPIKey(request.APIKey)
	region := strings.TrimSpace(request.Region)
	baseURL := strings.TrimSpace(request.BaseURL)
	instanceID := strings.TrimSpace(request.InstanceID)
	var catalogProvider *modelModule.Provider
	if instanceID != "" {
		var err error
		_, catalogProvider, _, _, err = m.systemModelInstance(ctx, providerName, instanceID)
		if err != nil {
			return nil, err
		}
	} else {
		var err error
		catalogProvider, err = findCatalogProvider(providerName)
		if err != nil {
			return nil, err
		}
	}
	if err := m.checkSystemProviderConnection(ctx, catalogProvider, apiKey, region, baseURL, instanceID, request.ModelInfo); err != nil {
		return nil, err
	}
	return map[string]interface{}{"success": true}, nil
}

func (m *ModelProviderService) checkSystemProviderConnection(ctx context.Context, provider *modelModule.Provider, apiKey, region, baseURL, instanceID string, modelInfo []CheckConnectionModelInfo) error {
	if provider == nil || provider.ModelDriver == nil {
		return common.NewCodedError(common.CodeNotFound, "Model provider driver is unavailable")
	}
	apiKey = strings.TrimSpace(apiKey)
	region = strings.TrimSpace(region)
	baseURL = strings.TrimSpace(baseURL)
	if region == "" {
		region = "default"
	}
	driver := provider.ModelDriver
	if strings.EqualFold(provider.Class, "local") {
		if baseURL == "" {
			return common.NewCodedError(common.CodeDataError, fmt.Sprintf("base_url is required for local provider %s", provider.Name))
		}
		var err error
		driver, err = newModelDriverForBaseURL(driver, provider.Name, region, baseURL)
		if err != nil {
			return fmt.Errorf("failed to initialize provider connection: %w", err)
		}
	}
	apiConfig := &modelModule.APIConfig{ApiKey: &apiKey, Region: &region, BaseURL: &baseURL}
	verifyResults, verifyErr := verifyProviderModel(ctx, driver, provider.Models, apiConfig, modelInfo)
	if instanceID != "" && len(verifyResults) > 0 {
		if err := m.updateSystemModelVerifyResults(ctx, provider.Name, instanceID, verifyResults); err != nil {
			common.Logger.Error("failed to persist system model verify results", zap.Error(err))
		}
	}
	if verifyErr != nil {
		return fmt.Errorf("model provider connection failed: %w", verifyErr)
	}
	return nil
}

func (m *ModelProviderService) updateSystemModelVerifyResults(ctx context.Context, providerName, instanceID string, results map[string]string) error {
	provider, _, instance, _, err := m.systemModelInstance(ctx, providerName, instanceID)
	if err != nil {
		return err
	}
	for modelName, verifyStatus := range results {
		var model entity.TenantModel
		if err := dao.DB.WithContext(ctx).
			Where("provider_id = ? AND instance_id = ? AND LOWER(model_name) = LOWER(?)", provider.ID, instance.ID, modelName).
			Take(&model).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return fmt.Errorf("failed to find system model %q for verification update: %w", modelName, err)
		}
		extra, err := decodeModelExtra(model.Extra)
		if err != nil {
			return fmt.Errorf("failed to decode system model %q settings: %w", modelName, err)
		}
		extra["verify"] = verifyStatus
		extraJSON, err := json.Marshal(extra)
		if err != nil {
			return fmt.Errorf("failed to encode system model %q settings: %w", modelName, err)
		}
		if err := dao.DB.WithContext(ctx).Model(&entity.TenantModel{}).
			Where("id = ? AND provider_id = ? AND instance_id = ?", model.ID, provider.ID, instance.ID).
			Update("extra", string(extraJSON)).Error; err != nil {
			return fmt.Errorf("failed to update system model %q verification status: %w", modelName, err)
		}
	}
	return nil
}

func (m *ModelProviderService) AlterSystemProviderInstance(ctx context.Context, providerName, instanceName string, request *AlterProviderInstanceRequest) (map[string]interface{}, error) {
	if request == nil || request.ModelInfo == nil {
		return nil, common.NewCodedError(common.CodeBadRequest, "model_info is required")
	}
	provider, _, instance, extra, err := m.systemModelInstance(ctx, providerName, instanceName)
	if err != nil {
		return nil, err
	}

	newInstanceName := strings.TrimSpace(request.InstanceName)
	updates := map[string]interface{}{"api_key": NormalizeAPIKey(request.APIKey)}
	if newInstanceName != "" && newInstanceName != instance.InstanceName {
		var duplicate entity.TenantModelInstance
		err := dao.DB.WithContext(ctx).Where("provider_id = ? AND instance_name = ? AND id <> ?", provider.ID, newInstanceName, instance.ID).
			Take(&duplicate).Error
		if err == nil {
			return nil, common.NewCodedError(common.CodeConflict, "A model instance with this name already exists")
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("failed to check model instance name: %w", err)
		}
		updates["instance_name"] = newInstanceName
	}
	baseURL := strings.TrimSpace(request.BaseURL)
	region := strings.TrimSpace(request.Region)
	if baseURL != "" {
		extra["base_url"] = baseURL
	}
	if region != "" {
		extra["region"] = region
	}
	extraJSON, err := json.Marshal(extra)
	if err != nil {
		return nil, fmt.Errorf("failed to encode model instance settings: %w", err)
	}
	updates["extra"] = string(extraJSON)
	if err := dao.DB.WithContext(ctx).Model(&entity.TenantModelInstance{}).
		Where("id = ? AND provider_id = ?", instance.ID, provider.ID).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("failed to update model instance: %w", err)
	}

	existingModels, err := m.modelDAO.GetModelsByInstanceID(ctx, dao.DB, instance.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to list system instance models: %w", err)
	}
	existingModelMap := make(map[string]*entity.TenantModel, len(existingModels))
	submittedModelNames := make(map[string]bool, len(*request.ModelInfo))
	for _, model := range existingModels {
		existingModelMap[model.ModelName] = model
	}
	for _, model := range *request.ModelInfo {
		if model.ModelName != "" {
			submittedModelNames[model.ModelName] = true
		}
	}

	var modelIDsToRemove []string
	for name, model := range existingModelMap {
		if !submittedModelNames[name] {
			modelIDsToRemove = append(modelIDsToRemove, model.ID)
		}
	}
	if len(modelIDsToRemove) > 0 {
		if err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := deleteRoleDefaultModels(tx, modelIDsToRemove); err != nil {
				return err
			}
			if err := tx.Unscoped().Where("provider_id = ? AND instance_id = ? AND id IN ?", provider.ID, instance.ID, modelIDsToRemove).
				Delete(&entity.TenantModel{}).Error; err != nil {
				return fmt.Errorf("failed to delete unselected system models: %w", err)
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}

	effectiveInstanceName := firstNonEmpty(newInstanceName, instance.InstanceName)
	for _, modelInfo := range *request.ModelInfo {
		if modelInfo.ModelName == "" {
			continue
		}
		if existingModel, exists := existingModelMap[modelInfo.ModelName]; exists {
			modelUpdates := make(map[string]interface{})
			if len(modelInfo.ModelTypes) > 0 {
				modelType := entity.ModelTypeFromStrings(modelInfo.ModelTypes)
				if int(modelType) != existingModel.ModelType {
					modelUpdates["model_type"] = int(modelType)
				}
			}
			modelExtra := make(map[string]interface{})
			if existingModel.Extra != "" {
				_ = json.Unmarshal([]byte(existingModel.Extra), &modelExtra)
			}
			for key, value := range modelInfo.Extra {
				modelExtra[key] = value
			}
			if modelInfo.MaxTokens > 0 {
				modelExtra["max_tokens"] = modelInfo.MaxTokens
			}
			extraBytes, err := json.Marshal(modelExtra)
			if err != nil {
				return nil, fmt.Errorf("failed to encode model %q settings: %w", modelInfo.ModelName, err)
			}
			modelUpdates["extra"] = string(extraBytes)
			if err := m.modelDAO.UpdateByID(ctx, dao.DB, existingModel.ID, modelUpdates); err != nil {
				return nil, fmt.Errorf("failed to update system model %q: %w", modelInfo.ModelName, err)
			}
			continue
		}
		if err := m.addModelToInstance(ctx, dao.DB, provider, instance, modelInfo); err != nil {
			return nil, err
		}
	}

	return m.ShowSystemProviderInstance(ctx, providerName, effectiveInstanceName)
}

func (m *ModelProviderService) AddSystemModelInstance(ctx context.Context, providerName string, request *CreateProviderInstanceRequest) (map[string]interface{}, error) {
	if request == nil {
		return nil, common.NewCodedError(common.CodeBadRequest, "Request is required")
	}
	provider, _, err := m.systemModelProvider(ctx, providerName)
	if err != nil {
		return nil, err
	}
	instanceName := strings.TrimSpace(request.InstanceName)
	if err = ValidateProviderInstanceName(instanceName); err != nil {
		return nil, common.NewCodedError(common.CodeBadRequest, err.Error())
	}
	apiKey := NormalizeAPIKey(request.APIKey)
	baseURL := strings.TrimSpace(request.BaseURL)
	region := strings.TrimSpace(request.Region)
	configured := apiKey != "" || baseURL != "" || region != "" || len(request.ModelInfo) > 0
	extra := "{}"
	if configured {
		extraBytes, err := json.Marshal(map[string]string{"region": region, "base_url": baseURL})
		if err != nil {
			return nil, fmt.Errorf("failed to encode system instance settings: %w", err)
		}
		extra = string(extraBytes)
	}
	instance := entity.TenantModelInstance{
		ID:           common.GenerateToken(),
		InstanceName: instanceName,
		ProviderID:   provider.ID,
		APIKey:       apiKey,
		Status:       "active",
		Extra:        extra,
	}
	if err = m.modelInstanceDAO.Create(ctx, dao.DB, &instance); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "already exists") {
			return nil, common.NewCodedError(common.CodeConflict, err.Error())
		}
		return nil, fmt.Errorf("failed to add system model instance: %w", err)
	}
	if configured {
		if err = m.addModelsToNewInstance(ctx, dao.DB, provider, &instance, request.ModelInfo); err != nil {
			if _, rollbackErr := m.modelInstanceDAO.DeleteByIDs(ctx, dao.DB, []string{instance.ID}); rollbackErr != nil {
				common.Logger.Error("failed to roll back system model instance after model creation failure", zap.String("instance_id", instance.ID), zap.Error(rollbackErr))
			}
			return nil, err
		}
	}
	return map[string]interface{}{
		"id":            instance.ID,
		"instance_name": instance.InstanceName,
		"provider_id":   provider.ID,
		"status":        instance.Status,
	}, nil
}

func (m *ModelProviderService) DeleteSystemModelInstances(ctx context.Context, providerName string, instanceNames []string) (map[string]interface{}, error) {
	provider, _, err := m.systemModelProvider(ctx, providerName)
	if err != nil {
		return nil, err
	}
	names := normalizedNames(instanceNames)
	if len(names) == 0 {
		return map[string]interface{}{"instance_names": []string{}}, nil
	}

	deleted := make([]string, 0, len(names))
	err = dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var instances []entity.TenantModelInstance
		if err := tx.Where("provider_id = ?", provider.ID).Find(&instances).Error; err != nil {
			return fmt.Errorf("failed to find system model instances: %w", err)
		}
		instanceByID := make(map[string]entity.TenantModelInstance, len(instances))
		instanceByName := make(map[string]entity.TenantModelInstance, len(instances))
		for _, instance := range instances {
			instanceByID[instance.ID] = instance
			instanceByName[strings.ToLower(instance.InstanceName)] = instance
		}
		instanceIDs := make([]string, 0, len(instances))
		selected := make(map[string]entity.TenantModelInstance, len(names))
		var missing []string
		for _, name := range names {
			instance, ok := instanceByID[name]
			if !ok {
				instance, ok = instanceByName[strings.ToLower(name)]
			}
			if !ok {
				missing = append(missing, name)
				continue
			}
			selected[instance.ID] = instance
		}
		if len(missing) > 0 {
			return common.NewCodedError(common.CodeNotFound, fmt.Sprintf("System model instances %q were not found", missing))
		}
		for _, instance := range selected {
			instanceIDs = append(instanceIDs, instance.ID)
			deleted = append(deleted, instance.InstanceName)
		}
		if len(instanceIDs) == 0 {
			return nil
		}

		var models []entity.TenantModel
		if err := tx.Where("provider_id = ? AND instance_id IN ?", provider.ID, instanceIDs).Find(&models).Error; err != nil {
			return fmt.Errorf("failed to find system instance models: %w", err)
		}
		if err := deleteRoleDefaultModels(tx, modelIDs(models)); err != nil {
			return err
		}
		if err := tx.Unscoped().Where("provider_id = ? AND instance_id IN ?", provider.ID, instanceIDs).Delete(&entity.TenantModel{}).Error; err != nil {
			return fmt.Errorf("failed to delete system instance models: %w", err)
		}
		if err := tx.Unscoped().Where("provider_id = ? AND id IN ?", provider.ID, instanceIDs).Delete(&entity.TenantModelInstance{}).Error; err != nil {
			return fmt.Errorf("failed to delete system model instances: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(deleted)
	return map[string]interface{}{"instance_names": deleted}, nil
}

func (m *ModelProviderService) ListSystemInstanceModels(ctx context.Context, providerName, instanceName string, supported bool) ([]map[string]interface{}, error) {
	_, catalogProvider, instance, extra, err := m.systemModelInstance(ctx, providerName, instanceName)
	if err != nil {
		return nil, err
	}
	if supported {
		driver, apiConfig, err := configuredModelDriver(catalogProvider, instance.APIKey, stringSetting(extra, "region"), stringSetting(extra, "base_url"))
		if err != nil {
			return nil, err
		}
		models, err := driver.ListModels(ctx, apiConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to list models supported by provider: %w", err)
		}
		result := make([]map[string]interface{}, 0, len(models))
		for _, model := range models {
			result = append(result, map[string]interface{}{
				"name":           model.Name,
				"context_length": model.ContextLength,
				"max_output":     model.MaxOutput,
				"model_types":    model.ModelTypes,
				"max_dimension":  model.MaxDimension,
				"max_batch_size": model.MaxBatchSize,
				"dimensions":     model.Dimensions,
				"thinking":       model.Thinking,
			})
		}
		return result, nil
	}

	var models []entity.TenantModel
	if err := dao.DB.WithContext(ctx).
		Where("provider_id = ? AND instance_id = ?", instance.ProviderID, instance.ID).
		Order("model_name ASC").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("failed to list configured system models: %w", err)
	}

	result := make([]map[string]interface{}, 0, len(models))
	for _, model := range models {
		modelExtra, err := decodeModelExtra(model.Extra)
		if err != nil {
			return nil, fmt.Errorf("failed to decode model %s settings: %w", model.ID, err)
		}
		maxTokens := numberSetting(modelExtra["max_tokens"])
		if maxTokens <= 0 {
			if catalogModel := dao.GetModelProviderManager().FindModel(catalogProvider, model.ModelName); catalogModel != nil {
				maxTokens = catalogModelContextLength(catalogModel)
			}
		}
		if maxTokens <= 0 {
			maxTokens = 8192
		}
		verify, _ := modelExtra["verify"].(string)
		if verify == "" {
			verify = entity.ModelVerifyUnknown
		}
		features := make([]string, 0, 2)
		if truthy(modelExtra["is_tools"]) {
			features = append(features, "is_tools")
		}
		if truthy(modelExtra["thinking"]) {
			features = append(features, "thinking")
		}
		result = append(result, map[string]interface{}{
			"id":         model.ID,
			"model_id":   model.ID,
			"name":       model.ModelName,
			"model_type": entity.ModelType(model.ModelType).HumanReadable(),
			"max_tokens": maxTokens,
			"status":     model.Status,
			"verify":     verify,
			"features":   features,
			"extra":      modelExtra,
		})
	}
	return result, nil
}

func (m *ModelProviderService) AlterSystemModel(ctx context.Context, providerName, instanceName, modelName string, request *AlterModelRequest) (map[string]interface{}, error) {
	if request == nil {
		return nil, common.NewCodedError(common.CodeBadRequest, "Request is required")
	}
	status := strings.TrimSpace(request.Status)
	if status != "" && status != "active" && status != "inactive" {
		return nil, common.NewCodedError(common.CodeBadRequest, "Status must be 'active' or 'inactive'")
	}
	provider, _, instance, _, err := m.systemModelInstance(ctx, providerName, instanceName)
	if err != nil {
		return nil, err
	}
	modelName = strings.TrimSpace(modelName)
	modelID := strings.TrimSpace(request.ModelID)
	if modelName == "" && modelID == "" {
		return nil, common.NewCodedError(common.CodeBadRequest, "Model name or model ID is required")
	}

	query := dao.DB.WithContext(ctx).Where("provider_id = ? AND instance_id = ?", provider.ID, instance.ID)
	if modelName != "" {
		query = query.Where("LOWER(model_name) = LOWER(?)", modelName)
	} else {
		query = query.Where("id = ?", modelID)
	}
	var model entity.TenantModel
	if err := query.Take(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewCodedError(common.CodeNotFound, "Model not found in this provider instance")
		}
		return nil, fmt.Errorf("failed to find system model: %w", err)
	}
	if modelID != "" && model.ID != modelID {
		return nil, common.NewCodedError(common.CodeBadRequest, "Model ID does not match model name")
	}
	updates := make(map[string]interface{})
	if status != "" {
		updates["status"] = status
	}
	if request.ModelType != nil {
		updates["model_type"] = resolveModelType(request.ModelType)
	}
	modelExtra := make(map[string]interface{})
	if request.Extra != nil {
		switch value := request.Extra.(type) {
		case map[string]interface{}:
			for key, item := range value {
				modelExtra[key] = item
			}
		default:
			return nil, common.NewCodedError(common.CodeBadRequest, "extra must be an object")
		}
	}
	if request.MaxTokens > 0 {
		modelExtra["max_tokens"] = request.MaxTokens
	}
	if len(modelExtra) > 0 {
		existingExtra, err := decodeModelExtra(model.Extra)
		if err != nil {
			return nil, fmt.Errorf("failed to decode system model settings: %w", err)
		}
		for key, value := range modelExtra {
			existingExtra[key] = value
		}
		extraJSON, err := json.Marshal(existingExtra)
		if err != nil {
			return nil, fmt.Errorf("failed to encode system model settings: %w", err)
		}
		updates["extra"] = string(extraJSON)
	}
	if len(updates) == 0 {
		return nil, common.NewCodedError(common.CodeBadRequest, "At least one model field must be updated")
	}
	if err := dao.DB.WithContext(ctx).Model(&entity.TenantModel{}).
		Where("id = ? AND provider_id = ? AND instance_id = ?", model.ID, provider.ID, instance.ID).
		Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("failed to update system model: %w", err)
	}
	if status == "" {
		status = model.Status
	}
	return map[string]interface{}{"model_id": model.ID, "model_name": model.ModelName, "status": status}, nil
}

func (m *ModelProviderService) AddSystemModel(ctx context.Context, request *AddModelRequest) error {
	if request == nil {
		return common.NewCodedError(common.CodeBadRequest, "Request is required")
	}

	modelName := strings.TrimSpace(request.ModelName)
	if modelName == "" {
		return common.NewCodedError(common.CodeBadRequest, "Model name is required")
	}

	provider, catalogProvider, instance, _, err := m.systemModelInstance(ctx, request.ProviderName, request.InstanceName)
	if err != nil {
		return err
	}

	if _, err = m.modelDAO.GetModelByProviderIDAndInstanceIDAndModelName(ctx, dao.DB, provider.ID, instance.ID, modelName); err == nil {
		return common.NewCodedError(common.CodeConflict, fmt.Sprintf("Model %q already exists for provider %q and instance %q", modelName, request.ProviderName, request.InstanceName))
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("failed to check existing system model %q: %w", modelName, err)
	}

	modelType := entity.ModelType(0)
	for _, rawType := range request.ModelTypes {
		modelType |= entity.ModelTypeFromString(strings.TrimSpace(rawType))
	}

	maxTokens := request.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 8192
	}
	extra := map[string]interface{}{"max_tokens": maxTokens}
	if catalogModel := dao.GetModelProviderManager().FindModel(catalogProvider, modelName); catalogModel != nil {
		if catalogModel.Tools != nil && catalogModel.Tools.Support {
			extra["is_tools"] = true
		}
		if catalogModel.Thinking != nil {
			extra["thinking"] = true
		}
	}
	for key, value := range request.Extra {
		extra[key] = value
	}

	if err := m.createModelRecord(ctx, dao.DB, provider.ID, instance.ID, modelName, modelType, "active", extra); err != nil {
		return fmt.Errorf("failed to add system model %q: %w", modelName, err)
	}
	return nil
}

func (m *ModelProviderService) DeleteSystemModels(ctx context.Context, providerName, instanceName string, modelNames []string) (map[string]interface{}, error) {
	provider, _, instance, _, err := m.systemModelInstance(ctx, providerName, instanceName)
	if err != nil {
		return nil, err
	}
	names := normalizedNames(modelNames)
	if len(names) == 0 {
		return map[string]interface{}{"model_names": []string{}}, nil
	}

	deleted := make([]string, 0, len(names))
	err = dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var models []entity.TenantModel
		if err := tx.Where("provider_id = ? AND instance_id = ?", provider.ID, instance.ID).Find(&models).Error; err != nil {
			return fmt.Errorf("failed to find system models: %w", err)
		}
		modelsByName := make(map[string]entity.TenantModel, len(models))
		for _, model := range models {
			modelsByName[strings.ToLower(model.ModelName)] = model
		}
		var missing []string
		for _, name := range names {
			if _, ok := modelsByName[strings.ToLower(name)]; !ok {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			return common.NewCodedError(common.CodeNotFound, fmt.Sprintf("System models %q were not found", missing))
		}
		selected := make(map[string]entity.TenantModel, len(names))
		for _, name := range names {
			model := modelsByName[strings.ToLower(name)]
			selected[model.ID] = model
		}
		models = models[:0]
		for _, model := range selected {
			models = append(models, model)
		}
		ids := modelIDs(models)
		for _, model := range models {
			deleted = append(deleted, model.ModelName)
		}
		if err := deleteRoleDefaultModels(tx, ids); err != nil {
			return err
		}
		if len(ids) > 0 {
			if err := tx.Unscoped().Where("provider_id = ? AND instance_id = ? AND id IN ?", provider.ID, instance.ID, ids).
				Delete(&entity.TenantModel{}).Error; err != nil {
				return fmt.Errorf("failed to delete system models: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(deleted)
	return map[string]interface{}{"model_names": deleted}, nil
}

func (m *ModelProviderService) systemModelProvider(ctx context.Context, providerName string) (*entity.TenantModelProvider, *modelModule.Provider, error) {
	catalogProvider, err := findCatalogProvider(providerName)
	if err != nil {
		return nil, nil, err
	}
	var provider entity.TenantModelProvider
	err = dao.DB.WithContext(ctx).
		Where("user_id = ? AND tenant_id = ? AND LOWER(provider_name) = LOWER(?)", "", "", catalogProvider.Name).
		Take(&provider).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, common.NewCodedError(common.CodeNotFound, fmt.Sprintf("System model provider %q is not configured", catalogProvider.Name))
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to find system model provider: %w", err)
	}
	return &provider, catalogProvider, nil
}

func (m *ModelProviderService) systemModelInstance(ctx context.Context, providerName, instanceName string) (*entity.TenantModelProvider, *modelModule.Provider, *entity.TenantModelInstance, map[string]interface{}, error) {
	provider, catalogProvider, err := m.systemModelProvider(ctx, providerName)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	instanceName = strings.TrimSpace(instanceName)
	if instanceName == "" {
		return nil, nil, nil, nil, common.NewCodedError(common.CodeBadRequest, "Instance name is required")
	}

	instance, err := m.modelInstanceDAO.GetByID(ctx, dao.DB, instanceName)
	if err != nil || instance.ProviderID != provider.ID {
		instance, err = m.modelInstanceDAO.GetByProviderIDAndInstanceName(ctx, dao.DB, provider.ID, instanceName)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, nil, nil, common.NewCodedError(common.CodeNotFound, fmt.Sprintf("System model instance %q not found", instanceName))
	}
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to find system model instance: %w", err)
	}
	extra, err := decodeModelExtra(instance.Extra)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to decode instance settings: %w", err)
	}
	return provider, catalogProvider, instance, extra, nil
}

func findCatalogProvider(providerName string) (*modelModule.Provider, error) {
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return nil, common.NewCodedError(common.CodeBadRequest, "Provider name is required")
	}
	provider := dao.GetModelProviderManager().FindProvider(providerName)
	if provider == nil {
		return nil, common.NewCodedError(common.CodeNotFound, fmt.Sprintf("Model provider %q is not supported", providerName))
	}
	return provider, nil
}

func configuredModelDriver(provider *modelModule.Provider, apiKey, region, baseURL string) (modelModule.ModelDriver, *modelModule.APIConfig, error) {
	if provider == nil || provider.ModelDriver == nil {
		return nil, nil, common.NewCodedError(common.CodeNotFound, "Model provider driver is unavailable")
	}
	apiKey = strings.TrimSpace(apiKey)
	region = strings.TrimSpace(region)
	baseURL = strings.TrimSpace(baseURL)
	if region == "" {
		region = "default"
	}
	driver := provider.ModelDriver
	if baseURL != "" {
		var err error
		driver, err = newModelDriverForBaseURL(provider.ModelDriver, provider.Name, region, baseURL)
		if err != nil {
			return nil, nil, common.NewCodedError(common.CodeBadRequest, err.Error())
		}
	}
	apiConfig := &modelModule.APIConfig{ApiKey: &apiKey, Region: &region, BaseURL: &baseURL}
	return driver, apiConfig, nil
}

func systemProviderResponse(provider *entity.TenantModelProvider) map[string]interface{} {
	return map[string]interface{}{
		"id":            provider.ID,
		"provider_id":   provider.ID,
		"provider_name": provider.ProviderName,
		"name":          provider.ProviderName,
	}
}

func normalizedNames(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func lowerNames(values []string) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = strings.ToLower(value)
	}
	return result
}

func modelIDs(models []entity.TenantModel) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}

func deleteRoleDefaultModels(tx *gorm.DB, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if err := tx.Where("model_id IN ?", ids).Delete(&entity.RoleDefaultModel{}).Error; err != nil {
		return fmt.Errorf("failed to clear deleted models from role defaults: %w", err)
	}
	return nil
}

func decodeModelExtra(raw string) (map[string]interface{}, error) {
	extra := make(map[string]interface{})
	if strings.TrimSpace(raw) == "" {
		return extra, nil
	}
	if err := json.Unmarshal([]byte(raw), &extra); err != nil {
		return nil, err
	}
	return extra, nil
}

func stringSetting(values map[string]interface{}, key string) string {
	value, _ := values[key].(string)
	return value
}

func catalogModelContextLength(model *modelModule.Model) int {
	if model == nil {
		return 0
	}
	if model.ContextLength != nil {
		return *model.ContextLength
	}
	if model.MaxTokens != nil {
		return *model.MaxTokens
	}
	return 0
}

func numberSetting(value interface{}) int {
	switch value := value.(type) {
	case float64:
		return int(value)
	case int:
		return value
	case int64:
		return int(value)
	case json.Number:
		parsed, _ := value.Int64()
		return int(parsed)
	default:
		return 0
	}
}

func truthy(value interface{}) bool {
	switch value := value.(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(value, "true") || value == "1"
	case float64:
		return value != 0
	case int:
		return value != 0
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
