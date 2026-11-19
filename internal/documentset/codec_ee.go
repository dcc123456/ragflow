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

// Package documentset holds how a version's document set is stored: the JSON a
// row carries and the content hash that names it.
//
// It is a package of its own because two sides of the history write it and they
// cannot import each other: the service that records a version, and the startup
// backfill that moved the sets out of dataset_version (dao cannot import a
// service). Nothing here knows what a version is - it only turns a set into the
// text a row stores, and names that text.
package documentset

import (
	"crypto/sha256"
	"encoding/hex"
)

// Encode returns the value a row stores for a document set: the JSON itself.
//
// The set is stored as text, uncompressed. It is one entry per document the
// dataset holds and it is written only when the set moves - an upload, a delete,
// a restore - so the bytes a compressor would save are paid once per milestone,
// while readable storage keeps a hand-written SELECT meaningful and keeps no codec
// between the hash and the row.
func Encode(raw []byte) string {
	return string(raw)
}

// hashBytes is how much of the digest names a set: 16 bytes, i.e. 32 hex
// characters of a sha256. The rest of the digest would only widen a primary key
// and every log line that quotes it, and 128 bits already leaves a collision
// unreachable for any dataset.
const hashBytes = 16

// Hash names a document set by its content, as the first 128 bits of a SHA-256
// digest in lowercase hex.
//
// It runs over the text exactly as the row stores it, so "the row whose hash is H
// holds the set H describes" is true without a codec in between. The JSON is
// canonical (ids ordered, one shape per set), so the same set always hashes the
// same and an unchanged set costs no second row.
func Hash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:hashBytes])
}

// ShortHash trims a hash to the prefix that identifies it in a log message.
func ShortHash(contentHash string) string {
	const keep = 8
	if len(contentHash) <= keep {
		return contentHash
	}
	return contentHash[:keep]
}
