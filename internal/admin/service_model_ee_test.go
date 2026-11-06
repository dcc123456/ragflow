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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"
	servicepkg "ragflow/internal/service"

	"github.com/gin-gonic/gin"
)

func TestSystemModelProviderCRUDUsesOnlySystemScope(t *testing.T) {
	db := testutil.SetupTestDB(t,
		&entity.TenantModelProvider{},
		&entity.TenantModelInstance{},
		&entity.TenantModel{},
		&entity.RoleDefaultModel{},
	)
	t.Cleanup(testutil.ReplaceDBForTest(t, db))

	manager := dao.GetModelProviderManager()
	var catalogProviderName string
	var catalogModelName string
	for _, candidate := range manager.Providers {
		if len(candidate.Models) == 0 {
			continue
		}
		catalogProviderName = candidate.Name
		catalogModelName = candidate.Models[0].Name
		break
	}
	if catalogProviderName == "" || catalogModelName == "" {
		t.Fatal("model provider catalog has no provider with models")
	}

	service := NewService()
	providerResult, err := service.AddModelProvider(t.Context(), catalogProviderName)
	if err != nil {
		t.Fatalf("AddModelProvider() error = %v", err)
	}
	providerID := providerResult["id"].(string)
	var systemProvider entity.TenantModelProvider
	if err := db.Where("id = ?", providerID).Take(&systemProvider).Error; err != nil {
		t.Fatalf("find created system provider: %v", err)
	}
	if systemProvider.UserID != "" || systemProvider.TenantID != "" {
		t.Fatalf("system provider scope = (%q, %q), want empty user_id and tenant_id", systemProvider.UserID, systemProvider.TenantID)
	}

	if err := db.Create(&entity.TenantModelProvider{
		ID: "tenant-provider", UserID: "tenant-user", TenantID: "tenant-1", ProviderName: systemProvider.ProviderName,
	}).Error; err != nil {
		t.Fatalf("create tenant provider with same name: %v", err)
	}

	configuredProviders, err := service.ListModelProviders(t.Context(), false)
	if err != nil {
		t.Fatalf("ListModelProviders(configured) error = %v", err)
	}
	if len(configuredProviders) != 1 || configuredProviders[0]["id"] != providerID || configuredProviders[0]["has_instance"] != false {
		t.Fatalf("ListModelProviders(configured) = %#v, want only the system provider with no instances", configuredProviders)
	}
	availableProviders, err := service.ListModelProviders(t.Context(), true)
	if err != nil || len(availableProviders) == 0 {
		t.Fatalf("ListModelProviders(available) = (%#v, %v), want catalog providers", availableProviders, err)
	}

	instanceResult, err := service.AddModelInstance(t.Context(), systemProvider.ProviderName, &servicepkg.CreateProviderInstanceRequest{InstanceName: "default"})
	if err != nil {
		t.Fatalf("AddModelInstance() error = %v", err)
	}
	instanceID := instanceResult["id"].(string)
	region := "us-test-1"
	baseURL := "https://models.example.test/v1"
	modelInfo := []servicepkg.CreateInstanceModelInfo{}
	instanceResult, err = service.AlterProviderInstance(t.Context(), systemProvider.ProviderName, "default", &servicepkg.AlterProviderInstanceRequest{
		APIKey:    json.RawMessage(`"system-api-key"`),
		Region:    region,
		BaseURL:   baseURL,
		ModelInfo: &modelInfo,
	})
	if err != nil {
		t.Fatalf("AlterProviderInstance() error = %v", err)
	}
	if instanceResult["region"] != region || instanceResult["base_url"] != baseURL || instanceResult["api_key"] != "system-api-key" {
		t.Fatalf("AlterProviderInstance() = %#v, want updated API key, region and base URL", instanceResult)
	}
	modelRequest := &servicepkg.AddModelRequest{
		ProviderName: systemProvider.ProviderName,
		InstanceName: "default",
		ModelName:    catalogModelName,
		ModelTypes:   []string{"chat"},
		MaxTokens:    1234,
		Extra:        map[string]interface{}{"is_tools": true},
	}
	if err := service.AddModel(t.Context(), modelRequest); err != nil {
		t.Fatalf("AddModel() error = %v", err)
	}
	if err := service.AddModel(t.Context(), modelRequest); err == nil {
		t.Fatal("AddModel() duplicate succeeded, want conflict")
	}

	models, err := service.ListInstanceModels(t.Context(), systemProvider.ProviderName, "default", false)
	if err != nil {
		t.Fatalf("ListInstanceModels() error = %v", err)
	}
	if len(models) != 1 || models[0]["name"] != catalogModelName || models[0]["id"] == "" {
		t.Fatalf("ListInstanceModels() = %#v, want the configured catalog model with an ID", models)
	}
	var storedModel entity.TenantModel
	if err := db.Where("id = ?", models[0]["id"]).Take(&storedModel).Error; err != nil {
		t.Fatalf("find added system model: %v", err)
	}
	if storedModel.ModelType != int(entity.ModelTypeChat) || !strings.Contains(storedModel.Extra, `"max_tokens":1234`) || !strings.Contains(storedModel.Extra, `"is_tools":true`) {
		t.Fatalf("stored system model = %#v, want API request model type and extra fields", storedModel)
	}

	addedModels, err := service.ListSystemAddedModels(t.Context(), "")
	if err != nil {
		t.Fatalf("ListSystemAddedModels() error = %v", err)
	}
	if len(addedModels) != 1 || addedModels[0]["name"] != catalogModelName || addedModels[0]["provider_id"] != providerID || addedModels[0]["instance_id"] != instanceID {
		t.Fatalf("ListSystemAddedModels() = %#v, want the system model with provider and instance IDs", addedModels)
	}
	if addedModels[0]["tenant_id"] != "" || addedModels[0]["tenant_name"] != "" {
		t.Fatalf("ListSystemAddedModels() exposed tenant scope: %#v", addedModels[0])
	}
	if modelTypes, ok := addedModels[0]["model_type"].([]string); !ok || len(modelTypes) == 0 {
		t.Fatalf("ListSystemAddedModels() model_type = %#v, want a non-empty string slice", addedModels[0]["model_type"])
	}
	modelID := models[0]["id"].(string)

	if _, err := service.AlterModel(t.Context(), systemProvider.ProviderName, "default", catalogModelName, &servicepkg.AlterModelRequest{ModelID: modelID, Status: "inactive"}); err != nil {
		t.Fatalf("AlterModel() error = %v", err)
	}
	models, err = service.ListInstanceModels(t.Context(), systemProvider.ProviderName, "default", false)
	if err != nil || models[0]["status"] != "inactive" {
		t.Fatalf("ListInstanceModels() after disabling = (%#v, %v), want inactive model", models, err)
	}
	if _, err := service.DeleteModels(t.Context(), systemProvider.ProviderName, "default", []string{strings.ToUpper(catalogModelName)}); err != nil {
		t.Fatalf("DeleteModels() error = %v", err)
	}
	models, err = service.ListInstanceModels(t.Context(), systemProvider.ProviderName, "default", false)
	if err != nil || len(models) != 0 {
		t.Fatalf("ListInstanceModels() after deleting model = (%#v, %v), want empty list", models, err)
	}
	if err := service.AddModel(t.Context(), modelRequest); err != nil {
		t.Fatalf("AddModel() after deleting model error = %v", err)
	}

	if _, err := service.DeleteModelInstances(t.Context(), systemProvider.ProviderName, []string{"default"}); err != nil {
		t.Fatalf("DeleteModelInstances() error = %v", err)
	}
	var remainingModels int64
	if err := db.Model(&entity.TenantModel{}).Where("provider_id = ?", providerID).Count(&remainingModels).Error; err != nil {
		t.Fatalf("count models after deleting instance: %v", err)
	}
	if remainingModels != 0 {
		t.Fatalf("models remaining after deleting instance = %d, want 0", remainingModels)
	}
	var remainingInstances int64
	if err := db.Model(&entity.TenantModelInstance{}).Where("id = ?", instanceID).Count(&remainingInstances).Error; err != nil {
		t.Fatalf("count deleted instance: %v", err)
	}
	if remainingInstances != 0 {
		t.Fatalf("instance rows remaining = %d, want 0", remainingInstances)
	}

	if _, err := service.AddModelInstance(t.Context(), systemProvider.ProviderName, &servicepkg.CreateProviderInstanceRequest{InstanceName: "second"}); err != nil {
		t.Fatalf("AddModelInstance(second) error = %v", err)
	}
	secondModelRequest := *modelRequest
	secondModelRequest.InstanceName = "second"
	if err := service.AddModel(t.Context(), &secondModelRequest); err != nil {
		t.Fatalf("AddModel(second) error = %v", err)
	}

	if _, err := service.DeleteModelProviders(t.Context(), []string{systemProvider.ProviderName}); err != nil {
		t.Fatalf("DeleteModelProviders() error = %v", err)
	}
	var remainingSystemProviders int64
	if err := db.Model(&entity.TenantModelProvider{}).Where("id = ?", providerID).Count(&remainingSystemProviders).Error; err != nil {
		t.Fatalf("count deleted system provider: %v", err)
	}
	if remainingSystemProviders != 0 {
		t.Fatalf("system providers remaining = %d, want 0", remainingSystemProviders)
	}
	if err := db.Model(&entity.TenantModelInstance{}).Where("provider_id = ?", providerID).Count(&remainingInstances).Error; err != nil {
		t.Fatalf("count instances after deleting provider: %v", err)
	}
	if remainingInstances != 0 {
		t.Fatalf("instances remaining after deleting provider = %d, want 0", remainingInstances)
	}
	if err := db.Model(&entity.TenantModel{}).Where("provider_id = ?", providerID).Count(&remainingModels).Error; err != nil {
		t.Fatalf("count models after deleting provider: %v", err)
	}
	if remainingModels != 0 {
		t.Fatalf("models remaining after deleting provider = %d, want 0", remainingModels)
	}
	var remainingTenantProviders int64
	if err := db.Model(&entity.TenantModelProvider{}).Where("id = ?", "tenant-provider").Count(&remainingTenantProviders).Error; err != nil {
		t.Fatalf("count tenant provider: %v", err)
	}
	if remainingTenantProviders != 1 {
		t.Fatalf("tenant provider rows remaining = %d, want 1", remainingTenantProviders)
	}
}

func TestListSystemAddedModelsRoute(t *testing.T) {
	db := testutil.SetupTestDB(t,
		&entity.TenantModelProvider{},
		&entity.TenantModelInstance{},
		&entity.TenantModel{},
	)
	t.Cleanup(testutil.ReplaceDBForTest(t, db))

	provider := entity.TenantModelProvider{ID: "system-provider", ProviderName: "OpenAI"}
	instance := entity.TenantModelInstance{ID: "system-instance", ProviderID: provider.ID, InstanceName: "default"}
	model := entity.TenantModel{ID: "system-model", ProviderID: provider.ID, InstanceID: instance.ID, ModelName: "gpt-test", ModelType: int(entity.ModelTypeChat), Status: "active"}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatalf("create system provider: %v", err)
	}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatalf("create system instance: %v", err)
	}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create system model: %v", err)
	}

	originalMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(originalMode) })

	engine := gin.New()
	RegisterEERouter(engine.Group("/api/v1/admin"), NewRouter(NewHandler(NewService())))

	addResponse := httptest.NewRecorder()
	addRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/providers/OpenAI/instances/default/models",
		strings.NewReader(`{"provider_name":"OpenAI","instance_name":"default","model_name":"tt","model_type":["chat"],"max_tokens":1234,"extra":{"is_tools":true}}`),
	)
	addRequest.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(addResponse, addRequest)
	if addResponse.Code != http.StatusOK || !strings.Contains(addResponse.Body.String(), `"code":0`) {
		t.Fatalf("POST /admin/providers/.../models status = %d, body = %s", addResponse.Code, addResponse.Body.String())
	}
	var addedModel entity.TenantModel
	if err := db.Where("provider_id = ? AND instance_id = ? AND model_name = ?", provider.ID, instance.ID, "tt").Take(&addedModel).Error; err != nil {
		t.Fatalf("find model added through admin API: %v", err)
	}
	if addedModel.ModelType != int(entity.ModelTypeChat) || !strings.Contains(addedModel.Extra, `"max_tokens":1234`) || !strings.Contains(addedModel.Extra, `"is_tools":true`) {
		t.Fatalf("model added through admin API = %#v, want API request fields", addedModel)
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/models?type=chat", nil)
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /admin/models status = %d, body = %s", response.Code, response.Body.String())
	}
	for _, expected := range []string{`"model_id":"system-model"`, `"provider_id":"system-provider"`, `"instance_id":"system-instance"`, `"model_type":["chat"]`} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("GET /admin/models response = %s, want %s", response.Body.String(), expected)
		}
	}

	providerCreateRouteRegistered := false
	providerDeleteRouteRegistered := false
	for _, route := range engine.Routes() {
		providerCreateRouteRegistered = providerCreateRouteRegistered ||
			(route.Path == "/api/v1/admin/providers" && route.Method == http.MethodPut)
		providerDeleteRouteRegistered = providerDeleteRouteRegistered ||
			(route.Path == "/api/v1/admin/providers/:provider_name" && route.Method == http.MethodDelete)
	}
	if !providerCreateRouteRegistered {
		t.Fatal("PUT /admin/providers was not registered")
	}
	if !providerDeleteRouteRegistered {
		t.Fatal("DELETE /admin/providers/:provider_name was not registered")
	}
}

func TestAdminProviderInstanceAndModelRoutesUseAPIRequestShapes(t *testing.T) {
	db := testutil.SetupTestDB(t,
		&entity.TenantModelProvider{},
		&entity.TenantModelInstance{},
		&entity.TenantModel{},
		&entity.RoleDefaultModel{},
	)
	t.Cleanup(testutil.ReplaceDBForTest(t, db))

	provider := entity.TenantModelProvider{ID: "system-provider", ProviderName: "OpenAI"}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatalf("create system provider: %v", err)
	}

	originalMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(originalMode) })
	engine := gin.New()
	RegisterEERouter(engine.Group("/api/v1/admin"), NewRouter(NewHandler(NewService())))

	post := httptest.NewRecorder()
	postRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/providers/OpenAI/instances", strings.NewReader(
		`{"instance_name":"custom","api_key":"system-key","base_url":"https://models.example.test/v1","region":"us-test-1","model_info":[{"model_name":"custom-model","model_type":["chat"],"max_tokens":512,"extra":{"is_tools":true}}]}`,
	))
	postRequest.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(post, postRequest)
	if post.Code != http.StatusOK || !strings.Contains(post.Body.String(), `"code":0`) {
		t.Fatalf("POST /admin/providers/.../instances status = %d, body = %s", post.Code, post.Body.String())
	}

	var instance entity.TenantModelInstance
	if err := db.Where("provider_id = ? AND instance_name = ?", provider.ID, "custom").Take(&instance).Error; err != nil {
		t.Fatalf("find created system instance: %v", err)
	}
	var model entity.TenantModel
	if err := db.Where("provider_id = ? AND instance_id = ? AND model_name = ?", provider.ID, instance.ID, "custom-model").Take(&model).Error; err != nil {
		t.Fatalf("find model created with instance: %v", err)
	}
	if model.ModelType != int(entity.ModelTypeChat) || !strings.Contains(model.Extra, `"max_tokens":512`) || !strings.Contains(model.Extra, `"is_tools":true`) {
		t.Fatalf("created model = %#v, want model_info fields from API-shaped request", model)
	}

	put := httptest.NewRecorder()
	putRequest := httptest.NewRequest(http.MethodPut, "/api/v1/admin/providers/OpenAI/instances/custom", strings.NewReader(
		`{"instance_name":"renamed","api_key":"updated-key","base_url":"https://models.example.test/v2","region":"us-test-2","model_info":[{"model_name":"custom-model","model_type":["embedding"],"max_tokens":1024,"extra":{"thinking":true}}]}`,
	))
	putRequest.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(put, putRequest)
	if put.Code != http.StatusOK || !strings.Contains(put.Body.String(), `"code":0`) {
		t.Fatalf("PUT /admin/providers/.../instances/:name status = %d, body = %s", put.Code, put.Body.String())
	}
	if err := db.Where("id = ?", instance.ID).Take(&instance).Error; err != nil {
		t.Fatalf("reload updated system instance: %v", err)
	}
	if instance.InstanceName != "renamed" || instance.APIKey != "updated-key" || !strings.Contains(instance.Extra, `"region":"us-test-2"`) {
		t.Fatalf("updated instance = %#v, want API-shaped instance fields", instance)
	}
	if err := db.Where("id = ?", model.ID).Take(&model).Error; err != nil {
		t.Fatalf("reload updated system model: %v", err)
	}
	if model.ModelType != int(entity.ModelTypeEmbedding) || !strings.Contains(model.Extra, `"max_tokens":1024`) || !strings.Contains(model.Extra, `"thinking":true`) {
		t.Fatalf("updated model = %#v, want model_info update fields", model)
	}

	patchModel := httptest.NewRecorder()
	patchRequest := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/providers/OpenAI/instances/renamed/models/custom-model", strings.NewReader(
		`{"model_id":"`+model.ID+`","status":"inactive","model_type":["chat"],"max_tokens":2048,"extra":{"is_tools":false}}`,
	))
	patchRequest.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(patchModel, patchRequest)
	if patchModel.Code != http.StatusOK || !strings.Contains(patchModel.Body.String(), `"code":0`) {
		t.Fatalf("PATCH /admin/providers/.../models/:name status = %d, body = %s", patchModel.Code, patchModel.Body.String())
	}
	if err := db.Where("id = ?", model.ID).Take(&model).Error; err != nil {
		t.Fatalf("reload patched system model: %v", err)
	}
	if model.Status != "inactive" || model.ModelType != int(entity.ModelTypeChat) || !strings.Contains(model.Extra, `"max_tokens":2048`) || !strings.Contains(model.Extra, `"is_tools":false`) {
		t.Fatalf("patched model = %#v, want API PATCH fields", model)
	}

	deleteModel := httptest.NewRecorder()
	deleteModelRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/providers/OpenAI/instances/renamed/models", strings.NewReader(`{"model_name":["custom-model"]}`))
	deleteModelRequest.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(deleteModel, deleteModelRequest)
	if deleteModel.Code != http.StatusOK || !strings.Contains(deleteModel.Body.String(), `"code":0`) {
		t.Fatalf("DELETE /admin/providers/.../models status = %d, body = %s", deleteModel.Code, deleteModel.Body.String())
	}

	deleteInstance := httptest.NewRecorder()
	deleteInstanceRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/providers/OpenAI/instances", strings.NewReader(`{"instances":["renamed"]}`))
	deleteInstanceRequest.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(deleteInstance, deleteInstanceRequest)
	if deleteInstance.Code != http.StatusOK || !strings.Contains(deleteInstance.Body.String(), `"code":0`) {
		t.Fatalf("DELETE /admin/providers/.../instances status = %d, body = %s", deleteInstance.Code, deleteInstance.Body.String())
	}
	var remainingInstances int64
	if err := db.Model(&entity.TenantModelInstance{}).Where("id = ?", instance.ID).Count(&remainingInstances).Error; err != nil {
		t.Fatalf("count deleted system instance: %v", err)
	}
	if remainingInstances != 0 {
		t.Fatalf("system instance rows remaining = %d, want 0", remainingInstances)
	}
}
