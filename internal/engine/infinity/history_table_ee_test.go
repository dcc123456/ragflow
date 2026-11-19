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

package infinity

import "testing"

// TestChunkHistoryTableNameExtendsTheChunkTable pins the history table to the
// chunk table it describes: the same name with "_history" appended, so the
// history is found (and dropped) through the table its chunks live in.
func TestChunkHistoryTableNameExtendsTheChunkTable(t *testing.T) {
	chunkTable := buildChunkTableName(chunkTableBaseName("tenant-1"), "ds-1")
	if chunkTable != "ragflow_tenant-1_ds-1" {
		t.Fatalf("chunk table = %q, want ragflow_tenant-1_ds-1", chunkTable)
	}
	if got, want := ChunkHistoryTableName("tenant-1", "ds-1"), chunkTable+"_history"; got != want {
		t.Fatalf("chunk history table = %q, want %q", got, want)
	}
}

// TestChunkHistoryTableNameFollowsSkillTables pins the one dataset id that names
// no suffix: a skill table is the base name alone, so its history is the base
// name plus "_history".
func TestChunkHistoryTableNameFollowsSkillTables(t *testing.T) {
	if got, want := ChunkHistoryTableName("tenant-1", "skill"), "ragflow_tenant-1_history"; got != want {
		t.Fatalf("skill history table = %q, want %q", got, want)
	}
}

// TestChunkHistoryTableRefusesMissingTenant pins that a request with no tenant is
// refused instead of writing into a table no chunk table belongs to.
func TestChunkHistoryTableRefusesMissingTenant(t *testing.T) {
	if _, err := chunkHistoryTable("", "ds-1"); err == nil {
		t.Fatal("chunkHistoryTable(no tenant) = nil error, want a refusal naming the tenant")
	}
}

// TestMetadataHistoryTableNameExtendsTheDocMetaTable pins the metadata history to
// the doc_meta table it describes, the way the chunk history extends its chunk
// table.
func TestMetadataHistoryTableNameExtendsTheDocMetaTable(t *testing.T) {
	docMeta := buildMetadataTableName("tenant-1")
	if docMeta != "ragflow_doc_meta_tenant-1" {
		t.Fatalf("doc_meta table = %q, want ragflow_doc_meta_tenant-1", docMeta)
	}
	if got, want := MetadataHistoryTableName("tenant-1"), docMeta+"_history"; got != want {
		t.Fatalf("metadata history table = %q, want %q", got, want)
	}
}

// lengthPrefixed frames values as Infinity's SDK returns a multi-row VARCHAR.
func lengthPrefixed(values ...string) string {
	var framed []byte
	for _, value := range values {
		length := len(value)
		framed = append(framed,
			byte(length), byte(length>>8), byte(length>>16), byte(length>>24))
		framed = append(framed, value...)
	}
	return string(framed)
}

// TestRealignLengthPrefixedColumnsSplitsBothColumns: both framed columns must
// be split, or every body is reported missing.
func TestRealignLengthPrefixedColumnsSplitsBothColumns(t *testing.T) {
	ids, snapshots := realignLengthPrefixedColumns(
		[]string{lengthPrefixed("chunk-1:0", "chunk-1:1", "chunk-2:0")},
		[]string{lengthPrefixed(`{"a":1}`, `{"a":2}`, `{"b":1}`)},
	)
	wantIDs := []string{"chunk-1:0", "chunk-1:1", "chunk-2:0"}
	wantSnapshots := []string{`{"a":1}`, `{"a":2}`, `{"b":1}`}
	if len(ids) != len(wantIDs) || len(snapshots) != len(wantSnapshots) {
		t.Fatalf("realigned = %v / %v, want %v / %v", ids, snapshots, wantIDs, wantSnapshots)
	}
	for i := range wantIDs {
		if ids[i] != wantIDs[i] || snapshots[i] != wantSnapshots[i] {
			t.Fatalf("row %d = %q / %q, want %q / %q", i, ids[i], snapshots[i], wantIDs[i], wantSnapshots[i])
		}
	}
}

// TestRealignLengthPrefixedColumnsLeavesAlignedColumnsAlone: aligned or
// mismatched frames are left as they are.
func TestRealignLengthPrefixedColumnsLeavesAlignedColumnsAlone(t *testing.T) {
	ids, snapshots := realignLengthPrefixedColumns([]string{"chunk-1:0"}, []string{`{"a":1}`})
	if len(ids) != 1 || ids[0] != "chunk-1:0" || len(snapshots) != 1 || snapshots[0] != `{"a":1}` {
		t.Fatalf("aligned = %v / %v, want them untouched", ids, snapshots)
	}

	ids, snapshots = realignLengthPrefixedColumns(
		[]string{lengthPrefixed("chunk-1:0", "chunk-1:1")},
		[]string{lengthPrefixed(`{"a":1}`)},
	)
	if len(ids) != 1 || len(snapshots) != 1 {
		t.Fatalf("mismatched frames = %v / %v, want them untouched", ids, snapshots)
	}
}
