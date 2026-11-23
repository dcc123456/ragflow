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

import "testing"

// TestResolveDatasetScope pins the mode 8 isolation rule: a tool argument may only NARROW
// the ticket's bound scope, never widen it. An id outside the bound set is a hard error,
// not a silent drop.
func TestResolveDatasetScope(t *testing.T) {
	bound := []string{"kb-a", "kb-b"}

	// Omitted -> the full bound scope.
	got, err := resolveDatasetScope(bound, nil)
	if err != nil {
		t.Fatalf("empty requested: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("empty requested = %v, want the bound scope", got)
	}

	// A subset is honored.
	got, err = resolveDatasetScope(bound, []string{"kb-a"})
	if err != nil {
		t.Fatalf("subset: %v", err)
	}
	if len(got) != 1 || got[0] != "kb-a" {
		t.Fatalf("subset = %v, want [kb-a]", got)
	}

	// An out-of-scope id is refused outright.
	if _, err := resolveDatasetScope(bound, []string{"kb-other"}); err == nil {
		t.Fatal("out-of-scope id must be rejected, not silently trimmed")
	}

	// A single out-of-scope id poisons the whole request (no partial widening).
	if _, err := resolveDatasetScope(bound, []string{"kb-a", "kb-other"}); err == nil {
		t.Fatal("a mixed request including an out-of-scope id must be rejected")
	}

	// No bound scope + a request = nothing is allowed.
	if _, err := resolveDatasetScope(nil, []string{"kb-a"}); err == nil {
		t.Fatal("empty bound scope must reject any requested id")
	}
}
