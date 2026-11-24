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

package service

import "testing"

// agenticDocAggs must aggregate per document in first-appearance order — the
// UI's citation popover resolves the document behind a [ID:N] marker by
// matching doc_aggs[].doc_id against the cited chunk's document_id.
func TestAgenticDocAggs(t *testing.T) {
	chunks := []map[string]interface{}{
		{"id": "c1", "doc_id": "d2", "docnm_kwd": "b.md"},
		{"id": "c2", "doc_id": "d1", "docnm_kwd": "a.md"},
		{"id": "c3", "doc_id": "d2", "docnm_kwd": "b.md"},
		{"id": "c4", "doc_id": "", "docnm_kwd": "orphan"}, // no doc_id: skipped
	}
	got := agenticDocAggs(chunks)
	if len(got) != 2 {
		t.Fatalf("doc_aggs = %d items, want 2", len(got))
	}
	first, ok := got[0].(map[string]interface{})
	if !ok || first["doc_id"] != "d2" || first["doc_name"] != "b.md" || first["count"] != 2 {
		t.Errorf("aggs[0] = %v, want d2/b.md/count=2", got[0])
	}
	second := got[1].(map[string]interface{})
	if second["doc_id"] != "d1" || second["count"] != 1 {
		t.Errorf("aggs[1] = %v, want d1/count=1", got[1])
	}
}

func TestAgenticDocAggsEmpty(t *testing.T) {
	if got := agenticDocAggs(nil); got == nil || len(got) != 0 {
		t.Errorf("agenticDocAggs(nil) = %v, want empty non-nil slice", got)
	}
}
