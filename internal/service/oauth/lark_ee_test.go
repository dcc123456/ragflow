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

func TestLarkAuthorizationURL(t *testing.T) {
	c, err := NewClient(Config{
		Type: "lark",
		// Defaults must be usable without configuring any URL.
		ClientID:     "cli_app_id",
		ClientSecret: "app_secret",
		RedirectURI:  "https://ragflow.example/api/v1/auth/oauth/feishu/callback",
		Scope:        "auth:user.id:read",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	raw, err := c.AuthorizationURL("state-value")
	if err != nil {
		t.Fatalf("AuthorizationURL: %v", err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("bad URL: %v", err)
	}
	if got, want := u.Scheme+"://"+u.Host+u.Path, larkAuthorizationURL; got != want {
		t.Errorf("authorization endpoint = %q, want %q", got, want)
	}
	q := u.Query()
	// Feishu names the tenant identifier `client_id`; `app_id` is the older
	// alias and is rejected for apps created after the OAuth 2.0 rollout.
	if got := q.Get("client_id"); got != "cli_app_id" {
		t.Errorf("client_id = %q, want %q", got, "cli_app_id")
	}
	if got := q.Get("response_type"); got != "code" {
		t.Errorf("response_type = %q, want code", got)
	}
	if got := q.Get("state"); got != "state-value" {
		t.Errorf("state = %q, want state-value", got)
	}
	if got := q.Get("redirect_uri"); got != "https://ragflow.example/api/v1/auth/oauth/feishu/callback" {
		t.Errorf("redirect_uri = %q", got)
	}
	if got := q.Get("scope"); got != "auth:user.id:read" {
		t.Errorf("scope = %q", got)
	}
}

func TestLarkScopeDefaultsWhenEmpty(t *testing.T) {
	c, err := NewClient(Config{
		Type:         "lark",
		ClientID:     "cli_app_id",
		ClientSecret: "app_secret",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	raw, err := c.AuthorizationURL("state-value")
	if err != nil {
		t.Fatalf("AuthorizationURL: %v", err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("bad URL: %v", err)
	}
	if got := u.Query().Get("scope"); got != larkDefaultScope {
		t.Errorf("scope = %q, want default %q", got, larkDefaultScope)
	}
}

// TestLarkExchangeCodeForTokenSendsJSON asserts both directions of the format
// difference from a vanilla OAuth2 provider: Feishu wants a JSON body and
// answers with top-level claims rather than a form and an envelope-less body.
func TestLarkExchangeCodeForTokenSendsJSON(t *testing.T) {
	var gotMethod, gotContentType, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotMethod, gotContentType, gotBody = r.Method, r.Header.Get("Content-Type"), string(body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":          0,
			"access_token":  "u-token",
			"token_type":    "Bearer",
			"expires_in":    7200,
			"refresh_token": "u-refresh",
			"scope":         "auth:user.id:read",
		})
	}))
	defer srv.Close()

	c, err := NewClient(Config{
		Type:         "lark",
		ClientID:     "cli_app_id",
		ClientSecret: "app_secret",
		TokenURL:     srv.URL,
		RedirectURI:  "https://ragflow.example/api/v1/auth/oauth/feishu/callback",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	token, err := c.ExchangeCodeForToken(context.Background(), "code-from-feishu")
	if err != nil {
		t.Fatalf("ExchangeCodeForToken: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if !strings.HasPrefix(gotContentType, "application/json") {
		t.Errorf("content type = %q, want application/json", gotContentType)
	}
	// A form body would arrive URL-encoded, which is what the shared OAuth2
	// client sends and what this endpoint rejects.
	if strings.Contains(gotBody, "grant_type=") {
		t.Errorf("body looks form-encoded: %q", gotBody)
	}
	for _, want := range []string{`"grant_type":"authorization_code"`, `"client_id":"cli_app_id"`, `"code":"code-from-feishu"`, `"redirect_uri":"https://ragflow.example/api/v1/auth/oauth/feishu/callback"`} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("body %q missing %q", gotBody, want)
		}
	}
	if token.AccessToken != "u-token" || token.ExpiresIn != 7200 || token.TokenType != "Bearer" {
		t.Errorf("token = %+v", token)
	}
}

// TestLarkExchangeCodeForTokenWithoutRedirectURI covers the field being
// optional: some application kinds register the callback out of band and must
// not have the parameter sent at all.
func TestLarkExchangeCodeForTokenWithoutRedirectURI(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"access_token":"u-token","expires_in":7200}`))
	}))
	defer srv.Close()

	c, err := NewClient(Config{
		Type:         "lark",
		ClientID:     "cli_app_id",
		ClientSecret: "app_secret",
		TokenURL:     srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.ExchangeCodeForToken(context.Background(), "code"); err != nil {
		t.Fatalf("ExchangeCodeForToken: %v", err)
	}
	if strings.Contains(gotBody, "redirect_uri") {
		t.Errorf("body should omit redirect_uri: %q", gotBody)
	}
}

// TestLarkExchangeCodeForTokenRejected covers the failure mode that is easy to
// get wrong: Feishu answers a rejected exchange with HTTP 200 and a non-zero
// `code`, so the reason has to come from the envelope.
func TestLarkExchangeCodeForTokenRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":20005,"msg":"invalid authorization code"}`))
	}))
	defer srv.Close()

	c, err := NewClient(Config{
		Type: "lark", ClientID: "cli", ClientSecret: "secret", TokenURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = c.ExchangeCodeForToken(context.Background(), "expired-code")
	if err == nil {
		t.Fatal("expected an error for a rejected exchange")
	}
	for _, want := range []string{"20005", "invalid authorization code"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should name %q", err, want)
		}
	}
}

// TestLarkFetchUserInfoUnwrapsData asserts the second Feishu-specific detail:
// the profile is nested under `data` and uses Feishu's own field names.
func TestLarkFetchUserInfoUnwrapsData(t *testing.T) {
	var gotAuth, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotAccept = r.Header.Get("Authorization"), r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 0,
			"msg": "success",
			"data": {
				"name": "李四",
				"en_name": "lisi",
				"email": "lisi@example.com",
				"mobile": "+8613800000000",
				"avatar_url": "https://avatar.example/lisi.png",
				"open_id": "ou_open",
				"union_id": "on_union",
				"user_id": "user_1"
			}
		}`))
	}))
	defer srv.Close()

	c, err := NewClient(Config{
		Type: "lark", ClientID: "cli", ClientSecret: "secret", UserinfoURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	info, err := c.FetchUserInfo(context.Background(), "u-token", "")
	if err != nil {
		t.Fatalf("FetchUserInfo: %v", err)
	}
	if gotAuth != "Bearer u-token" {
		t.Errorf("Authorization = %q, want Bearer u-token", gotAuth)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q", gotAccept)
	}
	if info.Email != "lisi@example.com" {
		t.Errorf("Email = %q", info.Email)
	}
	if info.Username != "lisi" {
		t.Errorf("Username = %q, want en_name preference", info.Username)
	}
	if info.Nickname != "李四" {
		t.Errorf("Nickname = %q, want name preference", info.Nickname)
	}
	if info.AvatarURL != "https://avatar.example/lisi.png" {
		t.Errorf("AvatarURL = %q", info.AvatarURL)
	}
	if info.Subject != "on_union" {
		t.Errorf("Subject = %q, want union_id preference", info.Subject)
	}
	if info.EmailVerified {
		t.Error("EmailVerified should stay false: this flow issues no id_token")
	}
}

// TestLarkFetchUserInfoFallsBackToOpenID covers the union id being absent for
// app-scoped identities and the localized name being the only name available.
func TestLarkFetchUserInfoFallsBackToOpenID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"name":"王五","email":"wu@example.com","open_id":"ou_only"}}`))
	}))
	defer srv.Close()

	c, err := NewClient(Config{
		Type: "lark", ClientID: "cli", ClientSecret: "secret", UserinfoURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	info, err := c.FetchUserInfo(context.Background(), "u-token", "")
	if err != nil {
		t.Fatalf("FetchUserInfo: %v", err)
	}
	if info.Subject != "ou_only" {
		t.Errorf("Subject = %q, want open_id fallback", info.Subject)
	}
	if info.Username != "wu" {
		t.Errorf("Username = %q, want email local part fallback", info.Username)
	}
	if info.Nickname != "王五" {
		t.Errorf("Nickname = %q", info.Nickname)
	}
}

// TestLarkFetchUserInfoFallsBackToEnterpriseEmail covers the tenant shape where
// the directory mailbox is empty and the address lives in the Feishu mailbox
// field, which would otherwise surface as email_missing.
func TestLarkFetchUserInfoFallsBackToEnterpriseEmail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"name":"赵六","en_name":"zhaoliu","enterprise_email":"zhaoliu@corp.example.com","open_id":"ou_mail"}}`))
	}))
	defer srv.Close()

	c, err := NewClient(Config{
		Type: "lark", ClientID: "cli", ClientSecret: "secret", UserinfoURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	info, err := c.FetchUserInfo(context.Background(), "u-token", "")
	if err != nil {
		t.Fatalf("FetchUserInfo: %v", err)
	}
	if info.Email != "zhaoliu@corp.example.com" {
		t.Errorf("Email = %q, want the enterprise_email fallback", info.Email)
	}
	if info.Username != "zhaoliu" {
		t.Errorf("Username = %q", info.Username)
	}
}

// TestLarkFetchUserInfoPrefersDirectoryEmail asserts the fallback does not
// displace the field Feishu documents first.
func TestLarkFetchUserInfoPrefersDirectoryEmail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"email":"dir@example.com","enterprise_email":"mail@corp.example.com"}}`))
	}))
	defer srv.Close()

	c, err := NewClient(Config{
		Type: "lark", ClientID: "cli", ClientSecret: "secret", UserinfoURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	info, err := c.FetchUserInfo(context.Background(), "u-token", "")
	if err != nil {
		t.Fatalf("FetchUserInfo: %v", err)
	}
	if info.Email != "dir@example.com" {
		t.Errorf("Email = %q, want the directory email", info.Email)
	}
}

// TestLarkFetchUserInfoRejected covers a permission error: /authen/v1/user_info
// answers with its own envelope rather than an OAuth error document.
func TestLarkFetchUserInfoRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":99991663,"msg":"permission denied: contact permission not granted"}`))
	}))
	defer srv.Close()

	c, err := NewClient(Config{
		Type: "lark", ClientID: "cli", ClientSecret: "secret", UserinfoURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = c.FetchUserInfo(context.Background(), "u-token", "")
	if err == nil {
		t.Fatal("expected an error for a rejected userinfo call")
	}
	if !strings.Contains(err.Error(), "99991663") {
		t.Errorf("error %q should name the Feishu code", err)
	}
}
