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

// DatasetDocumentSetSnapshot is the set of document ids a version describes,
// addressed by the hash of its canonical JSON. Versions reference it by hash, so
// a set shared by several versions is stored once.
type DatasetDocumentSetSnapshot struct {
	DatasetID string `gorm:"column:dataset_id;size:32;not null;primaryKey" json:"dataset_id"`
	// DocumentsHash is the first 128 bits of the sha256 of Documents, as 32
	// lowercase hex characters (see documentset.Hash).
	DocumentsHash string `gorm:"column:documents_hash;size:32;not null;primaryKey" json:"documents_hash"`
	// Documents is the set as stored: a JSON array of document ids, ordered by
	// id and written as text.
	Documents string `gorm:"column:documents;type:longtext" json:"-"`
}

// TableName specify table name
func (DatasetDocumentSetSnapshot) TableName() string {
	return "dataset_document_set_snapshot"
}
