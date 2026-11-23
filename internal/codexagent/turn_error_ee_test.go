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

package codexagent

import (
	"errors"
	"fmt"
	"testing"

	codexgo "github.com/yuzhichang/codex-app-server-go"
)

// TestTurnErrorRetryableProviderFailure pins the failover policy: only failures that another
// provider might survive are retryable. The server's classification is lossy, so `other` and an
// absent kind must NOT be retried — an unknown-model 400 and a rejected prompt land there, and
// retrying them would burn another turn for a deterministic failure.
func TestTurnErrorRetryableProviderFailure(t *testing.T) {
	cases := []struct {
		name string
		err  *TurnError
		want bool
	}{
		{"internal server error", &TurnError{Kind: codexgo.CodexErrorInternalServer}, true},
		{"server overloaded", &TurnError{Kind: codexgo.CodexErrorServerOverloaded}, true},
		{"retry budget exhausted", &TurnError{Kind: codexgo.CodexErrorTooManyFailedAttempts, HTTPStatus: 429}, true},
		{"http connection failed", &TurnError{Kind: codexgo.CodexErrorHTTPConnectionFailed, HTTPStatus: 500}, true},
		{"stream connection failed", &TurnError{Kind: codexgo.CodexErrorResponseStreamConnectionFailed}, true},
		{"stream disconnected", &TurnError{Kind: codexgo.CodexErrorResponseStreamDisconnected}, true},
		{"rejected credentials", &TurnError{Kind: codexgo.CodexErrorUnauthorized, HTTPStatus: 401}, true},
		{"catch-all other is not retryable", &TurnError{Kind: codexgo.CodexErrorOther, Message: "unknown model"}, false},
		{"unclassified is not retryable", &TurnError{Message: "boom"}, false},
		{"context overflow is not retryable", &TurnError{Kind: codexgo.CodexErrorContextWindowExceeded}, false},
		{"interrupted is never retryable", &TurnError{Status: "interrupted", Kind: codexgo.CodexErrorInternalServer}, false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.RetryableProviderFailure(); got != tc.want {
				t.Fatalf("RetryableProviderFailure() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestTurnErrorInterruptedAndAsTurnError pins the two ways a caller classifies a failure, and
// that a wrapped TurnError is still discoverable.
func TestTurnErrorInterruptedAndAsTurnError(t *testing.T) {
	if !(&TurnError{Status: "interrupted"}).Interrupted() {
		t.Fatal("interrupted status must report interrupted")
	}
	if (&TurnError{Status: "failed"}).Interrupted() {
		t.Fatal("failed status is not interrupted")
	}

	wrapped := fmt.Errorf("outer: %w", &TurnError{Status: "failed", Kind: codexgo.CodexErrorInternalServer})
	got, ok := AsTurnError(wrapped)
	if !ok || got.Kind != codexgo.CodexErrorInternalServer {
		t.Fatalf("AsTurnError(wrapped) = (%v, %v), want the inner TurnError", got, ok)
	}
	if _, ok := AsTurnError(errors.New("plain")); ok {
		t.Fatal("a plain error must not be reported as a TurnError")
	}
}

// TestShouldReuseThread pins the reuse rule, including the failover retry, which must never
// resume a thread bound to the failed provider even when every fingerprint matches.
func TestShouldReuseThread(t *testing.T) {
	rec := ThreadRecord{ScopeFingerprint: "scope", HistoryFingerprint: "hist"}
	cases := []struct {
		name         string
		found        bool
		rec          ThreadRecord
		scope, seen  string
		handleAlive  bool
		forceRebuild bool
		want         bool
	}{
		{"match", true, rec, "scope", "hist", true, false, true},
		{"not stored", false, rec, "scope", "hist", true, false, false},
		{"scope drift", true, rec, "other", "hist", true, false, false},
		{"history drift", true, rec, "scope", "other", true, false, false},
		{"dead handle", true, rec, "scope", "hist", false, false, false},
		{"failover retry never reuses", true, rec, "scope", "hist", true, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldReuseThread(tc.found, tc.rec, tc.scope, tc.seen, tc.handleAlive, tc.forceRebuild); got != tc.want {
				t.Fatalf("shouldReuseThread() = %v, want %v", got, tc.want)
			}
		})
	}
}
