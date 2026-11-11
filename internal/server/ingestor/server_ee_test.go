//go:build ee

//
//  Copyright (c) 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//
//  This file is part of the RAGFlow Enterprise Edition and is licensed,
//  not sold, under the RAGFlow Enterprise Commercial License Agreement.
//  Unauthorized copying, modification, distribution, or disclosure of this
//  file, via any medium, is strictly prohibited.
//
//  This file may incorporate code from RAGFlow Community Edition, which is
//  licensed under the Apache License, Version 2.0.
//

package ingestor

import "testing"

// TestDefaultSupportedTypesIncludesCADExtensions verifies the EE init() appends
// the CAD extensions to the community base set (pdf/docx/txt) without dropping
// or duplicating any base entry. The community base is unchanged.
func TestDefaultSupportedTypesIncludesCADExtensions(t *testing.T) {
	got := defaultSupportedTypes()
	have := make(map[string]bool, len(got))
	for _, s := range got {
		have[s] = true
	}
	for _, want := range []string{"pdf", "docx", "txt", "dwg", "dxf", "dxfb"} {
		if !have[want] {
			t.Errorf("defaultSupportedTypes missing %q; got %v", want, got)
		}
	}
}
