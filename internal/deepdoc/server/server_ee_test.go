//go:build cgo && ee

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

package deepdocserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"ragflow/internal/deepdoc/native"
)

// stubTSRFunc returns a fixed result regardless of input, so the HTTP wiring can
// be tested without loading the ONNX model.
func stubTSRFunc(res native.TSREEResult, err error) tsrFunc {
	return func(_ context.Context, _ string, _ *native.Image) (native.TSREEResult, error) {
		return res, err
	}
}

// makePNG returns the bytes of a tiny but valid PNG.
func makePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := 0; i < 64; i++ {
		img.Pix[i*4] = uint8(i)
		img.Pix[i*4+1] = uint8(i)
		img.Pix[i*4+2] = uint8(i)
		img.Pix[i*4+3] = 255
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// multipartBody wraps raw bytes as a "request" form field (matching the
// Python deepdoc server contract that the DLA/TSR/det endpoints now require).
func multipartBody(t *testing.T, name string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	fw, err := w.CreateFormFile("request", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return body, w.FormDataContentType()
}

// TestTSREndpoint_MultipartOK pins the primary contract: POST a multipart image
// file → 200 with the EE wire shape {"bboxes": [[x0,y0,x1,y1,score,cls],...]}.
func TestTSREndpoint_MultipartOK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTSRHandler("md", stubTSRFunc(native.TSREEResult{
		Boxes: []native.TSREEBox{{Label: "v", Score: 0.9, X0: 1, X1: 2, Top: 3, Bottom: 4}},
	}, nil))
	e := gin.New()
	h.registerRoutes(e)

	body, ct := multipartBody(t, "t.png", makePNG(t))
	req := httptest.NewRequest(http.MethodPost, "/predict/tsr", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Bboxes [][]float64 `json:"bboxes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if len(out.Bboxes) != 1 {
		t.Fatalf("want 1 box, got %d", len(out.Bboxes))
	}
	if got := out.Bboxes[0][5]; int(got) != 0 {
		t.Fatalf("want cls 0 (v), got %v", out.Bboxes[0])
	}
}

// TestTSREndpoint_Base64JSON pins the alternate JSON contract:
// POST {"image": "<base64>"} → 200.
func TestTSREndpoint_Base64JSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTSRHandler("md", stubTSRFunc(native.TSREEResult{
		Boxes: []native.TSREEBox{{Label: "h", Score: 0.8, X0: 0, X1: 1, Top: 0, Bottom: 1}},
	}, nil))
	e := gin.New()
	h.registerRoutes(e)

	payload, _ := json.Marshal(map[string]string{
		"image": base64.StdEncoding.EncodeToString(makePNG(t)),
	})
	req := httptest.NewRequest(http.MethodPost, "/predict/tsr", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Bboxes [][]float64 `json:"bboxes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Bboxes) != 1 || int(out.Bboxes[0][5]) != 1 {
		t.Fatalf("want 1 box cls 1, got %v", out.Bboxes)
	}
}

// TestTSREndpoint_NoRequest pins the bad-request path: no request image → 400.
func TestTSREndpoint_NoRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTSRHandler("md", stubTSRFunc(native.TSREEResult{}, nil))
	e := gin.New()
	h.registerRoutes(e)

	req := httptest.NewRequest(http.MethodPost, "/predict/tsr", bytes.NewReader(nil))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xxx")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

// TestTSREndpoint_InferenceError pins the error path: TSR failure → 500.
func TestTSREndpoint_InferenceError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTSRHandler("md", stubTSRFunc(native.TSREEResult{}, io.ErrUnexpectedEOF))
	e := gin.New()
	h.registerRoutes(e)

	body, ct := multipartBody(t, "t.png", makePNG(t))
	req := httptest.NewRequest(http.MethodPost, "/predict/tsr", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

// ── stubs + helpers for the other three endpoints ──

func stubDLAFunc(res native.DLAResult, err error) dlaFunc {
	return func(_ context.Context, _ string, _ *native.Image) (native.DLAResult, error) {
		return res, err
	}
}

func stubDetFunc(res native.DetResult, err error) detFunc {
	return func(_ context.Context, _ string, _ *native.Image) (native.DetResult, error) {
		return res, err
	}
}

func stubOCRRecBatchFunc(res []native.OCRRecResult, err error) ocrRecBatchFunc {
	return func(_ context.Context, _ string, _ []*native.Image) ([]native.OCRRecResult, error) {
		return res, err
	}
}

// multipartMultiBody wraps several raw byte blobs as "files" form fields (the
// field name /predict/ocr_rec accepts for batch recognition).
func multipartMultiBody(t *testing.T, fields map[string][]byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	for name, data := range fields {
		fw, err := w.CreateFormFile("files", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return body, w.FormDataContentType()
}

// ── /predict/dla ──

// TestDLAEndpoint_MultipartOK pins POST a multipart image → 200 with the DLA
// wire shape {"bboxes": [[x0,y0,x1,y1,score,cls],...]}.
func TestDLAEndpoint_MultipartOK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newDeepDocHandler("md", stubDLAFunc(native.DLAResult{
		W: 100, H: 100,
		Boxes: []native.DLABox{{X0: 1, Y0: 2, X1: 3, Y1: 4, Score: 0.9, Class: 1}},
	}, nil), nil, nil, nil)
	e := gin.New()
	h.registerRoutes(e)

	body, ct := multipartBody(t, "t.png", makePNG(t))
	req := httptest.NewRequest(http.MethodPost, "/predict/dla", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Bboxes [][]float64 `json:"bboxes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if len(out.Bboxes) != 1 {
		t.Fatalf("want 1 box, got %d", len(out.Bboxes))
	}
	if int(out.Bboxes[0][5]) != 1 {
		t.Fatalf("want cls 1, got %v", out.Bboxes[0])
	}
}

// TestDLAEndpoint_NoRequest pins no request image → 400.
func TestDLAEndpoint_NoRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newDeepDocHandler("md", stubDLAFunc(native.DLAResult{}, nil), nil, nil, nil)
	e := gin.New()
	h.registerRoutes(e)

	req := httptest.NewRequest(http.MethodPost, "/predict/dla", bytes.NewReader(nil))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xxx")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

// TestDLAEndpoint_InferenceError pins DLA failure → 500.
func TestDLAEndpoint_InferenceError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newDeepDocHandler("md", stubDLAFunc(native.DLAResult{}, io.ErrUnexpectedEOF), nil, nil, nil)
	e := gin.New()
	h.registerRoutes(e)

	body, ct := multipartBody(t, "t.png", makePNG(t))
	req := httptest.NewRequest(http.MethodPost, "/predict/dla", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

// ── /predict/ocr_det ──

// TestOCRDetEndpoint_MultipartOK pins POST a multipart image → 200 with the Det
// wire shape {"output": [[[quad,...]]]} (quads are 4×[x,y]).
func TestOCRDetEndpoint_MultipartOK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newDeepDocHandler("md", nil, nil, stubDetFunc(native.DetResult{
		Boxes: []native.DetBox{{Pts: [4][2]float32{{0, 0}, {1, 0}, {1, 1}, {0, 1}}, Score: 0.7}},
	}, nil), nil)
	e := gin.New()
	h.registerRoutes(e)

	body, ct := multipartBody(t, "t.png", makePNG(t))
	req := httptest.NewRequest(http.MethodPost, "/predict/ocr_det", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Output [][][][][2]float64 `json:"output"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if len(out.Output) != 1 || len(out.Output[0]) != 1 || len(out.Output[0][0]) != 1 {
		t.Fatalf("want 1 page / 1 quad batch, got %#v", out.Output)
	}
	if len(out.Output[0][0][0]) != 4 {
		t.Fatalf("want 4 quad points, got %#v", out.Output[0][0][0])
	}
}

// TestOCRDetEndpoint_NoRequest pins no request image → 400.
func TestOCRDetEndpoint_NoRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newDeepDocHandler("md", nil, nil, stubDetFunc(native.DetResult{}, nil), nil)
	e := gin.New()
	h.registerRoutes(e)

	req := httptest.NewRequest(http.MethodPost, "/predict/ocr_det", bytes.NewReader(nil))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xxx")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

// TestOCRDetEndpoint_InferenceError pins Det failure → 500.
func TestOCRDetEndpoint_InferenceError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newDeepDocHandler("md", nil, nil, stubDetFunc(native.DetResult{}, io.ErrUnexpectedEOF), nil)
	e := gin.New()
	h.registerRoutes(e)

	body, ct := multipartBody(t, "t.png", makePNG(t))
	req := httptest.NewRequest(http.MethodPost, "/predict/ocr_det", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

// ── /predict/ocr_rec (batch) ──

// TestOCRRecEndpoint_MultipartBatch pins the batch contract: N multipart "files"
// → 200 with a JSON array of N items, order-correspondent with the input.
func TestOCRRecEndpoint_MultipartBatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := stubOCRRecBatchFunc([]native.OCRRecResult{
		{Text: "first", Score: 0.91},
		{Text: "second", Score: 0.82},
	}, nil)
	h := newDeepDocHandler("md", nil, nil, nil, stub)
	e := gin.New()
	h.registerRoutes(e)

	body, ct := multipartMultiBody(t, map[string][]byte{
		"a.png": makePNG(t),
		"b.png": makePNG(t),
	})
	req := httptest.NewRequest(http.MethodPost, "/predict/ocr_rec", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var items []OCRRecItem
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	if items[0].Text != "first" || items[1].Text != "second" {
		t.Fatalf("order not preserved: %#v", items)
	}
	if items[0].Score != 0.91 {
		t.Fatalf("want score 0.91, got %v", items[0].Score)
	}
}

// TestOCRRecEndpoint_Base64Batch pins the alternate JSON contract:
// POST {"images": ["<base64>",...]} → 200 with a JSON array of equal length.
func TestOCRRecEndpoint_Base64Batch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := stubOCRRecBatchFunc([]native.OCRRecResult{
		{Text: "x", Score: 0.5},
		{Text: "y", Score: 0.6},
		{Text: "z", Score: 0.7},
	}, nil)
	h := newDeepDocHandler("md", nil, nil, nil, stub)
	e := gin.New()
	h.registerRoutes(e)

	pngB64 := base64.StdEncoding.EncodeToString(makePNG(t))
	payload, _ := json.Marshal(map[string][]string{"images": {pngB64, pngB64, pngB64}})
	req := httptest.NewRequest(http.MethodPost, "/predict/ocr_rec", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var items []OCRRecItem
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if len(items) != 3 {
		t.Fatalf("want 3 items, got %d", len(items))
	}
}

// TestOCRRecEndpoint_NoImages pins the bad-request path: no images → 400.
func TestOCRRecEndpoint_NoImages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newDeepDocHandler("md", nil, nil, nil, stubOCRRecBatchFunc(nil, nil))
	e := gin.New()
	h.registerRoutes(e)

	req := httptest.NewRequest(http.MethodPost, "/predict/ocr_rec", bytes.NewReader(nil))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xxx")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

// TestOCRRecEndpoint_InferenceError pins the batch failure → 500.
func TestOCRRecEndpoint_InferenceError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newDeepDocHandler("md", nil, nil, nil, stubOCRRecBatchFunc(nil, io.ErrUnexpectedEOF))
	e := gin.New()
	h.registerRoutes(e)

	body, ct := multipartMultiBody(t, map[string][]byte{"a.png": makePNG(t)})
	req := httptest.NewRequest(http.MethodPost, "/predict/ocr_rec", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

// ── /predict/ocr (operator-switched, mirroring the Python EE docker) ──

// multipartOCRBody wraps raw bytes as the "request" form field (the field name
// the Python OCREndpoint expects) plus an optional "operator" text field.
func multipartOCRBody(t *testing.T, data []byte, operator string) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	fw, err := w.CreateFormFile("request", "t.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatal(err)
	}
	if operator != "" {
		if err := w.WriteField("operator", operator); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return body, w.FormDataContentType()
}

// TestOCREndpoint_Det pins the det contract used by ocr_cli.py OCRClient.detect:
// multipart "request" image + operator=det → {"output": [[[quad,...]]]}.
func TestOCREndpoint_Det(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newDeepDocHandler("md", nil, nil, stubDetFunc(native.DetResult{
		Boxes: []native.DetBox{{Pts: [4][2]float32{{0, 0}, {1, 0}, {1, 1}, {0, 1}}, Score: 0.7}},
	}, nil), nil)
	e := gin.New()
	h.registerRoutes(e)

	body, ct := multipartOCRBody(t, makePNG(t), "det")
	req := httptest.NewRequest(http.MethodPost, "/predict/ocr", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Output [][][][][2]float64 `json:"output"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if len(out.Output) != 1 || len(out.Output[0]) != 1 || len(out.Output[0][0]) != 1 {
		t.Fatalf("want 1 page / 1 quad batch, got %#v", out.Output)
	}
	if len(out.Output[0][0][0]) != 4 {
		t.Fatalf("want 4 quad points, got %#v", out.Output[0][0][0])
	}
}

// TestOCREndpoint_DefaultOperator pins that omitting "operator" defaults to det.
func TestOCREndpoint_DefaultOperator(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newDeepDocHandler("md", nil, nil, stubDetFunc(native.DetResult{
		Boxes: []native.DetBox{{Pts: [4][2]float32{{0, 0}, {2, 0}, {2, 2}, {0, 2}}, Score: 0.5}},
	}, nil), nil)
	e := gin.New()
	h.registerRoutes(e)

	body, ct := multipartOCRBody(t, makePNG(t), "")
	req := httptest.NewRequest(http.MethodPost, "/predict/ocr", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Output [][][][][2]float64 `json:"output"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if len(out.Output) == 0 {
		t.Fatalf("want det output, got %s", w.Body.String())
	}
}

// TestOCREndpoint_Rec pins the rec contract used by ocr_cli.py
// OCRClient.recognize_batch: multipart "request" crop + operator=rec →
// {"output": [[[ ["text", score] ]]]}, where the recognized text is reachable
// at output[0][0][0][0] exactly as the Python client indexes it.
func TestOCREndpoint_Rec(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := stubOCRRecBatchFunc([]native.OCRRecResult{
		{Text: "hello", Score: 0.93},
	}, nil)
	h := newDeepDocHandler("md", nil, nil, nil, stub)
	e := gin.New()
	h.registerRoutes(e)

	body, ct := multipartOCRBody(t, makePNG(t), "rec")
	req := httptest.NewRequest(http.MethodPost, "/predict/ocr", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Output [][][][]any `json:"output"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	// Replicate ocr_cli.py's indexing: output[0][0][0][0] == text.
	if len(out.Output) == 0 || len(out.Output[0]) == 0 || len(out.Output[0][0]) == 0 {
		t.Fatalf("unexpected rec output shape: %#v", out.Output)
	}
	pair := out.Output[0][0][0]
	if len(pair) < 2 {
		t.Fatalf("unexpected rec pair: %#v", pair)
	}
	text, _ := pair[0].(string)
	if text != "hello" {
		t.Fatalf("want text 'hello' at output[0][0][0][0], got %v", pair[0])
	}
	score, _ := pair[1].(float64)
	if score != 0.93 {
		t.Fatalf("want score 0.93, got %v", score)
	}
}

// TestOCREndpoint_RecJSON pins the alternate JSON contract for rec:
// POST {"request": "<base64>", "operator": "rec"} → the same {"output":...} shape.
func TestOCREndpoint_RecJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := stubOCRRecBatchFunc([]native.OCRRecResult{
		{Text: "json", Score: 0.88},
	}, nil)
	h := newDeepDocHandler("md", nil, nil, nil, stub)
	e := gin.New()
	h.registerRoutes(e)

	payload, _ := json.Marshal(map[string]string{
		"request":  base64.StdEncoding.EncodeToString(makePNG(t)),
		"operator": "rec",
	})
	req := httptest.NewRequest(http.MethodPost, "/predict/ocr", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Output [][][][]any `json:"output"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	pair := out.Output[0][0][0]
	if len(pair) < 1 {
		t.Fatalf("unexpected rec pair: %#v", pair)
	}
	text, _ := pair[0].(string)
	if text != "json" {
		t.Fatalf("want 'json', got %v", pair[0])
	}
}

// TestOCREndpoint_UnknownOperator pins a bad operator → 400.
func TestOCREndpoint_UnknownOperator(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newDeepDocHandler("md", nil, nil, nil, stubOCRRecBatchFunc(nil, nil))
	e := gin.New()
	h.registerRoutes(e)

	body, ct := multipartOCRBody(t, makePNG(t), "bogus")
	req := httptest.NewRequest(http.MethodPost, "/predict/ocr", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestOCREndpoint_NoRequest pins missing "request" image → 400.
func TestOCREndpoint_NoRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newDeepDocHandler("md", nil, nil, nil, stubOCRRecBatchFunc(nil, nil))
	e := gin.New()
	h.registerRoutes(e)

	req := httptest.NewRequest(http.MethodPost, "/predict/ocr", bytes.NewReader(nil))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xxx")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}
