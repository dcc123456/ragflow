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

package permission

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"ragflow/internal/entity"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestDocumentReadQueryScopeIncludesDisabledDocuments(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+url.QueryEscape(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&entity.Knowledgebase{}, &entity.Document{}, &entity.UserTenant{}); err != nil {
		t.Fatalf("migrate permission tables: %v", err)
	}

	valid := string(entity.StatusValid)
	disabled := "0"
	ownerID := "owner-1"
	tenantID := "tenant-1"
	dataset := entity.Knowledgebase{
		ID: "dataset-1", UserID: ownerID, TenantID: tenantID, Name: "dataset",
		EmbdID: "embedding", Permission: string(entity.TenantPermissionMe), Status: &valid,
	}
	document := entity.Document{
		ID: "document-1", KbID: dataset.ID, UserID: ownerID, CreatedBy: ownerID,
		Status: &disabled, ParserConfig: entity.JSONMap{},
	}
	membership := entity.UserTenant{
		ID: "owner-membership", UserID: ownerID, TenantID: tenantID,
		Role: string(RoleOwner), Status: &valid,
	}
	if err := db.Create(&dataset).Error; err != nil {
		t.Fatalf("create dataset: %v", err)
	}
	if err := db.Create(&document).Error; err != nil {
		t.Fatalf("create disabled document: %v", err)
	}
	if err := db.Create(&membership).Error; err != nil {
		t.Fatalf("create owner membership: %v", err)
	}
	if err := prepareEnterpriseDatabaseSourceFixture(db, tenantID); err != nil {
		t.Fatalf("prepare Enterprise permission fixture: %v", err)
	}

	checker := NewDatabaseChecker(db)
	scope, err := checker.QueryScope(WithIdentity(context.Background(), ownerID, tenantID), ScopeQuery{
		Kind: ResourceKindDocument, Parent: ResourceRef{Kind: ResourceKindDataset, ID: dataset.ID}, Operation: OperationRead,
	})
	if err != nil {
		t.Fatalf("QueryScope(disabled document): %v", err)
	}
	var ids []string
	if err := scope.Apply(db.Table("document")).Pluck("document.id", &ids).Error; err != nil {
		t.Fatalf("list documents in query scope: %v", err)
	}
	if len(ids) != 1 || ids[0] != document.ID {
		t.Fatalf("QueryScope(disabled document) IDs = %v, want [%s]", ids, document.ID)
	}
}

func TestCheckFeatureUsesTenantRoleAndSystemRoleCeiling(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+url.QueryEscape(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&entity.User{}, &entity.Tenant{}, &entity.UserTenant{}, &entity.Role{}, &entity.RoleResource{}); err != nil {
		t.Fatalf("migrate permission tables: %v", err)
	}

	tenantID := "tenant-1"
	status := string(entity.StatusValid)
	systemRoleID := int32(23)
	if err := db.Create(&entity.Role{ID: systemRoleID, RoleName: "system"}).Error; err != nil {
		t.Fatalf("create system role: %v", err)
	}
	if err := db.Create(&entity.Role{ID: 17, RoleName: "member", TenantID: &tenantID}).Error; err != nil {
		t.Fatalf("create Tenant role: %v", err)
	}
	if err := db.Create(&entity.Tenant{ID: tenantID, ParserIDs: "", RoleID: &systemRoleID, Status: &status}).Error; err != nil {
		t.Fatalf("create Tenant: %v", err)
	}
	if err := db.Create(&entity.User{ID: "user-1", Email: "user@example.com", Nickname: "User"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&entity.UserTenant{
		ID: "membership-1", UserID: "user-1", TenantID: tenantID, Role: "17", Status: &status,
	}).Error; err != nil {
		t.Fatalf("create Tenant membership: %v", err)
	}
	if err := db.Create(&[]entity.RoleResource{
		{RoleID: systemRoleID, ResourceType: entity.RoleResourceTypeDataset, Action: entity.RoleActionEnable | entity.RoleActionWrite},
		{RoleID: 17, ResourceType: entity.RoleResourceTypeDataset, Action: entity.RoleActionEnable | entity.RoleActionWrite},
	}).Error; err != nil {
		t.Fatalf("create feature permissions: %v", err)
	}

	checker := NewDatabaseChecker(db)
	ctx := WithIdentity(context.Background(), "user-1", tenantID)
	if err := checker.CheckFeature(ctx, "dataset", ActionWrite); err != nil {
		t.Fatalf("CheckFeature with numeric Tenant role = %v, want nil", err)
	}

	if err := db.Model(&entity.RoleResource{}).
		Where("role_id = ? AND resource_type = ?", systemRoleID, entity.RoleResourceTypeDataset).
		Update("action", entity.RoleActionEnable|entity.RoleActionRead).Error; err != nil {
		t.Fatalf("restrict system role ceiling: %v", err)
	}
	if err := checker.CheckFeature(ctx, "dataset", ActionWrite); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("CheckFeature above system role ceiling = %v, want permission denied", err)
	}
}

func TestResolveMCPServerAccessesMatchesSingleResourceResolution(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+url.QueryEscape(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&entity.User{}, &entity.Tenant{}, &entity.UserTenant{}, &entity.MCPServer{}, &entity.Role{}, &entity.RoleResource{}, &entity.GroupInfo{}, &entity.GroupMember{}, &entity.Permission{}); err != nil {
		t.Fatalf("migrate permission tables: %v", err)
	}
	status := string(entity.StatusValid)
	tenantID := "tenant-1"
	if err := db.Create(&entity.Tenant{ID: tenantID, ParserIDs: "", Status: &status}).Error; err != nil {
		t.Fatalf("create Tenant: %v", err)
	}
	if err := db.Create(&entity.User{ID: "owner", Email: "owner@example.test", Nickname: "Owner"}).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}
	if err := db.Create(&entity.User{ID: "member", Email: "member@example.test", Nickname: "Member"}).Error; err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := db.Create(&entity.UserTenant{ID: "owner-membership", UserID: "owner", TenantID: tenantID, Role: "owner", Status: &status}).Error; err != nil {
		t.Fatalf("create owner membership: %v", err)
	}
	if err := db.Create(&entity.Role{ID: 17, RoleName: "member", TenantID: &tenantID}).Error; err != nil {
		t.Fatalf("create tenant role: %v", err)
	}
	if err := db.Create(&entity.RoleResource{RoleID: 17, ResourceType: entity.RoleResourceTypeMCP, Action: entity.RoleActionAll}).Error; err != nil {
		t.Fatalf("create MCP feature permission: %v", err)
	}
	if err := db.Create(&entity.UserTenant{ID: "member-membership", UserID: "member", TenantID: tenantID, Role: "17", Status: &status}).Error; err != nil {
		t.Fatalf("create member membership: %v", err)
	}
	for _, server := range []*entity.MCPServer{
		{ID: "mcp-1", TenantID: tenantID, Name: "one"},
		{ID: "mcp-2", TenantID: tenantID, Name: "two"},
		{ID: "mcp-other-tenant", TenantID: "tenant-2", Name: "other tenant"},
	} {
		if err := db.Create(server).Error; err != nil {
			t.Fatalf("create MCP server %q: %v", server.ID, err)
		}
	}
	resourceID := "mcp-1"
	memberID := "member-membership"
	if err := db.Create(&entity.Permission{
		ID: "mcp-read-grant", TenantID: tenantID, ResourceType: "mcp", ResourceID: &resourceID,
		MemberID: &memberID, Permission: entity.ResourcePermissionRead, Status: &status,
	}).Error; err != nil {
		t.Fatalf("grant MCP read access: %v", err)
	}
	groupID := "group-1"
	if err := db.Create(&entity.GroupInfo{ID: groupID, OwnerID: "owner-membership", TenantID: tenantID, Status: &status}).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := db.Create(&entity.GroupMember{ID: "group-member-1", MemberID: memberID, GroupID: groupID, Status: &status}).Error; err != nil {
		t.Fatalf("add member to group: %v", err)
	}
	groupResourceID := "mcp-2"
	if err := db.Create(&entity.Permission{
		ID: "mcp-group-manage-grant", TenantID: tenantID, ResourceType: "mcp", ResourceID: &groupResourceID,
		GroupID: &groupID, Permission: entity.ResourcePermissionManage, Status: &status,
	}).Error; err != nil {
		t.Fatalf("grant MCP access to group: %v", err)
	}

	checker := NewDatabaseChecker(db)
	refs := []ResourceRef{
		{Kind: ResourceKindMCPServer, ID: "mcp-1"},
		{Kind: ResourceKindMCPServer, ID: "mcp-2"},
	}
	for _, userID := range []string{"owner", "member"} {
		ctx := WithIdentity(context.Background(), userID, tenantID)
		batched, err := checker.ResolveAccesses(ctx, refs)
		if err != nil {
			t.Fatalf("ResolveAccesses(MCP servers) for %q: %v", userID, err)
		}
		for _, ref := range refs {
			individual, err := checker.ResolveAccess(ctx, ref)
			if err != nil {
				if _, ok := batched[ref]; ok {
					t.Errorf("batched access for %q unexpectedly grants %q: %#v", userID, ref.ID, batched[ref])
				}
				continue
			}
			got, ok := batched[ref]
			if !ok || got.PermissionMask != individual.PermissionMask || got.Source != individual.Source || len(got.Operations) != len(individual.Operations) {
				t.Errorf("batched access for user %q and resource %q = %#v, individual = %#v", userID, ref.ID, got, individual)
			}
		}
	}
	memberCtx := WithIdentity(context.Background(), "member", tenantID)
	memberAccess := checkerResolveAccessForTest(t, checker, memberCtx, refs[0])
	if memberAccess.PermissionMask != entity.ResourcePermissionRead {
		t.Errorf("shared MCP permission mask = %d, want %d", memberAccess.PermissionMask, entity.ResourcePermissionRead)
	}
	scope, err := checker.QueryScope(memberCtx, ScopeQuery{
		Kind: ResourceKindMCPServer, Operation: OperationRead,
	})
	if err != nil {
		t.Fatalf("QueryScope(MCP servers): %v", err)
	}
	var visibleIDs []string
	if err := scope.Apply(db.Model(&entity.MCPServer{})).Order("mcp_server.id").Pluck("mcp_server.id", &visibleIDs).Error; err != nil {
		t.Fatalf("apply MCP server QueryScope: %v", err)
	}
	if len(visibleIDs) != 2 || visibleIDs[0] != "mcp-1" || visibleIDs[1] != "mcp-2" {
		t.Fatalf("MCP server QueryScope returned %v, want direct and group grants", visibleIDs)
	}
}

func checkerResolveAccessForTest(t *testing.T, checker *Checker, ctx context.Context, ref ResourceRef) Access {
	t.Helper()
	access, err := checker.ResolveAccess(ctx, ref)
	if err != nil {
		t.Fatalf("ResolveAccess(%q): %v", ref.ID, err)
	}
	return access
}

func TestDatabaseCheckerAuthorizesTenantModelsWithInstanceReadGrants(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+url.QueryEscape(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&entity.Tenant{},
		&entity.TenantModelProvider{},
		&entity.TenantModelInstance{},
		&entity.TenantModel{},
		&entity.UserTenant{},
		&entity.Permission{},
		&entity.GroupInfo{},
		&entity.GroupMember{},
		&entity.Role{},
		&entity.RoleResource{},
	); err != nil {
		t.Fatalf("migrate permission tables: %v", err)
	}
	for _, tenantID := range []string{"team-tenant", "other-tenant"} {
		if err := db.Create(&entity.Tenant{ID: tenantID}).Error; err != nil {
			t.Fatalf("create tenant %q: %v", tenantID, err)
		}
	}

	providers := []entity.TenantModelProvider{
		{ID: "team-provider", UserID: "team-owner", ProviderName: "openai", TenantID: "team-tenant"},
		{ID: "other-provider", UserID: "other-owner", ProviderName: "openai", TenantID: "other-tenant"},
	}
	for i := range providers {
		if err := db.Create(&providers[i]).Error; err != nil {
			t.Fatalf("create provider %q: %v", providers[i].ID, err)
		}
	}
	instances := []entity.TenantModelInstance{
		{ID: "team-instance", InstanceName: "team-instance", ProviderID: "team-provider", APIKey: "secret-team-key", Status: "active"},
		{ID: "other-instance", InstanceName: "other-instance", ProviderID: "other-provider", APIKey: "secret-other-key", Status: "active"},
	}
	for i := range instances {
		if err := db.Create(&instances[i]).Error; err != nil {
			t.Fatalf("create instance %q: %v", instances[i].ID, err)
		}
	}
	models := []entity.TenantModel{
		{ID: "team-model", ModelName: "chat-model", ProviderID: "team-provider", InstanceID: "team-instance", ModelType: int(entity.ModelTypeChat), Status: "active"},
		{ID: "malformed-model", ModelName: "cross-provider-model", ProviderID: "team-provider", InstanceID: "other-instance", ModelType: int(entity.ModelTypeChat), Status: "active"},
	}
	for i := range models {
		if err := db.Create(&models[i]).Error; err != nil {
			t.Fatalf("create model %q: %v", models[i].ID, err)
		}
	}
	valid := string(entity.StatusValid)
	invited := entity.TenantMembershipStatusInvited
	memberships := []entity.UserTenant{
		{ID: "team-owner-membership", UserID: "team-owner", TenantID: "team-tenant", Role: string(RoleOwner), Status: &valid},
		{ID: "team-member-membership", UserID: "team-member", TenantID: "team-tenant", Role: "31", Status: &valid},
		{ID: "team-invite-membership", UserID: "team-invite", TenantID: "team-tenant", Status: &invited},
		{ID: "other-owner-membership", UserID: "other-owner", TenantID: "other-tenant", Role: string(RoleOwner), Status: &valid},
	}
	for i := range memberships {
		if err := db.Create(&memberships[i]).Error; err != nil {
			t.Fatalf("create membership %q: %v", memberships[i].ID, err)
		}
	}
	roleTenantID := "team-tenant"
	if err := db.Create(&entity.Role{ID: 31, RoleName: "member", TenantID: &roleTenantID}).Error; err != nil {
		t.Fatalf("create member feature role: %v", err)
	}
	if err := db.Create(&entity.RoleResource{
		RoleID: 31, ResourceType: entity.RoleResourceTypeModelProvider, Action: entity.RoleActionAll,
	}).Error; err != nil {
		t.Fatalf("enable member model feature: %v", err)
	}
	instanceID := "team-instance"
	if err := db.Create(&entity.Permission{
		ID: "team-member-instance-read", TenantID: "team-tenant", ResourceType: "model_instance",
		ResourceID: &instanceID, MemberID: &memberships[1].ID,
		Permission: entity.ResourcePermissionRead, Status: &valid,
	}).Error; err != nil {
		t.Fatalf("grant member read access to instance: %v", err)
	}

	checker := NewDatabaseChecker(db)
	member := permissionTestIdentity{UserID: "team-member"}
	providerRef := ResourceRef{Kind: ResourceKindModelProvider, ID: "team-provider"}
	instanceRef := ResourceRef{Kind: ResourceKindModelInstance, ID: "team-instance"}
	modelRef := ResourceRef{Kind: ResourceKindModel, ID: "team-model"}
	for _, test := range []struct {
		name      string
		subject   permissionTestIdentity
		ref       ResourceRef
		operation Operation
		wantErr   error
	}{
		{name: "member reads provider", subject: member, ref: providerRef, operation: OperationRead},
		{name: "member cannot manage provider", subject: member, ref: providerRef, operation: OperationUpdate, wantErr: ErrPermissionDenied},
		{name: "member uses shared instance", subject: member, ref: instanceRef, operation: OperationUse},
		{name: "member reads shared instance metadata", subject: member, ref: instanceRef, operation: OperationRead},
		{name: "member reads model", subject: member, ref: modelRef, operation: OperationRead},
		{name: "member uses model", subject: member, ref: modelRef, operation: OperationUse},
		{name: "member cannot manage model", subject: member, ref: modelRef, operation: OperationUpdate, wantErr: ErrPermissionDenied},
		{name: "outsider cannot use model", subject: permissionTestIdentity{UserID: "outsider", TenantID: "team-tenant"}, ref: modelRef, operation: OperationUse, wantErr: ErrMembershipNotFound},
		{name: "invite cannot use model", subject: permissionTestIdentity{UserID: "team-invite", TenantID: "team-tenant"}, ref: modelRef, operation: OperationUse, wantErr: ErrMembershipNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := checker.CheckResource(WithIdentity(context.Background(), test.subject.UserID, test.subject.TenantID), test.ref, test.operation)
			if test.wantErr == nil && err != nil {
				t.Fatalf("CheckResource(%s) error = %v, want nil", test.name, err)
			}
			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("CheckResource(%s) error = %v, want %v", test.name, err, test.wantErr)
			}
		})
	}

	owner := permissionTestIdentity{UserID: "team-owner"}
	if err := checker.CheckResource(WithIdentity(context.Background(), owner.UserID, owner.TenantID), instanceRef, OperationRead); err != nil {
		t.Errorf("tenant owner cannot read instance credentials: %v", err)
	}
	if err := checker.CheckResource(WithIdentity(context.Background(), member.UserID, member.TenantID), ResourceRef{Kind: ResourceKindModel, ID: "malformed-model"}, OperationRead); !errors.Is(err, ErrResourceNotFound) {
		t.Errorf("cross-provider model CheckResource error = %v, want resource not found", err)
	}

	for _, query := range []struct {
		name      string
		kind      ResourceKind
		parent    ResourceRef
		operation Operation
		want      string
	}{
		{name: "providers", kind: ResourceKindModelProvider, want: "team-provider"},
		{name: "usable instances under provider", kind: ResourceKindModelInstance, parent: providerRef, operation: OperationUse, want: "team-instance"},
		{name: "models under instance", kind: ResourceKindModel, parent: instanceRef, want: "team-model"},
		{name: "models across tenant", kind: ResourceKindModel, want: "team-model"},
	} {
		t.Run("scope "+query.name, func(t *testing.T) {
			operation := query.operation
			if operation == "" {
				operation = OperationRead
			}
			scope, err := checker.Scope(WithIdentity(context.Background(), member.UserID, member.TenantID), ScopeQuery{
				Kind: query.kind, Parent: query.parent, Operation: operation,
			})
			if err != nil {
				t.Fatalf("Scope(%s) error = %v", query.name, err)
			}
			if scope.Mode != ScopeIDs || len(scope.ResourceIDs) != 1 || scope.ResourceIDs[0] != query.want {
				t.Fatalf("Scope(%s) = %+v, want only %q", query.name, scope, query.want)
			}
		})
	}
	if _, err := checker.Scope(WithIdentity(context.Background(), member.UserID, member.TenantID), ScopeQuery{
		Kind:      ResourceKindModelInstance,
		Parent:    ResourceRef{Kind: ResourceKindModel, ID: "team-model"},
		Operation: OperationRead,
	}); !errors.Is(err, ErrInvalidPermission) {
		t.Errorf("Scope(instances with wrong parent) error = %v, want invalid permission", err)
	}
}
