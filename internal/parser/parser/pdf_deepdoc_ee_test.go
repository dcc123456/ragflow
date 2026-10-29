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
//

package parser

import (
	"testing"

	"ragflow/internal/deepdoc/client"
	deepdoctype "ragflow/internal/deepdoc/parser/type"
)

// TestDeepDocEECPUValidateParseMethod ensures the suffixed method passes
// validation, so a PDFParser configured with it does not error out before
// parsing.
func TestDeepDocEECPUValidateParseMethod(t *testing.T) {
	p := &PDFParser{ParseMethod: "deepdoc_cpu_ee"}
	if err := p.validateParseMethod(); err != nil {
		t.Fatalf("validateParseMethod(deepdoc_cpu_ee) = %v, want nil", err)
	}
}

// TestResolveDocAnalyzerForMethod_OSS confirms the OSS in-process path still
// resolves to the native factory when the method is empty/oss/deepdoc.
func TestResolveDocAnalyzerForMethod_OSS(t *testing.T) {
	useMockDocAnalyzer(t)
	for _, m := range []string{"", "deepdoc", "DeepDOC"} {
		a, err := resolveDocAnalyzerForMethod(m, deepdoctype.NativeDocAnalyzerFactory)
		if err != nil {
			t.Fatalf("resolveDocAnalyzerForMethod(%q) err = %v", m, err)
		}
		if a == nil {
			t.Fatalf("resolveDocAnalyzerForMethod(%q) returned nil analyzer", m)
		}
		var _ deepdoctype.DocAnalyzer = a
	}
}

// TestResolveDocAnalyzerForMethod_EECPU_Configured confirms deepdoc_cpu_ee
// resolves to a client when its URL is configured.
func TestResolveDocAnalyzerForMethod_EECPU_Configured(t *testing.T) {
	t.Setenv("DEEPDOC_CPU_EE_URL", "http://ee.test:8001")

	a, err := resolveDocAnalyzerForMethod("deepdoc_cpu_ee", deepdoctype.NativeDocAnalyzerFactory)
	if err != nil {
		t.Fatalf("resolveDocAnalyzerForMethod(deepdoc_cpu_ee) err = %v", err)
	}
	// client.Client implements DocAnalyzer; verify via the interface contract.
	var _ deepdoctype.DocAnalyzer = a
	if c, ok := a.(*client.Client); !ok || c == nil {
		t.Fatalf("expected *client.Client, got %T", a)
	}
}

// TestResolveDocAnalyzerForMethod_EECPU_Unconfigured confirms deepdoc_cpu_ee
// errors loudly (not a silent empty analyzer) when no EE URL is configured.
func TestResolveDocAnalyzerForMethod_EECPU_Unconfigured(t *testing.T) {
	t.Setenv("DEEPDOC_CPU_EE_URL", "")

	if _, err := resolveDocAnalyzerForMethod("deepdoc_cpu_ee", deepdoctype.NativeDocAnalyzerFactory); err == nil {
		t.Fatal("want error when deepdoc_cpu_ee selected but no EE URL configured")
	}
}
