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

package elasticsearch

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestStashChunkHistoryWritesBulkActionLines pins the shape of the request body:
// every snapshot is two lines, and the first one names the action, the index and
// the id. Without the action verb the bulk endpoint parses the line as a
// document and rejects the whole batch ("expected field [create], [delete],
// [index] or [update]"), which is what a live edit hit.
func TestStashChunkHistoryWritesBulkActionLines(t *testing.T) {
	var bulkBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/_bulk") {
			raw, _ := io.ReadAll(r.Body)
			bulkBody = string(raw)
			_, _ = w.Write([]byte(`{"took":1,"errors":false,"items":[{"index":{"_index":"ragflow_tenant-1_history","_id":"chunk-1:0","status":201}}]}`))
			return
		}
		if r.Method == http.MethodHead {
			// The history index already exists, so no mapping is written.
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)

	engine := newTestEngine(t, server.URL)
	err := engine.StashChunkHistory(t.Context(), "tenant-1", "ds-1", []map[string]interface{}{{
		"id": "chunk-1:0", "chunk_id": "chunk-1", "version": 0, "snapshot": "body",
	}})
	if err != nil {
		t.Fatalf("StashChunkHistory: %v", err)
	}

	lines := strings.Split(strings.TrimSuffix(bulkBody, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("bulk body = %q, want an action line and a document line", bulkBody)
	}
	var action map[string]map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &action); err != nil {
		t.Fatalf("action line %q is not an object: %v", lines[0], err)
	}
	index, ok := action["index"]
	if !ok {
		t.Fatalf("action line = %q, want it to name the action \"index\"", lines[0])
	}
	// One index per tenant, the way a tenant's chunks live in one index.
	if index["_index"] != "ragflow_tenant-1_history" {
		t.Fatalf("action index = %v, want the tenant's history index", index["_index"])
	}
	if index["_id"] != "chunk-1:0" {
		t.Fatalf("action id = %v, want the version's document id", index["_id"])
	}
	var doc map[string]interface{}
	if err := json.Unmarshal([]byte(lines[1]), &doc); err != nil {
		t.Fatalf("document line %q is not an object: %v", lines[1], err)
	}
	if doc["snapshot"] != "body" {
		t.Fatalf("document = %v, want the snapshot it was given", doc)
	}
}

// TestLoadChunkHistoryNarrowsToOneDataset pins the second half of the addressing:
// one index holds every dataset of a tenant, so a lookup names the dataset and
// cannot be answered with another dataset's body.
func TestLoadChunkHistoryNarrowsToOneDataset(t *testing.T) {
	var searchBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/ragflow_tenant-1_history/_search" {
			raw, _ := io.ReadAll(r.Body)
			searchBody = string(raw)
			_, _ = w.Write([]byte(`{"hits":{"hits":[{"_source":{"id":"chunk-1:0","snapshot":"body"}}]}}`))
			return
		}
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)

	engine := newTestEngine(t, server.URL)
	bodies, err := engine.LoadChunkHistory(t.Context(), "tenant-1", "ds-1", []string{"chunk-1:0"})
	if err != nil {
		t.Fatalf("LoadChunkHistory: %v", err)
	}
	if bodies["chunk-1:0"] != "body" {
		t.Fatalf("bodies = %v, want the stored snapshot", bodies)
	}

	query := map[string]interface{}{}
	if err := json.Unmarshal([]byte(searchBody), &query); err != nil {
		t.Fatalf("search body %q is not an object: %v", searchBody, err)
	}
	if !strings.Contains(searchBody, `"dataset_id":"ds-1"`) {
		t.Fatalf("search body = %q, want the dataset to narrow the lookup", searchBody)
	}
}

// TestChunkHistoryRequiresATenant pins that the store refuses a request it cannot
// address: without a tenant there is no index, and guessing one would write a
// body nothing reads back.
func TestChunkHistoryRequiresATenant(t *testing.T) {
	engine := newTestEngine(t, "http://127.0.0.1:1")
	err := engine.StashChunkHistory(t.Context(), "", "ds-1", []map[string]interface{}{{"id": "chunk-1:0"}})
	if err == nil || !strings.Contains(err.Error(), "tenant") {
		t.Fatalf("StashChunkHistory(no tenant) error = %v, want a refusal naming the tenant", err)
	}
}

// TestDropChunkHistoryKeepsTheTenantIndex pins that dropping a dataset deletes its
// documents instead of the index: the index belongs to the tenant, and another
// dataset's history lives in it.
func TestDropChunkHistoryKeepsTheTenantIndex(t *testing.T) {
	deleted := false
	method := ""
	path := ""
	var deleteBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.Header().Set("Content-Type", "application/json")
		method, path = r.Method, r.URL.Path
		if strings.HasSuffix(r.URL.Path, "/_delete_by_query") {
			raw, _ := io.ReadAll(r.Body)
			deleteBody = string(raw)
			deleted = true
			_, _ = w.Write([]byte(`{"deleted":2,"timed_out":false}`))
			return
		}
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)

	engine := newTestEngine(t, server.URL)
	if err := engine.DropChunkHistory(t.Context(), "tenant-1", "ds-1"); err != nil {
		t.Fatalf("DropChunkHistory: %v", err)
	}
	if !deleted {
		t.Fatalf("last request = %s %s, want a delete_by_query", method, path)
	}
	if strings.HasSuffix(path, "?") || method != http.MethodPost {
		t.Fatalf("request = %s %s, want a delete_by_query against the tenant index", method, path)
	}
	if !strings.Contains(path, "/ragflow_tenant-1_history/_delete_by_query") {
		t.Fatalf("path = %q, want the tenant's history index", path)
	}
	if !strings.Contains(deleteBody, `"dataset_id":"ds-1"`) {
		t.Fatalf("delete body = %q, want the dataset boundary", deleteBody)
	}
}
