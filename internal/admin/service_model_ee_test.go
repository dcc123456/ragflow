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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"

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

	instanceResult, err := service.AddModelInstance(t.Context(), systemProvider.ProviderName, "default")
	if err != nil {
		t.Fatalf("AddModelInstance() error = %v", err)
	}
	instanceID := instanceResult["id"].(string)
	region := "us-test-1"
	baseURL := "https://models.example.test/v1"
	instanceResult, err = service.AlterProviderInstance(t.Context(), systemProvider.ProviderName, "default", "", "system-api-key", &region, &baseURL)
	if err != nil {
		t.Fatalf("AlterProviderInstance() error = %v", err)
	}
	if instanceResult["region"] != region || instanceResult["base_url"] != baseURL || instanceResult["api_key"] != "system-api-key" {
		t.Fatalf("AlterProviderInstance() = %#v, want updated API key, region and base URL", instanceResult)
	}
	if _, err := service.AddModels(t.Context(), systemProvider.ProviderName, "default", []string{catalogModelName}); err != nil {
		t.Fatalf("AddModels() error = %v", err)
	}
	if _, err := service.AddModels(t.Context(), systemProvider.ProviderName, "default", []string{catalogModelName}); err != nil {
		t.Fatalf("AddModels() retry error = %v", err)
	}

	models, err := service.ListInstanceModels(t.Context(), systemProvider.ProviderName, "default", false)
	if err != nil {
		t.Fatalf("ListInstanceModels() error = %v", err)
	}
	if len(models) != 1 || models[0]["name"] != catalogModelName || models[0]["id"] == "" {
		t.Fatalf("ListInstanceModels() = %#v, want the configured catalog model with an ID", models)
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

	if _, err := service.EnableOrDisableModel(t.Context(), systemProvider.ProviderName, "default", catalogModelName, modelID, "inactive"); err != nil {
		t.Fatalf("EnableOrDisableModel() error = %v", err)
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
	if _, err := service.AddModels(t.Context(), systemProvider.ProviderName, "default", []string{catalogModelName}); err != nil {
		t.Fatalf("AddModels() after deleting model error = %v", err)
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

	if _, err := service.AddModelInstance(t.Context(), systemProvider.ProviderName, "second"); err != nil {
		t.Fatalf("AddModelInstance(second) error = %v", err)
	}
	if _, err := service.AddModels(t.Context(), systemProvider.ProviderName, "second", []string{catalogModelName}); err != nil {
		t.Fatalf("AddModels(second) error = %v", err)
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
