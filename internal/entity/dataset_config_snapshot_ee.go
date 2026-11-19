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

// DatasetConfigSnapshot is one configuration snapshot of a dataset, addressed by
// the hash of its canonical JSON. Versions reference it by hash, so a
// configuration shared by several versions is stored once.
type DatasetConfigSnapshot struct {
	DatasetID string `gorm:"column:dataset_id;size:32;not null;primaryKey" json:"dataset_id"`
	// ConfigHash is the first 128 bits of the sha256 of Config, as 32 lowercase
	// hex characters (see datasetversion.ConfigHash).
	ConfigHash string `gorm:"column:config_hash;size:32;not null;primaryKey" json:"config_hash"`
	// Config is the snapshot as the exact bytes the hash was computed from.
	Config JSONMap `gorm:"column:config;type:longtext;not null" json:"config"`
}

// TableName specify table name
func (DatasetConfigSnapshot) TableName() string {
	return "dataset_config_snapshot"
}
