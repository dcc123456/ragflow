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

package agentic_rag

import (
	"context"
	"errors"
	"ragflow/internal/retrievalaccount"
	"reflect"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// errStubTool is what stubTool returns when it is asked to fail.
var errStubTool = errors.New("stub tool failed")

// stubTool is an InvokableTool that echoes a canned payload, so the wrapper can
// be exercised without a retrieval backend.
type stubTool struct {
	out  string
	err  error
	name string
}

func (s *stubTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	name := s.name
	if name == "" {
		name = "stub_tool"
	}
	return &schema.ToolInfo{Name: name, Desc: "canned output"}, nil
}

func (s *stubTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	return s.out, s.err
}

// TestRecordedDocIDs pins the contract the retrieval ledger relies on: locate
// tools render doc_id/doc_name as XML attributes, list_chunks renders doc_id on
// its root element, and a web_search result renders neither (a web hit is not a
// corpus document, so it must never inflate retrieval recall).
func TestRecordedDocIDs(t *testing.T) {
	locate := `<search_results count="1" query="q">
<chunk rank="1" chunk_id="c1" doc_id="d1" page_num="0" chunk_index="2" dataset_id="kb1" doc_name="5412.md" score="0.900">
<match_snippet>snippet</match_snippet>
</chunk>
</search_results>`
	deepRead := `<chunks doc_id="d1" fetched="2" selected="2">
<chunk chunk_id="c1" doc_id="d1" page_num="0" chunk_index="2" dataset_id="kb1" doc_name="5412.md">
<content>text</content>
</chunk>
</chunks>`
	web := `<web_results count="1" query="q">
<result rank="1" url="https://example.com/a" title="A">snippet</result>
</web_results>`

	// Only the doc_name leg is recorded, normalized to the file stem the
	// benchmark corpora key their evidence on; the opaque 32-hex doc_id is
	// deliberately dropped.
	got := retrievalaccount.RecordedDocIDs(locate)
	want := []string{"5412"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("retrievalaccount.RecordedDocIDs(locate) = %v, want %v", got, want)
	}
	got = retrievalaccount.RecordedDocIDs(deepRead)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("retrievalaccount.RecordedDocIDs(deepRead) = %v, want %v", got, want)
	}
	// A bare stem and its .md twin normalize to the same value.
	stemTwins := `<chunks doc_id="d1" fetched="2" selected="2">
<chunk chunk_id="c1" doc_id="d1" doc_name="5412.md"><content>a</content></chunk>
<chunk chunk_id="c2" doc_id="d1" doc_name="5412"><content>b</content></chunk>
</chunks>`
	if got = retrievalaccount.RecordedDocIDs(stemTwins); !reflect.DeepEqual(got, []string{"5412", "5412"}) {
		t.Fatalf("retrievalaccount.RecordedDocIDs(stemTwins) = %v, want [5412 5412] (ledger dedups)", got)
	}
	if got := retrievalaccount.RecordedDocIDs(web); len(got) != 0 {
		t.Fatalf("retrievalaccount.RecordedDocIDs(web) = %v, want none", got)
	}
	if got := retrievalaccount.RecordedDocIDs(""); len(got) != 0 {
		t.Fatalf("retrievalaccount.RecordedDocIDs(empty) = %v, want none", got)
	}
}

// TestDocIDLedgerDedupsAndSorts: the ledger feeds a benchmark's retrieval-recall
// score, which is a SET intersection — duplicates must collapse, and the
// snapshot must be sorted so an archived payload is byte-stable across runs.
func TestDocIDLedgerDedupsAndSorts(t *testing.T) {
	ledger := NewDocIDLedger()
	ledger.Add("d2", "5412.md")
	ledger.Add("d1", "5412.md", "")
	ledger.Add()

	got := ledger.Snapshot()
	want := []string{"5412.md", "d1", "d2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Snapshot() = %v, want %v", got, want)
	}

	// A nil ledger must stay usable: the wrapper is built with a nil ledger
	// whenever the caller does not ask for one.
	var nilLedger *docIDLedger
	nilLedger.Add("d1")
	if got := nilLedger.Snapshot(); got != nil {
		t.Fatalf("nil Snapshot() = %v, want nil", got)
	}
}

// TestInstrumentedToolRecordsDocIDs: the wrapper is the only place the ledger
// is fed, so it must record on success and stay silent on failure — a tool
// that errored returned nothing the agent could have read — and it must
// record ONLY full-content tools (search_chunks, list_chunks): a doc seen
// through a locate-only tool (search_bm25_chunks, grep_chunks) was located,
// not retrieved.
func TestInstrumentedToolRecordsDocIDs(t *testing.T) {
	ledger := NewDocIDLedger()
	ok := &instrumentedTool{InvokableTool: &stubTool{
		name: "list_chunks",
		out:  `<chunks doc_id="d1" fetched="1"><chunk doc_name="5412.md"></chunk></chunks>`,
	}, docs: ledger}
	if _, err := ok.InvokableRun(t.Context(), "{}"); err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if got := ledger.Snapshot(); !reflect.DeepEqual(got, []string{"5412"}) {
		t.Fatalf("ledger after success = %v, want [5412]", got)
	}

	locateOnly := &instrumentedTool{InvokableTool: &stubTool{
		name: "grep_chunks",
		out:  `<search_results count="1"><chunk doc_id="d2" doc_name="6001.md"><match_snippet>x</match_snippet></chunk></search_results>`,
	}, docs: ledger}
	if _, err := locateOnly.InvokableRun(t.Context(), "{}"); err != nil {
		t.Fatalf("InvokableRun(locate-only): %v", err)
	}
	if got := ledger.Snapshot(); !reflect.DeepEqual(got, []string{"5412"}) {
		t.Fatalf("ledger after locate-only tool = %v, want it unchanged ([5412])", got)
	}

	failing := &instrumentedTool{InvokableTool: &stubTool{
		name: "list_chunks",
		out:  `<chunks doc_id="d2" fetched="1"><chunk doc_name="6001.md"></chunk></chunks>`,
		err:  errStubTool,
	}, docs: ledger}
	if _, err := failing.InvokableRun(t.Context(), "{}"); err == nil {
		t.Fatal("expected the stub tool's error")
	}
	if got := ledger.Snapshot(); !reflect.DeepEqual(got, []string{"5412"}) {
		t.Fatalf("ledger after failure = %v, want it unchanged ([5412])", got)
	}
}

// TestDocIDLedgerConcurrentAdd: same-round tool calls execute in parallel, so
// the ledger is written from several goroutines at once.
func TestDocIDLedgerConcurrentAdd(t *testing.T) {
	ledger := NewDocIDLedger()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ledger.Add("d1", string(rune('a'+i%8)))
		}(i)
	}
	wg.Wait()
	if got := len(ledger.Snapshot()); got != 9 {
		t.Fatalf("Snapshot() has %d entries, want 9", got)
	}
}
