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
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/entity"
)

const testModelGroupID = "11111111111111111111111111111111"

func TestModelGroupReferenceAttributesModelUsage(t *testing.T) {
	db := setupModelProviderServiceTestDB(t)
	useModelProviderServiceTestDB(t, db)
	seedModelProviderServiceScope(t, db)
	if err := db.AutoMigrate(&entity.TenantModelGroup{}, &entity.TenantModelGroupMap{}); err != nil {
		t.Fatalf("failed to migrate model group tables: %v", err)
	}
	if err := db.Create(&entity.TenantModelGroup{
		ID:        testModelGroupID,
		UserID:    "user-1",
		TenantID:  "user-1",
		GroupName: "failover",
		Status:    "active",
	}).Error; err != nil {
		t.Fatalf("failed to seed model group: %v", err)
	}
	if err := db.Create(&entity.TenantModelGroupMap{
		ID:       "group-map-1",
		GroupID:  testModelGroupID,
		ModelID:  "model-1",
		Position: 1,
		Status:   entity.ModelGroupMapStatusActive,
	}).Error; err != nil {
		t.Fatalf("failed to seed model group member: %v", err)
	}

	ctx := serviceTestContext(t, "user-1", "user-1")
	factory := NewModelFactory()

	groupModel, err := factory.NewChatModel(ctx, common.GroupModelRefPrefix+testModelGroupID)
	if err != nil {
		t.Fatalf("group reference: %v", err)
	}

	// The construction-time usage template carries the group identity, so the
	// nil-usage call paths (TenantStreamAdapter.ChatStream and friends) record
	// which gp- group mediated the call even though they pass nil.
	groupInfo := groupModel.Info()
	groupTemplate := groupInfo.UsageTemplate
	if groupTemplate == nil {
		t.Fatal("group model has no usage template")
	}
	if groupTemplate.GroupID != testModelGroupID || groupTemplate.GroupName != "failover" {
		t.Fatalf("group template attribution = %q/%q, want %q/failover", groupTemplate.GroupID, groupTemplate.GroupName, testModelGroupID)
	}
	if groupTemplate.TenantID != "user-1" || groupTemplate.UserID != "user-1" {
		t.Fatalf("group template identity = tenant %q user %q, want user-1", groupTemplate.TenantID, groupTemplate.UserID)
	}

	// The resolved model identity is recorded on the model info, so the usage
	// resolver tops up explicit usage records with the group identity.
	if groupInfo.GroupID != testModelGroupID || groupInfo.GroupName != "failover" {
		t.Fatalf("group info attribution = %q/%q, want %q/failover", groupInfo.GroupID, groupInfo.GroupName, testModelGroupID)
	}
	if groupInfo.Name != "gpt-test" || groupInfo.InstanceID != "instance-1" {
		t.Fatalf("picked model = %q/%q, want gpt-test/instance-1", groupInfo.Name, groupInfo.InstanceID)
	}

	directModel, err := factory.NewChatModel(ctx, "model-1")
	if err != nil {
		t.Fatalf("direct reference: %v", err)
	}
	directInfo := directModel.Info()
	directTemplate := directInfo.UsageTemplate
	if directTemplate == nil {
		t.Fatal("direct model has no usage template")
	}
	if directTemplate.GroupID != "" || directTemplate.GroupName != "" {
		t.Fatalf("direct template attribution = %q/%q, want empty", directTemplate.GroupID, directTemplate.GroupName)
	}
	if directTemplate.TenantID != "user-1" {
		t.Fatalf("direct template tenant = %q, want user-1", directTemplate.TenantID)
	}
	if directInfo.GroupID != "" || directInfo.GroupName != "" {
		t.Fatalf("direct info attribution = %q/%q, want empty", directInfo.GroupID, directInfo.GroupName)
	}
	if directInfo.Name != "gpt-test" || directInfo.InstanceID != "instance-1" {
		t.Fatalf("direct model = %q/%q, want gpt-test/instance-1", directInfo.Name, directInfo.InstanceID)
	}
}
