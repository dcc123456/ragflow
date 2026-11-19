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

package chunk

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"ragflow/internal/entity"
)

// defaultFilenameEmbdWeight mirrors the tokenizer component's default for
// filename_embd_weight: a knowledgebase that never set the weight embeds its
// chunks with this much of the document title.
const defaultFilenameEmbdWeight = 0.1

// chunkEmbedderEE computes one chunk embedding the way the ingestion tokenizer
// does. It is injected by the composition root that owns the embedding-model
// resolution (the ingestion task package), because that package already imports
// internal/service and the reverse import would close a cycle.
var chunkEmbedderEE func(context.Context, ChunkEmbedRequest) (ChunkEmbedding, error)

// SetChunkEmbedderEE installs the embedder used to re-embed edited chunks. A
// nil function disables re-embedding: edits are stored without touching their
// vectors.
func SetChunkEmbedderEE(fn func(context.Context, ChunkEmbedRequest) (ChunkEmbedding, error)) {
	chunkEmbedderEE = fn
}

// chunkEmbeddingForEditEE computes the vector an edit must write together with
// the body it derives from.
//
// It runs before either store is touched, so the vector rides in the same
// engine write as the text: a chunk never reaches the state "new body, old
// vector", and a provider failure refuses the edit instead of leaving the
// content saved with a vector that describes the text it used to hold.
//
// Only the edit path calls it. A restore - of one chunk or of a dataset version
// - never does: the vector it puts back is the one the version recorded, and a
// snapshot that carries none is restored as text rather than paying a model
// request per chunk. See replayedVector (replay_ee.go).
func (s *ChunkService) chunkEmbeddingForEditEE(ctx context.Context, kb *entity.Knowledgebase, doc *entity.Document, content string) (*ChunkEmbedding, error) {
	if chunkEmbedderEE == nil {
		// No embedder wired: this assembly does not embed edited chunks.
		return nil, nil
	}
	if kb == nil {
		return nil, fmt.Errorf("chunk embedding: dataset is not loaded")
	}
	name := ""
	if doc != nil && doc.Name != nil {
		name = *doc.Name
	}
	embedding, err := chunkEmbedderEE(ctx, ChunkEmbedRequest{
		TenantID:  kb.TenantID,
		DatasetID: kb.ID,
		Name:      name,
		Text:      content,
		Weight:    filenameEmbdWeight(kb.ParserConfig),
	})
	if err != nil {
		return nil, err
	}
	return &embedding, nil
}

// filenameEmbdWeight reads the title weight the tokenizer would use for a
// knowledgebase. Versioned parser configs keep it under a scoped
// "Tokenizer:<name>" node (one per parser); pre-migration configs keep the flat
// key. Absent means the tokenizer default.
func filenameEmbdWeight(parserConfig entity.JSONMap) float64 {
	if weight, ok := numericEmbedWeight(parserConfig["filename_embd_weight"]); ok {
		return weight
	}
	// A versioned config carries one Tokenizer node per parser, so the map can
	// hold several. Walk them in a stable order: ranging the map would let two
	// re-embeds of the same chunk pick different weights and produce vectors that
	// cannot be reproduced.
	scoped := make([]string, 0, len(parserConfig))
	for key := range parserConfig {
		if strings.HasPrefix(key, "Tokenizer:") {
			scoped = append(scoped, key)
		}
	}
	sort.Strings(scoped)
	for _, key := range scoped {
		node, ok := parserConfig[key].(map[string]interface{})
		if !ok {
			continue
		}
		if weight, ok := numericEmbedWeight(node["filename_embd_weight"]); ok {
			return weight
		}
	}
	return defaultFilenameEmbdWeight
}

// numericEmbedWeight accepts every shape the weight can arrive in: JSON
// numbers from the database, ints from a config written in Go, and strings from
// a config a client submitted as text.
func numericEmbedWeight(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		weight, err := v.Float64()
		return weight, err == nil
	case string:
		weight, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return weight, err == nil
	}
	return 0, false
}
