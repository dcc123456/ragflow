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
	"strings"
	"testing"

	"ragflow/internal/entity"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestAutoMigrateRoleTablesPreservesLegacyData(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.Exec("CREATE TABLE role (id integer PRIMARY KEY, role_name varchar(64) NOT NULL, description text)").Error; err != nil {
		t.Fatalf("create existing role table: %v", err)
	}
	if err := db.Exec("CREATE TABLE role_resource (id integer PRIMARY KEY, role_id integer NOT NULL, resource_type bigint NOT NULL, action integer NOT NULL)").Error; err != nil {
		t.Fatalf("create existing role resource table: %v", err)
	}
	if err := db.Exec("INSERT INTO role (id, role_name) VALUES (1, 'owner')").Error; err != nil {
		t.Fatalf("insert role: %v", err)
	}
	if err := db.Exec("INSERT INTO role_resource (id, role_id, resource_type, action) VALUES (1, 1, 2, 15)").Error; err != nil {
		t.Fatalf("insert role resource: %v", err)
	}
	if err := db.AutoMigrate(&entity.Role{}, &entity.RoleResource{}); err != nil {
		t.Fatalf("auto-migrate existing role tables: %v", err)
	}
	var role entity.Role
	if err := db.Take(&role, "id = ?", 1).Error; err != nil {
		t.Fatalf("read role: %v", err)
	}
	if role.TenantID != nil || role.RoleName != "owner" {
		t.Fatalf("existing role changed: %+v", role)
	}
	var resource entity.RoleResource
	if err := db.Take(&resource, "id = ?", 1).Error; err != nil {
		t.Fatalf("read role resource: %v", err)
	}
	if resource.RoleID != 1 || resource.ResourceType != 2 || resource.Action != 15 {
		t.Fatalf("existing role resource changed: %+v", resource)
	}
	if err := db.Exec("UPDATE role SET tenant_id = ? WHERE id = ?", "tenant-1", 1).Error; err != nil {
		t.Fatalf("assign role tenant: %v", err)
	}
	if err := db.AutoMigrate(&entity.Role{}, &entity.RoleResource{}); err != nil {
		t.Fatalf("repeat role auto-migration: %v", err)
	}
	if err := db.Take(&role, "id = ?", 1).Error; err != nil {
		t.Fatalf("read role after rerun: %v", err)
	}
	if role.TenantID == nil || *role.TenantID != "tenant-1" {
		t.Fatalf("role tenant_id = %v, want tenant-1", role.TenantID)
	}
}

func TestAutoMigrateCreatesRoleTables(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.AutoMigrate(&entity.Role{}, &entity.RoleResource{}); err != nil {
		t.Fatalf("auto-migrate role tables: %v", err)
	}
	if !db.Migrator().HasColumn(&entity.Role{}, "TenantID") || !db.Migrator().HasTable(&entity.RoleResource{}) {
		t.Fatal("role tables were not created")
	}
}
func openGroupMigrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	return db
}

func createLegacyDepartmentTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	autoMigrateGroupTables(t, db)
	for _, statement := range []string{
		`CREATE TABLE department (id varchar(32) PRIMARY KEY,
			create_time bigint, create_date datetime, update_time bigint, update_date datetime,
			name varchar(100) NOT NULL,
			description text, avatar text, path varchar(255) NOT NULL, formatted_path varchar(255) NOT NULL,
			parent_id varchar(32) NOT NULL, owner_id varchar(32) NOT NULL, tenant_id varchar(32) NOT NULL,
			status varchar(1))`,
		`CREATE TABLE department_member (id varchar(32) PRIMARY KEY,
			create_time bigint, create_date datetime, update_time bigint, update_date datetime,
			member_id varchar(32) NOT NULL,
			department_id varchar(32) NOT NULL, role varchar(32) NOT NULL, status varchar(1))`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create old table: %v", err)
		}
	}
	if err := db.Exec(`ALTER TABLE permission ADD COLUMN "department_id" varchar(32)`).Error; err != nil {
		t.Fatalf("add legacy permission.department_id: %v", err)
	}
}

func autoMigrateGroupTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.AutoMigrate(&entity.GroupInfo{}, &entity.GroupMember{}, &entity.Permission{}); err != nil {
		t.Fatalf("auto-migrate group and permission tables: %v", err)
	}
}

func migrateGroupsAfterAutoMigrate(t *testing.T, db *gorm.DB) error {
	t.Helper()
	return migrateGroups(t.Context(), db)
}

func TestMigrateGroups(t *testing.T) {
	db := openGroupMigrationDB(t)
	createLegacyDepartmentTables(t, db)
	for _, statement := range []string{
		`INSERT INTO group_info (id, name, owner_id, tenant_id, status)
			VALUES ('existing', 'Existing', 'owner-1', 'tenant-1', '1')`,
		`INSERT INTO department (id, name, description, path, formatted_path, parent_id, owner_id, tenant_id, status)
			VALUES ('root', 'Root', 'kept', '/root/', '/Root/', 'root', 'owner-1', 'tenant-1', '1')`,
		`INSERT INTO department (id, name, path, formatted_path, parent_id, owner_id, tenant_id, status)
			VALUES ('child', 'Child', '/root/child/', '/Root/Child/', 'root', 'owner-1', 'tenant-1', '1')`,
		`INSERT INTO department_member (id, member_id, department_id, role, status)
			VALUES ('member-1', 'user-tenant-1', 'child', 'admin', '1')`,
		`INSERT INTO permission (id, tenant_id, department_id, resource_type, resource_id, permission)
			VALUES ('grant-1', 'tenant-1', 'child', 'kb', 'kb-1', 4)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("insert old data: %v", err)
		}
	}
	if err := migrateGroupsAfterAutoMigrate(t, db); err != nil {
		t.Fatalf("migrate groups: %v", err)
	}
	var root, child, existing entity.GroupInfo
	for _, pair := range []struct {
		id    string
		group *entity.GroupInfo
	}{{"root", &root}, {"child", &child}, {"existing", &existing}} {
		if err := db.Take(pair.group, "id = ?", pair.id).Error; err != nil {
			t.Fatalf("read group %s: %v", pair.id, err)
		}
	}
	if root.ParentID != nil || root.Path == nil || *root.Path != "/root/" ||
		root.Description == nil || *root.Description != "kept" {
		t.Fatalf("root department was not preserved: %+v", root)
	}
	if child.ParentID == nil || *child.ParentID != "root" || child.Path == nil || *child.Path != "/root/child/" {
		t.Fatalf("child hierarchy was not preserved: %+v", child)
	}
	if existing.Path == nil || *existing.Path != "/existing/" {
		t.Fatalf("existing group path was not set: %+v", existing)
	}
	var member entity.GroupMember
	if err := db.Take(&member, "id = ?", "member-1").Error; err != nil {
		t.Fatalf("read migrated member: %v", err)
	}
	if member.GroupID != "child" || member.MemberID != "user-tenant-1" || member.Role != "admin" {
		t.Fatalf("member data changed: %+v", member)
	}
	var grant struct{ GroupID *string }
	if err := db.Table("permission").Select("group_id").Where("id = ?", "grant-1").Take(&grant).Error; err != nil {
		t.Fatalf("read migrated grant: %v", err)
	}
	if grant.GroupID == nil || *grant.GroupID != "child" {
		t.Fatalf("grant group_id = %v, want child", grant.GroupID)
	}
	if db.Migrator().HasTable("department") || db.Migrator().HasTable("department_member") ||
		db.Migrator().HasColumn(&entity.Permission{}, "department_id") {
		t.Fatalf("old department schema remains: department=%v department_member=%v permission.department_id=%v",
			db.Migrator().HasTable("department"), db.Migrator().HasTable("department_member"),
			db.Migrator().HasColumn(&entity.Permission{}, "department_id"))
	}
	if err := migrateGroupsAfterAutoMigrate(t, db); err != nil {
		t.Fatalf("repeat group migration: %v", err)
	}
}

func TestMigrateGroupsRejectsIDCollision(t *testing.T) {
	db := openGroupMigrationDB(t)
	createLegacyDepartmentTables(t, db)
	if err := db.Exec(`INSERT INTO group_info (id, name, owner_id, tenant_id)
		VALUES ('shared', 'Existing', 'owner-1', 'tenant-1')`).Error; err != nil {
		t.Fatalf("insert group: %v", err)
	}
	if err := db.Exec(`INSERT INTO department
		(id, name, path, formatted_path, parent_id, owner_id, tenant_id)
		VALUES ('shared', 'Different', '/shared/', '/Different/', 'shared', 'owner-1', 'tenant-1')`).Error; err != nil {
		t.Fatalf("insert department: %v", err)
	}
	err := migrateGroupsAfterAutoMigrate(t, db)
	if err == nil || !strings.Contains(err.Error(), "conflicting department/group IDs") {
		t.Fatalf("collision error = %v", err)
	}
	if !db.Migrator().HasTable("department") {
		t.Fatal("department table was removed after collision")
	}
}

func TestMigrateGroupsRejectsCrossTenantGrant(t *testing.T) {
	db := openGroupMigrationDB(t)
	createLegacyDepartmentTables(t, db)
	if err := db.Exec(`INSERT INTO department
		(id, name, path, formatted_path, parent_id, owner_id, tenant_id)
		VALUES ('dept-1', 'Team', '/dept-1/', '/Team/', 'dept-1', 'owner-1', 'tenant-1')`).Error; err != nil {
		t.Fatalf("insert department: %v", err)
	}
	if err := db.Exec(`INSERT INTO permission (id, tenant_id, department_id, resource_type, resource_id, permission)
		VALUES ('grant-1', 'tenant-2', 'dept-1', 'kb', 'kb-1', 4)`).Error; err != nil {
		t.Fatalf("insert grant: %v", err)
	}
	err := migrateGroupsAfterAutoMigrate(t, db)
	if err == nil || !strings.Contains(err.Error(), "grants crossing department tenants") {
		t.Fatalf("cross-tenant grant error = %v", err)
	}
	if !db.Migrator().HasTable("department") || !db.Migrator().HasColumn(&entity.Permission{}, "department_id") {
		t.Fatal("source data was removed after rejected grant")
	}
}

func TestAutoMigrateCreatesGroupTables(t *testing.T) {
	db := openGroupMigrationDB(t)
	autoMigrateGroupTables(t, db)
	if !db.Migrator().HasTable(&entity.GroupInfo{}) || !db.Migrator().HasTable(&entity.GroupMember{}) ||
		!db.Migrator().HasTable(&entity.Permission{}) {
		t.Fatal("group or permission tables were not created")
	}
}

func TestMigrateModelProviderSharesToInstances(t *testing.T) {
	db := openGroupMigrationDB(t)
	createLegacyDepartmentTables(t, db)
	if err := db.AutoMigrate(&entity.TenantModelProvider{}, &entity.TenantModelInstance{}); err != nil {
		t.Fatalf("create model tables: %v", err)
	}
	for _, provider := range []entity.TenantModelProvider{
		{ID: "provider-1", TenantID: "tenant-1", ProviderName: "openai"},
		{ID: "provider-2", TenantID: "tenant-1", ProviderName: "empty-provider"},
	} {
		if err := db.Create(&provider).Error; err != nil {
			t.Fatalf("insert provider %s: %v", provider.ID, err)
		}
	}
	for _, instance := range []entity.TenantModelInstance{
		{ID: "instance-1", ProviderID: "provider-1", InstanceName: "key-a", APIKey: "a"},
		{ID: "instance-2", ProviderID: "provider-1", InstanceName: "key-b", APIKey: "b"},
	} {
		if err := db.Create(&instance).Error; err != nil {
			t.Fatalf("insert instance %s: %v", instance.ID, err)
		}
	}
	for _, statement := range []string{
		`INSERT INTO permission (id, tenant_id, member_id, resource_type, resource_id, permission, status)
			VALUES ('provider-owner', 'tenant-1', 'member-1', 'llm', 'openai', 7, '1')`,
		`INSERT INTO permission (id, tenant_id, group_id, resource_type, resource_id, permission, status)
			VALUES ('provider-group', 'tenant-1', 'group-1', 'llm', 'openai', 2, '1')`,
		`INSERT INTO permission (id, tenant_id, member_id, resource_type, resource_id, permission, status)
			VALUES ('provider-id', 'tenant-1', 'member-id', 'llm', 'provider-1', 3, '1')`,
		`INSERT INTO permission (id, tenant_id, member_id, resource_type, resource_id, permission, status)
			VALUES ('provider-without-instance', 'tenant-1', 'member-2', 'llm', 'empty-provider', 1, '1')`,
		`INSERT INTO permission (id, tenant_id, member_id, resource_type, resource_id, permission, status)
			VALUES ('orphan-provider', 'tenant-1', 'member-3', 'llm', 'missing-provider', 1, '1')`,
		`INSERT INTO permission (id, tenant_id, member_id, resource_type, resource_id, permission, status)
			VALUES ('other-resource', 'tenant-1', 'member-1', 'kb', 'dataset-1', 4, '1')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("insert legacy permissions: %v", err)
		}
	}

	if err := migrateGroupsAfterAutoMigrate(t, db); err != nil {
		t.Fatalf("migrate provider shares: %v", err)
	}

	var migrated []struct {
		ResourceID string  `gorm:"column:resource_id"`
		MemberID   *string `gorm:"column:member_id"`
		GroupID    *string `gorm:"column:group_id"`
		Permission int64   `gorm:"column:permission"`
	}
	if err := db.Table("permission").Select("resource_id, member_id, group_id, permission").
		Where("resource_type = ?", "model_instance").Order("resource_id, id").Find(&migrated).Error; err != nil {
		t.Fatalf("read migrated instance permissions: %v", err)
	}
	if len(migrated) != 6 {
		t.Fatalf("migrated %d instance grants, want 6: %+v", len(migrated), migrated)
	}
	want := map[string]int64{
		"instance-1/member-1":  15,
		"instance-2/member-1":  15,
		"instance-1/group-1":   3,
		"instance-2/group-1":   3,
		"instance-1/member-id": 3,
		"instance-2/member-id": 3,
	}
	for _, grant := range migrated {
		subjectID := grant.MemberID
		if subjectID == nil {
			subjectID = grant.GroupID
		}
		if subjectID == nil {
			t.Fatalf("migrated grant has no subject: %+v", grant)
		}
		key := grant.ResourceID + "/" + *subjectID
		if grant.Permission != want[key] {
			t.Errorf("permission for %s = %d, want %d", key, grant.Permission, want[key])
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Errorf("missing expected instance grants: %v", want)
	}
	var oldProviderGrants int64
	if err := db.Table("permission").Where("resource_type = ?", "llm").Count(&oldProviderGrants).Error; err != nil {
		t.Fatalf("count old provider grants: %v", err)
	}
	if oldProviderGrants != 0 {
		t.Fatalf("legacy provider grants remain: %d", oldProviderGrants)
	}
	var unrelated int64
	if err := db.Table("permission").Where("id = ? AND resource_type = ? AND permission = ?", "other-resource", "kb", 4).Count(&unrelated).Error; err != nil {
		t.Fatalf("check unrelated grant: %v", err)
	}
	if unrelated != 1 {
		t.Fatal("migration changed an unrelated resource grant")
	}

	newInstance := entity.TenantModelInstance{ID: "instance-later", ProviderID: "provider-1", InstanceName: "key-c", APIKey: "c"}
	if err := db.Create(&newInstance).Error; err != nil {
		t.Fatalf("insert later instance: %v", err)
	}
	var laterGrants int64
	if err := db.Table("permission").Where("resource_type = ? AND resource_id = ?", "model_instance", "instance-later").Count(&laterGrants).Error; err != nil {
		t.Fatalf("check later instance grants: %v", err)
	}
	if laterGrants != 0 {
		t.Fatalf("new instance inherited %d grants, want none", laterGrants)
	}
	if err := migrateGroupsAfterAutoMigrate(t, db); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	var grantCount int64
	if err := db.Table("permission").Where("resource_type = ?", "model_instance").Count(&grantCount).Error; err != nil {
		t.Fatalf("count instance grants after rerun: %v", err)
	}
	if grantCount != 6 {
		t.Fatalf("repeat migration produced %d instance grants, want 6", grantCount)
	}
}

func TestLegacyPermissionMask(t *testing.T) {
	for _, test := range []struct {
		legacy int64
		mask   int64
	}{
		{legacy: 0, mask: 0},
		{legacy: 1, mask: 1},
		{legacy: 2, mask: 3},
		{legacy: 3, mask: 3},
		{legacy: 4, mask: 7},
		{legacy: 5, mask: 7},
		{legacy: 6, mask: 7},
		{legacy: 7, mask: 15},
	} {
		got, err := legacyPermissionMask(test.legacy)
		if err != nil || got != test.mask {
			t.Errorf("legacyPermissionMask(%d) = %d, %v; want %d", test.legacy, got, err, test.mask)
		}
	}
	if _, err := legacyPermissionMask(8); err == nil {
		t.Fatal("legacyPermissionMask accepted an out-of-range value")
	}
}
