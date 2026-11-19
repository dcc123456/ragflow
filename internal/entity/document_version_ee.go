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

	"gorm.io/gorm"
)

// DocumentVersion is the state of one document as one dataset version produced
// it. Parse settings, metadata values and the name are told apart by Kind.
type DocumentVersion struct {
	// DocumentID, Kind and DatasetVersion are the primary key, in that order: the
	// only keyed read is "newest state of this document of this kind at or before
	// a version", which is exactly that prefix (equality on two columns, a
	// descending range on the third), so the primary key answers it without a
	// second index.
	DocumentID string `gorm:"column:document_id;size:32;not null;primaryKey" json:"document_id"`
	// Kind is the version's kind: document_config for parse settings,
	// document_meta for the values. size:16 is the width the DatasetVersionKind*
	// values are held to (the longest is document_config at 15), the same width
	// dataset_version.kind and chunk_version.kind use.
	Kind string `gorm:"column:kind;size:16;not null;default:'';primaryKey;index:idx_document_version_dataset,priority:2" json:"kind"`
	// DatasetVersion is the version number that produced this state.
	DatasetVersion int64 `gorm:"column:dataset_version;not null;primaryKey;index:idx_document_version_dataset,priority:3" json:"dataset_version"`
	// DatasetID scopes the rows to the dataset the document belongs to.
	DatasetID string `gorm:"column:dataset_id;size:32;not null;index:idx_document_version_dataset,priority:1" json:"dataset_id"`
	// ParserID, PipelineID and PipelineVersionID are the parse settings the
	// document ran under when this version was recorded.
	ParserID          string  `gorm:"column:parser_id;size:32;not null;default:''" json:"parser_id"`
	PipelineID        *string `gorm:"column:pipeline_id;size:32" json:"pipeline_id,omitempty"`
	PipelineVersionID *string `gorm:"column:pipeline_version_id;size:32" json:"pipeline_version_id,omitempty"`
	// ParserConfig is the full parser configuration of the version.
	ParserConfig JSONMap `gorm:"column:parser_config;type:longtext" json:"parser_config"`
	// MetaFields carries the metadata map while the row is being built. It is
	// not a column; the values live in the document engine.
	MetaFields JSONMap `gorm:"-" json:"meta_fields"`
	// MetaFieldsSnapshotID names the document-engine snapshot holding this row's
	// values: "<document_id>:<hash>", 32 + 1 + 32 = 65 characters today
	// (history.SnapshotID truncates the digest to 16 bytes). size:128 also holds
	// the 97 characters a full sha256 would take, so widening the digest later
	// needs no migration, while the column stays narrow enough to index.
	MetaFieldsSnapshotID string `gorm:"column:meta_fields_snapshot_id;size:128" json:"meta_fields_snapshot_id,omitempty"`
	// MetaFieldsStashed marks values a batch writer already stored, so the
	// per-row hook does not store them again.
	MetaFieldsStashed bool `gorm:"-" json:"-"`
	// Status is the document's visibility when the row was written, for the
	// document_status rows only. It is a column and not a document-engine
	// snapshot: the value is one byte, and a restore reads it to put the switch
	// back without touching the store the metadata values live in.
	Status *string `gorm:"column:status;size:8" json:"status,omitempty"`
}

// TableName specify table name
func (DocumentVersion) TableName() string {
	return "document_version"
}

// BeforeCreate stores a metadata row's values in the document engine before the
// row is written.
func (v *DocumentVersion) BeforeCreate(tx *gorm.DB) error {
	if v == nil {
		return nil
	}
	if v.Kind != DatasetVersionKindDocumentMetadata || v.MetaFieldsStashed ||
		v.MetaFieldsSnapshotID != "" || StashMetadataValues == nil {
		return nil
	}
	return StashMetadataValues(tx.Statement.Context, v)
}

// StashMetadataValues stores a row's values in the document engine. It is
// installed by internal/engine/history.
var StashMetadataValues func(ctx context.Context, row *DocumentVersion) error
