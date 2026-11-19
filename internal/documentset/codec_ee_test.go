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

package documentset

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// TestEncodeStoresTheJSONItself pins what a row holds: the set as text, with no
// codec in between, so a hand-written SELECT reads it and the hash names it.
func TestEncodeStoresTheJSONItself(t *testing.T) {
	cases := map[string][]byte{
		"empty":  []byte("[]"),
		"one":    []byte(`["doc-1"]`),
		"many":   []byte(`["doc-1","doc-2","doc-3"]`),
		"nested": []byte(`["doc-2"]`),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if stored := Encode(raw); stored != string(raw) {
				t.Fatalf("stored = %q, want the JSON itself", stored)
			}
		})
	}
}

// TestEncodeIsDeterministic keeps content addressing honest: the same set must
// produce the same bytes, or two versions of one state would store it twice.
func TestEncodeIsDeterministic(t *testing.T) {
	raw := []byte(`["doc-1","doc-2"]`)
	if first, second := Encode(raw), Encode(raw); first != second {
		t.Fatalf("encode is not deterministic:\nfirst  = %q\nsecond = %q", first, second)
	}
}

// TestHashNamesTheStoredText pins the identity: the hash is taken over the text the
// row holds, so the hash and the bytes cannot describe two different sets.
func TestHashNamesTheStoredText(t *testing.T) {
	raw := []byte(`["doc-1","doc-2"]`)
	sum := sha256.Sum256(raw)
	if got, want := Hash(raw), hex.EncodeToString(sum[:hashBytes]); got != want {
		t.Fatalf("hash = %q, want the sha256 of the stored text %q", got, want)
	}
	if got, want := Hash(raw), Hash([]byte(Encode(raw))); got != want {
		t.Fatalf("hashing the stored value = %q, want the same as hashing the text %q", got, want)
	}
}

// TestShortHashKeepsThePrefix keeps a log line short without cutting a hash
// that is already short.
func TestShortHashKeepsThePrefix(t *testing.T) {
	full := Hash([]byte("[]"))
	if got, want := ShortHash(full), full[:8]; got != want {
		t.Fatalf("short hash = %q, want %q", got, want)
	}
	if got := ShortHash("abc"); got != "abc" {
		t.Fatalf("short hash of a short value = %q, want it unchanged", got)
	}
}
