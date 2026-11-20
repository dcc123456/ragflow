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

func TestDingtalkAuthorizationURL(t *testing.T) {
	c, err := NewClient(Config{
		Type: "dingtalk",
		// Defaults must be usable without configuring any URL.
		ClientID:     "ding_app_key",
		ClientSecret: "app_secret",
		RedirectURI:  "https://ragflow.example/api/v1/auth/oauth/dingtalk/callback",
		Scope:        "openid corpid",
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
	if got, want := u.Scheme+"://"+u.Host+u.Path, dingtalkAuthorizationURL; got != want {
		t.Errorf("authorization endpoint = %q, want %q", got, want)
	}
	q := u.Query()
	if got := q.Get("client_id"); got != "ding_app_key" {
		t.Errorf("client_id = %q, want %q", got, "ding_app_key")
	}
	if got := q.Get("response_type"); got != "code" {
		t.Errorf("response_type = %q, want code", got)
	}
	if got := q.Get("state"); got != "state-value" {
		t.Errorf("state = %q, want state-value", got)
	}
	if got := q.Get("redirect_uri"); got != "https://ragflow.example/api/v1/auth/oauth/dingtalk/callback" {
		t.Errorf("redirect_uri = %q", got)
	}
	if got := q.Get("scope"); got != "openid corpid" {
		t.Errorf("scope = %q", got)
	}
}

func TestDingtalkScopeDefaultsWhenEmpty(t *testing.T) {
	c, err := NewClient(Config{
		Type:         "dingtalk",
		ClientID:     "ding_app_key",
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
	if got := u.Query().Get("scope"); got != dingtalkDefaultScope {
		t.Errorf("scope = %q, want default %q", got, dingtalkDefaultScope)
	}
}

// TestDingtalkExchangeCodeForTokenSendsJSON asserts both directions of the
// format difference from a vanilla OAuth2 provider: DingTalk wants a JSON
// body and answers with camelCase claims rather than snake_case ones.
func TestDingtalkExchangeCodeForTokenSendsJSON(t *testing.T) {
	var gotMethod, gotContentType, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotMethod, gotContentType, gotBody = r.Method, r.Header.Get("Content-Type"), string(body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"expireIn":     7200,
			"accessToken":  "u-token",
			"refreshToken": "u-refresh",
			"corpId":       "ding_org",
			"scope":        "openid",
		})
	}))
	defer srv.Close()

	c, err := NewClient(Config{
		Type:         "dingtalk",
		ClientID:     "ding_app_key",
		ClientSecret: "app_secret",
		TokenURL:     srv.URL,
		RedirectURI:  "https://ragflow.example/api/v1/auth/oauth/dingtalk/callback",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	token, err := c.ExchangeCodeForToken(context.Background(), "code-from-dingtalk")
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
	for _, want := range []string{`"grant_type":"authorization_code"`, `"client_id":"ding_app_key"`, `"code":"code-from-dingtalk"`, `"redirect_uri":"https://ragflow.example/api/v1/auth/oauth/dingtalk/callback"`} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("body %q missing %q", gotBody, want)
		}
	}
	if token.AccessToken != "u-token" || token.ExpiresIn != 7200 {
		t.Errorf("token = %+v", token)
	}
}

// TestDingtalkExchangeCodeForTokenRejected covers the failure mode that is
// easy to get wrong: DingTalk answers a rejected exchange with HTTP 400 and
// an `error` document, whose reason has to be lifted out of the body.
func TestDingtalkExchangeCodeForTokenRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"InvalidAuthenticationCode","error_description":"invalid grant"}`))
	}))
	defer srv.Close()

	c, err := NewClient(Config{
		Type: "dingtalk", ClientID: "cli", ClientSecret: "secret", TokenURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = c.ExchangeCodeForToken(context.Background(), "expired-code")
	if err == nil {
		t.Fatal("expected an error for a rejected exchange")
	}
	for _, want := range []string{"InvalidAuthenticationCode", "invalid grant"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should name %q", err, want)
		}
	}
}

// TestDingtalkFetchUserInfoUsesDingtalkHeader asserts the DingTalk-specific
// detail: the access token rides on `x-acs-dingtalk-access-token`, not on the
// Authorization header, and the claims use DingTalk's own field names.
func TestDingtalkFetchUserInfoUsesDingtalkHeader(t *testing.T) {
	var gotDingtalkToken, gotAuth, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotDingtalkToken = r.Header.Get("x-acs-dingtalk-access-token")
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"nick": "李四",
			"email": "lisi@example.com",
			"mobile": "+8613800000000",
			"avatarUrl": "https://avatar.example/lisi.png",
			"openId": "dt_open",
			"unionId": "dt_union",
			"stateCode": "+86"
		}`))
	}))
	defer srv.Close()

	c, err := NewClient(Config{
		Type: "dingtalk", ClientID: "cli", ClientSecret: "secret", UserinfoURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	info, err := c.FetchUserInfo(context.Background(), "u-token", "")
	if err != nil {
		t.Fatalf("FetchUserInfo: %v", err)
	}
	if gotDingtalkToken != "u-token" {
		t.Errorf("x-acs-dingtalk-access-token = %q, want u-token", gotDingtalkToken)
	}
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want empty: DingTalk authenticates through its own header", gotAuth)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q", gotAccept)
	}
	if info.Email != "lisi@example.com" {
		t.Errorf("Email = %q", info.Email)
	}
	if info.Nickname != "李四" {
		t.Errorf("Nickname = %q, want nick preference", info.Nickname)
	}
	if info.AvatarURL != "https://avatar.example/lisi.png" {
		t.Errorf("AvatarURL = %q", info.AvatarURL)
	}
	if info.Subject != "dt_union" {
		t.Errorf("Subject = %q, want unionId preference", info.Subject)
	}
	if info.EmailVerified {
		t.Error("EmailVerified should stay false: this flow issues no id_token")
	}
}

// TestDingtalkFetchUserInfoFallsBackToOpenID covers the union id being absent
// for app-scoped identities and the localized nick being the only name
// available, which leaves the email local part as the username.
func TestDingtalkFetchUserInfoFallsBackToOpenID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"nick":"王五","email":"wu@example.com","openId":"dt_only"}`))
	}))
	defer srv.Close()

	c, err := NewClient(Config{
		Type: "dingtalk", ClientID: "cli", ClientSecret: "secret", UserinfoURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	info, err := c.FetchUserInfo(context.Background(), "u-token", "")
	if err != nil {
		t.Fatalf("FetchUserInfo: %v", err)
	}
	if info.Subject != "dt_only" {
		t.Errorf("Subject = %q, want openId fallback", info.Subject)
	}
	if info.Username != "wu" {
		t.Errorf("Username = %q, want email local part fallback", info.Username)
	}
	if info.Nickname != "王五" {
		t.Errorf("Nickname = %q", info.Nickname)
	}
}

// TestDingtalkFetchUserInfoRejected covers a permission error:
// /contact/users/me answers with its own envelope rather than an OAuth error
// document.
func TestDingtalkFetchUserInfoRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"Forbidden.AccessDenied.ResourceIsRestricted","message":"access denied: contact permission not granted"}`))
	}))
	defer srv.Close()

	c, err := NewClient(Config{
		Type: "dingtalk", ClientID: "cli", ClientSecret: "secret", UserinfoURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = c.FetchUserInfo(context.Background(), "u-token", "")
	if err == nil {
		t.Fatal("expected an error for a rejected userinfo call")
	}
	for _, want := range []string{"Forbidden.AccessDenied.ResourceIsRestricted", "contact permission not granted"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should name %q", err, want)
		}
	}
}
