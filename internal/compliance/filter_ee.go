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
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/storage"
	"ragflow/internal/tokenizer"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	Bucket             = "ragflow-system"
	ObjectKey          = "sensitive_word_filter/compliance.txt"
	EnabledSetting     = "sensitive_word_filter.enabled"
	ResponseSetting    = "sensitive_word_filter.response"
	FileVersionSetting = "sensitive_word_filter.file_version"
	FileNameSetting    = "sensitive_word_filter.file_name"
	MaxFileSize        = 30 * 1024 * 1024
	DefaultResponse    = "Your request could not be processed because it contains restricted content. Please modify your input and try again."
	reloadInterval     = 60 * time.Second
)

type filterState struct {
	enabled    bool
	response   string
	version    string
	loaded     bool
	dictionary map[string]struct{}
	nextReload time.Time
}

type filter struct {
	mu      sync.Mutex
	loading chan struct{}
	db      *gorm.DB
	state   filterState
}

var questionFilter filter

// ValidateQuestion returns an error with the configured response when a question matches.
func ValidateQuestion(ctx context.Context, question string) error {
	if dao.DB == nil {
		return ctx.Err()
	}
	return questionFilter.check(ctx, dao.DB, storage.GetStorageFactory().GetStorage(), question)
}

func (f *filter) check(ctx context.Context, db *gorm.DB, store storage.Storage, question string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	state := f.state
	if f.db != db || !time.Now().Before(state.nextReload) {
		if f.loading != nil {
			loading := f.loading
			f.mu.Unlock()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-loading:
				return f.check(ctx, db, store, question)
			}
		}
		if f.db != db {
			state = filterState{}
		}
		f.loading = make(chan struct{})
		f.mu.Unlock()
		state = f.reload(ctx, db, store, state)
		f.mu.Lock()
		if ctx.Err() != nil {
			state.nextReload = time.Time{}
		}
		f.state = state
		f.db = db
		close(f.loading)
		f.loading = nil
	}
	f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !state.enabled || len(state.dictionary) == 0 || question == "" {
		return nil
	}
	tokens, err := tokenizer.Tokenize(question)
	if err != nil {
		return fmt.Errorf("tokenize sensitive word filter question: %w", err)
	}
	if matches(state.dictionary, strings.Split(tokens, " ")) {
		return errors.New(state.response)
	}
	return nil
}

func (f *filter) reload(ctx context.Context, db *gorm.DB, store storage.Storage, state filterState) filterState {
	state.nextReload = time.Now().Add(reloadInterval)
	settings, err := dao.NewSystemSettingsDAO().GetByNamePrefix(ctx, db, "sensitive_word_filter.")
	if err != nil {
		common.Warn("Failed to load sensitive word filter settings", zap.Error(err))
		return state
	}
	state.enabled = false
	state.response = DefaultResponse
	version := ""
	for _, setting := range settings {
		switch setting.Name {
		case EnabledSetting:
			state.enabled = setting.Value == "true"
		case ResponseSetting:
			if setting.Value != "" {
				state.response = setting.Value
			}
		case FileVersionSetting:
			version = setting.Value
		}
	}
	if !state.enabled {
		state.dictionary = nil
		state.loaded = false
		state.version = ""
		return state
	}
	if state.loaded && state.version == version {
		return state
	}
	if store == nil {
		common.Warn("Sensitive word filter storage is not initialized")
		return state
	}
	content, err := store.Get(ctx, Bucket, ObjectKey)
	if err != nil {
		common.Warn("Failed to load sensitive word filter file", zap.Error(err))
		return state
	}
	if len(content) == 0 {
		return state
	}
	if !utf8.Valid(content) || len(content) > MaxFileSize {
		common.Warn("Sensitive word filter file must be UTF-8 and at most 30 MiB")
		return state
	}
	state.dictionary = dictionary(content)
	state.loaded = true
	state.version = version
	return state
}

func dictionary(content []byte) map[string]struct{} {
	words := make(map[string]struct{})
	for _, line := range SplitLines(strings.TrimPrefix(string(content), "\ufeff")) {
		if word := strings.ToLower(strings.TrimSpace(line)); word != "" {
			words[word] = struct{}{}
		}
	}
	return words
}

// SplitLines recognizes the Unicode line boundaries accepted by uploaded word lists.
func SplitLines(content string) []string {
	return strings.FieldsFunc(content, func(r rune) bool {
		return r == '\n' || r == '\r' || r == '\v' || r == '\f' || r == '\x1c' || r == '\x1d' || r == '\x1e' || r == '\u0085' || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r)
	})
}

func matches(words map[string]struct{}, tokens []string) bool {
	for size := 1; size <= 5 && size <= len(tokens); size++ {
		for i := 0; i+size <= len(tokens); i++ {
			window := tokens[i : i+size]
			if _, ok := words[strings.Join(window, "")]; ok {
				return true
			}
			if _, ok := words[strings.Join(window, " ")]; ok {
				return true
			}
		}
	}
	return false
}
