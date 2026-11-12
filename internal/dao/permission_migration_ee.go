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

package dao

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/entity"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	legacyModelProviderResourceType       = "llm"
	modelInstanceResourceType             = "model_instance"
	permissionMigrationEEMarker           = "mysql_migration.enterprise.permission.version"
	permissionMigrationEETarget           = permissionMigrationTargetVersion
	legacyTenantGroupCreateAction   int32 = 1 << 4
)

// migratePermissionDataEE migrates enterprise data after schema setup.
func migratePermissionDataEE(ctx context.Context, db *gorm.DB) error {
	if err := migrateEvaluations(ctx, db); err != nil {
		return fmt.Errorf("initialize enterprise evaluations: %w", err)
	}
	if err := ensureWhitelistSchema(ctx, db); err != nil {
		return fmt.Errorf("migrate registration whitelist: %w", err)
	}
	if err := migrateTenantSystemRolesEE(ctx, db); err != nil {
		return fmt.Errorf("move system roles from users to Tenants: %w", err)
	}
	version, err := getDatabaseMigrationVersion(ctx, db, permissionMigrationEEMarker)
	if err != nil {
		return fmt.Errorf("read enterprise permission migration version: %w", err)
	}
	if shouldSkipMigration(version, permissionMigrationEETarget) {
		return nil
	}
	if err := migrateGroups(ctx, db); err != nil {
		return err
	}
	if err := migrateGroupResourceNamesEE(ctx, db); err != nil {
		return err
	}
	if err := migrateTenantDefaultRolesEE(ctx, db); err != nil {
		return err
	}
	if err := migrateDefaultTenantRoleMCPFeatureEE(ctx, db); err != nil {
		return err
	}
	if err := migrateResourceOwnersEE(ctx, db); err != nil {
		return err
	}
	if err := setDatabaseMigrationVersionForMarker(ctx, db, permissionMigrationEEMarker, permissionMigrationEETarget); err != nil {
		return fmt.Errorf("mark enterprise permission migration version: %w", err)
	}
	return nil
}

// migrateDefaultTenantRoleMCPFeatureEE gives the existing default member roles
// the newly supported MCP feature without overwriting an explicit MCP setting.
func migrateDefaultTenantRoleMCPFeatureEE(ctx context.Context, db *gorm.DB) error {
	scoped := db.WithContext(ctx)
	for _, model := range []interface{}{&entity.Tenant{}, &entity.Role{}, &entity.RoleResource{}} {
		if !scoped.Migrator().HasTable(model) {
			return nil
		}
	}
	return scoped.Transaction(func(tx *gorm.DB) error {
		var defaultRoleIDs []int32
		if err := tx.Model(&entity.Tenant{}).Where("default_role_id IS NOT NULL").Pluck("default_role_id", &defaultRoleIDs).Error; err != nil {
			return fmt.Errorf("list Tenant default role IDs for MCP feature migration: %w", err)
		}
		if len(defaultRoleIDs) == 0 {
			return nil
		}
		var roles []entity.Role
		if err := tx.Where("id IN ? AND role_name = ? AND tenant_id IS NOT NULL", defaultRoleIDs, defaultTenantMemberRoleName).
			Order("id ASC").Find(&roles).Error; err != nil {
			return fmt.Errorf("list default Tenant member roles for MCP feature migration: %w", err)
		}
		expectedActions := make(map[int64]int32, len(defaultTenantRoleResourceActions)-1)
		for _, action := range defaultTenantRoleResourceActions {
			if action.resourceType != entity.RoleResourceTypeMCP {
				expectedActions[action.resourceType] = action.action
			}
		}
		for _, role := range roles {
			var existing []entity.RoleResource
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("role_id = ?", role.ID).Find(&existing).Error; err != nil {
				return fmt.Errorf("load permissions for default Tenant role %d: %w", role.ID, err)
			}
			actualActions := make(map[int64]int32, len(existing))
			hasMCPAction := false
			for _, permission := range existing {
				actualActions[permission.ResourceType] = permission.Action
				if permission.ResourceType == entity.RoleResourceTypeMCP {
					hasMCPAction = true
				}
			}
			if hasMCPAction || len(existing) != len(expectedActions) {
				continue
			}
			isDefaultRole := true
			for resourceType, action := range expectedActions {
				if actualActions[resourceType] != action {
					isDefaultRole = false
					break
				}
			}
			if !isDefaultRole {
				continue
			}
			if err := tx.Create(&entity.RoleResource{
				RoleID: role.ID, ResourceType: entity.RoleResourceTypeMCP, Action: entity.RoleActionAll,
			}).Error; err != nil {
				return fmt.Errorf("add MCP permissions to default Tenant role %d: %w", role.ID, err)
			}
		}
		return nil
	})
}

type resourceOwnerCandidate struct {
	ID          string `gorm:"column:id"`
	TenantID    string `gorm:"column:tenant_id"`
	OwnerUserID string `gorm:"column:owner_user_id"`
}

func migrateResourceOwnersEE(ctx context.Context, db *gorm.DB) error {
	scoped := db.WithContext(ctx)
	if !scoped.Migrator().HasTable(&entity.Permission{}) || !scoped.Migrator().HasTable(&entity.UserTenant{}) {
		return nil
	}
	return scoped.Transaction(func(tx *gorm.DB) error {
		var memberships []entity.UserTenant
		if err := tx.Select("id", "user_id", "tenant_id", "role").
			Where("status = ?", string(entity.StatusValid)).Order("tenant_id ASC").Order("user_id ASC").Find(&memberships).Error; err != nil {
			return fmt.Errorf("load active Tenant memberships for resource ownership migration: %w", err)
		}
		membershipIDs := make(map[string]map[string]string)
		ownersByTenant := make(map[string]string)
		for _, membership := range memberships {
			members := membershipIDs[membership.TenantID]
			if members == nil {
				members = make(map[string]string)
				membershipIDs[membership.TenantID] = members
			}
			members[membership.UserID] = membership.ID
			if membership.Role == "owner" {
				if currentOwner := ownersByTenant[membership.TenantID]; currentOwner != "" && currentOwner != membership.UserID {
					return fmt.Errorf("Tenant %s has multiple active owners during resource ownership migration", membership.TenantID)
				}
				ownersByTenant[membership.TenantID] = membership.UserID
			}
		}

		var existingGrants []entity.Permission
		if err := tx.Select("tenant_id", "resource_type", "resource_id").
			Where("status = ? AND (permission & ?) <> 0 AND resource_id IS NOT NULL AND resource_id <> ''",
				string(entity.StatusValid), entity.ResourcePermissionOwner).Find(&existingGrants).Error; err != nil {
			return fmt.Errorf("load existing resource owners: %w", err)
		}
		existingOwners := make(map[string]struct{}, len(existingGrants))
		for _, grant := range existingGrants {
			if grant.ResourceID != nil {
				existingOwners[resourceOwnerKey(grant.TenantID, grant.ResourceType, *grant.ResourceID)] = struct{}{}
			}
		}

		queries := []struct {
			table        string
			selectFields string
			where        string
			args         []interface{}
			resourceType string
		}{
			{"knowledgebase AS resource_rows", "resource_rows.id, resource_rows.tenant_id, resource_rows.created_by AS owner_user_id", "resource_rows.status = ?", []interface{}{string(entity.StatusValid)}, "kb"},
			{"dialog AS resource_rows", "resource_rows.id, resource_rows.tenant_id, '' AS owner_user_id", "resource_rows.status = ?", []interface{}{string(entity.StatusValid)}, "dialog"},
			{"search AS resource_rows", "resource_rows.id, resource_rows.tenant_id, resource_rows.created_by AS owner_user_id", "resource_rows.status = ?", []interface{}{string(entity.StatusValid)}, "search"},
			{"user_canvas AS resource_rows", "resource_rows.id, COALESCE(NULLIF(resource_rows.tenant_id, ''), resource_rows.user_id) AS tenant_id, resource_rows.user_id AS owner_user_id", "1 = 1", nil, "canvas"},
			{"memory AS resource_rows", "resource_rows.id, resource_rows.tenant_id, resource_rows.tenant_id AS owner_user_id", "1 = 1", nil, "memory"},
			{"file AS resource_rows", "resource_rows.id, resource_rows.tenant_id, resource_rows.created_by AS owner_user_id", "resource_rows.parent_id IS NOT NULL AND resource_rows.parent_id <> '' AND resource_rows.parent_id <> resource_rows.id", nil, "file"},
			{"mcp_server AS resource_rows", "resource_rows.id, resource_rows.tenant_id, '' AS owner_user_id", "1 = 1", nil, "mcp"},
			{"connector AS resource_rows", "resource_rows.id, resource_rows.tenant_id, '' AS owner_user_id", "1 = 1", nil, "connector"},
			{"skill_spaces AS resource_rows", "resource_rows.id, resource_rows.tenant_id, '' AS owner_user_id", "resource_rows.status = ?", []interface{}{string(entity.StatusValid)}, "file"},
			{"chat_channel AS resource_rows", "resource_rows.id, resource_rows.tenant_id, '' AS owner_user_id", "1 = 1", nil, "dialog"},
			{"tenant_model_instance AS resource_rows JOIN tenant_model_provider ON tenant_model_provider.id = resource_rows.provider_id", "resource_rows.id, tenant_model_provider.tenant_id AS tenant_id, tenant_model_provider.user_id AS owner_user_id", "resource_rows.status = ? AND tenant_model_provider.tenant_id <> ''", []interface{}{"active"}, "model_instance"},
		}
		for _, query := range queries {
			if !migrationTableExists(tx, query.table) {
				continue
			}
			statement := tx.Table(query.table).Select(query.selectFields)
			if query.where != "" {
				statement = statement.Where(query.where, query.args...)
			}
			if err := backfillResourceOwnerGrants(tx, statement, query.resourceType, membershipIDs, ownersByTenant, existingOwners); err != nil {
				return fmt.Errorf("backfill %s resource owners: %w", query.resourceType, err)
			}
		}
		if migrationTableExists(tx, "document") && migrationTableExists(tx, "knowledgebase") {
			documents := tx.Table("document AS resource_rows JOIN knowledgebase AS dataset_rows ON dataset_rows.id = resource_rows.kb_id").
				Select("resource_rows.id, dataset_rows.tenant_id, resource_rows.created_by AS owner_user_id").
				Where("resource_rows.status = ? AND dataset_rows.status = ?", string(entity.StatusValid), string(entity.StatusValid))
			if err := backfillResourceOwnerGrants(tx, documents, "document", membershipIDs, ownersByTenant, existingOwners); err != nil {
				return fmt.Errorf("backfill document resource owners: %w", err)
			}
		}
		return nil
	})
}

func migrationTableExists(db *gorm.DB, table string) bool {
	return db.Migrator().HasTable(strings.Fields(table)[0])
}

func backfillResourceOwnerGrants(tx, query *gorm.DB, resourceType string, membershipIDs map[string]map[string]string, ownersByTenant map[string]string, existingOwners map[string]struct{}) error {
	var pending []entity.Permission
	var pendingLogs []entity.PermissionChangeLog
	flush := func() error {
		if len(pending) == 0 {
			return nil
		}
		if err := tx.CreateInBatches(pending, 500).Error; err != nil {
			return err
		}
		if len(pendingLogs) > 0 {
			if err := tx.CreateInBatches(pendingLogs, 500).Error; err != nil {
				return err
			}
		}
		pending = pending[:0]
		pendingLogs = pendingLogs[:0]
		return nil
	}
	status := string(entity.StatusValid)
	const batchSize = 500
	lastID := ""
	for {
		batch := make([]resourceOwnerCandidate, 0, batchSize)
		batchQuery := query.Session(&gorm.Session{Initialized: true}).Order("resource_rows.id ASC").Limit(batchSize)
		if lastID != "" {
			batchQuery = batchQuery.Where("resource_rows.id > ?", lastID)
		}
		if err := batchQuery.Find(&batch).Error; err != nil {
			return err
		}
		if len(batch) == 0 {
			break
		}
		for _, candidate := range batch {
			if candidate.ID == "" || candidate.TenantID == "" {
				continue
			}
			key := resourceOwnerKey(candidate.TenantID, resourceType, candidate.ID)
			if _, exists := existingOwners[key]; exists {
				continue
			}
			ownerUserID := candidate.OwnerUserID
			memberID := membershipIDs[candidate.TenantID][ownerUserID]
			if memberID == "" {
				ownerUserID = ownersByTenant[candidate.TenantID]
				memberID = membershipIDs[candidate.TenantID][ownerUserID]
			}
			if memberID == "" {
				return fmt.Errorf("resource %s/%s has no active member to own it", resourceType, candidate.ID)
			}
			resourceID := candidate.ID
			pending = append(pending, entity.Permission{
				ID: common.GenerateUUID(), MemberID: &memberID, TenantID: candidate.TenantID,
				ResourceType: resourceType, ResourceID: &resourceID,
				Permission: entity.ResourcePermissionOwner, Status: &status,
			})
			beforeData := `{"permission":0}`
			afterBytes, err := json.Marshal(map[string]interface{}{
				"permission":    entity.ResourcePermissionOwner,
				"owner_user_id": ownerUserID,
				"migration":     "legacy_resource_ownership",
			})
			if err != nil {
				return fmt.Errorf("encode migrated resource owner audit: %w", err)
			}
			afterData := string(afterBytes)
			pendingLogs = append(pendingLogs, entity.PermissionChangeLog{
				ID: common.GenerateUUID(), TenantID: candidate.TenantID,
				TargetType: "member", TargetID: memberID, ResourceType: resourceType, ResourceID: candidate.ID,
				OldPermission: 0, NewPermission: entity.ResourcePermissionOwner,
				ActionType: "migrate", EventType: "resource.owner.migrated", ScopeType: "system",
				BeforeData: &beforeData, AfterData: &afterData, Status: &status,
			})
			existingOwners[key] = struct{}{}
		}
		lastID = batch[len(batch)-1].ID
		if err := flush(); err != nil {
			return err
		}
		if len(batch) < batchSize {
			break
		}
	}
	return flush()
}

func resourceOwnerKey(tenantID, resourceType, resourceID string) string {
	return tenantID + "\x00" + resourceType + "\x00" + resourceID
}

// migrateGroupResourceNamesEE renames legacy group-and-department resources
// and maps the temporary Tenant group-create bit to the shared write action.
func migrateGroupResourceNamesEE(ctx context.Context, db *gorm.DB) error {
	scoped := db.WithContext(ctx)
	if scoped.Migrator().HasTable(&entity.Permission{}) && scoped.Migrator().HasColumn(&entity.Permission{}, "resource_type") {
		if err := scoped.Model(&entity.Permission{}).Where("resource_type = ?", "team").Update("resource_type", "group").Error; err != nil {
			return fmt.Errorf("rename team permission resources to group: %w", err)
		}
	}
	if scoped.Migrator().HasTable(&entity.PermissionChangeLog{}) {
		for _, column := range []string{"resource_type", "target_type"} {
			if !scoped.Migrator().HasColumn(&entity.PermissionChangeLog{}, column) {
				continue
			}
			if err := scoped.Model(&entity.PermissionChangeLog{}).Where(column+" = ?", "team").Update(column, "group").Error; err != nil {
				return fmt.Errorf("rename team audit %s to group: %w", column, err)
			}
		}
		if scoped.Migrator().HasColumn(&entity.PermissionChangeLog{}, "event_type") {
			if err := scoped.Model(&entity.PermissionChangeLog{}).
				Where("event_type LIKE ?", "team.%").
				Update("event_type", gorm.Expr("REPLACE(event_type, ?, ?)", "team.", "group.")).Error; err != nil {
				return fmt.Errorf("rename team audit event types to group: %w", err)
			}
		}
	}
	if !scoped.Migrator().HasTable(&entity.RoleResource{}) || !scoped.Migrator().HasTable(&entity.Role{}) {
		return nil
	}
	type roleResourceAction struct {
		ID     int32 `gorm:"column:id"`
		Action int32 `gorm:"column:action"`
	}
	var rows []roleResourceAction
	if err := scoped.Table("role_resource").
		Select("role_resource.id, role_resource.action").
		Joins("JOIN role ON role.id = role_resource.role_id").
		Where("role.tenant_id IS NOT NULL AND role_resource.resource_type = ?", entity.RoleResourceTypeGroup).
		Where("(role_resource.action & ?) <> 0", legacyTenantGroupCreateAction).
		Order("role_resource.id ASC").Find(&rows).Error; err != nil {
		return fmt.Errorf("list Tenant group role permissions: %w", err)
	}
	for _, row := range rows {
		newAction := row.Action&^legacyTenantGroupCreateAction | entity.RoleActionWrite
		if err := scoped.Model(&entity.RoleResource{}).Where("id = ?", row.ID).Update("action", newAction).Error; err != nil {
			return fmt.Errorf("migrate Tenant group role permission %d: %w", row.ID, err)
		}
	}
	return nil
}

// migrateTenantDefaultRolesEE creates a full-feature member role for each
// Tenant, assigns it as the default, and migrates existing member roles.
func migrateTenantDefaultRolesEE(ctx context.Context, db *gorm.DB) error {
	scoped := db.WithContext(ctx)
	migrator := scoped.Migrator()
	if !migrator.HasTable(&entity.Tenant{}) {
		return nil
	}
	for _, model := range []interface{}{
		&entity.UserTenant{},
		&entity.Role{},
		&entity.RoleResource{},
	} {
		if !migrator.HasTable(model) {
			return fmt.Errorf("cannot migrate default Tenant roles: %T table is missing", model)
		}
	}

	return scoped.Transaction(func(tx *gorm.DB) error {
		var tenants []entity.Tenant
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Order("id ASC").Find(&tenants).Error; err != nil {
			return fmt.Errorf("list Tenants for default role migration: %w", err)
		}
		for _, tenant := range tenants {
			memberRole, created, err := findOrCreateDefaultTenantMemberRole(tx, tenant.ID)
			if err != nil {
				return err
			}
			if created {
				if err := replaceDefaultTenantRolePermissions(tx, memberRole.ID); err != nil {
					return fmt.Errorf("set full permissions for Tenant %s member role %d: %w", tenant.ID, memberRole.ID, err)
				}
			}
			if err := migrateLegacyTenantAdminPermissions(tx, tenant.ID); err != nil {
				return err
			}

			// Only initialize an unset or invalid default role. A later permission
			// migration must not replace a role selected by the Tenant owner.
			defaultRoleID := memberRole.ID
			needsDefaultRole := tenant.DefaultRoleID == nil
			if tenant.DefaultRoleID != nil {
				var existingRole entity.Role
				roleErr := tx.Where("id = ? AND tenant_id = ?", *tenant.DefaultRoleID, tenant.ID).Take(&existingRole).Error
				if roleErr == nil {
					defaultRoleID = existingRole.ID
				} else if !errors.Is(roleErr, gorm.ErrRecordNotFound) {
					return fmt.Errorf("load Tenant %s default role %d: %w", tenant.ID, *tenant.DefaultRoleID, roleErr)
				} else {
					needsDefaultRole = true
				}
			}
			if needsDefaultRole {
				if err := tx.Model(&entity.Tenant{}).Where("id = ?", tenant.ID).Update("default_role_id", defaultRoleID).Error; err != nil {
					return fmt.Errorf("set Tenant %s default role: %w", tenant.ID, err)
				}
			}
			roleID := fmt.Sprint(memberRole.ID)
			if err := tx.Model(&entity.UserTenant{}).
				Where("tenant_id = ? AND role IN ?", tenant.ID, []string{"normal", "admin"}).
				Update("role", roleID).Error; err != nil {
				return fmt.Errorf("assign Tenant %s member role to legacy memberships: %w", tenant.ID, err)
			}
		}
		return nil
	})
}

func migrateLegacyTenantAdminPermissions(tx *gorm.DB, tenantID string) error {
	var admins []entity.UserTenant
	if err := tx.Where("tenant_id = ? AND role = ? AND status = ?", tenantID, "admin", string(entity.StatusValid)).
		Find(&admins).Error; err != nil {
		return fmt.Errorf("list legacy Tenant admins for Tenant %s: %w", tenantID, err)
	}

	permissionMask := entity.TenantPermissionManageMembers | entity.TenantPermissionManageRoles
	valid := string(entity.StatusValid)
	resourceID := tenantID
	for _, admin := range admins {
		var grant entity.Permission
		err := tx.Where("tenant_id = ? AND resource_type = ? AND resource_id = ? AND member_id = ? AND status = ? AND (group_id IS NULL OR group_id = '')",
			tenantID, "tenant", tenantID, admin.ID, valid).Take(&grant).Error
		if err == nil {
			if grant.Permission&permissionMask == permissionMask {
				continue
			}
			if err := tx.Model(&entity.Permission{}).Where("id = ?", grant.ID).
				Update("permission", grant.Permission|permissionMask).Error; err != nil {
				return fmt.Errorf("preserve management permissions for legacy Tenant admin %s: %w", admin.UserID, err)
			}
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("find management permission for legacy Tenant admin %s: %w", admin.UserID, err)
		}

		memberID := admin.ID
		grant = entity.Permission{
			ID:           common.GenerateUUID(),
			MemberID:     &memberID,
			TenantID:     tenantID,
			ResourceType: "tenant",
			ResourceID:   &resourceID,
			Permission:   permissionMask,
			Status:       &valid,
		}
		if err := tx.Create(&grant).Error; err != nil {
			return fmt.Errorf("grant Tenant management permissions to legacy admin %s: %w", admin.UserID, err)
		}
	}
	return nil
}

type tenantSystemRoleRow struct {
	ID            string `gorm:"column:id"`
	RoleID        *int32 `gorm:"column:role_id"`
	DefaultRoleID *int32 `gorm:"column:default_role_id"`
}

type tenantUserSystemRole struct {
	TenantID string `gorm:"column:tenant_id"`
	UserID   string `gorm:"column:user_id"`
	Role     string `gorm:"column:role"`
	RoleID   int32  `gorm:"column:role_id"`
}

// migrateTenantSystemRolesEE moves the user-level system role to each Tenant,
// while preserving the previous tenant.role_id member-role assignment under
// default_role_id. It runs before the enterprise migration marker so existing
// databases receive the schema and data change even when older migrations are
// already current.
func migrateTenantSystemRolesEE(ctx context.Context, db *gorm.DB) error {
	scoped := db.WithContext(ctx)
	migrator := scoped.Migrator()
	if !migrator.HasTable(&entity.Tenant{}) {
		return nil
	}
	if !migrator.HasColumn(&entity.Tenant{}, "RoleID") {
		return fmt.Errorf("tenant.role_id column is missing")
	}
	if !migrator.HasColumn(&entity.Tenant{}, "DefaultRoleID") {
		if err := migrator.AddColumn(&entity.Tenant{}, "DefaultRoleID"); err != nil {
			return fmt.Errorf("add tenant.default_role_id: %w", err)
		}
	}
	if err := migrateDefaultTenantSystemRoleSettingEE(ctx, scoped); err != nil {
		return err
	}
	hasRoles := migrator.HasTable(&entity.Role{})
	hasUsers := migrator.HasTable(&entity.User{})
	hasUserRole := hasUsers && migrator.HasColumn(&entity.User{}, "role_id")
	if !hasRoles {
		for _, table := range []string{"tenant", "user"} {
			if table == "user" && !hasUserRole {
				continue
			}
			var count int64
			if err := scoped.Table(table).Where("role_id IS NOT NULL").Count(&count).Error; err != nil {
				return fmt.Errorf("check legacy %s.role_id values without role table: %w", table, err)
			}
			if count > 0 {
				return fmt.Errorf("cannot migrate %s.role_id values because the role table is missing", table)
			}
		}
		var tenantCount int64
		if err := scoped.Model(&entity.Tenant{}).Count(&tenantCount).Error; err != nil {
			return fmt.Errorf("count Tenants while checking required system roles: %w", err)
		}
		if tenantCount > 0 {
			return fmt.Errorf("cannot require a system role for each Tenant because the role table is missing")
		}
	}
	var defaultSystemRole *entity.Role
	if hasRoles {
		if err := ensureDefaultTenantSystemRoleEE(ctx, scoped); err != nil {
			return fmt.Errorf("initialize default system role: %w", err)
		}
		var err error
		defaultSystemRole, err = GetDefaultTenantSystemRole(ctx, scoped)
		if err != nil {
			return fmt.Errorf("load default system role for Tenant migration: %w", err)
		}
	}

	if hasRoles {
		if err := scoped.Transaction(func(tx *gorm.DB) error {
			var tenants []tenantSystemRoleRow
			if err := tx.Table("tenant").Select("id, role_id, default_role_id").Order("id ASC").Find(&tenants).Error; err != nil {
				return fmt.Errorf("list Tenants for system role migration: %w", err)
			}
			for i := range tenants {
				tenant := &tenants[i]
				if tenant.RoleID == nil {
					continue
				}
				var role entity.Role
				if err := tx.Select("id", "tenant_id").Where("id = ?", *tenant.RoleID).Take(&role).Error; err != nil {
					return fmt.Errorf("load Tenant %s role %d: %w", tenant.ID, *tenant.RoleID, err)
				}
				if role.TenantID == nil {
					continue
				}
				if *role.TenantID != tenant.ID {
					return fmt.Errorf("tenant %s references role %d owned by tenant %s", tenant.ID, role.ID, *role.TenantID)
				}
				if tenant.DefaultRoleID == nil {
					if err := tx.Model(&entity.Tenant{}).Where("id = ?", tenant.ID).Update("default_role_id", role.ID).Error; err != nil {
						return fmt.Errorf("move Tenant %s member role to default_role_id: %w", tenant.ID, err)
					}
					tenant.DefaultRoleID = &role.ID
				}
				if err := tx.Model(&entity.Tenant{}).Where("id = ?", tenant.ID).Update("role_id", nil).Error; err != nil {
					return fmt.Errorf("clear legacy member role from Tenant %s: %w", tenant.ID, err)
				}
				tenant.RoleID = nil
			}

			if !hasUserRole {
				return nil
			}
			if !tx.Migrator().HasTable(&entity.UserTenant{}) {
				var count int64
				if err := tx.Table("user").Where("role_id IS NOT NULL").Count(&count).Error; err != nil {
					return fmt.Errorf("check user.role_id values without user_tenant table: %w", err)
				}
				if count > 0 {
					return fmt.Errorf("cannot move user.role_id to Tenants: user_tenant table is missing")
				}
				return nil
			}
			var assignments []tenantUserSystemRole
			if err := tx.Table("user_tenant AS ut").
				Select("ut.tenant_id, ut.user_id, ut.role, u.role_id").
				Joins("JOIN user AS u ON u.id = ut.user_id").
				Where("ut.status = ? AND u.role_id IS NOT NULL", string(entity.StatusValid)).
				Order("ut.tenant_id ASC, ut.role ASC, ut.user_id ASC").Find(&assignments).Error; err != nil {
				return fmt.Errorf("list user system roles for Tenant migration: %w", err)
			}
			var orphanedUsers int64
			if err := tx.Table("user AS u").
				Joins("LEFT JOIN user_tenant AS ut ON ut.user_id = u.id AND ut.status = ?", string(entity.StatusValid)).
				Where("u.role_id IS NOT NULL AND ut.id IS NULL").Count(&orphanedUsers).Error; err != nil {
				return fmt.Errorf("check user system roles without active Tenant membership: %w", err)
			}
			if orphanedUsers > 0 {
				return fmt.Errorf("cannot move %d user.role_id assignments: those users have no active Tenant membership", orphanedUsers)
			}
			byTenant := make(map[string][]tenantUserSystemRole)
			for _, assignment := range assignments {
				byTenant[assignment.TenantID] = append(byTenant[assignment.TenantID], assignment)
			}
			for i := range tenants {
				tenant := &tenants[i]
				if tenant.RoleID != nil {
					continue
				}
				members := byTenant[tenant.ID]
				if len(members) == 0 {
					continue
				}
				var owners []tenantUserSystemRole
				for _, member := range members {
					if member.Role == "owner" {
						owners = append(owners, member)
					}
				}
				var selectedRoleID int32
				switch len(owners) {
				case 1:
					selectedRoleID = owners[0].RoleID
				case 0:
					uniqueRoles := make(map[int32]struct{})
					for _, member := range members {
						uniqueRoles[member.RoleID] = struct{}{}
					}
					if len(uniqueRoles) != 1 {
						return fmt.Errorf("Tenant %s has conflicting user roles and no active owner role to migrate", tenant.ID)
					}
					for roleID := range uniqueRoles {
						selectedRoleID = roleID
					}
				default:
					return fmt.Errorf("Tenant %s has %d active owners with user roles; cannot choose a Tenant role", tenant.ID, len(owners))
				}
				var role entity.Role
				if err := tx.Select("id", "tenant_id").Where("id = ?", selectedRoleID).Take(&role).Error; err != nil {
					return fmt.Errorf("load user role %d for Tenant %s: %w", selectedRoleID, tenant.ID, err)
				}
				if role.TenantID != nil {
					return fmt.Errorf("user role %d for Tenant %s is tenant-scoped", selectedRoleID, tenant.ID)
				}
				if err := tx.Model(&entity.Tenant{}).Where("id = ? AND role_id IS NULL", tenant.ID).Update("role_id", selectedRoleID).Error; err != nil {
					return fmt.Errorf("assign system role %d to Tenant %s: %w", selectedRoleID, tenant.ID, err)
				}
				tenant.RoleID = &selectedRoleID
			}
			for _, tenant := range tenants {
				if tenant.RoleID != nil {
					continue
				}
				if defaultSystemRole == nil {
					return fmt.Errorf("Tenant %s has no system role and no default system role is configured", tenant.ID)
				}
				if err := tx.Model(&entity.Tenant{}).Where("id = ? AND role_id IS NULL", tenant.ID).Update("role_id", defaultSystemRole.ID).Error; err != nil {
					return fmt.Errorf("assign default system role %d to Tenant %s: %w", defaultSystemRole.ID, tenant.ID, err)
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}

	return nil
}

func migrateDefaultTenantSystemRoleSettingEE(ctx context.Context, db *gorm.DB) error {
	if !db.Migrator().HasTable(&entity.SystemSettings{}) {
		return nil
	}
	const oldName = "user.default_system_role_id"
	const newName = "tenant.default_system_role_id"
	var oldSetting entity.SystemSettings
	if err := db.WithContext(ctx).Where("name = ?", oldName).Take(&oldSetting).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return fmt.Errorf("load legacy default user role setting: %w", err)
	}
	var currentSetting entity.SystemSettings
	if err := db.WithContext(ctx).Where("name = ?", newName).Take(&currentSetting).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("load default Tenant role setting: %w", err)
		}
		oldSetting.Name = newName
		if oldSetting.Source == "" {
			oldSetting.Source = "admin"
		}
		if err := db.WithContext(ctx).Create(&oldSetting).Error; err != nil {
			return fmt.Errorf("rename default user role setting: %w", err)
		}
	} else if strings.TrimSpace(currentSetting.Value) == "" && strings.TrimSpace(oldSetting.Value) != "" {
		if err := db.WithContext(ctx).Model(&entity.SystemSettings{}).Where("name = ?", newName).
			Updates(map[string]interface{}{"value": oldSetting.Value, "source": oldSetting.Source, "data_type": oldSetting.DataType}).Error; err != nil {
			return fmt.Errorf("move default user role value to Tenant setting: %w", err)
		}
	}
	if err := db.WithContext(ctx).Where("name = ?", oldName).Delete(&entity.SystemSettings{}).Error; err != nil {
		return fmt.Errorf("remove legacy default user role setting: %w", err)
	}
	return nil
}

func migrateGroups(ctx context.Context, db *gorm.DB) error {
	migrator := db.WithContext(ctx).Migrator()
	if !migrator.HasTable(&entity.GroupInfo{}) {
		return fmt.Errorf("group_info table is missing after GORM migration")
	}
	if !migrator.HasTable(&entity.GroupMember{}) {
		return fmt.Errorf("group_member table is missing after GORM migration")
	}
	for _, field := range []string{"Description", "ParentID", "Path", "FormattedPath"} {
		if !migrator.HasColumn(&entity.GroupInfo{}, field) {
			return fmt.Errorf("group_info.%s is missing after GORM migration", field)
		}
	}
	if err := backfillGroupPaths(ctx, db); err != nil {
		return err
	}
	if err := migrateDepartments(ctx, db); err != nil {
		return err
	}
	if err := migratePermissionTable(ctx, db); err != nil {
		return err
	}
	return migrateModelProviderSharesToInstances(ctx, db)
}

func migratePermissionTable(ctx context.Context, db *gorm.DB) error {
	migrator := db.WithContext(ctx).Migrator()
	if !migrator.HasTable(&entity.Permission{}) {
		return fmt.Errorf("permission table is missing after GORM migration")
	}
	if migrator.HasColumn(&entity.Permission{}, "department_id") {
		return fmt.Errorf("permission.department_id remains after department migration")
	}
	for _, column := range []string{"member_id", "group_id", "tenant_id", "resource_type", "resource_id", "permission", "status"} {
		if !migrator.HasColumn(&entity.Permission{}, column) {
			return fmt.Errorf("permission.%s column is missing", column)
		}
	}
	return nil
}

type legacyModelPermission struct {
	ID         string     `gorm:"column:id"`
	TenantID   string     `gorm:"column:tenant_id"`
	MemberID   *string    `gorm:"column:member_id"`
	GroupID    *string    `gorm:"column:group_id"`
	ResourceID string     `gorm:"column:resource_id"`
	Permission int64      `gorm:"column:permission"`
	Status     *string    `gorm:"column:status"`
	CreateTime *int64     `gorm:"column:create_time"`
	CreateDate *time.Time `gorm:"column:create_date"`
	UpdateTime *int64     `gorm:"column:update_time"`
	UpdateDate *time.Time `gorm:"column:update_date"`
}

type modelID struct {
	ID string `gorm:"column:id"`
}

type existingModelPermission struct {
	ID         string  `gorm:"column:id"`
	Permission int64   `gorm:"column:permission"`
	Status     *string `gorm:"column:status"`
}

func migrateModelProviderSharesToInstances(ctx context.Context, db *gorm.DB) error {
	// Legacy llm grants name a provider. Materialize them only for instances
	// that exist during this migration; future instances receive no grant.
	migrator := db.WithContext(ctx).Migrator()
	if !migrator.HasTable("permission") {
		return nil
	}
	for _, column := range []string{"tenant_id", "resource_type", "resource_id", "permission", "member_id", "group_id", "status"} {
		if !migrator.HasColumn(&entity.Permission{}, column) {
			return nil
		}
	}

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var grants []legacyModelPermission
		if err := tx.Table("permission").
			Where("resource_type = ?", legacyModelProviderResourceType).
			Order("id").
			Find(&grants).Error; err != nil {
			return fmt.Errorf("list legacy model provider permissions: %w", err)
		}
		if len(grants) == 0 {
			return nil
		}
		for _, table := range []string{"tenant_model_provider", "tenant_model_instance"} {
			if !tx.Migrator().HasTable(table) {
				return fmt.Errorf("cannot migrate model provider permissions: %s table is missing", table)
			}
		}

		for _, grant := range grants {
			if grant.Status == nil || *grant.Status != string(entity.StatusValid) {
				if err := tx.Exec("DELETE FROM permission WHERE id = ?", grant.ID).Error; err != nil {
					return fmt.Errorf("remove inactive model provider permission %s: %w", grant.ID, err)
				}
				continue
			}
			memberID := nonEmptyPermissionSubject(grant.MemberID)
			groupID := nonEmptyPermissionSubject(grant.GroupID)
			if (memberID == nil) == (groupID == nil) {
				return fmt.Errorf("legacy model permission %s must target exactly one member or group", grant.ID)
			}
			mask, err := legacyPermissionMask(grant.Permission)
			if err != nil {
				return fmt.Errorf("migrate legacy model permission %s: %w", grant.ID, err)
			}

			var provider modelID
			providerQuery := tx.Table("tenant_model_provider").
				Select("id").
				Where("tenant_id = ? AND id = ?", grant.TenantID, grant.ResourceID)
			err = providerQuery.Take(&provider).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				err = tx.Table("tenant_model_provider").
					Select("id").
					Where("tenant_id = ? AND provider_name = ?", grant.TenantID, grant.ResourceID).
					Take(&provider).Error
			}
			if errors.Is(err, gorm.ErrRecordNotFound) {
				if err := tx.Exec("DELETE FROM permission WHERE id = ?", grant.ID).Error; err != nil {
					return fmt.Errorf("remove orphaned model provider permission %s: %w", grant.ID, err)
				}
				common.Warn("Removed model provider permission without a provider in its tenant",
					zap.String("permission_id", grant.ID),
					zap.String("tenant_id", grant.TenantID),
					zap.String("resource_id", grant.ResourceID))
				continue
			}
			if err != nil {
				return fmt.Errorf("resolve model provider %q for tenant %s by ID or name: %w", grant.ResourceID, grant.TenantID, err)
			}

			var instances []modelID
			if err := tx.Table("tenant_model_instance").
				Select("id").
				Where("provider_id = ?", provider.ID).
				Order("id").
				Find(&instances).Error; err != nil {
				return fmt.Errorf("list instances for provider %s: %w", provider.ID, err)
			}

			sourceRowAvailable := true
			for _, instance := range instances {
				query := tx.Table("permission").
					Where("tenant_id = ? AND resource_type = ? AND resource_id = ?", grant.TenantID, modelInstanceResourceType, instance.ID)
				if memberID != nil {
					query = query.Where("member_id = ? AND (group_id IS NULL OR group_id = '')", *memberID)
				} else {
					query = query.Where("group_id = ? AND (member_id IS NULL OR member_id = '')", *groupID)
				}

				var existing existingModelPermission
				err := query.Take(&existing).Error
				if err == nil {
					merged := existing.Permission | mask
					updates := map[string]any{"permission": merged}
					if grant.Status != nil && *grant.Status == string(entity.StatusValid) &&
						(existing.Status == nil || *existing.Status != string(entity.StatusValid)) {
						updates["status"] = string(entity.StatusValid)
					}
					if err := tx.Table("permission").Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
						return fmt.Errorf("merge model instance permission %s: %w", existing.ID, err)
					}
					continue
				}
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return fmt.Errorf("find model instance permission for %s: %w", instance.ID, err)
				}

				values := map[string]any{
					"tenant_id": grant.TenantID, "member_id": memberID, "group_id": groupID,
					"resource_type": modelInstanceResourceType, "resource_id": instance.ID,
					"permission": mask, "status": grant.Status,
					"create_time": grant.CreateTime, "create_date": grant.CreateDate,
					"update_time": grant.UpdateTime, "update_date": grant.UpdateDate,
				}
				if sourceRowAvailable {
					if err := tx.Table("permission").Where("id = ?", grant.ID).Updates(values).Error; err != nil {
						return fmt.Errorf("convert model provider permission %s to instance %s: %w", grant.ID, instance.ID, err)
					}
					sourceRowAvailable = false
					continue
				}
				values["id"] = common.GenerateUUID()
				if err := tx.Table("permission").Create(values).Error; err != nil {
					return fmt.Errorf("create model instance permission for %s: %w", instance.ID, err)
				}
			}

			if sourceRowAvailable {
				if err := tx.Exec("DELETE FROM permission WHERE id = ?", grant.ID).Error; err != nil {
					return fmt.Errorf("remove legacy model provider permission %s: %w", grant.ID, err)
				}
			}
		}
		return nil
	})
}

func nonEmptyPermissionSubject(subject *string) *string {
	if subject == nil || *subject == "" {
		return nil
	}
	return subject
}

func legacyPermissionMask(permission int64) (int64, error) {
	switch permission {
	case 0:
		return 0, nil
	case 1:
		return 1, nil
	case 2, 3:
		return 3, nil
	case 4, 5, 6:
		return 7, nil
	case 7:
		return 15, nil
	default:
		return 0, fmt.Errorf("unsupported legacy permission value %d", permission)
	}
}

func backfillGroupPaths(ctx context.Context, db *gorm.DB) error {
	var groups []struct {
		ID            string
		Name          *string
		Path          *string
		FormattedPath *string
	}
	if err := db.WithContext(ctx).Table("group_info").
		Select("id, name, path, formatted_path").
		Where("path IS NULL OR path = '' OR formatted_path IS NULL OR formatted_path = ''").
		Find(&groups).Error; err != nil {
		return fmt.Errorf("list groups needing paths: %w", err)
	}
	for _, group := range groups {
		values := map[string]any{}
		if group.Path == nil || *group.Path == "" {
			values["path"] = "/" + group.ID + "/"
		}
		if group.FormattedPath == nil || *group.FormattedPath == "" {
			name := ""
			if group.Name != nil {
				name = *group.Name
			}
			values["formatted_path"] = "/" + name + "/"
		}
		if err := db.WithContext(ctx).Table("group_info").Where("id = ?", group.ID).Updates(values).Error; err != nil {
			return fmt.Errorf("backfill group %s path: %w", group.ID, err)
		}
	}
	return nil
}

func migrateDepartments(ctx context.Context, db *gorm.DB) error {
	migrator := db.WithContext(ctx).Migrator()
	hasDepartments := migrator.HasTable("department")
	hasMembers := migrator.HasTable("department_member")
	hasGroupMemberRole, err := migrationHasColumn(migrator, &legacyGroupMemberSchema{}, "role")
	if err != nil {
		return fmt.Errorf("inspect group_member.role: %w", err)
	}
	hasDepartmentMemberRole := false
	if hasMembers {
		hasDepartmentMemberRole, err = migrationHasColumn(migrator, &legacyDepartmentMemberSchema{}, "role")
		if err != nil {
			return fmt.Errorf("inspect department_member.role: %w", err)
		}
	}
	if hasMembers && !hasDepartments {
		var count int64
		if err := db.WithContext(ctx).Table("department_member").Count(&count).Error; err != nil {
			return fmt.Errorf("count department members: %w", err)
		}
		if count != 0 {
			return fmt.Errorf("department_member has %d rows without department table", count)
		}
	}
	hasDepartmentPermission := false
	if migrator.HasTable("permission") {
		hasDepartmentPermission, err = migrationHasColumn(migrator, &legacyPermissionDepartmentSchema{}, "department_id")
		if err != nil {
			return fmt.Errorf("inspect permission.department_id: %w", err)
		}
	}
	if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if hasDepartments {
			if err := checkDepartmentGroups(ctx, tx); err != nil {
				return err
			}
			if err := tx.WithContext(ctx).Exec(`INSERT INTO group_info
				(id, create_time, create_date, update_time, update_date, name, description, avatar,
				 owner_id, tenant_id, parent_id, path, formatted_path, status)
				SELECT d.id, d.create_time, d.create_date, d.update_time, d.update_date,
				 d.name, d.description, d.avatar, d.owner_id, d.tenant_id,
				 CASE WHEN d.parent_id = d.id THEN NULL ELSE d.parent_id END,
				 d.path, d.formatted_path, d.status
				FROM department d WHERE NOT EXISTS (SELECT 1 FROM group_info g WHERE g.id = d.id)`).Error; err != nil {
				return fmt.Errorf("copy departments to groups: %w", err)
			}
		}
		if hasMembers {
			if err := checkDepartmentMembers(ctx, tx, hasGroupMemberRole); err != nil {
				return err
			}
			if err := copyDepartmentMembersToGroups(ctx, tx, hasGroupMemberRole, hasDepartmentMemberRole); err != nil {
				return fmt.Errorf("copy department members to group members: %w", err)
			}
		}
		if hasDepartmentPermission {
			if err := checkDepartmentPermissions(ctx, tx, hasDepartments); err != nil {
				return err
			}
			if err := tx.WithContext(ctx).Exec(`UPDATE permission SET group_id = department_id, department_id = NULL
				WHERE department_id IS NOT NULL AND department_id <> ''`).Error; err != nil {
				return fmt.Errorf("move department grants to groups: %w", err)
			}
		}
		if err := migrateLegacyGroupMemberPermissions(ctx, tx, hasGroupMemberRole, hasMembers && !hasGroupMemberRole && hasDepartmentMemberRole); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	if hasGroupMemberRole {
		if err := dropLegacyGroupMemberRoleColumn(db.WithContext(ctx)); err != nil {
			return err
		}
	}
	if hasDepartmentPermission {
		if err := dropLegacyColumn(db.WithContext(ctx), &legacyPermissionDepartmentSchema{}, "permission", "department_id"); err != nil {
			return err
		}
	}
	if hasMembers {
		if err := migrator.DropTable("department_member"); err != nil {
			return fmt.Errorf("drop department_member: %w", err)
		}
	}
	if hasDepartments {
		if err := migrator.DropTable("department"); err != nil {
			return fmt.Errorf("drop department: %w", err)
		}
	}
	return nil
}

type legacyGroupMemberSchema struct {
	Role string `gorm:"column:role"`
}

func (legacyGroupMemberSchema) TableName() string { return "group_member" }

type legacyDepartmentMemberSchema struct {
	Role string `gorm:"column:role"`
}

func (legacyDepartmentMemberSchema) TableName() string { return "department_member" }

type legacyPermissionDepartmentSchema struct {
	DepartmentID string `gorm:"column:department_id"`
}

func (legacyPermissionDepartmentSchema) TableName() string { return "permission" }

func migrationHasColumn(migrator gorm.Migrator, model interface{}, name string) (bool, error) {
	columns, err := migrator.ColumnTypes(model)
	if err != nil {
		return false, err
	}
	for _, column := range columns {
		if strings.EqualFold(column.Name(), name) {
			return true, nil
		}
	}
	return false, nil
}

func dropLegacyGroupMemberRoleColumn(db *gorm.DB) error {
	return dropLegacyColumn(db, &legacyGroupMemberSchema{}, "group_member", "role")
}

func dropLegacyColumn(db *gorm.DB, model interface{}, table, column string) error {
	migrator := db.Migrator()
	if !migrator.HasTable(model) {
		return nil
	}
	hasColumn, err := migrationHasColumn(migrator, model, column)
	if err != nil || !hasColumn {
		return err
	}
	dropErr := migrator.DropColumn(model, column)
	hasColumn, err = migrationHasColumn(migrator, model, column)
	if err != nil || !hasColumn {
		return err
	}
	indexes, err := migrator.GetIndexes(model)
	if err != nil {
		return fmt.Errorf("list indexes for %s.%s: %w", table, column, err)
	}
	for _, index := range indexes {
		for _, indexColumn := range index.Columns() {
			if strings.EqualFold(indexColumn, column) {
				if err := migrator.DropIndex(model, index.Name()); err != nil {
					return fmt.Errorf("drop index %s on %s.%s: %w", index.Name(), table, column, err)
				}
				break
			}
		}
	}
	if err := db.Exec("ALTER TABLE " + table + " DROP COLUMN " + column).Error; err != nil {
		if dropErr != nil {
			return fmt.Errorf("drop %s.%s (GORM drop failed: %v): %w", table, column, dropErr, err)
		}
		return fmt.Errorf("drop %s.%s: %w", table, column, err)
	}
	hasColumn, err = migrationHasColumn(migrator, model, column)
	if err != nil {
		return err
	}
	if hasColumn {
		return fmt.Errorf("%s.%s remains after migration", table, column)
	}
	return nil
}

func copyDepartmentMembersToGroups(ctx context.Context, tx *gorm.DB, groupHasRole, departmentHasRole bool) error {
	columns := "id, create_time, create_date, update_time, update_date, member_id, group_id, status"
	selectColumns := "d.id, d.create_time, d.create_date, d.update_time, d.update_date, d.member_id, d.department_id, d.status"
	if groupHasRole {
		columns = "id, create_time, create_date, update_time, update_date, member_id, group_id, role, status"
		role := "'member'"
		if departmentHasRole {
			role = "d.role"
		}
		selectColumns = "d.id, d.create_time, d.create_date, d.update_time, d.update_date, d.member_id, d.department_id, " + role + ", d.status"
	}
	statement := fmt.Sprintf(`INSERT INTO group_member (%s)
		SELECT %s FROM department_member d
		WHERE NOT EXISTS (SELECT 1 FROM group_member g WHERE g.id = d.id)`, columns, selectColumns)
	return tx.WithContext(ctx).Exec(statement).Error
}

type legacyGroupPermissionRow struct {
	TenantID string `gorm:"column:tenant_id"`
	GroupID  string `gorm:"column:group_id"`
	OwnerID  string `gorm:"column:owner_id"`
	MemberID string `gorm:"column:member_id"`
	Role     string `gorm:"column:role"`
}

type legacyGroupPermissionKey struct {
	tenantID string
	groupID  string
	memberID string
}

type legacyGroupPermissionGrant struct {
	mask  int32
	roles []string
}

func migrateLegacyGroupMemberPermissions(ctx context.Context, tx *gorm.DB, groupHasRole, useDepartmentRoles bool) error {
	if !groupHasRole && !useDepartmentRoles {
		return nil
	}
	table, groupColumn := "group_member AS gm", "gm.group_id"
	if !groupHasRole {
		table, groupColumn = "department_member AS gm", "gm.department_id"
	}
	var rows []legacyGroupPermissionRow
	query := tx.WithContext(ctx).Table(table).
		Select("group_info.tenant_id, "+groupColumn+" AS group_id, group_info.owner_id, gm.member_id, gm.role").
		Joins("JOIN group_info ON group_info.id = "+groupColumn).
		Joins("JOIN user_tenant ON user_tenant.id = gm.member_id AND user_tenant.tenant_id = group_info.tenant_id").
		Where("gm.status = ? AND user_tenant.status = ? AND group_info.status = ?",
			entity.GroupMemberStatusJoined, string(entity.StatusValid), string(entity.StatusValid)).
		Where("gm.role IS NOT NULL AND gm.role <> ''")
	if err := query.Order("group_info.tenant_id ASC").Order("group_id ASC").Order("gm.member_id ASC").Find(&rows).Error; err != nil {
		return fmt.Errorf("load legacy group member roles: %w", err)
	}
	grants := make(map[legacyGroupPermissionKey]*legacyGroupPermissionGrant)
	for _, row := range rows {
		key := legacyGroupPermissionKey{tenantID: row.TenantID, groupID: row.GroupID, memberID: row.MemberID}
		role := strings.ToLower(strings.TrimSpace(row.Role))
		var mask int32
		switch role {
		case "owner":
			if row.MemberID != row.OwnerID {
				return fmt.Errorf("group %s has an owner member %s that differs from group_info.owner_id %s", row.GroupID, row.MemberID, row.OwnerID)
			}
			continue
		case "admin":
			mask = entity.GroupPermissionAddSubgroup | entity.GroupPermissionDeleteSubgroup |
				entity.GroupPermissionAddMember | entity.GroupPermissionRemoveMember |
				entity.GroupPermissionConfigureMemberPermissions
		case "member":
			continue
		default:
			return fmt.Errorf("group %s member %s has unsupported legacy role %q", row.GroupID, row.MemberID, row.Role)
		}
		grant := grants[key]
		if grant == nil {
			grant = &legacyGroupPermissionGrant{}
			grants[key] = grant
		}
		grant.mask |= mask
		grant.roles = append(grant.roles, role)
	}
	if len(grants) == 0 {
		return nil
	}
	keys := make([]legacyGroupPermissionKey, 0, len(grants))
	for key := range grants {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].tenantID != keys[j].tenantID {
			return keys[i].tenantID < keys[j].tenantID
		}
		if keys[i].groupID != keys[j].groupID {
			return keys[i].groupID < keys[j].groupID
		}
		return keys[i].memberID < keys[j].memberID
	})
	for _, key := range keys {
		grant := grants[key]
		var existing []entity.Permission
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND resource_type = ? AND resource_id = ? AND member_id = ? AND (group_id IS NULL OR group_id = '') AND status = ?",
				key.tenantID, "group", key.groupID, key.memberID, string(entity.StatusValid)).
			Order("id ASC").Find(&existing).Error; err != nil {
			return fmt.Errorf("load existing group permissions for group %s member %s: %w", key.groupID, key.memberID, err)
		}
		var oldMask int32
		for _, row := range existing {
			oldMask |= row.Permission
		}
		newMask := oldMask | grant.mask
		if oldMask == newMask && len(existing) <= 1 {
			continue
		}
		resourceID := key.groupID
		status := string(entity.StatusValid)
		if len(existing) == 0 {
			if err := tx.Create(&entity.Permission{
				ID: common.GenerateUUID(), MemberID: &key.memberID, TenantID: key.tenantID,
				ResourceType: "group", ResourceID: &resourceID, Permission: newMask, Status: &status,
			}).Error; err != nil {
				return fmt.Errorf("create migrated group permissions for group %s member %s: %w", key.groupID, key.memberID, err)
			}
		} else {
			if err := tx.Model(&entity.Permission{}).Where("id = ?", existing[0].ID).
				Updates(map[string]interface{}{"permission": newMask, "status": status}).Error; err != nil {
				return fmt.Errorf("update migrated group permissions for group %s member %s: %w", key.groupID, key.memberID, err)
			}
			if len(existing) > 1 {
				duplicateIDs := make([]string, 0, len(existing)-1)
				for _, row := range existing[1:] {
					duplicateIDs = append(duplicateIDs, row.ID)
				}
				if err := tx.Model(&entity.Permission{}).Where("id IN ?", duplicateIDs).Update("status", string(entity.StatusInvalid)).Error; err != nil {
					return fmt.Errorf("remove duplicate migrated group permissions for group %s member %s: %w", key.groupID, key.memberID, err)
				}
			}
		}
		beforeData, err := json.Marshal(map[string]interface{}{"permission": oldMask, "legacy_roles": grant.roles})
		if err != nil {
			return fmt.Errorf("encode previous migrated group permissions: %w", err)
		}
		afterData, err := json.Marshal(map[string]interface{}{"permission": newMask, "migration": "legacy_group_member_roles"})
		if err != nil {
			return fmt.Errorf("encode migrated group permissions: %w", err)
		}
		if tx.Migrator().HasTable(&entity.PermissionChangeLog{}) {
			beforeValue, afterValue := string(beforeData), string(afterData)
			if err := tx.Create(&entity.PermissionChangeLog{
				ID: common.GenerateUUID(), TenantID: key.tenantID, TargetType: "member", TargetID: key.memberID,
				ResourceType: "group", ResourceID: key.groupID,
				OldPermission: oldMask, NewPermission: newMask, ActionType: "migrate",
				EventType: "group.member.permissions.migrated", ScopeType: "system",
				BeforeData: &beforeValue, AfterData: &afterValue, Status: &status,
			}).Error; err != nil {
				return fmt.Errorf("audit migrated group permissions for group %s member %s: %w", key.groupID, key.memberID, err)
			}
		}
	}
	return nil
}

func checkDepartmentGroups(ctx context.Context, db *gorm.DB) error {
	checks := []struct {
		query string
		label string
	}{
		{`SELECT COUNT(*) FROM department d WHERE d.parent_id <> d.id AND NOT EXISTS
			(SELECT 1 FROM department p WHERE p.id = d.parent_id AND p.tenant_id = d.tenant_id)`, "departments with missing or cross-tenant parents"},
		{`SELECT COUNT(*) FROM department d JOIN group_info g ON g.id = d.id WHERE
			g.tenant_id <> d.tenant_id OR g.owner_id <> d.owner_id OR COALESCE(g.name, '') <> d.name OR
			COALESCE(g.description, '') <> COALESCE(d.description, '') OR
			COALESCE(g.avatar, '') <> COALESCE(d.avatar, '') OR
			COALESCE(g.status, '') <> COALESCE(d.status, '') OR
			COALESCE(g.path, '') <> d.path OR COALESCE(g.formatted_path, '') <> d.formatted_path OR
			COALESCE(g.parent_id, '') <> CASE WHEN d.parent_id = d.id THEN '' ELSE d.parent_id END`, "conflicting department/group IDs"},
	}
	for _, check := range checks {
		if err := checkMigrationCount(ctx, db, check.query, check.label); err != nil {
			return err
		}
	}
	return nil
}

func checkDepartmentMembers(ctx context.Context, db *gorm.DB, groupMemberHasRole bool) error {
	conflictingMembersQuery := `SELECT COUNT(*) FROM department_member d JOIN group_member g ON g.id = d.id WHERE
		g.group_id <> d.department_id OR g.member_id <> d.member_id OR
		COALESCE(g.status, '') <> COALESCE(d.status, '')`
	if groupMemberHasRole {
		conflictingMembersQuery = `SELECT COUNT(*) FROM department_member d JOIN group_member g ON g.id = d.id WHERE
			g.group_id <> d.department_id OR g.member_id <> d.member_id OR g.role <> d.role OR
			COALESCE(g.status, '') <> COALESCE(d.status, '')`
	}
	checks := []struct {
		query string
		label string
	}{
		{`SELECT COUNT(*) FROM department_member d LEFT JOIN group_info g ON g.id = d.department_id
			WHERE g.id IS NULL`, "department members without migrated groups"},
		{conflictingMembersQuery, "conflicting department/group member IDs"},
		{`SELECT COUNT(*) FROM department_member d JOIN group_member g
			ON g.group_id = d.department_id AND g.member_id = d.member_id WHERE g.id <> d.id`, "duplicate group memberships"},
	}
	for _, check := range checks {
		if err := checkMigrationCount(ctx, db, check.query, check.label); err != nil {
			return err
		}
	}
	return nil
}

func checkDepartmentPermissions(ctx context.Context, db *gorm.DB, hasDepartments bool) error {
	if !hasDepartments {
		return checkMigrationCount(ctx, db,
			`SELECT COUNT(*) FROM permission WHERE department_id IS NOT NULL AND department_id <> ''`,
			"department grants without department table")
	}
	checks := []struct {
		query string
		label string
	}{
		{`SELECT COUNT(*) FROM permission p LEFT JOIN department d ON d.id = p.department_id
			WHERE p.department_id IS NOT NULL AND p.department_id <> '' AND d.id IS NULL`, "grants with unknown departments"},
		{`SELECT COUNT(*) FROM permission p JOIN department d ON d.id = p.department_id
			WHERE p.tenant_id <> d.tenant_id`, "grants crossing department tenants"},
		{`SELECT COUNT(*) FROM permission WHERE department_id IS NOT NULL AND department_id <> ''
			AND group_id IS NOT NULL AND group_id <> department_id`, "grants with conflicting group and department"},
	}
	for _, check := range checks {
		if err := checkMigrationCount(ctx, db, check.query, check.label); err != nil {
			return err
		}
	}
	return nil
}

func checkMigrationCount(ctx context.Context, db *gorm.DB, query, label string) error {
	var count int64
	if err := db.WithContext(ctx).Raw(query).Scan(&count).Error; err != nil {
		return fmt.Errorf("check %s: %w", label, err)
	}
	if count != 0 {
		return fmt.Errorf("cannot migrate: %d %s", count, label)
	}
	return nil
}
