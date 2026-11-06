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

package file

import (
	"errors"
	"testing"

	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"
	"ragflow/internal/permission"
)

func TestTenantFileListingHidesUnsharedEntriesBeforePagination(t *testing.T) {
	db := testutil.SetupTestDB(t,
		&entity.User{}, &entity.UserTenant{}, &entity.Role{}, &entity.RoleResource{},
		&entity.GroupInfo{}, &entity.GroupMember{}, &entity.Permission{},
		&entity.File{}, &entity.File2Document{}, &entity.Document{}, &entity.Knowledgebase{},
	)
	t.Cleanup(testutil.ReplaceDBForTest(t, db))
	status := string(entity.StatusValid)
	tenantID := "tenant-files"
	for _, userID := range []string{"owner", "reader"} {
		if err := db.Create(&entity.User{ID: userID, Email: userID + "@example.test", Nickname: userID}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&entity.Role{ID: 17, RoleName: "member", TenantID: &tenantID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.RoleResource{RoleID: 17, ResourceType: entity.RoleResourceTypeFile, Action: entity.RoleActionAll}).Error; err != nil {
		t.Fatal(err)
	}
	for _, member := range []entity.UserTenant{
		{ID: "owner-member", UserID: "owner", TenantID: tenantID, Role: "owner", Status: &status},
		{ID: "reader-member", UserID: "reader", TenantID: tenantID, Role: "17", Status: &status},
	} {
		if err := db.Create(&member).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []entity.File{
		{ID: "root", ParentID: "root", TenantID: tenantID, CreatedBy: "owner", Name: "/", Type: FileTypeFolder},
		{ID: "shared-a", ParentID: "root", TenantID: tenantID, CreatedBy: "owner", Name: "a", Type: FileTypeFolder},
		{ID: "private-b", ParentID: "root", TenantID: tenantID, CreatedBy: "owner", Name: "b", Type: FileTypeFolder},
		{ID: "shared-c", ParentID: "root", TenantID: tenantID, CreatedBy: "owner", Name: "c", Type: FileTypeFolder},
	} {
		if err := db.Create(&file).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"shared-a", "shared-c"} {
		resourceID := id
		memberID := "reader-member"
		if err := db.Create(&entity.Permission{
			ID: "grant-" + id, MemberID: &memberID, TenantID: tenantID,
			ResourceType: "file", ResourceID: &resourceID,
			Permission: entity.ResourcePermissionRead, Status: &status,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}

	svc := testFileService()
	ctx := permission.WithIdentity(t.Context(), "reader", tenantID)
	first, err := svc.ListFilesForTenant(ctx, tenantID, "root", 1, 1, []dao.OrderTerm{{Column: "name"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 2 || len(first.Files) != 1 || first.Files[0]["id"] != "shared-a" {
		t.Fatalf("first page = %+v, want first shared entry and total 2", first)
	}
	second, err := svc.ListFilesForTenant(ctx, tenantID, "root", 2, 1, []dao.OrderTerm{{Column: "name"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if second.Total != 2 || len(second.Files) != 1 || second.Files[0]["id"] != "shared-c" {
		t.Fatalf("second page = %+v, want second shared entry and total 2", second)
	}
	if _, err := svc.GetAllParentFoldersForTenant(ctx, tenantID, "private-b"); !errors.Is(err, permission.ErrPermissionDenied) {
		t.Fatalf("private folder path error = %v, want permission denied", err)
	}
	if _, err := svc.GetAllParentFoldersForTenant(ctx, tenantID, "shared-a"); err != nil {
		t.Fatalf("shared folder path: %v", err)
	}
	search, err := svc.ListFilesForTenant(ctx, tenantID, "root", 1, 10, nil, "b")
	if err != nil || search.Total != 0 || len(search.Files) != 0 {
		t.Fatalf("private keyword result = %+v, %v, want no entries", search, err)
	}
	if err := db.Model(&entity.Permission{}).Where("id = ?", "grant-shared-a").Update("status", string(entity.StatusInvalid)).Error; err != nil {
		t.Fatal(err)
	}
	afterRevoke, err := svc.ListFilesForTenant(ctx, tenantID, "root", 1, 10, nil, "")
	if err != nil || afterRevoke.Total != 1 || len(afterRevoke.Files) != 1 || afterRevoke.Files[0]["id"] != "shared-c" {
		t.Fatalf("listing after revocation = %+v, %v, want only remaining shared folder", afterRevoke, err)
	}
	if _, err := svc.GetAllParentFoldersForTenant(ctx, tenantID, "shared-a"); !errors.Is(err, permission.ErrPermissionDenied) {
		t.Fatalf("revoked folder path error = %v, want permission denied", err)
	}
	groupID := "group-files"
	if err := db.Create(&entity.GroupInfo{ID: groupID, OwnerID: "owner-member", TenantID: tenantID, Status: &status}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.GroupMember{ID: "reader-group", MemberID: "reader-member", GroupID: groupID, Status: &status}).Error; err != nil {
		t.Fatal(err)
	}
	privateID := "private-b"
	if err := db.Create(&entity.Permission{
		ID: "group-private-b", GroupID: &groupID, TenantID: tenantID,
		ResourceType: "file", ResourceID: &privateID,
		Permission: entity.ResourcePermissionRead, Status: &status,
	}).Error; err != nil {
		t.Fatal(err)
	}
	viaGroup, err := svc.ListFilesForTenant(ctx, tenantID, "root", 1, 10, nil, "")
	if err != nil || viaGroup.Total != 2 {
		t.Fatalf("listing with group grant = %+v, %v, want 2 entries", viaGroup, err)
	}
	if _, err := svc.GetAllParentFoldersForTenant(ctx, tenantID, privateID); err != nil {
		t.Fatalf("group-shared folder path: %v", err)
	}
	if err := db.Model(&entity.GroupMember{}).Where("id = ?", "reader-group").Update("status", entity.GroupMemberStatusRemoved).Error; err != nil {
		t.Fatal(err)
	}
	afterGroupRemoval, err := svc.ListFilesForTenant(ctx, tenantID, "root", 1, 10, nil, "")
	if err != nil || afterGroupRemoval.Total != 1 {
		t.Fatalf("listing after group removal = %+v, %v, want 1 entry", afterGroupRemoval, err)
	}
}
