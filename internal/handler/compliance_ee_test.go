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

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/compliance"
	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"
	"ragflow/internal/storage"
	"ragflow/internal/tokenizer"

	"github.com/gin-gonic/gin"
)

func TestSensitiveQuestionsReturnEmptySearchResults(t *testing.T) {
	db := testutil.SetupTestDB(t, &entity.SystemSettings{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))
	if err := db.Create(&entity.SystemSettings{Name: compliance.EnabledSetting, Source: "variable", DataType: "bool", Value: "true"}).Error; err != nil {
		t.Fatal(err)
	}
	factory := storage.GetStorageFactory()
	previous := factory.GetStorage()
	store := storage.NewMemoryStorage()
	factory.SetStorage(store)
	t.Cleanup(func() { factory.SetStorage(previous) })
	if err := store.Put(t.Context(), compliance.Bucket, compliance.ObjectKey, []byte("restricted")); err != nil {
		t.Fatal(err)
	}
	tokenizer.SetEngineType("infinity")
	t.Cleanup(func() { tokenizer.SetEngineType("") })
	mode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(mode) })
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user", &entity.User{ID: "user"}) })
	datasets := &DatasetsHandler{}
	r.POST("/datasets/search", datasets.SearchDatasets)
	r.POST("/datasets/:dataset_id/search", datasets.SearchDataset)
	r.POST("/retrieval", (&SearchBotHandler{}).RetrievalTest)
	r.POST("/dify", (&DifyRetrievalHandler{}).Retrieval)
	for _, tc := range []struct {
		path string
		body string
	}{
		{"/datasets/search", `{"question":"restricted","dataset_ids":["dataset"]}`},
		{"/datasets/dataset/search", `{"question":"restricted"}`},
		{"/retrieval", `{"question":"restricted","kb_ids":["dataset"]}`},
		{"/dify", `{"query":"restricted","knowledge_id":"dataset"}`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			var result map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusOK {
				t.Fatalf("filtered result = %d: %s", w.Code, w.Body.String())
			}
			if result["message"] != compliance.DefaultResponse {
				t.Fatalf("expected configured rejection message: %s", w.Body.String())
			}
			if result["code"] != float64(common.CodeSuccess) {
				t.Fatalf("expected success: %s", w.Body.String())
			}
			data, ok := result["data"].(map[string]interface{})
			if !ok {
				t.Fatalf("expected result data: %s", w.Body.String())
			}
			if tc.path == "/dify" {
				if records, ok := data["records"].([]interface{}); !ok || len(records) != 0 {
					t.Fatalf("expected empty records: %s", w.Body.String())
				}
				return
			}
			if data["total"] != float64(0) {
				t.Fatalf("expected zero results: %s", w.Body.String())
			}
			if data["labels"] != nil {
				t.Fatalf("expected no labels: %s", w.Body.String())
			}
			for _, field := range []string{"chunks", "doc_aggs"} {
				if items, ok := data[field].([]interface{}); !ok || len(items) != 0 {
					t.Fatalf("expected empty %s: %s", field, w.Body.String())
				}
			}
		})
	}
}
