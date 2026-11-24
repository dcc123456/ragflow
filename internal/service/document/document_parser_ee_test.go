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

package document

import (
	"reflect"
	"testing"

	"ragflow/internal/entity"
	_ "ragflow/internal/ingestion/component/chunker"
	"ragflow/internal/utility"
)

func TestResolveCADDocumentParserPreservesDeclaredCanvas(t *testing.T) {
	pipelineID := "custom-cad-canvas"
	config := entity.JSONMap{"Parser:Custom": map[string]any{"cad": map[string]any{}}}
	kb := &entity.Knowledgebase{PipelineID: &pipelineID, ParserConfig: config}
	for _, filename := range []string{"drawing.dwg", "drawing.dxf", "DRAWING.DXFB"} {
		t.Run(filename, func(t *testing.T) {
			selection, got := resolveDocumentParser(t.Context(), kb, filename, utility.FilenameType(filename), config)
			if !selection.IsPipeline() || selection.PipelineID != pipelineID {
				t.Fatalf("CAD-declaring canvas replaced: pipeline_id=%q parser_id=%q", selection.PipelineID, selection.ParserID)
			}
			if !reflect.DeepEqual(got, config) {
				t.Fatalf("declared canvas config changed: %v", got)
			}
		})
	}
}

func TestResolveCADDocumentParserUsesBuiltinForUndeclaredFamily(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	seedDocumentParserTenant(t, "tenant-1", "user-1")
	config := generalParserConfig()
	kb := &entity.Knowledgebase{TenantID: "tenant-1", ParserID: "general", ParserConfig: config}
	for _, filename := range []string{"drawing.dwg", "drawing.dxf", "DRAWING.DXFB"} {
		t.Run(filename, func(t *testing.T) {
			selection, got := resolveDocumentParser(t.Context(), kb, filename, utility.FilenameType(filename), config)
			if !selection.IsBuiltIn() || selection.ParserID != "cad" {
				t.Fatalf("CAD drawing did not select builtin pipeline: %#v", selection)
			}
			params, ok := got["Parser:PaperTrail"].(map[string]any)
			if !ok {
				t.Fatalf("CAD parser config missing: %v", got)
			}
			if _, ok := params["cad"]; !ok {
				t.Fatalf("CAD setup missing: %v", params)
			}
			if _, ok := got["CADChunker:SpatialSheet"]; !ok {
				t.Fatalf("CAD chunker config missing: %v", got)
			}
		})
	}
}

func TestResolveVisioDocumentParserPreservesDeclaredCanvas(t *testing.T) {
	pipelineID := "custom-visio-canvas"
	config := entity.JSONMap{"Parser:Custom": map[string]any{"visio": map[string]any{}}}
	kb := &entity.Knowledgebase{PipelineID: &pipelineID, ParserConfig: config}
	for _, filename := range []string{"arch.vsdx", "ARCH.VSDX", "arch.vsd", "ARCH.VSD", "arch.vsdm", "ARCH.VSDM"} {
		t.Run(filename, func(t *testing.T) {
			selection, got := resolveDocumentParser(t.Context(), kb, filename, utility.FilenameType(filename), config)
			if !selection.IsPipeline() || selection.PipelineID != pipelineID {
				t.Fatalf("Visio-declaring canvas replaced: pipeline_id=%q parser_id=%q", selection.PipelineID, selection.ParserID)
			}
			if !reflect.DeepEqual(got, config) {
				t.Fatalf("declared canvas config changed: %v", got)
			}
		})
	}
}

func TestResolveVisioDocumentParserUsesBuiltinForUndeclaredFamily(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	seedDocumentParserTenant(t, "tenant-1", "user-1")
	config := generalParserConfig()
	kb := &entity.Knowledgebase{TenantID: "tenant-1", ParserID: "general", ParserConfig: config}
	for _, filename := range []string{"arch.vsdx", "ARCH.VSDX", "arch.vsd", "ARCH.VSD", "arch.vsdm", "ARCH.VSDM"} {
		t.Run(filename, func(t *testing.T) {
			selection, got := resolveDocumentParser(t.Context(), kb, filename, utility.FilenameType(filename), config)
			if !selection.IsBuiltIn() || selection.ParserID != "visio" {
				t.Fatalf("Visio drawing did not select builtin pipeline: %#v", selection)
			}
			params, ok := got["Parser:PaperTrail"].(map[string]any)
			if !ok {
				t.Fatalf("Visio parser config missing: %v", got)
			}
			if _, ok := params["visio"]; !ok {
				t.Fatalf("Visio setup missing: %v", params)
			}
			if _, ok := got["PageChunker:VisioPages"]; !ok {
				t.Fatalf("Visio page chunker config missing: %v", got)
			}
		})
	}
}
