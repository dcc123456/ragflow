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

package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"ragflow/internal/server/bootstrap"
)

// captureHelpStderr runs fn with os.Stderr redirected to a pipe and returns
// everything it wrote. printHelp writes to os.Stderr directly, so this is the
// only way to assert on its output without changing production code.
func captureHelpStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = old })
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close help pipe: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read help output: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close help read pipe: %v", err)
	}
	return string(out)
}

func TestPrintHelpDefaultStartup(t *testing.T) {
	help := captureHelpStderr(t, func() { printHelp(&bootstrap.ServerArgs{}) })
	for _, want := range []string{"one process by default", "--admin", "--api", "--ingestor", "--syncer", "--chatter", "--migrate", "--config", "--log-level", "default: warn", "--port", "--profile", "--help", "--version"} {
		if !strings.Contains(help, want) {
			t.Errorf("help does not document %q:\n%s", want, help)
		}
	}
	for _, flag := range []string{"--ingestor-tier", "--init-superuser"} {
		if !strings.Contains(help, flag) {
			t.Errorf("help does not document enterprise flag %q:\n%s", flag, help)
		}
	}
	for _, flag := range []string{"--deepdoc-inference-concurrency", "--deepdoc-inference-cpu-cores"} {
		if strings.Contains(help, flag) {
			t.Errorf("help documents removed flag %q:\n%s", flag, help)
		}
	}
	for _, want := range []string{"low, medium or high", "default: low"} {
		if !strings.Contains(help, want) {
			t.Errorf("help does not explain %q:\n%s", want, help)
		}
	}
}

func TestPrintHelpChatter(t *testing.T) {
	for _, mode := range []string{"api", "chatter"} {
		help := captureHelpStderr(t, func() { printHelp(&bootstrap.ServerArgs{Mode: new(mode)}) })
		want := "--" + mode
		if !strings.Contains(help, want) {
			t.Fatalf("%s help missing %s: %s", mode, want, help)
		}
	}
}
