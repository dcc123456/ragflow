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
	"context"
	"encoding/json"
	"image"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	deepdoctype "ragflow/internal/deepdoc/parser/type"
)

// ocrRecWire mirrors the flat /predict/ocr_rec response element decoded by
// both OCRRecognize and OCRRecognizeBatch.
type ocrRecWire struct {
	Text  string  `json:"text"`
	Score float64 `json:"score"`
}

// serveOCRRec returns a stub that answers every /predict/ocr_rec POST with the
// same reply and counts the requests it saw. The count lets a consistency test
// also pin that the single-crop path still costs exactly one round-trip.
func serveOCRRec(t *testing.T, reply []ocrRecWire) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/predict/ocr_rec" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		calls.Add(1)
		if err := json.NewEncoder(w).Encode(reply); err != nil {
			t.Errorf("encode reply: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// TestClient_OCRRecognize_MatchesBatch_EmptyResponse is adversarial for the
// single/batch equivalence: when the service returns no items, OCRRecognize has
// always meant "no text" (nil, nil). A one-crop batch must mean the same, or the
// parser's per-crop fallback changes the result it produces for the same crop.
// Currently OCRRecognizeBatch turns this into a count-mismatch error -> RED.
func TestClient_OCRRecognize_MatchesBatch_EmptyResponse(t *testing.T) {
	srv, _ := serveOCRRec(t, []ocrRecWire{})
	c := New(srv.URL)
	ctx := context.Background()
	img := testImage(t)

	single, errSingle := c.OCRRecognize(ctx, img)
	batch, errBatch := c.OCRRecognizeBatch(ctx, []image.Image{img})

	if (errSingle == nil) != (errBatch == nil) {
		t.Fatalf("single/batch disagree on error for empty response: single err=%v, batch err=%v", errSingle, errBatch)
	}
	if errSingle != nil {
		return
	}
	if len(batch) != 1 || len(batch[0]) != len(single) {
		t.Fatalf("batch %v does not mirror single %v", batch, single)
	}
}

// TestClient_OCRRecognize_MatchesBatch_SurplusResults is the opposite adversarial
// fixture: the service returns MORE items than crops. OCRRecognize silently takes
// items[0]; OCRRecognizeBatch rejects the mismatch. They must agree so a fallback
// cannot alter behavior. Currently only the batch method errors -> RED.
func TestClient_OCRRecognize_MatchesBatch_SurplusResults(t *testing.T) {
	srv, _ := serveOCRRec(t, []ocrRecWire{
		{Text: "first", Score: 0.9},
		{Text: "second", Score: 0.8},
	})
	c := New(srv.URL)
	ctx := context.Background()
	img := testImage(t)

	_, errSingle := c.OCRRecognize(ctx, img)
	_, errBatch := c.OCRRecognizeBatch(ctx, []image.Image{img})

	if (errSingle == nil) != (errBatch == nil) {
		t.Fatalf("single/batch disagree on error for surplus results: single err=%v, batch err=%v", errSingle, errBatch)
	}
}

// ── regression guards: the equivalence must hold on the normal paths too ──

func TestClient_OCRRecognize_MatchesBatch_Happy(t *testing.T) {
	srv, calls := serveOCRRec(t, []ocrRecWire{{Text: "hello", Score: 0.95}})
	c := New(srv.URL)
	ctx := context.Background()
	img := testImage(t)

	single, err := c.OCRRecognize(ctx, img)
	if err != nil {
		t.Fatalf("OCRRecognize: %v", err)
	}
	batch, err := c.OCRRecognizeBatch(ctx, []image.Image{img})
	if err != nil {
		t.Fatalf("OCRRecognizeBatch: %v", err)
	}
	if len(single) != 1 || len(batch) != 1 || len(batch[0]) != 1 {
		t.Fatalf("single=%v batch=%v", single, batch)
	}
	if single[0] != batch[0][0] {
		t.Fatalf("single[0]=%+v batch[0][0]=%+v", single[0], batch[0][0])
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("expected exactly one request per call (2 total), got %d", n)
	}
}

func TestClient_OCRRecognize_MatchesBatch_DropScore(t *testing.T) {
	srv, _ := serveOCRRec(t, []ocrRecWire{{Text: "hidden", Score: 0.1}})
	c := New(srv.URL, WithDropScore(0.5))
	ctx := context.Background()
	img := testImage(t)

	single, err := c.OCRRecognize(ctx, img)
	if err != nil {
		t.Fatalf("OCRRecognize: %v", err)
	}
	batch, err := c.OCRRecognizeBatch(ctx, []image.Image{img})
	if err != nil {
		t.Fatalf("OCRRecognizeBatch: %v", err)
	}
	var want deepdoctype.OCRText = single[0]
	if len(batch) != 1 || len(batch[0]) != 1 || batch[0][0] != want {
		t.Fatalf("batch=%v want single=%+v", batch, want)
	}
	if want.Text != "" || want.Confidence != 0.1 {
		t.Fatalf("drop_score not applied consistently: %+v", want)
	}
}
