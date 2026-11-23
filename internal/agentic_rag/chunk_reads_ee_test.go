//go:build ee

package agentic_rag

import (
	"ragflow/internal/retrievalaccount"
	"reflect"
	"testing"
)

func TestCountChunkElements(t *testing.T) {
	cases := map[string]int{
		// list_chunks deep read: wrapper + two chunks
		`<chunks doc_id="d1" fetched="2"><chunk chunk_id="c1" doc_id="d1">x</chunk><chunk chunk_id="c2" doc_id="d1">y</chunk></chunks>`: 2,
		// search_bm25 triage view
		`<search_results count="2" query="q"><chunk rank="1" chunk_id="c1" doc_id="d1"><match_snippet>s</match_snippet></chunk><chunk rank="2" chunk_id="c2" doc_id="d2"><match_snippet>t</match_snippet></chunk></search_results>`: 2,
		// empty result set: only the wrapper
		`<chunks doc_id="d1" fetched="0"></chunks>`:             0,
		`<search_results count="0" query="q"></search_results>`: 0,
		``: 0,
		`no chunks here, just <other tag="x"> text`: 0,
	}
	for out, want := range cases {
		if got := retrievalaccount.CountChunkElements(out); got != want {
			t.Fatalf("retrievalaccount.CountChunkElements(%q) = %d, want %d", out, got, want)
		}
	}
}

func TestChunkReadLedger(t *testing.T) {
	var nilLedger *chunkReadLedger
	nilLedger.AddDeep(5) // nil-safe
	nilLedger.AddShallow(5)
	deep, shallow := nilLedger.Snapshot()
	if deep != 0 || shallow != 0 {
		t.Fatalf("nil ledger snapshot = (%d, %d)", deep, shallow)
	}

	l := NewChunkReadLedger()
	l.AddDeep(2)
	l.AddDeep(3)
	l.AddShallow(7)
	l.AddShallow(0) // ignored
	l.AddDeep(-1)   // ignored
	deep, shallow = l.Snapshot()
	if deep != 5 || shallow != 7 {
		t.Fatalf("ledger = (deep=%d, shallow=%d), want (5, 7)", deep, shallow)
	}
}

func TestRecordedChunkIDs(t *testing.T) {
	cases := map[string][]string{
		// list_chunks deep read: the <chunks> wrapper carries doc_id only, so
		// the chunk_id attributes are exactly the elements counted.
		`<chunks doc_id="d1" fetched="2"><chunk chunk_id="c1" doc_id="d1">x</chunk><chunk chunk_id="c2" doc_id="d1">y</chunk></chunks>`: {"c1", "c2"},
		// search_bm25 triage view: same attribute, shallow read.
		`<search_results count="1" query="q"><chunk rank="1" chunk_id="c1" doc_id="d1"><match_snippet>s</match_snippet></chunk></search_results>`: {"c1"},
		// empty result set: only the wrapper.
		`<chunks doc_id="d1" fetched="0"></chunks>`:             nil,
		`<search_results count="0" query="q"></search_results>`: nil,
		``: nil,
		`no chunks here, just <other tag="x"> text`:                    nil,
		`<chunk chunk_id="  " doc_id="d1">blank id is dropped</chunk>`: nil,
	}
	for out, want := range cases {
		got := retrievalaccount.RecordedChunkIDs(out)
		if len(got) != len(want) {
			t.Fatalf("retrievalaccount.RecordedChunkIDs(%q) = %v, want %v", out, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("retrievalaccount.RecordedChunkIDs(%q) = %v, want %v", out, got, want)
			}
		}
	}
}

func TestChunkReadLedgerRecordsIDs(t *testing.T) {
	var nilLedger *chunkReadLedger
	nilLedger.AddDeepIDs("c1") // nil-safe
	nilLedger.AddShallowIDs("c2")
	if deep, shallow := nilLedger.ChunkIDs(); len(deep) != 0 || len(shallow) != 0 {
		t.Fatalf("nil ledger ids = (%v, %v)", deep, shallow)
	}

	// A bare struct still records: the id sets are created on first use, so a
	// ledger built without the constructor cannot silently drop ids.
	bare := &chunkReadLedger{}
	bare.AddDeepIDs("c9")
	if deep, _ := bare.ChunkIDs(); !reflect.DeepEqual(deep, []string{"c9"}) {
		t.Fatalf("bare ledger ids = %v, want [c9]", deep)
	}

	l := NewChunkReadLedger()
	l.AddDeepIDs("c2", "c1", "c1", "") // repeats collapse, empties dropped
	l.AddDeepIDs("c3")
	l.AddShallowIDs("s1")
	l.AddShallowIDs("s1", "s2")
	deep, shallow := l.ChunkIDs()
	if want := []string{"c1", "c2", "c3"}; !reflect.DeepEqual(deep, want) {
		t.Fatalf("deep ids = %v, want %v", deep, want)
	}
	if want := []string{"s1", "s2"}; !reflect.DeepEqual(shallow, want) {
		t.Fatalf("shallow ids = %v, want %v", shallow, want)
	}
	// Ids and counts are independent accounts of the same reads: recording ids
	// must not move the totals a benchmark compares across runs.
	if deepN, shallowN := l.Snapshot(); deepN != 0 || shallowN != 0 {
		t.Fatalf("ids must not move the counts: (%d, %d)", deepN, shallowN)
	}

	// Two ledgers must not share state through the constructor's maps.
	if otherDeep, otherShallow := NewChunkReadLedger().ChunkIDs(); len(otherDeep) != 0 || len(otherShallow) != 0 {
		t.Fatalf("fresh ledger is not empty: (%v, %v)", otherDeep, otherShallow)
	}
}
