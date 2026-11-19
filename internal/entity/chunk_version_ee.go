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

package entity

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// ChunkVersionSystemActor is the actor a history row carries when no user wrote
// it. Two kinds of row have no user behind them: the parse baseline and the
// tombstones a parse produces, and every row a dataset restore's replay writes.
//
// A chunk edit, a chunk delete and a single-chunk restore name the user who asked
// for them. A dataset restore is a dataset-level operation - who asked for it is
// recorded on the dataset version row it closes - so its chunk rows say what
// happened to the chunk (the replay put it back, the replay dropped it) instead
// of crediting the operator with a chunk edit they never made.
const ChunkVersionSystemActor = "system"

// ChunkVersionEmptyDocumentID marks a parse that produced no chunks, so a
// restore can restore the empty set instead of reporting a lost history.
// Readers skip it: it addresses no chunk.
const ChunkVersionEmptyDocumentID = "__document__"

// ChunkVersion is one snapshot of a source chunk's state, appended when an edit
// changes the chunk body or its picture; an edit that only touches annotations
// rewrites the current version in place, and a restore always appends. Version 0
// is captured lazily on a chunk's first edit. The chunks themselves live in the
// document engine; this table is the history beside them.
//
// The (chunk_id, version) pair is the key: every read names a chunk first, so
// clustering by that pair turns each read into one range scan.
type ChunkVersion struct {
	// ChunkID is the engine row id, and those are not all 32 characters: a
	// parsed chunk is named by a 64-bit hash (16), while a compiled product is
	// named by knowledge_compiler.common.StableRowID - a SHA-256 digest, so
	// exactly 64 hex characters - or by a dataset-level structure id that
	// prefixes "dataset_structure_" to one (82). The parse baseline of a
	// document carries the products its run wrote, so a column sized for the
	// 16-character parse id rejected the whole batch with "Data too long for
	// column 'chunk_id'", and that document kept no history at all - which a
	// later restore can only report as "no chunk history".
	// 128 leaves room for another prefixed scheme without another migration.
	ChunkID string `gorm:"column:chunk_id;size:128;primaryKey" json:"chunk_id"`
	Version int    `gorm:"column:version;primaryKey" json:"version"`
	// DocID scopes a replay to one document: a document delete reads every
	// version of it to collect the pictures they hold, so the lookup is by
	// doc_id alone.
	DocID string `gorm:"column:doc_id;size:32;not null;index:idx_chunk_version_doc" json:"doc_id"`
	// DatasetID scopes the history to the dataset the chunk belongs to, so a
	// chunk id can never resolve across datasets.
	DatasetID string `gorm:"column:dataset_id;size:32;not null;index:idx_chunk_version_dataset" json:"dataset_id"`
	// DatasetVersion is the dataset version this row was written under, and the
	// anchor a replay compares against. It is the dataset's sequence number
	// rather than a timestamp because two edits of one chunk can land in the
	// same millisecond. Always set: a row with no version could not be placed
	// on the timeline.
	DatasetVersion int64 `gorm:"column:dataset_version;not null" json:"dataset_version"`
	// RestoredFrom is the version a restore copied this row from; 0 for a row
	// recorded by a normal edit.
	RestoredFrom int `gorm:"column:restored_from;not null;default:0" json:"restored_from"`
	// CreatedBy is the authenticated user that produced the version.
	CreatedBy string `gorm:"column:created_by;size:32;not null;default:''" json:"created_by"`
	// Kind records what produced the row: "parse" is the baseline the parser
	// wrote, "delete" marks the chunk as gone from that point on. It is read off
	// a returned row and never filtered on, so it carries no index. Every writer
	// sets it; the default only keeps the column definition identical to the
	// other two kind columns.
	Kind string `gorm:"column:kind;size:16;not null;default:''" json:"kind"`
	// Snapshot holds the version's state: the whole engine row, tokens and vector
	// included, so a restore is a copy. A row written without that shape is not
	// admitted by chunkversion.FullSnapshotRow and is rebuilt from its text.
	//
	// It is not a column: the state lives in the document engine, one document per
	// version named "<chunk_id>:<version>", and a reader gets it back through
	// history.HydrateChunks.
	Snapshot JSONMap `gorm:"-" json:"snapshot,omitempty"`
	// SnapshotStashed records that this row's body is already in the engine, so
	// the per-row hook does not store it a second time.
	SnapshotStashed bool `gorm:"-" json:"-"`
	// CreateTime is the millisecond the row was written. Display only: the
	// anchor a replay ranges over is DatasetVersion. BeforeCreate always stamps
	// it, so the column is never NULL; the default only covers a write that
	// bypasses the model hook.
	CreateTime *int64 `gorm:"column:create_time;not null;default:0" json:"create_time,omitempty"`
}

// BeforeCreate stamps the moment the row was appended, from the database's
// clock. Ordering comes from DatasetVersion, not from this column, so two rows
// written in the same millisecond are still ordered.
func (v *ChunkVersion) BeforeCreate(tx *gorm.DB) error {
	if v == nil {
		return nil
	}
	// A replay selects "dataset_version <= anchor", so a row carrying none
	// would be read by every anchor. Report the bug instead of attaching the
	// row to the baseline.
	if v.DatasetVersion <= 0 {
		return fmt.Errorf("chunk %s v%d carries no dataset version: a history row is addressed by the version that wrote it",
			v.ChunkID, v.Version)
	}
	// The body is written to the document engine before the row that names it.
	// A row whose state nobody holds would replay as an empty chunk, so the
	// write fails instead.
	if v.Snapshot != nil && !v.SnapshotStashed && StashVersionSnapshot != nil {
		if err := StashVersionSnapshot(tx.Statement.Context, v); err != nil {
			return err
		}
	}
	if v.CreateTime != nil {
		return nil
	}
	if timestamp, err := DBTimeMillis(tx); err == nil {
		v.CreateTime = &timestamp
		return nil
	}
	timestamp, _ := autoModelTime()
	v.CreateTime = &timestamp
	return nil
}

// Chunk version kinds: written by the parser, an editor adding or changing a
// chunk, a removal, or a restore. A replay reads them as one timeline.
const (
	ChunkVersionKindParse   = "parse"
	ChunkVersionKindAdd     = "add"
	ChunkVersionKindEdit    = "edit"
	ChunkVersionKindDelete  = "delete"
	ChunkVersionKindRestore = "restore"
)

// TableName specify table name
func (ChunkVersion) TableName() string {
	return "chunk_version"
}

// StashVersionSnapshot stores a version's body in the document engine. It is
// installed by internal/engine/history, which owns that store.
var StashVersionSnapshot func(ctx context.Context, row *ChunkVersion) error
