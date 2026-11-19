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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"ragflow/internal/common"

	"go.uber.org/zap"
)

// WeCom endpoints. The consent page is served from
// login.work.weixin.qq.com while the API lives on qyapi.weixin.qq.com, so
// the two hosts differ.
const (
	wecomAuthorizationURL = "https://login.work.weixin.qq.com/wwlogin/sso/login"
	wecomGetTokenURL      = "https://qyapi.weixin.qq.com/cgi-bin/gettoken"
	wecomGetuserinfoURL   = "https://qyapi.weixin.qq.com/cgi-bin/auth/getuserinfo"
	// wecomDirectoryURL is the contact directory endpoint. Directory reads
	// require the app to have been granted the contact permission in the
	// WeCom admin console.
	wecomDirectoryURL = "https://qyapi.weixin.qq.com/cgi-bin/user/get"
)

// wecomTokenRefreshEarly mirrors WeCom's own guidance: renew the app access
// token five minutes before its recorded expiry instead of racing the clock.
const wecomTokenRefreshEarly = 5 * time.Minute

// wecomClient implements the WeCom self-built-application (CorpApp) login
// flow.
//
// WeCom deviates from a vanilla OAuth2 provider in three ways:
//
//   - the consent page takes `appid` (the corp id) and `agentid` (the app id)
//     instead of `client_id`/`response_type`, and there is no scope list;
//   - there is no per-user token exchange. The browser-scanned `code` is
//     resolved with an application-level access token obtained from
//     corpid+secret (/cgi-bin/gettoken), which WeCom shares across all users
//     and rate-limits, so it is cached in-process;
//   - the code only yields a `userid`; the profile (name, avatar, biz_mail)
//     comes from the contact directory (/cgi-bin/user/get).
//
// The flow issues no id_token, so EmailVerified stays false and the callback's
// email-verified guard, which targets OIDC channels, does not apply — the
// same position GitHub is in, where whoever controls the app vouches for its
// members.
type wecomClient struct {
	*oauthClient
}

func init() {
	registerProvider("wecom", func(cfg Config) (Client, error) {
		return newWecomClient(cfg)
	})
}

func newWecomClient(cfg Config) (*wecomClient, error) {
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("wecom: client_id (corp id) is required")
	}
	if cfg.ClientSecret == "" {
		return nil, fmt.Errorf("wecom: client_secret (app secret) is required")
	}
	if cfg.AgentID == "" {
		return nil, fmt.Errorf("wecom: agent_id is required")
	}
	// Existing values are kept so an operator can point the channel at a
	// proxied deployment and so tests can inject a stub endpoint.
	if cfg.AuthorizationURL == "" {
		cfg.AuthorizationURL = wecomAuthorizationURL
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = wecomGetTokenURL
	}
	if cfg.UserinfoURL == "" {
		cfg.UserinfoURL = wecomGetuserinfoURL
	}
	if cfg.DirectoryURL == "" {
		cfg.DirectoryURL = wecomDirectoryURL
	}
	base, err := newOAuthClient(cfg)
	if err != nil {
		return nil, err
	}
	return &wecomClient{oauthClient: base}, nil
}

// AuthorizationURL builds the wwlogin consent URL. WeCom names its parameters
// `appid`/`agentid` and takes no scope, so the shared oauthClient builder is
// bypassed.
func (c *wecomClient) AuthorizationURL(state string) (string, error) {
	params := url.Values{}
	params.Set("login_type", "CorpApp")
	params.Set("appid", c.cfg.ClientID)
	params.Set("agentid", c.cfg.AgentID)
	if c.cfg.RedirectURI != "" {
		params.Set("redirect_uri", c.cfg.RedirectURI)
	}
	if state != "" {
		params.Set("state", state)
	}
	sep := "?"
	if strings.Contains(c.cfg.AuthorizationURL, "?") {
		sep = "&"
	}
	return c.cfg.AuthorizationURL + sep + params.Encode(), nil
}

// ExchangeCodeForToken resolves the browser-scanned `code` against the
// directory API. There is no per-user token to exchange, so the returned
// TokenResponse carries the application access token in AccessToken and the
// resolved userid in IDToken — the pair of values FetchUserInfo consumes.
func (c *wecomClient) ExchangeCodeForToken(ctx context.Context, code string) (*TokenResponse, error) {
	appToken, err := c.appAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange authorization code for token: %w", err)
	}

	body, err := c.getJSON(ctx, c.cfg.UserinfoURL, url.Values{
		"access_token": {appToken},
		"code":         {code},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to exchange authorization code for token: %w", err)
	}
	var resp wecomGetuserinfoResponse
	if jerr := json.Unmarshal(body, &resp); jerr != nil {
		return nil, fmt.Errorf("failed to exchange authorization code for token: parse response: %w", jerr)
	}
	if resp.ErrCode != 0 {
		return nil, fmt.Errorf("failed to exchange authorization code for token: %s", wecomEnvelopeError(resp.ErrCode, resp.ErrMsg))
	}
	if resp.UserID == "" {
		// getuserinfo answers errcode 0 without a userid for external users,
		// who do not exist in the directory and can never log in here.
		return nil, fmt.Errorf("failed to exchange authorization code for token: no userid in response")
	}
	common.Info("wecom code resolved to a directory member",
		zap.String("channel", c.cfg.Channel),
		zap.String("userid", resp.UserID),
	)
	return &TokenResponse{
		AccessToken: appToken,
		IDToken:     resp.UserID,
		TokenType:   "wecom",
	}, nil
}

// FetchUserInfo loads the member profile from the contact directory.
// accessToken is the application access token and idToken is the userid
// produced by ExchangeCodeForToken.
//
// The mailbox policy is deliberate: only `biz_mail` (the tenant's WeCom
// mailbox) is accepted. Login binding is by email, and a member without the
// mailbox service cannot be bound to anything unambiguous, so the email stays
// empty and the callback reports email_missing. A deployment whose members
// have no biz_mail needs to enable the mailbox service first.
func (c *wecomClient) FetchUserInfo(ctx context.Context, accessToken, idToken string) (*UserInfo, error) {
	if idToken == "" {
		return nil, fmt.Errorf("failed to fetch user info: no userid resolved from the code")
	}
	body, err := c.getJSON(ctx, c.cfg.DirectoryURL, url.Values{
		"access_token": {accessToken},
		"userid":       {idToken},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user info: %w", err)
	}
	var resp wecomUserResponse
	if jerr := json.Unmarshal(body, &resp); jerr != nil {
		return nil, fmt.Errorf("failed to fetch user info: parse response: %w", jerr)
	}
	if resp.ErrCode != 0 {
		return nil, fmt.Errorf("failed to fetch user info: %s", wecomEnvelopeError(resp.ErrCode, resp.ErrMsg))
	}
	if resp.BizMail == "" {
		// The directory answers successfully for a member without the
		// mailbox service; the callback can then only report email_missing.
		// The userid is what an operator needs to find that member; the body
		// is deliberately not logged, since it carries personal fields.
		common.Warn("wecom directory entry carries no biz_mail",
			zap.String("channel", c.cfg.Channel),
			zap.String("userid", resp.UserID),
		)
	}

	claims := map[string]interface{}{
		"biz_mail":   resp.BizMail,
		"name":       resp.Name,
		"avatar_url": resp.AvatarURL,
		"userid":     resp.UserID,
	}
	info := normalizeUserInfoWithClaims(claims, Config{
		// Only biz_mail binds an identity: the personal `email` field a
		// member may have bound in WeCom is a private mailbox we do not
		// trust as an account anchor.
		EmailClaims: []string{"biz_mail"},
		// The userid is stable and unique within the corp; the display name
		// is usually a Chinese personal name and is already carried by
		// Nickname, so it makes a poor username.
		UsernameClaims: []string{"userid"},
		NicknameClaims: []string{"name"},
		AvatarClaims:   []string{"avatar_url"},
	})
	info.Subject = resp.UserID
	return info, nil
}

// appAccessToken returns the application access token, refreshing and caching
// it when the cached copy is missing or within the refresh window.
//
// WeCom issues one token per corp+secret pair, rate-limits its issuance, and
// keeps it valid for 7200 seconds, so the cache is keyed on the credential
// pair (not the channel name) and lives for the lifetime of the process. A
// multi-replica deployment caches one token per replica, which stays within
// the issuance budget.
func (c *wecomClient) appAccessToken(ctx context.Context) (string, error) {
	key := wecomTokenCacheKey(c.cfg.ClientID, c.cfg.ClientSecret)
	wecomAppTokens.Lock()
	entry, ok := wecomAppTokens.entries[key]
	wecomAppTokens.Unlock()
	if ok && time.Now().Before(entry.expiresAt) {
		return entry.accessToken, nil
	}

	body, err := c.getJSON(ctx, c.cfg.TokenURL, url.Values{
		"corpid":     {c.cfg.ClientID},
		"corpsecret": {c.cfg.ClientSecret},
	})
	if err != nil {
		return "", fmt.Errorf("fetch wecom app access token: %w", err)
	}
	var resp wecomGettokenResponse
	if jerr := json.Unmarshal(body, &resp); jerr != nil {
		return "", fmt.Errorf("fetch wecom app access token: parse response: %w", jerr)
	}
	if resp.ErrCode != 0 {
		return "", fmt.Errorf("fetch wecom app access token: %s", wecomEnvelopeError(resp.ErrCode, resp.ErrMsg))
	}
	if resp.AccessToken == "" {
		return "", fmt.Errorf("fetch wecom app access token: empty access_token")
	}

	// A response without expires_in is treated as immediately stale so the
	// next login refetches rather than pinning a token of unknown age.
	expiresAt := time.Now().Add(-time.Nanosecond)
	if resp.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(resp.ExpiresIn)*time.Second - wecomTokenRefreshEarly)
	}
	wecomAppTokens.Lock()
	wecomAppTokens.entries[key] = wecomTokenEntry{accessToken: resp.AccessToken, expiresAt: expiresAt}
	wecomAppTokens.Unlock()
	return resp.AccessToken, nil
}

// wecomTokenEntry is one cached application access token. expiresAt already
// includes the refresh window.
type wecomTokenEntry struct {
	accessToken string
	expiresAt   time.Time
}

var wecomAppTokens = struct {
	sync.Mutex
	entries map[string]wecomTokenEntry
}{entries: map[string]wecomTokenEntry{}}

// wecomTokenCacheKey identifies a corp+secret pair without storing the
// secret itself.
func wecomTokenCacheKey(corpID, secret string) string {
	sum := sha256.Sum256([]byte(corpID + "\x00" + secret))
	return hex.EncodeToString(sum[:])[:16]
}

// getJSON performs a GET whose parameters carry the credential (WeCom takes
// the access token as a query parameter, not an Authorization header) and
// returns the raw body.
func (c *wecomClient) getJSON(ctx context.Context, endpoint string, params url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	// A rejected call still answers with WeCom's error envelope, which
	// carries the real reason, so the status is only used when there is
	// nothing else to report.
	if resp.StatusCode >= 400 && len(bytes.TrimSpace(body)) == 0 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return body, nil
}

func wecomEnvelopeError(errCode int, errMsg string) string {
	if errMsg == "" {
		return fmt.Sprintf("wecom error %d", errCode)
	}
	return fmt.Sprintf("wecom error %d: %s", errCode, errMsg)
}

// wecomGettokenResponse is the shape of /cgi-bin/gettoken.
type wecomGettokenResponse struct {
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// wecomGetuserinfoResponse is the shape of /cgi-bin/auth/getuserinfo for an
// enterprise member.
type wecomGetuserinfoResponse struct {
	ErrCode    int    `json:"errcode"`
	ErrMsg     string `json:"errmsg"`
	UserID     string `json:"userid"`
	UserTicket string `json:"user_ticket"`
}

// wecomUserResponse is the shape of /cgi-bin/user/get, narrowed to the
// claims the login flow consumes.
type wecomUserResponse struct {
	ErrCode   int    `json:"errcode"`
	ErrMsg    string `json:"errmsg"`
	UserID    string `json:"userid"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar"`
	// BizMail is the tenant's WeCom mailbox address. It is populated only
	// when the tenant has the mailbox service enabled and the member uses
	// it, and it is then the only address the login flow accepts.
	BizMail string `json:"biz_mail"`
}
