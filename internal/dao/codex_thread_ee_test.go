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
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"ragflow/internal/codexagent"
	"ragflow/internal/entity"
)

func setupCodexThreadTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&entity.CodexThreadMap{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestCodexThreadStoreCRUD(t *testing.T) {
	db := setupCodexThreadTestDB(t)
	store := NewCodexThreadStore(db)
	ctx := context.Background()

	if _, found, err := store.Get(ctx, "t1", "s1"); err != nil || found {
		t.Fatalf("missing record: found=%v err=%v", found, err)
	}

	rec := codexagent.ThreadRecord{
		TenantID:           "t1",
		SessionID:          "s1",
		ThreadID:           "thread-a",
		ScopeFingerprint:   "scope1",
		HistoryFingerprint: "hist1",
		Ticket:             "tok1",
	}
	if err := store.Put(ctx, rec); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, found, err := store.Get(ctx, "t1", "s1")
	if err != nil || !found {
		t.Fatalf("get after put: found=%v err=%v", found, err)
	}
	if got.ThreadID != "thread-a" || got.ScopeFingerprint != "scope1" || got.Ticket != "tok1" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	// Upsert: same key, new thread -> overwrites in place (one row).
	rec.ThreadID = "thread-b"
	rec.Ticket = "tok2"
	if err := store.Put(ctx, rec); err != nil {
		t.Fatalf("second put: %v", err)
	}
	got, _, _ = store.Get(ctx, "t1", "s1")
	if got.ThreadID != "thread-b" || got.Ticket != "tok2" {
		t.Fatalf("upsert did not overwrite: %+v", got)
	}
	var count int64
	if err := db.Model(&entity.CodexThreadMap{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("upsert created %d rows, want 1", count)
	}

	if err := store.Delete(ctx, "t1", "s1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, found, _ := store.Get(ctx, "t1", "s1"); found {
		t.Fatal("record survived delete")
	}
}
