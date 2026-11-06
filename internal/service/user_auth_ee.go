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
	"ragflow/internal/common"
	"ragflow/internal/engine/kvrocks"
	"ragflow/internal/server"
	"ragflow/internal/utility"
	"strconv"
	"strings"
	"time"

	"ragflow/internal/dao"
	"ragflow/internal/entity"
)

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
	return tenantID, nil
}

// ForgotIssueCaptcha mints a captcha for the given email and stores the
// expected text in Redis under utility.CaptchaIDRedisKey, keyed by a
// fresh server-side captcha_id, with a 60s TTL. Returns the captcha_id
// and a renderable SVG image (data URL) the FE drops into <img src> so
// the human can read the challenge and type the answer. The plaintext
// code itself is never sent to the client outside the rendered image.
//
// Refuses unknown emails to avoid leaking the user list — matches Python.
func (s *UserService) ForgotIssueCaptcha(ctx context.Context, email string) (captchaID, imageDataURL string, code common.ErrorCode, err error) {
	if email == "" {
		return "", "", common.CodeArgumentError, fmt.Errorf("email is required")
	}
	if _, err = s.userDAO.GetByEmail(ctx, dao.DB, email); err != nil {
		return "", "", common.CodeDataError, fmt.Errorf("invalid email")
	}

	text, err := utility.GenerateCaptchaCode()
	if err != nil {
		return "", "", common.CodeServerError, err
	}
	captchaID = common.GenerateToken()
	if ok := kvrocks.Get().Set(ctx, utility.CaptchaIDRedisKey(captchaID), text, 60*time.Second); !ok {
		return "", "", common.CodeServerError, fmt.Errorf("failed to store captcha")
	}
	imageDataURL = utility.RenderCaptchaPNGDataURL(text)
	return captchaID, imageDataURL, common.CodeSuccess, nil
}

// ForgotSendOTP verifies the captcha (looked up by the server-issued
// captcha_id), then issues an OTP and emails it. Hash-and-salt is
// stored in Redis under the keys returned by utility.OTPRedisKeys.
// Resend cooldown and per-email lockout behaviour otherwise match the
// Python implementation byte-for-byte.
func (s *UserService) ForgotSendOTP(ctx context.Context, email, captchaID, captcha string) (common.ErrorCode, error) {
	if email == "" || captchaID == "" || captcha == "" {
		return common.CodeArgumentError, fmt.Errorf("email, captcha_id and captcha required")
	}
	if _, err := s.userDAO.GetByEmail(ctx, dao.DB, email); err != nil {
		return common.CodeDataError, fmt.Errorf("invalid email")
	}

	rc := kvrocks.Get()
	captchaKey := utility.CaptchaIDRedisKey(captchaID)
	stored, _ := rc.Get(ctx, captchaKey)
	if stored == "" {
		return common.CodeNotEffective, fmt.Errorf("invalid or expired captcha")
	}
	if !strings.EqualFold(strings.TrimSpace(stored), strings.TrimSpace(captcha)) {
		return common.CodeAuthenticationError, fmt.Errorf("invalid or expired captcha")
	}
	// One-shot: consume the captcha so a leaked captcha_id cannot be
	// reused for a stream of OTP requests.
	rc.Delete(ctx, captchaKey)

	codeKey, attemptsKey, lastSentKey, lockKey := utility.OTPRedisKeys(email)

	// Lockout — a previous verify burst already locked this email; do not
	// let a request for a new OTP wipe the lock (deliberate divergence
	// from the Python implementation, which deletes the lock here and so
	// allows a locked attacker to clear their own lockout by re-requesting).
	if locked, _ := rc.Get(ctx, lockKey); locked != "" {
		return common.CodeNotEffective, fmt.Errorf("too many attempts, try later")
	}

	// Resend cooldown — refuse if we already sent within the window.
	if lastSent, _ := rc.Get(ctx, lastSentKey); lastSent != "" {
		ts, parseErr := strconv.ParseInt(lastSent, 10, 64)
		if parseErr == nil {
			elapsed := time.Since(time.Unix(ts, 0))
			remaining := utility.OTPResendCooldown - elapsed
			if remaining > 0 {
				return common.CodeNotEffective, fmt.Errorf("you still have to wait %d seconds", int(remaining.Seconds()))
			}
		}
	}

	otp, err := utility.GenerateOTPCode()
	if err != nil {
		return common.CodeServerError, err
	}
	salt, err := utility.GenerateOTPSalt()
	if err != nil {
		return common.CodeServerError, err
	}
	codeHash := utility.HashOTPCode(otp, salt)
	now := strconv.FormatInt(time.Now().Unix(), 10)

	// Snapshot the previous OTP-flow state so we can restore it if email
	// delivery fails — otherwise the user is throttled by lastSentKey
	// even though they never received the code.
	prevCode, _ := rc.Get(ctx, codeKey)
	prevAttempts, _ := rc.Get(ctx, attemptsKey)
	prevLastSent, _ := rc.Get(ctx, lastSentKey)

	if !rc.Set(ctx, codeKey, utility.EncodeOTPStorageValue(codeHash, salt), utility.OTPTTL) {
		return common.CodeServerError, fmt.Errorf("failed to store otp")
	}
	rc.Set(ctx, attemptsKey, "0", utility.OTPTTL)
	rc.Set(ctx, lastSentKey, now, utility.OTPTTL)
	// Note: lockKey is intentionally not cleared here. If the user has
	// been locked out by a previous verify burst, requesting a new OTP
	// does not lift the lock — we already refused above.

	ttlMin := int(utility.OTPTTL.Minutes())
	cfg := server.GetConfig()
	if err = utility.SendResetCodeEmail(cfg.GetSMTPConfig(), email, otp, ttlMin); err != nil {
		// Roll back: restore prior code/attempts/last-sent or remove the
		// keys we just wrote so the next attempt isn't blocked by the
		// resend cooldown a failed send just installed.
		if prevCode != "" {
			rc.Set(ctx, codeKey, prevCode, utility.OTPTTL)
		} else {
			rc.Delete(ctx, codeKey)
		}
		if prevAttempts != "" {
			rc.Set(ctx, attemptsKey, prevAttempts, utility.OTPTTL)
		} else {
			rc.Delete(ctx, attemptsKey)
		}
		if prevLastSent != "" {
			rc.Set(ctx, lastSentKey, prevLastSent, utility.OTPTTL)
		} else {
			rc.Delete(ctx, lastSentKey)
		}
		return common.CodeServerError, fmt.Errorf("failed to send email")
	}
	return common.CodeSuccess, nil
}

// ForgotVerifyOTP checks an OTP submitted by the user. On success, it
// consumes the OTP/attempt counters and writes a short-lived "verified"
// flag the reset endpoint will gate on.
func (s *UserService) ForgotVerifyOTP(ctx context.Context, email, otp string) (common.ErrorCode, error) {
	if email == "" || otp == "" {
		return common.CodeArgumentError, fmt.Errorf("email and otp are required")
	}
	if _, err := s.userDAO.GetByEmail(ctx, dao.DB, email); err != nil {
		return common.CodeDataError, fmt.Errorf("invalid email")
	}

	rc := kvrocks.Get()
	codeKey, attemptsKey, lastSentKey, lockKey := utility.OTPRedisKeys(email)

	if locked, _ := rc.Get(ctx, lockKey); locked != "" {
		return common.CodeNotEffective, fmt.Errorf("too many attempts, try later")
	}

	stored, _ := rc.Get(ctx, codeKey)
	if stored == "" {
		return common.CodeNotEffective, fmt.Errorf("expired otp")
	}
	storedHash, salt, err := utility.DecodeOTPStorageValue(stored)
	if err != nil {
		return common.CodeServerError, fmt.Errorf("otp storage corrupted")
	}

	if utility.HashOTPCode(strings.ToUpper(strings.TrimSpace(otp)), salt) != storedHash {
		// bump attempts; lock on >= limit
		attempts := 0
		if cur, _ := rc.Get(ctx, attemptsKey); cur != "" {
			if n, perr := strconv.Atoi(cur); perr == nil {
				attempts = n
			}
		}
		attempts++
		rc.Set(ctx, attemptsKey, strconv.Itoa(attempts), utility.OTPTTL)
		if attempts >= utility.OTPAttemptLimit {
			rc.Set(ctx, lockKey, strconv.FormatInt(time.Now().Unix(), 10), utility.OTPAttemptLockDuration)
		}
		return common.CodeAuthenticationError, fmt.Errorf("expired otp")
	}

	// Success: clear OTP state, mark email verified.
	rc.Delete(ctx, codeKey)
	rc.Delete(ctx, attemptsKey)
	rc.Delete(ctx, lastSentKey)
	rc.Delete(ctx, lockKey)
	if !rc.Set(ctx, utility.OTPVerifiedRedisKey(email), "1", utility.OTPTTL) {
		return common.CodeServerError, fmt.Errorf("failed to set verification state")
	}
	return common.CodeSuccess, nil
}

// ForgotResetPasswordRequest carries the JSON body of /auth/password/reset.
//
// No `binding` tags on purpose: gin's validator fires inside
// c.ShouldBindJSON and produces a verbose
// `Key: 'ForgotResetPasswordRequest.Email' Error:Field validation ...`
// message that diverges from the Python contract for this endpoint,
// which returns the friendlier `"email and passwords are required"`
// (api/apps/restful_apis/user_api.py:forget_reset_password). Letting
// the binding succeed with zero values means the existing service
// check below produces the matching message, and an entirely missing
// JSON body now gets exactly Python's response.
type ForgotResetPasswordRequest struct {
	Email              string `json:"email"`
	NewPassword        string `json:"new_password"`
	ConfirmNewPassword string `json:"confirm_new_password"`
}

// ForgotResetPassword finalises the reset: only proceeds if the verified
// flag is set, validates the two ciphertexts match after RSA decryption,
// updates the password hash, and clears the verified flag. Returns the
// user so the handler can auto-login (matching Python's
// `construct_response(auth=user.get_id())`).
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
