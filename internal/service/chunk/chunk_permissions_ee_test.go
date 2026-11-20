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

package chunk

import (
	"context"
	"errors"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"

	"gorm.io/gorm"
)

func grantChunkDocumentPermission(t *testing.T, db *gorm.DB, tenantID, userID, documentID string, mask int32) {
	t.Helper()

	var membership entity.UserTenant
	if err := db.Where("tenant_id = ? AND user_id = ? AND status = ?", tenantID, userID, string(entity.StatusValid)).Take(&membership).Error; err != nil {
		t.Fatalf("load %s membership for document permission: %v", userID, err)
	}
	memberID, resourceID, status := membership.ID, documentID, string(entity.StatusValid)
	if err := db.Create(&entity.Permission{
		ID: common.GenerateToken(), MemberID: &memberID, TenantID: tenantID,
		ResourceType: "document", ResourceID: &resourceID, Permission: mask, Status: &status,
	}).Error; err != nil {
		t.Fatalf("grant %s access to document %s: %v", userID, documentID, err)
	}
}

func grantChunkDocumentReadWrite(t *testing.T, db *gorm.DB, tenantID, userID, documentID string) {
	t.Helper()
	grantChunkDocumentPermission(t, db, tenantID, userID, documentID,
		entity.ResourcePermissionRead|entity.ResourcePermissionWrite)
}

func chunkVersionManageContext(t *testing.T, tenantID, userID, documentID string) context.Context {
	t.Helper()
	ctx := chunkTestContext(t, userID, tenantID)
	var membership entity.UserTenant
	if err := dao.DB.Where("tenant_id = ? AND user_id = ? AND status = ?", tenantID, userID, string(entity.StatusValid)).Take(&membership).Error; err != nil {
		t.Fatalf("load %s membership for version permissions: %v", userID, err)
	}
	var grant entity.Permission
	err := dao.DB.Where("tenant_id = ? AND resource_type = ? AND resource_id = ? AND member_id = ? AND (group_id IS NULL OR group_id = '')",
		tenantID, "document", documentID, membership.ID).Take(&grant).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		memberID, id, status := membership.ID, documentID, string(entity.StatusValid)
		if err := dao.DB.Create(&entity.Permission{
			ID: common.GenerateToken(), MemberID: &memberID, TenantID: tenantID,
			ResourceType: "document", ResourceID: &id,
			Permission: entity.ResourcePermissionManage, Status: &status,
		}).Error; err != nil {
			t.Fatalf("grant manage access to document %s: %v", documentID, err)
		}
	} else if err != nil {
		t.Fatalf("load document permission for version test: %v", err)
	} else if err := dao.DB.Model(&grant).Update("permission", entity.ResourcePermissionManage).Error; err != nil {
		t.Fatalf("grant manage access to document %s: %v", documentID, err)
	}
	return ctx
}

func grantChunkTeamDocumentPermissions(t *testing.T, db *gorm.DB, tenantID, userID string) {
	t.Helper()

	var membership entity.UserTenant
	if err := db.Where("tenant_id = ? AND user_id = ? AND status = ?", tenantID, userID, string(entity.StatusValid)).Take(&membership).Error; err != nil {
		t.Fatalf("load %s membership for team document permissions: %v", userID, err)
	}
	var datasets []entity.Knowledgebase
	if err := db.Where("tenant_id = ? AND permission = ?", tenantID, string(entity.TenantPermissionTeam)).Find(&datasets).Error; err != nil {
		t.Fatalf("load team datasets for document permissions: %v", err)
	}
	for _, dataset := range datasets {
		var documents []entity.Document
		if err := db.Where("kb_id = ?", dataset.ID).Find(&documents).Error; err != nil {
			t.Fatalf("load documents for team dataset %s: %v", dataset.ID, err)
		}
		for _, document := range documents {
			ownerUserID := document.UserID
			if ownerUserID == "" {
				ownerUserID = document.CreatedBy
			}
			if ownerUserID == userID {
				continue
			}
			var existing entity.Permission
			err := db.Where("tenant_id = ? AND resource_type = ? AND resource_id = ? AND member_id = ? AND (group_id IS NULL OR group_id = '')",
				tenantID, "document", document.ID, membership.ID).Take(&existing).Error
			if err == nil {
				continue
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("load team document permission: %v", err)
			}
			grantChunkDocumentPermission(t, db, tenantID, userID, document.ID,
				entity.ResourcePermissionRead|entity.ResourcePermissionWrite)
		}
	}
}
