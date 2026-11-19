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

// DatasetRemovedDocument keeps a removed document's row and file-manager linkage
// so a content version can put the document back. One row is one removal; the
// newest row of a document is the state a restore inserts.
type DatasetRemovedDocument struct {
	ID string `gorm:"column:id;primaryKey;size:32" json:"id"`
	// DatasetID scopes the row to the dataset whose version may restore it.
	DatasetID string `gorm:"column:dataset_id;size:32;not null;index:idx_removed_document_removal,priority:1;index:idx_removed_document_time,priority:1" json:"dataset_id"`
	// DocumentID is the removed document. A document removed twice has one row
	// per removal.
	DocumentID string `gorm:"column:document_id;size:32;not null;index:idx_removed_document_removal,priority:2" json:"document_id"`
	// Document is the document row as it stood just before the delete.
	Document JSONMap `gorm:"column:document;type:longtext" json:"document"`
	// Files holds the file rows and their file2document mappings. Their blobs
	// stay in storage until the row is removed.
	Files JSONMap `gorm:"column:files;type:longtext" json:"files"`
	// CreateTime is the millisecond of the removal and the ordering key of both
	// indexes. It is stamped by BeforeCreate.
	CreateTime *int64 `gorm:"column:create_time;not null;default:0;index:idx_removed_document_removal,priority:3;index:idx_removed_document_time,priority:2" json:"create_time,omitempty"`
}

// BeforeCreate stamps the moment of the removal from the database clock, with
// the local clock as the fallback.
func (r *DatasetRemovedDocument) BeforeCreate(tx *gorm.DB) error {
	if r == nil || r.CreateTime != nil {
		return nil
	}
	if timestamp, err := DBTimeMillis(tx); err == nil {
		r.CreateTime = &timestamp
		return nil
	}
	timestamp, _ := autoModelTime()
	r.CreateTime = &timestamp
	return nil
}

// TableName specify table name
func (DatasetRemovedDocument) TableName() string {
	return "dataset_removed_document"
}
