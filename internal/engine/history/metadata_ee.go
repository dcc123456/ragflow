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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"ragflow/internal/entity"
)

// MetadataStore is the half of the document version history that lives in the
// document engine: the metadata values. A metadata state row (document id,
// dataset, moment, snapshot name) stays in document_version; the values are user
// data - arbitrary strings of arbitrary length - and keeping them in the row
// would make every version as large as the values themselves.
//
// One snapshot holds one document's values, named by content within the
// document: a document whose values move back and forth stores each distinct
// snapshot once, and a rewrite of the same values reaches the same name instead
// of appending a second copy. That is the one difference from ChunkStore, whose
// snapshots are per version. The name carries the document id, so a purge of one
// document's history can drop its snapshots without touching another document's
// - even when two documents happen to hold the same values.
//
// The store is addressed by tenant and dataset: Elasticsearch and Infinity both
// keep a tenant's doc_meta rows in one table (ragflow_doc_meta_<tenant>), so the
// history mirrors that and narrows a read to one dataset's slice of it. The tenant
// is resolved from the dataset by the package, not passed by every caller.
type MetadataStore interface {
	// StashMetadataHistory writes one document per entry, named by entry["id"].
	// Writing an id that already exists replaces that document, so a retried
	// write is the same write.
	StashMetadataHistory(ctx context.Context, tenantID, datasetID string, docs []map[string]interface{}) error
	// LoadMetadataHistory returns the values JSON of every id it finds, keyed by
	// id. An id with no document is absent from the map rather than reported as
	// an empty snapshot.
	LoadMetadataHistory(ctx context.Context, tenantID, datasetID string, ids []string) (map[string]string, error)
	// DeleteMetadataHistory removes every snapshot a document recorded and
	// returns how many it removed.
	DeleteMetadataHistory(ctx context.Context, tenantID, datasetID, documentID string) (int64, error)
	// DropMetadataHistory removes every snapshot a dataset recorded.
	DropMetadataHistory(ctx context.Context, tenantID, datasetID string) error
}

// ErrMetadataUnavailable reports an engine that offers no metadata history
// store. The values have no other home - the version row does not carry them -
// so a write is refused instead of being recorded without them. A caller that
// can live without the history asks MetadataAvailable first and records nothing.
var ErrMetadataUnavailable = errors.New("metadata history: the document engine provides no metadata history store")

// ErrMetadataMissing reports a snapshot whose values the engine does not hold.
// It is returned instead of an empty map: an empty map is how a removal is
// recorded, so answering it for a lost snapshot would erase a document's
// metadata.
var ErrMetadataMissing = errors.New("metadata history: the stored values are missing")

// SetMetadataStore installs the store of the running document engine. It is
// called once at startup, by the engine that owns the connection.
func SetMetadataStore(store MetadataStore) {
	mu.Lock()
	defer mu.Unlock()
	metadataStore = store
}

// MetadataAvailable reports whether this deployment keeps the values in the
// engine at all. There is nowhere else to keep them, so a deployment without it
// records no metadata history.
func MetadataAvailable() bool {
	return currentMetadataStore() != nil
}

// snapshotHashBytes is how much of the digest names a snapshot: 16 bytes, i.e.
// 32 hex characters. The document id already scopes the name, so the digest only
// has to keep two values apart inside one document - 128 bits is far past what
// that needs, and the rest would only widen every engine document id.
const snapshotHashBytes = 16

// SnapshotID names the snapshot one document's values are stored under: the
// document, then the digest of the values. The document scopes a purge; the
// digest keeps values that did not change from being stored twice, so two
// versions carrying them name one snapshot. A nil map and an empty one are the
// same snapshot - a document with no metadata - so both reach one name.
func SnapshotID(documentID string, values entity.JSONMap) string {
	encoded, err := json.Marshal(snapshotValues(values))
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return documentID + ":" + hex.EncodeToString(digest[:snapshotHashBytes])
}

// snapshotValues is the map a snapshot stores. Normalising here keeps the digest
// and the stored bytes in step: json.Marshal writes a nil map as null, so
// without this the same "no metadata" would reach two names holding two shapes.
func snapshotValues(values entity.JSONMap) entity.JSONMap {
	if len(values) == 0 {
		return entity.JSONMap{}
	}
	return values
}

// StashMetadata moves the values of one row into the document engine, leaving
// the row naming the snapshot.
//
// A store that refuses the write is returned to the caller rather than swallowed:
// the values are the only copy of what a version described, so a row recorded
// without them would replay an empty map over a document's metadata. That is the
// same rule chunk history follows for a body.
//
// An engine that offers no store at all is ErrMetadataUnavailable for the same
// reason: the version row no longer carries the values, so there is nowhere to
// put them and nothing to fall back on.
func StashMetadata(ctx context.Context, row *entity.DocumentVersion) error {
	return StashMetadataRows(ctx, []*entity.DocumentVersion{row})
}

// StashMetadataRows is StashMetadata for the rows a batch write produced.
func StashMetadataRows(ctx context.Context, rows []*entity.DocumentVersion) error {
	groups := make(map[string][]*entity.DocumentVersion, 1)
	for _, row := range rows {
		if row == nil || row.DocumentID == "" || row.DatasetID == "" {
			continue
		}
		// An empty map is a removal, and a removal is a snapshot of its own: it
		// is what a replay of that version puts back, so it is stored like any
		// other.
		groups[row.DatasetID] = append(groups[row.DatasetID], row)
	}
	if len(groups) == 0 {
		return nil
	}
	store := currentMetadataStore()
	if store == nil {
		return ErrMetadataUnavailable
	}
	for datasetID, group := range groups {
		docs := make([]map[string]interface{}, 0, len(group))
		// The stashed rows travel with the snapshot each one named, so a row
		// that could not be stored keeps its values instead of being pointed at
		// another row's snapshot - the two lists cannot drift apart.
		stashed := make([]*entity.DocumentVersion, 0, len(group))
		snapshotIDs := make([]string, 0, len(group))
		for _, row := range group {
			values := snapshotValues(row.MetaFields)
			encoded, err := json.Marshal(values)
			if err != nil {
				return fmt.Errorf("metadata history: failed to encode the values of document %s: %w",
					row.DocumentID, err)
			}
			snapshotID := SnapshotID(row.DocumentID, values)
			docs = append(docs, map[string]interface{}{
				"id":          snapshotID,
				"document_id": row.DocumentID,
				"dataset_id":  datasetID,
				"snapshot":    string(encoded),
			})
			stashed = append(stashed, row)
			snapshotIDs = append(snapshotIDs, snapshotID)
		}
		if len(docs) == 0 {
			continue
		}
		tenantID, err := TenantOf(ctx, datasetID)
		if err != nil {
			return err
		}
		if err := store.StashMetadataHistory(ctx, tenantID, datasetID, docs); err != nil {
			return fmt.Errorf("metadata history: failed to store the values of dataset %s: %w", datasetID, err)
		}
		// The rows are written without the values from here on, and they name
		// the snapshot that holds them. A caller that still holds the row reads
		// it back through HydrateMetadata like everybody else, so there is one
		// shape of truth instead of two.
		for i, row := range stashed {
			row.MetaFieldsSnapshotID = snapshotIDs[i]
			row.MetaFields = nil
			// The batch wrote these snapshots, so the entity hook must not write
			// them again one request at a time.
			row.MetaFieldsStashed = true
		}
	}
	return nil
}

// HydrateMetadata loads the values that live in the document engine; a row that
// names no snapshot has nothing to load and is left alone.
//
// Every reader of a version needs the values - a replay to put them back, the
// writer to tell whether they moved - and none of them should know where they
// came from. Hydrating is that boundary: after it, a row carries its values.
//
// One error is enough to fail the call: a replay that quietly wrote an empty map
// would erase a document's metadata, and the caller is the one that knows
// whether it can live with that.
func HydrateMetadata(ctx context.Context, rows []*entity.DocumentVersion) error {
	groups := make(map[string][]*entity.DocumentVersion, 1)
	for _, row := range rows {
		if row == nil || row.MetaFieldsSnapshotID == "" || len(row.MetaFields) != 0 {
			continue
		}
		groups[row.DatasetID] = append(groups[row.DatasetID], row)
	}
	if len(groups) == 0 {
		return nil
	}
	store := currentMetadataStore()
	if store == nil {
		return ErrMetadataUnavailable
	}

	var failure error
	for datasetID, group := range groups {
		ids := make([]string, 0, len(group))
		for _, row := range group {
			ids = append(ids, row.MetaFieldsSnapshotID)
		}
		tenantID, err := TenantOf(ctx, datasetID)
		if err != nil {
			if failure == nil {
				failure = err
			}
			continue
		}
		bodies, err := store.LoadMetadataHistory(ctx, tenantID, datasetID, ids)
		if err != nil {
			if failure == nil {
				failure = err
			}
			continue
		}
		for _, row := range group {
			body, ok := bodies[row.MetaFieldsSnapshotID]
			if !ok {
				if failure == nil {
					failure = fmt.Errorf("%w: document %s of dataset %s", ErrMetadataMissing, row.DocumentID, datasetID)
				}
				continue
			}
			values := entity.JSONMap{}
			if body != "" {
				if err := json.Unmarshal([]byte(body), &values); err != nil {
					if failure == nil {
						failure = fmt.Errorf("metadata history: failed to decode the values of document %s: %w",
							row.DocumentID, err)
					}
					continue
				}
			}
			row.MetaFields = values
		}
	}
	return failure
}

// DeleteMetadataDocument removes the snapshots a document recorded, which is what
// a document purge asks for: its values must go with the history that names them.
func DeleteMetadataDocument(ctx context.Context, datasetID, documentID string) error {
	store := currentMetadataStore()
	if store == nil {
		return ErrMetadataUnavailable
	}
	tenantID, err := TenantOf(ctx, datasetID)
	if err != nil {
		return err
	}
	_, err = store.DeleteMetadataHistory(ctx, tenantID, datasetID, documentID)
	return err
}

// DeleteMetadataDataset removes the whole metadata history of a dataset.
func DeleteMetadataDataset(ctx context.Context, datasetID string) error {
	store := currentMetadataStore()
	if store == nil {
		return ErrMetadataUnavailable
	}
	tenantID, err := TenantOf(ctx, datasetID)
	if err != nil {
		return err
	}
	return store.DropMetadataHistory(ctx, tenantID, datasetID)
}

// init hooks the entity so a metadata row cannot be written without its values:
// every insert path in the edition - an edit, a batch update, the parser's own
// writes, a restore - goes through BeforeCreate, and none of them can forget the
// store. It is the same seam chunk history uses for a version's body.
func init() {
	entity.StashMetadataValues = func(ctx context.Context, row *entity.DocumentVersion) error {
		// No store is ErrMetadataUnavailable, not a pass: the row would name no
		// snapshot and a replay would read it as a document with no metadata.
		return StashMetadata(ctx, row)
	}
}
