//go:build integration && ee

// Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package evaluation exercises a real API without the parent suite's model
// provisioning. It creates and removes only its own collection/chat fixtures.
package evaluation

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

const evaluationPath = "/api/v1/evaluations"

type object = map[string]any

type apiClient struct {
	host, key string
}

type response struct {
	status int
	header http.Header
	body   []byte
	code   *int
	data   object
}

func client(t *testing.T) apiClient {
	t.Helper()
	host := strings.TrimRight(os.Getenv("HOST_ADDRESS"), "/")
	if host == "" {
		host = "http://127.0.0.1:9380"
	}
	key := os.Getenv("RAGFLOW_TEST_API_KEY")
	if key == "" {
		t.Fatal("RAGFLOW_TEST_API_KEY must identify a disposable test tenant")
	}
	return apiClient{host: host, key: key}
}

func (c apiClient) request(t *testing.T, method, path, contentType string, body []byte) response {
	t.Helper()
	// Cleanup runs after the test context is cancelled and still needs a bound.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, c.host+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal("construct API request:", err)
	}
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %s", method, path, strings.ReplaceAll(err.Error(), c.key, "[redacted]"))
	}
	defer r.Body.Close()
	b, err := io.ReadAll(io.LimitReader(r.Body, 8<<20+1))
	if err != nil || len(b) > 8<<20 {
		t.Fatalf("%s %s: invalid or oversized response", method, path)
	}
	var envelope struct {
		Code *int   `json:"code"`
		Data object `json:"data"`
	}
	_ = json.Unmarshal(b, &envelope)
	return response{status: r.StatusCode, header: r.Header, body: b, code: envelope.Code, data: envelope.Data}
}

func (c apiClient) json(t *testing.T, method, path string, body any) response {
	t.Helper()
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	return c.request(t, method, path, "application/json", encoded)
}

func requireOK(t *testing.T, r response) object {
	t.Helper()
	if r.status < 200 || r.status >= 300 || r.code == nil || *r.code != 0 {
		code := "missing"
		if r.code != nil {
			code = fmt.Sprint(*r.code)
		}
		t.Fatalf("expected successful JSON envelope; HTTP=%d code=%s content-type=%q", r.status, code, r.header.Get("Content-Type"))
	}
	if r.header.Get("X-API-Source") != "go" {
		t.Fatal("response was not handled by Go")
	}
	return r.data
}

func requireRejected(t *testing.T, r response) {
	t.Helper()
	if r.code == nil || *r.code == 0 || r.status >= 500 {
		t.Fatalf("expected application rejection, got HTTP=%d JSON code=%v", r.status, r.code)
	}
}

func requireValidationError(t *testing.T, r response) {
	t.Helper()
	requireRejected(t, r)
	if r.status == http.StatusNotFound || *r.code == http.StatusNotFound {
		t.Fatal("missing route is not a validation error")
	}
}

func id(t *testing.T, data object) string {
	t.Helper()
	value, _ := data["id"].(string)
	if value == "" {
		t.Fatal("resource has no id")
	}
	return value
}

func items(t *testing.T, data object, key string) []any {
	t.Helper()
	value, ok := data[key].([]any)
	if !ok {
		t.Fatalf("%s is not an array", key)
	}
	return value
}

func (c apiClient) upload(t *testing.T, name string, content []byte) response {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteField("name", fmt.Sprintf("evaluation_contract_%d", time.Now().UnixNano())); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return c.request(t, http.MethodPost, evaluationPath+"/collections", w.FormDataContentType(), body.Bytes())
}

func (c apiClient) collection(t *testing.T, name string, content []byte) string {
	t.Helper()
	r := c.upload(t, name, content)
	// Capture fixture ownership before validating the remaining response fields.
	collectionID, _ := r.data["collection_id"].(string)
	if collectionID == "" {
		collectionID, _ = r.data["id"].(string)
	}
	if collectionID != "" {
		t.Cleanup(func() {
			requireOK(t, c.json(t, http.MethodDelete, evaluationPath+"/collections/"+collectionID, nil))
		})
	}
	requireOK(t, r)
	if collectionID == "" {
		t.Fatal("upload response has no collection id")
	}
	return collectionID
}

func (c apiClient) cases(t *testing.T, collectionID string) []any {
	t.Helper()
	data := requireOK(t, c.json(t, http.MethodGet, evaluationPath+"/collections/"+collectionID+"/cases", nil))
	return items(t, data, "cases")
}
