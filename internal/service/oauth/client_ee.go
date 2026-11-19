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

// Package oauth ports the auth-client surface from api/apps/auth (Python)
// to Go. It wires three flavors of OAuth/OIDC providers behind a common
// Client interface so the login + callback handlers can stay flavor-blind:
//
//   - "oauth2": vanilla OAuth 2.0 authorization-code flow with a
//     provider-supplied /userinfo endpoint
//   - "oidc": OAuth 2.0 + OIDC discovery via .well-known/openid-configuration
//   - "GitHub": OAuth 2.0 plus GitHub's split user / emails endpoints
//
// Note on OIDC ID-token validation: like the Python OIDCClient, the OIDC
// flavor verifies the id_token signature against the provider's discovered
// JWKS (see oidc_ee.go) and merges its claims underneath the /userinfo
// response. Only asymmetric algorithms advertised by the provider are
// accepted, and the `sub` of both responses must agree.

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
	"time"

	"go.uber.org/zap"
	"ragflow/internal/common"
)

// Config is the channel configuration consumed by NewClient. It mirrors the
// shape of server.OAuthConfig but is copied here to keep this package free
// of imports from the rest of the server.
type Config struct {
	Type             string
	ClientID         string
	ClientSecret     string
	AuthorizationURL string
	TokenURL         string
	UserinfoURL      string
	// RedirectURI is optional. When empty the parameter is left off both the
	// authorization and token requests, which lets a provider that registers
	// the callback out of band (GitHub) use that registration as-is.
	RedirectURI string
	Scope       string
	Issuer      string

	// AgentID is the WeCom self-built application id. It is sent alongside
	// the corp id on the wwlogin consent URL and carries no meaning for the
	// other flavors.
	AgentID string
	// DirectoryURL is the WeCom contact directory endpoint the member
	// profile is loaded from. It defaults to the official endpoint and is
	// overridable so tests can point at a stub server.
	DirectoryURL string

	// Channel is the configured provider name. It is only used to identify
	// the provider in logs.
	Channel string

	// TrustEmailVerified lets an institutionally controlled provider
	// authenticate a user even when it does not assert `email_verified`.
	// Public providers must leave it false.
	TrustEmailVerified bool

	// Ordered claim-precedence lists; empty means "use the OIDC defaults".
	UsernameClaims []string
	EmailClaims    []string
	NicknameClaims []string
	AvatarClaims   []string
}

// UserInfo is the normalized user profile returned by FetchUserInfo. Email
// is the only field treated as required by the callback handler; the rest
// are best-effort.
type UserInfo struct {
	Email     string `json:"email"`
	Username  string `json:"username"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatar_url"`

	// EmailVerified is the provider's assertion that it controls the mailbox.
	// It is what stops a login at a public provider from being bound to an
	// account whose address the user never proved ownership of. The OAuth2 and
	// GitHub flavors leave it false; the OIDC flavor fills it from the
	// verified id_token / userinfo claims.
	EmailVerified bool `json:"-"`
	// Subject is the OIDC `sub` claim, kept for consistent-principal checks
	// between the id_token and the userinfo response.
	Subject string `json:"-"`
}

// Client is the auth-client surface used by the login + callback handlers.
type Client interface {
	AuthorizationURL(state string) (string, error)
	ExchangeCodeForToken(ctx context.Context, code string) (*TokenResponse, error)
	FetchUserInfo(ctx context.Context, accessToken, idToken string) (*UserInfo, error)
}

// TokenResponse is the subset of fields we use from the token endpoint
// response.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type,omitempty"`
	IDToken     string `json:"id_token,omitempty"`
	ExpiresIn   int    `json:"expires_in,omitempty"`
	Scope       string `json:"scope,omitempty"`
}

// HTTPRequestTimeout is the per-request timeout applied to token and
// userinfo calls. Matches the Python http_request_timeout (7s).
const HTTPRequestTimeout = 7 * time.Second

// providerFactories holds the provider types a build adds for itself. The
// shared flavors below are the ones every build ships; a provider that only
// one edition carries registers here from its own file instead of being named
// in the switch, so the switch stays free of provider code the other edition
// does not compile.
var providerFactories = map[string]func(Config) (Client, error){}

// registerProvider makes a provider type resolvable by NewClient. It is meant
// for an implementation file's init.
func registerProvider(name string, factory func(Config) (Client, error)) {
	providerFactories[name] = factory
}

// NewClient returns the Client implementation matching cfg.Type. When type
// is empty, Issuer presence selects OIDC; otherwise OAuth2.
func NewClient(cfg Config) (Client, error) {
	t := strings.ToLower(strings.TrimSpace(cfg.Type))
	if t == "" {
		if cfg.Issuer != "" {
			t = "oidc"
		} else {
			t = "oauth2"
		}
	}
	switch t {
	case "oauth2":
		return newOAuthClient(cfg)
	case "oidc":
		return newOIDCClient(cfg)
	case "github":
		return newGitHubClient(cfg)
	}
	if factory, ok := providerFactories[t]; ok {
		return factory(cfg)
	}
	return nil, fmt.Errorf("unsupported type: %s", t)
}

// oauthClient is the base OAuth 2.0 implementation. The OIDC, GitHub and
// Feishu flavors embed it and override fetchUserInfo.
type oauthClient struct {
	cfg        Config
	httpClient *http.Client
}

func newOAuthClient(cfg Config) (*oauthClient, error) {
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("oauth: client_id is required")
	}
	if cfg.AuthorizationURL == "" {
		return nil, fmt.Errorf("oauth: authorization_url is required")
	}
	if cfg.TokenURL == "" {
		return nil, fmt.Errorf("oauth: token_url is required")
	}
	return &oauthClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: HTTPRequestTimeout},
	}, nil
}

// AuthorizationURL builds the URL the browser should be redirected to.
// Mirrors OAuthClient.get_authorization_url.
//
// redirect_uri is omitted when unset so the provider falls back to the callback
// registered with the application. GitHub requires an explicitly sent
// redirect_uri to match that registration byte for byte, which makes sending
// nothing the safer default for a provider the operator configures by hand.
func (c *oauthClient) AuthorizationURL(state string) (string, error) {
	params := url.Values{}
	params.Set("client_id", c.cfg.ClientID)
	if c.cfg.RedirectURI != "" {
		params.Set("redirect_uri", c.cfg.RedirectURI)
	}
	params.Set("response_type", "code")
	if c.cfg.Scope != "" {
		params.Set("scope", c.cfg.Scope)
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

// ExchangeCodeForToken exchanges an authorization code for an access token.
// Mirrors OAuthClient.exchange_code_for_token.
func (c *oauthClient) ExchangeCodeForToken(ctx context.Context, code string) (*TokenResponse, error) {
	form := url.Values{}
	form.Set("client_id", c.cfg.ClientID)
	form.Set("client_secret", c.cfg.ClientSecret)
	form.Set("code", code)
	if c.cfg.RedirectURI != "" {
		form.Set("redirect_uri", c.cfg.RedirectURI)
	}
	form.Set("grant_type", "authorization_code")

	c.logTokenRequest(code)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to exchange authorization code for token: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange authorization code for token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to exchange authorization code for token: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("failed to exchange authorization code for token: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	token := &TokenResponse{}
	if jerr := json.Unmarshal(body, token); jerr != nil {
		// Some providers (notably GitHub when Accept is not set) return
		// application/x-www-form-urlencoded here instead of JSON.
		if values, perr := url.ParseQuery(string(body)); perr == nil {
			token.AccessToken = values.Get("access_token")
			token.TokenType = values.Get("token_type")
			token.IDToken = values.Get("id_token")
			token.Scope = values.Get("scope")
		} else {
			return nil, fmt.Errorf("failed to exchange authorization code for token: parse response: %w", jerr)
		}
	}
	if token.AccessToken == "" {
		// Providers answer a rejected exchange with HTTP 200 and an `error`
		// body, so the reason has to be lifted out of the response instead
		// of being inferred from the status.
		return nil, fmt.Errorf("failed to exchange authorization code for token: %s", tokenEndpointError(body))
	}
	return token, nil
}

// tokenEndpointError renders the reason a token endpoint gave for not
// returning an access token. Both encodings appear in the wild: JSON from
// most providers, application/x-www-form-urlencoded when a provider ignores
// the Accept header.
func tokenEndpointError(body []byte) string {
	payload := struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}{}
	if jerr := json.Unmarshal(body, &payload); jerr == nil && payload.Error != "" {
		if payload.ErrorDescription != "" {
			return payload.Error + ": " + payload.ErrorDescription
		}
		return payload.Error
	}
	if values, perr := url.ParseQuery(string(body)); perr == nil {
		if e := values.Get("error"); e != "" {
			if d := values.Get("error_description"); d != "" {
				return e + ": " + d
			}
			return e
		}
	}
	return "empty access_token"
}

// FetchUserInfo fetches user information using the access token.
// Mirrors OAuthClient.fetch_user_info / normalize_user_info.
func (c *oauthClient) FetchUserInfo(ctx context.Context, accessToken, idToken string) (*UserInfo, error) {
	if c.cfg.UserinfoURL == "" {
		return nil, fmt.Errorf("failed to fetch user info: userinfo_url is required")
	}
	raw, err := c.fetchUserinfoRaw(ctx, c.cfg.UserinfoURL, accessToken)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user info: %w", err)
	}
	return normalizeUserInfoWithClaims(raw, c.cfg), nil
}

// logTokenRequest records the credentials being sent to the token endpoint,
// without writing the secret itself: a fingerprint is enough to compare
// against the value in the database, and `secret_padded` catches the usual
// cause of a mismatch, a value the admin form saved with trailing whitespace.
func (c *oauthClient) logTokenRequest(code string) {
	sum := sha256.Sum256([]byte(c.cfg.ClientSecret))
	common.Info("oauth token request",
		zap.String("channel", c.cfg.Channel),
		zap.String("type", c.cfg.Type),
		zap.String("token_url", c.cfg.TokenURL),
		zap.String("client_id", c.cfg.ClientID),
		zap.String("client_secret_sha256", hex.EncodeToString(sum[:])[:16]),
		zap.Int("client_secret_len", len(c.cfg.ClientSecret)),
		zap.Bool("client_secret_padded", c.cfg.ClientSecret != strings.TrimSpace(c.cfg.ClientSecret)),
		zap.String("redirect_uri", c.cfg.RedirectURI),
		zap.Int("code_len", len(code)),
	)
}

// fetchUserinfoBody performs the bearer-authenticated GET a provider's
// userinfo endpoint expects and returns the raw body together with the HTTP
// status. Flavors whose response wraps its claims (Feishu answers inside
// `data`) or reports a rejection with HTTP 200 need the envelope rather than
// an already-flattened map.
func (c *oauthClient) fetchUserinfoBody(ctx context.Context, endpoint, bearer string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
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

func (c *oauthClient) fetchUserinfoRaw(ctx context.Context, endpoint, bearer string) (map[string]interface{}, error) {
	body, status, err := c.fetchUserinfoBody(ctx, endpoint, bearer)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", status, strings.TrimSpace(string(body)))
	}
	var out map[string]interface{}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("parse userinfo response: %w", err)
	}
	return out, nil
}

// postJSON posts a JSON body and returns the raw response. Feishu's token
// endpoint expects this shape rather than the form encoding the other
// flavors use. bearer may be empty.
func (c *oauthClient) postJSON(ctx context.Context, endpoint string, payload map[string]string, bearer string) ([]byte, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	// A rejected provider call often still answers with its own error
	// envelope, which carries the real reason, so the status is only used
	// when there is nothing else to report.
	if resp.StatusCode >= 400 && len(bytes.TrimSpace(body)) == 0 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return body, nil
}

// Default claim precedence, matching OIDC Core and the Python normalize_user_info.
// A channel overrides them via Config.*Claims so a provider-specific id claim
// needs no code change.
var (
	defaultUsernameClaims = []string{"username", "preferred_username", "sub"}
	defaultEmailClaims    = []string{"email"}
	defaultNicknameClaims = []string{"nickname", "name"}
	defaultAvatarClaims   = []string{"avatar_url", "picture"}
)

// normalizeUserInfoWithClaims normalizes provider claims using the channel's
// precedence lists: username falls back to the email local part, nickname
// falls back to username, and the avatar falls back to OIDC's "picture".
func normalizeUserInfoWithClaims(raw map[string]interface{}, cfg Config) *UserInfo {
	ui := &UserInfo{}
	pick := func(configured, defaults []string) string {
		keys := configured
		if len(keys) == 0 {
			keys = defaults
		}
		return firstStringClaim(raw, keys)
	}

	ui.Email = pick(cfg.EmailClaims, defaultEmailClaims)
	ui.Username = pick(cfg.UsernameClaims, defaultUsernameClaims)
	if ui.Username == "" && ui.Email != "" {
		if at := strings.IndexByte(ui.Email, '@'); at > 0 {
			ui.Username = ui.Email[:at]
		} else {
			ui.Username = ui.Email
		}
	}

	ui.Nickname = pick(cfg.NicknameClaims, defaultNicknameClaims)
	if ui.Nickname == "" {
		ui.Nickname = ui.Username
	}

	ui.AvatarURL = pick(cfg.AvatarClaims, defaultAvatarClaims)

	ui.EmailVerified = getBoolClaim(raw, "email_verified")
	if v, ok := raw["sub"].(string); ok {
		ui.Subject = v
	}
	return ui
}

// firstStringClaim returns the first non-empty string among keys.
func firstStringClaim(raw map[string]interface{}, keys []string) string {
	for _, k := range keys {
		if v, ok := raw[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// getBoolClaim reads a boolean claim that some providers serialize as the
// string "true"/"false" rather than a JSON bool.
func getBoolClaim(raw map[string]interface{}, key string) bool {
	switch v := raw[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}
