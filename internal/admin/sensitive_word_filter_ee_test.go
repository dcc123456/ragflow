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

package admin

import (
	"bytes"
	"encoding/json"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/compliance"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/storage"

	"github.com/gin-gonic/gin"
)

func uploadWordList(t *testing.T, engine http.Handler, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/sensitive-words-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

func TestSensitiveWordFileEndpoints(t *testing.T) {
	engine := setupWhitelistTest(t)
	factory := storage.GetStorageFactory()
	previous := factory.GetStorage()
	factory.SetStorage(storage.NewMemoryStorage())
	t.Cleanup(func() { factory.SetStorage(previous) })
	download := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/admin/sensitive-words-file", nil))
		return w
	}
	if w := download(); w.Code != http.StatusNotFound {
		t.Fatalf("missing download = %d: %s", w.Code, w.Body.String())
	}
	filename := "敏感词.TXT"
	content := []byte("\ufeffblocked\r\n\n restricted phrase \nblocked\n")
	w := uploadWordList(t, engine, filename, content)
	var result struct {
		Code common.ErrorCode `json:"code"`
		Data struct {
			Filename  string `json:"filename"`
			Size      int    `json:"size"`
			WordCount int    `json:"word_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || result.Code != common.CodeSuccess || result.Data.Filename != filename || result.Data.Size != len(content) || result.Data.WordCount != 3 {
		t.Fatalf("upload = %d: %s", w.Code, w.Body.String())
	}
	var first entity.SystemSettings
	if err := dao.DB.Where("name = ?", compliance.FileVersionSetting).Take(&first).Error; err != nil || first.Value == "" {
		t.Fatalf("version = %+v, %v", first, err)
	}
	w = download()
	_, params, err := mime.ParseMediaType(w.Header().Get("Content-Disposition"))
	if err != nil || params["filename"] != filename || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" || !bytes.Equal(w.Body.Bytes(), content) {
		t.Fatalf("download headers = %v, body = %q, err = %v", w.Header(), w.Body.Bytes(), err)
	}
	w = uploadWordList(t, engine, "new.txt", []byte("replacement"))
	if w.Code != http.StatusOK {
		t.Fatalf("replacement = %s", w.Body.String())
	}
	var next entity.SystemSettings
	if err := dao.DB.Where("name = ?", compliance.FileVersionSetting).Take(&next).Error; err != nil || next.Value == first.Value {
		t.Fatalf("version did not change: %+v, %v", next, err)
	}
	if w := download(); w.Body.String() != "replacement" {
		t.Fatalf("replacement download = %q", w.Body.String())
	}
	for _, tc := range []struct {
		name     string
		filename string
		content  []byte
	}{
		{"extension", "words.csv", []byte("blocked")},
		{"encoding", "words.txt", []byte{0xff}},
		{"size", "words.txt", bytes.Repeat([]byte("x"), compliance.MaxFileSize+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := uploadWordList(t, engine, tc.filename, tc.content)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("invalid upload = %d: %s", w.Code, w.Body.String())
			}
			if w := download(); w.Body.String() != "replacement" {
				t.Fatalf("invalid upload replaced stored content: %q", w.Body.String())
			}
		})
	}
	h := &Handler{}
	protected := gin.New()
	RegisterEERouter(protected.Group("/api/v1/admin", h.AuthMiddleware()), NewRouter(h))
	for _, method := range []string{http.MethodPost, http.MethodGet} {
		w := httptest.NewRecorder()
		protected.ServeHTTP(w, httptest.NewRequest(method, "/api/v1/admin/sensitive-words-file", nil))
		var result struct {
			Code common.ErrorCode `json:"code"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Code != common.CodeUnauthorized {
			t.Fatalf("unauthenticated request = %s, %v", w.Body.String(), err)
		}
	}
}
