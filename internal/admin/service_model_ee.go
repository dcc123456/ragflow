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

package admin

import (
	"context"

	servicepkg "ragflow/internal/service"
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

func (s *Service) CheckProviderConnection(ctx context.Context, providerName string, request *servicepkg.CheckConnectionRequest) (map[string]interface{}, error) {
	return s.modelProviderService.CheckSystemProviderConnection(ctx, providerName, request)
}

func (s *Service) AlterProviderInstance(ctx context.Context, providerName, instanceName string, request *servicepkg.AlterProviderInstanceRequest) (map[string]interface{}, error) {
	return s.modelProviderService.AlterSystemProviderInstance(ctx, providerName, instanceName, request)
}

func (s *Service) AddModelInstance(ctx context.Context, providerName string, request *servicepkg.CreateProviderInstanceRequest) (map[string]interface{}, error) {
	return s.modelProviderService.AddSystemModelInstance(ctx, providerName, request)
}

func (s *Service) DeleteModelInstances(ctx context.Context, providerName string, instanceNames []string) (map[string]interface{}, error) {
	return s.modelProviderService.DeleteSystemModelInstances(ctx, providerName, instanceNames)
}

func (s *Service) ListInstanceModels(ctx context.Context, providerName, instanceName string, supported bool) ([]map[string]interface{}, error) {
	return s.modelProviderService.ListSystemInstanceModels(ctx, providerName, instanceName, supported)
}

func (s *Service) AlterModel(ctx context.Context, providerName, instanceName, modelName string, request *servicepkg.AlterModelRequest) (map[string]interface{}, error) {
	return s.modelProviderService.AlterSystemModel(ctx, providerName, instanceName, modelName, request)
}

func (s *Service) AddModel(ctx context.Context, request *servicepkg.AddModelRequest) error {
	return s.modelProviderService.AddSystemModel(ctx, request)
}

func (s *Service) DeleteModels(ctx context.Context, providerName, instanceName string, modelNames []string) (map[string]interface{}, error) {
	return s.modelProviderService.DeleteSystemModels(ctx, providerName, instanceName, modelNames)
}
