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
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// resetWecomTokenCache drops every cached app access token so a test cannot
// inherit one minted against an earlier stub server.
func resetWecomTokenCache() {
	wecomAppTokens.Lock()
	wecomAppTokens.entries = map[string]wecomTokenEntry{}
	wecomAppTokens.Unlock()
}

func TestWecomAuthorizationURL(t *testing.T) {
	c, err := NewClient(Config{
		Type: "wecom",
		// Defaults must be usable without configuring any URL.
		ClientID:     "ww_corp_id",
		ClientSecret: "app_secret",
		AgentID:      "1000002",
		RedirectURI:  "https://ragflow.example/api/v1/auth/oauth/wecom/callback",
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
	if got, want := u.Scheme+"://"+u.Host+u.Path, wecomAuthorizationURL; got != want {
		t.Errorf("authorization endpoint = %q, want %q", got, want)
	}
	q := u.Query()
	if got := q.Get("login_type"); got != "CorpApp" {
		t.Errorf("login_type = %q, want CorpApp", got)
	}
	// WeCom names the tenant identifier `appid` and the application id
	// `agentid`; the vanilla `client_id`/`response_type` spellings are not
	// understood by wwlogin.
	if got := q.Get("appid"); got != "ww_corp_id" {
		t.Errorf("appid = %q, want ww_corp_id", got)
	}
	if got := q.Get("agentid"); got != "1000002" {
		t.Errorf("agentid = %q, want 1000002", got)
	}
	if got := q.Get("state"); got != "state-value" {
		t.Errorf("state = %q, want state-value", got)
	}
	if got := q.Get("redirect_uri"); got != "https://ragflow.example/api/v1/auth/oauth/wecom/callback" {
		t.Errorf("redirect_uri = %q", got)
	}
	if got := q.Get("client_id"); got != "" {
		t.Errorf("client_id = %q, wwlogin takes appid instead", got)
	}
	if got := q.Get("response_type"); got != "" {
		t.Errorf("response_type = %q, wwlogin takes no response_type", got)
	}
}

func TestWecomNewClientRequiresAgentID(t *testing.T) {
	_, err := NewClient(Config{
		Type:         "wecom",
		ClientID:     "ww_corp_id",
		ClientSecret: "app_secret",
	})
	if err == nil {
		t.Fatal("expected an error when agent_id is missing")
	}
	if !strings.Contains(err.Error(), "agent_id") {
		t.Errorf("error %q should name agent_id", err)
	}
}

// wecomStubServer answers the three WeCom endpoints a login needs. counters
// records how often the token and code endpoints were hit so the cache
// behaviour can be asserted.
type wecomStubServer struct {
	gettokenCalls    int
	getuserinfoCalls int
	userCalls        int
	getuserinfoResp  string
	userResp         string
}

func (s *wecomStubServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	s.getuserinfoResp = `{"errcode":0,"errmsg":"ok","userid":"zhangsan","user_ticket":"ticket"}`
	s.userResp = `{
		"errcode": 0,
		"errmsg": "ok",
		"userid": "zhangsan",
		"name": "张三",
		"avatar": "https://avatar.example/zhangsan.png",
		"biz_mail": "zhangsan@corp.example.com"
	}`
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			s.gettokenCalls++
			if r.URL.Query().Get("corpid") != "ww_corp_id" || r.URL.Query().Get("corpsecret") != "app_secret" {
				_, _ = w.Write([]byte(`{"errcode":40013,"errmsg":"invalid corpid"}`))
				return
			}
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok","access_token":"app-token","expires_in":7200}`))
		case "/cgi-bin/auth/getuserinfo":
			s.getuserinfoCalls++
			q := r.URL.Query()
			if q.Get("access_token") != "app-token" || q.Get("code") != "code-from-wecom" {
				_, _ = w.Write([]byte(`{"errcode":40029,"errmsg":"invalid code"}`))
				return
			}
			_, _ = w.Write([]byte(s.getuserinfoResp))
		case "/cgi-bin/user/get":
			s.userCalls++
			if r.URL.Query().Get("access_token") != "app-token" || r.URL.Query().Get("userid") == "" {
				_, _ = w.Write([]byte(`{"errcode":60111,"errmsg":"userid not found"}`))
				return
			}
			_, _ = w.Write([]byte(s.userResp))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestWecomExchangeCodeForTokenAndFetchUserInfo(t *testing.T) {
	resetWecomTokenCache()
	stub := &wecomStubServer{}
	srv := stub.start(t)
	defer srv.Close()

	c, err := NewClient(Config{
		Type:         "wecom",
		ClientID:     "ww_corp_id",
		ClientSecret: "app_secret",
		AgentID:      "1000002",
		TokenURL:     srv.URL + "/cgi-bin/gettoken",
		UserinfoURL:  srv.URL + "/cgi-bin/auth/getuserinfo",
		DirectoryURL: srv.URL + "/cgi-bin/user/get",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// There is no per-user token exchange: the code is resolved against the
	// application access token, and the userid rides in IDToken because
	// FetchUserInfo consumes exactly the pair the callback hands it.
	token, err := c.ExchangeCodeForToken(context.Background(), "code-from-wecom")
	if err != nil {
		t.Fatalf("ExchangeCodeForToken: %v", err)
	}
	if token.AccessToken != "app-token" {
		t.Errorf("AccessToken = %q, want the application token", token.AccessToken)
	}
	if token.IDToken != "zhangsan" {
		t.Errorf("IDToken = %q, want the resolved userid", token.IDToken)
	}
	if stub.gettokenCalls != 1 || stub.getuserinfoCalls != 1 {
		t.Errorf("gettoken=%d getuserinfo=%d, want 1 each", stub.gettokenCalls, stub.getuserinfoCalls)
	}

	info, err := c.FetchUserInfo(context.Background(), token.AccessToken, token.IDToken)
	if err != nil {
		t.Fatalf("FetchUserInfo: %v", err)
	}
	// Login binding is by biz_mail only; the personal email field is
	// deliberately ignored.
	if info.Email != "zhangsan@corp.example.com" {
		t.Errorf("Email = %q, want biz_mail", info.Email)
	}
	if info.Username != "zhangsan" {
		t.Errorf("Username = %q, want the userid (a Chinese name makes a poor username)", info.Username)
	}
	if info.Nickname != "张三" {
		t.Errorf("Nickname = %q", info.Nickname)
	}
	if info.AvatarURL != "https://avatar.example/zhangsan.png" {
		t.Errorf("AvatarURL = %q", info.AvatarURL)
	}
	if info.Subject != "zhangsan" {
		t.Errorf("Subject = %q, want the userid", info.Subject)
	}
	if info.EmailVerified {
		t.Error("EmailVerified should stay false: this flow issues no id_token")
	}
}

// TestWecomAppTokenCachedAcrossLogins asserts the rate-limit-driven cache:
// gettoken is issued once per credential pair, and a second login (a fresh
// client instance, as the callback builds one) reuses it.
func TestWecomAppTokenCachedAcrossLogins(t *testing.T) {
	resetWecomTokenCache()
	stub := &wecomStubServer{}
	srv := stub.start(t)
	defer srv.Close()

	newCfg := func() Config {
		return Config{
			Type:         "wecom",
			ClientID:     "ww_corp_id",
			ClientSecret: "app_secret",
			AgentID:      "1000002",
			TokenURL:     srv.URL + "/cgi-bin/gettoken",
			UserinfoURL:  srv.URL + "/cgi-bin/auth/getuserinfo",
			DirectoryURL: srv.URL + "/cgi-bin/user/get",
		}
	}

	first, err := NewClient(newCfg())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err = first.ExchangeCodeForToken(context.Background(), "code-from-wecom"); err != nil {
		t.Fatalf("first exchange: %v", err)
	}

	second, err := NewClient(newCfg())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err = second.ExchangeCodeForToken(context.Background(), "code-from-wecom"); err != nil {
		t.Fatalf("second exchange: %v", err)
	}

	if stub.gettokenCalls != 1 {
		t.Errorf("gettoken = %d calls, want 1 (the app token must be cached)", stub.gettokenCalls)
	}
	if stub.getuserinfoCalls != 2 {
		t.Errorf("getuserinfo = %d calls, want 2 (one per login)", stub.getuserinfoCalls)
	}

	// A rotated secret must not ride the old cache entry.
	rotated, err := NewClient(Config{
		Type:         "wecom",
		ClientID:     "ww_corp_id",
		ClientSecret: "app_secret_rotated",
		AgentID:      "1000002",
		TokenURL:     srv.URL + "/cgi-bin/gettoken",
		UserinfoURL:  srv.URL + "/cgi-bin/auth/getuserinfo",
		DirectoryURL: srv.URL + "/cgi-bin/user/get",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	// The stub only accepts app_secret, so the rotated secret surfaces as a
	// rejected gettoken call — which proves the cache was bypassed.
	if _, err = rotated.ExchangeCodeForToken(context.Background(), "code-from-wecom"); err == nil {
		t.Fatal("expected the rotated secret to miss the cache and hit gettoken")
	}
	if stub.gettokenCalls != 2 {
		t.Errorf("gettoken = %d calls, want 2 (rotated secret must refetch)", stub.gettokenCalls)
	}
}

// TestWecomFetchUserInfoWithoutBizMail covers the tenant that has not enabled
// the mailbox service: the directory answers successfully, the email stays
// empty, and the callback turns that into email_missing. The call itself must
// not fail, or the operator would lose the email_missing signal.
func TestWecomFetchUserInfoWithoutBizMail(t *testing.T) {
	resetWecomTokenCache()
	stub := &wecomStubServer{}
	srv := stub.start(t)
	defer srv.Close()
	stub.userResp = `{"errcode":0,"errmsg":"ok","userid":"lisi","name":"李四","biz_mail":""}`

	c, err := NewClient(Config{
		Type:         "wecom",
		ClientID:     "ww_corp_id",
		ClientSecret: "app_secret",
		AgentID:      "1000002",
		TokenURL:     srv.URL + "/cgi-bin/gettoken",
		UserinfoURL:  srv.URL + "/cgi-bin/auth/getuserinfo",
		DirectoryURL: srv.URL + "/cgi-bin/user/get",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	info, err := c.FetchUserInfo(context.Background(), "app-token", "lisi")
	if err != nil {
		t.Fatalf("FetchUserInfo: %v", err)
	}
	if info.Email != "" {
		t.Errorf("Email = %q, want empty (no biz_mail means nothing to bind)", info.Email)
	}
	if info.Subject != "lisi" {
		t.Errorf("Subject = %q", info.Subject)
	}
}

func TestWecomExchangeCodeForTokenRejected(t *testing.T) {
	resetWecomTokenCache()
	stub := &wecomStubServer{}
	srv := stub.start(t)
	defer srv.Close()
	stub.getuserinfoResp = `{"errcode":40029,"errmsg":"invalid code"}`

	c, err := NewClient(Config{
		Type:         "wecom",
		ClientID:     "ww_corp_id",
		ClientSecret: "app_secret",
		AgentID:      "1000002",
		TokenURL:     srv.URL + "/cgi-bin/gettoken",
		UserinfoURL:  srv.URL + "/cgi-bin/auth/getuserinfo",
		DirectoryURL: srv.URL + "/cgi-bin/user/get",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = c.ExchangeCodeForToken(context.Background(), "code-from-wecom")
	if err == nil {
		t.Fatal("expected an error for a rejected code")
	}
	for _, want := range []string{"40029", "invalid code"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should name %q", err, want)
		}
	}
}

func TestWecomFetchUserInfoRejected(t *testing.T) {
	resetWecomTokenCache()
	stub := &wecomStubServer{}
	srv := stub.start(t)
	defer srv.Close()
	stub.userResp = `{"errcode":60011,"errmsg":"no privilege to access/contact data"}`

	c, err := NewClient(Config{
		Type:         "wecom",
		ClientID:     "ww_corp_id",
		ClientSecret: "app_secret",
		AgentID:      "1000002",
		TokenURL:     srv.URL + "/cgi-bin/gettoken",
		UserinfoURL:  srv.URL + "/cgi-bin/auth/getuserinfo",
		DirectoryURL: srv.URL + "/cgi-bin/user/get",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = c.FetchUserInfo(context.Background(), "app-token", "zhangsan")
	if err == nil {
		t.Fatal("expected an error for a permission-rejected directory read")
	}
	if !strings.Contains(err.Error(), "60011") {
		t.Errorf("error %q should name the WeCom code", err)
	}
}
