//
// Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package dao

import (
	"context"
	"errors"
	"fmt"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/entity"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	legacyModelProviderResourceType = "llm"
	modelInstanceResourceType       = "model_instance"
)

// migratePermissionDataEE migrates enterprise permission data after schema setup.
func migratePermissionDataEE(ctx context.Context, db *gorm.DB) error {
	return migrateGroups(ctx, db)
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
	if hasMembers && !hasDepartments {
		var count int64
		if err := db.WithContext(ctx).Table("department_member").Count(&count).Error; err != nil {
			return fmt.Errorf("count department members: %w", err)
		}
		if count != 0 {
			return fmt.Errorf("department_member has %d rows without department table", count)
		}
	}
	hasDepartmentPermission := migrator.HasTable("permission") && migrator.HasColumn(&entity.Permission{}, "department_id")
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
			if err := checkDepartmentMembers(ctx, tx); err != nil {
				return err
			}
			if err := tx.WithContext(ctx).Exec(`INSERT INTO group_member
				(id, create_time, create_date, update_time, update_date, member_id, group_id, role, status)
				SELECT d.id, d.create_time, d.create_date, d.update_time, d.update_date,
				 d.member_id, d.department_id, d.role, d.status
				FROM department_member d WHERE NOT EXISTS (SELECT 1 FROM group_member g WHERE g.id = d.id)`).Error; err != nil {
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
		return nil
	}); err != nil {
		return err
	}
	if hasDepartmentPermission {
		if err := migrator.DropColumn(&entity.Permission{}, "department_id"); err != nil {
			return fmt.Errorf("drop permission.department_id: %w", err)
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

func checkDepartmentMembers(ctx context.Context, db *gorm.DB) error {
	checks := []struct {
		query string
		label string
	}{
		{`SELECT COUNT(*) FROM department_member d LEFT JOIN group_info g ON g.id = d.department_id
			WHERE g.id IS NULL`, "department members without migrated groups"},
		{`SELECT COUNT(*) FROM department_member d JOIN group_member g ON g.id = d.id WHERE
			g.group_id <> d.department_id OR g.member_id <> d.member_id OR g.role <> d.role OR
			COALESCE(g.status, '') <> COALESCE(d.status, '')`, "conflicting department/group member IDs"},
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
