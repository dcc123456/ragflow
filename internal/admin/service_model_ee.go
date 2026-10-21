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

package admin

import (
	"context"
)

func (s *Service) ListModelProviders(ctx context.Context, available bool) ([]map[string]interface{}, error) {
	return s.modelProviderService.ListSystemModelProviders(ctx, available)
}

func (s *Service) ListSystemAddedModels(ctx context.Context, modelType string) ([]map[string]interface{}, error) {
	return s.modelProviderService.ListSystemAddedModels(ctx, modelType)
}

func (s *Service) AddModelProvider(ctx context.Context, providerName string) (map[string]interface{}, error) {
	return s.modelProviderService.AddSystemModelProvider(ctx, providerName)
}

func (s *Service) DeleteModelProviders(ctx context.Context, providerNames []string) (map[string]interface{}, error) {
	return s.modelProviderService.DeleteSystemModelProviders(ctx, providerNames)
}

func (s *Service) ListModelInstances(ctx context.Context, providerName string) ([]map[string]interface{}, error) {
	return s.modelProviderService.ListSystemModelInstances(ctx, providerName)
}

func (s *Service) ShowProviderInstance(ctx context.Context, providerName, instanceName string) (map[string]interface{}, error) {
	return s.modelProviderService.ShowSystemProviderInstance(ctx, providerName, instanceName)
}

func (s *Service) ShowProviderInstanceBalance(ctx context.Context, providerName, instanceName string) (map[string]interface{}, error) {
	return s.modelProviderService.ShowSystemProviderInstanceBalance(ctx, providerName, instanceName)
}

func (s *Service) CheckInstanceConnection(ctx context.Context, providerName, instanceName string) (map[string]interface{}, error) {
	return s.modelProviderService.CheckSystemInstanceConnection(ctx, providerName, instanceName)
}

func (s *Service) CheckProviderConnection(ctx context.Context, providerName, region, apiKey, baseURL string) (map[string]interface{}, error) {
	return s.modelProviderService.CheckSystemProviderConnection(ctx, providerName, region, apiKey, baseURL)
}

func (s *Service) AlterProviderInstance(ctx context.Context, providerName, instanceName, newInstanceName, newAPIKey string, region, baseURL *string) (map[string]interface{}, error) {
	return s.modelProviderService.AlterSystemProviderInstance(ctx, providerName, instanceName, newInstanceName, newAPIKey, region, baseURL)
}

func (s *Service) AddModelInstance(ctx context.Context, providerName, instanceName string) (map[string]interface{}, error) {
	return s.modelProviderService.AddSystemModelInstance(ctx, providerName, instanceName)
}

func (s *Service) DeleteModelInstances(ctx context.Context, providerName string, instanceNames []string) (map[string]interface{}, error) {
	return s.modelProviderService.DeleteSystemModelInstances(ctx, providerName, instanceNames)
}

func (s *Service) ListInstanceModels(ctx context.Context, providerName, instanceName string, supported bool) ([]map[string]interface{}, error) {
	return s.modelProviderService.ListSystemInstanceModels(ctx, providerName, instanceName, supported)
}

func (s *Service) EnableOrDisableModel(ctx context.Context, providerName, instanceName, modelName, modelID, status string) (map[string]interface{}, error) {
	return s.modelProviderService.EnableOrDisableSystemModel(ctx, providerName, instanceName, modelName, modelID, status)
}

func (s *Service) AddModels(ctx context.Context, providerName, instanceName string, modelNames []string) (map[string]interface{}, error) {
	return s.modelProviderService.AddSystemModels(ctx, providerName, instanceName, modelNames)
}

func (s *Service) DeleteModels(ctx context.Context, providerName, instanceName string, modelNames []string) (map[string]interface{}, error) {
	return s.modelProviderService.DeleteSystemModels(ctx, providerName, instanceName, modelNames)
}
