//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

//go:build ee

package dao

import (
	"net/url"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// openEmptyTestDB opens a shared in-memory database with no tables, so a test
// can build exactly the shape it needs to check.
func openEmptyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+url.QueryEscape(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("access sqlite handle: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

// TestRequireDatasetVersionColumnEENamesTheCause pins the startup check that
// replaces a first-write "unknown column" failure: a database without the
// column, or without the table at all, has to be reported as needing
// --migrate, and a migrated database has to pass.
func TestRequireDatasetVersionColumnEENamesTheCause(t *testing.T) {
	t.Run("missing table", func(t *testing.T) {
		db := openEmptyTestDB(t)
		err := requireDatasetVersionColumnEE(t.Context(), db)
		if err == nil {
			t.Fatal("a database without the knowledgebase table must be reported")
		}
	})

	t.Run("missing column", func(t *testing.T) {
		db := openEmptyTestDB(t)
		if err := db.Exec("CREATE TABLE knowledgebase (id varchar(32) PRIMARY KEY)").Error; err != nil {
			t.Fatalf("create knowledgebase: %v", err)
		}
		err := requireDatasetVersionColumnEE(t.Context(), db)
		if err == nil {
			t.Fatal("a knowledgebase without the version column must be reported")
		}
	})

	t.Run("missing restore marker", func(t *testing.T) {
		db := openEmptyTestDB(t)
		if err := db.Exec("CREATE TABLE knowledgebase (id varchar(32) PRIMARY KEY)").Error; err != nil {
			t.Fatalf("create knowledgebase: %v", err)
		}
		if err := db.Exec("ALTER TABLE knowledgebase ADD COLUMN version bigint NOT NULL DEFAULT 0").Error; err != nil {
			t.Fatalf("add version column: %v", err)
		}
		if err := db.Exec("CREATE TABLE document (id varchar(32) PRIMARY KEY)").Error; err != nil {
			t.Fatalf("create document: %v", err)
		}
		err := requireDatasetVersionColumnEE(t.Context(), db)
		if err == nil {
			t.Fatal("a document without the restored_version column must be reported")
		}
		if !strings.Contains(err.Error(), "restored_version") {
			t.Fatalf("error = %v, want it to name document.restored_version", err)
		}
	})

	t.Run("migrated", func(t *testing.T) {
		db := openEmptyTestDB(t)
		if err := db.Exec("CREATE TABLE knowledgebase (id varchar(32) PRIMARY KEY)").Error; err != nil {
			t.Fatalf("create knowledgebase: %v", err)
		}
		if err := db.Exec("ALTER TABLE knowledgebase ADD COLUMN version bigint NOT NULL DEFAULT 0").Error; err != nil {
			t.Fatalf("add version column: %v", err)
		}
		if err := db.Exec("CREATE TABLE document (id varchar(32) PRIMARY KEY)").Error; err != nil {
			t.Fatalf("create document: %v", err)
		}
		if err := db.Exec("ALTER TABLE document ADD COLUMN restored_version bigint NULL").Error; err != nil {
			t.Fatalf("add restored_version column: %v", err)
		}
		if err := requireDatasetVersionColumnEE(t.Context(), db); err != nil {
			t.Fatalf("a migrated database must pass: %v", err)
		}
	})
}
