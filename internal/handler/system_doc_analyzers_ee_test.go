// Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build ee

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestEE_DocAnalyzers_ProbesHealth verifies the EE DeepDoc backends are
// reported available only when their <METHOD>_URL endpoint passes the
// remote /health probe.
func TestEE_DocAnalyzers_ProbesHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	t.Setenv("DEEPDOC_CPU_EE_URL", srv.URL)
	t.Setenv("DEEPDOC_GPU_EE_URL", "")
	t.Setenv("DEEPDOC_ASCEND_EE_URL", "")

	list := eeDocAnalyzers(context.Background())
	byID := map[string]DocAnalyzerInfo{}
	for _, a := range list {
		byID[a.ID] = a
	}
	cpu, ok := byID["deepdoc_cpu_ee"]
	if !ok {
		t.Fatalf("expected deepdoc_cpu_ee in %+v", list)
	}
	if cpu.Kind != "ee" {
		t.Errorf("cpu entry = %+v, want kind=ee", cpu)
	}
	if !cpu.Available {
		t.Errorf("deepdoc_cpu_ee should be available when /health passes")
	}
	gpu, ok := byID["deepdoc_gpu_ee"]
	if !ok || gpu.Available {
		t.Errorf("deepdoc_gpu_ee must be unavailable without DEEPDOC_GPU_EE_URL, got %+v", gpu)
	}
}

// TestEE_DocAnalyzers_UnavailableOnBadHealth verifies an unreachable EE
// endpoint yields available=false rather than panicking.
func TestEE_DocAnalyzers_UnavailableOnBadHealth(t *testing.T) {
	t.Setenv("DEEPDOC_CPU_EE_URL", "http://127.0.0.1:1")

	list := eeDocAnalyzers(context.Background())
	for _, a := range list {
		if a.ID == "deepdoc_cpu_ee" && a.Available {
			t.Errorf("deepdoc_cpu_ee must be unavailable when endpoint is unreachable")
		}
	}
}

// TestEE_DocAnalyzers_ProbeBounded pins that an unreachable backend does not
// block the probe beyond the 2s bound. (It used to share the 30s client
// timeout and could hang the whole request for up to 3 backends.)
func TestEE_DocAnalyzers_ProbeBounded(t *testing.T) {
	t.Setenv("DEEPDOC_CPU_EE_URL", "http://127.0.0.1:1") // nothing listening

	start := time.Now()
	list := eeDocAnalyzers(context.Background())
	elapsed := time.Since(start)
	if elapsed > 3*time.Second {
		t.Fatalf("eeDocAnalyzers blocked for %s; want bounded by the 2s probe timeout", elapsed)
	}
	for _, a := range list {
		if a.ID == "deepdoc_cpu_ee" && a.Available {
			t.Errorf("deepdoc_cpu_ee must be unavailable when endpoint is unreachable")
		}
	}
}

// TestEE_DocAnalyzers_ProbeIsolatedPerBackend pins that each configured
// backend gets its own probe budget: a hanging backend must not mask a
// healthy peer. The former implementation shared one 2s context across a
// sequential loop, so a single hang consumed the whole budget and every
// later backend was reported unavailable with an already-expired context.
func TestEE_DocAnalyzers_ProbeIsolatedPerBackend(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer healthy.Close()

	hung := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer hung.Close()

	t.Setenv("DEEPDOC_CPU_EE_URL", hung.URL)    // first in EEDeepDocBackends
	t.Setenv("DEEPDOC_GPU_EE_URL", healthy.URL) // second
	t.Setenv("DEEPDOC_ASCEND_EE_URL", "")

	start := time.Now()
	list := eeDocAnalyzers(context.Background())
	elapsed := time.Since(start)

	byID := map[string]DocAnalyzerInfo{}
	for _, a := range list {
		byID[a.ID] = a
	}
	if byID["deepdoc_cpu_ee"].Available {
		t.Error("hanging backend must be unavailable")
	}
	if !byID["deepdoc_gpu_ee"].Available {
		t.Errorf("healthy backend must stay available despite a hanging peer; list=%+v", list)
	}
	if elapsed > 3*time.Second {
		t.Errorf("probe took %s; independent concurrent probes should stay near the 2s bound", elapsed)
	}
}

// TestEE_DocAnalyzers_AllHealthy pins the happy path: every configured
// backend whose /health passes is reported available.
func TestEE_DocAnalyzers_AllHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	t.Setenv("DEEPDOC_CPU_EE_URL", srv.URL)
	t.Setenv("DEEPDOC_GPU_EE_URL", srv.URL)
	t.Setenv("DEEPDOC_ASCEND_EE_URL", srv.URL)

	list := eeDocAnalyzers(context.Background())
	for _, a := range list {
		if !a.Available {
			t.Errorf("%s should be available when /health passes", a.ID)
		}
	}
}

// TestEE_DocAnalyzers_CancelledContextReturnsFast pins that a cancelled
// caller context short-circuits all probes without waiting for the budget.
func TestEE_DocAnalyzers_CancelledContextReturnsFast(t *testing.T) {
	hung := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer hung.Close()

	t.Setenv("DEEPDOC_CPU_EE_URL", hung.URL)
	t.Setenv("DEEPDOC_GPU_EE_URL", hung.URL)
	t.Setenv("DEEPDOC_ASCEND_EE_URL", hung.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	list := eeDocAnalyzers(ctx)
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Errorf("cancelled context took %s; want fast return", elapsed)
	}
	for _, a := range list {
		if a.Available {
			t.Errorf("%s must be unavailable on cancelled context", a.ID)
		}
	}
}
