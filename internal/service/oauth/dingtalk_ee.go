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
	"fmt"
	"io"
	"net/http"

	"ragflow/internal/common"

	"go.uber.org/zap"
)

// DingTalk OAuth endpoints. The consent page is served from
// login.dingtalk.com while the API itself lives on api.dingtalk.com, so the
// two hosts differ.
const (
	dingtalkAuthorizationURL = "https://login.dingtalk.com/oauth2/auth"
	dingtalkTokenURL         = "https://api.dingtalk.com/v1.0/oauth2/userAccessToken"
	dingtalkUserinfoURL      = "https://api.dingtalk.com/v1.0/contact/users/me"

	// dingtalkDefaultScope is the smallest authorization scope that still
	// lets /contact/users/me answer. Reading the email additionally requires
	// the "通讯录个人信息读" permission (Contact.User.Read) to be granted on
	// the DingTalk app; a deployment that has it widens the list through the
	// channel's `scope` setting.
	dingtalkDefaultScope = "openid"
)

// dingtalkClient implements the DingTalk browser-web SSO flow.
//
// Three details separate it from a vanilla OAuth2 provider:
//
//   - the token endpoint takes a JSON body instead of a form, and names its
//     fields in camelCase (`accessToken`, `expireIn`);
//   - the userinfo endpoint authenticates through the `x-acs-dingtalk-access-token`
//     header instead of `Authorization: Bearer`;
//   - the userinfo claims are named DingTalk's own way (`nick`, `unionId`)
//     instead of OIDC's, and the identity is pinned to `unionId`, which
//     survives re-installing the app in a different organization.
//
// The flow issues no id_token, so EmailVerified stays false and the callback's
// email-verified guard, which targets OIDC channels, does not apply — the
// same position GitHub is in, where whoever controls the app vouches for its
// members.
type dingtalkClient struct {
	*oauthClient
}

func init() {
	registerProvider("dingtalk", func(cfg Config) (Client, error) {
		return newDingtalkClient(cfg)
	})
}

func newDingtalkClient(cfg Config) (*dingtalkClient, error) {
	// Existing values are kept so an operator can point the channel at a
	// self-hosted deployment and so tests can inject a stub endpoint.
	if cfg.AuthorizationURL == "" {
		cfg.AuthorizationURL = dingtalkAuthorizationURL
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = dingtalkTokenURL
	}
	if cfg.UserinfoURL == "" {
		cfg.UserinfoURL = dingtalkUserinfoURL
	}
	if cfg.Scope == "" {
		cfg.Scope = dingtalkDefaultScope
	}
	base, err := newOAuthClient(cfg)
	if err != nil {
		return nil, err
	}
	return &dingtalkClient{oauthClient: base}, nil
}

func (c *dingtalkClient) ExchangeCodeForToken(ctx context.Context, code string) (*TokenResponse, error) {
	payload := map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     c.cfg.ClientID,
		"client_secret": c.cfg.ClientSecret,
		"code":          code,
	}
	// Whatever is configured has to match what was sent to the consent page,
	// and nothing is sent when the channel has none.
	if c.cfg.RedirectURI != "" {
		payload["redirect_uri"] = c.cfg.RedirectURI
	}

	c.logTokenRequest(code)

	body, err := c.postJSON(ctx, c.cfg.TokenURL, payload, "")
	if err != nil {
		return nil, fmt.Errorf("failed to exchange authorization code for token: %w", err)
	}
	var resp dingtalkTokenResponse
	if jerr := json.Unmarshal(body, &resp); jerr != nil {
		return nil, fmt.Errorf("failed to exchange authorization code for token: parse response: %w", jerr)
	}
	if resp.AccessToken == "" {
		return nil, fmt.Errorf("failed to exchange authorization code for token: %s", tokenEndpointError(body))
	}
	token := &TokenResponse{
		AccessToken: resp.AccessToken,
		TokenType:   resp.TokenType,
		Scope:       resp.Scope,
	}
	if resp.ExpireIn > 0 {
		token.ExpiresIn = resp.ExpireIn
	}
	return token, nil
}

func (c *dingtalkClient) FetchUserInfo(ctx context.Context, accessToken, _ string) (*UserInfo, error) {
	if c.cfg.UserinfoURL == "" {
		return nil, fmt.Errorf("failed to fetch user info: userinfo_url is required")
	}
	body, status, err := c.fetchDingtalkUserinfo(ctx, c.cfg.UserinfoURL, accessToken)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user info: %w", err)
	}
	var resp dingtalkUserinfoClaims
	if jerr := json.Unmarshal(body, &resp); jerr != nil {
		common.Warn("dingtalk userinfo response is not JSON",
			zap.String("channel", c.cfg.Channel),
			zap.Int("status", status),
			zap.Int("body_len", len(body)),
			zap.String("body_prefix", truncateForLog(string(body), 256)),
		)
		return nil, fmt.Errorf("failed to fetch user info: parse response: %w", jerr)
	}
	if resp.Code != "" {
		return nil, fmt.Errorf("failed to fetch user info: %s", dingtalkEnvelopeError(resp.Code, resp.Message))
	}
	if resp.Email == "" {
		// DingTalk answers successfully for a member whose app lacks the
		// contact permission, and the callback can then only report
		// email_missing. The ids are what an operator needs to find that
		// member; the body is deliberately not logged, since it carries
		// personal fields.
		common.Warn("dingtalk userinfo response carries no email",
			zap.String("channel", c.cfg.Channel),
			zap.String("open_id", resp.OpenID),
			zap.String("union_id", resp.UnionID),
		)
	}

	claims := map[string]interface{}{
		"name":       resp.Nick,
		"email":      resp.Email,
		"mobile":     resp.Mobile,
		"avatar_url": resp.AvatarURL,
		"union_id":   resp.UnionID,
		"open_id":    resp.OpenID,
		"nick":       resp.Nick,
	}
	info := normalizeUserInfoWithClaims(claims, Config{
		// `nick` is a localized display name, usually a Chinese personal
		// name. DingTalk exposes no separate latin name, so it is not a
		// username candidate and both fall through to the shared
		// email-local-part fallback, which stays readable in either case.
		UsernameClaims: []string{},
		NicknameClaims: []string{"nick"},
		AvatarClaims:   []string{"avatar_url"},
	})
	// A DingTalk identity survives re-installing the application in a
	// different organization at the union level, so union_id is the better
	// value to pin a user to when it is present.
	for _, candidate := range []string{resp.UnionID, resp.OpenID} {
		if candidate != "" {
			info.Subject = candidate
			break
		}
	}
	return info, nil
}

// fetchDingtalkUserinfo performs the GET a DingTalk userinfo endpoint
// expects: the access token rides on `x-acs-dingtalk-access-token`, not on
// the standard Authorization header the other flavors use.
func (c *dingtalkClient) fetchDingtalkUserinfo(ctx context.Context, endpoint, accessToken string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("x-acs-dingtalk-access-token", accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

// dingtalkEnvelopeError renders DingTalk's `code`/`message` pair. DingTalk
// reports most failures with HTTP 200, so these numbers are the only way to
// tell them apart.
func dingtalkEnvelopeError(code, message string) string {
	if message == "" {
		return fmt.Sprintf("dingtalk error %s", code)
	}
	return fmt.Sprintf("dingtalk error %s: %s", code, message)
}

// dingtalkTokenResponse is the shape of a successful token response. DingTalk
// names its fields in camelCase; every field but the token is auxiliary.
type dingtalkTokenResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpireIn     int    `json:"expireIn"`
	CorpID       string `json:"corpId"`
	TokenType    string `json:"tokenType"`
	Scope        string `json:"scope"`
}

type dingtalkUserinfoClaims struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Nick      string `json:"nick"`
	AvatarURL string `json:"avatarUrl"`
	Mobile    string `json:"mobile"`
	OpenID    string `json:"openId"`
	UnionID   string `json:"unionId"`
	Email     string `json:"email"`
	StateCode string `json:"stateCode"`
}
