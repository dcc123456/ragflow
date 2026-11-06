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

	"ragflow/internal/common"

	"go.uber.org/zap"
)

// Feishu (Lark) OAuth endpoints. The consent page is served from
// accounts.feishu.cn while the API itself lives on open.feishu.cn, so the
// two hosts differ.
const (
	larkAuthorizationURL = "https://accounts.feishu.cn/open-apis/authen/v1/authorize"
	larkTokenURL         = "https://open.feishu.cn/open-apis/authen/v2/oauth/token"
	larkUserinfoURL      = "https://open.feishu.cn/open-apis/authen/v1/user_info"

	// larkDefaultScope is the smallest authorization scope that still lets
	// /authen/v1/user_info answer. Reading the email additionally requires
	// the contact permission to be granted on the Feishu app; a deployment
	// that has it widens the list through the channel's `scope` setting.
	larkDefaultScope = "auth:user.id:read"
)

// larkClient implements the Feishu/Lark browser-web SSO flow.
//
// Three details separate it from a vanilla OAuth2 provider:
//
//   - the token endpoint takes a JSON body instead of a form;
//   - a rejected call answers with a top-level `code`/`msg` envelope under
//     HTTP 200, so the envelope rather than the status carries the reason;
//   - the userinfo endpoint wraps its claims in `data` and names them
//     Feishu's own way (`en_name`/`name`) instead of OIDC's.
//
// The flow issues no id_token, so EmailVerified stays false and the callback's
// email-verified guard, which targets OIDC channels, does not apply — the
// same position GitHub is in, where whoever controls the app vouches for its
// members.
type larkClient struct {
	*oauthClient
}

func init() {
	registerProvider("lark", func(cfg Config) (Client, error) {
		return newLarkClient(cfg)
	})
}

func newLarkClient(cfg Config) (*larkClient, error) {
	// Existing values are kept so an operator can point the channel at a
	// self-hosted deployment and so tests can inject a stub endpoint.
	if cfg.AuthorizationURL == "" {
		cfg.AuthorizationURL = larkAuthorizationURL
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = larkTokenURL
	}
	if cfg.UserinfoURL == "" {
		cfg.UserinfoURL = larkUserinfoURL
	}
	if cfg.Scope == "" {
		cfg.Scope = larkDefaultScope
	}
	base, err := newOAuthClient(cfg)
	if err != nil {
		return nil, err
	}
	return &larkClient{oauthClient: base}, nil
}

func (c *larkClient) ExchangeCodeForToken(ctx context.Context, code string) (*TokenResponse, error) {
	payload := map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     c.cfg.ClientID,
		"client_secret": c.cfg.ClientSecret,
		"code":          code,
	}
	// `redirect_uri` is optional for some application kinds and required for
	// others: whatever is configured has to match what was sent to the
	// consent page, and nothing is sent when the channel has none.
	if c.cfg.RedirectURI != "" {
		payload["redirect_uri"] = c.cfg.RedirectURI
	}

	c.logTokenRequest(code)

	body, err := c.postJSON(ctx, c.cfg.TokenURL, payload, "")
	if err != nil {
		return nil, fmt.Errorf("failed to exchange authorization code for token: %w", err)
	}
	var resp larkTokenEnvelope
	if jerr := json.Unmarshal(body, &resp); jerr != nil {
		return nil, fmt.Errorf("failed to exchange authorization code for token: parse response: %w", jerr)
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("failed to exchange authorization code for token: %s", larkEnvelopeError(resp.Code, resp.Msg))
	}
	if resp.AccessToken == "" {
		return nil, fmt.Errorf("failed to exchange authorization code for token: %s", tokenEndpointError(body))
	}
	token := &TokenResponse{
		AccessToken: resp.AccessToken,
		TokenType:   resp.TokenType,
		Scope:       resp.Scope,
	}
	if expires, cerr := resp.ExpiresIn.Int64(); cerr == nil && expires > 0 {
		token.ExpiresIn = int(expires)
	}
	return token, nil
}

func (c *larkClient) FetchUserInfo(ctx context.Context, accessToken, _ string) (*UserInfo, error) {
	if c.cfg.UserinfoURL == "" {
		return nil, fmt.Errorf("failed to fetch user info: userinfo_url is required")
	}
	body, status, err := c.fetchUserinfoBody(ctx, c.cfg.UserinfoURL, accessToken)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user info: %w", err)
	}
	var resp larkUserinfoResponse
	if jerr := json.Unmarshal(body, &resp); jerr != nil {
		common.Warn("feishu userinfo response is not JSON",
			zap.String("channel", c.cfg.Channel),
			zap.Int("status", status),
			zap.Int("body_len", len(body)),
			zap.String("body_prefix", truncateForLog(string(body), 256)),
		)
		return nil, fmt.Errorf("failed to fetch user info: parse response: %w", jerr)
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("failed to fetch user info: %s", larkEnvelopeError(resp.Code, resp.Msg))
	}
	if resp.Data.Email == "" && resp.Data.EnterpriseEmail == "" {
		// Feishu answers successfully for a member with no mailbox in the
		// directory, and the callback can then only report email_missing. The
		// ids are what an operator needs to find that member; the body is
		// deliberately not logged, since it carries personal fields.
		common.Warn("feishu userinfo response carries no email",
			zap.String("channel", c.cfg.Channel),
			zap.String("open_id", resp.Data.OpenID),
			zap.String("union_id", resp.Data.UnionID),
			zap.String("user_id", resp.Data.UserID),
		)
	}

	claims := map[string]interface{}{
		"name":             resp.Data.Name,
		"en_name":          resp.Data.EnName,
		"email":            resp.Data.Email,
		"enterprise_email": resp.Data.EnterpriseEmail,
		"mobile":           resp.Data.Mobile,
		"avatar_url":       resp.Data.AvatarURL,
		"union_id":         resp.Data.UnionID,
		"open_id":          resp.Data.OpenID,
		"user_id":          resp.Data.UserID,
		// OIDC calls display name "preferred_username"; the shared defaults
		// look for it, so map Feishu's English name onto it rather than
		// relying on the per-flavor list alone.
		"preferred_username": resp.Data.EnName,
	}
	info := normalizeUserInfoWithClaims(claims, Config{
		// `email` is the directory mailbox and stays empty for a user who has
		// none; `enterprise_email` is the Feishu mailbox address, which is the
		// only address such a tenant has. Both identify the same person.
		EmailClaims: []string{"email", "enterprise_email"},
		// Only the English name is a username candidate. The localized name
		// is usually a Chinese personal name, and the Feishu ids are opaque
		// and already carried by Subject, so both fall through to the shared
		// email-local-part fallback, which stays readable in either case.
		UsernameClaims: []string{"en_name"},
		NicknameClaims: []string{"name", "en_name"},
		AvatarClaims:   []string{"avatar_url"},
	})
	// A Feishu identity survives app re-installs at the union level, so
	// union_id is the better value to pin a user to when it is present.
	for _, candidate := range []string{resp.Data.UnionID, resp.Data.OpenID, resp.Data.UserID} {
		if candidate != "" {
			info.Subject = candidate
			break
		}
	}
	return info, nil
}

func truncateForLog(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

// larkEnvelopeError renders Feishu's `code`/`msg` pair. Feishu reports most
// failures with HTTP 200, so these numbers are the only way to tell them apart.
func larkEnvelopeError(code int, msg string) string {
	if msg == "" {
		return fmt.Sprintf("feishu error %d", code)
	}
	return fmt.Sprintf("feishu error %d: %s", code, msg)
}

// larkTokenEnvelope is the top-level shape of a token response. Every field
// but `code` is absent when the call was rejected.
type larkTokenEnvelope struct {
	Code        int    `json:"code"`
	Msg         string `json:"msg"`
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	// ExpiresIn stays raw because some Feishu responses carry a fractional
	// value, and a session should not fail to start over that.
	ExpiresIn json.Number `json:"expires_in"`
}

type larkUserinfoResponse struct {
	Code int                `json:"code"`
	Msg  string             `json:"msg"`
	Data larkUserinfoClaims `json:"data"`
}

type larkUserinfoClaims struct {
	Name   string `json:"name"`
	EnName string `json:"en_name"`
	Email  string `json:"email"`
	// EnterpriseEmail is the Feishu mailbox address. It is populated only for
	// tenants that have the mailbox service, and it is then the only address
	// the account has, so it stands in for `email`.
	EnterpriseEmail string `json:"enterprise_email"`
	Mobile          string `json:"mobile"`
	AvatarURL       string `json:"avatar_url"`
	OpenID          string `json:"open_id"`
	UnionID         string `json:"union_id"`
	UserID          string `json:"user_id"`
}
