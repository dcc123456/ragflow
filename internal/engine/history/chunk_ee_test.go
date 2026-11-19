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

package history

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"ragflow/internal/entity"
)

// memoryStore is the document engine as the history sees it: one store per
// dataset, one document per version, named by the pair. It keeps the dataset as
// the key - the shape Infinity has - and records the tenant it was handed.
type memoryStore struct {
	mu      sync.Mutex
	stores  map[string]map[string]map[string]interface{}
	failPut error
	// loads counts reads, so a read that quietly re-fetches is visible.
	loads int
	// tenants records the tenant each call named, so a test can see that the
	// store was addressed with the one the resolver answered.
	tenants []string
}

func newMemoryStore(t *testing.T) *memoryStore {
	t.Helper()
	store := &memoryStore{stores: map[string]map[string]map[string]interface{}{}}
	previous := currentChunkStore()
	SetChunkStore(store)
	t.Cleanup(func() { SetChunkStore(previous) })
	return store
}

func (s *memoryStore) StashChunkHistory(_ context.Context, tenantID, datasetID string, docs []map[string]interface{}) error {
	if s.failPut != nil {
		return s.failPut
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tenants = append(s.tenants, tenantID)
	store := s.stores[datasetID]
	if store == nil {
		store = map[string]map[string]interface{}{}
		s.stores[datasetID] = store
	}
	for _, doc := range docs {
		store[doc["id"].(string)] = doc
	}
	return nil
}

func (s *memoryStore) LoadChunkHistory(_ context.Context, tenantID, datasetID string, ids []string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loads++
	s.tenants = append(s.tenants, tenantID)
	store := s.stores[datasetID]
	bodies := make(map[string]string, len(ids))
	for _, id := range ids {
		doc, ok := store[id]
		if !ok {
			continue
		}
		bodies[id] = doc["snapshot"].(string)
	}
	return bodies, nil
}

func (s *memoryStore) DeleteChunkHistory(_ context.Context, tenantID, datasetID, docID string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tenants = append(s.tenants, tenantID)
	store := s.stores[datasetID]
	removed := int64(0)
	for id, doc := range store {
		if doc["doc_id"] == docID {
			delete(store, id)
			removed++
		}
	}
	return removed, nil
}

func (s *memoryStore) DeleteChunkHistoryIDs(_ context.Context, tenantID, datasetID string, ids []string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	store := s.stores[datasetID]
	removed := int64(0)
	for _, id := range ids {
		if _, ok := store[id]; ok {
			delete(store, id)
			removed++
		}
	}
	return removed, nil
}

func (s *memoryStore) DropChunkHistory(_ context.Context, tenantID, datasetID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.stores, datasetID)
	return nil
}

func (s *memoryStore) ids(datasetID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.stores[datasetID]))
	for id := range s.stores[datasetID] {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func versionRow(datasetID, docID, chunkID string, version int, body string) *entity.ChunkVersion {
	return &entity.ChunkVersion{
		ChunkID:        chunkID,
		Version:        version,
		DocID:          docID,
		DatasetID:      datasetID,
		DatasetVersion: 1,
		Kind:           entity.ChunkVersionKindParse,
		Snapshot:       entity.JSONMap{"content_with_weight": body},
	}
}

// TestStashWritesOneDocumentPerVersion pins the addressing: the body is stored
// under the pair the metadata row is keyed by, in the dataset's own store, and
// the row keeps nothing.
func TestStashWritesOneDocumentPerVersion(t *testing.T) {
	store := newMemoryStore(t)
	ctx := t.Context()

	rows := []*entity.ChunkVersion{
		versionRow("kb-1", "doc-1", "chunk-1", 0, "曹操，字孟德"),
		versionRow("kb-1", "doc-2", "chunk-2", 3, "刘备，字玄德"),
	}
	if err := StashChunkRows(ctx, rows); err != nil {
		t.Fatalf("stash: %v", err)
	}

	if got := store.ids("kb-1"); len(got) != 2 || got[0] != "chunk-1:0" || got[1] != "chunk-2:3" {
		t.Fatalf("stored ids = %v", got)
	}
	if rows[0].Snapshot == nil {
		t.Fatal("a stashed row lost the body it was written with")
	}
}

// TestHydrateReadsEveryBodyBack covers the read side: a row without a body gets
// its own text back, not its neighbour's.
func TestHydrateReadsEveryBodyBack(t *testing.T) {
	newMemoryStore(t)
	ctx := t.Context()

	if err := StashChunkRows(ctx, []*entity.ChunkVersion{
		versionRow("kb-1", "doc-1", "chunk-1", 0, "one"),
		versionRow("kb-1", "doc-1", "chunk-2", 1, "two"),
	}); err != nil {
		t.Fatalf("stash: %v", err)
	}

	rows := []*entity.ChunkVersion{
		{ChunkID: "chunk-1", Version: 0, DocID: "doc-1", DatasetID: "kb-1"},
		{ChunkID: "chunk-2", Version: 1, DocID: "doc-1", DatasetID: "kb-1"},
	}
	if err := HydrateChunks(ctx, rows); err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	if got := rows[0].Snapshot["content_with_weight"]; got != "one" {
		t.Fatalf("chunk-1 body = %v", got)
	}
	if got := rows[1].Snapshot["content_with_weight"]; got != "two" {
		t.Fatalf("chunk-2 body = %v", got)
	}
}

// TestHydrateFailsOnAMissingBody keeps a replay honest: a body the engine does
// not hold is an error, not an empty chunk.
func TestHydrateFailsOnAMissingBody(t *testing.T) {
	newMemoryStore(t)

	row := &entity.ChunkVersion{ChunkID: "chunk-1", Version: 7, DocID: "doc-1", DatasetID: "kb-1"}
	err := HydrateChunks(t.Context(), []*entity.ChunkVersion{row})
	if !errors.Is(err, ErrChunkMissing) {
		t.Fatalf("hydrate of a missing body = %v, want ErrChunkMissing", err)
	}
}

// TestStashReportsAFailedWrite is the guarantee the whole design rests on: a
// body that could not be stored is returned to the caller, never swallowed.
func TestStashReportsAFailedWrite(t *testing.T) {
	store := newMemoryStore(t)
	store.failPut = errors.New("cluster unavailable")

	if err := StashChunk(t.Context(), versionRow("kb-1", "doc-1", "chunk-1", 0, "body")); err == nil {
		t.Fatal("a failed write was reported as success")
	}
}

// TestRewritingAVersionOverwritesOneDocument covers the in-place annotation
// edit: the version keeps its number, so its body must keep its address.
func TestRewritingAVersionOverwritesOneDocument(t *testing.T) {
	store := newMemoryStore(t)
	ctx := t.Context()

	if err := StashChunk(ctx, versionRow("kb-1", "doc-1", "chunk-1", 2, "before")); err != nil {
		t.Fatalf("stash: %v", err)
	}
	if err := StashChunk(ctx, versionRow("kb-1", "doc-1", "chunk-1", 2, "after")); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	if ids := store.ids("kb-1"); len(ids) != 1 || ids[0] != "chunk-1:2" {
		t.Fatalf("rewrite produced %v", ids)
	}
	row := &entity.ChunkVersion{ChunkID: "chunk-1", Version: 2, DocID: "doc-1", DatasetID: "kb-1"}
	if err := HydrateChunks(ctx, []*entity.ChunkVersion{row}); err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	if got := row.Snapshot["content_with_weight"]; got != "after" {
		t.Fatalf("body = %v, want the rewritten one", got)
	}
}

// TestStashKeepsDatasetsApart pins the scoping: one dataset's history is not
// readable, purgeable or droppable through another's.
func TestStashKeepsDatasetsApart(t *testing.T) {
	store := newMemoryStore(t)
	ctx := t.Context()

	if err := StashChunk(ctx, versionRow("kb-1", "doc-1", "chunk-1", 0, "one")); err != nil {
		t.Fatalf("stash kb-1: %v", err)
	}
	if err := StashChunk(ctx, versionRow("kb-2", "doc-1", "chunk-1", 0, "two")); err != nil {
		t.Fatalf("stash kb-2: %v", err)
	}

	if err := DeleteChunkDataset(ctx, "kb-1"); err != nil {
		t.Fatalf("drop kb-1: %v", err)
	}
	if got := store.ids("kb-1"); len(got) != 0 {
		t.Fatalf("kb-1 kept %v", got)
	}
	row := &entity.ChunkVersion{ChunkID: "chunk-1", Version: 0, DocID: "doc-1", DatasetID: "kb-2"}
	if err := HydrateChunks(ctx, []*entity.ChunkVersion{row}); err != nil {
		t.Fatalf("hydrate kb-2: %v", err)
	}
	if got := row.Snapshot["content_with_weight"]; got != "two" {
		t.Fatalf("kb-2 body = %v", got)
	}
}

// TestDeleteDocumentRemovesOnlyThatDocument covers the purge path: a document's
// bodies go with it and its siblings' stay.
func TestDeleteDocumentRemovesOnlyThatDocument(t *testing.T) {
	store := newMemoryStore(t)
	ctx := t.Context()

	if err := StashChunkRows(ctx, []*entity.ChunkVersion{
		versionRow("kb-1", "doc-1", "chunk-1", 0, "a"),
		versionRow("kb-1", "doc-1", "chunk-1", 1, "b"),
		versionRow("kb-1", "doc-2", "chunk-2", 0, "c"),
	}); err != nil {
		t.Fatalf("stash: %v", err)
	}

	if err := DeleteChunkDocument(ctx, "kb-1", "doc-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if ids := store.ids("kb-1"); len(ids) != 1 || ids[0] != "chunk-2:0" {
		t.Fatalf("left behind %v", ids)
	}
}

// TestNoStoreMeansNoHistory keeps the edition usable on an engine that holds no
// history: the calls report that rather than pretending to have stored a body.
func TestNoStoreMeansNoHistory(t *testing.T) {
	previous := currentChunkStore()
	SetChunkStore(nil)
	t.Cleanup(func() { SetChunkStore(previous) })

	if ChunkAvailable() {
		t.Fatal("an engine with no store reported chunk history as available")
	}
	if err := StashChunk(t.Context(), versionRow("kb-1", "doc-1", "chunk-1", 0, "body")); !errors.Is(err, ErrChunkUnavailable) {
		t.Fatalf("stash = %v, want ErrChunkUnavailable", err)
	}
}

// TestDeleteChunkDocumentsRemovesOnlyTheNamedBodies pins the per-version purge:
// the body whose row was deleted goes, and the surviving versions of the same
// chunk keep theirs.
func TestDeleteChunkDocumentsRemovesOnlyTheNamedBodies(t *testing.T) {
	store := newMemoryStore(t)
	ctx := t.Context()

	if err := StashChunkRows(ctx, []*entity.ChunkVersion{
		versionRow("kb-1", "doc-a", "chunk-1", 0, "parsed"),
		versionRow("kb-1", "doc-a", "chunk-1", 1, "edited"),
		versionRow("kb-1", "doc-a", "chunk-1", 2, "edited twice"),
	}); err != nil {
		t.Fatalf("StashChunkRows() error = %v", err)
	}

	removed, err := DeleteChunkDocuments(ctx, "kb-1", []string{ChunkDocumentID("chunk-1", 1)})
	if err != nil {
		t.Fatalf("DeleteChunkDocuments() error = %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if ids := store.ids("kb-1"); len(ids) != 2 || ids[0] != "chunk-1:0" || ids[1] != "chunk-1:2" {
		t.Fatalf("remaining bodies = %v, want versions 0 and 2", ids)
	}

	// Nothing named: nothing to drop, and no store call.
	if removed, err = DeleteChunkDocuments(ctx, "kb-1", nil); err != nil || removed != 0 {
		t.Fatalf("DeleteChunkDocuments(no ids) = %d, %v; want 0, nil", removed, err)
	}
}

// TestDeleteChunkDocumentsWithoutStore reports the missing store instead of
// pretending the body is gone.
func TestDeleteChunkDocumentsWithoutStore(t *testing.T) {
	previous := currentChunkStore()
	SetChunkStore(nil)
	t.Cleanup(func() { SetChunkStore(previous) })

	if _, err := DeleteChunkDocuments(t.Context(), "kb-1", []string{"chunk-1:0"}); !errors.Is(err, ErrChunkUnavailable) {
		t.Fatalf("DeleteChunkDocuments() error = %v, want ErrChunkUnavailable", err)
	}
}

func TestDocumentIDIsTheRowKey(t *testing.T) {
	if got, want := ChunkDocumentID("chunk-1", 12), "chunk-1:"+strconv.Itoa(12); got != want {
		t.Fatalf("DocumentID = %q, want %q", got, want)
	}
	if strings.Count(ChunkDocumentID("chunk:1", 0), ":") != 2 {
		t.Fatal("a chunk id containing a colon must still address one document")
	}
}

// TestStashAddressesTheStoreWithTheDatasetsTenant pins the addressing of an
// engine keyed by tenant: the caller names the dataset, the resolver turns it into
// the tenant, and the store is handed both.
func TestStashAddressesTheStoreWithTheDatasetsTenant(t *testing.T) {
	store := newMemoryStore(t)
	lookups := 0
	SetTenantResolver(func(_ context.Context, datasetID string) (string, error) {
		lookups++
		if datasetID != "kb-1" {
			return "", errors.New("unexpected dataset")
		}
		return "tenant-1", nil
	})
	t.Cleanup(func() { SetTenantResolver(nil) })

	// Two writes of one dataset resolve once: a dataset belongs to one tenant for
	// life, so the lookup is memoized instead of run per version.
	if err := StashChunk(t.Context(), versionRow("kb-1", "doc-1", "chunk-1", 0, "one")); err != nil {
		t.Fatalf("stash: %v", err)
	}
	if err := StashChunk(t.Context(), versionRow("kb-1", "doc-1", "chunk-2", 0, "two")); err != nil {
		t.Fatalf("stash: %v", err)
	}
	if lookups != 1 {
		t.Fatalf("tenant lookups = %d, want the lookup memoized", lookups)
	}
	for _, tenant := range store.tenants {
		if tenant != "tenant-1" {
			t.Fatalf("store addressed with tenant %q, want the dataset's owner", tenant)
		}
	}
	if got := store.ids("kb-1"); len(got) != 2 {
		t.Fatalf("stored ids = %v, want both versions", got)
	}
}

// TestStashFailsWhenTheTenantCannotBeResolved keeps the write honest: an index
// named after the wrong tenant is a body no reader of this dataset would find.
func TestStashFailsWhenTheTenantCannotBeResolved(t *testing.T) {
	store := newMemoryStore(t)
	SetTenantResolver(func(context.Context, string) (string, error) {
		return "", errors.New("metadata database unavailable")
	})
	t.Cleanup(func() { SetTenantResolver(nil) })

	if err := StashChunk(t.Context(), versionRow("kb-1", "doc-1", "chunk-1", 0, "body")); err == nil {
		t.Fatal("a stash whose tenant could not be resolved was reported as success")
	}
	if got := store.ids("kb-1"); len(got) != 0 {
		t.Fatalf("stored ids = %v, want nothing written", got)
	}
}

// TestTenantOfWithoutAResolverIsEmpty pins the engine that needs no tenant: an
// empty answer is passed through instead of failing a store keyed by dataset.
func TestTenantOfWithoutAResolverIsEmpty(t *testing.T) {
	SetTenantResolver(nil)
	tenant, err := TenantOf(t.Context(), "kb-1")
	if err != nil || tenant != "" {
		t.Fatalf("TenantOf() = %q, %v; want no tenant and no error", tenant, err)
	}
}
