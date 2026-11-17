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

package pipeline

import (
	"testing"

	chunkerpkg "ragflow/internal/ingestion/component/chunker"
)

// CADChunker has no user-editable params; its spatial defaults live in the
// chunker package rather than componentParamSchemaKeys.
func init() {
	componentParamWhitelistExemptions[chunkerpkg.ComponentNameCADChunker] = "no user-editable params; spatial defaults live in the chunker package"
}

// The task golden tests cover node IDs and defaults. This check ensures the
// template selects the CAD implementation through its component_name.
func TestCADTemplateUsesCADChunker(t *testing.T) {
	dsl, err := LoadBuiltinDSL("cad")
	if err != nil {
		t.Fatalf("LoadBuiltinDSL(cad): %v", err)
	}
	components, err := ExtractAllComponentParams([]byte(dsl))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, component := range components {
		if component.ComponentName == chunkerpkg.ComponentNameCADChunker {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("CADChunker components = %d, want 1", count)
	}
}
