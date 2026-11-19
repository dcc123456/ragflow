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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/elastic/go-elasticsearch/v8/esapi"
)

// MetadataHistoryIndexPrefix names, together with the tenant id, the index that
// holds document metadata snapshots. It is the doc_meta index family the history
// extends (ragflow_doc_meta_<tenant>), and dataset_id narrows every read to one
// dataset's slice of it.
//
// The name matches the "ragflow_doc_meta_*" template, which carries the doc_meta
// columns. The index's own mapping below is what the store uses: its explicit
// properties win over the template's, and its "dynamic": false keeps the row to
// the fields written here instead of the template's runtime behaviour.
const MetadataHistoryIndexPrefix = "ragflow_doc_meta_"

// MetadataHistoryIndexSuffix is appended to the doc_meta index name, so the
// history is the index it belongs to with the suffix (ragflow_doc_meta_<tenant>
// -> ragflow_doc_meta_<tenant>_history).
const MetadataHistoryIndexSuffix = "_history"

// MetadataHistoryIndexName is the index a tenant's metadata snapshots live in:
// the tenant's doc_meta index with MetadataHistoryIndexSuffix appended.
func MetadataHistoryIndexName(tenantID string) string {
	return MetadataHistoryIndexPrefix + tenantID + MetadataHistoryIndexSuffix
}

// metadataHistoryMapping is written explicitly instead of inherited: the values
// are stored, never searched, so they are kept in _source and left out of the
// inverted index and the doc values. The name matches the "ragflow_doc_meta_*"
// template, whose mappings this one overrides field by field - the template's
// kb_id/meta_fields end up defined but unused, and the "dynamic": false below
// keeps the store to the fields written here.
var metadataHistoryMapping = map[string]interface{}{
	"settings": map[string]interface{}{
		"number_of_shards":   1,
		"number_of_replicas": 1,
	},
	"mappings": map[string]interface{}{
		"dynamic": false,
		"properties": map[string]interface{}{
			"id":          map[string]interface{}{"type": "keyword"},
			"document_id": map[string]interface{}{"type": "keyword"},
			"dataset_id":  map[string]interface{}{"type": "keyword"},
			"snapshot":    map[string]interface{}{"type": "keyword", "index": false, "doc_values": false},
		},
	},
}

func (e *Engine) ensureMetadataHistoryIndex(ctx context.Context, indexName string) error {
	exists, err := e.indexExists(ctx, indexName)
	if err != nil {
		return fmt.Errorf("failed to check the metadata history index %s: %w", indexName, err)
	}
	if exists {
		return nil
	}
	body, err := json.Marshal(metadataHistoryMapping)
	if err != nil {
		return fmt.Errorf("failed to encode the metadata history mapping: %w", err)
	}
	req := esapi.IndicesCreateRequest{Index: indexName, Body: bytes.NewReader(body)}
	res, err := req.Do(ctx, e.client)
	if err != nil {
		return fmt.Errorf("failed to create the metadata history index %s: %w", indexName, err)
	}
	defer res.Body.Close()
	if res.IsError() {
		// A concurrent writer may have created it between the check and here.
		if res.StatusCode == 400 {
			if again, checkErr := e.indexExists(ctx, indexName); checkErr == nil && again {
				return nil
			}
		}
		raw, _ := io.ReadAll(res.Body)
		if reason := extractErrorReason(raw); reason != "" {
			return fmt.Errorf("failed to create the metadata history index %s: %s", indexName, reason)
		}
		return fmt.Errorf("failed to create the metadata history index %s: %s", indexName, res.Status())
	}
	return nil
}

// StashMetadataHistory writes one document per entry, named by entry["id"].
//
// The write refreshes: a snapshot is read back by a restore as soon as the edit
// returns, and values that are not visible yet would be reported as missing.
func (e *Engine) StashMetadataHistory(ctx context.Context, tenantID, datasetID string, docs []map[string]interface{}) error {
	if tenantID == "" || datasetID == "" || len(docs) == 0 {
		return nil
	}
	indexName := MetadataHistoryIndexName(tenantID)
	if err := e.ensureMetadataHistoryIndex(ctx, indexName); err != nil {
		return err
	}

	var buf bytes.Buffer
	for _, doc := range docs {
		id, _ := doc["id"].(string)
		if id == "" {
			return fmt.Errorf("metadata history: a snapshot arrived without an id")
		}
		// A bulk line is two lines: an action naming the index and the id, then the
		// document. The action verb is part of the line - without it the reader
		// takes this for a document ("expected field [create], [delete], [index]
		// or [update]").
		meta, err := json.Marshal(map[string]interface{}{
			"index": map[string]interface{}{"_index": indexName, "_id": id},
		})
		if err != nil {
			return fmt.Errorf("metadata history: failed to encode the bulk metadata of %s: %w", id, err)
		}
		source, err := json.Marshal(doc)
		if err != nil {
			return fmt.Errorf("metadata history: failed to encode the snapshot of %s: %w", id, err)
		}
		buf.Write(meta)
		buf.WriteByte('\n')
		buf.Write(source)
		buf.WriteByte('\n')
	}

	req := esapi.BulkRequest{Body: bytes.NewReader(buf.Bytes()), Refresh: "true"}
	res, err := req.Do(ctx, e.client)
	if err != nil {
		return fmt.Errorf("metadata history: failed to write a snapshot batch to %s: %w", indexName, err)
	}
	defer res.Body.Close()
	if res.IsError() {
		raw, _ := io.ReadAll(res.Body)
		if reason := extractErrorReason(raw); reason != "" {
			return fmt.Errorf("metadata history: failed to write a snapshot batch to %s: %s", indexName, reason)
		}
		return fmt.Errorf("metadata history: failed to write a snapshot batch to %s: %s", indexName, res.Status())
	}

	var result struct {
		Errors bool `json:"errors"`
		Items  []map[string]struct {
			Error *struct {
				Reason string `json:"reason"`
			} `json:"error"`
		} `json:"items"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return fmt.Errorf("metadata history: failed to read the write response of %s: %w", indexName, err)
	}
	if result.Errors {
		for _, item := range result.Items {
			for _, op := range item {
				if op.Error != nil {
					return fmt.Errorf("metadata history: failed to write a snapshot to %s: %s", indexName, op.Error.Reason)
				}
			}
		}
		return fmt.Errorf("metadata history: failed to write a snapshot batch to %s", indexName)
	}
	return nil
}

// LoadMetadataHistory returns the values JSON of every id it finds. An id with no
// document is absent from the map: the caller turns that into a missing snapshot
// instead of an empty one.
func (e *Engine) LoadMetadataHistory(ctx context.Context, tenantID, datasetID string, ids []string) (map[string]string, error) {
	bodies := make(map[string]string, len(ids))
	if tenantID == "" || datasetID == "" || len(ids) == 0 {
		return bodies, nil
	}
	indexName := MetadataHistoryIndexName(tenantID)
	exists, err := e.indexExists(ctx, indexName)
	if err != nil {
		return nil, fmt.Errorf("failed to check the metadata history index %s: %w", indexName, err)
	}
	if !exists {
		return bodies, nil
	}

	// The index holds every dataset of the tenant, so the dataset narrows the
	// lookup: an id is only unique within a dataset's history.
	query := map[string]interface{}{
		"size":    len(ids),
		"_source": []string{"id", "snapshot"},
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must":   []interface{}{map[string]interface{}{"terms": map[string]interface{}{"id": ids}}},
				"filter": []interface{}{map[string]interface{}{"term": map[string]interface{}{"dataset_id": datasetID}}},
			},
		},
	}
	body, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("metadata history: failed to encode a snapshot lookup: %w", err)
	}
	res, err := e.client.Search(
		e.client.Search.WithContext(ctx),
		e.client.Search.WithIndex(indexName),
		e.client.Search.WithBody(bytes.NewReader(body)),
	)
	if err != nil {
		return nil, fmt.Errorf("metadata history: failed to read snapshots from %s: %w", indexName, err)
	}
	defer res.Body.Close()
	if res.IsError() {
		raw, _ := io.ReadAll(res.Body)
		if reason := extractErrorReason(raw); reason != "" {
			return nil, fmt.Errorf("metadata history: failed to read snapshots from %s: %s", indexName, reason)
		}
		return nil, fmt.Errorf("metadata history: failed to read snapshots from %s: %s", indexName, res.Status())
	}

	var result struct {
		Hits struct {
			Hits []struct {
				Source struct {
					ID       string `json:"id"`
					Snapshot string `json:"snapshot"`
				} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("metadata history: failed to decode the snapshots of %s: %w", indexName, err)
	}
	for _, hit := range result.Hits.Hits {
		if hit.Source.ID == "" {
			continue
		}
		bodies[hit.Source.ID] = hit.Source.Snapshot
	}
	return bodies, nil
}

// DeleteMetadataHistory removes every snapshot a document recorded and reports how
// many went.
func (e *Engine) DeleteMetadataHistory(ctx context.Context, tenantID, datasetID, documentID string) (int64, error) {
	if tenantID == "" || datasetID == "" || documentID == "" {
		return 0, nil
	}
	indexName := MetadataHistoryIndexName(tenantID)
	exists, err := e.indexExists(ctx, indexName)
	if err != nil {
		return 0, fmt.Errorf("failed to check the metadata history index %s: %w", indexName, err)
	}
	if !exists {
		return 0, nil
	}
	body, err := json.Marshal(map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must":   []interface{}{map[string]interface{}{"term": map[string]interface{}{"document_id": documentID}}},
				"filter": []interface{}{map[string]interface{}{"term": map[string]interface{}{"dataset_id": datasetID}}},
			},
		},
	})
	if err != nil {
		return 0, fmt.Errorf("metadata history: failed to encode a document purge: %w", err)
	}
	refresh := true
	req := esapi.DeleteByQueryRequest{Index: []string{indexName}, Body: bytes.NewReader(body), Refresh: &refresh}
	res, err := req.Do(ctx, e.client)
	if err != nil {
		return 0, fmt.Errorf("metadata history: failed to purge the snapshots of document %s: %w", documentID, err)
	}
	defer res.Body.Close()
	if res.IsError() {
		raw, _ := io.ReadAll(res.Body)
		if reason := extractErrorReason(raw); reason != "" {
			return 0, fmt.Errorf("metadata history: failed to purge the snapshots of document %s: %s", documentID, reason)
		}
		return 0, fmt.Errorf("metadata history: failed to purge the snapshots of document %s: %s", documentID, res.Status())
	}
	var result map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("metadata history: failed to read the purge response of document %s: %w", documentID, err)
	}
	deleted, _ := result["deleted"].(float64)
	// A delete that timed out or reported failures leaves snapshots behind for a
	// document that no longer exists, so it is an error rather than a count.
	if timedOut, _ := result["timed_out"].(bool); timedOut {
		return int64(deleted), fmt.Errorf("metadata history: purging the snapshots of document %s timed out after %d", documentID, int64(deleted))
	}
	if failures, ok := result["failures"].([]interface{}); ok && len(failures) > 0 {
		return int64(deleted), fmt.Errorf("metadata history: purging the snapshots of document %s reported %d failures", documentID, len(failures))
	}
	return int64(deleted), nil
}

// DropMetadataHistory removes a dataset's whole metadata history.
//
// The index belongs to the tenant, not to the dataset, so it stays: dropping it
// would take every other dataset of the tenant's history with it. What goes is
// this dataset's slice of it.
func (e *Engine) DropMetadataHistory(ctx context.Context, tenantID, datasetID string) error {
	if tenantID == "" || datasetID == "" {
		return nil
	}
	indexName := MetadataHistoryIndexName(tenantID)
	// Only a name this file would have built can be purged: the doc_meta index
	// itself is the same name without the suffix, and it must never go with the
	// history.
	if !strings.HasPrefix(indexName, MetadataHistoryIndexPrefix) || !strings.HasSuffix(indexName, MetadataHistoryIndexSuffix) {
		return fmt.Errorf("metadata history: refusing to drop %q", indexName)
	}
	exists, err := e.indexExists(ctx, indexName)
	if err != nil {
		return fmt.Errorf("failed to check the metadata history index %s: %w", indexName, err)
	}
	if !exists {
		return nil
	}
	body, err := json.Marshal(map[string]interface{}{
		"query": map[string]interface{}{"term": map[string]interface{}{"dataset_id": datasetID}},
	})
	if err != nil {
		return fmt.Errorf("metadata history: failed to encode the purge of dataset %s: %w", datasetID, err)
	}
	refresh := true
	req := esapi.DeleteByQueryRequest{Index: []string{indexName}, Body: bytes.NewReader(body), Refresh: &refresh}
	res, err := req.Do(ctx, e.client)
	if err != nil {
		return fmt.Errorf("metadata history: failed to purge the snapshots of dataset %s: %w", datasetID, err)
	}
	defer res.Body.Close()
	if res.IsError() {
		raw, _ := io.ReadAll(res.Body)
		if reason := extractErrorReason(raw); reason != "" {
			return fmt.Errorf("metadata history: failed to purge the snapshots of dataset %s: %s", datasetID, reason)
		}
		return fmt.Errorf("metadata history: failed to purge the snapshots of dataset %s: %s", datasetID, res.Status())
	}
	var result map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return fmt.Errorf("metadata history: failed to read the purge response of dataset %s: %w", datasetID, err)
	}
	if timedOut, _ := result["timed_out"].(bool); timedOut {
		return fmt.Errorf("metadata history: purging the snapshots of dataset %s timed out", datasetID)
	}
	if failures, ok := result["failures"].([]interface{}); ok && len(failures) > 0 {
		return fmt.Errorf("metadata history: purging the snapshots of dataset %s reported %d failures", datasetID, len(failures))
	}
	return nil
}
