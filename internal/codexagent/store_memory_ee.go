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

package codexagent

import (
	"context"
	"sync"
)

// MemoryThreadStore is an in-process ThreadStore. It is the test double and the
// fallback when no database is available (multi-replica deployments need the GORM-backed
// store so the mapping is shared).
type MemoryThreadStore struct {
	mu    sync.Mutex
	items map[string]ThreadRecord
}

// NewMemoryThreadStore returns an empty in-memory store.
func NewMemoryThreadStore() *MemoryThreadStore {
	return &MemoryThreadStore{items: make(map[string]ThreadRecord)}
}

func memoryKey(tenantID, sessionID string) string { return tenantID + "\x00" + sessionID }

func (s *MemoryThreadStore) Get(_ context.Context, tenantID, sessionID string) (ThreadRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.items[memoryKey(tenantID, sessionID)]
	return rec, ok, nil
}

func (s *MemoryThreadStore) Put(_ context.Context, rec ThreadRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[memoryKey(rec.TenantID, rec.SessionID)] = rec
	return nil
}

func (s *MemoryThreadStore) Delete(_ context.Context, tenantID, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, memoryKey(tenantID, sessionID))
	return nil
}
