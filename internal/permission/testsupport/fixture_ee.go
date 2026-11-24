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

package testsupport

import (
	"errors"
	"fmt"
	"strconv"

	"gorm.io/gorm"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
)

// PrepareEnterprisePermissionFixture ensures test users have an active tenant
// membership and a feature role accepted by the enterprise permission policy.
func PrepareEnterprisePermissionFixture(db *gorm.DB, tenantID string, memberIDs ...string) error {
	if db == nil {
		return errors.New("prepare enterprise permission fixture: database is nil")
	}

	models := []any{
		&entity.User{}, &entity.Tenant{}, &entity.UserTenant{}, &entity.Role{}, &entity.RoleResource{},
		&entity.GroupInfo{}, &entity.GroupMember{}, &entity.Permission{}, &entity.PermissionChangeLog{},
		&entity.TenantModelProvider{}, &entity.TenantModelInstance{},
	}
	var missing []any
	for _, model := range models {
		if !db.Migrator().HasTable(model) {
			missing = append(missing, model)
		}
	}
	if len(missing) > 0 {
		if err := db.AutoMigrate(missing...); err != nil {
			return fmt.Errorf("migrate permission test tables: %w", err)
		}
	}

	roleID, err := ensureTenantRole(db, tenantID)
	if err != nil {
		return err
	}
	if err := ensureTenant(db, tenantID, roleID); err != nil {
		return err
	}
	for _, memberID := range memberIDs {
		if memberID == "" {
			continue
		}
		if err := ensureUser(db, memberID); err != nil {
			return err
		}
		if err := ensureMembership(db, tenantID, memberID, roleID); err != nil {
			return err
		}
	}
	if err := ensureTenantOwner(db, tenantID); err != nil {
		return err
	}
	return grantLegacyTeamResources(db, tenantID)
}

func ensureTenant(db *gorm.DB, tenantID string, defaultRoleID int32) error {
	var tenant entity.Tenant
	err := db.Where("id = ?", tenantID).Take(&tenant).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		status := string(entity.StatusValid)
		if err := db.Create(&entity.Tenant{
			ID: tenantID, ParserIDs: "", DefaultRoleID: &defaultRoleID, Status: &status,
		}).Error; err != nil {
			return fmt.Errorf("create test Tenant %q: %w", tenantID, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("load test Tenant %q: %w", tenantID, err)
	}
	if tenant.DefaultRoleID == nil {
		if err := db.Model(&entity.Tenant{}).Where("id = ?", tenantID).
			Update("default_role_id", defaultRoleID).Error; err != nil {
			return fmt.Errorf("set test Tenant %q default role: %w", tenantID, err)
		}
	}
	return nil
}

// ensureTenantOwner fills in an owner missing from community-era test fixtures.
// The fixtures commonly use tenant_id == owner user_id for personal tenants,
// while model-provider fixtures carry the owner in tenant_model_provider.user_id.
func ensureTenantOwner(db *gorm.DB, tenantID string) error {
	var count int64
	if err := db.Model(&entity.UserTenant{}).
		Where("tenant_id = ? AND role = ? AND status = ?", tenantID, string(permission.RoleOwner), entity.TenantMembershipStatusJoined).
		Count(&count).Error; err != nil {
		return fmt.Errorf("count test tenant owners: %w", err)
	}
	if count > 0 {
		return nil
	}
	var membership entity.UserTenant
	var ownerUserID = tenantID
	ownerInferredFromProvider := false
	err := db.Where("tenant_id = ? AND user_id = ?", tenantID, ownerUserID).Take(&membership).Error
	if errors.Is(err, gorm.ErrRecordNotFound) && db.Migrator().HasTable(&entity.TenantModelProvider{}) {
		var provider entity.TenantModelProvider
		providerErr := db.Where("tenant_id = ?", tenantID).Order("id").Take(&provider).Error
		if providerErr == nil && provider.UserID != "" {
			ownerUserID = provider.UserID
			ownerInferredFromProvider = true
			err = db.Where("tenant_id = ? AND user_id = ?", tenantID, ownerUserID).Take(&membership).Error
		} else if providerErr != nil && !errors.Is(providerErr, gorm.ErrRecordNotFound) {
			return fmt.Errorf("load test tenant model provider: %w", providerErr)
		}
	}
	if errors.Is(err, gorm.ErrRecordNotFound) && !ownerInferredFromProvider {
		err = db.Where("tenant_id = ? AND status = ?", tenantID, entity.TenantMembershipStatusJoined).
			Order("id").Take(&membership).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if err := ensureUser(db, ownerUserID); err != nil {
			return err
		}
		status := entity.TenantMembershipStatusJoined
		if err := db.Create(&entity.UserTenant{
			ID: common.GenerateToken(), UserID: ownerUserID, TenantID: tenantID,
			Role: string(permission.RoleOwner), Status: &status,
		}).Error; err != nil {
			return fmt.Errorf("create test tenant owner membership: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("load test tenant membership: %w", err)
	}
	if err := db.Model(&entity.UserTenant{}).Where("id = ?", membership.ID).
		Updates(map[string]any{"role": string(permission.RoleOwner), "status": entity.TenantMembershipStatusJoined}).Error; err != nil {
		return fmt.Errorf("promote test tenant owner: %w", err)
	}
	return nil
}

// Shared tests still exercise the community edition's legacy me/team visibility
// fields. Translate only existing team-visible rows into read grants so those
// tests retain their intent under the enterprise permission policy.
func grantLegacyTeamResources(db *gorm.DB, tenantID string) error {
	var memberships []entity.UserTenant
	if err := db.Where("tenant_id = ? AND status = ?", tenantID, entity.TenantMembershipStatusJoined).
		Find(&memberships).Error; err != nil {
		return fmt.Errorf("load test tenant memberships: %w", err)
	}
	for _, membership := range memberships {
		if err := ensureUser(db, membership.UserID); err != nil {
			return err
		}
	}

	type resource struct {
		resourceType string
		resourceID   string
		ownerID      string
	}
	var resources []resource
	if db.Migrator().HasTable(&entity.Knowledgebase{}) {
		var datasets []entity.Knowledgebase
		if err := db.Where("tenant_id = ? AND permission = ?", tenantID, string(entity.TenantPermissionTeam)).
			Find(&datasets).Error; err != nil {
			return fmt.Errorf("load team-visible test datasets: %w", err)
		}
		for _, dataset := range datasets {
			resources = append(resources, resource{resourceType: "kb", resourceID: dataset.ID, ownerID: dataset.CreatedBy})
		}
	}

	if db.Migrator().HasTable(&entity.UserCanvas{}) {
		var canvases []entity.UserCanvas
		if err := db.Find(&canvases).Error; err != nil {
			return fmt.Errorf("load team-visible test canvases: %w", err)
		}
		for _, canvas := range canvases {
			if canvas.EffectiveTenantID() == tenantID && canvas.Permission == string(entity.TenantPermissionTeam) {
				resources = append(resources, resource{resourceType: "canvas", resourceID: canvas.ID, ownerID: canvas.UserID})
			}
		}
	}

	if db.Migrator().HasTable(&entity.TenantModelInstance{}) {
		// Members may use a saved model instance but not edit its configuration,
		// so the shared fixture grants read/use only.
		var instances []entity.TenantModelInstance
		if err := db.Find(&instances).Error; err != nil {
			return fmt.Errorf("load test model instances: %w", err)
		}
		for _, instance := range instances {
			for _, membership := range memberships {
				if err := ensureTeamGrant(db, tenantID, "model_instance", instance.ID, membership.ID, entity.ResourcePermissionRead); err != nil {
					return err
				}
			}
		}
	}

	if db.Migrator().HasTable(&entity.Memory{}) {
		var memories []entity.Memory
		if err := db.Where("tenant_id = ? AND permissions = ?", tenantID, string(entity.TenantPermissionTeam)).
			Find(&memories).Error; err != nil {
			return fmt.Errorf("load team-visible test memories: %w", err)
		}
		for _, memory := range memories {
			resources = append(resources, resource{resourceType: "memory", resourceID: memory.ID})
		}
	}

	for _, item := range resources {
		for _, membership := range memberships {
			mask := entity.ResourcePermissionRead | entity.ResourcePermissionWrite
			if item.ownerID != "" && item.ownerID == membership.UserID {
				mask |= entity.ResourcePermissionManage
			}
			if err := ensureTeamGrant(db, tenantID, item.resourceType, item.resourceID, membership.ID, mask); err != nil {
				return err
			}
		}
	}
	return nil
}

func ensureTeamGrant(db *gorm.DB, tenantID, resourceType, resourceID, membershipID string, mask int32) error {
	var existing entity.Permission
	err := db.Where("tenant_id = ? AND resource_type = ? AND resource_id = ? AND member_id = ? AND (group_id IS NULL OR group_id = '')",
		tenantID, resourceType, resourceID, membershipID).Take(&existing).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("load test resource permission: %w", err)
	}
	status := string(entity.StatusValid)
	if err := db.Create(&entity.Permission{
		ID: common.GenerateToken(), TenantID: tenantID, ResourceType: resourceType, ResourceID: &resourceID,
		MemberID:   &membershipID,
		Permission: mask,
		Status:     &status,
	}).Error; err != nil {
		return fmt.Errorf("create test resource permission: %w", err)
	}
	return nil
}

func ensureUser(db *gorm.DB, userID string) error {
	var user entity.User
	err := db.Where("id = ?", userID).Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		status := string(entity.StatusValid)
		if err := db.Create(&entity.User{
			ID: userID, Nickname: userID, Email: userID + "@test.local",
			IsAuthenticated: "1", IsActive: "1", IsAnonymous: "0", Status: &status,
		}).Error; err != nil {
			return fmt.Errorf("create test user %q: %w", userID, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("load test user %q: %w", userID, err)
	}
	return nil
}

func ensureMembership(db *gorm.DB, tenantID, userID string, roleID int32) error {
	var membership entity.UserTenant
	err := db.Where("user_id = ? AND tenant_id = ?", userID, tenantID).Take(&membership).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		role := strconv.FormatInt(int64(roleID), 10)
		if userID == tenantID {
			role = string(permission.RoleOwner)
		}
		status := entity.TenantMembershipStatusJoined
		if err := db.Create(&entity.UserTenant{
			ID: common.GenerateToken(), UserID: userID, TenantID: tenantID, Role: role, Status: &status,
		}).Error; err != nil {
			return fmt.Errorf("create test tenant membership for user %q: %w", userID, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("load test tenant membership for user %q: %w", userID, err)
	}

	updates := map[string]any{}
	if membership.Status == nil || *membership.Status != entity.TenantMembershipStatusJoined {
		updates["status"] = entity.TenantMembershipStatusJoined
	}
	if membership.Role != string(permission.RoleOwner) {
		if _, parseErr := strconv.ParseInt(membership.Role, 10, 32); parseErr != nil {
			updates["role"] = strconv.FormatInt(int64(roleID), 10)
		}
	}
	if len(updates) > 0 {
		if err := db.Model(&entity.UserTenant{}).Where("id = ?", membership.ID).Updates(updates).Error; err != nil {
			return fmt.Errorf("update test tenant membership for user %q: %w", userID, err)
		}
	}
	return nil
}

func ensureTenantRole(db *gorm.DB, tenantID string) (int32, error) {
	var role entity.Role
	err := db.Where("tenant_id = ? AND role_name = ?", tenantID, "service-test-default").Take(&role).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		role = entity.Role{RoleName: "service-test-default", TenantID: &tenantID}
		if err := db.Create(&role).Error; err != nil {
			return 0, fmt.Errorf("create test tenant role: %w", err)
		}
	} else if err != nil {
		return 0, fmt.Errorf("load test tenant role: %w", err)
	}

	resourceTypes := []int64{
		entity.RoleResourceTypeDataset, entity.RoleResourceTypeChat, entity.RoleResourceTypeAgent,
		entity.RoleResourceTypeSearch, entity.RoleResourceTypeFile, entity.RoleResourceTypeGroup,
		entity.RoleResourceTypeMemory, entity.RoleResourceTypeModelProvider, entity.RoleResourceTypeMCP,
		entity.RoleResourceTypeChatChannel,
	}
	for _, resourceType := range resourceTypes {
		var count int64
		if err := db.Model(&entity.RoleResource{}).
			Where("role_id = ? AND resource_type = ?", role.ID, resourceType).Count(&count).Error; err != nil {
			return 0, fmt.Errorf("check test tenant role permissions: %w", err)
		}
		if count == 0 {
			if err := db.Create(&entity.RoleResource{
				RoleID: role.ID, ResourceType: resourceType, Action: entity.RoleActionAll,
			}).Error; err != nil {
				return 0, fmt.Errorf("create test tenant role permission: %w", err)
			}
		}
	}
	return role.ID, nil
}

// GrantTestResourcePermission adds an explicit resource grant to an enterprise test fixture.
func GrantTestResourcePermission(db *gorm.DB, tenantID, resourceType, resourceID, userID string, actions ...string) error {
	if db == nil {
		return errors.New("grant test resource permission: database is nil")
	}
	var membership entity.UserTenant
	if err := db.Where("tenant_id = ? AND user_id = ? AND status = ?", tenantID, userID, entity.TenantMembershipStatusJoined).Take(&membership).Error; err != nil {
		return fmt.Errorf("load test resource member %q: %w", userID, err)
	}
	var mask int32
	for _, action := range actions {
		switch action {
		case "read":
			mask |= entity.ResourcePermissionRead
		case "write":
			mask |= entity.ResourcePermissionWrite
		case "manage":
			mask |= entity.ResourcePermissionManage
		case "owner":
			mask |= entity.ResourcePermissionOwner
		default:
			return fmt.Errorf("unknown test resource permission %q", action)
		}
	}
	if mask == 0 {
		return errors.New("grant test resource permission: permission mask is empty")
	}
	memberID, resourceIDValue, status := membership.ID, resourceID, string(entity.StatusValid)
	return db.Create(&entity.Permission{
		ID: common.GenerateToken(), MemberID: &memberID, TenantID: tenantID,
		ResourceType: resourceType, ResourceID: &resourceIDValue, Permission: mask, Status: &status,
	}).Error
}
