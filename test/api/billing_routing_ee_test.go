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
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// TestBillingStatusAnonymous verifies the bare JSON contract the web client
// reads at startup.
func TestBillingStatusAnonymous(t *testing.T) {
	resp, err := http.Get(TestConfig.HostAddress + "/api/v1/billing/status")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	payload := requireStatusCode(t, resp, http.StatusOK)
	if _, ok := payload["billing_enabled"].(bool); !ok {
		t.Fatalf("billing_enabled must be a top-level bool, got %v", payload)
	}
	if _, wrapped := payload["code"]; wrapped {
		t.Fatalf("billing status must not use the code/data envelope: %v", payload)
	}
}

// TestBillingPlansAnonymous verifies the plans route stays guard-only.
func TestBillingPlansAnonymous(t *testing.T) {
	resp, err := http.Get(TestConfig.HostAddress + "/api/v1/billing/plans")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	payload := requireStatusCode(t, resp, http.StatusOK)
	code, _ := payload["code"].(float64)
	if int(code) != 0 && int(code) != 102 {
		t.Fatalf("unexpected plans code %v: %v", payload["code"], payload)
	}
}

// TestBillingSubscriptionRequiresAuth verifies authenticated routes reject
// anonymous callers.
func TestBillingSubscriptionRequiresAuth(t *testing.T) {
	resp, err := http.Get(TestConfig.HostAddress + "/api/v1/billing/subscription")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// requireStatusCode asserts the HTTP status and decodes the JSON body.
func requireStatusCode(t *testing.T, resp *http.Response, want int) map[string]interface{} {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != want {
		t.Fatalf("status = %d, want %d: %s", resp.StatusCode, want, body)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode body %q: %v", body, err)
	}
	return payload
}
