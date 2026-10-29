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

package parser

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// standUpEEMock returns an httptest server emulating the EE DeepDoc inference
// service. It records how many predict calls it received and can be switched
// into a failing mode (every predict returns 500) to exercise the error path.
func standUpEEMock(t *testing.T, fail bool) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
			return
		}
		atomic.AddInt32(&hits, 1)
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("boom"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/predict/dla", "/predict/tsr":
			_, _ = w.Write([]byte(`{"bboxes":[]}`))
		case "/predict/ocr_det":
			_, _ = w.Write([]byte(`{"output":[]}`))
		case "/predict/ocr_rec":
			_, _ = w.Write([]byte(`[]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func loadDoc1(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "..", "test", "benchmark", "test_docs", "Doc1.pdf")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return data
}

// TestPDFParser_ParseWithResult_DeepDocEECPU_RoutesToEE proves the
// deepdoc_cpu_ee parse method resolves to the EE HTTP client and reaches the
// external service, not the in-process OSS backend.
func TestPDFParser_ParseWithResult_DeepDocEECPU_RoutesToEE(t *testing.T) {
	useMockDocAnalyzer(t) // irrelevant for EE, but keeps OSS factory sane.
	srv, hits := standUpEEMock(t, false)
	t.Setenv("DEEPDOC_CPU_EE_URL", srv.URL)

	pdf := NewPDFParser()
	pdf.ConfigureFromSetup(map[string]any{"parse_method": "deepdoc_cpu_ee"})
	data := loadDoc1(t)

	res := pdf.ParseWithResult(t.Context(), "Doc1.pdf", data)
	if res.Err != nil {
		t.Fatalf("ParseWithResult(deepdoc_cpu_ee) err = %v", res.Err)
	}
	if atomic.LoadInt32(hits) == 0 {
		t.Fatal("EE service received no predict calls; deepdoc_cpu_ee did not route to EE HTTP client")
	}
}

// TestPDFParser_ParseWithResult_DeepDocEECPU_ServiceError verifies that a
// failing EE service (HTTP 500) is still reached through the deepdoc_cpu_ee
// route. The shared DeepDoc pipeline intentionally degrades per-page inference
// errors to "no regions" for resilience (OSS and EE alike), so a 500 must not
// panic or hard-fail the whole parse -- it must be swallowed at page level.
// The client side error mapping itself is covered in the client package.
func TestPDFParser_ParseWithResult_DeepDocEECPU_ServiceError(t *testing.T) {
	useMockDocAnalyzer(t)
	srv, hits := standUpEEMock(t, true)
	t.Setenv("DEEPDOC_CPU_EE_URL", srv.URL)

	pdf := NewPDFParser()
	pdf.ConfigureFromSetup(map[string]any{"parse_method": "deepdoc_cpu_ee"})
	data := loadDoc1(t)

	res := pdf.ParseWithResult(t.Context(), "Doc1.pdf", data)
	if res.Err != nil {
		t.Fatalf("deepdoc_cpu_ee must degrade gracefully on EE 500, got hard error: %v", res.Err)
	}
	if atomic.LoadInt32(hits) == 0 {
		t.Fatal("EE service received no calls; deepdoc_cpu_ee did not route to the failing EE client")
	}
}

// TestPDFParser_ParseWithResult_DeepDocEECPU_UnconfiguredError proves that
// selecting deepdoc_cpu_ee without a configured EE URL fails loudly at parse
// time.
func TestPDFParser_ParseWithResult_DeepDocEECPU_UnconfiguredError(t *testing.T) {
	useMockDocAnalyzer(t)
	t.Setenv("DEEPDOC_CPU_EE_URL", "")

	pdf := NewPDFParser()
	pdf.ConfigureFromSetup(map[string]any{"parse_method": "deepdoc_cpu_ee"})
	data := loadDoc1(t)

	res := pdf.ParseWithResult(t.Context(), "Doc1.pdf", data)
	if res.Err == nil {
		t.Fatal("want error when deepdoc_cpu_ee selected but no EE URL configured")
	}
}
