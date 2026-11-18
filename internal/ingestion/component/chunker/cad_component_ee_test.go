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
	"fmt"
	"strings"
	"testing"

	"ragflow/internal/agent/runtime"
	"ragflow/internal/ingestion/component/schema"
	"ragflow/internal/parser/cadsource"
	"ragflow/internal/storage"
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
	if metadata.Outputs["warnings"] == "" || ChunkerOutputs["warnings"] != "" {
		t.Fatal("visual enhancement warnings must be declared only for CADChunker")
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

func TestCADChunkerVisionRunsOnRenderedChunks(t *testing.T) {
	factory := storage.GetStorageFactory()
	original := factory.GetStorage()
	store := storage.NewMemoryStorage()
	factory.SetStorage(store)
	t.Cleanup(func() { factory.SetStorage(original) })
	if err := store.Put(t.Context(), "kb1", "source.dxf", []byte(cadImageTestDXF)); err != nil {
		t.Fatal(err)
	}
	drawing, err := cadsource.Extract(t.Context(), "drawing.dxf", []byte(cadImageTestDXF))
	if err != nil {
		t.Fatal(err)
	}
	create, _, _, _ := runtime.DefaultRegistry.Lookup(ComponentNameCADChunker)
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("enabled=%t", enabled), func(t *testing.T) {
			c, err := create("CADChunker:SpatialSheet", map[string]any{"vision": map[string]any{"enabled": enabled}})
			if err != nil {
				t.Fatal(err)
			}
			// Without a tenant, optional enhancement must preserve rendered chunks.
			out, err := c.Invoke(t.Context(), nil, map[string]any{
				"name": "drawing.dxf", "bucket": "kb1", "path": "source.dxf", "kb_id": "kb1", "doc_id": "doc1",
				"output_format": "json", "json": []any{}, "file": map[string]any{"name": "drawing.dxf", "cad": drawing},
			})
			if err != nil {
				t.Fatal(err)
			}
			chunks, ok := schema.GetChunks(out)
			if enabled {
				warnings, _ := out["warnings"].([]string)
				if len(warnings) != 1 || !strings.Contains(warnings[0], "tenant ID is missing") {
					t.Fatalf("missing model diagnostic: %v", warnings)
				}
			}
			if !ok || len(chunks) != 1 || chunks[0].ImgID == "" || len(chunks[0].Positions) == 0 || chunks[0].Text == "" {
				t.Fatalf("optional enhancement lost rendered text or highlight: %+v", out)
			}
			if _, err := store.Get(t.Context(), "kb1", strings.TrimPrefix(chunks[0].ImgID, "kb1-")); err != nil {
				t.Fatal(err)
			}
		})
	}
}
