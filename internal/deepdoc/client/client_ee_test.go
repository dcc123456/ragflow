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
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	deepdoctype "ragflow/internal/deepdoc/parser/type"
)

func readAll(r *http.Request) ([]byte, error) { return io.ReadAll(r.Body) }

// ── helpers ──

func testImage(t *testing.T) image.Image {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for i := range img.Pix {
		img.Pix[i] = uint8(i)
	}
	return img
}

func encodeImage(t *testing.T, img image.Image) string {
	t.Helper()
	var buf strings.Builder
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString([]byte(buf.String()))
}

// assertImagePayload decodes the JSON request body and confirms it carries a
// base64 image (so the client really sent something decodable).
func assertImagePayload(t *testing.T, body []byte) {
	t.Helper()
	var req struct {
		Image  string   `json:"image"`
		Images []string `json:"images"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("request body not JSON: %v", err)
	}
	if req.Image == "" && len(req.Images) == 0 {
		t.Fatalf("request carried neither 'image' nor 'images'")
	}
}

// ── DLA ──

func TestClient_DLA_Happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/predict/dla" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		body, _ := readAll(r)
		assertImagePayload(t, body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"bboxes": [][]float64{{10, 20, 30, 40, 0.9, 1}, {1, 2, 3, 4, 0.8, 0}},
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	got, err := c.DLA(context.Background(), testImage(t))
	if err != nil {
		t.Fatalf("DLA: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 regions, got %d", len(got))
	}
	wantLabels := []string{deepdoctype.DefaultDLALabels()[1], deepdoctype.DefaultDLALabels()[0]}
	for i, g := range got {
		if g.X0 != float64([]float64{10, 1}[i]) || g.Y0 != float64([]float64{20, 2}[i]) ||
			g.X1 != float64([]float64{30, 3}[i]) || g.Y1 != float64([]float64{40, 4}[i]) {
			t.Errorf("region %d coords = %+v", i, g)
		}
		if g.Confidence != float64([]float64{0.9, 0.8}[i]) {
			t.Errorf("region %d confidence = %v", i, g.Confidence)
		}
		if g.Label != wantLabels[i] {
			t.Errorf("region %d label = %q, want %q", i, g.Label, wantLabels[i])
		}
	}
}

func TestClient_DLA_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"bboxes": [][]float64{}})
	}))
	defer srv.Close()

	c := New(srv.URL)
	got, err := c.DLA(context.Background(), testImage(t))
	if err != nil {
		t.Fatalf("DLA: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want 0 regions, got %d", len(got))
	}
}

func TestClient_DLA_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.DLA(context.Background(), testImage(t))
	if err == nil {
		t.Fatal("want error on 500")
	}
}

func TestClient_DLA_Malformed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.DLA(context.Background(), testImage(t))
	if err == nil {
		t.Fatal("want error on malformed body")
	}
}

func TestClient_DLA_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"bboxes": [][]float64{}})
	}))
	defer srv.Close()

	c := New(srv.URL, WithHTTPClient(&http.Client{Timeout: 50 * time.Millisecond}))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.DLA(ctx, testImage(t))
	if err == nil {
		t.Fatal("want error on timeout")
	}
}

// ── TSR ──

func TestClient_TSR_Happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"bboxes": [][]float64{{10, 20, 30, 40, 0.9, 0}, {1, 2, 3, 4, 0.8, 1}},
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	got, err := c.TSR(context.Background(), testImage(t))
	if err != nil {
		t.Fatalf("TSR: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 cells, got %d", len(got))
	}
	// cls 0 -> "v", cls 1 -> "h"
	if got[0].Label != "v" || got[1].Label != "h" {
		t.Fatalf("labels = %q/%q, want v/h", got[0].Label, got[1].Label)
	}
	if got[0].Y0 != 20 || got[0].Y1 != 40 || got[1].Y0 != 2 || got[1].Y1 != 4 {
		t.Fatalf("coords = %+v", got)
	}
	if got[0].Score != 0.9 || got[1].Score != 0.8 {
		t.Fatalf("scores = %v", []float64{got[0].Score, got[1].Score})
	}
}

func TestClient_TSR_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	c := New(srv.URL)
	if _, err := c.TSR(context.Background(), testImage(t)); err == nil {
		t.Fatal("want error on 502")
	}
}

func TestClient_TSR_Malformed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{"))
	}))
	defer srv.Close()
	c := New(srv.URL)
	if _, err := c.TSR(context.Background(), testImage(t)); err == nil {
		t.Fatal("want error on malformed body")
	}
}

// ── OCRDetect ──

func TestClient_OCRDetect_Happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// output[0][0][i] = 4 points
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": [][][][][2]float64{{{{{1, 2}, {3, 4}, {5, 6}, {7, 8}}}}},
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	got, err := c.OCRDetect(context.Background(), testImage(t))
	if err != nil {
		t.Fatalf("OCRDetect: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 box, got %d", len(got))
	}
	b := got[0]
	if b.X0 != 1 || b.Y0 != 2 || b.X1 != 3 || b.Y1 != 4 || b.X2 != 5 || b.Y2 != 6 || b.X3 != 7 || b.Y3 != 8 {
		t.Fatalf("box = %+v", b)
	}
}

func TestClient_OCRDetect_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := New(srv.URL)
	if _, err := c.OCRDetect(context.Background(), testImage(t)); err == nil {
		t.Fatal("want error on 500")
	}
}

func TestClient_OCRDetect_Malformed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("garbage"))
	}))
	defer srv.Close()
	c := New(srv.URL)
	if _, err := c.OCRDetect(context.Background(), testImage(t)); err == nil {
		t.Fatal("want error on malformed body")
	}
}

// ── OCRRecognize ──

func TestClient_OCRRecognize_Happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := readAll(r)
		var req struct {
			Images []string `json:"images"`
		}
		_ = json.Unmarshal(body, &req)
		if len(req.Images) != 1 {
			t.Errorf("want 1 image, got %d", len(req.Images))
		}
		// The Go EE server returns a flat JSON array, not the Python "output" wrapper.
		_, _ = w.Write([]byte(`[{"text":"hello","score":0.95}]`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	got, err := c.OCRRecognize(context.Background(), testImage(t))
	if err != nil {
		t.Fatalf("OCRRecognize: %v", err)
	}
	if len(got) != 1 || got[0].Text != "hello" || got[0].Confidence != 0.95 {
		t.Fatalf("got %+v", got)
	}
}

func TestClient_OCRRecognize_DropScore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"text":"x","score":0.1}]`))
	}))
	defer srv.Close()

	c := New(srv.URL, WithDropScore(0.5))
	got, err := c.OCRRecognize(context.Background(), testImage(t))
	if err != nil {
		t.Fatalf("OCRRecognize: %v", err)
	}
	if len(got) != 1 || got[0].Text != "" || got[0].Confidence != 0.1 {
		t.Fatalf("drop_score not applied: %+v", got)
	}
}

func TestClient_OCRRecognize_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := New(srv.URL)
	if _, err := c.OCRRecognize(context.Background(), testImage(t)); err == nil {
		t.Fatal("want error on 500")
	}
}

func TestClient_OCRRecognize_Malformed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-an-array"))
	}))
	defer srv.Close()
	c := New(srv.URL)
	if _, err := c.OCRRecognize(context.Background(), testImage(t)); err == nil {
		t.Fatal("want error on malformed body")
	}
}

// ── Health ──

func TestClient_Health_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()
	c := New(srv.URL)
	if !c.HealthContext(context.Background()) {
		t.Fatal("want healthy")
	}
}

func TestClient_Health_Down(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := New(srv.URL)
	if c.HealthContext(context.Background()) {
		t.Fatal("want unhealthy on 500")
	}
}

func TestClient_Health_Unreachable(t *testing.T) {
	c := New("http://127.0.0.1:1") // nothing listening
	if c.HealthContext(context.Background()) {
		t.Fatal("want unhealthy on unreachable")
	}
}

// TestClient_Health_ContextCancelled pins that a cancelled context makes
// Health return false without blocking on the connection. Before ctx was
// threaded in, Health ignored caller cancellation and could hang for the
// full client timeout.
func TestClient_Health_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	c := New(srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the call

	start := time.Now()
	if c.HealthContext(ctx) {
		t.Fatal("want unhealthy on cancelled context")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Health blocked for %s on a cancelled context; want fast return", elapsed)
	}
}

// TestClient_Health_ContextTimeout pins that a short deadline bounds the
// probe even when the backend hangs.
func TestClient_Health_ContextTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	c := New(srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	if c.HealthContext(ctx) {
		t.Fatal("want unhealthy on deadline-exceeded")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Health blocked for %s past the 150ms deadline; want bounded return", elapsed)
	}
}

// --- response-body ownership on cancellation (adversarial) ---

// trackingBody records whether the transport's response body was closed.
type trackingBody struct {
	closed chan struct{}
	once   sync.Once
}

func (b *trackingBody) Read([]byte) (int, error) { return 0, io.EOF }
func (b *trackingBody) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

// bodyTrackingTransport ignores ctx (like a hung/buggy backend) and always
// returns a 200 with a tracked body, so the probe cannot succeed by the
// transport itself rejecting the cancelled request.
type bodyTrackingTransport struct {
	mu     sync.Mutex
	bodies []*trackingBody
}

func (t *bodyTrackingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	b := &trackingBody{closed: make(chan struct{})}
	t.mu.Lock()
	t.bodies = append(t.bodies, b)
	t.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       b,
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func (t *bodyTrackingTransport) snapshot() []*trackingBody {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]*trackingBody(nil), t.bodies...)
}

// TestClient_HealthContext_ClosesBodyWhenCallerCancels pins the ownership
// contract: even when the caller's context is cancelled, any response body
// the HTTP stack produced must be closed. The old implementation raced the
// Do call in a goroutine against ctx.Done(); when Done won, the 200 response
// and its body were left sitting in a buffered channel, never closed, leaking
// the underlying connection. The fix (ctx-bound Do, no race) closes it.
func TestClient_HealthContext_ClosesBodyWhenCallerCancels(t *testing.T) {
	tr := &bodyTrackingTransport{}
	c := New("http://health.test", WithHTTPClient(&http.Client{Transport: tr}))

	const n = 64
	for i := 0; i < n; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancelled before the probe starts
		_ = c.HealthContext(ctx)
	}

	// Wait until every RoundTrip has produced its response.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(tr.snapshot()) < n {
		time.Sleep(time.Millisecond)
	}
	bodies := tr.snapshot()
	if len(bodies) != n {
		t.Fatalf("got %d RoundTrips, want %d", len(bodies), n)
	}
	leaked := 0
	for _, b := range bodies {
		select {
		case <-b.closed:
		default:
			leaked++
		}
	}
	if leaked != 0 {
		t.Fatalf("%d/%d response bodies left unclosed after cancelled probes", leaked, n)
	}
}

// TestClient_Health_DelegatesToContextProbe pins that the interface method
// Health still delegates to the ctx-bound probe (healthy backend -> true).
func TestClient_Health_DelegatesToContextProbe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()
	if !New(srv.URL).Health() {
		t.Fatal("Health() should report healthy via the ctx-bound probe")
	}
}
