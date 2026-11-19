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

// TestCADCorpusQuality uses an explicitly supplied local corpus. Downloaded
// drawings with unverified redistribution rights are not repository fixtures.
func TestCADCorpusQuality(t *testing.T) {
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
			doc, err := cadsource.Extract(context.Background(), entry.Name(), data)
			if err != nil {
				t.Fatal(err)
			}
			chunks, diagnostics := buildCADChunks(doc, nil, entry.Name())
			if len(doc.Texts) > 0 && len(chunks) == 0 {
				t.Fatal("searchable source text produced no chunks")
			}
			excluded := 0
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == "cad_sheet_grid_excluded" {
					excluded += diagnostic.Count
				}
				if diagnostic.Code == "cad_text_unassigned" || diagnostic.Code == "cad_source_duplicate" {
					t.Errorf("source coverage failure: %+v", diagnostic)
				}
			}
			sources := cadTestSourceIDs(chunks)
			if len(sources) != len(doc.Texts)-excluded {
				t.Errorf("source coverage=%d want=%d after explicit grid exclusion", len(sources), len(doc.Texts)-excluded)
			}
			for id, count := range sources {
				if count != 1 {
					t.Errorf("source %s emitted %d times", id, count)
				}
			}

			bodies := make([]string, 0, len(chunks))
			for _, chunk := range chunks {
				if strings.TrimSpace(chunk.Content) == "" {
					t.Fatal("empty body")
				}
				v, b := chunk.Viewport, chunk.Bounds
				if v[2] <= v[0] || v[3] <= v[1] {
					t.Errorf("unusable viewport: %s/%s %v", chunk.SheetID, chunk.RegionID, v)
				}
				const tolerance = 1e-6
				if b[0] < v[0]-tolerance || b[1] < v[1]-tolerance || b[2] > v[2]+tolerance || b[3] > v[3]+tolerance {
					t.Errorf("chunk outside render viewport: %s/%s bbox=%v viewport=%v", chunk.SheetID, chunk.RegionID, b, v)
				}
				bodies = append(bodies, chunk.Content)
			}
			all := strings.Join(bodies, "\n")
			for _, residue := range []string{"xqc;", "xqj;", "xsm0.7", "xb0.25", "xi1,ql;", "�"} {
				if strings.Contains(all, residue) {
					t.Errorf("format/encoding residue %q", residue)
				}
			}
			var want []string
			switch strings.TrimSuffix(entry.Name(), ext) {
			case "pulley_drive_3d":
				want = []string{"ISO 2768"}
			case "two_storey_house_3d":
				want = []string{"Interpiano"}
			case "apartment_floor_plan", "#apartment_floor_plan":
				want = []string{"SUPERFICI UTILI NETTE", "83,89", "Altezza interna netta 2,70 m"}
			}
			for _, text := range want {
				if !strings.Contains(all, text) {
					t.Errorf("missing source fact %q", text)
				}
			}
			if entry.Name() == "Flow_Instrument_Family.dwg" {
				paired := false
				for _, body := range bodies {
					if strings.Contains(body, "FC 095") && strings.Contains(body, "FLOW CONTROLLER (CP)") {
						paired = true
					}
				}
				if !paired {
					t.Error("FC 095 lost its legend description")
				}
			}
			if strings.Contains(entry.Name(), "VALVE_AND_CONTROL_VALVE") {
				if len(doc.Sheets) != 1 || doc.Sheets[0].Source != "fallback" {
					t.Errorf("local legend panel mistaken for model sheets: %+v", doc.Sheets)
				}
				for _, chunk := range chunks {
					if chunk.Kind == cadKindTitleBlock && strings.Contains(chunk.Content, "43\tROTARY") {
						t.Error("title block absorbed legend row")
					}
					if chunk.Kind == cadKindTitleBlock && !strings.Contains(chunk.Content, "VALVE AND CONTROL VALVE FAMILY") {
						t.Errorf("equipment labels misclassified as title block: %s", chunk.Content)
					}
				}
			}
			if entry.Name() == "EQUIPMENT_SYMBOLS_DRAWING.dwg" {
				found := false
				for _, chunk := range chunks {
					if strings.Contains(chunk.Content, "095I") && len(chunk.Keywords) > 0 {
						found = true
					}
				}
				if !found {
					t.Error("numeric equipment tag lost legend context, keywords or evidence")
				}
			}
			if entry.Name() == "DrawingSheet.dwg" {
				found := false
				for _, c := range chunks {
					if c.Kind == cadKindTitleBlock && strings.Contains(c.Content, "NAMA") {
						found = true
					}
				}
				if !found {
					t.Error("localized title fields not recognized")
				}
			}
			if strings.HasPrefix(entry.Name(), "pulley_drive_3d") {
				for _, c := range chunks {
					if c.Kind == cadKindTitleBlock && (strings.Contains(c.Content, "DISTINTA DEI PEZZI") || strings.Contains(c.Content, "ISO 2768")) {
						t.Error("title block absorbed BOM or notes")
					}
				}
			}

			short := 0
			for _, body := range bodies {
				if len(strings.Split(body, "\n")) <= 2 {
					short++
				}
			}
			t.Logf("chunks=%d short=%d diagnostics=%v", len(chunks), short, diagnostics)
			if dir := os.Getenv("CAD_REVIEW_OUTPUT"); dir != "" {
				raw, _ := json.MarshalIndent(map[string]any{"document": doc, "chunks": chunks, "diagnostics": diagnostics}, "", "  ")
				name := strings.ReplaceAll(rel, string(filepath.Separator), "_") + ".json"
				if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
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
