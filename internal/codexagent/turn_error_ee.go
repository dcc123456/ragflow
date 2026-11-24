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

	codexgo "github.com/yuzhichang/codex-app-server-go"
)

// TurnError is a turn the Codex server ended as failed or interrupted. It carries the provider
// classification the server sent (`codexErrorInfo`) so a caller can decide whether the failure
// is worth retrying on other infrastructure — a provider failover — instead of matching on the
// error text.
//
// A turn failure is NOT the only way a turn ends badly: the per-session lock
// (mcp.ErrCodexTurnBusy) and a cancelled request context are plain errors, deliberately not of
// this type, because neither is a provider failure.
type TurnError struct {
	// Status is the turn status that ended it: "failed" or "interrupted". Empty for a
	// failure reported by a notification mid-turn (the error event carries no status).
	Status string
	// Kind is the server's codexErrorInfo classification; CodexErrorUnknown when none was sent.
	Kind codexgo.CodexErrorKind
	// HTTPStatus is the classification's HTTP status, when it carries one.
	HTTPStatus int
	// Message is the server's human-readable detail.
	Message string
}

func (e *TurnError) Error() string {
	status := e.Status
	if status == "" {
		status = "failed"
	}
	if e.Kind != codexgo.CodexErrorUnknown {
		if e.HTTPStatus != 0 {
			return fmt.Sprintf("codexagent: turn %s (kind=%s, http=%d): %s", status, e.Kind, e.HTTPStatus, e.Message)
		}
		return fmt.Sprintf("codexagent: turn %s (kind=%s): %s", status, e.Kind, e.Message)
	}
	return fmt.Sprintf("codexagent: turn %s: %s", status, e.Message)
}

// Interrupted reports a turn the client or user stopped, which is not a provider failure.
func (e *TurnError) Interrupted() bool {
	return e.Status == string(codexgo.TurnStatusInterrupted)
}

// RetryableProviderFailure reports whether the failure looks provider-side, i.e. something a
// different provider might survive.
//
// It is deliberately conservative. The server's classification is lossy — an unknown-model 400,
// a rejected prompt and a content filter all collapse into `other` — so `other` and an absent
// kind are NOT retryable: those failures are deterministic and would simply repeat on the next
// provider (while a partial answer has already been paid for). `unauthorized` IS retryable,
// because a rejected credential is exactly a case where a second provider can answer.
func (e *TurnError) RetryableProviderFailure() bool {
	if e == nil || e.Interrupted() {
		return false
	}
	switch e.Kind {
	case codexgo.CodexErrorInternalServer,
		codexgo.CodexErrorServerOverloaded,
		codexgo.CodexErrorTooManyFailedAttempts,
		codexgo.CodexErrorHTTPConnectionFailed,
		codexgo.CodexErrorResponseStreamConnectionFailed,
		codexgo.CodexErrorResponseStreamDisconnected,
		codexgo.CodexErrorUnauthorized:
		return true
	}
	return false
}

// AsTurnError extracts a *TurnError from err, reporting whether err was one.
func AsTurnError(err error) (*TurnError, bool) {
	var turnErr *TurnError
	if errors.As(err, &turnErr) {
		return turnErr, true
	}
	return nil, false
}
