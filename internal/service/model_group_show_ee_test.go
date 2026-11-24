//go:build ee

package service

import (
	"testing"

	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"
	"ragflow/internal/permission"

	"gorm.io/gorm"
)

func setupModelGroupShowTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.SetupTestDB(t,
		&entity.Tenant{},
		&entity.User{},
		&entity.UserTenant{},
		&entity.Role{},
		&entity.RoleResource{},
		&entity.Permission{},
		&entity.GroupInfo{},
		&entity.GroupMember{},
		&entity.TenantModelProvider{},
		&entity.TenantModelInstance{},
		&entity.TenantModel{},
		&entity.TenantModelGroup{},
		&entity.TenantModelGroupMap{},
	)
	t.Cleanup(testutil.ReplaceDBForTest(t, db))
	return db
}

func TestShowModelGroupDistinguishesDeletedFromUnreadableMembers(t *testing.T) {
	db := setupModelGroupShowTestDB(t)
	seedTenantRoleTenant(t, db, "tenant-model-group")
	seedTenantRoleMember(t, db, "owner-link", "owner", "tenant-model-group", TenantRoleOwner)

	active := entity.ModelGroupMapStatusActive
	rows := []any{
		&entity.TenantModelProvider{ID: "provider-1", UserID: "owner", ProviderName: "DeepSeek", TenantID: "tenant-model-group"},
		&entity.TenantModelProvider{ID: "provider-2", UserID: "other", ProviderName: "MiniMax", TenantID: "tenant-model-group"},
		&entity.TenantModelInstance{ID: "instance-1", InstanceName: "default", ProviderID: "provider-1", APIKey: "key-1", Status: "active", Extra: "{}"},
		&entity.TenantModelInstance{ID: "instance-2", InstanceName: "shared", ProviderID: "provider-2", APIKey: "key-2", Status: "active", Extra: "{}"},
		&entity.TenantModel{ID: "model-live", ModelName: "chat-a", ModelType: int(entity.ModelTypeChat), ProviderID: "provider-1", InstanceID: "instance-1", Status: "active", Extra: "{}"},
		&entity.TenantModel{ID: "model-unreadable", ModelName: "chat-b", ModelType: int(entity.ModelTypeChat), ProviderID: "provider-2", InstanceID: "instance-2", Status: "active", Extra: "{}"},
		&entity.TenantModelGroup{ID: "group-1", UserID: "owner", TenantID: "tenant-model-group", GroupName: "fallback", Status: active},
		&entity.TenantModelGroupMap{ID: "map-live", GroupID: "group-1", ModelID: "model-live", Position: 0, Status: active},
		&entity.TenantModelGroupMap{ID: "map-unreadable", GroupID: "group-1", ModelID: "model-unreadable", Position: 1, Status: active},
		&entity.TenantModelGroupMap{ID: "map-deleted", GroupID: "group-1", ModelID: "model-deleted", Position: 2, Status: active},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seed %T: %v", row, err)
		}
	}

	ctx := permission.WithIdentity(t.Context(), "owner", "tenant-model-group")
	members, err := NewModelProviderService().ShowModelGroup(ctx, "group-1")
	if err != nil {
		t.Fatalf("show model group: %v", err)
	}
	byID := make(map[string]*ShowModelGroupResponse, len(members))
	for _, member := range members {
		byID[member.ID] = member
	}

	deleted := byID["map-deleted"]
	if deleted == nil || !deleted.Unavailable || deleted.Restricted || deleted.ModelID != "model-deleted" {
		t.Fatalf("deleted member = %+v, want unavailable member carrying its model id", deleted)
	}
	unreadable := byID["map-unreadable"]
	if unreadable == nil || !unreadable.Restricted || unreadable.Unavailable || unreadable.ModelID != "" {
		t.Fatalf("unreadable member = %+v, want restricted member without model id", unreadable)
	}
	live := byID["map-live"]
	if live == nil || live.Unavailable || live.Restricted || live.Model != "chat-a" {
		t.Fatalf("live member = %+v, want available member with model name", live)
	}
}
