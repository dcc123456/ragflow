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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
)

// oidcClient is the OIDC flavor: it resolves the authorization / token /
// userinfo / JWKS endpoints from the Issuer's discovery document
// (.well-known/openid-configuration) before delegating the OAuth flow to
// oauthClient.
//
// Unlike plain OAuth 2.0, the id_token is a signed assertion about the user
// and is verified against the provider's JWKS before any of its claims are
// trusted. Verification is what makes the email claim safe to bind to a local
// account: without it, nothing stops a forged token from asserting an
// arbitrary address.
type oidcClient struct {
	*oauthClient
	issuer      string
	jwksURI     string
	signingAlgs []string
}

// oidcMetadataTTL bounds how long a discovery document / JWKS is reused, so a
// login burst does not re-fetch them per request.
const oidcMetadataTTL = time.Hour

// jwksForceRefreshFloor rate-limits forced JWKS refreshes: a token carrying a
// random `kid` must not turn every callback into an outbound fetch.
const jwksForceRefreshFloor = time.Minute

func newOIDCClient(cfg Config) (*oidcClient, error) {
	if cfg.Issuer == "" {
		return nil, fmt.Errorf("missing issuer in configuration")
	}
	meta, err := fetchOIDCMetadataCached(cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OIDC metadata: %w", err)
	}
	// The id_token's `iss` must equal the issuer the provider DECLARES in its
	// discovery document rather than the configured discovery base: the two
	// routinely differ by a trailing slash (OIDC Core §3.1.3.7).
	if meta.Issuer != "" {
		cfg.Issuer = meta.Issuer
	}
	if cfg.AuthorizationURL == "" {
		cfg.AuthorizationURL = meta.AuthorizationEndpoint
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = meta.TokenEndpoint
	}
	if cfg.UserinfoURL == "" {
		cfg.UserinfoURL = meta.UserinfoEndpoint
	}
	base, err := newOAuthClient(cfg)
	if err != nil {
		return nil, err
	}
	return &oidcClient{
		oauthClient: base,
		issuer:      cfg.Issuer,
		jwksURI:     meta.JWKSURI,
		signingAlgs: resolveSigningAlgs(meta.SigningAlgsSupported),
	}, nil
}

// oidcMetadata is the subset of fields we use from the discovery document.
type oidcMetadata struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	UserinfoEndpoint      string   `json:"userinfo_endpoint"`
	JWKSURI               string   `json:"jwks_uri"`
	SigningAlgsSupported  []string `json:"id_token_signing_alg_values_supported"`
}

// cachedMetadata wraps a discovery document with the time it was fetched.
type cachedMetadata struct {
	meta      *oidcMetadata
	fetchedAt time.Time
}

// cachedJWKS wraps a provider key set with the time it was fetched.
type cachedJWKS struct {
	set       *jose.JSONWebKeySet
	fetchedAt time.Time
}

var (
	metadataCacheMu sync.Mutex
	metadataCache   = map[string]*cachedMetadata{}

	jwksCacheMu sync.Mutex
	jwksCache   = map[string]*cachedJWKS{}
)

// resetOIDCCaches drops the discovery and JWKS caches. Tests call it so a
// previous case cannot satisfy a later lookup.
func resetOIDCCaches() {
	metadataCacheMu.Lock()
	metadataCache = map[string]*cachedMetadata{}
	metadataCacheMu.Unlock()

	jwksCacheMu.Lock()
	jwksCache = map[string]*cachedJWKS{}
	jwksCacheMu.Unlock()
}

// fetchOIDCMetadataCached returns the discovery document for issuer, reusing a
// cached copy for oidcMetadataTTL. The fetch itself stays behind
// loadOIDCMetadata so tests can substitute a fake provider.
func fetchOIDCMetadataCached(issuer string) (*oidcMetadata, error) {
	metadataCacheMu.Lock()
	if e, ok := metadataCache[issuer]; ok && time.Since(e.fetchedAt) < oidcMetadataTTL {
		meta := e.meta
		metadataCacheMu.Unlock()
		return meta, nil
	}
	metadataCacheMu.Unlock()

	meta, err := loadOIDCMetadata(issuer)
	if err != nil {
		return nil, err
	}
	if meta.Issuer == "" && meta.AuthorizationEndpoint == "" && meta.TokenEndpoint == "" {
		return nil, fmt.Errorf("incomplete OIDC metadata for %s", issuer)
	}

	metadataCacheMu.Lock()
	metadataCache[issuer] = &cachedMetadata{meta: meta, fetchedAt: time.Now()}
	metadataCacheMu.Unlock()
	return meta, nil
}

// loadOIDCMetadata is the indirection used to fetch a provider's discovery
// document. Tests override it.
var loadOIDCMetadata = func(issuer string) (*oidcMetadata, error) {
	metadataURL := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	ctx, cancel := context.WithTimeout(context.Background(), HTTPRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	meta := &oidcMetadata{}
	if err := json.Unmarshal(body, meta); err != nil {
		return nil, fmt.Errorf("parse OIDC metadata: %w", err)
	}
	return meta, nil
}

// allowedSigningAlgs are the ID-token algorithms safe to accept. Symmetric
// HMAC (HS*) and "none" are deliberately excluded: the verification key comes
// from the provider's public JWKS, so accepting HS256 would let anyone HMAC a
// token with those public key bytes and have it verify (RSA/HMAC confusion,
// CWE-347), and "none" disables verification outright. Mirrors
// api/apps/auth/oidc.py.
var allowedSigningAlgs = map[string]struct{}{
	"RS256": {}, "RS384": {}, "RS512": {},
	"ES256": {}, "ES384": {}, "ES512": {},
	"PS256": {}, "PS384": {}, "PS512": {},
	"EdDSA": {},
}

// resolveSigningAlgs intersects the provider-advertised algorithms with the
// allowlist, defaulting to RS256 (the OIDC Core default). It never trusts the
// `alg` header of the token being verified.
func resolveSigningAlgs(advertised []string) []string {
	safe := make([]string, 0, len(advertised))
	for _, a := range advertised {
		if _, ok := allowedSigningAlgs[a]; ok {
			safe = append(safe, a)
		}
	}
	if len(safe) == 0 {
		return []string{"RS256"}
	}
	return safe
}

// FetchUserInfo returns the normalized identity, preferring the userinfo
// endpoint over id_token claims (both describe the same principal, and the
// userinfo response is bound to the access token we just obtained).
func (c *oidcClient) FetchUserInfo(ctx context.Context, accessToken, idToken string) (*UserInfo, error) {
	claims := map[string]interface{}{}

	var idTokenSub string
	if idToken != "" && c.jwksURI != "" {
		idClaims, err := c.verifyIDToken(ctx, idToken)
		if err != nil {
			return nil, fmt.Errorf("verify id_token: %w", err)
		}
		if sub, ok := idClaims["sub"].(string); ok {
			idTokenSub = sub
		}
		for k, v := range idClaims {
			claims[k] = v
		}
	}

	if c.cfg.UserinfoURL != "" {
		raw, err := c.fetchUserinfoRaw(ctx, c.cfg.UserinfoURL, accessToken)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch user info: %w", err)
		}
		// OIDC Core §5.3.2: the userinfo `sub` MUST match the id_token `sub`.
		// Rejecting the mismatch stops userinfo claims (which overwrite the
		// id_token claims above) from describing a different principal.
		if uiSub, _ := raw["sub"].(string); idTokenSub != "" && uiSub != "" && uiSub != idTokenSub {
			return nil, errors.New("failed to fetch user info: userinfo sub does not match id_token sub")
		}
		for k, v := range raw {
			claims[k] = v
		}
	}

	if len(claims) == 0 {
		return nil, errors.New("failed to fetch user info: provider returned neither an id_token nor a userinfo response")
	}
	return normalizeUserInfoWithClaims(claims, c.cfg), nil
}

// verifyIDToken checks the signature against the provider's JWKS and enforces
// the claims OIDC requires of an authentication response.
func (c *oidcClient) verifyIDToken(ctx context.Context, idToken string) (jwt.MapClaims, error) {
	keyfunc := func(t *jwt.Token) (interface{}, error) {
		kid, _ := t.Header["kid"].(string)
		set, err := c.fetchJWKS(ctx, false)
		if err != nil {
			return nil, err
		}
		keys := set.Key(kid)
		if len(keys) == 0 {
			// The kid may be new because the provider rotated its signing key
			// after we cached the JWKS. Force one rate-limited refresh so
			// logins survive rotation instead of failing for a full TTL.
			if fresh, ferr := c.fetchJWKS(ctx, true); ferr == nil {
				keys = fresh.Key(kid)
			}
		}
		if len(keys) == 0 {
			return nil, fmt.Errorf("no signing key for kid %q", kid)
		}
		return keys[0].Key, nil
	}

	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(idToken, claims, keyfunc,
		jwt.WithValidMethods(c.signingAlgs),
		jwt.WithAudience(c.cfg.ClientID),
		jwt.WithIssuer(c.issuer),
		jwt.WithExpirationRequired(), // OIDC Core §2 makes exp mandatory.
		jwt.WithIssuedAt(),           // reject tokens whose iat is in the future.
	)
	if err != nil {
		return nil, err
	}
	return claims, nil
}

// fetchJWKS returns the provider's key set, cached for oidcMetadataTTL. When
// force is set the cache is bypassed unless it was refreshed within
// jwksForceRefreshFloor, which keeps unknown-kid refreshes bounded.
func (c *oidcClient) fetchJWKS(ctx context.Context, force bool) (*jose.JSONWebKeySet, error) {
	if c.jwksURI == "" {
		return nil, errors.New("provider did not advertise a jwks_uri")
	}

	jwksCacheMu.Lock()
	if e, ok := jwksCache[c.jwksURI]; ok && time.Since(e.fetchedAt) < oidcMetadataTTL &&
		(!force || time.Since(e.fetchedAt) < jwksForceRefreshFloor) {
		set := e.set
		jwksCacheMu.Unlock()
		return set, nil
	}
	jwksCacheMu.Unlock()

	body, err := c.httpGetJSON(ctx, c.jwksURI)
	if err != nil {
		return nil, fmt.Errorf("fetch JWKS: %w", err)
	}
	var set jose.JSONWebKeySet
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, fmt.Errorf("decode JWKS: %w", err)
	}

	jwksCacheMu.Lock()
	jwksCache[c.jwksURI] = &cachedJWKS{set: &set, fetchedAt: time.Now()}
	jwksCacheMu.Unlock()
	return &set, nil
}

// httpGetJSON performs a bounded GET and returns the body, so a hanging provider
// cannot pin a callback goroutine.
func (c *oidcClient) httpGetJSON(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
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
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned %d", rawURL, resp.StatusCode)
	}
	return body, nil
}
