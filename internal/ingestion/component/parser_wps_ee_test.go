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

//go:build ee

package component

import (
	"testing"

	"ragflow/internal/utility"
)

func TestWPSParserDispatch(t *testing.T) {
	for _, tc := range []struct {
		ext    string
		ft     utility.FileType
		family string
	}{
		{"wps", utility.FileTypeDOC, "doc"}, {"wpt", utility.FileTypeDOC, "doc"},
		{"et", utility.FileTypeXLS, "spreadsheet"}, {"ett", utility.FileTypeXLS, "spreadsheet"},
		{"dps", utility.FileTypePPT, "slides"}, {"dpt", utility.FileTypePPT, "slides"},
	} {
		t.Run(tc.ext, func(t *testing.T) {
			if got := ParserFileFamily(tc.ext); got != tc.family {
				t.Errorf("family = %q, want %q", got, tc.family)
			}
			for _, inputs := range []map[string]any{
				{"file_type": tc.ext}, {"name": "sample." + tc.ext},
				{"file": map[string]any{"name": "sample." + tc.ext}},
			} {
				if got := fileTypeFromInputs(inputs); got != tc.ft {
					t.Errorf("fileTypeFromInputs(%v) = %q, want %q", inputs, got, tc.ft)
				}
			}
		})
	}
}

func TestWPSParserRejectsInvalidBinary(t *testing.T) {
	for _, ext := range []string{"wps", "wpt", "et", "ett", "dps", "dpt"} {
		t.Run(ext, func(t *testing.T) {
			c, err := NewParserComponent(map[string]any{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.Invoke(t.Context(), nil, map[string]any{
				"name": "broken." + ext, "binary": []byte("invalid Office binary"),
			}); err == nil {
				t.Fatal("invalid Office file accepted as raw text")
			}
		})
	}
}
