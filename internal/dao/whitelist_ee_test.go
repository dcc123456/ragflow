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

//go:build ee

package dao

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/entity"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/gorm"
)

func TestWhitelistSchemaPreservesHistoricalRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "whitelist.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE white_list (id integer PRIMARY KEY, email text NOT NULL, create_time integer, create_date datetime, update_time integer, update_date datetime)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO white_list(id,email,create_time,update_time) VALUES (1,' ALICE@Example.com ',101,102),(2,'bob@example.com',201,202),(3,'alice@example.com',301,302)`).Error; err != nil {
		t.Fatal(err)
	}
	var original []entity.WhiteList
	if err := db.Order("id").Find(&original).Error; err != nil {
		t.Fatal(err)
	}
	for run := 0; run < 2; run++ {
		if err := ensureWhitelistSchema(t.Context(), db); err != nil {
			t.Fatal(err)
		}
		var entries []entity.WhiteList
		if err := db.Order("id").Find(&entries).Error; err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(entries, original) {
			t.Fatalf("schema preparation changed historical rows: %+v", entries)
		}
	}
	if err := db.Create(&entity.WhiteList{Email: "alice@example.com"}).Error; err == nil {
		t.Fatal("duplicate email was accepted after migration")
	}
}

func TestWhitelistSchemaPreservesEquivalentUniqueIndex(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "whitelist.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE white_list (id integer PRIMARY KEY, email text NOT NULL, create_time integer, create_date datetime, update_time integer, update_date datetime)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX existing_whitelist_email ON white_list(email)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := ensureWhitelistSchema(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasIndex(&entity.WhiteList{}, "idx_white_list_email") {
		t.Fatal("migration created a redundant unique index")
	}
}

func setupLegacyWhitelistMigrationTest(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "whitelist.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	if err := db.Exec(`CREATE TABLE white_list (id integer PRIMARY KEY, email text COLLATE NOCASE NOT NULL, create_time integer, create_date datetime, update_time integer, update_date datetime)`).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func TestWhitelistSchemaPreservesInvalidRows(t *testing.T) {
	for _, existingIndex := range []bool{false, true} {
		name := "without unique index"
		if existingIndex {
			name = "with unique index"
		}
		t.Run(name, func(t *testing.T) {
			db := setupLegacyWhitelistMigrationTest(t)
			if err := db.Exec(`INSERT INTO white_list(id,email,create_time,update_time) VALUES
				(1,' ALICE@Example.com ',101,102),(2,'bob@example.com',201,202),(3,'alice@example.com',301,302),
				(4,' NOT-AN-EMAIL ',401,402),(5,'Alice <alice@example.com>',501,502),(6,'',601,602),
				(7,'a@b.c',701,702),(8,'alice+tag@example.com',801,802)`).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec(`UPDATE white_list SET create_date = '2020-01-01 00:00:00', update_date = '2020-01-02 00:00:00' WHERE id >= 4`).Error; err != nil {
				t.Fatal(err)
			}
			if existingIndex {
				if err := db.Exec(`CREATE UNIQUE INDEX existing_whitelist_email ON white_list(email)`).Error; err != nil {
					t.Fatal(err)
				}
			}
			var original []entity.WhiteList
			if err := db.Order("id").Find(&original).Error; err != nil {
				t.Fatal(err)
			}
			core, logs := observer.New(zap.WarnLevel)
			previous := common.Logger
			common.Logger = zap.New(core)
			t.Cleanup(func() { common.Logger = previous })
			for run := 0; run < 2; run++ {
				if err := ensureWhitelistSchema(t.Context(), db); err != nil {
					t.Fatalf("migration %d rejected invalid historical rows: %v", run, err)
				}
				var entries []entity.WhiteList
				if err := db.Order("id").Find(&entries).Error; err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(entries, original) {
					t.Fatalf("historical rows changed: got %+v, want %+v", entries, original)
				}
			}
			for _, entry := range original {
				if entry.ID == 2 || entry.ID == 3 {
					continue
				}
				warnings := logs.FilterField(zap.Int64("id", entry.ID)).All()
				if len(warnings) != 2 {
					t.Fatalf("row %d has %d warnings, want one per migration", entry.ID, len(warnings))
				}
				for _, warning := range warnings {
					if warning.ContextMap()["email"] != entry.Email || warning.ContextMap()["error"] == nil || !strings.Contains(warning.Message, "preserved") {
						t.Fatalf("warning omitted the preserved row or reason: %+v", warning)
					}
				}
			}
			if err := db.Exec(`INSERT INTO white_list(email) VALUES ('alice@example.com')`).Error; err == nil {
				t.Fatal("migration lost email uniqueness")
			}
		})
	}
}

func TestWhitelistSchemaReportsIndexConflicts(t *testing.T) {
	for _, emails := range [][2]string{{"invalid", "invalid"}, {"INVALID", "invalid"}, {"Alice@example.com", "alice@example.com"}} {
		t.Run(emails[0]+" and "+emails[1], func(t *testing.T) {
			db := setupLegacyWhitelistMigrationTest(t)
			if err := db.Exec(`INSERT INTO white_list(id,email,create_time,update_time) VALUES (1,?,101,102),(2,?,201,202)`, emails[0], emails[1]).Error; err != nil {
				t.Fatal(err)
			}
			var original []entity.WhiteList
			if err := db.Order("id").Find(&original).Error; err != nil {
				t.Fatal(err)
			}
			err := ensureWhitelistSchema(t.Context(), db)
			if err == nil || !strings.Contains(err.Error(), "unique index") || !strings.Contains(err.Error(), "rerun --migrate") {
				t.Fatalf("index conflict lacks repair guidance: %v", err)
			}
			const prefix = "inspect conflicting rows with: "
			_, query, found := strings.Cut(err.Error(), prefix)
			if !found {
				t.Fatalf("index conflict omitted diagnostic SQL: %v", err)
			}
			var conflicts []entity.WhiteList
			if err := db.Raw(query).Scan(&conflicts).Error; err != nil {
				t.Fatalf("diagnostic SQL is not executable: %v", err)
			}
			if len(conflicts) != 2 || conflicts[0].ID != 1 || conflicts[1].ID != 2 || conflicts[0].Email != emails[0] || conflicts[1].Email != emails[1] {
				t.Fatalf("diagnostic SQL did not locate the conflicting rows: %+v", conflicts)
			}
			var entries []entity.WhiteList
			if err := db.Order("id").Find(&entries).Error; err != nil || !reflect.DeepEqual(entries, original) {
				t.Fatalf("index failure changed invalid rows: %+v, error = %v", entries, err)
			}
		})
	}
}

func TestWhitelistSettingDefaultsOff(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "settings.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&entity.SystemSettings{}); err != nil {
		t.Fatal(err)
	}
	if enabled, err := WhitelistEnabled(t.Context(), db); err != nil || enabled {
		t.Fatalf("missing setting = (%v, %v), want disabled", enabled, err)
	}
	if err := ensureWhitelistSetting(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.SystemSettings{}).Where("name = ?", WhitelistEnabledSettingName).Update("value", "true").Error; err != nil {
		t.Fatal(err)
	}
	if err := ensureWhitelistSetting(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if enabled, err := WhitelistEnabled(t.Context(), db); err != nil || !enabled {
		t.Fatalf("initialization overwrote the switch: (%v, %v)", enabled, err)
	}
}
