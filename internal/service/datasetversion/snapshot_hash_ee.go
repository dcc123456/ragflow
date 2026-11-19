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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"ragflow/internal/dao"
	"ragflow/internal/entity"
)

// canonicalJSON renders a snapshot as the bytes that identify it.
//
// encoding/json sorts every map's keys and keeps slices in their own order, so two
// snapshots that describe the same configuration encode identically without a
// bespoke normalizer: the shape is one fixed set of keys filled from the dataset
// row, plus parser_config and the connector bindings (which ConnectorBindings
// already orders by connector id). The bytes and the hash therefore cannot
// disagree, and a stored snapshot is re-read exactly as it was hashed.
//
// The rule this function must never break is that it only re-encodes: it does not
// trim strings, coerce numbers or drop keys. A canonical form that changes values
// would make two different configurations hash alike - the one failure mode of
// content addressing that silently shows a reader another state's settings.
func canonicalJSON(snapshot entity.JSONMap) ([]byte, error) {
	if snapshot == nil {
		return nil, errors.New("dataset configuration snapshot is nil")
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("failed to encode the dataset configuration snapshot: %w", err)
	}
	return encoded, nil
}

// hashBytes is how much of the digest names a snapshot: 16 bytes, i.e. 32 hex
// characters.
//
// A full sha256 is 32 bytes, and hex doubles that; 128 bits is already far past
// what a dataset can collide with (the birthday bound for a billion snapshots is
// about 1e-21), so the wider name buys nothing and costs half its width in every
// primary key, index and log line that carries it. The value is a number rather
// than a literal so the width lives in one place per store.
const hashBytes = 16

// ConfigHash names a snapshot by the first 128 bits of the sha256 of its
// canonical JSON, as lowercase hex.
//
// The hash is the snapshot's identity: two versions that ran under the same
// configuration share one row, and an unchanged configuration costs nothing to
// record again.
func ConfigHash(snapshot entity.JSONMap) (string, error) {
	encoded, err := canonicalJSON(snapshot)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:hashBytes]), nil
}

// StoreConfigSnapshot records a snapshot in the dataset's configuration table and
// returns the hash that names it, for the version row to point at.
//
// It runs inside the caller's transaction, before the version row is written, so a
// committed version can never name a snapshot that was rolled back with an earlier
// statement. Writing the same snapshot twice is the same write: the hash is the
// primary key's second half, and the insert does nothing when it is already there.
func StoreConfigSnapshot(ctx context.Context, tx *gorm.DB, datasetID string, snapshot entity.JSONMap) (string, error) {
	hash, err := ConfigHash(snapshot)
	if err != nil {
		return "", err
	}
	if err := dao.NewDatasetConfigSnapshotDAO().Upsert(ctx, tx, datasetID, hash, snapshot); err != nil {
		return "", fmt.Errorf("failed to store the dataset configuration snapshot: %w", err)
	}
	return hash, nil
}
