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
	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission/testsupport"
	"ragflow/internal/service"
	"strings"
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
	grantDocumentTestResource(t, db, "tenant-1", "document", "doc-prev", "user-2", entity.ResourcePermissionRead)
}

func grantDocumentTestResource(t *testing.T, db *gorm.DB, tenantID, resourceType, resourceID, userID string, mask int32) {
	t.Helper()
	var membership entity.UserTenant
	if err := db.Where("tenant_id = ? AND user_id = ? AND status = ?", tenantID, userID, string(entity.StatusValid)).Take(&membership).Error; err != nil {
		t.Fatalf("load %s membership for resource grant: %v", userID, err)
	}
	memberID, resourceIDValue, status := membership.ID, resourceID, string(entity.StatusValid)
	if err := db.Create(&entity.Permission{
		ID: common.GenerateToken(), MemberID: &memberID, TenantID: tenantID,
		ResourceType: resourceType, ResourceID: &resourceIDValue, Permission: mask, Status: &status,
	}).Error; err != nil {
		t.Fatalf("grant %s access to %s %s: %v", userID, resourceType, resourceID, err)
	}
}

func grantDocumentTestResourceReadWrite(t *testing.T, db *gorm.DB, tenantID, resourceType, resourceID, userID string) {
	t.Helper()
	grantDocumentTestResource(t, db, tenantID, resourceType, resourceID, userID,
		entity.ResourcePermissionRead|entity.ResourcePermissionWrite)
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

func TestBatchUpdateDocumentStatusRetainsPartialFailure(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	insertTestKB(t, "kb-1", "tenant-1", 1, 0, 0)
	if err := db.Model(&entity.Knowledgebase{}).Where("id = ?", "kb-1").Update("permission", string(entity.TenantPermissionMe)).Error; err != nil {
		t.Fatalf("make dataset private: %v", err)
	}
	insertTestDoc(t, "owner-doc", "kb-1", 0, 0)
	insertTestDoc(t, "member-doc", "kb-1", 0, 0)
	if err := db.Model(&entity.Document{}).Where("id = ?", "member-doc").Updates(map[string]interface{}{"created_by": "member", "user_id": "member"}).Error; err != nil {
		t.Fatalf("make one document member-owned: %v", err)
	}
	valid := string(entity.StatusValid)
	if err := db.Create(&entity.UserTenant{
		ID: "member-tenant-1", UserID: "member", TenantID: "tenant-1", Role: "normal", Status: &valid,
	}).Error; err != nil {
		t.Fatalf("create tenant member: %v", err)
	}
	if err := testsupport.PrepareEnterprisePermissionFixture(db, "tenant-1", "user-1", "member"); err != nil {
		t.Fatalf("prepare document status permission fixture: %v", err)
	}
	grantDocumentTestResourceReadWrite(t, db, "tenant-1", "kb", "kb-1", "member")
	grantDocumentTestResourceReadWrite(t, db, "tenant-1", "document", "member-doc", "member")

	svc := testDocumentService(t)
	ctx := documentTestActorContext(t.Context(), "member", "tenant-1")
	_, code, err := svc.BatchUpdateDocumentStatus(ctx, "kb-1", "1", []string{"member-doc", "owner-doc"})
	if err == nil {
		t.Fatal("expected a partial failure")
	}
	if code != common.CodeServerError || !strings.Contains(err.Error(), "partial failure:") || !strings.Contains(err.Error(), "owner-doc") || !strings.Contains(err.Error(), "Permission denied") {
		t.Fatalf("result code/error = %d/%v, want partial failure", code, err)
	}
}
