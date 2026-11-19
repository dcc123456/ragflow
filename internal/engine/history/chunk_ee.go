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
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"ragflow/internal/entity"
)

// ChunkStore is the half of the chunk version history that lives in the document
// engine: the snapshot bodies. The metadata row of a version (chunk id, version,
// document, kind, author, time) stays in chunk_version; the body is what this
// holds - the whole engine row for an edit or a restore, so restoring it is a
// copy, and the parser's field set for a baseline, whose derived fields are
// rebuilt on restore.
//
// Both kinds of engine are addressed the same way - one document per version,
// named "<chunk_id>:<version>", the pair the metadata table is keyed by - and
// every entry carries the dataset it belongs to. Where that store sits follows
// the engine's chunk table: Elasticsearch keeps one index per tenant
// (ragflow_<tenant>_history, the tenant's chunk index with "_history"
// appended), so the tenant is passed alongside the dataset and the dataset is a
// filter inside the index; Infinity keeps one table per dataset's chunk table
// (ragflow_<tenant>_<dataset>_history, the way its chunks live in
// ragflow_<tenant>_<dataset>), so the two ids name the same table its chunks
// live in.
type ChunkStore interface {
	// StashChunkHistory writes one document per entry, named by entry["id"].
	// Writing an id that already exists replaces that document, so a retried
	// write is the same write.
	StashChunkHistory(ctx context.Context, tenantID, datasetID string, docs []map[string]interface{}) error
	// LoadChunkHistory returns the snapshot JSON text of every id it finds,
	// keyed by id. An id with no document is absent from the map rather than
	// reported as an empty body.
	LoadChunkHistory(ctx context.Context, tenantID, datasetID string, ids []string) (map[string]string, error)
	// DeleteChunkHistory removes every version a document recorded and returns
	// how many documents it removed.
	DeleteChunkHistory(ctx context.Context, tenantID, datasetID string, docID string) (int64, error)
	// DeleteChunkHistoryIDs removes the named version bodies and returns how many
	// documents it removed. It is the per-version counterpart of the document
	// purge: a deleted version row can never be read again, so its body must go
	// with the row instead of being left behind as an orphan document.
	DeleteChunkHistoryIDs(ctx context.Context, tenantID, datasetID string, ids []string) (int64, error)
	// DropChunkHistory removes every version body of one dataset. A store shared
	// by a tenant's datasets (Elasticsearch) keeps its index and drops the
	// dataset's documents from it.
	DropChunkHistory(ctx context.Context, tenantID, datasetID string) error
}

// ErrChunkUnavailable reports an engine that offers no chunk history store. It
// is distinct from a failed read or write: the edition keeps chunk history only
// when the document engine can hold it, and callers treat this as "this
// deployment keeps no history" rather than as data loss.
var ErrChunkUnavailable = errors.New("chunk history: the document engine provides no chunk history store")

// ErrChunkMissing reports a version whose body the engine does not hold. It is
// returned instead of an empty snapshot: rebuilding a chunk from half its state
// would write something nobody recorded.
var ErrChunkMissing = errors.New("chunk history: the stored snapshot is missing")

// SetChunkStore installs the store of the running document engine. It is called
// once at startup, by the engine that owns the connection.
func SetChunkStore(store ChunkStore) {
	mu.Lock()
	defer mu.Unlock()
	chunkStore = store
}

// TenantResolver answers which tenant owns a dataset.
//
// Every engine's history table name carries the tenant - the one Elasticsearch
// keys its index by, the one Infinity extends the tenant's chunk table with -
// while every row and every caller names the dataset; this package cannot
// resolve it, because the DAO (which owns that lookup) imports this package. The
// resolver is therefore installed at startup, next to the store it serves.
type TenantResolver func(ctx context.Context, datasetID string) (string, error)

var (
	tenantMu       sync.Mutex
	tenantResolver TenantResolver
	// tenantCache memoizes the lookup. A dataset belongs to one tenant for life,
	// so an entry never goes stale; the map is cleared when it grows past the cap
	// rather than grown without bound, and a rebuilt entry costs one query.
	tenantCache    = make(map[string]string)
	tenantCacheCap = 4096
)

// SetTenantResolver installs the dataset -> tenant lookup. Every store names its
// history after the tenant, so every store needs it.
func SetTenantResolver(resolver TenantResolver) {
	tenantMu.Lock()
	defer tenantMu.Unlock()
	tenantResolver = resolver
	tenantCache = make(map[string]string)
}

// TenantOf returns the tenant that owns a dataset. An empty id means no resolver
// is installed, and a resolver failure is returned: a body written to the wrong
// tenant's history could never be read back.
func TenantOf(ctx context.Context, datasetID string) (string, error) {
	if datasetID == "" {
		return "", nil
	}
	tenantMu.Lock()
	cached, ok := tenantCache[datasetID]
	resolver := tenantResolver
	tenantMu.Unlock()
	if ok {
		return cached, nil
	}
	if resolver == nil {
		return "", nil
	}
	tenant, err := resolver(ctx, datasetID)
	if err != nil {
		return "", err
	}
	if tenant == "" {
		return "", fmt.Errorf("chunk history: dataset %s resolved to no tenant", datasetID)
	}
	tenantMu.Lock()
	if len(tenantCache) >= tenantCacheCap {
		tenantCache = make(map[string]string)
	}
	tenantCache[datasetID] = tenant
	tenantMu.Unlock()
	return tenant, nil
}

// ChunkAvailable reports whether this deployment keeps chunk history at all.
// Callers skip recording instead of failing an edit the engine was only never
// asked to keep.
func ChunkAvailable() bool {
	return currentChunkStore() != nil
}

// ChunkDocumentID is the id a version's body is stored under. It is the metadata
// table's primary key written as one string, so the two stores cannot disagree
// about which body belongs to which row, and a rewritten version overwrites
// exactly one document.
func ChunkDocumentID(chunkID string, version int) string {
	return chunkID + ":" + strconv.Itoa(version)
}

// StashChunkRows moves the bodies of rows that have not been written yet into
// the document engine. It returns the error of the store: the body has no second
// copy, so a caller that carried on would record a version it cannot show.
func StashChunkRows(ctx context.Context, rows []*entity.ChunkVersion) error {
	groups := make(map[string][]*entity.ChunkVersion, 1)
	for _, row := range rows {
		if row == nil || row.DatasetID == "" {
			continue
		}
		groups[row.DatasetID] = append(groups[row.DatasetID], row)
	}
	if len(groups) == 0 {
		return nil
	}
	store := currentChunkStore()
	if store == nil {
		return ErrChunkUnavailable
	}
	for datasetID, group := range groups {
		tenantID, err := TenantOf(ctx, datasetID)
		if err != nil {
			return err
		}
		docs := make([]map[string]interface{}, 0, len(group))
		for _, row := range group {
			snapshot := row.Snapshot
			if snapshot == nil {
				snapshot = entity.JSONMap{}
			}
			encoded, err := json.Marshal(snapshot)
			if err != nil {
				return fmt.Errorf("chunk history: failed to encode the snapshot of %s: %w", row.ChunkID, err)
			}
			docs = append(docs, map[string]interface{}{
				"id":         ChunkDocumentID(row.ChunkID, row.Version),
				"chunk_id":   row.ChunkID,
				"version":    row.Version,
				"doc_id":     row.DocID,
				"dataset_id": row.DatasetID,
				"snapshot":   string(encoded),
			})
		}
		if err := store.StashChunkHistory(ctx, tenantID, datasetID, docs); err != nil {
			return err
		}
		// The batch wrote these bodies, so the entity hook must not write them
		// again one request at a time.
		for _, row := range group {
			row.SnapshotStashed = true
		}
	}
	return nil
}

// StashChunk is StashChunkRows for the single row a small write produces.
func StashChunk(ctx context.Context, row *entity.ChunkVersion) error {
	return StashChunkRows(ctx, []*entity.ChunkVersion{row})
}

// init hooks the entity so a row cannot be written without its body: every
// insert path in the edition - the edit writer, the parse baseline, a rebuild -
// goes through BeforeCreate, and none of them can forget the store.
func init() {
	entity.StashVersionSnapshot = func(ctx context.Context, row *entity.ChunkVersion) error {
		if !ChunkAvailable() {
			// An engine that cannot hold a body keeps no history at all; the
			// callers above it record no rows on such a deployment.
			return nil
		}
		return StashChunk(ctx, row)
	}
}

// HydrateChunks loads the snapshot bodies a row does not carry, leaving rows
// that already hold theirs untouched.
//
// Every reader of a version needs the body - a replay to rebuild the chunk, the
// history panel to show it, a diff to compare two of them - and none of them
// should know where it came from. Hydrating is that boundary.
//
// One missing body fails the call: a replay that quietly rebuilt a chunk from an
// empty snapshot would write a document nobody recorded, and the caller is the
// one that knows whether it can live with that.
func HydrateChunks(ctx context.Context, rows []*entity.ChunkVersion) error {
	groups := make(map[string][]*entity.ChunkVersion, 1)
	for _, row := range rows {
		if row == nil || row.DatasetID == "" || row.Snapshot != nil {
			continue
		}
		groups[row.DatasetID] = append(groups[row.DatasetID], row)
	}
	if len(groups) == 0 {
		return nil
	}
	store := currentChunkStore()
	if store == nil {
		return ErrChunkUnavailable
	}

	var failure error
	for datasetID, group := range groups {
		tenantID, err := TenantOf(ctx, datasetID)
		if err != nil {
			if failure == nil {
				failure = err
			}
			continue
		}
		ids := make([]string, 0, len(group))
		byID := make(map[string]*entity.ChunkVersion, len(group))
		for _, row := range group {
			id := ChunkDocumentID(row.ChunkID, row.Version)
			if _, seen := byID[id]; !seen {
				ids = append(ids, id)
			}
			byID[id] = row
		}
		bodies, err := store.LoadChunkHistory(ctx, tenantID, datasetID, ids)
		if err != nil {
			if failure == nil {
				failure = err
			}
			continue
		}
		for id, row := range byID {
			body, ok := bodies[id]
			if !ok {
				if failure == nil {
					failure = fmt.Errorf("%w: chunk %s version %d of dataset %s",
						ErrChunkMissing, row.ChunkID, row.Version, datasetID)
				}
				continue
			}
			snapshot := entity.JSONMap{}
			if body != "" {
				if err := json.Unmarshal([]byte(body), &snapshot); err != nil {
					if failure == nil {
						failure = fmt.Errorf("chunk history: failed to decode the snapshot of %s version %d: %w",
							row.ChunkID, row.Version, err)
					}
					continue
				}
			}
			row.Snapshot = snapshot
		}
	}
	return failure
}

// DeleteChunkDocument removes the history a document recorded, which is what a
// document purge asks for: its chunks' bodies must go with it.
func DeleteChunkDocument(ctx context.Context, datasetID, docID string) error {
	store := currentChunkStore()
	if store == nil {
		return ErrChunkUnavailable
	}
	tenantID, err := TenantOf(ctx, datasetID)
	if err != nil {
		return err
	}
	_, err = store.DeleteChunkHistory(ctx, tenantID, datasetID, docID)
	return err
}

// DeleteChunkDocuments removes the bodies of the named versions.
//
// A version row that is gone can never be read again, so its body is one
// document per deleted version that nobody will ever name - this is what keeps
// a version delete from growing the engine store for the life of the dataset.
func DeleteChunkDocuments(ctx context.Context, datasetID string, ids []string) (int64, error) {
	if datasetID == "" || len(ids) == 0 {
		return 0, nil
	}
	store := currentChunkStore()
	if store == nil {
		return 0, ErrChunkUnavailable
	}
	tenantID, err := TenantOf(ctx, datasetID)
	if err != nil {
		return 0, err
	}
	return store.DeleteChunkHistoryIDs(ctx, tenantID, datasetID, ids)
}

// DeleteChunkDataset removes the whole history of a dataset.
func DeleteChunkDataset(ctx context.Context, datasetID string) error {
	store := currentChunkStore()
	if store == nil {
		return ErrChunkUnavailable
	}
	tenantID, err := TenantOf(ctx, datasetID)
	if err != nil {
		return err
	}
	return store.DropChunkHistory(ctx, tenantID, datasetID)
}
