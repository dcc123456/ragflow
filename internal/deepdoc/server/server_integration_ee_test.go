//go:build cgo && integration

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
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gin-gonic/gin"
	"ragflow/internal/deepdoc/native"
)

// TestTSREndpoint_RealFixture drives the full HTTP path with the real EE 2-class
// YOLOv8 model: POST the parity fixture PNG → assert the response matches the
// EE Python service's wire shape and the validated parity numbers (v=283/h=17,
// ≥298 boxes). This is the end-to-end proof that the gin /predict/tsr endpoint
// is functionally equivalent to the EE Python service.
func TestTSREndpoint_RealFixture(t *testing.T) {
	if os.Getenv("RUN_EE_TESTS") == "" {
		t.Skip("set RUN_EE_TESTS=1 to run EE integration tests")
	}
	md := os.Getenv("EE_MODEL_DIR")
	if md == "" {
		t.Skip("EE_MODEL_DIR unset: skipping real-model endpoint test")
	}
	if err := native.InitORT(); err != nil {
		t.Fatalf("InitORT: %v", err)
	}

	gin.SetMode(gin.TestMode)
	h := newTSRHandler(md, nil) // nil → real native.RunTSREE
	e := gin.New()
	h.registerRoutes(e)

	pngBytes, err := os.ReadFile(fixturePath(t, "internal/deepdoc/native/testdata/tsr_ee_parity/tsr_ee_table.png"))
	if err != nil {
		t.Fatal(err)
	}
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	fw, err := w.CreateFormFile("request", "tsr_ee_table.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(pngBytes); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/predict/tsr", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Bboxes [][]float64 `json:"bboxes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if len(out.Bboxes) < 298 {
		t.Fatalf("expected >=298 boxes, got %d", len(out.Bboxes))
	}
	v, h2 := 0, 0
	for _, b := range out.Bboxes {
		switch int(b[5]) {
		case 0:
			v++
		case 1:
			h2++
		}
	}
	if v != 283 || h2 != 17 {
		t.Fatalf("class distribution want v=283/h=17, got v=%d/h=%d", v, h2)
	}
}

// fixturePath resolves a path relative to the module root regardless of the test's
// working directory.
func fixturePath(t *testing.T, rel string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, rel)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("repo root (go.mod) not found")
	return ""
}
