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

//go:build ee && manual

package chunker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ragflow/internal/parser/cadsource"
)

// TestCADInkCorpus renders a local CAD corpus and chunks it through the ink
// path. It is a local review tool: set CAD_CORPUS_DIR to a directory of
// DWG/DXF/DXFB files and inspect the chunk count/content printed by -v.
func TestCADInkCorpus(t *testing.T) {
	root := os.Getenv("CAD_CORPUS_DIR")
	if root == "" {
		t.Skip("set CAD_CORPUS_DIR to a local CAD corpus directory")
	}
	files := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".dwg" && ext != ".dxf" && ext != ".dxfb" {
			return nil
		}
		files++
		rel, _ := filepath.Rel(root, path)
		t.Run(rel, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			drawing, err := cadsource.Extract(context.Background(), entry.Name(), data)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := parseCADDocument(entry.Name(), data)
			if err != nil {
				t.Fatal(err)
			}
			preview, err := buildCADPreview(context.Background(), doc, drawing.Sheets, cadPreviewPageWidth)
			if err != nil {
				t.Fatalf("preview: %v", err)
			}
			ink := buildCADInk(preview.Pages)
			if ink == nil && len(drawing.Texts) > 0 {
				t.Fatal("no ink index for a drawing with text")
			}
			chunks, diagnostics := buildCADChunks(drawing, ink, entry.Name())
			excluded := 0
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == "cad_sheet_grid_excluded" {
					excluded += diagnostic.Count
				}
				if diagnostic.Code == "cad_text_unassigned" || diagnostic.Code == "cad_source_duplicate" {
					t.Errorf("source coverage failure: %+v", diagnostic)
				}
				if diagnostic.Code == "cad_ink_unavailable" {
					t.Errorf("ink fallback on a renderable drawing: %+v", diagnostic)
				}
			}
			sources := cadTestSourceIDs(chunks)
			if len(sources) != len(drawing.Texts)-excluded {
				t.Errorf("source coverage=%d want=%d after grid exclusion", len(sources), len(drawing.Texts)-excluded)
			}
			maxRunes := 0
			for _, chunk := range chunks {
				if runes := len([]rune(chunk.Content)); runes > maxRunes {
					maxRunes = runes
				}
			}
			if maxRunes > cadChunkOversizeRunes {
				t.Errorf("chunk exceeds size guardrail: %d runes", maxRunes)
			}
			t.Logf("chunks=%d max_runes=%d diagnostics=%v", len(chunks), maxRunes, diagnostics)
			if dir := os.Getenv("CAD_REVIEW_OUTPUT"); dir != "" {
				raw, err := json.MarshalIndent(map[string]any{
					"document": entry.Name(), "chunks": chunks, "diagnostics": diagnostics,
				}, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				_ = os.MkdirAll(dir, 0o755)
				if err := os.WriteFile(filepath.Join(dir, entry.Name()+".json"), raw, 0o600); err != nil {
					t.Fatal(err)
				}
			}
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Fatal("CAD_CORPUS_DIR contains no CAD drawings")
	}
}
