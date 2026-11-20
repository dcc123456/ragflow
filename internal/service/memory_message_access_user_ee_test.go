//go:build ee

package service

import (
	"testing"

	"ragflow/internal/dao"
	"ragflow/internal/engine"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
)

func TestListMemoriesForAccessUserUsesCallerPermission(t *testing.T) {
	setupMemoryMessageTestDB(t)
	if err := dao.DB.AutoMigrate(&entity.UserCanvas{}); err != nil {
		t.Fatalf("migrate canvas table: %v", err)
	}
	if err := dao.DB.Create(&[]*entity.User{
		{ID: "canvas-owner", Nickname: "Canvas owner"},
		{ID: "caller", Nickname: "Caller"},
	}).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
	status := "1"
	for _, membership := range []*entity.UserTenant{
		{ID: "owner-membership", UserID: "canvas-owner", TenantID: "canvas-owner", Role: string(permission.RoleOwner), InvitedBy: "canvas-owner", Status: &status},
		{ID: "caller-membership", UserID: "caller", TenantID: "canvas-owner", Role: string(permission.RoleNormal), InvitedBy: "canvas-owner", Status: &status},
	} {
		if err := dao.DB.Create(membership).Error; err != nil {
			t.Fatalf("seed membership: %v", err)
		}
	}
	canvas := entity.UserCanvas{
		ID:         "canvas-1",
		UserID:     "canvas-owner",
		TenantID:   sptr("canvas-owner"),
		Permission: string(TenantPermissionTeam),
	}
	if err := dao.DB.Create(&canvas).Error; err != nil {
		t.Fatalf("seed canvas: %v", err)
	}
	ctx := serviceTestContext(t, "caller", "canvas-owner", "canvas-owner")
	grantServiceTestResource(t, "canvas-owner", "canvas", "canvas-1", "caller", testResourcePermissionRead)
	grantServiceTestResource(t, "canvas-owner", "memory", "memory-shared", "caller", testResourcePermissionRead)
	for _, memory := range []*entity.Memory{
		{ID: "memory-shared", Name: "Shared", TenantID: "canvas-owner", EmbdID: "embd", LLMID: "llm", Permissions: string(TenantPermissionTeam)},
		{ID: "memory-caller-only", Name: "Caller only", TenantID: "caller", EmbdID: "embd", LLMID: "llm", Permissions: string(TenantPermissionMe)},
		{ID: "memory-owner-only", Name: "Owner only", TenantID: "canvas-owner", EmbdID: "embd", LLMID: "llm", Permissions: string(TenantPermissionMe)},
	} {
		if err := dao.DB.Create(memory).Error; err != nil {
			t.Fatalf("seed memory %s: %v", memory.ID, err)
		}
	}

	response, err := NewMemoryService(dao.DB, engine.Get()).ListMemories(ctx, nil, nil, "", "", 1, 30, "canvas-owner")
	if err != nil {
		t.Fatalf("ListMemories(access user): %v", err)
	}
	if response.TotalCount != 1 || len(response.MemoryList) != 1 || response.MemoryList[0]["id"] != "memory-shared" {
		t.Fatalf("canvas memory list = %#v (total=%d), want only memory-shared", response.MemoryList, response.TotalCount)
	}
}
