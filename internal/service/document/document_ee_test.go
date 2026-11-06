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

package document

import (
	"context"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/service"
	"testing"

	"gorm.io/gorm"
)

func documentTestExtraModels() []interface{} {
	return []interface{}{&entity.Role{}, &entity.RoleResource{}, &entity.GroupInfo{}, &entity.GroupMember{}, &entity.Permission{}, &entity.PermissionChangeLog{}}
}

func seedDocumentPreviewMemberAccessEE(t *testing.T, db *gorm.DB) {
	t.Helper()
	memberRoleID := int32(31)
	if err := db.Create(&entity.Role{ID: memberRoleID, RoleName: "member", TenantID: sptr("tenant-1")}).Error; err != nil {
		t.Fatalf("insert member role: %v", err)
	}
	if err := db.Create(&entity.RoleResource{RoleID: memberRoleID, ResourceType: entity.RoleResourceTypeDataset, Action: entity.RoleActionAll}).Error; err != nil {
		t.Fatalf("enable member dataset feature: %v", err)
	}
	if err := db.Model(&entity.UserTenant{}).Where("id = ?", "ut-prev").Update("role", "31").Error; err != nil {
		t.Fatalf("assign member role: %v", err)
	}
	memberID := "ut-prev"
	datasetID := "kb-prev"
	if err := db.Create(&entity.Permission{
		ID: "kb-prev-member-read", MemberID: &memberID, TenantID: "tenant-1", ResourceType: "kb",
		ResourceID: &datasetID, Permission: entity.ResourcePermissionRead, Status: sptr("1"),
	}).Error; err != nil {
		t.Fatalf("share dataset with member: %v", err)
	}
}

func assertUploadedDocumentOwnerGrant(t *testing.T, ctx context.Context, documentID string) {
	t.Helper()
	var membership entity.UserTenant
	if err := dao.DB.Where("tenant_id = ? AND user_id = ? AND status = ?",
		"tenant-1", "user-1", string(entity.StatusValid)).Take(&membership).Error; err != nil {
		t.Fatalf("load document owner membership: %v", err)
	}
	var ownerGrant entity.Permission
	if err := dao.DB.Where("tenant_id = ? AND resource_type = ? AND resource_id = ? AND status = ?",
		"tenant-1", "document", documentID, string(entity.StatusValid)).Take(&ownerGrant).Error; err != nil {
		t.Fatalf("load uploaded document owner grant: %v", err)
	}
	if ownerGrant.MemberID == nil || *ownerGrant.MemberID != membership.ID || ownerGrant.Permission != entity.ResourcePermissionOwner {
		t.Fatalf("uploaded document owner grant = %+v, want OWNER for user-1", ownerGrant)
	}
	listed, err := service.NewTenantService().ListResourcePermissions(ctx, "user-1", "tenant-1", service.ResourcePermissionListRequest{
		TenantID: "tenant-1", ResourceType: "document", ResourceIDs: []string{documentID},
	})
	if err != nil {
		t.Fatalf("list uploaded document permissions: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != membership.ID || listed[0].Permissions[documentID] != entity.ResourcePermissionOwner {
		t.Fatalf("uploaded document permissions = %+v, want document OWNER for user-1", listed)
	}
}
