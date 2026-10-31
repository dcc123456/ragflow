//go:build integration && ee

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

package api

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestFileUpload01HappyPathOnTrial(t *testing.T) {
	if os.Getenv("BILLING_STORAGE_E2E") != "1" {
		t.Skip("set BILLING_STORAGE_E2E=1 with object storage running")
	}
	tenant := newTenant(t)
	resp := tenant.uploadFile("file-upload-01.txt", []byte("hello billing"))
	if resp.status != http.StatusOK {
		t.Fatalf("upload failed: status=%d body=%s", resp.status, resp.body)
	}
}

// registerMember creates a user account so it can be invited.

func TestFileUpload02StorageQuotaExceeded(t *testing.T) {
	if os.Getenv("BILLING_STORAGE_E2E") != "1" {
		t.Skip("set BILLING_STORAGE_E2E=1 with object storage running")
	}
	tenant := newTenant(t)
	// Trial storage is 100MB; upload beyond it.
	payload := make([]byte, 120*1000*1000)
	resp := tenant.uploadFile("oversize.bin", payload)
	if !strings.Contains(resp.body, "\"code\":2003") {
		t.Fatalf("expected storage quota error 2003, got: %s", resp.body)
	}
	if !strings.Contains(resp.body, "\"resource\":\"storage\"") {
		t.Fatalf("storage error missing resource detail: %s", resp.body)
	}
}

// TestFileUpload03ResponseShape ports FILE-UPLOAD-BILLING-03/04: the error shape
// matches the dataset upload path.

func TestFileUpload03ResponseShape(t *testing.T) {
	if os.Getenv("BILLING_STORAGE_E2E") != "1" {
		t.Skip("set BILLING_STORAGE_E2E=1 with object storage running")
	}
	tenant := newTenant(t)
	resp := tenant.uploadFile("oversize.bin", make([]byte, 120*1000*1000))
	var payload map[string]interface{}
	if err := jsonUnmarshal(resp.body, &payload); err != nil {
		t.Fatalf("error body is not JSON: %s", resp.body)
	}
	if code, _ := payload["code"].(float64); int(code) != 2003 {
		t.Fatalf("expected code 2003, got %v", payload)
	}
	detail, _ := payload["detail"].(map[string]interface{})
	if detail == nil || detail["resource"] != "storage" {
		t.Fatalf("error detail does not match the dataset path shape: %v", payload)
	}
}

// removeMember deletes a pending invite or member by email.
