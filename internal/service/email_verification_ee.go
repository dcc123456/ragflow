//go:build ee

package service

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/engine/kvrocks"
	"ragflow/internal/utility"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type emailVerifier struct {
	db    *gorm.DB
	cache *redis.Client
}

type emailVerificationKeys struct {
	captcha, code, attempts, cooldown, lock, verified string
}

func newEmailVerifier() emailVerifier {
	v := emailVerifier{db: dao.DB}
	if cache := kvrocks.Get(); cache != nil {
		v.cache = cache.GetClient()
	}
	return v
}

func verificationErrorCode(err error) common.ErrorCode {
	if err == nil {
		return common.CodeSuccess
	}
	var coded *common.CodedError
	if errors.As(err, &coded) {
		return coded.Code
	}
	return common.CodeServerError
}

func (v *emailVerifier) issueCaptcha(ctx context.Context, keys emailVerificationKeys) ([]byte, error) {
	if v.cache == nil {
		return nil, fmt.Errorf("verification cache is unavailable")
	}
	answer, err := utility.GenerateCaptchaCode()
	if err != nil {
		return nil, err
	}
	image := utility.RenderCaptchaPNG(answer)
	if err := v.cache.Set(ctx, keys.captcha, answer, time.Minute).Err(); err != nil {
		return nil, err
	}
	return image, nil
}

func (v *emailVerifier) sendOTP(ctx context.Context, keys emailVerificationKeys, email, answer, subject, codeLabel string) error {
	if strings.TrimSpace(answer) == "" {
		return common.NewCodedError(common.CodeArgumentError, "email and captcha required")
	}
	if v.cache == nil {
		return fmt.Errorf("verification cache is unavailable")
	}
	raw, err := v.cache.Get(ctx, keys.captcha).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	if errors.Is(err, redis.Nil) {
		return common.NewCodedError(common.CodeNotEffective, "invalid or expired captcha")
	}
	if !strings.EqualFold(raw, strings.TrimSpace(answer)) {
		return common.NewCodedError(common.CodeAuthenticationError, "invalid or expired captcha")
	}
	consumed, err := v.cache.Eval(ctx, `if redis.call('get', KEYS[1]) == ARGV[1] then return redis.call('del', KEYS[1]) end return 0`, []string{keys.captcha}, raw).Int()
	if err != nil {
		return err
	}
	if consumed != 1 {
		return common.NewCodedError(common.CodeNotEffective, "invalid or expired captcha")
	}
	locked, err := v.cache.Exists(ctx, keys.lock).Result()
	if err != nil {
		return err
	}
	if locked != 0 {
		return common.NewCodedError(common.CodeNotEffective, "too many attempts, try later")
	}
	cfg, timeout, err := loadMailSettings(ctx, v.db)
	if err != nil {
		return err
	}
	reservation := common.GenerateToken()
	reservationTTL := min(timeout, time.Duration(1<<63-1)-utility.OTPResendCooldown) + utility.OTPResendCooldown
	reserved, err := v.cache.SetNX(ctx, keys.cooldown, reservation, reservationTTL).Result()
	if err != nil {
		return err
	}
	if !reserved {
		return common.NewCodedError(common.CodeNotEffective, "please wait before requesting another code")
	}
	sent := false
	defer func() {
		if !sent {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			defer cancel()
			v.cache.Eval(cleanup, `if redis.call('get', KEYS[1]) == ARGV[1] then return redis.call('del', KEYS[1]) end return 0`, []string{keys.cooldown}, reservation)
		}
	}()
	otp, err := utility.GenerateOTPCode()
	if err != nil {
		return err
	}
	salt, err := utility.GenerateOTPSalt()
	if err != nil {
		return err
	}
	body := fmt.Sprintf("Hello,\n%s is: %s\nThis code will expire in %d minutes.\n", codeLabel, otp, int(utility.OTPTTL.Minutes()))
	if err := utility.SendSMTPEmail(ctx, cfg, timeout, email, subject, body); err != nil {
		common.Error("Failed to send email verification code", err)
		var notConfigured utility.SMTPNotConfiguredError
		if errors.As(err, &notConfigured) {
			return common.NewCodedError(common.CodeServerError, notConfigured.Error())
		}
		return common.NewCodedError(common.CodeServerError, "failed to send email")
	}
	// Keep the previous code usable until SMTP accepts the replacement.
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	// Only the reservation owner can replace the code and reset its attempt budget.
	committed, err := v.cache.Eval(commitCtx, `
if redis.call('get', KEYS[3]) ~= ARGV[1] then return 0 end
redis.call('set', KEYS[1], ARGV[2], 'EX', ARGV[3])
redis.call('del', KEYS[2])
redis.call('set', KEYS[3], 'sent', 'EX', ARGV[4])
return 1`, []string{keys.code, keys.attempts, keys.cooldown}, reservation, utility.EncodeOTPStorageValue(utility.HashOTPCode(otp, salt), salt), int(utility.OTPTTL.Seconds()), int(utility.OTPResendCooldown.Seconds())).Int()
	if err != nil {
		return err
	}
	if committed != 1 {
		return common.NewCodedError(common.CodeNotEffective, "verification request expired, please request another code")
	}
	sent = true
	return nil
}

func (v *emailVerifier) verifyOTP(ctx context.Context, keys emailVerificationKeys, otp, verifiedValue string) error {
	if strings.TrimSpace(otp) == "" {
		return common.NewCodedError(common.CodeArgumentError, "email and otp are required")
	}
	if v.cache == nil {
		return fmt.Errorf("verification cache is unavailable")
	}
	locked, err := v.cache.Exists(ctx, keys.lock).Result()
	if err != nil {
		return err
	}
	if locked != 0 {
		return common.NewCodedError(common.CodeNotEffective, "too many attempts, try later")
	}
	raw, err := v.cache.Get(ctx, keys.code).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	if errors.Is(err, redis.Nil) {
		return common.NewCodedError(common.CodeNotEffective, "expired otp")
	}
	hash, salt, err := utility.DecodeOTPStorageValue(raw)
	if err != nil {
		return err
	}
	calculated := utility.HashOTPCode(strings.ToUpper(strings.TrimSpace(otp)), salt)
	matches := subtle.ConstantTimeCompare([]byte(hash), []byte(calculated)) == 1
	// Verifying the previous code must not release an in-flight send reservation.
	result, err := v.cache.Eval(ctx, `
if redis.call('exists', KEYS[3]) == 1 then return -2 end
if redis.call('get', KEYS[1]) ~= ARGV[1] then return -1 end
if ARGV[2] == 'true' then
 redis.call('set', KEYS[4], ARGV[6], 'EX', ARGV[3])
 if redis.call('get', KEYS[5]) == 'sent' then redis.call('del', KEYS[5]) end
 redis.call('del', KEYS[1], KEYS[2]); return 1
end
local attempts = redis.call('incr', KEYS[2])
if attempts == 1 then redis.call('expire', KEYS[2], ARGV[3]) end
if attempts >= tonumber(ARGV[4]) then
 redis.call('set', KEYS[3], '1', 'EX', ARGV[5]); redis.call('del', KEYS[1]); return -2
end
return 0`, []string{keys.code, keys.attempts, keys.lock, keys.verified, keys.cooldown}, raw, strconv.FormatBool(matches), int(utility.OTPTTL.Seconds()), utility.OTPAttemptLimit, int(utility.OTPAttemptLockDuration.Seconds()), verifiedValue).Int()
	if err != nil {
		return err
	}
	if result == -2 {
		return common.NewCodedError(common.CodeNotEffective, "too many attempts, try later")
	}
	if result != 1 {
		return common.NewCodedError(common.CodeAuthenticationError, "expired otp")
	}
	return nil
}
