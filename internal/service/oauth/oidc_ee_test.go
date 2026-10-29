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

package oauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
)

const (
	oidcTestClientID = "ragflow-client"
	oidcTestKID      = "kid-1"
)

// mockIdP is an in-process OpenID Provider serving discovery, JWKS, token and
// userinfo endpoints. No external services are involved.
type mockIdP struct {
	t          *testing.T
	srv        *httptest.Server
	signingKey *rsa.PrivateKey
	kid        string
	// advertisedIssuer overrides the issuer published in the discovery
	// document (defaults to the server URL).
	advertisedIssuer string
	userinfo         map[string]interface{}
	userinfoHits     atomic.Int32
	jwksHits         atomic.Int32
}

func newMockIdP(t *testing.T) *mockIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	m := &mockIdP{t: t, signingKey: key, kid: oidcTestKID}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeIDPJSON(t, w, map[string]interface{}{
			"issuer":                                m.issuerValue(),
			"authorization_endpoint":                m.srv.URL + "/authorize",
			"token_endpoint":                        m.srv.URL + "/token",
			"userinfo_endpoint":                     m.srv.URL + "/userinfo",
			"jwks_uri":                              m.srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		m.jwksHits.Add(1)
		writeIDPJSON(t, w, jose.JSONWebKeySet{
			Keys: []jose.JSONWebKey{{
				Key: &m.signingKey.PublicKey, KeyID: m.kid, Algorithm: "RS256", Use: "sig",
			}},
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		writeIDPJSON(t, w, map[string]interface{}{
			"access_token": "access-token",
			"token_type":   "Bearer",
		})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		m.userinfoHits.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Errorf("userinfo Authorization header = %q", got)
		}
		writeIDPJSON(t, w, m.userinfo)
	})

	m.srv = httptest.NewServer(mux)
	t.Cleanup(m.srv.Close)
	return m
}

func writeIDPJSON(t *testing.T, w http.ResponseWriter, v interface{}) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

// issuerValue is the issuer the discovery document advertises.
func (m *mockIdP) issuerValue() string {
	if m.advertisedIssuer != "" {
		return m.advertisedIssuer
	}
	return m.srv.URL
}

// signIDToken builds a signed id_token; claimsOverride may replace defaults or
// drop a claim (nil value).
func (m *mockIdP) signIDToken(t *testing.T, key *rsa.PrivateKey, kid string, claimsOverride map[string]interface{}) string {
	t.Helper()
	now := time.Now()
	claims := jwt.MapClaims{
		"iss":            m.issuerValue(),
		"aud":            oidcTestClientID,
		"sub":            "user-123",
		"email":          "alice@example.com",
		"email_verified": true,
		"iat":            now.Add(-time.Minute).Unix(),
		"exp":            now.Add(5 * time.Minute).Unix(),
	}
	for k, v := range claimsOverride {
		if v == nil {
			delete(claims, k)
			continue
		}
		claims[k] = v
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign id_token: %v", err)
	}
	return signed
}

// newTestOIDCClient wires an oidcClient to the mock IdP.
func newTestOIDCClient(t *testing.T, m *mockIdP, cfg Config) *oidcClient {
	t.Helper()
	resetOIDCCaches()
	if cfg.Type == "" {
		cfg.Type = "oidc"
	}
	if cfg.ClientID == "" {
		cfg.ClientID = oidcTestClientID
	}
	if cfg.Issuer == "" {
		cfg.Issuer = m.srv.URL
	}
	if cfg.RedirectURI == "" {
		cfg.RedirectURI = "https://ragflow.example.com/api/v1/auth/oauth/oidc/callback"
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	oidc, ok := client.(*oidcClient)
	if !ok {
		t.Fatalf("expected *oidcClient, got %T", client)
	}
	return oidc
}

func TestOIDCFetchUserInfoHappyPath(t *testing.T) {
	m := newMockIdP(t)
	m.userinfo = map[string]interface{}{
		"sub": "user-123", "email": "alice@example.com", "email_verified": true,
		"preferred_username": "alice", "name": "Alice Example", "picture": "https://cdn/alice.png",
	}
	c := newTestOIDCClient(t, m, Config{})

	info, err := c.FetchUserInfo(context.Background(), "access-token", m.signIDToken(t, m.signingKey, oidcTestKID, nil))
	if err != nil {
		t.Fatalf("FetchUserInfo: %v", err)
	}
	if info.Email != "alice@example.com" || !info.EmailVerified {
		t.Errorf("email/verified = %q/%v", info.Email, info.EmailVerified)
	}
	if info.Username != "alice" {
		t.Errorf("username = %q, want preferred_username", info.Username)
	}
	if info.Nickname != "Alice Example" {
		t.Errorf("nickname = %q", info.Nickname)
	}
	if info.AvatarURL != "https://cdn/alice.png" {
		t.Errorf("avatar = %q", info.AvatarURL)
	}
	if info.Subject != "user-123" {
		t.Errorf("subject = %q", info.Subject)
	}
}

func TestOIDCRejectsBadIDTokens(t *testing.T) {
	m := newMockIdP(t)
	m.userinfo = map[string]interface{}{"sub": "user-123", "email": "alice@example.com", "email_verified": true}

	now := time.Now()
	cases := []struct {
		name   string
		claims map[string]interface{}
	}{
		{"wrong audience", map[string]interface{}{"aud": "someone-else"}},
		{"wrong issuer", map[string]interface{}{"iss": "https://evil.example"}},
		{"expired", map[string]interface{}{"exp": now.Add(-2 * time.Minute).Unix()}},
		{"missing exp", map[string]interface{}{"exp": nil}},
		{"iat in the future", map[string]interface{}{"iat": now.Add(time.Hour).Unix()}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestOIDCClient(t, m, Config{})
			token := m.signIDToken(t, m.signingKey, oidcTestKID, tc.claims)
			if _, err := c.FetchUserInfo(context.Background(), "access-token", token); err == nil {
				t.Fatalf("expected rejection for %s", tc.name)
			}
		})
	}
}

func TestOIDCRejectsAlgNoneAndHMAC(t *testing.T) {
	m := newMockIdP(t)
	m.userinfo = map[string]interface{}{"sub": "user-123", "email": "alice@example.com"}
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": m.issuerValue(), "aud": oidcTestClientID, "sub": "user-123",
		"email": "alice@example.com", "iat": now.Add(-time.Minute).Unix(),
		"exp": now.Add(5 * time.Minute).Unix(),
	}

	t.Run("alg none", func(t *testing.T) {
		tok := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
		tok.Header["kid"] = oidcTestKID
		signed, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
		if err != nil {
			t.Fatalf("sign none: %v", err)
		}
		c := newTestOIDCClient(t, m, Config{})
		if _, err := c.FetchUserInfo(context.Background(), "access-token", signed); err == nil {
			t.Fatal("alg=none must be rejected")
		}
	})

	t.Run("hs256 with public key bytes", func(t *testing.T) {
		// RSA/HMAC confusion: an attacker HMACs with the provider's public key
		// because sloppy verifiers pick the algorithm from the token header.
		tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tok.Header["kid"] = oidcTestKID
		signed, err := tok.SignedString(x509.MarshalPKCS1PublicKey(&m.signingKey.PublicKey))
		if err != nil {
			t.Fatalf("sign hs256: %v", err)
		}
		c := newTestOIDCClient(t, m, Config{})
		if _, err := c.FetchUserInfo(context.Background(), "access-token", signed); err == nil {
			t.Fatal("HS256 must be rejected")
		}
	})
}

func TestOIDCRejectsForgedSignature(t *testing.T) {
	m := newMockIdP(t)
	m.userinfo = map[string]interface{}{"sub": "user-123", "email": "alice@example.com"}
	rogue, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rogue key: %v", err)
	}
	c := newTestOIDCClient(t, m, Config{})
	if _, err := c.FetchUserInfo(context.Background(), "access-token", m.signIDToken(t, rogue, oidcTestKID, nil)); err == nil {
		t.Fatal("token signed by an unknown key must be rejected")
	}
}

func TestOIDCSurvivesKeyRotation(t *testing.T) {
	m := newMockIdP(t)
	m.userinfo = map[string]interface{}{"sub": "user-123", "email": "alice@example.com", "email_verified": true}
	c := newTestOIDCClient(t, m, Config{})

	// Warm the JWKS cache with the original key.
	if _, err := c.FetchUserInfo(context.Background(), "access-token", m.signIDToken(t, m.signingKey, oidcTestKID, nil)); err != nil {
		t.Fatalf("first login: %v", err)
	}
	if got := m.jwksHits.Load(); got != 1 {
		t.Fatalf("jwks hits after first login = %d, want 1", got)
	}

	// Rotate: the provider now signs with a new kid absent from our cache.
	rotated, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rotated key: %v", err)
	}
	m.signingKey = rotated
	m.kid = "kid-2"
	// The forced refresh is rate-limited to one per minute, so age the cache
	// entry as if it had been populated a while ago.
	ageJWKSCache(m.srv.URL+"/jwks", 2*time.Minute)

	if _, err := c.FetchUserInfo(context.Background(), "access-token", m.signIDToken(t, m.signingKey, "kid-2", nil)); err != nil {
		t.Fatalf("login after key rotation: %v", err)
	}
	if got := m.jwksHits.Load(); got != 2 {
		t.Errorf("jwks hits after rotation = %d, want 2 (one forced refresh)", got)
	}
}

func TestOIDCUnknownKidRefreshIsRateLimited(t *testing.T) {
	m := newMockIdP(t)
	m.userinfo = map[string]interface{}{"sub": "user-123", "email": "alice@example.com"}
	c := newTestOIDCClient(t, m, Config{})

	if _, err := c.FetchUserInfo(context.Background(), "access-token", m.signIDToken(t, m.signingKey, oidcTestKID, nil)); err != nil {
		t.Fatalf("first login: %v", err)
	}
	// A token with an unknown kid must not trigger a second fetch while the
	// cache is fresh, otherwise random kids become a request amplifier.
	for i := 0; i < 3; i++ {
		if _, err := c.FetchUserInfo(context.Background(), "access-token", m.signIDToken(t, m.signingKey, "random-kid", nil)); err == nil {
			t.Fatal("unknown kid must be rejected")
		}
	}
	if got := m.jwksHits.Load(); got != 1 {
		t.Errorf("jwks hits = %d, want 1 (forced refresh rate-limited)", got)
	}
}

func TestOIDCRejectsUserinfoSubMismatch(t *testing.T) {
	m := newMockIdP(t)
	m.userinfo = map[string]interface{}{"sub": "someone-else", "email": "mallory@example.com"}
	c := newTestOIDCClient(t, m, Config{})

	if _, err := c.FetchUserInfo(context.Background(), "access-token", m.signIDToken(t, m.signingKey, oidcTestKID, nil)); err == nil {
		t.Fatal("mismatched userinfo sub must be rejected")
	}
}

func TestOIDCUsesDiscoveredIssuerWithTrailingSlash(t *testing.T) {
	m := newMockIdP(t)
	// The provider advertises "https://host:port/" while the config carries the
	// discovery base without the slash — a routine mismatch that must not
	// break `iss` validation.
	m.advertisedIssuer = m.srv.URL + "/"
	m.userinfo = map[string]interface{}{"sub": "user-123", "email": "alice@example.com", "email_verified": true}
	c := newTestOIDCClient(t, m, Config{})

	if _, err := c.FetchUserInfo(context.Background(), "access-token", m.signIDToken(t, m.signingKey, oidcTestKID, nil)); err != nil {
		t.Fatalf("issuer with trailing slash should verify: %v", err)
	}
}

func TestOIDCClaimMappingIsConfigurable(t *testing.T) {
	raw := map[string]interface{}{
		"email":        "alice@example.com",
		"org_uid":      "x500",
		"display_name": "Alice",
		"gravatar":     "https://cdn/a.png",
		"sub":          "user-123",
	}
	cfg := Config{
		UsernameClaims: []string{"org_uid", "preferred_username", "sub"},
		NicknameClaims: []string{"display_name"},
		AvatarClaims:   []string{"gravatar"},
	}
	info := normalizeUserInfoWithClaims(raw, cfg)
	if info.Username != "x500" {
		t.Errorf("username = %q, want configured claim", info.Username)
	}
	if info.Nickname != "Alice" {
		t.Errorf("nickname = %q", info.Nickname)
	}
	if info.AvatarURL != "https://cdn/a.png" {
		t.Errorf("avatar = %q", info.AvatarURL)
	}
	if info.Email != "alice@example.com" {
		t.Errorf("email = %q", info.Email)
	}
}

func TestNormalizeUserInfoEmailVerifiedForms(t *testing.T) {
	cases := []struct {
		value interface{}
		want  bool
	}{
		{true, true},
		{"true", true},
		{" TRUE ", true},
		{false, false},
		{"false", false},
		{nil, false},
		{1, false},
	}
	for _, tc := range cases {
		raw := map[string]interface{}{"email": "a@example.com"}
		if tc.value != nil {
			raw["email_verified"] = tc.value
		}
		if got := normalizeUserInfoWithClaims(raw, Config{}).EmailVerified; got != tc.want {
			t.Errorf("email_verified=%v: got %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestResolveSigningAlgs(t *testing.T) {
	if got := resolveSigningAlgs(nil); len(got) != 1 || got[0] != "RS256" {
		t.Errorf("empty advertised set should default to RS256, got %v", got)
	}
	got := resolveSigningAlgs([]string{"RS256", "HS256", "ES256", "none"})
	if strings.Join(got, ",") != "RS256,ES256" {
		t.Errorf("allowlist intersection = %v, want [RS256 ES256]", got)
	}
}

// ageJWKSCache rewinds a cached JWKS entry so a forced refresh is permitted.
func ageJWKSCache(uri string, d time.Duration) {
	jwksCacheMu.Lock()
	defer jwksCacheMu.Unlock()
	if e, ok := jwksCache[uri]; ok {
		e.fetchedAt = e.fetchedAt.Add(-d)
	}
}
