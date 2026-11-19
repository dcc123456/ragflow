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

// Package historytest holds the stores tests install in place of the document
// engine's own. It lives apart from history so no server binary links the
// in-memory copies: production always installs the engine store, which is the
// only copy of the bodies and the values.
package historytest

import (
	"context"
	"sort"
	"sync"
)

// ChunkStore is a history.ChunkStore held in memory.
type ChunkStore struct {
	mu     sync.Mutex
	stores map[string]map[string]map[string]interface{}
}

// NewChunkStore returns an empty in-memory chunk history store.
func NewChunkStore() *ChunkStore {
	return &ChunkStore{stores: map[string]map[string]map[string]interface{}{}}
}

func (s *ChunkStore) StashChunkHistory(_ context.Context, _, datasetID string, docs []map[string]interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stash(s.stores, datasetID, docs)
	return nil
}

func (s *ChunkStore) LoadChunkHistory(_ context.Context, _, datasetID string, ids []string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return load(s.stores, datasetID, ids), nil
}

func (s *ChunkStore) DeleteChunkHistory(_ context.Context, _, datasetID, docID string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return drop(s.stores, datasetID, "doc_id", docID), nil
}

func (s *ChunkStore) DeleteChunkHistoryIDs(_ context.Context, _, datasetID string, ids []string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := int64(0)
	for _, id := range ids {
		if _, ok := s.stores[datasetID][id]; ok {
			delete(s.stores[datasetID], id)
			removed++
		}
	}
	return removed, nil
}

func (s *ChunkStore) DropChunkHistory(_ context.Context, _, datasetID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.stores, datasetID)
	return nil
}

// DocumentIDs lists the ids this store holds for a dataset, for a test that
// wants to see what a write addressed.
func (s *ChunkStore) DocumentIDs(datasetID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.stores[datasetID]))
	for id := range s.stores[datasetID] {
		ids = append(ids, id)
	}
	return ids
}

// MetadataStore is a history.MetadataStore held in memory.
type MetadataStore struct {
	mu     sync.Mutex
	stores map[string]map[string]map[string]interface{}
}

// NewMetadataStore returns an empty in-memory metadata history store.
func NewMetadataStore() *MetadataStore {
	return &MetadataStore{stores: map[string]map[string]map[string]interface{}{}}
}

func (s *MetadataStore) StashMetadataHistory(_ context.Context, _, datasetID string, docs []map[string]interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stash(s.stores, datasetID, docs)
	return nil
}

func (s *MetadataStore) LoadMetadataHistory(_ context.Context, _, datasetID string, ids []string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return load(s.stores, datasetID, ids), nil
}

func (s *MetadataStore) DeleteMetadataHistory(_ context.Context, _, datasetID, documentID string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return drop(s.stores, datasetID, "document_id", documentID), nil
}

func (s *MetadataStore) DropMetadataHistory(_ context.Context, _, datasetID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.stores, datasetID)
	return nil
}

// SnapshotIDs lists the snapshots this store holds for a dataset, sorted, for a
// test that wants to see what a write addressed.
func (s *MetadataStore) SnapshotIDs(datasetID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.stores[datasetID]))
	for id := range s.stores[datasetID] {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// stash writes one document per entry, named by entry["id"], which is what both
// stores promise.
func stash(stores map[string]map[string]map[string]interface{}, datasetID string, docs []map[string]interface{}) {
	store := stores[datasetID]
	if store == nil {
		store = map[string]map[string]interface{}{}
		stores[datasetID] = store
	}
	for _, doc := range docs {
		if id, ok := doc["id"].(string); ok {
			store[id] = doc
		}
	}
}

// load returns the snapshot text of every id the store holds, keyed by id.
func load(stores map[string]map[string]map[string]interface{}, datasetID string, ids []string) map[string]string {
	store := stores[datasetID]
	bodies := make(map[string]string, len(ids))
	for _, id := range ids {
		if doc, ok := store[id]; ok {
			body, _ := doc["snapshot"].(string)
			bodies[id] = body
		}
	}
	return bodies
}

// drop removes every document of one owner - a chunk's "doc_id" or a metadata
// snapshot's "document_id" - and reports how many it removed.
func drop(stores map[string]map[string]map[string]interface{}, datasetID, ownerKey, ownerID string) int64 {
	removed := int64(0)
	for id, doc := range stores[datasetID] {
		if doc[ownerKey] == ownerID {
			delete(stores[datasetID], id)
			removed++
		}
	}
	return removed
}
