//go:build enterprise

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

func TestCheckFeatureUsesTenantRoleAndSystemRoleCeiling(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+url.QueryEscape(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&entity.User{}, &entity.UserTenant{}, &entity.Role{}, &entity.RoleResource{}); err != nil {
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
	if err := db.Create(&entity.User{ID: "user-1", Email: "user@example.com", Nickname: "User", RoleID: &systemRoleID}).Error; err != nil {
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
	if err := db.AutoMigrate(&entity.User{}, &entity.UserTenant{}, &entity.MCPServer{}, &entity.Role{}, &entity.RoleResource{}, &entity.GroupInfo{}, &entity.GroupMember{}, &entity.Permission{}); err != nil {
		t.Fatalf("migrate permission tables: %v", err)
	}
	status := string(entity.StatusValid)
	tenantID := "tenant-1"
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
