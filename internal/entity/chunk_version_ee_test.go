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
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

// TestChunkVersionChunkIDHoldsEveryEngineID pins the width of the chunk id
// column against the ids the document engine actually stores.
//
// The column was sized for a parsed chunk, whose id is a 64-bit hash written as
// 16 hex characters. The parser is not the only writer of a document's rows: a
// knowledge-compilation run puts the products it compiled into the same batch,
// and those are named by a SHA-256 digest - knowledge_compiler.common.StableRowID
// documents its ids as "exactly 64 hex characters" - or by a dataset-level
// structure id that prefixes "dataset_structure_" to one. A batch carrying one
// of them failed outright ("Data too long for column 'chunk_id'"), so the whole
// document recorded no history at all and a later restore could only report it
// as "no chunk history reaches this version".
//
// The test reads the width off the model instead of inserting a long id, because
// the unit tests run on SQLite, which accepts any length in a VARCHAR column and
// would therefore pass either way.
func TestChunkVersionChunkIDHoldsEveryEngineID(t *testing.T) {
	idShapes := []struct {
		name  string
		width int
	}{
		{"a parsed chunk (64-bit hash)", 16},
		{"a compiled product (SHA-256 hex)", 64},
		{"a dataset-level structure id", len("dataset_structure_") + 64},
	}

	parsed, err := schema.Parse(&ChunkVersion{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse the chunk version model: %v", err)
	}
	field := parsed.LookUpField("chunk_id")
	if field == nil {
		t.Fatal("chunk_version has no chunk_id column")
	}
	for _, shape := range idShapes {
		if field.Size < shape.width {
			t.Errorf("chunk_id is varchar(%d) but %s is %d characters: inserting it would fail and the document would lose its history",
				field.Size, shape.name, shape.width)
		}
	}
}
