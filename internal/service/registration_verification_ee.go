//go:build ee

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/server"

	"gorm.io/gorm"
)

type registrationVerifier struct {
	emailVerifier
	registerEnabled bool
}

type registrationProofContextKey struct{}

var registrationEmailPattern = regexp.MustCompile(`^[\w\._-]+@([\w_-]+\.)+[\w-]{2,}$`)

// WithRegistrationProof carries the browser's registration proof to the service.
func WithRegistrationProof(ctx context.Context, proof string) context.Context {
	return context.WithValue(ctx, registrationProofContextKey{}, proof)
}

func registrationProofHash(proof string) string {
	hash := sha256.Sum256([]byte(proof))
	return hex.EncodeToString(hash[:])
}

func newRegistrationVerifier() *registrationVerifier {
	v := &registrationVerifier{emailVerifier: newEmailVerifier()}
	if cfg := server.GetConfig(); cfg != nil {
		v.registerEnabled = cfg.EnableRegister()
	}
	return v
}

func registrationCacheKeys(email string) emailVerificationKeys {
	return emailVerificationKeys{
		captcha:  "reg_captcha:" + email,
		code:     "reg_otp:" + email,
		attempts: "reg_otp_attempts:" + email,
		cooldown: "reg_otp_last_sent:" + email,
		lock:     "reg_otp_lock:" + email,
		verified: "reg_otp:verified:" + email,
	}
}

func (v *registrationVerifier) consumeVerification(ctx context.Context, email string) (common.ErrorCode, error) {
	enabled, err := emailVerificationEnabled(ctx, v.db)
	if err != nil {
		return common.CodeServerError, fmt.Errorf("failed to load email verification setting: %w", err)
	}
	if !enabled {
		return common.CodeSuccess, nil
	}
	if v.cache == nil {
		return common.CodeServerError, fmt.Errorf("verification cache is unavailable")
	}
	proof, _ := ctx.Value(registrationProofContextKey{}).(string)
	if proof == "" {
		return common.CodeAuthenticationError, fmt.Errorf("Email not verified. Please verify your email first.")
	}
	key := registrationCacheKeys(strings.ToLower(strings.TrimSpace(email))).verified
	consumed, err := v.cache.Eval(ctx, `if redis.call('get', KEYS[1]) == ARGV[1] then return redis.call('del', KEYS[1]) end return 0`, []string{key}, registrationProofHash(proof)).Int()
	if err != nil {
		return common.CodeServerError, fmt.Errorf("failed to read email verification state: %w", err)
	}
	if consumed != 1 {
		return common.CodeAuthenticationError, fmt.Errorf("Email not verified. Please verify your email first.")
	}
	return common.CodeSuccess, nil
}

// consumeRegistrationProof validates and consumes the proof when email verification is enabled.
func consumeRegistrationProof(ctx context.Context, email string) (common.ErrorCode, error) {
	return newRegistrationVerifier().consumeVerification(ctx, email)
}

func (v *registrationVerifier) checkEmail(ctx context.Context, email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || !registrationEmailPattern.MatchString(email) {
		return "", common.NewCodedError(common.CodeArgumentError, "invalid email")
	}
	if !v.registerEnabled {
		return "", common.NewCodedError(common.CodeOperatingError, "user registration is disabled")
	}
	enabled, err := emailVerificationEnabled(ctx, v.db)
	if err != nil {
		return "", err
	}
	if !enabled {
		return "", common.NewCodedError(common.CodeOperatingError, "email verification is disabled")
	}
	user, err := dao.NewUserDAO().GetByEmail(ctx, v.db, email)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}
	if user != nil {
		return "", common.NewCodedError(common.CodeDataError, "Email has already been registered!")
	}
	return email, nil
}

func (v *registrationVerifier) issueCaptcha(ctx context.Context, email string) ([]byte, error) {
	email, err := v.checkEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	return v.emailVerifier.issueCaptcha(ctx, registrationCacheKeys(email))
}

func (v *registrationVerifier) sendOTP(ctx context.Context, email, answer string) error {
	email, err := v.checkEmail(ctx, email)
	if err != nil {
		return err
	}
	return v.emailVerifier.sendOTP(ctx, registrationCacheKeys(email), email, answer,
		"Your Registration Verification Code", "Your registration verification code")
}

func (v *registrationVerifier) verifyOTP(ctx context.Context, email, otp string) (string, error) {
	email, err := v.checkEmail(ctx, email)
	if err != nil {
		return "", err
	}
	proof := common.GenerateToken()
	if err := v.emailVerifier.verifyOTP(ctx, registrationCacheKeys(email), otp, registrationProofHash(proof)); err != nil {
		return "", err
	}
	return proof, nil
}

// IssueRegistrationCaptcha creates a short-lived image challenge bound to the email.
func (s *UserService) IssueRegistrationCaptcha(ctx context.Context, email string) ([]byte, error) {
	return newRegistrationVerifier().issueCaptcha(ctx, email)
}

// SendRegistrationOTP consumes the image challenge and emails a verification code.
func (s *UserService) SendRegistrationOTP(ctx context.Context, email, answer string) error {
	return newRegistrationVerifier().sendOTP(ctx, email, answer)
}

// VerifyRegistrationOTP consumes the code and issues a five-minute registration proof.
func (s *UserService) VerifyRegistrationOTP(ctx context.Context, email, otp string) (string, error) {
	return newRegistrationVerifier().verifyOTP(ctx, email, otp)
}
