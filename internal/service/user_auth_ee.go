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
	"fmt"
	"strings"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/engine/kvrocks"
	"ragflow/internal/entity"
	"ragflow/internal/utility"
)

// ErrNoActiveLoginTenant means the user has no joined tenant available for login.
var ErrNoActiveLoginTenant = common.NewCodedError(common.CodeForbidden, "No active tenant is available for login")

func resolveUserAuthTenant(ctx context.Context, user *entity.User, tenantID string) (string, error) {
	if user == nil {
		return "", fmt.Errorf("user is required")
	}
	if user.IsSuperuser != nil && *user.IsSuperuser {
		return "", nil
	}
	if tenantID == "" {
		return "", fmt.Errorf("tenant context is required")
	}
	if _, err := dao.NewUserTenantDAO().FilterByUserIDAndTenantID(ctx, dao.DB, user.ID, tenantID); err != nil {
		return "", fmt.Errorf("user is not an active member of tenant %q", tenantID)
	}
	if _, err := dao.NewTenantDAO().GetByID(ctx, dao.DB, tenantID); err != nil {
		return "", fmt.Errorf("tenant %q is inactive or unavailable: %w", tenantID, err)
	}
	return tenantID, nil
}

// ResolveLoginTenantID chooses the user's last active tenant, falling back to
// their personal tenant.
func ResolveLoginTenantID(ctx context.Context, user *entity.User) (string, error) {
	if user == nil {
		return "", fmt.Errorf("user is required")
	}
	if user.IsSuperuser != nil && *user.IsSuperuser {
		return "", nil
	}
	tenantID, err := dao.NewUserTenantDAO().FindActiveTenantIDForLogin(ctx, dao.DB, user.ID)
	if err != nil {
		return "", fmt.Errorf("find active tenant for login: %w", err)
	}
	if tenantID == "" {
		return "", ErrNoActiveLoginTenant
	}
	return tenantID, nil
}

func passwordResetCacheKeys(email string) emailVerificationKeys {
	code, attempts, cooldown, lock := utility.OTPRedisKeys(email)
	return emailVerificationKeys{
		captcha:  "captcha:" + email,
		code:     code,
		attempts: attempts,
		cooldown: cooldown,
		lock:     lock,
		verified: utility.OTPVerifiedRedisKey(email),
	}
}

// ForgotIssueCaptcha returns a PNG challenge for an existing account.
func (s *UserService) ForgotIssueCaptcha(ctx context.Context, email string) ([]byte, common.ErrorCode, error) {
	if email == "" {
		return nil, common.CodeArgumentError, fmt.Errorf("email is required")
	}
	if _, err := s.userDAO.GetByEmail(ctx, dao.DB, email); err != nil {
		return nil, common.CodeDataError, fmt.Errorf("invalid email")
	}
	v := newEmailVerifier()
	image, err := v.issueCaptcha(ctx, passwordResetCacheKeys(email))
	return image, verificationErrorCode(err), err
}

// ForgotSendOTP sends a password reset code after validating the image challenge.
func (s *UserService) ForgotSendOTP(ctx context.Context, email, captcha string) (common.ErrorCode, error) {
	if email == "" || strings.TrimSpace(captcha) == "" {
		return common.CodeArgumentError, fmt.Errorf("email and captcha required")
	}
	if _, err := s.userDAO.GetByEmail(ctx, dao.DB, email); err != nil {
		return common.CodeDataError, fmt.Errorf("invalid email")
	}
	v := newEmailVerifier()
	err := v.sendOTP(ctx, passwordResetCacheKeys(email), email, captcha,
		"Your Password Reset Code", "Your password reset code")
	return verificationErrorCode(err), err
}

// ForgotVerifyOTP verifies the email code and allows password reset for five minutes.
func (s *UserService) ForgotVerifyOTP(ctx context.Context, email, otp string) (common.ErrorCode, error) {
	if email == "" || otp == "" {
		return common.CodeArgumentError, fmt.Errorf("email and otp are required")
	}
	if _, err := s.userDAO.GetByEmail(ctx, dao.DB, email); err != nil {
		return common.CodeDataError, fmt.Errorf("invalid email")
	}
	v := newEmailVerifier()
	err := v.verifyOTP(ctx, passwordResetCacheKeys(email), otp, "1")
	return verificationErrorCode(err), err
}

// ForgotResetPasswordRequest carries the password reset body. Required fields
// are checked by the service so the endpoint returns a concise validation error.
type ForgotResetPasswordRequest struct {
	Email              string `json:"email"`
	NewPassword        string `json:"new_password"`
	ConfirmNewPassword string `json:"confirm_new_password"`
}

// ForgotResetPassword updates the password and login token, then consumes the
// successful email verification. The handler uses the returned user to log in.
func (s *UserService) ForgotResetPassword(ctx context.Context, req *ForgotResetPasswordRequest) (*entity.User, common.ErrorCode, error) {
	if req.Email == "" || req.NewPassword == "" || req.ConfirmNewPassword == "" {
		return nil, common.CodeArgumentError, fmt.Errorf("email and passwords are required")
	}

	rc := kvrocks.Get()
	verifiedKey := utility.OTPVerifiedRedisKey(req.Email)
	if v, _ := rc.Get(ctx, verifiedKey); v != "1" {
		return nil, common.CodeAuthenticationError, fmt.Errorf("email not verified")
	}

	plain, err := common.DecryptPassword(req.NewPassword)
	if err != nil {
		return nil, common.CodeServerError, fmt.Errorf("fail to decrypt password")
	}
	confirm, err := common.DecryptPassword(req.ConfirmNewPassword)
	if err != nil {
		return nil, common.CodeServerError, fmt.Errorf("fail to decrypt password")
	}
	if plain != confirm {
		return nil, common.CodeArgumentError, fmt.Errorf("passwords do not match")
	}

	user, err := s.userDAO.GetByEmail(ctx, dao.DB, req.Email)
	if err != nil {
		return nil, common.CodeDataError, fmt.Errorf("invalid email")
	}
	if code, err := validateLoginTenant(ctx, user); err != nil {
		return nil, code, err
	}

	hashed, err := common.GenerateWerkzeugPasswordHash(plain)
	if err != nil {
		return nil, common.CodeServerError, fmt.Errorf("failed to hash new password: %w", err)
	}
	user.Password = &hashed

	// Auto-login: rotate the access token like LoginByEmail does so the
	// handler can immediately mint an Authorization header.
	token := common.GenerateToken()
	user.AccessToken = &token
	now := time.Now().Truncate(time.Second)
	user.LastLoginTime = &now

	if err = s.userDAO.Update(ctx, dao.DB, user); err != nil {
		return nil, common.CodeServerError, fmt.Errorf("failed to reset password: %w", err)
	}

	rc.Delete(ctx, verifiedKey)
	return user, common.CodeSuccess, nil
}
