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
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"ragflow/internal/entity"

	"github.com/glebarez/sqlite"
	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

func setupWhitelistStoreTest(t *testing.T) (*WhitelistDAO, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "whitelist.db")), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	if err := db.AutoMigrate(&entity.WhiteList{}); err != nil {
		t.Fatal(err)
	}
	return NewWhitelistDAO(), db
}

func TestWhitelistDAOOperations(t *testing.T) {
	store, db := setupWhitelistStoreTest(t)
	ctx := t.Context()
	entries, err := store.List(ctx, db)
	if err != nil || entries == nil || len(entries) != 0 {
		t.Fatalf("empty list = %v, error = %v", entries, err)
	}
	for run := 0; run < 2; run++ {
		added, err := store.InsertEmails(ctx, db, []string{"bob@example.com", "alice@example.com"})
		if err != nil || added != int64(2*(1-run)) {
			t.Fatalf("insert %d = (%d, %v)", run, added, err)
		}
	}
	entries, err = store.List(ctx, db)
	if err != nil || len(entries) != 2 || entries[0].Email != "alice@example.com" || entries[0].ID >= entries[1].ID {
		t.Fatalf("list = %+v, error = %v", entries, err)
	}
	if _, _, err := store.UpdateEmail(ctx, db, 999, "missing@example.com"); !errors.Is(err, entity.ErrWhitelistNotFound) {
		t.Fatalf("missing update = %v", err)
	}
	for _, id := range []int64{entries[0].ID, 999} {
		if _, updated, err := store.UpdateEmail(ctx, db, id, "bob@example.com"); err != nil || updated {
			t.Fatalf("existing email update for row %d = (%v, %v), want no-op", id, updated, err)
		}
	}
	var unchanged entity.WhiteList
	if err := db.Where("id = ?", entries[0].ID).Take(&unchanged).Error; err != nil || unchanged.Email != entries[0].Email || *unchanged.UpdateTime != *entries[0].UpdateTime {
		t.Fatalf("no-op changed the source row: %+v, error = %v", unchanged, err)
	}
	if previous, updated, err := store.UpdateEmail(ctx, db, entries[0].ID, "carol@example.com"); err != nil || !updated || previous != "alice@example.com" {
		t.Fatalf("new email update = (%q, %v, %v)", previous, updated, err)
	}
	for run := 0; run < 2; run++ {
		deleted, err := store.DeleteByEmail(ctx, db, "carol@example.com")
		if err != nil || deleted != int64(1-run) {
			t.Fatalf("delete %d = (%d, %v)", run, deleted, err)
		}
	}
	deleted, err := store.DeleteByIDs(ctx, db, []int64{entries[1].ID, entries[1].ID, 999})
	if err != nil || deleted != 1 {
		t.Fatalf("batch delete = (%d, %v)", deleted, err)
	}
	entries, err = store.List(ctx, db)
	if err != nil || entries == nil || len(entries) != 0 {
		t.Fatalf("final list = %v, error = %v", entries, err)
	}
}

func TestWhitelistDAOInsertRollsBackEarlierBatch(t *testing.T) {
	store, db := setupWhitelistStoreTest(t)
	if err := db.Exec("CREATE TRIGGER reject_whitelist_entry BEFORE INSERT ON white_list WHEN NEW.email = 'zz-reject@example.com' BEGIN SELECT RAISE(ABORT, 'test storage failure'); END").Error; err != nil {
		t.Fatal(err)
	}
	emails := make([]string, 251)
	for index := 0; index < 250; index++ {
		emails[index] = fmt.Sprintf("user%03d@example.com", index)
	}
	emails[250] = "zz-reject@example.com"
	added, err := store.InsertEmails(t.Context(), db, emails)
	if err == nil || added != 0 {
		t.Fatalf("failed insert = (%d, %v)", added, err)
	}
	entries, err := store.List(t.Context(), db)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed second batch retained %d rows, error = %v", len(entries), err)
	}
}

func TestWhitelistDAOUsesDatabaseEmailEquality(t *testing.T) {
	for _, caseSensitive := range []bool{true, false} {
		name := "case insensitive"
		if caseSensitive {
			name = "case sensitive"
		}
		t.Run(name, func(t *testing.T) {
			var store *WhitelistDAO
			var db *gorm.DB
			if caseSensitive {
				store, db = setupWhitelistStoreTest(t)
			} else {
				db = setupLegacyWhitelistMigrationTest(t)
				if err := ensureWhitelistSchema(t.Context(), db); err != nil {
					t.Fatal(err)
				}
				store = NewWhitelistDAO()
			}
			const original = "Alice@Example.COM"
			const variant = "alice@example.com"
			if added, err := store.InsertEmails(t.Context(), db, []string{original}); err != nil || added != 1 {
				t.Fatalf("initial insert = (%d, %v)", added, err)
			}
			if allowed, err := WhitelistContains(t.Context(), db, variant); err != nil || allowed == caseSensitive {
				t.Fatalf("case variant membership = (%v, %v), case sensitive %v", allowed, err, caseSensitive)
			}
			wantAdded := int64(0)
			if caseSensitive {
				wantAdded = 1
			}
			if added, err := store.InsertEmails(t.Context(), db, []string{variant}); err != nil || added != wantAdded {
				t.Fatalf("variant insert = (%d, %v), want %d", added, err, wantAdded)
			}
			entries, err := store.List(t.Context(), db)
			if err != nil || len(entries) != int(1+wantAdded) || entries[0].Email != original {
				t.Fatalf("stored spelling changed: %+v, error = %v", entries, err)
			}
			if _, updated, err := store.UpdateEmail(t.Context(), db, 999, variant); err != nil || updated {
				t.Fatalf("existing email update = (%v, %v), want no-op", updated, err)
			}
			if deleted, err := store.DeleteByEmail(t.Context(), db, variant); err != nil || deleted != 1 {
				t.Fatalf("variant delete = (%d, %v), want one database match", deleted, err)
			}
			if allowed, err := WhitelistContains(t.Context(), db, original); err != nil || allowed != caseSensitive {
				t.Fatalf("membership after variant delete = (%v, %v), case sensitive %v", allowed, err, caseSensitive)
			}
		})
	}
}

func TestWhitelistDAOStorageErrors(t *testing.T) {
	for _, test := range []struct{ input, want error }{
		{gorm.ErrRecordNotFound, entity.ErrWhitelistNotFound},
		{gorm.ErrDuplicatedKey, entity.ErrWhitelistEmailConflict},
		{&mysqldriver.MySQLError{Number: 1062}, entity.ErrWhitelistEmailConflict},
	} {
		if err := whitelistStorageError(fmt.Errorf("driver: %w", test.input)); !errors.Is(err, test.want) {
			t.Fatalf("translated error = %v, want %v", err, test.want)
		}
	}
	failure := errors.New("connection lost")
	if err := whitelistStorageError(failure); !errors.Is(err, failure) {
		t.Fatalf("masked storage error: %v", err)
	}
}
