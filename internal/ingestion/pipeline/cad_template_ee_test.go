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
	"reflect"
	"testing"

	chunkerpkg "ragflow/internal/ingestion/component/chunker"
)

func init() {
	componentParamWhitelistExemptions[chunkerpkg.ComponentNameCADChunker] = "editable keys come from the CAD template defaults"
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

func TestCADTemplateSavesVisionConfig(t *testing.T) {
	dsl, err := LoadBuiltinDSL("cad")
	if err != nil {
		t.Fatal(err)
	}
	const id = "CADChunker:SpatialSheet"
	vision := map[string]any{"enabled": true, "llm_id": "vision1", "system_prompt": "visible geometry"}
	config, err := BuildParserConfig([]byte(dsl), map[string]any{id: map[string]any{"vision": vision}})
	if err != nil {
		t.Fatal(err)
	}
	params, ok := config[id].(map[string]any)
	if !ok || !reflect.DeepEqual(params["vision"], vision) {
		t.Fatalf("vision config lost when saving the CAD template: %#v", config)
	}
}
