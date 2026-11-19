//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

//go:build ee

package handler

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ragflow/internal/permission"
	"ragflow/internal/service"

	"github.com/gin-gonic/gin"
)

// mockChunkVersionSvc pairs the shared chunk mock with the history methods this
// edition adds to the chunk service.
type mockChunkVersionSvc struct {
	mockChunkSvc

	listVersionsFn   func(context.Context, *service.ChunkVersionRequest) (*service.ListChunkVersionsResponse, error)
	getVersionFn     func(context.Context, *service.ChunkVersionRequest) (*service.GetChunkVersionResponse, error)
	diffVersionsFn   func(context.Context, *service.ChunkVersionDiffRequest) (*service.ChunkVersionDiffResponse, error)
	restoreVersionFn func(context.Context, *service.ChunkVersionRequest) (*service.RestoreChunkVersionResponse, error)
	versionImageFn   func(context.Context, *service.ChunkVersionRequest) ([]byte, error)
	deleteVersionFn  func(context.Context, *service.ChunkVersionRequest) (*service.DeleteChunkVersionResponse, error)
}

func (m *mockChunkVersionSvc) ListVersions(ctx context.Context, req *service.ChunkVersionRequest) (*service.ListChunkVersionsResponse, error) {
	return m.listVersionsFn(ctx, req)
}

func (m *mockChunkVersionSvc) GetVersion(ctx context.Context, req *service.ChunkVersionRequest) (*service.GetChunkVersionResponse, error) {
	return m.getVersionFn(ctx, req)
}

func (m *mockChunkVersionSvc) DiffVersions(ctx context.Context, req *service.ChunkVersionDiffRequest) (*service.ChunkVersionDiffResponse, error) {
	return m.diffVersionsFn(ctx, req)
}

func (m *mockChunkVersionSvc) RestoreVersion(ctx context.Context, req *service.ChunkVersionRequest) (*service.RestoreChunkVersionResponse, error) {
	return m.restoreVersionFn(ctx, req)
}

func (m *mockChunkVersionSvc) DeleteVersion(ctx context.Context, req *service.ChunkVersionRequest) (*service.DeleteChunkVersionResponse, error) {
	if m.deleteVersionFn == nil {
		return &service.DeleteChunkVersionResponse{ChunkID: req.ChunkID, DeletedVersion: req.Version}, nil
	}
	return m.deleteVersionFn(ctx, req)
}

func (m *mockChunkVersionSvc) VersionImage(ctx context.Context, req *service.ChunkVersionRequest) ([]byte, error) {
	if m.versionImageFn == nil {
		return nil, service.ErrChunkVersionImageNotFound
	}
	return m.versionImageFn(ctx, req)
}

func setupChunkVersionHandler(mock chunkService) (*gin.Engine, *ChunkHandler) {
	h := &ChunkHandler{chunkService: mock}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(permission.WithIdentity(c.Request.Context(), "user-1", ""))
	})
	return r, h
}

// TestChunkVersionRoutesBindPathParams pins that the version endpoints route
// dataset, document, chunk and version to the service and answer with its
// payload.
func TestChunkVersionRoutesBindPathParams(t *testing.T) {
	mock := &mockChunkVersionSvc{
		listVersionsFn: func(_ context.Context, req *service.ChunkVersionRequest) (*service.ListChunkVersionsResponse, error) {
			if req.DatasetID != "kb-1" || req.DocumentID != "doc-1" || req.ChunkID != "chunk-1" {
				t.Fatalf("unexpected list target: %#v", req)
			}
			return &service.ListChunkVersionsResponse{
				ChunkID:  req.ChunkID,
				Total:    2,
				Versions: []service.ChunkVersionSummary{{Version: 1, Latest: true}, {Version: 0}},
			}, nil
		},
		getVersionFn: func(_ context.Context, req *service.ChunkVersionRequest) (*service.GetChunkVersionResponse, error) {
			if req.Version != 1 {
				t.Fatalf("version = %d, want 1", req.Version)
			}
			return &service.GetChunkVersionResponse{
				Version: req.Version,
				Latest:  true,
				Chunk:   map[string]interface{}{"content_with_weight": "v1 body"},
			}, nil
		},
		diffVersionsFn: func(_ context.Context, req *service.ChunkVersionDiffRequest) (*service.ChunkVersionDiffResponse, error) {
			if req.DatasetID != "kb-1" || req.DocumentID != "doc-1" || req.ChunkID != "chunk-1" {
				t.Fatalf("unexpected diff target: %#v", req)
			}
			if req.From != 0 || req.To == nil || *req.To != 1 {
				t.Fatalf("diff range = %d..%v, want 0..1", req.From, req.To)
			}
			return &service.ChunkVersionDiffResponse{
				ChunkID: req.ChunkID,
				From:    0,
				To:      1,
				Fields: []service.ChunkVersionFieldDiff{{
					Field: "content_with_weight",
					Kind:  service.ChunkVersionDiffText,
					Diff:  "@@ -1,1 +1,1 @@\n-v0 body\n+v1 body\n",
				}},
			}, nil
		},
		restoreVersionFn: func(_ context.Context, req *service.ChunkVersionRequest) (*service.RestoreChunkVersionResponse, error) {
			if req.Version != 0 {
				t.Fatalf("version = %d, want 0", req.Version)
			}
			return &service.RestoreChunkVersionResponse{Version: 2}, nil
		},
	}

	r, h := setupChunkVersionHandler(mock)
	r.GET("/api/v1/datasets/:dataset_id/documents/:document_id/chunks/:chunk_id/versions", h.ListChunkVersions)
	r.GET("/api/v1/datasets/:dataset_id/documents/:document_id/chunks/:chunk_id/versions/diff", h.DiffChunkVersions)
	r.GET("/api/v1/datasets/:dataset_id/documents/:document_id/chunks/:chunk_id/versions/:version", h.GetChunkVersion)
	r.POST("/api/v1/datasets/:dataset_id/documents/:document_id/chunks/:chunk_id/versions/:version/restore", h.RestoreChunkVersion)

	base := "/api/v1/datasets/kb-1/documents/doc-1/chunks/chunk-1/versions"
	for _, tc := range []struct {
		method string
		path   string
		code   int
	}{
		{http.MethodGet, base, http.StatusOK},
		{http.MethodGet, base + "/1", http.StatusOK},
		{http.MethodGet, base + "/diff?from=0&to=1", http.StatusOK},
		{http.MethodGet, base + "/diff?from=x", http.StatusBadRequest},
		{http.MethodGet, base + "/diff?from=0&to=-1", http.StatusBadRequest},
		{http.MethodPost, base + "/0/restore", http.StatusOK},
		{http.MethodGet, base + "/not-a-number", http.StatusBadRequest},
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, nil)
		r.ServeHTTP(w, req)
		if w.Code != tc.code {
			t.Fatalf("%s %s: status = %d, want %d (%s)", tc.method, tc.path, w.Code, tc.code, w.Body.String())
		}
	}
}

// TestChunkVersionDiffMissingVersionAnswers404 pins that a comparison naming a
// version the history does not hold is a client error, not a server error.
func TestChunkVersionDiffMissingVersionAnswers404(t *testing.T) {
	mock := &mockChunkVersionSvc{
		diffVersionsFn: func(context.Context, *service.ChunkVersionDiffRequest) (*service.ChunkVersionDiffResponse, error) {
			return nil, fmt.Errorf("%w: version 9 does not exist", service.ErrChunkVersionNotFound)
		},
	}

	r, h := setupChunkVersionHandler(mock)
	r.GET("/api/v1/datasets/:dataset_id/documents/:document_id/chunks/:chunk_id/versions/diff", h.DiffChunkVersions)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/kb-1/documents/doc-1/chunks/chunk-1/versions/diff?from=9", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (%s)", w.Code, http.StatusNotFound, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "version 9") {
		t.Fatalf("body = %s, want the missing version named", body)
	}
}

// TestChunkVersionImageServesStoredBytes pins that the version image endpoint
// answers with the picture itself and its type, so a browser can render the
// picture an old version recorded.
func TestChunkVersionImageServesStoredBytes(t *testing.T) {
	picture := []byte("\x89PNG\r\n\x1a\nold picture")
	mock := &mockChunkVersionSvc{
		versionImageFn: func(_ context.Context, req *service.ChunkVersionRequest) ([]byte, error) {
			if req.DatasetID != "kb-1" || req.DocumentID != "doc-1" || req.ChunkID != "chunk-1" || req.Version != 2 {
				t.Fatalf("unexpected image target: %#v", req)
			}
			return picture, nil
		},
	}

	r, h := setupChunkVersionHandler(mock)
	r.GET("/api/v1/datasets/:dataset_id/documents/:document_id/chunks/:chunk_id/versions/:version/image", h.GetChunkVersionImage)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/kb-1/documents/doc-1/chunks/chunk-1/versions/2/image", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", w.Code, http.StatusOK, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("content type = %q, want image/png", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache control = %q, want no-store", got)
	}
	if body := w.Body.Bytes(); !bytes.Equal(body, picture) {
		t.Fatalf("body = %q, want the stored picture", body)
	}
}

// TestChunkVersionImageMissingAnswers404 pins that both "the version recorded
// no picture" and "the version does not exist" are client errors: the caller
// asked for something no retry can produce.
func TestChunkVersionImageMissingAnswers404(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "no picture", err: service.ErrChunkVersionImageNotFound},
		{name: "no version", err: fmt.Errorf("%w: version 9 does not exist", service.ErrChunkVersionNotFound)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockChunkVersionSvc{
				versionImageFn: func(context.Context, *service.ChunkVersionRequest) ([]byte, error) {
					return nil, tc.err
				},
			}

			r, h := setupChunkVersionHandler(mock)
			r.GET("/api/v1/datasets/:dataset_id/documents/:document_id/chunks/:chunk_id/versions/:version/image", h.GetChunkVersionImage)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/kb-1/documents/doc-1/chunks/chunk-1/versions/9/image", nil)
			r.ServeHTTP(w, req)
			if w.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d (%s)", w.Code, http.StatusNotFound, w.Body.String())
			}
		})
	}
}

// TestChunkVersionImageRejectsMalformedVersion pins that the route answers a
// non-numeric version before the service is consulted.
func TestChunkVersionImageRejectsMalformedVersion(t *testing.T) {
	mock := &mockChunkVersionSvc{
		versionImageFn: func(context.Context, *service.ChunkVersionRequest) ([]byte, error) {
			t.Fatalf("service must not be called for a malformed version")
			return nil, nil
		},
	}

	r, h := setupChunkVersionHandler(mock)
	r.GET("/api/v1/datasets/:dataset_id/documents/:document_id/chunks/:chunk_id/versions/:version/image", h.GetChunkVersionImage)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets/kb-1/documents/doc-1/chunks/chunk-1/versions/v2/image", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
}
