//go:build cgo && ee

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

package parser

import (
	"strings"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/deepdoc/client"
	deepdoctype "ragflow/internal/deepdoc/parser/type"
)

// eeMethodURLE returns the generic METHOD_URL env key for a parse method,
// matching the externalAnalyzerURL convention.
func eeMethodURLE(m string) string {
	return strings.ToUpper(m) + "_URL"
}

// TestEEDeepDocMethods_ValidateParseMethod confirms each new EE method passes
// validation, so a PDFParser configured with it does not error out before
// parsing.
func TestEEDeepDocMethods_ValidateParseMethod(t *testing.T) {
	for _, m := range eeDeepDocMethods {
		t.Run(m, func(t *testing.T) {
			p := &PDFParser{ParseMethod: m}
			if err := p.validateParseMethod(); err != nil {
				t.Fatalf("validateParseMethod failed: %v", err)
			}
		})
	}
}

// TestEEDeepDocMethods_IsPDFParseMethod confirms each new EE method is
// recognized as a real PDF parse method rather than a VLM model selector.
func TestEEDeepDocMethods_IsPDFParseMethod(t *testing.T) {
	for _, m := range eeDeepDocMethods {
		if !IsPDFParseMethod(m) {
			t.Fatalf("IsPDFParseMethod returned false for %s", m)
		}
	}
}

// TestEEDeepDocMethods_ResolveConfigured confirms each new EE method resolves
// to a client.Client when its METHOD_URL is set, and NOT to the in-process
// native analyzer.
func TestEEDeepDocMethods_ResolveConfigured(t *testing.T) {
	for _, m := range eeDeepDocMethods {
		t.Run(m, func(t *testing.T) {
			t.Setenv(eeMethodURLE(m), "http://ee.test:8001")
			a, err := resolveDocAnalyzerForMethod(m, deepdoctype.NativeDocAnalyzerFactory)
			if err != nil {
				t.Fatalf("resolve failed: %v", err)
			}
			var _ deepdoctype.DocAnalyzer = a
			if c, ok := a.(*client.Client); !ok || c == nil {
				t.Fatalf("expected client.Client for %s, got %T", m, a)
			}
		})
	}
}

// TestEEDeepDocMethods_ResolveUnconfigured confirms each new EE method errors
// loudly when its METHOD_URL is unset, instead of falling back to the
// in-process native analyzer.
func TestEEDeepDocMethods_ResolveUnconfigured(t *testing.T) {
	for _, m := range eeDeepDocMethods {
		t.Run(m, func(t *testing.T) {
			t.Setenv(eeMethodURLE(m), "")
			if _, err := resolveDocAnalyzerForMethod(m, deepdoctype.NativeDocAnalyzerFactory); err == nil {
				t.Fatalf("expected error for %s when url unset", m)
			}
		})
	}
}

// TestEEDeepDocBackends_URLConvention guards the single source of truth:
// every common.EEDeepDocBackends entry must use the generic <METHOD>_URL env
// key the parser resolves through (eeMethodURLE), so the handler probe and the
// parser dispatch cannot disagree on which env var holds a backend URL.
func TestEEDeepDocBackends_URLConvention(t *testing.T) {
	for _, b := range common.EEDeepDocBackends {
		want := eeMethodURLE(b.ID)
		if b.URLEnv != want {
			t.Fatalf("backend %s: URLEnv %q != expected %q", b.ID, b.URLEnv, want)
		}
	}
}
