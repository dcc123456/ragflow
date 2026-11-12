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
	"mime/multipart"
	"reflect"
	"testing"

	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
	"ragflow/internal/storage"
)

func TestWPSUploadLocalDocuments(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	factory := storage.GetStorageFactory()
	originalStorage := factory.GetStorage()
	factory.SetStorage(newFakeUploadStorage())
	t.Cleanup(func() { factory.SetStorage(originalStorage) })
	seedDocumentParserTenant(t, "tenant-1", "user-1")
	config := generalParserConfig()
	setups := config["Parser:HipSignsRhyme"].(map[string]any)
	setups["doc"] = map[string]any{}
	setups["spreadsheet"] = map[string]any{}
	kb := &entity.Knowledgebase{
		ID: "kb-wps", TenantID: "tenant-1", Name: "WPS", CreatedBy: "user-1",
		ParserID: "general", ParserConfig: config,
	}
	if err := dao.DB.Create(kb).Error; err != nil {
		t.Fatal(err)
	}
	ctx := permission.WithIdentity(t.Context(), "user-1", "")
	cases := []struct {
		name     string
		parserID string
		family   string
	}{
		{"document.wps", "general", "doc"}, {"template.wpt", "general", "doc"},
		{"book.et", "general", "spreadsheet"}, {"template.ett", "general", "spreadsheet"},
		{"deck.dps", "presentation", "slides"}, {"DECK.DPT", "presentation", "slides"},
	}
	var files []*multipart.FileHeader
	for _, tc := range cases {
		files = append(files, makeTestFileHeader(t, "file", tc.name, []byte("upload routing fixture")))
	}
	got, errs := testDocumentService(t).UploadLocalDocuments(ctx, kb, files, "", nil)
	if len(errs) != 0 || len(got) != len(cases) {
		t.Fatalf("uploaded %d/%d files, errors: %v", len(got), len(cases), errs)
	}
	for i, tc := range cases {
		if got[i]["parser_id"] != tc.parserID || got[i]["type"] != "doc" {
			t.Errorf("%s: parser=%v type=%v, want %s/doc", tc.name, got[i]["parser_id"], got[i]["type"], tc.parserID)
		}
		parserFamilySetup(t, got[i], tc.family)
	}
}

func TestWPSPresentationKeepsDatasetParserDeclaringSlides(t *testing.T) {
	config := presentationParserConfig()
	kb := &entity.Knowledgebase{ParserID: "table", ParserConfig: config}
	for _, name := range []string{"deck.dps", "deck.dpt"} {
		selection, got := resolveDocumentParser(t.Context(), kb, name, "doc", config)
		if selection.ParserID != "table" || !reflect.DeepEqual(got, config) {
			t.Fatalf("%s: dataset parser/config replaced despite declaring slides", name)
		}
	}
}
