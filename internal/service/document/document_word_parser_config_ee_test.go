//go:build cgo && ee

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

package document

import (
	"os"
	"slices"
	"strings"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/ingestion/component"
	"ragflow/internal/parser/parser"
	"ragflow/internal/utility"
)

func TestWordFilterFollowsDOCToFallbackParser(t *testing.T) {
	const sourceID = "Parser:DatasetWord"
	const filename = "word-low-contrast.doc"
	const marker = "Low contrast sample"
	// Synthetic Word 97 document with one #F9F9F9 text run, generated
	// using the same binary fixture layout as the native Word parser tests.
	data, err := os.ReadFile("testdata/" + filename)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		enabled bool
	}{
		{"off", false},
		{"on despite another Parser opting out", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := entity.JSONMap{
				sourceID: map[string]any{
					"docx":                     map[string]any{},
					"remove_low_contrast_text": tc.enabled,
				},
				"Parser:WordFilterDisabled": map[string]any{
					"docx":                     map[string]any{},
					"remove_low_contrast_text": false,
				},
			}
			kb := &entity.Knowledgebase{ParserID: "qa", ParserConfig: config}
			selection, resolved := resolveDocumentParser(t.Context(), kb, filename, utility.FileTypeDOC, config)
			if selection.ParserID != generalParserID {
				t.Fatalf("resolved to %q, want %q", selection.ParserID, generalParserID)
			}
			var params map[string]any
			for id, raw := range resolved {
				if id != sourceID && common.IsParserComponentID(id) {
					params, _ = raw.(map[string]any)
					break
				}
			}
			if params == nil {
				t.Fatal("expected a different Parser ID on the fallback")
			}
			cpn, err := component.NewParserComponent(params)
			if err != nil {
				t.Fatal(err)
			}
			out, err := cpn.Invoke(t.Context(), nil, map[string]any{"name": filename, "binary": data})
			if err != nil {
				t.Fatal(err)
			}
			items, ok := out["json"].([]parser.DocItem)
			if !ok {
				t.Fatal("expected structured Parser output")
			}
			retained := slices.ContainsFunc(items, func(item parser.DocItem) bool {
				text, _ := item.ToMap()["text"].(string)
				return strings.Contains(text, marker)
			})
			if retained == tc.enabled {
				t.Errorf("low-contrast text retained = %t, want %t", retained, !tc.enabled)
			}
		})
	}
}
