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

// Package client is the HTTP client side of the EE DeepDoc inference service.
//
// The EE DeepDoc backend runs as a standalone docker container exposing
// /predict/dla, /predict/tsr, /predict/ocr_det, /predict/ocr_rec and /health
// (see ragflow/internal/deepdoc/server). client implements the same
// doctype.DocAnalyzer interface the in-process OSS backend uses, so the PDF
// parser can pick either backend by parse_method ("deepdoc" -> OSS in-process,
// "DeepDoc_EE" -> this client) without the parser knowing which one it got.
//
// It deliberately shares NO cgo/onnxruntime code: it only speaks HTTP/JSON to
// the EE service, mirroring the exact wire contract that server emits.
//go:build ee

package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"strings"
	"time"

	deepdoctype "ragflow/internal/deepdoc/parser/type"
)

// tsrEELabels maps the EE 2-class separator wire id back to its kind. Mirrors
// native/tsr_ee.go's tsrEELabels (v/h); the EE service emits class ids 0/1.
var tsrEELabels = []string{"v", "h"}

// DefaultDropScore mirrors deepdoc/vision/ocr.py's Recognizer.drop_score (0.5).
// OCRRecognize blanks text whose score is below it while preserving the real
// score, so callers consume an identical OCRText regardless of backend.
const DefaultDropScore = 0.5

// Client talks to a running EE DeepDoc inference service over HTTP.
type Client struct {
	baseURL   string
	http      *http.Client
	dropScore float64
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient overrides the underlying *http.Client (e.g. to set a timeout).
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// WithDropScore overrides the OCR confidence threshold (see DefaultDropScore).
func WithDropScore(s float64) Option {
	return func(c *Client) { c.dropScore = s }
}

// New builds an EE DeepDoc client for the given base URL (e.g.
// http://deepdoc-ee-cpu:8001). It does not dial the server; Health must be used to
// confirm reachability.
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		http:      &http.Client{Timeout: 30 * time.Second},
		dropScore: DefaultDropScore,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// DLA runs layout detection by POSTing the page image to /predict/dla.
func (c *Client) DLA(ctx context.Context, img image.Image) ([]deepdoctype.DLARegion, error) {
	var resp struct {
		Bboxes [][]float64 `json:"bboxes"`
	}
	if err := c.postJSON(ctx, "/predict/dla", img, &resp); err != nil {
		return nil, err
	}
	labels := deepdoctype.DefaultDLALabels()
	out := make([]deepdoctype.DLARegion, 0, len(resp.Bboxes))
	for _, b := range resp.Bboxes {
		if len(b) < 6 {
			return nil, fmt.Errorf("client DLA: malformed bbox %v", b)
		}
		label := ""
		cls := int(b[5])
		if cls >= 0 && cls < len(labels) {
			label = labels[cls]
		}
		out = append(out, deepdoctype.DLARegion{
			X0: b[0], Y0: b[1], X1: b[2], Y1: b[3],
			Label: label, Confidence: b[4],
		})
	}
	return out, nil
}

// TSR recognises table structure by POSTing the cropped image to /predict/tsr.
func (c *Client) TSR(ctx context.Context, img image.Image) ([]deepdoctype.TSRCell, error) {
	var resp struct {
		Bboxes [][]float64 `json:"bboxes"`
	}
	if err := c.postJSON(ctx, "/predict/tsr", img, &resp); err != nil {
		return nil, err
	}
	out := make([]deepdoctype.TSRCell, 0, len(resp.Bboxes))
	for _, b := range resp.Bboxes {
		if len(b) < 6 {
			return nil, fmt.Errorf("client TSR: malformed bbox %v", b)
		}
		label := ""
		cls := int(b[5])
		if cls >= 0 && cls < len(tsrEELabels) {
			label = tsrEELabels[cls]
		}
		out = append(out, deepdoctype.TSRCell{
			X0: b[0], Y0: b[1], X1: b[2], Y1: b[3],
			Label: label, Score: b[4],
		})
	}
	return out, nil
}

// OCRDetect detects text-region quadrilaterals by POSTing to /predict/ocr_det.
func (c *Client) OCRDetect(ctx context.Context, img image.Image) ([]deepdoctype.OCRBox, error) {
	var resp struct {
		Output [][][][][]float64 `json:"output"`
	}
	if err := c.postJSON(ctx, "/predict/ocr_det", img, &resp); err != nil {
		return nil, err
	}
	if len(resp.Output) == 0 || len(resp.Output[0]) == 0 {
		return nil, nil
	}
	quads := resp.Output[0][0]
	out := make([]deepdoctype.OCRBox, 0, len(quads))
	for _, q := range quads {
		if len(q) < 4 {
			return nil, fmt.Errorf("client OCRDetect: malformed quad %v", q)
		}
		out = append(out, deepdoctype.OCRBox{
			X0: q[0][0], Y0: q[0][1],
			X1: q[1][0], Y1: q[1][1],
			X2: q[2][0], Y2: q[2][1],
			X3: q[3][0], Y3: q[3][1],
		})
	}
	return out, nil
}

// OCRRecognize recognizes text in one cropped region by POSTing to
// /predict/ocr_rec. The EE service returns a flat JSON array of
// {"text","score"} items; this maps the single-image result and applies the
// drop_score blanking contract shared with the OSS backend.
func (c *Client) OCRRecognize(ctx context.Context, img image.Image) ([]deepdoctype.OCRText, error) {
	b64 := encodeImageB64(img)
	payload, err := json.Marshal(map[string]any{"images": []string{b64}})
	if err != nil {
		return nil, err
	}
	raw, err := c.do(ctx, http.MethodPost, "/predict/ocr_rec", payload)
	if err != nil {
		return nil, err
	}
	var items []struct {
		Text  string  `json:"text"`
		Score float64 `json:"score"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("client OCRRecognize decode: %w", err)
	}
	if len(items) == 0 {
		return nil, nil
	}
	it := items[0]
	if it.Score < c.dropScore {
		return []deepdoctype.OCRText{{Text: "", Confidence: it.Score}}, nil
	}
	return []deepdoctype.OCRText{{Text: it.Text, Confidence: it.Score}}, nil
}

// Health reports whether the EE service is reachable and reports ok.
func (c *Client) Health() bool {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// postJSON encodes the image as a base64 JSON {"image": ...} body, POSTs it,
// and decodes the JSON response into out.
func (c *Client) postJSON(ctx context.Context, path string, img image.Image, out any) error {
	payload, err := json.Marshal(map[string]any{"image": encodeImageB64(img)})
	if err != nil {
		return err
	}
	raw, err := c.do(ctx, http.MethodPost, path, payload)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("client decode %s: %w", path, err)
	}
	return nil
}

// do performs an HTTP POST with a JSON body and returns the raw response bytes,
// turning non-2xx statuses into errors.
func (c *Client) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("client %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("client %s read: %w", path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("client %s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return raw, nil
}

// encodeImageB64 renders the image as PNG and base64-encodes it, matching the
// {"image": "<b64>"} / {"images": ["<b64>"]} contracts the EE server accepts.
func encodeImageB64(img image.Image) string {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		// png.Encode only fails on unsupported image types; fall back to an
		// empty payload rather than panicking mid-request.
		return ""
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}
