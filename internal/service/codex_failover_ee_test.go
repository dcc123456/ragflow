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

package service

import (
	"context"
	"errors"
	"testing"

	"ragflow/internal/codexagent"
	"ragflow/internal/mcp"

	codexgo "github.com/yuzhichang/codex-app-server-go"
)

// TestCodexFailoverPlan pins the circle: every candidate exactly once, starting at current.
func TestCodexFailoverPlan(t *testing.T) {
	cases := []struct {
		name       string
		candidates int
		current    int
		want       []int
	}{
		{name: "starts at primary", candidates: 3, current: 0, want: []int{0, 1, 2}},
		{name: "wraps from middle", candidates: 3, current: 1, want: []int{1, 2, 0}},
		{name: "wraps from tail", candidates: 3, current: 2, want: []int{2, 0, 1}},
		{name: "single candidate", candidates: 1, current: 0, want: []int{0}},
		{name: "no candidates", candidates: 0, current: 0, want: nil},
		{name: "current out of range wraps", candidates: 2, current: 5, want: []int{1, 0}},
		{name: "negative current starts at zero", candidates: 2, current: -3, want: []int{0, 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := codexFailoverPlan(tc.candidates, tc.current)
			if len(got) != len(tc.want) {
				t.Fatalf("plan = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("plan = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// TestCodexProviderCursor proves the sticky start: only a COMPLETED turn moves the cursor.
func TestCodexProviderCursor(t *testing.T) {
	targets := []*ResolvedModelTarget{
		{ModelName: "m", BaseURL: "http://a", APIKey: "k1"},
		{ModelName: "m", BaseURL: "http://b", APIKey: "k2"},
	}
	candidates := []codexTurnTarget{{Target: targets[0]}, {Target: targets[1]}}

	cursor := &codexProviderCursor{}
	if got := cursor.Start("chat", candidates); got != 0 {
		t.Fatalf("unmarked chat starts at %d, want 0", got)
	}

	cursor.Mark("chat", targets[1])
	if got := cursor.Start("chat", candidates); got != 1 {
		t.Fatalf("after marking the second candidate, start = %d, want 1", got)
	}

	// Another chat is unaffected, and a stale key (candidate no longer in the roster) falls
	// back to the dialog's own model rather than pointing at nothing.
	if got := cursor.Start("other", candidates); got != 0 {
		t.Fatalf("other chat start = %d, want 0", got)
	}
	cursor.Mark("stale", &ResolvedModelTarget{ModelName: "gone", BaseURL: "http://gone", APIKey: "k9"})
	if got := cursor.Start("stale", candidates); got != 0 {
		t.Fatalf("stale cursor start = %d, want 0", got)
	}
}

// TestCodexFailoverEligible pins when a failed turn may be retried on the next provider. The
// refusals matter as much as the acceptance: a held session lock is not a provider failure, a
// cancelled request has nobody to answer, an interrupted turn was stopped on purpose, and once
// answer text has reached the client a retry would append a second answer.
func TestCodexFailoverEligible(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	providerFailure := &codexagent.TurnError{Status: "failed", Kind: codexgo.CodexErrorInternalServer, HTTPStatus: 500}
	cases := []struct {
		name     string
		ctx      context.Context
		runErr   error
		emitted  int
		wantOK   bool
		wantKind codexgo.CodexErrorKind
	}{
		{"provider failure", context.Background(), providerFailure, 0, true, codexgo.CodexErrorInternalServer},
		{"no error", context.Background(), nil, 0, false, ""},
		{"session busy is a lock, not a provider", context.Background(), mcp.ErrCodexTurnBusy, 0, false, ""},
		{"cancelled request", cancelled, providerFailure, 0, false, ""},
		{"answer already streamed", context.Background(), providerFailure, 12, false, ""},
		{"interrupted turn", context.Background(),
			&codexagent.TurnError{Status: "interrupted", Kind: codexgo.CodexErrorInternalServer}, 0, false, ""},
		{"catch-all other", context.Background(),
			&codexagent.TurnError{Kind: codexgo.CodexErrorOther, Message: "unknown model"}, 0, false, ""},
		{"untyped error", context.Background(), errors.New("plain"), 0, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			turnErr, ok := codexFailoverEligible(tc.ctx, tc.runErr, tc.emitted)
			if ok != tc.wantOK {
				t.Fatalf("codexFailoverEligible() ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && turnErr.Kind != tc.wantKind {
				t.Fatalf("kind = %q, want %q", turnErr.Kind, tc.wantKind)
			}
			if !ok && turnErr != nil {
				t.Fatalf("a refused failure must not return a TurnError, got %v", turnErr)
			}
		})
	}
}

// TestCodexResponsesUsable (moved to codex_responses_probe_test.go) pins the bar a mode-8
// target must clear; it now covers both the known-capable fast path and the dynamic probe.

// TestCandidateKey pins the dedup key: two candidates are the same only when they share the
// endpoint, the model AND the credential, so a sibling instance of one vendor (same endpoint,
// different key) is still a distinct failover member.
func TestCandidateKey(t *testing.T) {
	base := &ResolvedModelTarget{BaseURL: "https://a/v1", ModelName: "m1", APIKey: "key-1"}
	if candidateKey(base) != candidateKey(&ResolvedModelTarget{BaseURL: "https://a/v1", ModelName: "m1", APIKey: "key-1"}) {
		t.Fatal("the same endpoint, model and credential must share a key")
	}
	if candidateKey(base) == candidateKey(&ResolvedModelTarget{BaseURL: "https://b/v1", ModelName: "m1", APIKey: "key-1"}) {
		t.Fatal("a different endpoint is a different candidate")
	}
	if candidateKey(base) == candidateKey(&ResolvedModelTarget{BaseURL: "https://a/v1", ModelName: "m2", APIKey: "key-1"}) {
		t.Fatal("a different model is a different candidate")
	}
	// The failover group this exists for: one vendor, one endpoint, several keys (a per-key
	// rate limit). Keying on (endpoint, model) alone collapsed these into the primary.
	if candidateKey(base) == candidateKey(&ResolvedModelTarget{BaseURL: "https://a/v1", ModelName: "m1", APIKey: "key-2"}) {
		t.Fatal("a different credential on the same endpoint and model is a distinct failover member")
	}
}

// TestCodexTargetLabel pins the human label the failover narration uses. Candidates routinely
// share provider and model and differ only by instance, so the instance must qualify the model —
// otherwise the line reads "<model> failed; retrying on <model>".
func TestCodexTargetLabel(t *testing.T) {
	cases := []struct {
		name   string
		target *ResolvedModelTarget
		want   string
	}{
		{"instance qualified", &ResolvedModelTarget{ModelName: "MiniMax-M3", InstanceName: "zyf"}, "MiniMax-M3(zyf)"},
		{"no instance name", &ResolvedModelTarget{ModelName: "MiniMax-M3"}, "MiniMax-M3"},
		{"nil target", nil, "unknown model"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := codexTargetLabel(tc.target); got != tc.want {
				t.Fatalf("codexTargetLabel() = %q, want %q", got, tc.want)
			}
		})
	}
}
