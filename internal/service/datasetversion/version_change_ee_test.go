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
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"ragflow/internal/entity"
)

// TestNormalizeChangesKeepsTargetsAndDropsRepeats pins what a stored change set
// is allowed to contain: entries without a target name nothing and are dropped,
// and the same chunk touched twice in one request is one entry.
func TestNormalizeChangesKeepsTargetsAndDropsRepeats(t *testing.T) {
	changes, _ := normalizeChanges([]entity.DatasetVersionChange{
		{Entity: entity.DatasetVersionChangeEntityChunk, Action: entity.DatasetVersionChangeActionUpdate, DocID: "doc-1", ChunkID: "chunk-1"},
		{Entity: entity.DatasetVersionChangeEntityChunk, Action: entity.DatasetVersionChangeActionUpdate, DocID: "doc-1", ChunkID: "chunk-1"},
		{Entity: entity.DatasetVersionChangeEntityChunk, Action: entity.DatasetVersionChangeActionUpdate, DocID: "doc-1"},
		{Entity: "", Action: entity.DatasetVersionChangeActionUpdate, ChunkID: "chunk-2"},
	})

	if len(changes) != 1 {
		t.Fatalf("changes = %d, want 1 (one real entry)", len(changes))
	}
	if changes[0].ChunkID != "chunk-1" || changes[0].DocID != "doc-1" {
		t.Fatalf("entry = %#v, want the chunk with its document", changes[0])
	}
}

// TestNormalizeChangesDocumentCarriesItsOwnID keeps the document entry readable:
// the versions page groups chunks by doc_id, and a document entry names itself.
func TestNormalizeChangesDocumentCarriesItsOwnID(t *testing.T) {
	changes, _ := normalizeChanges([]entity.DatasetVersionChange{
		{Entity: entity.DatasetVersionChangeEntityDocument, Action: entity.DatasetVersionChangeActionAdd, DocID: "doc-9", Name: "manual.pdf"},
	})

	if len(changes) != 1 {
		t.Fatalf("changes = %d, want 1", len(changes))
	}
	if changes[0].DocID != "doc-9" {
		t.Fatalf("doc_id = %q, want the target id", changes[0].DocID)
	}
	if changes[0].Name != "manual.pdf" {
		t.Fatalf("name = %q, want the stored one", changes[0].Name)
	}
}

// TestNormalizeChangesCapsTheSet keeps one parse from filling the row: the
// version's counters still describe the whole change.
func TestNormalizeChangesCapsTheSet(t *testing.T) {
	changes := make([]entity.DatasetVersionChange, 0, maxVersionChanges+25)
	for i := 0; i < maxVersionChanges+25; i++ {
		changes = append(changes, entity.DatasetVersionChange{
			Entity:  entity.DatasetVersionChangeEntityChunk,
			Action:  entity.DatasetVersionChangeActionParse,
			DocID:   "doc-1",
			ChunkID: fmt.Sprintf("chunk-%d", i),
		})
	}

	normalized, total := normalizeChanges(changes)
	if len(normalized) != maxVersionChanges {
		t.Fatalf("changes = %d, want the cap %d", len(normalized), maxVersionChanges)
	}
	// The total survives the cut: a reader can tell a sample from a whole change.
	if total != maxVersionChanges+25 {
		t.Fatalf("total = %d, want %d", total, maxVersionChanges+25)
	}
}

// TestNormalizeChangesPutsDocumentsFirst keeps the stored order equal to the
// rendered one: a reader wants the files a version touched before the chunks
// inside them, and nothing re-sorts the set on the way out.
func TestNormalizeChangesPutsDocumentsFirst(t *testing.T) {
	changes, _ := normalizeChanges([]entity.DatasetVersionChange{
		{Entity: entity.DatasetVersionChangeEntityChunk, Action: entity.DatasetVersionChangeActionUpdate, DocID: "doc-1", ChunkID: "chunk-1"},
		{Entity: entity.DatasetVersionChangeEntityChunk, Action: entity.DatasetVersionChangeActionUpdate, DocID: "doc-1", ChunkID: "chunk-2"},
		{Entity: entity.DatasetVersionChangeEntityDocument, Action: entity.DatasetVersionChangeActionAdd, DocID: "doc-1"},
	})

	if len(changes) != 3 {
		t.Fatalf("changes = %d, want 3", len(changes))
	}
	if changes[0].Entity != entity.DatasetVersionChangeEntityDocument {
		t.Fatalf("first entry = %#v, want the document", changes[0])
	}
	// The chunks keep the order they were handed in: the sort is stable.
	if changes[1].ChunkID != "chunk-1" || changes[2].ChunkID != "chunk-2" {
		t.Fatalf("chunks = %q, %q, want them in the given order", changes[1].ChunkID, changes[2].ChunkID)
	}
}

// TestDatasetVersionChangesRoundTrip covers the column's encoding: the set
// travels as JSON, an empty set costs no bytes, and decoding replaces whatever
// the previous row left in the slice.
func TestDatasetVersionChangesRoundTrip(t *testing.T) {
	stored := entity.DatasetVersionChanges{
		{Entity: entity.DatasetVersionChangeEntityDocument, Action: entity.DatasetVersionChangeActionAdd, DocID: "doc-1", Name: "manual.pdf"},
		{Entity: entity.DatasetVersionChangeEntityChunk, Action: entity.DatasetVersionChangeActionUpdate, DocID: "doc-1", ChunkID: "chunk-1"},
	}
	value, err := stored.Value()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	encoded, ok := value.([]byte)
	if !ok {
		t.Fatalf("encoded type = %T, want []byte", value)
	}
	if !strings.Contains(string(encoded), "manual.pdf") {
		t.Fatalf("encoded = %s, want the entry names in it", encoded)
	}
	// Each kind names itself in the field its entity uses: no shared target
	// column, and no second id on the document entry that already has doc_id.
	if strings.Contains(string(encoded), "target_id") {
		t.Fatalf("encoded = %s, want no target_id column", encoded)
	}
	if !strings.Contains(string(encoded), `"chunk_id":"chunk-1"`) {
		t.Fatalf("encoded = %s, want the chunk id on the chunk entry", encoded)
	}
	if count := strings.Count(string(encoded), `"chunk_id"`); count != 1 {
		t.Fatalf("encoded = %s, want one chunk id, the chunk entry's", encoded)
	}

	var decoded entity.DatasetVersionChanges
	if err := decoded.Scan(encoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded) != 2 || decoded[1].ChunkID != "chunk-1" || decoded[1].Name != "" {
		t.Fatalf("decoded = %#v, want the entries back, the chunk with no text", decoded)
	}
	if decoded[0].DocID != "doc-1" || decoded[0].ChunkID != "" {
		t.Fatalf("document entry = %#v, want the document id only", decoded[0])
	}

	// A second decode into the same value must not keep entries from the first.
	if err := decoded.Scan([]byte(`[{"entity":"document","action":"delete","doc_id":"doc-9"}]`)); err != nil {
		t.Fatalf("decode again: %v", err)
	}
	if len(decoded) != 1 || decoded[0].DocID != "doc-9" {
		t.Fatalf("decoded = %#v, want only the new entry", decoded)
	}

	if err := decoded.Scan(nil); err != nil {
		t.Fatalf("decode NULL: %v", err)
	}
	if decoded != nil {
		t.Fatalf("decoded NULL = %#v, want nil", decoded)
	}

	empty, err := entity.DatasetVersionChanges{}.Value()
	if err != nil || empty != nil {
		t.Fatalf("empty set encoded as (%v, %v), want NULL", empty, err)
	}
}

// TestTruncateNameCountsCharacters cuts on a rune boundary and counts runes: the
// display holds characters, so a byte-based cut silently halved a Chinese name.
func TestTruncateNameCountsCharacters(t *testing.T) {
	// A name well inside the limit keeps every character: 600 Chinese characters
	// are 1800 bytes, which a byte-based cap would have cut.
	survives := strings.Repeat("解", 600)
	if got := truncateName(survives); got != survives {
		t.Fatalf("600-character name was cut to %d runes", len([]rune(got)))
	}

	name := strings.Repeat("解", changeNameLimit+10)
	truncated := truncateName(name)
	if count := len([]rune(truncated)); count != changeNameLimit {
		t.Fatalf("truncated runes = %d, want %d", count, changeNameLimit)
	}
	if !utf8.ValidString(truncated) {
		t.Fatal("truncated name is not valid UTF-8")
	}
	if !strings.HasPrefix(name, truncated) {
		t.Fatal("truncated name is not a prefix of the original")
	}
	if got := truncateName("short"); got != "short" {
		t.Fatalf("short name = %q, want it untouched", got)
	}
}

// TestNormalizeChangesKeepsTheMetadataDiff pins the one part of an entry that
// the generic copy would drop: normalizeChanges rebuilds every entry field by
// field, so a diff it does not copy would vanish without a trace - the version
// would still be written and read as "the metadata changed, somehow".
func TestNormalizeChangesKeepsTheMetadataDiff(t *testing.T) {
	changes, _ := normalizeChanges([]entity.DatasetVersionChange{{
		Entity: entity.DatasetVersionChangeEntityDocument,
		Action: entity.DatasetVersionChangeActionUpdate,
		DocID:  "doc-1",
		Name:   "report.txt",
		MetaChanges: []entity.MetadataFieldChange{
			{Key: "author", Before: json.RawMessage(`"alice"`), After: json.RawMessage(`"bob"`)},
			{Key: "tags", After: json.RawMessage(`["a","b"]`)},
		},
	}})

	if len(changes) != 1 {
		t.Fatalf("changes = %d, want 1", len(changes))
	}
	diff := changes[0].MetaChanges
	if len(diff) != 2 {
		t.Fatalf("meta changes = %+v, want both keys", diff)
	}
	if diff[0].Key != "author" || string(diff[0].Before) != `"alice"` || string(diff[0].After) != `"bob"` {
		t.Fatalf("first change = %+v, want author alice -> bob", diff[0])
	}
	if diff[1].Key != "tags" || diff[1].Before != nil || string(diff[1].After) != `["a","b"]` {
		t.Fatalf("second change = %+v, want an introduced list", diff[1])
	}
}

// TestNormalizeChangesKeepsThePageCommit pins the other field the generic copy
// drops by rebuild: an artifact entry names the commit that explains it, and
// without that id the panel cannot open the exact edit - the page may have moved
// on since.
func TestNormalizeChangesKeepsThePageCommit(t *testing.T) {
	changes, _ := normalizeChanges([]entity.DatasetVersionChange{{
		Entity:   entity.DatasetVersionChangeEntityArtifact,
		Action:   entity.DatasetVersionChangeActionUpdate,
		DocID:    "kb-1/wiki/人物",
		Name:     "人物",
		CommitID: "commit-1",
	}})

	if len(changes) != 1 {
		t.Fatalf("changes = %d, want 1", len(changes))
	}
	if got := changes[0].CommitID; got != "commit-1" {
		t.Fatalf("commit id = %q, want it kept whole", got)
	}
}

// TestNormalizeChangesBoundsTheMetadataDiff keeps a wide metadata document from
// becoming the largest thing in the row: the keys stop at their limit, and a
// value longer than the display limit is cut and marked instead of silently
// shortened.
func TestNormalizeChangesBoundsTheMetadataDiff(t *testing.T) {
	diff := make([]entity.MetadataFieldChange, 0, maxMetadataChanges+5)
	for i := 0; i < maxMetadataChanges+5; i++ {
		diff = append(diff, entity.MetadataFieldChange{
			Key:   fmt.Sprintf("key-%d", i),
			After: json.RawMessage(`"value"`),
		})
	}
	long, err := json.Marshal(strings.Repeat("x", metadataValueLimit+50))
	if err != nil {
		t.Fatal(err)
	}
	diff = append(diff, entity.MetadataFieldChange{Key: "note", After: long})

	changes, _ := normalizeChanges([]entity.DatasetVersionChange{{
		Entity:      entity.DatasetVersionChangeEntityDocument,
		Action:      entity.DatasetVersionChangeActionUpdate,
		DocID:       "doc-1",
		MetaChanges: diff,
	}})
	if len(changes) != 1 {
		t.Fatalf("changes = %d, want 1", len(changes))
	}
	if got := len(changes[0].MetaChanges); got != maxMetadataChanges {
		t.Fatalf("meta changes = %d, want the cap %d", got, maxMetadataChanges)
	}

	// A value over the limit is cut and marked, so the reader can tell a short
	// value from a shortened one.
	bounded := boundMetadataChanges([]entity.MetadataFieldChange{{Key: "note", After: long}})
	if len(bounded) != 1 {
		t.Fatalf("bounded = %+v, want the one key", bounded)
	}
	clipped := string(bounded[0].After)
	if !strings.HasSuffix(clipped, `…"`) {
		t.Fatalf("clipped value = %q, want it marked as cut", clipped)
	}
	if len(clipped) >= len(long) {
		t.Fatalf("clipped value is not shorter than the original (%d >= %d)", len(clipped), len(long))
	}
	var decoded string
	if err := json.Unmarshal(bounded[0].After, &decoded); err != nil {
		t.Fatalf("clipped value is not valid JSON: %v", err)
	}
	if !utf8.ValidString(decoded) {
		t.Fatal("clipped value is not valid UTF-8")
	}
	if got := boundMetadataChanges(nil); got != nil {
		t.Fatalf("empty diff = %+v, want nothing", got)
	}
}
