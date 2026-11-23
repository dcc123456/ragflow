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
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ragflow/internal/codexagent"
	"ragflow/internal/entity"
)

// CodexThreadStore persists mode 8 session -> Codex-thread mappings. It implements
// codexagent.ThreadStore so the mapping is shared across replicas (the in-memory store
// only works for a single process).
type CodexThreadStore struct {
	db *gorm.DB
}

// NewCodexThreadStore returns a store backed by db.
func NewCodexThreadStore(db *gorm.DB) *CodexThreadStore {
	return &CodexThreadStore{db: db}
}

func (s *CodexThreadStore) Get(ctx context.Context, tenantID, sessionID string) (codexagent.ThreadRecord, bool, error) {
	var row entity.CodexThreadMap
	err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND session_id = ?", tenantID, sessionID).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return codexagent.ThreadRecord{}, false, nil
	}
	if err != nil {
		return codexagent.ThreadRecord{}, false, err
	}
	return codexagent.ThreadRecord{
		TenantID:           row.TenantID,
		SessionID:          row.SessionID,
		ThreadID:           row.ThreadID,
		ScopeFingerprint:   row.ScopeFingerprint,
		HistoryFingerprint: row.HistoryFingerprint,
		Ticket:             row.Ticket,
	}, true, nil
}

func (s *CodexThreadStore) Put(ctx context.Context, rec codexagent.ThreadRecord) error {
	now := time.Now().UnixMilli()
	row := entity.CodexThreadMap{
		TenantID:           rec.TenantID,
		SessionID:          rec.SessionID,
		ThreadID:           rec.ThreadID,
		ScopeFingerprint:   rec.ScopeFingerprint,
		HistoryFingerprint: rec.HistoryFingerprint,
		Ticket:             rec.Ticket,
		CreateTime:         now,
		UpdateTime:         now,
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "session_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"thread_id", "scope_fingerprint", "history_fingerprint", "ticket", "update_time"}),
	}).Create(&row).Error
}

func (s *CodexThreadStore) Delete(ctx context.Context, tenantID, sessionID string) error {
	return s.db.WithContext(ctx).
		Where("tenant_id = ? AND session_id = ?", tenantID, sessionID).
		Delete(&entity.CodexThreadMap{}).Error
}

// DeleteBySessionID removes a session's mapping regardless of tenant (session ids are
// globally unique) and returns the ticket it held, so the caller can revoke it. A missing
// row is not an error and yields an empty ticket.
func (s *CodexThreadStore) DeleteBySessionID(ctx context.Context, sessionID string) (string, error) {
	if sessionID == "" {
		return "", nil
	}
	var row entity.CodexThreadMap
	err := s.db.WithContext(ctx).Where("session_id = ?", sessionID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if err := s.db.WithContext(ctx).Where("session_id = ?", sessionID).Delete(&entity.CodexThreadMap{}).Error; err != nil {
		return "", err
	}
	return row.Ticket, nil
}
