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

package task

import (
	"context"
	"fmt"
	"strings"

	componentpkg "ragflow/internal/ingestion/component"
	chunkpkg "ragflow/internal/service/chunk"
)

// init wires the single-chunk embedder the chunk service uses to rebuild an
// edited chunk's vector. The chunk service cannot import this package (it
// already imports internal/service, the embedding-model owner, and this package
// imports it too), so the dependency is inverted here — the same seam
// DefaultEmbedderResolver uses.
func init() {
	chunkpkg.SetChunkEmbedderEE(embedChunkForEdit)
}

// embedChunkForEdit resolves the knowledgebase's embedding model through the
// production embedder resolver and embeds one chunk with the tokenizer's own
// rules: table tags stripped, text truncated to the model's window, then merged
// with the document title weighted by filename_embd_weight. Going through the
// resolver (rather than building a model here) keeps the re-embedded vector
// identical to what a re-parse of the document would produce.
func embedChunkForEdit(ctx context.Context, req chunkpkg.ChunkEmbedRequest) (chunkpkg.ChunkEmbedding, error) {
	resolver := componentpkg.DefaultEmbedderResolver
	if resolver == nil {
		return chunkpkg.ChunkEmbedding{}, fmt.Errorf("chunk embedder: no embedder resolver configured")
	}
	embedder, embdID, err := resolver(ctx, req.TenantID, req.DatasetID)
	if err != nil {
		return chunkpkg.ChunkEmbedding{}, fmt.Errorf("chunk embedder: resolve model: %w", err)
	}
	if embedder == nil || strings.TrimSpace(embdID) == "" {
		// The knowledgebase has no embedding model: there is no vector to
		// rebuild and nothing to report as a failure.
		return chunkpkg.ChunkEmbedding{}, nil
	}
	field, vector, err := componentpkg.EmbedChunkText(ctx, embedder, req.Name, req.Text, req.Weight)
	if err != nil {
		return chunkpkg.ChunkEmbedding{}, err
	}
	return chunkpkg.ChunkEmbedding{Field: field, Vector: vector}, nil
}
