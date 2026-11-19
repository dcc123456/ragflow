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

// Package history keeps the halves of a version's state that the metadata
// database cannot hold, in the document engine beside the chunks they describe:
// a chunk version's body (ChunkStore) and a document version's metadata values
// (MetadataStore).
//
// The split is the same for both. The metadata database is the source of truth
// for what happened - which version, which document, when, by whom - and stays
// small; the engine holds what the state said, which is the large half. A row
// never carries that half, it names it, so history rows stay the size of a
// pointer.
//
// Neither store has a second copy, so a version whose state the engine lost
// cannot be rebuilt from anything: restoring it means restoring the engine's own
// durability, which is why every failure here is returned to the caller instead
// of logged and swallowed. The metadata database and the engine are not
// transactional together and do not need to be - a write that rolls back leaves
// its document behind, and because a document is named by the pair a retry
// overwrites that same document with the state the row finally records.
//
// Each store is installed once at startup by the engine that owns the
// connection; an engine that offers none keeps no history, which is reported at
// startup rather than discovered by a failed restore.
package history

import "sync"

var (
	mu            sync.RWMutex
	chunkStore    ChunkStore
	metadataStore MetadataStore
)

func currentChunkStore() ChunkStore {
	mu.RLock()
	defer mu.RUnlock()
	return chunkStore
}

func currentMetadataStore() MetadataStore {
	mu.RLock()
	defer mu.RUnlock()
	return metadataStore
}
