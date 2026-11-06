//
//  Copyright (c) 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//
//  This file is part of the RAGFlow Enterprise Edition and is licensed,
//  not sold, under the RAGFlow Enterprise Commercial License Agreement.
//  Unauthorized copying, modification, distribution, or disclosure of this
//  file, via any medium, is strictly prohibited.
//
//  Portions of this file may incorporate code from RAGFlow Community Edition,
//  which is licensed under the Apache License, Version 2.0.
//
//  For commercial licensing inquiries: sales@infiniflow.ai
//

//go:build ee

package oauth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAuthorizationURLOAuth2(t *testing.T) {
	c, err := NewClient(Config{
		Type:             "oauth2",
		ClientID:         "abc",
		ClientSecret:     "secret",
		AuthorizationURL: "https://provider.example/authorize",
		TokenURL:         "https://provider.example/token",
		UserinfoURL:      "https://provider.example/userinfo",
		RedirectURI:      "https://ragflow.example/api/v1/auth/oauth/myorg/callback",
		Scope:            "openid email",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	got, err := c.AuthorizationURL("state-token")
	if err != nil {
		t.Fatalf("AuthorizationURL: %v", err)
	}
	u, perr := url.Parse(got)
	if perr != nil {
		t.Fatalf("parse url: %v", perr)
	}
	q := u.Query()
	if q.Get("client_id") != "abc" {
		t.Errorf("client_id: got %q", q.Get("client_id"))
	}
	if q.Get("redirect_uri") != "https://ragflow.example/api/v1/auth/oauth/myorg/callback" {
		t.Errorf("redirect_uri: got %q", q.Get("redirect_uri"))
	}
	if q.Get("response_type") != "code" {
		t.Errorf("response_type: got %q", q.Get("response_type"))
	}
	if q.Get("scope") != "openid email" {
		t.Errorf("scope: got %q", q.Get("scope"))
	}
	if q.Get("state") != "state-token" {
		t.Errorf("state: got %q", q.Get("state"))
	}
	if u.Host != "provider.example" {
		t.Errorf("host: got %q", u.Host)
	}
}

func TestAuthorizationURLPreservesExistingQuery(t *testing.T) {
	c, _ := NewClient(Config{
		Type:             "oauth2",
		ClientID:         "abc",
		AuthorizationURL: "https://provider.example/authorize?tenant=acme",
		TokenURL:         "https://provider.example/token",
		RedirectURI:      "https://ragflow.example/cb",
	})
	got, err := c.AuthorizationURL("s")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "tenant=acme&") {
		t.Errorf("existing tenant=acme query should be preserved with `&` separator, got %q", got)
	}
}

func TestNewClientUnsupportedType(t *testing.T) {
	_, err := NewClient(Config{Type: "saml"})
	if err == nil || !strings.Contains(err.Error(), "unsupported type") {
		t.Fatalf("expected unsupported-type error, got %v", err)
	}
}

func TestNewClientDefaultsBasedOnIssuer(t *testing.T) {
	orig := loadOIDCMetadata
	defer func() { loadOIDCMetadata = orig }()
	loadOIDCMetadata = func(issuer string) (*oidcMetadata, error) {
		return &oidcMetadata{
			Issuer:                issuer,
			AuthorizationEndpoint: issuer + "/authorize",
			TokenEndpoint:         issuer + "/token",
			UserinfoEndpoint:      issuer + "/userinfo",
		}, nil
	}
	c, err := NewClient(Config{
		Issuer:      "https://issuer.example",
		ClientID:    "id",
		RedirectURI: "https://ragflow/cb",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, ok := c.(*oidcClient); !ok {
		t.Fatalf("expected oidcClient, got %T", c)
	}
	got, _ := c.AuthorizationURL("s")
	if !strings.HasPrefix(got, "https://issuer.example/authorize") {
		t.Errorf("authorization URL didn't pick up discovered endpoint, got %q", got)
	}
}

// TestExchangeCodeForTokenOmitsUnsetRedirectURI covers the token leg of the
// provider-registered-callback mode: an unset redirect_uri must not be sent as
// an empty form field, which GitHub rejects as a mismatch.
func TestExchangeCodeForTokenOmitsUnsetRedirectURI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Has("redirect_uri") {
			t.Errorf("token request must omit redirect_uri, got %q", r.Form.Get("redirect_uri"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"the-access-token","token_type":"Bearer"}`)
	}))
	defer srv.Close()

	c, err := NewClient(Config{
		Type:             "oauth2",
		ClientID:         "abc",
		ClientSecret:     "secret",
		AuthorizationURL: "https://github.example/authorize",
		TokenURL:         srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.ExchangeCodeForToken(context.Background(), "the-code"); err != nil {
		t.Fatalf("ExchangeCodeForToken: %v", err)
	}
}

func TestExchangeCodeForTokenJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "authorization_code" {
			t.Errorf("grant_type: got %q", r.Form.Get("grant_type"))
		}
		if r.Form.Get("code") != "the-code" {
			t.Errorf("code: got %q", r.Form.Get("code"))
		}
		if r.Form.Get("client_id") != "abc" {
			t.Errorf("client_id: got %q", r.Form.Get("client_id"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"the-access-token","token_type":"Bearer","id_token":"abc.def.ghi"}`)
	}))
	defer srv.Close()

	c, _ := NewClient(Config{
		Type:             "oauth2",
		ClientID:         "abc",
		ClientSecret:     "secret",
		AuthorizationURL: "https://provider/authorize",
		TokenURL:         srv.URL,
		UserinfoURL:      "https://provider/userinfo",
		RedirectURI:      "https://ragflow/cb",
	})
	tok, err := c.ExchangeCodeForToken(context.Background(), "the-code")
	if err != nil {
		t.Fatalf("ExchangeCodeForToken: %v", err)
	}
	if tok.AccessToken != "the-access-token" {
		t.Errorf("access_token: got %q", tok.AccessToken)
	}
	if tok.IDToken != "abc.def.ghi" {
		t.Errorf("id_token: got %q", tok.IDToken)
	}
}

func TestExchangeCodeForTokenFormResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-www-form-urlencoded")
		_, _ = io.WriteString(w, "access_token=abc&token_type=bearer&scope=user")
	}))
	defer srv.Close()
	c, _ := NewClient(Config{
		Type:             "oauth2",
		ClientID:         "abc",
		AuthorizationURL: "https://x/authorize",
		TokenURL:         srv.URL,
		UserinfoURL:      "https://x/userinfo",
		RedirectURI:      "https://x/cb",
	})
	tok, err := c.ExchangeCodeForToken(context.Background(), "c")
	if err != nil {
		t.Fatalf("ExchangeCodeForToken: %v", err)
	}
	if tok.AccessToken != "abc" {
		t.Errorf("access_token: got %q", tok.AccessToken)
	}
}

func TestExchangeCodeForTokenError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
	}))
	defer srv.Close()
	c, _ := NewClient(Config{
		Type:             "oauth2",
		ClientID:         "abc",
		AuthorizationURL: "https://x/authorize",
		TokenURL:         srv.URL,
		UserinfoURL:      "https://x/userinfo",
		RedirectURI:      "https://x/cb",
	})
	_, err := c.ExchangeCodeForToken(context.Background(), "c")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "HTTP 400") {
		t.Errorf("expected HTTP 400 surfaced, got %v", err)
	}
}

func TestFetchUserInfoOAuth2Normalization(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer the-token" {
			t.Errorf("Authorization header: got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"email":"alice@example.com","picture":"https://cdn/alice.png"}`)
	}))
	defer srv.Close()
	c, _ := NewClient(Config{
		Type:             "oauth2",
		ClientID:         "abc",
		AuthorizationURL: "https://x/authorize",
		TokenURL:         "https://x/token",
		UserinfoURL:      srv.URL,
		RedirectURI:      "https://x/cb",
	})
	info, err := c.FetchUserInfo(context.Background(), "the-token", "")
	if err != nil {
		t.Fatalf("FetchUserInfo: %v", err)
	}
	if info.Email != "alice@example.com" {
		t.Errorf("email: %q", info.Email)
	}
	if info.Username != "alice" {
		t.Errorf("username (fallback to email local): %q", info.Username)
	}
	if info.Nickname != "alice" {
		t.Errorf("nickname (fallback to username): %q", info.Nickname)
	}
	if info.AvatarURL != "https://cdn/alice.png" {
		t.Errorf("avatar (picture fallback): %q", info.AvatarURL)
	}
}

func TestFetchUserInfoGitHubMergesPrimaryEmail(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"login":"bob","name":"Bob Bobson","email":null,"avatar_url":"https://gh/bob.png"}`)
	})
	mux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		emails := []map[string]interface{}{
			{"email": "bob-noreply@users.noreply.github.com", "primary": false, "verified": true},
			{"email": "bob@example.com", "primary": true, "verified": true},
		}
		raw, _ := json.Marshal(emails)
		_, _ = w.Write(raw)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, _ := NewClient(Config{
		Type:         "github",
		ClientID:     "abc",
		ClientSecret: "secret",
		RedirectURI:  "https://ragflow/cb",
	})
	gh := c.(*gitHubClient)
	gh.cfg.UserinfoURL = srv.URL + "/user"

	info, err := gh.FetchUserInfo(context.Background(), "tok", "")
	if err != nil {
		t.Fatalf("FetchUserInfo: %v", err)
	}
	if info.Email != "bob@example.com" {
		t.Errorf("primary email merge failed: got %q", info.Email)
	}
	if info.Username != "bob" {
		t.Errorf("username (from login): got %q", info.Username)
	}
	if info.Nickname != "Bob Bobson" {
		t.Errorf("nickname (from name): got %q", info.Nickname)
	}
}

func TestNormalizeUserInfoEmptyEmail(t *testing.T) {
	ui := normalizeUserInfoWithClaims(map[string]interface{}{"email": ""}, Config{})
	if ui.Email != "" || ui.Username != "" || ui.Nickname != "" {
		t.Errorf("empty email should produce empty fields, got %+v", ui)
	}
}

func TestNewOAuthClientMissingFields(t *testing.T) {
	cases := []Config{
		{Type: "oauth2"},
		{Type: "oauth2", ClientID: "x"},
		{Type: "oauth2", ClientID: "x", AuthorizationURL: "https://x/a"},
	}
	for i, cfg := range cases {
		if _, err := NewClient(cfg); err == nil {
			t.Errorf("case %d: expected error for missing required field", i)
		}
	}
}

// TestRedirectURIOmittedWhenUnset pins the provider-registered-callback mode:
// GitHub matches an explicitly sent redirect_uri against the app registration,
// so an unset value must leave the parameter off both requests rather than
// failing client construction or sending an empty one.
func TestRedirectURIOmittedWhenUnset(t *testing.T) {
	// The omission lives in the shared oauthClient, so the plain oauth2 flavor
	// exercises it without GitHub's hardcoded endpoints in the way.
	cfg := Config{
		Type:             "oauth2",
		ClientID:         "abc",
		ClientSecret:     "secret",
		AuthorizationURL: "https://github.example/authorize",
		TokenURL:         "https://github.example/token",
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient without redirect_uri: %v", err)
	}

	authURL, err := client.AuthorizationURL("state-token")
	if err != nil {
		t.Fatalf("AuthorizationURL: %v", err)
	}
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	if parsed.Query().Has("redirect_uri") {
		t.Errorf("authorization URL must omit redirect_uri, got %s", authURL)
	}

	withRedirect, err := NewClient(Config{
		Type:             "oauth2",
		ClientID:         "abc",
		ClientSecret:     "secret",
		AuthorizationURL: "https://github.example/authorize",
		TokenURL:         "https://github.example/token",
		RedirectURI:      "https://ragflow.example/api/v1/auth/oauth/github/callback",
	})
	if err != nil {
		t.Fatalf("NewClient with redirect_uri: %v", err)
	}
	authURL, err = withRedirect.AuthorizationURL("state-token")
	if err != nil {
		t.Fatalf("AuthorizationURL: %v", err)
	}
	parsed, err = url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	if got := parsed.Query().Get("redirect_uri"); got != "https://ragflow.example/api/v1/auth/oauth/github/callback" {
		t.Errorf("redirect_uri = %q, want the configured callback", got)
	}
}

func TestNewOIDCMissingIssuer(t *testing.T) {
	_, err := NewClient(Config{Type: "oidc"})
	if err == nil || !strings.Contains(err.Error(), "missing issuer") {
		t.Errorf("expected missing issuer error, got %v", err)
	}
}
