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
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/ldap"
	"ragflow/internal/service/billing"
	"ragflow/internal/service/oauth"

	"go.uber.org/zap"
)

// Sentinel errors surfaced by the directory login. The handler relays the
// message, so they read as something a user can act on.
var (
	// ErrLDAPInvalidChannel is returned for a channel that names no usable
	// server: unknown, disabled, or missing the settings a bind needs.
	ErrLDAPInvalidChannel = errors.New("invalid channel name")
	// ErrLDAPMissingCredentials is returned when either field is blank.
	ErrLDAPMissingCredentials = errors.New("please input your username and password")
	// ErrLDAPAuthFailed covers both a filter that matched nothing and a wrong
	// password: telling them apart would let an unauthenticated caller probe
	// which names exist in the directory.
	ErrLDAPAuthFailed = errors.New("please check if the username and password are correct")
	// ErrLDAPEmailMissing is returned when the matched entry publishes no
	// address, which leaves nothing to bind the local account to.
	ErrLDAPEmailMissing = errors.New("the ldap account has no email address")
)

// ldapAuthenticate is the directory call the login flow makes. Tests replace it
// to exercise the account handling without a directory.
var ldapAuthenticate = ldap.Authenticate

// decodeLDAPSecret strips the base64 layer the login form applies before
// RSA-encrypting, so the directory receives the literal password the user
// typed. Values that do not decode (for example a password whose plaintext
// contains characters outside the base64 alphabet) are passed through as-is.
func decodeLDAPSecret(secret string) string {
	decoded, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		return secret
	}
	return string(decoded)
}

// LDAPLogin authenticates a login name and password against the directory the
// channel names and returns the local account they map to.
//
// The password arrives RSA-encrypted from the login form, exactly like the
// email login, and is only decrypted here so it can be handed to the directory.
// An account is matched by the address the directory resolves, so a first-time
// user is provisioned the same way an OAuth login provisions one.
func (s *UserService) LDAPLogin(ctx context.Context, channel, username, password string) (*entity.User, common.ErrorCode, error) {
	server, ok := lookupLDAPServer(ctx, channel)
	if !ok {
		return nil, common.CodeDataError, fmt.Errorf("%w: %s", ErrLDAPInvalidChannel, channel)
	}
	login := strings.TrimSpace(username)
	if login == "" || password == "" {
		return nil, common.CodeAuthenticationError, ErrLDAPMissingCredentials
	}

	secret, err := common.DecryptPassword(password)
	if err != nil {
		return nil, common.CodeServerError, fmt.Errorf("decrypt password: %w", err)
	}
	secret = decodeLDAPSecret(secret)

	identity, err := ldapAuthenticate(ctx, server.ldapConfig(), login, secret)
	switch {
	case err == nil:
	case errors.Is(err, ldap.ErrInvalidCredentials), errors.Is(err, ldap.ErrUserNotFound):
		return nil, common.CodeAuthenticationError, ErrLDAPAuthFailed
	case errors.Is(err, ldap.ErrMissingCredentials):
		return nil, common.CodeAuthenticationError, ErrLDAPMissingCredentials
	default:
		common.Warn("ldap authentication failed",
			zap.String("channel", server.Channel),
			zap.String("username", login),
			zap.Error(err))
		return nil, common.CodeServerError, fmt.Errorf("ldap authentication failed: %w", err)
	}
	if identity.Email == "" {
		return nil, common.CodeDataError, ErrLDAPEmailMissing
	}

	return s.ldapSignIn(ctx, server.Channel, identity)
}

// ldapSignIn maps a verified directory identity onto a local account. The
// directory is the authority on the credential, so an existing account is not
// asked for its local password: it is bound by email and signed in.
func (s *UserService) ldapSignIn(ctx context.Context, channel string, identity *ldap.Identity) (*entity.User, common.ErrorCode, error) {
	existing, err := s.userDAO.GetByEmail(ctx, dao.DB, identity.Email)
	if err == nil && existing != nil {
		if existing.IsActive == "0" {
			return nil, common.CodeForbidden, ErrOAuthUserInactive
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

	created, code, err := s.registerOAuthUser(ctx, channel, &oauth.UserInfo{
		Email:     identity.Email,
		Nickname:  identity.Nickname,
		AvatarURL: identity.AvatarURL,
	})
	if err != nil {
		return nil, code, err
	}
	return created, common.CodeSuccess, nil
}
