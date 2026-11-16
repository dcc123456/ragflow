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

package compliance

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"
	"ragflow/internal/storage"
	"ragflow/internal/tokenizer"
)

func TestSensitiveWordMatching(t *testing.T) {
	for _, tc := range []struct {
		name   string
		words  string
		tokens string
		want   bool
	}{
		{"single word", "\ufeff  BLOCKED \r\n\nblocked\n", "a blocked question", true},
		{"word boundary", "block", "a blocked question", false},
		{"spaced phrase", "restricted phrase", "a restricted phrase here", true},
		{"joined phrase", "restrictedphrase", "a restricted phrase here", true},
		{"unicode joined", "敏感词", "这是 敏感 词", true},
		{"unicode spaced", "敏感 词", "这是 敏感 词", true},
		{"five tokens", "one two three four five", "one two three four five six", true},
		{"six tokens", "one two three four five six", "one two three four five six", false},
		{"empty dictionary", " \r\n", "anything", false},
		{"empty tokens", "blocked", "", false},
		{"unicode line endings", "safe\u2028blocked\u0085other", "blocked", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := matches(dictionary([]byte(tc.words)), strings.Split(tc.tokens, " ")); got != tc.want {
				t.Fatalf("match = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSensitiveWordReload(t *testing.T) {
	db := testutil.SetupTestDB(t, &entity.SystemSettings{})
	store := storage.NewMemoryStorage()
	ctx := t.Context()
	tokenizer.SetEngineType("infinity")
	t.Cleanup(func() { tokenizer.SetEngineType("") })
	settings := dao.NewSystemSettingsDAO()
	set := func(name, value string) {
		t.Helper()
		if err := settings.SaveOrCreate(ctx, db, name, value, "variable", entity.InferSystemSettingDataType(name)); err != nil {
			t.Fatal(err)
		}
	}
	put := func(content string) {
		t.Helper()
		if err := store.Put(ctx, Bucket, ObjectKey, []byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	var f filter
	check := func(question, want string) {
		t.Helper()
		err := f.check(ctx, db, store, question)
		if want == "" {
			if err != nil {
				t.Fatalf("check(%q) = %v, want nil", question, err)
			}
		} else if err == nil || err.Error() != want {
			t.Fatalf("check(%q) = %v, want error %q", question, err, want)
		}
	}
	expire := func() {
		f.mu.Lock()
		f.state.nextReload = time.Time{}
		f.mu.Unlock()
	}
	put("blocked")
	check("blocked", "")
	set(EnabledSetting, "true")
	set(FileVersionSetting, "v1")
	check("blocked", "") // Settings remain cached until the interval expires.
	expire()
	check("blocked", DefaultResponse)
	check("ordinary question", "")
	put("replacement")
	expire()
	check("blocked", DefaultResponse) // A matching version does not re-read storage.
	set(ResponseSetting, "Custom rejection")
	set(FileVersionSetting, "v2")
	expire()
	check("blocked", "")
	check("replacement", "Custom rejection")
	set(FileVersionSetting, "v3")
	if err := store.Remove(ctx, Bucket, ObjectKey); err != nil {
		t.Fatal(err)
	}
	expire()
	check("replacement", "Custom rejection") // Keep the last working dictionary on load failure.
	put("\xff")
	expire()
	check("replacement", "Custom rejection")
	put("retried")
	expire()
	check("retried", "Custom rejection")
	set(EnabledSetting, "false")
	expire()
	check("retried", "")
	set(EnabledSetting, "true")
	set(ResponseSetting, "")
	expire()
	check("retried", DefaultResponse)
	put("")
	set(FileVersionSetting, "empty")
	expire()
	check("retried", DefaultResponse)
	put(" \n")
	expire()
	check("retried", "")
	put("concurrent")
	set(FileVersionSetting, "concurrent")
	expire()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := f.check(ctx, db, store, "concurrent")
			if err == nil || err.Error() != DefaultResponse {
				t.Errorf("concurrent check = %v, want error %q", err, DefaultResponse)
			}
		}()
	}
	wg.Wait()
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := f.check(canceled, db, store, "concurrent"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled check = %v", err)
	}
}
