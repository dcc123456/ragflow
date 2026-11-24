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
//  Portions of this file may incorporate code from RAGFlow Community Edition,
//  which is licensed under the Apache License, Version 2.0.
//
//  For commercial licensing inquiries: sales@infiniflow.ai
//

package pipeline

import "testing"

func TestBuildParserConfigPreservesWordTextFilter(t *testing.T) {
	const node = "Parser:Word"
	dsl := []byte(`{"components":{"Parser:Word":{"obj":{"component_name":"Parser","params":{"doc":{},"docx":{}}}}}}`)
	for _, enabled := range []bool{true, false} {
		raw := map[string]any{node: map[string]any{"remove_low_contrast_text": enabled}}
		saved, err := BuildParserConfig(dsl, raw)
		if err != nil {
			t.Fatal(err)
		}
		parserParams, _ := saved[node].(map[string]any)
		if got := parserParams["remove_low_contrast_text"]; got != enabled {
			t.Errorf("Parser filter = %v, want %t", got, enabled)
		}
	}
}
