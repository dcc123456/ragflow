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

package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// ocrRecWireItem mirrors the standalone service's flat /predict/ocr_rec
// response element: {"text": ..., "score": ...}.
type ocrRecWireItem struct {
	Text  string  `json:"text"`
	Score float64 `json:"score"`
}

// batchServerState records every request the client makes so tests can pin the
// single-request contract (the bug was one POST per text line, N+1 HTTP).
type batchServerState struct {
	mu     sync.Mutex
	calls  int
	paths  []string
	images [][]string
}

func (s *batchServerState) snapshot() (int, []string, [][]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, append([]string(nil), s.paths...), append([][]string(nil), s.images...)
}

// newBatchRecServer returns a /predict/ocr_rec stub that records the request
// body and responds with respond(numberOfImages).
func newBatchRecServer(t *testing.T, state *batchServerState, respond func(int) []ocrRecWireItem) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			return
		}
		var req struct {
			Images []string `json:"images"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("request body is not JSON: %v", err)
		}
		state.mu.Lock()
		state.calls++
		state.paths = append(state.paths, r.URL.Path)
		state.images = append(state.images, req.Images)
		n := len(req.Images)
		state.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(respond(n)); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
}

// batchTestImage returns a deterministic, non-uniform image. Distinct seeds
// produce distinct JPEG payloads, so the test can detect dedup/reordering.
func batchTestImage(seed int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 24, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 24; x++ {
			img.Set(x, y, color.RGBA{R: uint8(seed*37 + x), G: uint8(y * 21), B: uint8(seed * 11), A: 255})
		}
	}
	return img
}

// TestClient_OCRRecognizeBatch_OneRequestOrderedAndJPEG is the core contract:
// N crops go out in ONE POST, and results come back in input order, one
// OCRText per crop. A per-crop (N+1 HTTP) implementation sends N requests and
// fails the `calls == 1` assertion; a naive reordering fails the text checks.
func TestClient_OCRRecognizeBatch_OneRequestOrderedAndJPEG(t *testing.T) {
	want := []ocrRecWireItem{
		{Text: "alpha", Score: 0.91},
		{Text: "beta", Score: 0.82},
		{Text: "alpha", Score: 0.73}, // duplicate result text must not be collapsed
		{Text: "应急管理 120111T", Score: 0.99},
	}
	state := &batchServerState{}
	srv := newBatchRecServer(t, state, func(int) []ocrRecWireItem { return want })
	defer srv.Close()

	imgs := []image.Image{
		batchTestImage(1), batchTestImage(2), batchTestImage(3), batchTestImage(4),
	}
	got, err := New(srv.URL).OCRRecognizeBatch(context.Background(), imgs)
	if err != nil {
		t.Fatalf("OCRRecognizeBatch: %v", err)
	}

	calls, paths, batches := state.snapshot()
	if calls != 1 {
		t.Fatalf("HTTP calls = %d, want 1: crops must be recognized in a single POST", calls)
	}
	if len(paths) != 1 || paths[0] != "/predict/ocr_rec" {
		t.Fatalf("paths = %v, want [/predict/ocr_rec]", paths)
	}
	if len(batches) != 1 || len(batches[0]) != len(imgs) {
		t.Fatalf("request images = %v, want %d entries in one request", batches, len(imgs))
	}
	// Compare against the shared encoder as an order oracle: reversing or
	// deduplicating the crops before encoding would fail here, even though
	// the stub's response is fixed by index.
	for i, b64 := range batches[0] {
		if want := encodeImageB64(imgs[i]); b64 != want {
			t.Errorf("request image %d does not match input crop %d (reorder/dedup corruption)", i, i)
		}
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			t.Fatalf("image %d is not base64: %v", i, err)
		}
		if _, err := jpeg.Decode(bytes.NewReader(raw)); err != nil {
			t.Fatalf("image %d is not JPEG (PNG breaks thin-separator detection): %v", i, err)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if len(got[i]) != 1 {
			t.Fatalf("row %d has %d OCRText entries, want exactly 1", i, len(got[i]))
		}
		if got[i][0].Text != want[i].Text {
			t.Errorf("row %d text = %q, want %q (result order must follow input order)", i, got[i][0].Text, want[i].Text)
		}
		if got[i][0].Confidence != want[i].Score {
			t.Errorf("row %d confidence = %v, want %v", i, got[i][0].Confidence, want[i].Score)
		}
	}
}

// TestClient_OCRRecognizeBatch_EmptyMakesNoRequest pins the empty-input edge:
// no HTTP call and a nil result (mirroring the in-process analyzer).
func TestClient_OCRRecognizeBatch_EmptyMakesNoRequest(t *testing.T) {
	state := &batchServerState{}
	srv := newBatchRecServer(t, state, func(int) []ocrRecWireItem { return nil })
	defer srv.Close()

	got, err := New(srv.URL).OCRRecognizeBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("empty batch: %v", err)
	}
	if got != nil {
		t.Fatalf("empty batch result = %v, want nil", got)
	}
	if calls, _, _ := state.snapshot(); calls != 0 {
		t.Fatalf("empty batch sent %d HTTP requests, want 0", calls)
	}
}

// TestClient_OCRRecognizeBatch_SingleElement is the boundary case: one crop
// still takes the batch path (one request, one result).
func TestClient_OCRRecognizeBatch_SingleElement(t *testing.T) {
	state := &batchServerState{}
	srv := newBatchRecServer(t, state, func(int) []ocrRecWireItem {
		return []ocrRecWireItem{{Text: "only", Score: 0.88}}
	})
	defer srv.Close()

	got, err := New(srv.URL).OCRRecognizeBatch(context.Background(), []image.Image{batchTestImage(1)})
	if err != nil {
		t.Fatalf("single crop: %v", err)
	}
	calls, _, batches := state.snapshot()
	if calls != 1 || len(batches) != 1 || len(batches[0]) != 1 {
		t.Fatalf("single crop: calls=%d batches=%v, want exactly one image in one request", calls, batches)
	}
	if len(got) != 1 || got[0][0].Text != "only" || got[0][0].Confidence != 0.88 {
		t.Fatalf("single crop result = %v", got)
	}
}

// TestClient_OCRRecognizeBatch_DropScoreBlanksTextKeepsScore is adversarial
// for the coupled {text,score} field pair: below-threshold text must be blanked
// while the real confidence is preserved, and above-threshold text survives.
func TestClient_OCRRecognizeBatch_DropScoreBlanksTextKeepsScore(t *testing.T) {
	state := &batchServerState{}
	srv := newBatchRecServer(t, state, func(int) []ocrRecWireItem {
		return []ocrRecWireItem{
			{Text: "hidden", Score: 0.40},
			{Text: "visible", Score: 0.95},
		}
	})
	defer srv.Close()

	c := New(srv.URL, WithDropScore(0.5))
	got, err := c.OCRRecognizeBatch(context.Background(), []image.Image{batchTestImage(1), batchTestImage(2)})
	if err != nil {
		t.Fatalf("OCRRecognizeBatch: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}
	if got[0][0].Text != "" || got[0][0].Confidence != 0.40 {
		t.Fatalf("low-score row = %+v, want blanked text with confidence 0.40 preserved", got[0][0])
	}
	if got[1][0].Text != "visible" || got[1][0].Confidence != 0.95 {
		t.Fatalf("high-score row = %+v, want text kept", got[1][0])
	}
}

// TestClient_OCRRecognizeBatch_ResultCountMismatch pins the error path when
// the service returns a different number of results than input crops: fail
// loudly instead of panicking or silently dropping rows.
func TestClient_OCRRecognizeBatch_ResultCountMismatch(t *testing.T) {
	tests := []struct {
		name    string
		results []ocrRecWireItem
	}{
		{name: "fewer results", results: []ocrRecWireItem{{Text: "one", Score: 0.9}}},
		{name: "more results", results: []ocrRecWireItem{
			{Text: "one", Score: 0.9}, {Text: "two", Score: 0.9}, {Text: "three", Score: 0.9},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &batchServerState{}
			srv := newBatchRecServer(t, state, func(int) []ocrRecWireItem { return tt.results })
			defer srv.Close()

			_, err := New(srv.URL).OCRRecognizeBatch(context.Background(), []image.Image{
				batchTestImage(1), batchTestImage(2),
			})
			if err == nil {
				t.Fatal("want error on result/image count mismatch, got nil")
			}
			if !strings.Contains(err.Error(), "results for") {
				t.Fatalf("error %q should report the count mismatch", err.Error())
			}
		})
	}
}

// TestClient_OCRRecognizeBatch_MalformedJSON pins the decode error path.
func TestClient_OCRRecognizeBatch_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-an-array"))
	}))
	defer srv.Close()

	if _, err := New(srv.URL).OCRRecognizeBatch(context.Background(), []image.Image{batchTestImage(1)}); err == nil {
		t.Fatal("want error on malformed response body")
	}
}

// TestClient_OCRRecognizeBatch_ServerError pins that a non-2xx response is
// surfaced as an error (no partial results).
func TestClient_OCRRecognizeBatch_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	if _, err := New(srv.URL).OCRRecognizeBatch(context.Background(), []image.Image{batchTestImage(1)}); err == nil {
		t.Fatal("want error on 500")
	}
}

// TestClient_OCRRecognizeBatch_ContextCancelled pins that a cancelled caller
// aborts the batch instead of returning results.
func TestClient_OCRRecognizeBatch_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(srv.URL).OCRRecognizeBatch(ctx, []image.Image{batchTestImage(1)}); err == nil {
		t.Fatal("want error on cancelled context")
	}
}

// BenchmarkClient_OCRRecognizeBatch guards the performance contract: the
// whole point of this method is one HTTP round-trip for N crops.
func BenchmarkClient_OCRRecognizeBatch(b *testing.B) {
	const n = 32
	items := make([]ocrRecWireItem, n)
	for i := range items {
		items[i] = ocrRecWireItem{Text: fmt.Sprintf("line-%d", i), Score: 0.9}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(items)
	}))
	defer srv.Close()

	c := New(srv.URL)
	imgs := make([]image.Image, n)
	for i := range imgs {
		imgs[i] = image.NewRGBA(image.Rect(0, 0, 64, 16))
	}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.OCRRecognizeBatch(ctx, imgs); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkClient_OCRRecognizeBatch_PerCrop is the baseline the batch path
// replaces: N individual POSTs. Keeping it here makes the speedup explicit.
func BenchmarkClient_OCRRecognizeBatch_PerCrop(b *testing.B) {
	const n = 32
	items := []ocrRecWireItem{{Text: "line", Score: 0.9}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(items)
	}))
	defer srv.Close()

	c := New(srv.URL)
	imgs := make([]image.Image, n)
	for i := range imgs {
		imgs[i] = image.NewRGBA(image.Rect(0, 0, 64, 16))
	}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, img := range imgs {
			if _, err := c.OCRRecognize(ctx, img); err != nil {
				b.Fatal(err)
			}
		}
	}
}
