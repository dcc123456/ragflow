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
	"gorm.io/gorm"
)

// DatasetVersion is one snapshot of a dataset's configuration, appended whenever
// a request changes it. Rows are append-only: a restore appends the state it
// produced, and version 0 is captured lazily from the state a change started
// from. The dataset row stays the live state; this table is the history beside
// it.
type DatasetVersion struct {
	// (dataset_id, version) is the primary key: every lookup and the restore
	// path use that pair, so the rows cluster by it and "latest version of a
	// dataset" is a range scan.
	DatasetID string `gorm:"column:dataset_id;size:32;not null;primaryKey;index:idx_dataset_version_config,priority:1;index:idx_dataset_version_documents,priority:1" json:"dataset_id"`
	Version   int64  `gorm:"column:version;not null;primaryKey" json:"version"`
	// Kind is one of the DatasetVersionKind* constants. size:16 is the width
	// those values are held to (the longest is document_config at 15); it is the
	// same width chunk_version.kind uses. No index of its own: every query pins
	// one dataset, and a selective kind lookup would want (dataset_id, kind).
	Kind string `gorm:"column:kind;size:16;not null;default:''" json:"kind"`
	// RestoredFrom is the version a restore copied this state from; 0 otherwise.
	RestoredFrom int64 `gorm:"column:restored_from;not null;default:0" json:"restored_from"`
	// CreatedBy is the authenticated user that produced the version.
	CreatedBy string `gorm:"column:created_by;size:32;not null;default:''" json:"created_by"`
	// ConfigHash names the configuration snapshot this version ran under, as a
	// row of dataset_config_snapshot. It is a content hash: a state-recording
	// version writes a snapshot and points at it, a content change points at the
	// head's hash. NULL means "not recorded", never "no configuration". The
	// (dataset_id, config_hash) index serves the sweep of unreferenced snapshots.
	ConfigHash *string `gorm:"column:config_hash;size:32;index:idx_dataset_version_config,priority:2" json:"config_hash,omitempty"`
	// Config is the resolved snapshot, not a column: readers get it filled in
	// from dataset_config_snapshot by DatasetVersionDAO.resolveConfig.
	Config JSONMap `gorm:"-" json:"config,omitempty"`
	// Documents is the resolved document set: a JSON array of document ids, not
	// a column, filled in by DatasetVersionDAO.resolveDocuments from
	// DocumentsHash. Empty means "not recorded", never "no documents", and a
	// restore of such a version is refused. Only the kind that establishes the
	// set writes one; the others carry the head's hash, so the history stays
	// O(documents) instead of O(documents) per chunk touch. Ids only, no names
	// and no count: the page resolves those at read time.
	Documents string `gorm:"-" json:"-"`
	// DocumentsHash names the document set this version describes, as a row of
	// dataset_document_set_snapshot. A content hash like ConfigHash, so a dropped
	// version takes only its pointer with it. NULL means "not recorded". The
	// (dataset_id, documents_hash) index serves the sweep of unreferenced sets.
	DocumentsHash *string `gorm:"column:documents_hash;size:32;index:idx_dataset_version_documents,priority:2" json:"documents_hash,omitempty"`
	// Changes is the readable half of a content version: the documents and
	// chunks it touched, in the order the page renders them. It lives beside the
	// version because it is written, deleted and read with it; the writer bounds
	// it.
	Changes DatasetVersionChanges `gorm:"column:changes;type:longtext" json:"changes,omitempty"`
	// ChangesTotal is how many entries the change set held before the writer cut
	// it at the storage limit. Once the stored set is shorter it is a sample, and
	// the reader says so.
	ChangesTotal int `gorm:"column:changes_total;not null;default:0" json:"changes_total"`
	// CreateTime is the version's moment in milliseconds, stamped from the
	// database's clock: it is also the anchor a content restore replays the chunk
	// history to. No index: the anchor is read one row at a time by
	// (dataset_id, version). BeforeCreate always stamps it, so the column is
	// never NULL; the default only covers a write that bypasses the model hook.
	CreateTime *int64 `gorm:"column:create_time;not null;default:0" json:"create_time,omitempty"`
}

// BeforeCreate stamps the moment the version was appended, from the database's
// clock (it is the anchor a content restore replays to). The local clock is the
// fallback for a dialect DBTimeMillis does not know.
func (v *DatasetVersion) BeforeCreate(tx *gorm.DB) error {
	if v == nil || v.CreateTime != nil {
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

// TableName specify table name
func (DatasetVersion) TableName() string {
	return "dataset_version"
}
