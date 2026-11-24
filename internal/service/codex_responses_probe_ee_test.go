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
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	modelModule "ragflow/internal/entity/models"
	serverconfig "ragflow/internal/server/config"
)

// probeForTest points the probe at an httptest server and clears the cache. The httptest
// client is used so no environment proxy can intercept a 127.0.0.1 probe.
func probeForTest(t *testing.T, client *http.Client) {
	t.Helper()
	savedClient := codexResponsesProbeClient
	savedConfig := getCodexConfig()
	resetCodexResponsesProbeCache()
	codexResponsesProbeClient = client
	t.Cleanup(func() {
		codexResponsesProbeClient = savedClient
		SetCodexConfig(savedConfig)
		resetCodexResponsesProbeCache()
	})
}

// TestCodexResponsesRouteExistsDifferential pins the differential rule: the route counts as
// present only when "/responses" answers differently from a path nothing serves. The cases
// that must stay ABSENT are the point — an auth-first gateway answers 401 to everything, a
// gateway without the route answers 404 to everything, and a catch-all answers 200 to
// everything; none of them is a usable Responses endpoint.
func TestCodexResponsesRouteExistsDifferential(t *testing.T) {
	cases := []struct {
		name    string
		status  int // status for /responses
		control int // status for the control path
		want    bool
	}{
		{"routed differently (400 vs 404) -> present", http.StatusBadRequest, http.StatusNotFound, true},
		{"routed differently (200 vs 404) -> present", http.StatusOK, http.StatusNotFound, true},
		{"no route: 404 to both -> absent", http.StatusNotFound, http.StatusNotFound, false},
		{"auth-first: 401 to both -> absent", http.StatusUnauthorized, http.StatusUnauthorized, false},
		{"catch-all 200 to both -> absent", http.StatusOK, http.StatusOK, false},
		{"control routed differently is still present", http.StatusUnauthorized, http.StatusOK, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/responses" {
					w.WriteHeader(tc.status)
					return
				}
				w.WriteHeader(tc.control)
			}))
			defer srv.Close()
			probeForTest(t, srv.Client())

			if got := codexResponsesRouteExists(context.Background(), srv.URL, "sk-test"); got != tc.want {
				t.Fatalf("codexResponsesRouteExists = %v, want %v (probe %d, control %d)", got, tc.want, tc.status, tc.control)
			}
		})
	}
}

// TestCodexResponsesRouteExistsSendsKeyAndIsCached pins two properties at once: the probe
// carries the instance's key (an auth-first gateway needs it to answer /responses
// differently from the catch-all) and its verdict is cached, so a second call issues no
// further requests.
func TestCodexResponsesRouteExistsSendsKeyAndIsCached(t *testing.T) {
	var hits atomic.Int32
	var sawAuth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		sawAuth.Store(r.Header.Get("Authorization"))
		if r.URL.Path == "/responses" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	probeForTest(t, srv.Client())

	if !codexResponsesRouteExists(context.Background(), srv.URL, "sk-secret") {
		t.Fatal("a routed /responses must be present")
	}
	if got := sawAuth.Load(); got != "Bearer sk-secret" {
		t.Fatalf("probe Authorization = %v, want Bearer sk-secret", got)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("first probe issued %d requests, want 2 (probe + control)", got)
	}

	if !codexResponsesRouteExists(context.Background(), srv.URL, "sk-secret") {
		t.Fatal("cached verdict must stay present")
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("cached probe issued %d requests total, want 2", got)
	}

	// A different key is a different cache slot.
	if !codexResponsesRouteExists(context.Background(), srv.URL, "sk-other") {
		t.Fatal("a different key must probe afresh and stay present")
	}
	if got := hits.Load(); got != 4 {
		t.Fatalf("probe with a new key issued %d requests total, want 4", got)
	}
}

// TestCodexResponsesRouteExistsUnreachableIsAbsent pins the conservative failure mode: an
// endpoint the probe cannot reach is not claimed capable.
func TestCodexResponsesRouteExistsUnreachableIsAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing listening now
	probeForTest(t, http.DefaultClient)

	if codexResponsesRouteExists(context.Background(), url, "sk-test") {
		t.Fatal("an unreachable endpoint must not be reported as a Responses route")
	}
}

// TestCodexResponsesUsable pins the bar a mode-8 target must clear: a known-capable provider
// is trusted without any network call, any other provider must expose a Responses route at its
// instance base_url, and a missing base_url is refused.
func TestCodexResponsesUsable(t *testing.T) {
	dummy := modelModule.NewDummyModel(nil, modelModule.URLSuffix{}) // Name() == "dummy"

	// A nil target, and a target without a base_url, are unusable whatever the set says.
	SetCodexConfig(nil)
	if _, ok := codexResponsesUsable(context.Background(), nil); ok {
		t.Fatal("a nil target must not be usable")
	}
	if _, ok := codexResponsesUsable(context.Background(), &ResolvedModelTarget{Driver: dummy}); ok {
		t.Fatal("an empty base_url must not be usable")
	}

	// A known-capable provider is trusted without touching the network: the base_url is
	// deliberately unreachable, so a probe here would fail the test.
	probeForTest(t, http.DefaultClient)
	SetCodexConfig(&serverconfig.CodexConfig{ResponsesProviders: []string{"dummy"}})
	if _, ok := codexResponsesUsable(context.Background(), &ResolvedModelTarget{Driver: dummy, BaseURL: "https://127.0.0.1:1/v1"}); !ok {
		t.Fatal("a known-capable provider must be usable without a probe")
	}

	// Outside the set, the instance's base_url decides: a routed /responses is usable...
	probeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/responses" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer probeSrv.Close()

	probeForTest(t, probeSrv.Client())
	SetCodexConfig(&serverconfig.CodexConfig{ResponsesProviders: []string{"someoneelse"}})
	if _, ok := codexResponsesUsable(context.Background(), &ResolvedModelTarget{Driver: dummy, BaseURL: probeSrv.URL}); !ok {
		t.Fatal("a provider whose endpoint exposes a Responses route must be usable")
	}

	// ...and a 404-everything endpoint is not.
	absentSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer absentSrv.Close()

	probeForTest(t, absentSrv.Client())
	if _, ok := codexResponsesUsable(context.Background(), &ResolvedModelTarget{Driver: dummy, BaseURL: absentSrv.URL}); ok {
		t.Fatal("a provider whose endpoint exposes no Responses route must not be usable")
	}
}
