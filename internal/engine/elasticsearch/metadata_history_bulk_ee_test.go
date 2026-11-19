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

// TestStashMetadataHistoryWritesToTheTenantIndex pins the addressing of the
// store: one index per tenant, the way a tenant's doc_meta rows live in one index,
// and the dataset travels inside the document instead of in its name.
func TestStashMetadataHistoryWritesToTheTenantIndex(t *testing.T) {
	var bulkBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/_bulk") {
			raw, _ := io.ReadAll(r.Body)
			bulkBody = string(raw)
			_, _ = w.Write([]byte(`{"errors":false,"items":[]}`))
			return
		}
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write([]byte(`{"acknowledged":true}`))
	}))
	t.Cleanup(server.Close)

	engine := newTestEngine(t, server.URL)
	err := engine.StashMetadataHistory(t.Context(), "tenant-1", "ds-1", []map[string]interface{}{{
		"id": "doc-1:abc", "document_id": "doc-1", "dataset_id": "ds-1", "snapshot": `{"author":"alice"}`,
	}})
	if err != nil {
		t.Fatalf("StashMetadataHistory: %v", err)
	}

	first, _, _ := strings.Cut(bulkBody, "\n")
	action := map[string]interface{}{}
	if err := json.Unmarshal([]byte(first), &action); err != nil {
		t.Fatalf("bulk action %q is not an object: %v", first, err)
	}
	index, _ := action["index"].(map[string]interface{})
	if index["_index"] != "ragflow_doc_meta_tenant-1_history" {
		t.Fatalf("action index = %v, want the tenant's history index", index["_index"])
	}
	if index["_id"] != "doc-1:abc" {
		t.Fatalf("action id = %v, want the snapshot id", index["_id"])
	}
}

// TestLoadMetadataHistoryNarrowsToOneDataset pins the second half of the
// addressing: one index holds every dataset of a tenant, so a lookup names the
// dataset and cannot be answered with another dataset's values.
func TestLoadMetadataHistoryNarrowsToOneDataset(t *testing.T) {
	var searchBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/ragflow_doc_meta_tenant-1_history/_search" {
			raw, _ := io.ReadAll(r.Body)
			searchBody = string(raw)
			_, _ = w.Write([]byte(`{"hits":{"hits":[{"_source":{"id":"doc-1:abc","snapshot":"{}"}}]}}`))
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
	bodies, err := engine.LoadMetadataHistory(t.Context(), "tenant-1", "ds-1", []string{"doc-1:abc"})
	if err != nil {
		t.Fatalf("LoadMetadataHistory: %v", err)
	}
	if bodies["doc-1:abc"] != "{}" {
		t.Fatalf("bodies = %v, want the stored values", bodies)
	}
	if !strings.Contains(searchBody, `"dataset_id":"ds-1"`) {
		t.Fatalf("search body = %q, want the dataset to narrow the lookup", searchBody)
	}
}

// TestDropMetadataHistoryKeepsTheTenantIndex pins that dropping a dataset deletes
// its snapshots instead of the index: the index belongs to the tenant, and
// another dataset's history lives in it.
func TestDropMetadataHistoryKeepsTheTenantIndex(t *testing.T) {
	path := ""
	var deleteBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/_delete_by_query") {
			raw, _ := io.ReadAll(r.Body)
			deleteBody = string(raw)
			path = r.URL.Path
			_, _ = w.Write([]byte(`{"deleted":3,"timed_out":false}`))
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
	if err := engine.DropMetadataHistory(t.Context(), "tenant-1", "ds-1"); err != nil {
		t.Fatalf("DropMetadataHistory: %v", err)
	}
	if !strings.Contains(path, "/ragflow_doc_meta_tenant-1_history/_delete_by_query") {
		t.Fatalf("path = %q, want a purge inside the tenant's history index", path)
	}
	if !strings.Contains(deleteBody, `"dataset_id":"ds-1"`) {
		t.Fatalf("delete body = %q, want the dataset boundary", deleteBody)
	}
}
