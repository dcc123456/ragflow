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

//go:build ee && cgo

package file

import (
	"strings"
	"testing"
)

func TestWPSCompatibleOfficeContentExtraction(t *testing.T) {
	// Exercise content detection with Office content carrying WPS extensions.
	// Native WPS variants need their own real-document acceptance fixtures.
	data := minimalDOCX(t, "Enterprise Office content")
	for _, name := range []string{"document.wps", "template.wpt", "book.et", "template.ett", "deck.dps", "DECK.DPT"} {
		t.Run(name, func(t *testing.T) {
			content := parseFileContent(t.Context(), name, data)
			if !strings.Contains(content, "Enterprise Office content") || content == string(data) {
				t.Fatal("file content did not use the detected Office parser")
			}
			content, err := parseAgentUploadContent(t.Context(), name, data, "")
			if err != nil || !strings.Contains(content, "Enterprise Office content") {
				t.Fatalf("agent content = %q, error = %v", content, err)
			}
		})
	}
}
