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
	"testing"

	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/service"
)

// TestChunkVersionOrigin pins which side a history row belongs to: a change a
// user made on the chunk page, one the ingestion pipeline wrote, or one the
// replay of a dataset restore produced. The two are indistinguishable from the
// row's kind and actor alone when a removal is involved - both are actorless
// "delete" rows - which is why the dataset version the row was written under
// decides that case.
func TestChunkVersionOrigin(t *testing.T) {
	const (
		user    = "user-1"
		system  = entity.ChunkVersionSystemActor
		parse   = entity.ChunkVersionKindParse
		add     = entity.ChunkVersionKindAdd
		edit    = entity.ChunkVersionKindEdit
		remove  = entity.ChunkVersionKindDelete
		restore = entity.ChunkVersionKindRestore
	)
	cases := []struct {
		name               string
		kind               string
		createdBy          string
		datasetVersionKind string
		want               string
	}{
		{"parse baseline", parse, system, "", service.ChunkVersionOriginParse},
		{"chunk added by hand", add, system, "", service.ChunkVersionOriginChunk},
		{"chunk edited", edit, user, "", service.ChunkVersionOriginChunk},
		{"enable switch", edit, user, entity.DatasetVersionKindChunkEnable, service.ChunkVersionOriginChunk},
		{"chunk removed by hand", remove, user, "", service.ChunkVersionOriginChunk},
		{"chunk restored from its own history", restore, user, "", service.ChunkVersionOriginChunk},
		{"chunk restored by a dataset restore", restore, system, "", service.ChunkVersionOriginRestore},
		{"chunk dropped by a dataset restore", remove, system, entity.DatasetVersionKindRestore, service.ChunkVersionOriginRestore},
		{"chunk dropped with no actor at all", remove, "", entity.DatasetVersionKindRestore, service.ChunkVersionOriginRestore},
		{"chunk dropped by a re-parse", remove, system, entity.DatasetVersionKindParse, service.ChunkVersionOriginParse},
		{"removal whose dataset version is gone", remove, system, "", ""},
		{"unknown kind", "unknown", user, "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := chunkVersionOrigin(tc.kind, tc.createdBy, tc.datasetVersionKind); got != tc.want {
				t.Fatalf("chunkVersionOrigin(%q, %q, %q) = %q, want %q",
					tc.kind, tc.createdBy, tc.datasetVersionKind, got, tc.want)
			}
		})
	}
}

// TestChunkVersionTrigger pins who a row credits. The point is the system-written
// rows: a parse baseline, the tombstone a re-parse left and the rows a dataset
// restore replayed all name nobody themselves, so the history credits the user who
// triggered the dataset version they were anchored at - the one who asked for the
// parse or the restore.
func TestChunkVersionTrigger(t *testing.T) {
	row := func(kind, createdBy string) *entity.ChunkVersion {
		return &entity.ChunkVersion{Kind: kind, CreatedBy: createdBy}
	}
	cases := []struct {
		name string
		row  *entity.ChunkVersion
		fact dao.DatasetVersionFact
		want string
	}{
		{"an edit credits its author", row(entity.ChunkVersionKindEdit, "user-1"), dao.DatasetVersionFact{}, "user-1"},
		{"a parse baseline credits whoever ran the parse",
			row(entity.ChunkVersionKindParse, entity.ChunkVersionSystemActor),
			dao.DatasetVersionFact{Kind: entity.DatasetVersionKindParse, CreatedBy: "user-2"}, "user-2"},
		{"a replay credits whoever ran the restore",
			row(entity.ChunkVersionKindRestore, entity.ChunkVersionSystemActor),
			dao.DatasetVersionFact{Kind: entity.DatasetVersionKindRestore, CreatedBy: "user-3"}, "user-3"},
		{"a removal a re-parse left credits whoever ran the parse",
			row(entity.ChunkVersionKindDelete, ""),
			dao.DatasetVersionFact{Kind: entity.DatasetVersionKindParse, CreatedBy: " user-4 "}, "user-4"},
		{"a system row whose dataset version is gone credits nobody",
			row(entity.ChunkVersionKindParse, entity.ChunkVersionSystemActor), dao.DatasetVersionFact{}, ""},
		{"a missing row credits nobody", nil, dao.DatasetVersionFact{CreatedBy: "user-5"}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := chunkVersionTrigger(tc.row, tc.fact); got != tc.want {
				t.Fatalf("chunkVersionTrigger = %q, want %q", got, tc.want)
			}
		})
	}
}
