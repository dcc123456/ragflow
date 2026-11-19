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

// ChunkHistoryIndexSuffix names, together with the tenant id, the index that
// holds version bodies. One index per tenant, the way a tenant's chunks live in
// one index (ragflow_<tenant>), so a dataset's history sits beside the chunks it
// describes and the number of indices follows tenants rather than datasets.
//
// The name is the chunk index with the suffix appended (ragflow_<tenant> ->
// ragflow_<tenant>_history): the history is named after the index it belongs to,
// the way the doc_meta history is named after ragflow_doc_meta_<tenant>.
const ChunkHistoryIndexSuffix = "_history"

// ChunkHistoryIndexPrefix is the part of the name that does not depend on the
// tenant. It is what the drop and the template match on.
const ChunkHistoryIndexPrefix = "ragflow_"

// ChunkHistoryIndexName is the index a tenant's version bodies live in: the
// tenant's chunk index with ChunkHistoryIndexSuffix appended.
func ChunkHistoryIndexName(tenantID string) string {
	return ChunkHistoryIndexPrefix + tenantID + ChunkHistoryIndexSuffix
}

// chunkHistoryMapping is written explicitly instead of inherited from the
// "ragflow_*" chunk template: the snapshot is stored, never searched, so it is
// kept in _source and left out of the inverted index and the doc values. The
// explicit properties win over the template's, and "dynamic": false keeps the
// template's chunk fields from being filled in by anything written here.
var chunkHistoryMapping = map[string]interface{}{
	"settings": map[string]interface{}{
		"number_of_shards":   1,
		"number_of_replicas": 1,
	},
	"mappings": map[string]interface{}{
		"dynamic": false,
		"properties": map[string]interface{}{
			"id":         map[string]interface{}{"type": "keyword"},
			"chunk_id":   map[string]interface{}{"type": "keyword"},
			"version":    map[string]interface{}{"type": "integer"},
			"doc_id":     map[string]interface{}{"type": "keyword"},
			"dataset_id": map[string]interface{}{"type": "keyword"},
			"snapshot":   map[string]interface{}{"type": "keyword", "index": false, "doc_values": false},
		},
	},
}

// chunkHistoryIndex resolves the index a call addresses. The tenant names the
// index; the dataset is a filter inside it, because one index holds every
// dataset of a tenant and a body must never be read through another dataset's
// name.
func chunkHistoryIndex(tenantID string) (string, error) {
	if tenantID == "" {
		return "", fmt.Errorf("chunk history: no tenant for the request; the history index is keyed by tenant")
	}
	return ChunkHistoryIndexName(tenantID), nil
}

func (e *Engine) ensureChunkHistoryIndex(ctx context.Context, indexName string) error {
	exists, err := e.indexExists(ctx, indexName)
	if err != nil {
		return fmt.Errorf("failed to check the chunk history index %s: %w", indexName, err)
	}
	if exists {
		return nil
	}
	body, err := json.Marshal(chunkHistoryMapping)
	if err != nil {
		return fmt.Errorf("failed to encode the chunk history mapping: %w", err)
	}
	req := esapi.IndicesCreateRequest{Index: indexName, Body: bytes.NewReader(body)}
	res, err := req.Do(ctx, e.client)
	if err != nil {
		return fmt.Errorf("failed to create the chunk history index %s: %w", indexName, err)
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
			return fmt.Errorf("failed to create the chunk history index %s: %s", indexName, reason)
		}
		return fmt.Errorf("failed to create the chunk history index %s: %s", indexName, res.Status())
	}
	return nil
}

// StashChunkHistory writes one document per entry, named by entry["id"].
//
// The write refreshes: a version is read back by the panel and by a restore as
// soon as the edit returns, and a body that is not visible yet would be reported
// as missing.
func (e *Engine) StashChunkHistory(ctx context.Context, tenantID, datasetID string, docs []map[string]interface{}) error {
	if datasetID == "" || len(docs) == 0 {
		return nil
	}
	indexName, err := chunkHistoryIndex(tenantID)
	if err != nil {
		return err
	}
	if err := e.ensureChunkHistoryIndex(ctx, indexName); err != nil {
		return err
	}

	var buf bytes.Buffer
	for _, doc := range docs {
		id, _ := doc["id"].(string)
		if id == "" {
			return fmt.Errorf("chunk history: a snapshot arrived without an id")
		}
		// A bulk line is two lines: an action naming the index and the id, then the
		// document. The action verb is part of the line - without it the reader
		// takes this for a document ("expected field [create], [delete], [index]
		// or [update]").
		meta, err := json.Marshal(map[string]interface{}{
			"index": map[string]interface{}{"_index": indexName, "_id": id},
		})
		if err != nil {
			return fmt.Errorf("chunk history: failed to encode the bulk metadata of %s: %w", id, err)
		}
		source, err := json.Marshal(doc)
		if err != nil {
			return fmt.Errorf("chunk history: failed to encode the snapshot of %s: %w", id, err)
		}
		buf.Write(meta)
		buf.WriteByte('\n')
		buf.Write(source)
		buf.WriteByte('\n')
	}

	req := esapi.BulkRequest{Body: bytes.NewReader(buf.Bytes()), Refresh: "true"}
	res, err := req.Do(ctx, e.client)
	if err != nil {
		return fmt.Errorf("chunk history: failed to write a snapshot batch to %s: %w", indexName, err)
	}
	defer res.Body.Close()
	if res.IsError() {
		raw, _ := io.ReadAll(res.Body)
		if reason := extractErrorReason(raw); reason != "" {
			return fmt.Errorf("chunk history: failed to write a snapshot batch to %s: %s", indexName, reason)
		}
		return fmt.Errorf("chunk history: failed to write a snapshot batch to %s: %s", indexName, res.Status())
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
		return fmt.Errorf("chunk history: failed to read the bulk response of %s: %w", indexName, err)
	}
	if result.Errors {
		for _, item := range result.Items {
			for _, op := range item {
				if op.Error != nil {
					return fmt.Errorf("chunk history: failed to write a snapshot to %s: %s", indexName, op.Error.Reason)
				}
			}
		}
		return fmt.Errorf("chunk history: failed to write a snapshot batch to %s", indexName)
	}
	return nil
}

// LoadChunkHistory returns the snapshot text of every id it finds. An id with no
// document is absent from the map: the caller turns that into a missing body
// instead of an empty chunk.
func (e *Engine) LoadChunkHistory(ctx context.Context, tenantID, datasetID string, ids []string) (map[string]string, error) {
	bodies := make(map[string]string, len(ids))
	if datasetID == "" || len(ids) == 0 {
		return bodies, nil
	}
	indexName, err := chunkHistoryIndex(tenantID)
	if err != nil {
		return nil, err
	}
	exists, err := e.indexExists(ctx, indexName)
	if err != nil {
		return nil, fmt.Errorf("failed to check the chunk history index %s: %w", indexName, err)
	}
	if !exists {
		return bodies, nil
	}

	// One request per read, bounded by the ids the caller asked for: the store is
	// addressed by id, and the dataset narrows it to the rows this document owns.
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
		return nil, fmt.Errorf("chunk history: failed to encode a snapshot lookup: %w", err)
	}
	res, err := e.client.Search(
		e.client.Search.WithContext(ctx),
		e.client.Search.WithIndex(indexName),
		e.client.Search.WithBody(bytes.NewReader(body)),
	)
	if err != nil {
		return nil, fmt.Errorf("chunk history: failed to read snapshots from %s: %w", indexName, err)
	}
	defer res.Body.Close()
	if res.IsError() {
		raw, _ := io.ReadAll(res.Body)
		if reason := extractErrorReason(raw); reason != "" {
			return nil, fmt.Errorf("chunk history: failed to read snapshots from %s: %s", indexName, reason)
		}
		return nil, fmt.Errorf("chunk history: failed to read snapshots from %s: %s", indexName, res.Status())
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
		return nil, fmt.Errorf("chunk history: failed to read the snapshot response of %s: %w", indexName, err)
	}
	for _, hit := range result.Hits.Hits {
		if hit.Source.ID != "" {
			bodies[hit.Source.ID] = hit.Source.Snapshot
		}
	}
	return bodies, nil
}

// DeleteChunkHistory removes every version a document recorded and reports how
// many documents went.
func (e *Engine) DeleteChunkHistory(ctx context.Context, tenantID, datasetID, docID string) (int64, error) {
	if datasetID == "" || docID == "" {
		return 0, nil
	}
	return e.deleteChunkHistoryByQuery(ctx, tenantID, datasetID, map[string]interface{}{
		"term": map[string]interface{}{"doc_id": docID},
	}, fmt.Sprintf("the snapshots of document %s", docID))
}

// DeleteChunkHistoryIDs removes the named version bodies and reports how many
// documents went.
func (e *Engine) DeleteChunkHistoryIDs(ctx context.Context, tenantID, datasetID string, ids []string) (int64, error) {
	if datasetID == "" || len(ids) == 0 {
		return 0, nil
	}
	return e.deleteChunkHistoryByQuery(ctx, tenantID, datasetID, map[string]interface{}{
		"terms": map[string]interface{}{"id": ids},
	}, fmt.Sprintf("%d version bodies", len(ids)))
}

// deleteChunkHistoryByQuery deletes the documents one dataset matches.
//
// The dataset is always part of the filter: an index holds every dataset of a
// tenant, so a purge that named only a document would remove another dataset's
// body if an id ever repeated across datasets.
func (e *Engine) deleteChunkHistoryByQuery(ctx context.Context, tenantID, datasetID string, match map[string]interface{}, what string) (int64, error) {
	indexName, err := chunkHistoryIndex(tenantID)
	if err != nil {
		return 0, err
	}
	exists, err := e.indexExists(ctx, indexName)
	if err != nil {
		return 0, fmt.Errorf("failed to check the chunk history index %s: %w", indexName, err)
	}
	if !exists {
		return 0, nil
	}
	body, err := json.Marshal(map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must":   []interface{}{match},
				"filter": []interface{}{map[string]interface{}{"term": map[string]interface{}{"dataset_id": datasetID}}},
			},
		},
	})
	if err != nil {
		return 0, fmt.Errorf("chunk history: failed to encode the purge of %s: %w", what, err)
	}
	req := esapi.DeleteByQueryRequest{
		Index:   []string{indexName},
		Body:    bytes.NewReader(body),
		Refresh: func(b bool) *bool { return &b }(true),
	}
	res, err := req.Do(ctx, e.client)
	if err != nil {
		return 0, fmt.Errorf("chunk history: failed to purge %s: %w", what, err)
	}
	defer res.Body.Close()
	if res.IsError() {
		raw, _ := io.ReadAll(res.Body)
		if reason := extractErrorReason(raw); reason != "" {
			return 0, fmt.Errorf("chunk history: failed to purge %s: %s", what, reason)
		}
		return 0, fmt.Errorf("chunk history: failed to purge %s: %s", what, res.Status())
	}
	var result map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("chunk history: failed to read the purge response of %s: %w", what, err)
	}
	deleted, _ := result["deleted"].(float64)
	// A delete that timed out or reported failures leaves bodies behind for rows
	// that no longer exist, so it is an error rather than a count.
	if timedOut, _ := result["timed_out"].(bool); timedOut {
		return int64(deleted), fmt.Errorf("chunk history: purging %s timed out after %d documents", what, int64(deleted))
	}
	if failures, ok := result["failures"].([]interface{}); ok && len(failures) > 0 {
		return int64(deleted), fmt.Errorf("chunk history: purging %s reported %d failures", what, len(failures))
	}
	return int64(deleted), nil
}

// DropChunkHistory removes a dataset's whole chunk history.
//
// The index belongs to the tenant, not to the dataset, so it stays: dropping it
// would take every other dataset of the tenant's history with it. What goes is
// this dataset's slice of it.
func (e *Engine) DropChunkHistory(ctx context.Context, tenantID, datasetID string) error {
	if datasetID == "" {
		return nil
	}
	indexName, err := chunkHistoryIndex(tenantID)
	if err != nil {
		return err
	}
	// Only a name this file would have built can be purged: the chunk index
	// itself is the same name without the suffix, and it must never go with the
	// history.
	if !strings.HasPrefix(indexName, ChunkHistoryIndexPrefix) || !strings.HasSuffix(indexName, ChunkHistoryIndexSuffix) {
		return fmt.Errorf("chunk history: refusing to drop %q", indexName)
	}
	if _, err := e.deleteChunkHistoryByQuery(ctx, tenantID, datasetID, map[string]interface{}{
		"match_all": map[string]interface{}{},
	}, fmt.Sprintf("the chunk history of dataset %s", datasetID)); err != nil {
		return err
	}
	return nil
}
