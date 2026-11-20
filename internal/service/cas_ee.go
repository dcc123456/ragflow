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
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/engine/kvrocks"
	"ragflow/internal/entity"
	"ragflow/internal/service/billing"
	"ragflow/internal/service/cas"
	"ragflow/internal/service/oauth"

	"gorm.io/gorm"
)

var (
	// ErrCASInvalidChannel means CAS is disabled or its configuration is incomplete.
	ErrCASInvalidChannel = errors.New("CAS is not configured")
	// ErrCASInvalidState means a callback is expired, consumed or belongs to another browser.
	ErrCASInvalidState = errors.New("invalid_state")
	// ErrCASMissingTicket means the provider returned without a service ticket.
	ErrCASMissingTicket = errors.New("missing_ticket")
)

// CASLoginInit carries the redirect and browser binding for a CAS login attempt.
type CASLoginInit struct {
	State        string
	AuthURL      string
	RedirectURI  string
	CookieMaxAge time.Duration
}

func casStateKey(state string) string {
	return "cas_state:" + state
}

func casStateBinding(channel string, cfg cas.Config) string {
	value, _ := json.Marshal(cfg)
	digest := sha256.Sum256(value)
	return channel + ":" + hex.EncodeToString(digest[:])
}

// CASLoginInitiate binds the callback service URL to a short-lived browser state.
func (s *UserService) CASLoginInitiate(ctx context.Context, channel string, cache *kvrocks.Client) (*CASLoginInit, common.ErrorCode, error) {
	provider, ok := loadCASProviders(ctx)[channel]
	if !ok {
		return nil, common.CodeDataError, ErrCASInvalidChannel
	}
	client, err := cas.NewClient(provider.Config)
	if err != nil {
		return nil, common.CodeServerError, err
	}
	state, err := generateOAuthState()
	if err != nil {
		return nil, common.CodeServerError, fmt.Errorf("generate CAS state: %w", err)
	}
	if cache == nil || !cache.Set(ctx, casStateKey(state), casStateBinding(channel, provider.Config), oauthStateTTL) {
		return nil, common.CodeServerError, errors.New("failed to persist CAS state")
	}
	return &CASLoginInit{
		State: state, AuthURL: client.AuthorizationURL(state), RedirectURI: provider.Config.ServiceURL,
		CookieMaxAge: oauthStateTTL,
	}, common.CodeSuccess, nil
}

// CASCallback validates a one-time state and ticket before entering the existing account flow.
func (s *UserService) CASCallback(ctx context.Context, ticket, state, cookieState string, cache *kvrocks.Client) (*entity.User, common.ErrorCode, error) {
	if len(state) != 64 || subtle.ConstantTimeCompare([]byte(state), []byte(cookieState)) != 1 {
		return nil, common.CodeDataError, ErrCASInvalidState
	}
	if cache == nil {
		return nil, common.CodeServerError, ErrCASInvalidState
	}
	key := casStateKey(state)
	binding, err := cache.Get(ctx, key)
	channel, _, ok := strings.Cut(binding, ":")
	if err != nil || !ok {
		return nil, common.CodeDataError, ErrCASInvalidState
	}
	provider, ok := loadCASProviders(ctx)[channel]
	if !ok {
		return nil, common.CodeDataError, ErrCASInvalidChannel
	}
	consumed, err := cache.DeleteIfEqual(ctx, key, casStateBinding(channel, provider.Config))
	if err != nil || !consumed {
		return nil, common.CodeDataError, ErrCASInvalidState
	}
	if ticket == "" {
		return nil, common.CodeDataError, ErrCASMissingTicket
	}
	client, err := cas.NewClient(provider.Config)
	if err != nil {
		return nil, common.CodeServerError, err
	}
	info, err := client.ValidateTicket(ctx, client.ServiceURL(state), ticket)
	if err != nil {
		return nil, common.CodeAuthenticationError, err
	}
	return s.casSignIn(ctx, channel, info)
}

// casSignIn follows the account mapping used by the existing directory login.
func (s *UserService) casSignIn(ctx context.Context, channel string, info *cas.UserInfo) (*entity.User, common.ErrorCode, error) {
	if info.Email == "" {
		return nil, common.CodeDataError, ErrOAuthEmailMissing
	}
	existing, err := s.userDAO.GetByEmail(ctx, dao.DB, info.Email)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, common.CodeServerError, fmt.Errorf("find CAS account: %w", err)
	}
	if err == nil && existing != nil {
		if existing.IsActive == "0" {
			return nil, common.CodeForbidden, ErrOAuthUserInactive
		}
		if code, err := validateLoginTenant(ctx, existing); err != nil {
			return nil, code, err
		}
		token := common.GenerateToken()
		existing.AccessToken = &token
		now := time.Now().Truncate(time.Second)
		existing.LastLoginTime = &now
		if err := s.userDAO.Update(ctx, dao.DB, existing); err != nil {
			return nil, common.CodeServerError, fmt.Errorf("update user: %w", err)
		}
		if billingService := billing.Get(); billingService != nil && billingService.Enabled() {
			billingService.SyncLoginTokenRateLimit(ctx, existing.ID, token)
		}
		return existing, common.CodeSuccess, nil
	}
	return s.registerOAuthUser(ctx, channel, &oauth.UserInfo{Email: info.Email, Username: info.Username, Nickname: info.Nickname})
}
