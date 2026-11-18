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
	"errors"
	"testing"

	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"
	"ragflow/internal/permission"
)

func TestCheckDocumentAccessRequiresDatasetAndDocumentGrants(t *testing.T) {
	db := testutil.SetupTestDB(t,
		&entity.Tenant{}, &entity.User{}, &entity.UserTenant{}, &entity.Role{}, &entity.RoleResource{},
		&entity.Knowledgebase{}, &entity.Document{}, &entity.Permission{}, &entity.GroupInfo{}, &entity.GroupMember{},
	)
	t.Cleanup(testutil.ReplaceDBForTest(t, db))

	tenantID := "tenant-doc-access"
	valid := string(entity.StatusValid)
	roleID := int32(31)
	if err := db.Create(&entity.Tenant{ID: tenantID, ParserIDs: "", Status: &valid}).Error; err != nil {
		t.Fatalf("create Tenant: %v", err)
	}
	if err := db.Create(&entity.Role{ID: roleID, RoleName: "member", TenantID: &tenantID}).Error; err != nil {
		t.Fatalf("create member role: %v", err)
	}
	if err := db.Create(&entity.RoleResource{RoleID: roleID, ResourceType: entity.RoleResourceTypeDataset, Action: entity.RoleActionAll}).Error; err != nil {
		t.Fatalf("enable dataset feature: %v", err)
	}
	for _, user := range []entity.User{
		{ID: "owner-user", Email: "owner@example.com", Nickname: "Owner"},
		{ID: "member-user", Email: "member@example.com", Nickname: "Member"},
	} {
		if err := db.Create(&user).Error; err != nil {
			t.Fatalf("create user %q: %v", user.ID, err)
		}
	}
	for _, membership := range []entity.UserTenant{
		{ID: "owner-link", UserID: "owner-user", TenantID: tenantID, Role: TenantRoleOwner, Status: &valid},
		{ID: "member-link", UserID: "member-user", TenantID: tenantID, Role: "31", Status: &valid},
	} {
		if err := db.Create(&membership).Error; err != nil {
			t.Fatalf("create membership %q: %v", membership.ID, err)
		}
	}
	kbID := "private-dataset"
	if err := db.Create(&entity.Knowledgebase{
		ID: kbID, TenantID: tenantID, Name: "Private", EmbdID: "", Permission: string(entity.TenantPermissionMe),
		CreatedBy: "owner-user", ParserID: "general", ParserConfig: entity.JSONMap{}, Status: &valid,
	}).Error; err != nil {
		t.Fatalf("create dataset: %v", err)
	}
	if err := db.Create(&entity.Document{
		ID: "shared-document", KbID: kbID, ParserID: "general", ParserConfig: entity.JSONMap{},
		SourceType: "local", Type: "pdf", CreatedBy: "owner-user", Suffix: "pdf", Status: &valid,
	}).Error; err != nil {
		t.Fatalf("create document: %v", err)
	}
	memberID := "member-link"
	docID := "shared-document"
	if err := db.Create(&entity.Permission{
		ID: "document-grant", MemberID: &memberID, TenantID: tenantID, ResourceType: "document",
		ResourceID: &docID, Permission: entity.ResourcePermissionRead, Status: &valid,
	}).Error; err != nil {
		t.Fatalf("share document: %v", err)
	}
	ctx := permission.WithIdentity(t.Context(), "member-user", tenantID)
	if err := CheckDocumentAccess(ctx, docID, kbID, permission.OperationRead); !errors.Is(err, permission.ErrPermissionDenied) {
		t.Fatalf("document grant without dataset grant error = %v, want permission denied", err)
	}

	kbGrantID := kbID
	if err := db.Create(&entity.Permission{
		ID: "dataset-grant", MemberID: &memberID, TenantID: tenantID, ResourceType: "kb",
		ResourceID: &kbGrantID, Permission: entity.ResourcePermissionRead, Status: &valid,
	}).Error; err != nil {
		t.Fatalf("share dataset: %v", err)
	}
	if err := CheckDocumentAccess(ctx, docID, kbID, permission.OperationRead); err != nil {
		t.Fatalf("dataset and document read grants were rejected: %v", err)
	}
	if err := CheckDocumentAccess(ctx, docID, kbID, permission.OperationUpdate); !errors.Is(err, permission.ErrPermissionDenied) {
		t.Fatalf("read-only document grant update error = %v, want permission denied", err)
	}
	if err := db.Delete(&entity.Permission{}, "id = ?", "document-grant").Error; err != nil {
		t.Fatalf("remove document grant: %v", err)
	}
	if err := CheckDocumentAccess(ctx, docID, kbID, permission.OperationRead); !errors.Is(err, permission.ErrPermissionDenied) {
		t.Fatalf("dataset grant without document grant error = %v, want permission denied", err)
	}
}
