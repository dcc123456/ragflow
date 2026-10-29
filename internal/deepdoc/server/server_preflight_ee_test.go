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
	"context"
	"os"
	"path/filepath"
	"testing"

	"ragflow/internal/common"
)

// TestValidateModelDir_Empty pins that an unset EE_MODEL_DIR fails the boot
// preflight before the server starts.
func TestValidateModelDir_Empty(t *testing.T) {
	if err := validateModelDir(""); err == nil {
		t.Fatal("want error for empty modelDir, got nil")
	}
}

// TestValidateModelDir_MissingFile pins that an EE_MODEL_DIR missing any one of the
// weights listed in common.DeepDocModelFiles fails the preflight (catches a
// partially-provisioned model dir at launch instead of 500-ing on the first
// inference request).
func TestValidateModelDir_MissingFile(t *testing.T) {
	dir := t.TempDir()
	files := common.DeepDocModelFiles
	if len(files) == 0 {
		t.Fatal("DeepDocModelFiles is empty; nothing to check")
	}
	// Write every file EXCEPT the last one.
	for i, f := range files {
		if i == len(files)-1 {
			break
		}
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateModelDir(dir); err == nil {
		t.Fatalf("want error for missing %q, got nil", files[len(files)-1])
	}
}

// TestValidateModelDir_AllPresent pins that a fully-provisioned EE_MODEL_DIR passes
// the preflight.
func TestValidateModelDir_AllPresent(t *testing.T) {
	dir := t.TempDir()
	for _, f := range common.DeepDocModelFiles {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateModelDir(dir); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

// TestRun_MissingModelFileReturnsError pins the end-to-end boot behavior:
// Run() must call the preflight once at startup, so an EE_MODEL_DIR missing weights
// returns an error before any HTTP listener is opened (no hang, no 500 at
// request time).
func TestRun_MissingModelFileReturnsError(t *testing.T) {
	dir := t.TempDir()
	files := common.DeepDocModelFiles
	if len(files) == 0 {
		t.Fatal("DeepDocModelFiles is empty; nothing to check")
	}
	for i, f := range files {
		if i == len(files)-1 {
			break
		}
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("EE_MODEL_DIR", dir)

	// A cancelled context guarantees we never block on <-ctx.Done() even if the
	// preflight were to pass; here the preflight must fail first and Run must
	// return before reaching ListenAndServe.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := Run(ctx, 0); err == nil {
		t.Fatal("want error from Run() when model files are missing, got nil")
	}
}
