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

package chunker

import (
	"context"
	"strings"
	"testing"

	"ragflow/internal/agent/runtime"
	"ragflow/internal/parser/cadsource"
)

// TestCADChunkerComponentRegistered guards the DSL contract: the cad pipeline
// template names "CADChunker", so the component must exist under that exact
// name in the ingestion registry.
func TestCADChunkerComponentRegistered(t *testing.T) {
	factory, category, metadata, ok := runtime.DefaultRegistry.Lookup(ComponentNameCADChunker)
	if !ok {
		t.Fatalf("%q is not registered", ComponentNameCADChunker)
	}
	if category != runtime.CategoryIngestion {
		t.Fatalf("category = %q, want %q", category, runtime.CategoryIngestion)
	}
	if metadata.Version == "" {
		t.Fatal("registered without a contract version")
	}
	if factory == nil {
		t.Fatal("registered without a factory")
	}
}

// TestCADChunkerComponentInvoke runs the CAD component over a parsed
// drawing and asserts the emitted chunks keep the drawing context header.
func TestCADChunkerComponentInvoke(t *testing.T) {
	drawing := cadTestDrawing(
		[]cadsource.Sheet{cadTestSheet("s1", [4]float64{0, 0, 1000, 600})},
		[]cadsource.Text{
			cadTestText("主视图", "s1", 100, 500, 10),
			cadTestText("φ30", "s1", 110, 495, 10),
			cadTestText("120", "s1", 120, 490, 10),
		},
	)
	inputs := map[string]any{
		"name":          "drawing.dxf",
		"output_format": "json",
		"json":          []any{},
		"file": map[string]any{
			"name": "drawing.dxf",
			"cad":  drawing,
		},
	}

	component := &cadChunkerComponent{}
	out, err := component.Invoke(context.Background(), nil, inputs)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	chunks, ok := out["chunks"].([]map[string]any)
	if !ok {
		t.Fatalf("chunks type = %T", out["chunks"])
	}
	if len(chunks) == 0 {
		t.Fatal("no chunks emitted")
	}
	// chunkOutputs folds the drawing context into the chunk body, so the
	// header must lead the text rather than sit in context_above. The header
	// uses the drawing name without its extension.
	text, _ := chunks[0]["text"].(string)
	if !strings.HasPrefix(text, "[drawing · s1/") {
		t.Fatalf("chunk has no drawing context: %q", text)
	}
}

// TestCADChunkerComponentRejectsMissingPayload locks the failure mode: the cad
// pipeline only runs for CAD documents, so a missing structured payload is a
// wiring defect and must fail instead of emitting an empty chunk list.
func TestCADChunkerComponentRejectsMissingPayload(t *testing.T) {
	component := &cadChunkerComponent{}
	_, err := component.Invoke(context.Background(), nil, map[string]any{
		"name":          "drawing.dxf",
		"output_format": "json",
		"json":          []any{},
	})
	if err == nil {
		t.Fatal("Invoke accepted an upstream without a CAD payload")
	}
}
