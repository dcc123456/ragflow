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
	"encoding/json"
	"errors"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
)

func TestResolveSavedProviderModelConfigUsePermissionCannotOverrideSavedEndpoint(t *testing.T) {
	db := setupModelProviderServiceTestDB(t)
	useModelProviderServiceTestDB(t, db)
	seedModelProviderServiceScope(t, db)
	activeStatus := string(entity.TenantMembershipStatusJoined)
	if err := db.Model(&entity.TenantModelProvider{}).Where("id = ?", "provider-1").Update("tenant_id", "tenant-1").Error; err != nil {
		t.Fatalf("move provider into test tenant: %v", err)
	}
	if err := db.Model(&entity.TenantModelInstance{}).Where("id = ?", "instance-1").Updates(map[string]interface{}{
		"api_key": "sk-owner-secret",
		"extra":   `{"region":"us-east-1","base_url":"https://stored.example"}`,
	}).Error; err != nil {
		t.Fatalf("update saved instance: %v", err)
	}
	if err := db.Create(&entity.UserTenant{
		ID: "user-tenant-member", UserID: "member-1", TenantID: "tenant-1", Role: string(permission.RoleNormal), Status: &activeStatus,
	}).Error; err != nil {
		t.Fatalf("seed tenant member: %v", err)
	}
	if err := db.Create(&entity.UserTenant{
		ID: "user-tenant-owner", UserID: "user-1", TenantID: "tenant-1", Role: string(permission.RoleOwner), Status: &activeStatus,
	}).Error; err != nil {
		t.Fatalf("seed tenant owner: %v", err)
	}
	service := NewModelProviderService()
	ctx := serviceTestContext(t, "member-1", "tenant-1")
	config, err := service.ResolveSavedProviderModelConfig(ctx, "OpenAI", "default", SavedProviderModelOptions{})
	if err != nil {
		t.Fatalf("ResolveSavedProviderModelConfig(use saved credentials) error = %v, want success", err)
	}
	if config.APIKey != "sk-owner-secret" || config.BaseURL != "https://stored.example" || config.Region != "us-east-1" {
		t.Fatalf("saved model config = %+v, want stored credentials and endpoint", config)
	}

	attackerURL := "https://attacker.example"
	_, err = service.ResolveSavedProviderModelConfig(ctx, "OpenAI", "default", SavedProviderModelOptions{BaseURL: &attackerURL})
	if err == nil || !errors.Is(err, permission.ErrPermissionDenied) {
		t.Fatalf("ResolveSavedProviderModelConfig(use permission with endpoint override) error = %v, want hidden denial", err)
	}

	_, err = service.ResolveSavedProviderModelConfig(ctx, "OpenAI", "default", SavedProviderModelOptions{
		APIKeyConfig: json.RawMessage(`{"paddleocr_api_url":"https://attacker.example"}`),
	})
	if err == nil || !errors.Is(err, permission.ErrPermissionDenied) {
		t.Fatalf("ResolveSavedProviderModelConfig(use permission with provider endpoint override) error = %v, want hidden denial", err)
	}

	ownerCtx := serviceTestContext(t, "user-1", "tenant-1")
	ownerInstance, code, err := service.ShowProviderInstance(ownerCtx, "OpenAI", "default")
	if err != nil || code != common.CodeSuccess || ownerInstance["is_owner"] != true {
		t.Fatalf("ShowProviderInstance(owner) = (%v, %v, %v), want owner metadata", ownerInstance, code, err)
	}
	ownerURL := "https://owner-override.example"
	ownerRegion := "eu-west-1"
	ownerConfig, err := service.ResolveSavedProviderModelConfig(ownerCtx, "OpenAI", "default", SavedProviderModelOptions{
		APIKeyConfig: json.RawMessage(`{"group_id":"owner-group"}`),
		BaseURL:      &ownerURL,
		Region:       &ownerRegion,
	})
	if err != nil {
		t.Fatalf("ResolveSavedProviderModelConfig(owner overrides) error = %v, want success", err)
	}
	if ownerConfig.BaseURL != ownerURL || ownerConfig.Region != ownerRegion || ownerConfig.APIKey != `{"api_key":"sk-owner-secret","group_id":"owner-group"}` {
		t.Fatalf("owner override config = %+v; want stored key, owner config and endpoint overrides", ownerConfig)
	}
}
