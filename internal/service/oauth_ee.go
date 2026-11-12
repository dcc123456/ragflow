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

package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/engine/kvrocks"
	"ragflow/internal/entity"
	"ragflow/internal/server"
	"ragflow/internal/server/config"
	"ragflow/internal/service/oauth"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Sentinel errors surfaced by the OAuth login + callback endpoints. The
// handler maps each to one of Python's `?error=` redirect codes so the
// frontend can show the same messages.
var (
	// ErrOAuthInvalidChannel mirrors Python's ValueError("Invalid channel name: ...").
	ErrOAuthInvalidChannel = errors.New("invalid channel name")
	// ErrOAuthInvalidState is returned when the callback's state mismatches the
	// stored one (CSRF guard) — maps to "?error=invalid_state".
	ErrOAuthInvalidState = errors.New("invalid_state")
	// ErrOAuthMissingCode is returned when the callback URL has no code
	// query param — maps to "?error=missing_code".
	ErrOAuthMissingCode = errors.New("missing_code")
	// ErrOAuthTokenFailed is returned when token exchange yields no
	// access_token — maps to "?error=token_failed".
	ErrOAuthTokenFailed = errors.New("token_failed")
	// ErrOAuthEmailMissing is returned when the /userinfo response has no
	// email claim — maps to "?error=email_missing".
	ErrOAuthEmailMissing = errors.New("email_missing")
	// ErrOAuthUserInactive is returned when the matched user has
	// is_active=0 — maps to "?error=user_inactive".
	ErrOAuthUserInactive = errors.New("user_inactive")
	// ErrOAuthEmailUnverified is returned when the provider does not assert a
	// verified email for a channel that is not marked trust_email_verified —
	// maps to "?error=email_unverified". Without it an SSO login could be bound
	// to an account whose address the user never proved ownership of.
	ErrOAuthEmailUnverified = errors.New("email_unverified")
)

// oauthStateTTL bounds how long an in-flight OAuth state token is honored.
// Five minutes matches typical session-cookie flows used by the Python
// counterpart.
const oauthStateTTL = 5 * time.Minute

// oauthStateKey builds the consume-once key for an in-flight login. Encoding
// both the channel and the state keeps parallel logins on different channels
// from evicting each other, and lets the stored value double as the channel
// binding checked at callback time.
func oauthStateKey(channel, state string) string {
	return "oauth_state:" + channel + ":" + state
}

// OAuthLoginInit prepares a redirect to the channel's authorization URL.
// The returned state must also be set as a short-lived cookie on the caller
// so the callback can perform a CSRF check that ties the state token to the
// browser that initiated the flow.
type OAuthLoginInit struct {
	State        string
	AuthURL      string
	Channel      string
	CookieMaxAge time.Duration
}

// OAuthLoginInitiate generates a state, persists it in Redis with a TTL,
// and returns the authorization URL the browser should be redirected to.
// Mirrors the body of Python's oauth_login.
func (s *UserService) OAuthLoginInitiate(ctx context.Context, channel string, kvrocks *kvrocks.Client) (*OAuthLoginInit, common.ErrorCode, error) {
	cfg, ok := lookupOAuthConfig(ctx, channel)
	if !ok {
		return nil, common.CodeDataError, fmt.Errorf("%w: %s", ErrOAuthInvalidChannel, channel)
	}
	client, err := oauth.NewClient(toOAuthClientConfig(channel, cfg))
	if err != nil {
		return nil, common.CodeServerError, err
	}

	state, err := generateOAuthState()
	if err != nil {
		return nil, common.CodeServerError, fmt.Errorf("generate oauth state: %w", err)
	}
	if kvrocks != nil {
		// A state we failed to persist must not be handed to the browser: the
		// callback would be rejected for reasons the user cannot see.
		if ok := kvrocks.Set(ctx, oauthStateKey(channel, state), channel, oauthStateTTL); !ok {
			return nil, common.CodeServerError, errors.New("failed to persist oauth state")
		}
	}

	authURL, err := client.AuthorizationURL(state)
	if err != nil {
		return nil, common.CodeServerError, err
	}
	return &OAuthLoginInit{
		State:        state,
		AuthURL:      authURL,
		Channel:      channel,
		CookieMaxAge: oauthStateTTL,
	}, common.CodeSuccess, nil
}

// OAuthCallbackResult is returned to the handler after a successful callback
// so it can mint the user-facing auth response (cookie + redirect).
type OAuthCallbackResult struct {
	User      *entity.User
	IsNewUser bool
}

// OAuthCallback verifies the callback state, exchanges the code for a token,
// fetches the user profile, and either creates a new local user or logs an
// existing one in. Mirrors the body of Python's oauth_callback.
//
// expectedState is the value the handler pulled out of the state cookie.
// When kvrocks is non-nil the state is also verified against and consumed
// from Redis, defending against a replay where an attacker fishes a valid
// state out of a victim's URL but does not have the cookie.
func (s *UserService) OAuthCallback(ctx context.Context, channel, code, callbackState, expectedState string, kvrocks *kvrocks.Client) (*OAuthCallbackResult, common.ErrorCode, error) {
	cfg, ok := lookupOAuthConfig(ctx, channel)
	if !ok {
		return nil, common.CodeDataError, fmt.Errorf("%w: %s", ErrOAuthInvalidChannel, channel)
	}

	if callbackState == "" || expectedState == "" || callbackState != expectedState {
		return nil, common.CodeDataError, ErrOAuthInvalidState
	}
	if kvrocks != nil {
		// The stored value is the channel that started the flow, so a key
		// minted for one channel cannot be redeemed on another. The entry is
		// deleted whether or not it matched, which makes it single-use even if
		// the cookie leaks. Lookup and delete are two calls rather than one
		// compare-and-delete: the store client keeps that primitive
		// unexported, and the residual window is closed at the provider anyway
		// because the authorization code this callback carries is itself
		// single-use. A store error fails closed.
		stored, gerr := kvrocks.Get(ctx, oauthStateKey(channel, callbackState))
		kvrocks.Delete(ctx, oauthStateKey(channel, callbackState))
		if gerr != nil {
			common.Warn("oauth callback: load state failed", zap.String("channel", channel), zap.Error(gerr))
			return nil, common.CodeServerError, ErrOAuthInvalidState
		}
		if stored != channel {
			return nil, common.CodeDataError, ErrOAuthInvalidState
		}
	}
	if code == "" {
		return nil, common.CodeDataError, ErrOAuthMissingCode
	}

	client, err := oauth.NewClient(toOAuthClientConfig(channel, cfg))
	if err != nil {
		return nil, common.CodeServerError, err
	}

	token, err := client.ExchangeCodeForToken(ctx, code)
	if err != nil {
		return nil, common.CodeServerError, fmt.Errorf("%w: %v", ErrOAuthTokenFailed, err)
	}
	if token.AccessToken == "" {
		return nil, common.CodeServerError, ErrOAuthTokenFailed
	}

	info, err := client.FetchUserInfo(ctx, token.AccessToken, token.IDToken)
	if err != nil {
		return nil, common.CodeServerError, fmt.Errorf("failed to fetch user info: %v", err)
	}
	if info.Email == "" {
		// The provider answered but withheld email, usually because the account
		// has none on record or the app was never granted the permission.
		common.Warn("oauth callback: provider returned no email",
			zap.String("channel", channel),
			zap.String("scope", cfg.Scope),
			zap.String("token_scope", token.Scope),
			zap.String("subject", info.Subject),
		)
		return nil, common.CodeDataError, ErrOAuthEmailMissing
	}

	// Account-takeover guard, applied before the account is matched AND before
	// a new one is provisioned: SSO binds to local accounts by email, so an
	// address the provider never verified must not authenticate as — or
	// squat — that account.
	if err := assertEmailTrusted(channel, cfg, info); err != nil {
		return nil, common.CodeForbidden, err
	}

	existing, err := s.userDAO.GetByEmail(ctx, dao.DB, info.Email)
	if err == nil && existing != nil {
		if existing.IsActive == "0" {
			return nil, common.CodeForbidden, ErrOAuthUserInactive
		}
		if code, err := validateLoginTenant(ctx, existing); err != nil {
			return nil, code, err
		}
		newToken := common.GenerateToken()
		existing.AccessToken = &newToken
		now := time.Now().Truncate(time.Second)
		existing.LastLoginTime = &now
		if err = s.userDAO.Update(ctx, dao.DB, existing); err != nil {
			return nil, common.CodeServerError, fmt.Errorf("update user: %w", err)
		}
		return &OAuthCallbackResult{User: existing, IsNewUser: false}, common.CodeSuccess, nil
	}

	// New user — register a fresh account bound to this channel.
	created, ecode, err := s.registerOAuthUser(ctx, channel, info)
	if err != nil {
		return nil, ecode, err
	}
	return &OAuthCallbackResult{User: created, IsNewUser: true}, common.CodeSuccess, nil
}

// assertEmailTrusted enforces the email_verified requirement for OIDC
// channels. OAuth2 / GitHub channels keep their previous behaviour: they have
// no verified-email claim to check, and adding the requirement would break
// GitHub logins (which never send one).
func assertEmailTrusted(channel string, cfg config.OAuthChannel, info *oauth.UserInfo) error {
	if !isOIDCChannel(cfg) || info.EmailVerified || cfg.TrustEmailVerified {
		return nil
	}
	return fmt.Errorf("%w: identity provider did not return a verified email for channel %s", ErrOAuthEmailUnverified, channel)
}

// isOIDCChannel reports whether the channel authenticates users through an OIDC
// id token, i.e. whether it carries an `email_verified` claim at all. Mirrors
// the client selection rule: an explicit "oidc" type or a configured issuer.
func isOIDCChannel(cfg config.OAuthChannel) bool {
	return strings.EqualFold(strings.TrimSpace(cfg.Type), "oidc") || cfg.Issuer != ""
}

// registerOAuthUser provisions a new user + tenant for an OAuth identity.
// Models the relevant fields the email-password Register path sets so the
// rest of the app (kbs, files, llm config) sees a fully-shaped tenant.
func (s *UserService) registerOAuthUser(ctx context.Context, channel string, info *oauth.UserInfo) (*entity.User, common.ErrorCode, error) {
	if err := CheckRegistrationWhitelist(ctx, info.Email); err != nil {
		var coded *common.CodedError
		if errors.As(err, &coded) {
			return nil, coded.Code, err
		}
		return nil, common.CodeServerError, err
	}
	cfg := server.GetConfig()
	userID := common.GenerateToken()
	accessToken := common.GenerateToken()
	status := "1"
	loginChannel := channel
	isSuperuser := false
	nickname := info.Nickname
	if nickname == "" {
		nickname = info.Username
	}
	if nickname == "" {
		nickname = info.Email
	}

	user := &entity.User{
		ID:              userID,
		AccessToken:     &accessToken,
		Email:           info.Email,
		Nickname:        nickname,
		Avatar:          &info.AvatarURL,
		Status:          &status,
		IsActive:        "1",
		IsAuthenticated: "1",
		IsAnonymous:     "0",
		LoginChannel:    &loginChannel,
		IsSuperuser:     &isSuperuser,
	}

	tenantName := nickname + "'s Kingdom"
	tenant := &entity.Tenant{
		ID:   userID,
		Name: &tenantName,

		ParserIDs: "naive:General,qa:Q&A,resume:Resume,manual:Manual,table:Table,paper:Research Paper,book:Book,laws:Laws,presentation:Presentation,picture:Picture,one:One,audio:Audio,email:Email,tag:Tag",
		Status:    &status,
	}
	userTenantID := common.GenerateToken()
	userTenant := &entity.UserTenant{
		ID:        userTenantID,
		UserID:    userID,
		TenantID:  userID,
		Role:      "owner",
		InvitedBy: userID,
		Status:    &status,
		LLMID:     cfg.GetDefaultChatModel().Name,
		EmbdID:    cfg.GetDefaultEmbeddingModel().Name,
		ASRID:     cfg.GetDefaultASRModel().Name,
		Img2TxtID: cfg.GetDefaultVisionModel().Name,
		RerankID:  cfg.GetDefaultRerankModel().Name,
	}
	fileID := common.GenerateToken()
	rootFile := &entity.File{
		ID:        fileID,
		ParentID:  fileID,
		TenantID:  userID,
		CreatedBy: userID,
		Name:      "/",
		Type:      "folder",
		Size:      0,
	}

	tenantDAO := dao.NewTenantDAO()
	userTenantDAO := dao.NewUserTenantDAO()
	fileDAO := dao.NewFileDAO()

	err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.userDAO.Create(ctx, tx, user); err != nil {
			return fmt.Errorf("failed to register %s: %w", info.Email, err)
		}
		if err := tenantDAO.Create(ctx, tx, tenant); err != nil {
			return fmt.Errorf("failed to register %s: %w", info.Email, err)
		}
		if err := userTenantDAO.Create(ctx, tx, userTenant); err != nil {
			return fmt.Errorf("failed to register %s: %w", info.Email, err)
		}
		if err := fileDAO.Create(ctx, tx, rootFile); err != nil {
			return fmt.Errorf("failed to register %s: %w", info.Email, err)
		}
		return nil
	})
	if err != nil {
		return nil, common.CodeServerError, err
	}

	return user, common.CodeSuccess, nil
}

// toOAuthClientConfig narrows config.OAuthChannel (which carries display-only
// fields like Icon) into the oauth.Config the client package consumes.
func toOAuthClientConfig(channel string, cfg config.OAuthChannel) oauth.Config {
	return oauth.Config{
		Channel:            channel,
		Type:               cfg.Type,
		ClientID:           cfg.ClientID,
		ClientSecret:       cfg.ClientSecret,
		AuthorizationURL:   cfg.AuthorizationURL,
		TokenURL:           cfg.TokenURL,
		UserinfoURL:        cfg.UserinfoURL,
		RedirectURI:        cfg.RedirectURI,
		Scope:              cfg.Scope,
		Issuer:             cfg.Issuer,
		TrustEmailVerified: cfg.TrustEmailVerified,
		UsernameClaims:     cfg.UsernameClaims,
		EmailClaims:        cfg.EmailClaims,
		NicknameClaims:     cfg.NicknameClaims,
		AvatarClaims:       cfg.AvatarClaims,
	}
}

// generateOAuthState returns a cryptographically random 32-byte hex string
// used as the OAuth state parameter. 256 bits keeps collisions and
// brute-force guessing comfortably out of reach for the 5-minute TTL.
func generateOAuthState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
