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

package datasetversion

import (
	"testing"

	"gorm.io/gorm"

	"ragflow/internal/entity"
)

// TestConfigHashIgnoresKeyOrder pins the identity a snapshot is stored under: two
// snapshots that describe the same configuration hash alike no matter how their
// maps were built, so an unchanged configuration is stored once.
func TestConfigHashIgnoresKeyOrder(t *testing.T) {
	first := entity.JSONMap{
		"name":     "Dataset",
		"language": "English",
		"parser_config": entity.JSONMap{
			"chunk_token_num": float64(128),
			"delimiter":       "\n",
		},
		"connectors": []interface{}{
			entity.JSONMap{"id": "connector-a", "auto_parse": "1"},
			entity.JSONMap{"id": "connector-b", "auto_parse": "0"},
		},
	}
	second := entity.JSONMap{
		"connectors": []interface{}{
			entity.JSONMap{"auto_parse": "1", "id": "connector-a"},
			entity.JSONMap{"auto_parse": "0", "id": "connector-b"},
		},
		"parser_config": entity.JSONMap{
			"delimiter":       "\n",
			"chunk_token_num": float64(128),
		},
		"language": "English",
		"name":     "Dataset",
	}

	firstHash, err := ConfigHash(first)
	if err != nil {
		t.Fatalf("hash the first snapshot: %v", err)
	}
	secondHash, err := ConfigHash(second)
	if err != nil {
		t.Fatalf("hash the second snapshot: %v", err)
	}
	if firstHash != secondHash {
		t.Fatalf("hashes differ (%s vs %s) for the same configuration", firstHash, secondHash)
	}
	if len(firstHash) != 2*hashBytes {
		t.Fatalf("hash = %q, want the %d hex characters a %d-byte digest renders as", firstHash, 2*hashBytes, hashBytes)
	}
}

// TestConfigHashKeepsValuesAndOrderApart stops the canonical form from collapsing
// states that differ: a changed value or a reordered binding list is a different
// configuration, and both must hash differently.
func TestConfigHashKeepsValuesAndOrderApart(t *testing.T) {
	base := entity.JSONMap{"name": "Dataset", "connectors": []interface{}{"a", "b"}}
	baseHash, err := ConfigHash(base)
	if err != nil {
		t.Fatalf("hash the base: %v", err)
	}
	renamed, err := ConfigHash(entity.JSONMap{"name": "Dataset ", "connectors": []interface{}{"a", "b"}})
	if err != nil {
		t.Fatalf("hash the renamed snapshot: %v", err)
	}
	if renamed == baseHash {
		t.Fatal("a trailing space in a value must not be normalized away")
	}
	reordered, err := ConfigHash(entity.JSONMap{"name": "Dataset", "connectors": []interface{}{"b", "a"}})
	if err != nil {
		t.Fatalf("hash the reordered snapshot: %v", err)
	}
	if reordered == baseHash {
		t.Fatal("the order of a recorded list is part of the configuration")
	}
}

// TestStoreConfigSnapshotWritesOnce pins the deduplication the whole indirection
// exists for: the same configuration recorded twice is one row.
func TestStoreConfigSnapshotWritesOnce(t *testing.T) {
	db := setupVersionTestDB(t)
	insertVersionTestDataset(t, db, "kb-1")

	snapshot := entity.JSONMap{"name": "Dataset", "language": "English"}
	var first, second string
	if err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		first, err = StoreConfigSnapshot(t.Context(), tx, "kb-1", snapshot)
		return err
	}); err != nil {
		t.Fatalf("store the snapshot: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		second, err = StoreConfigSnapshot(t.Context(), tx, "kb-1", entity.JSONMap{"language": "English", "name": "Dataset"})
		return err
	}); err != nil {
		t.Fatalf("store the same snapshot again: %v", err)
	}
	if first != second {
		t.Fatalf("hashes = %s and %s, want one identity for one configuration", first, second)
	}
	if stored := countConfigSnapshots(t, db, "kb-1"); stored != 1 {
		t.Fatalf("stored snapshots = %d, want 1", stored)
	}
}
