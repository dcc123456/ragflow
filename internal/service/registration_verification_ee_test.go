//go:build ee

package service

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"net"
	"net/textproto"
	"regexp"
	"strings"
	"testing"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"
	"ragflow/internal/utility"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func registrationTestVerifier(t *testing.T) (*registrationVerifier, *miniredis.Miniredis) {
	t.Helper()
	cache := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: cache.Addr()})
	t.Cleanup(func() { client.Close() })
	v := &registrationVerifier{
		emailVerifier:   emailVerifier{db: testutil.SetupTestDB(t, &entity.SystemSettings{}, &entity.User{}), cache: client},
		registerEnabled: true,
	}
	setRegistrationSetting(t, v, "email_verification.enabled", "true")
	return v, cache
}

func setRegistrationSetting(t *testing.T, v *registrationVerifier, name, value string) {
	t.Helper()
	if err := v.db.Save(&entity.SystemSettings{Name: name, Value: value, DataType: "string"}).Error; err != nil {
		t.Fatal(err)
	}
}

func registrationTestCaptcha(t *testing.T, v *registrationVerifier, email string) string {
	t.Helper()
	image, err := v.issueCaptcha(t.Context(), email)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(bytes.NewReader(image)); err != nil {
		t.Fatalf("captcha is not a PNG image: %v", err)
	}
	answer, err := v.cache.Get(t.Context(), registrationCacheKeys(strings.ToLower(strings.TrimSpace(email))).captcha).Result()
	if err != nil {
		t.Fatal(err)
	}
	return answer
}

func registrationTestSMTP(t *testing.T, v *registrationVerifier, cancelRequest context.CancelFunc) <-chan string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	for name, value := range map[string]string{
		"mail.server": "127.0.0.1", "mail.port": fmt.Sprint(listener.Addr().(*net.TCPAddr).Port),
		"mail.use_ssl": "false", "mail.use_tls": "false", "mail.timeout": "1",
		"mail.default_sender": "RAGFlow <sender@example.com>",
	} {
		setRegistrationSetting(t, v, name, value)
	}
	message := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		wire := textproto.NewConn(conn)
		wire.PrintfLine("220 test SMTP")
		for {
			line, err := wire.ReadLine()
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"), strings.HasPrefix(line, "MAIL FROM:"), strings.HasPrefix(line, "RCPT TO:"):
				wire.PrintfLine("250 OK")
			case line == "DATA":
				wire.PrintfLine("354 send message")
				data, err := wire.ReadDotBytes()
				if err != nil {
					return
				}
				message <- string(data)
				wire.PrintfLine("250 accepted")
			case line == "QUIT":
				if cancelRequest != nil {
					cancelRequest()
				}
				wire.PrintfLine("221 bye")
				return
			default:
				wire.PrintfLine("500 unexpected command")
			}
		}
	}()
	return message
}

func TestRegistrationVerification(t *testing.T) {
	v, cache := registrationTestVerifier(t)
	mailbox := registrationTestSMTP(t, v, nil)
	const email = "new@example.com"
	keys := registrationCacheKeys(email)
	cache.Set(keys.attempts, "2")
	answer := registrationTestCaptcha(t, v, " NEW@EXAMPLE.COM ")
	if err := v.sendOTP(t.Context(), email, strings.ToLower(answer)); err != nil {
		t.Fatal(err)
	}
	if cache.Exists(keys.attempts) {
		t.Fatal("new OTP inherited the previous code's failed attempts")
	}
	var message string
	select {
	case message = <-mailbox:
	case <-time.After(3 * time.Second):
		t.Fatal("verification email was not delivered")
	}
	if !strings.Contains(message, "To: "+email) || !strings.Contains(message, "From: \"RAGFlow\" <sender@example.com>") {
		t.Fatalf("unexpected email headers: %s", message)
	}
	match := regexp.MustCompile(`verification code is: ([A-Z]{4})`).FindStringSubmatch(message)
	if len(match) != 2 {
		t.Fatalf("email has no verification code: %s", message)
	}
	stored, err := cache.Get(keys.code)
	if err != nil || strings.Contains(stored, match[1]) || cache.TTL(keys.code) != utility.OTPTTL {
		t.Fatalf("OTP was not stored as a hash with an expiry: %v", err)
	}
	if err := v.sendOTP(t.Context(), email, answer); err == nil {
		t.Fatal("captcha was reused")
	}
	answer = registrationTestCaptcha(t, v, email)
	if err := v.sendOTP(t.Context(), email, answer); err == nil {
		t.Fatal("resend cooldown was bypassed")
	}
	proof, err := v.verifyOTP(t.Context(), email, strings.ToLower(match[1]))
	if err != nil {
		t.Fatalf("verify OTP: %v", err)
	}
	storedProof, err := cache.Get(keys.verified)
	if err != nil || proof == "" || storedProof != registrationProofHash(proof) || cache.TTL(keys.verified) != utility.OTPTTL {
		t.Fatalf("email was not marked verified with an expiry: %v", err)
	}
	if _, err := v.verifyOTP(t.Context(), email, match[1]); err == nil {
		t.Fatal("OTP was reused")
	}
	for _, submitted := range []string{"", "wrong-proof"} {
		if code, err := v.consumeVerification(WithRegistrationProof(t.Context(), submitted), email); err == nil || code != common.CodeAuthenticationError {
			t.Fatal("another client consumed the registration proof")
		}
	}
	ctx := WithRegistrationProof(t.Context(), proof)
	if code, err := v.consumeVerification(ctx, email); err != nil || code != common.CodeSuccess {
		t.Fatalf("proof owner could not register: %v", err)
	}
	if _, err := v.consumeVerification(ctx, email); err == nil {
		t.Fatal("registration proof was reused")
	}
}

func TestRegistrationVerificationGuards(t *testing.T) {
	v, cache := registrationTestVerifier(t)
	const email = "new@example.com"
	for _, email := range []string{"invalid", "new+tag@example.com"} {
		if _, err := v.issueCaptcha(t.Context(), email); err == nil {
			t.Fatalf("unsupported registration email accepted: %s", email)
		}
	}
	if err := v.db.Create(&entity.User{ID: "existing", Email: "existing@example.com"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := v.issueCaptcha(t.Context(), "existing@example.com"); err == nil {
		t.Fatal("registered email accepted")
	}
	v.registerEnabled = false
	if _, err := v.issueCaptcha(t.Context(), email); err == nil {
		t.Fatal("disabled registration accepted")
	}
	v.registerEnabled = true
	setRegistrationSetting(t, v, "email_verification.enabled", "false")
	if _, err := v.issueCaptcha(t.Context(), email); err == nil {
		t.Fatal("disabled verification accepted")
	}
	setRegistrationSetting(t, v, "email_verification.enabled", "true")
	answer := registrationTestCaptcha(t, v, email)
	if err := v.sendOTP(t.Context(), "other@example.com", answer); err == nil {
		t.Fatal("captcha was accepted for another email")
	}
	answer = registrationTestCaptcha(t, v, email)
	cache.FastForward(time.Minute)
	if err := v.sendOTP(t.Context(), email, answer); err == nil {
		t.Fatal("expired captcha accepted")
	}
	registrationTestSMTP(t, v, nil)
	answer = registrationTestCaptcha(t, v, email)
	if err := v.sendOTP(t.Context(), email, answer); err != nil {
		t.Fatal(err)
	}
	for range utility.OTPAttemptLimit {
		if _, err := v.verifyOTP(t.Context(), email, "WRONG"); err == nil {
			t.Fatal("wrong OTP accepted")
		}
	}
	keys := registrationCacheKeys(email)
	if !cache.Exists(keys.lock) || cache.Exists(keys.code) {
		t.Fatal("too many OTP attempts did not lock and invalidate the code")
	}
	cache.FastForward(utility.OTPResendCooldown)
	answer = registrationTestCaptcha(t, v, email)
	if err := v.sendOTP(t.Context(), email, answer); err == nil {
		t.Fatal("resend bypassed the OTP lock")
	}
}

func TestRegistrationMailSurvivesRequestCancellation(t *testing.T) {
	v, _ := registrationTestVerifier(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	mailbox := registrationTestSMTP(t, v, cancel)
	setRegistrationSetting(t, v, "mail.timeout", "180")
	if _, timeout, err := loadMailSettings(t.Context(), v.db); err != nil || timeout != 180*time.Second {
		t.Fatalf("saved SMTP timeout was not usable: timeout=%v error=%v", timeout, err)
	}
	const email = "new@example.com"
	answer := registrationTestCaptcha(t, v, email)
	if err := v.sendOTP(ctx, email, answer); err != nil {
		t.Fatalf("accepted email lost its verification code: %v", err)
	}
	if ctx.Err() == nil {
		t.Fatal("request was not canceled after SMTP accepted the email")
	}
	select {
	case message := <-mailbox:
		match := regexp.MustCompile(`verification code is: ([A-Z]{4})`).FindStringSubmatch(message)
		if len(match) != 2 {
			t.Fatal("verification email had no code")
		}
		if _, err := v.verifyOTP(t.Context(), email, match[1]); err != nil {
			t.Fatalf("delivered code was not usable: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("verification email was not delivered")
	}
}

func TestRegistrationSMTPTimeoutOverflow(t *testing.T) {
	v, cache := registrationTestVerifier(t)
	registrationTestSMTP(t, v, nil)
	setRegistrationSetting(t, v, "mail.timeout", "9223372036")
	const email = "new@example.com"
	answer := registrationTestCaptcha(t, v, email)
	if err := v.sendOTP(t.Context(), email, answer); err != nil {
		t.Fatalf("maximum SMTP timeout overflowed the reservation: %v", err)
	}
	keys := registrationCacheKeys(email)
	if !cache.Exists(keys.code) || cache.TTL(keys.cooldown) != utility.OTPResendCooldown {
		t.Fatal("maximum SMTP timeout did not leave a usable OTP and resend cooldown")
	}
}

func TestRegistrationSMTPTimeout(t *testing.T) {
	v, cache := registrationTestVerifier(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			conn.SetReadDeadline(time.Now().Add(4 * time.Second))
			var buf [1]byte
			conn.Read(buf[:])
		}
	}()
	setRegistrationSetting(t, v, "mail.server", "127.0.0.1")
	setRegistrationSetting(t, v, "mail.port", fmt.Sprint(listener.Addr().(*net.TCPAddr).Port))
	setRegistrationSetting(t, v, "mail.use_ssl", "false")
	setRegistrationSetting(t, v, "mail.default_sender", "sender@example.com")
	setRegistrationSetting(t, v, "mail.timeout", "1")
	const email = "new@example.com"
	keys := registrationCacheKeys(email)
	cache.Set(keys.code, "previous OTP")
	answer := registrationTestCaptcha(t, v, email)
	start := time.Now()
	err = v.sendOTP(t.Context(), email, answer)
	if elapsed := time.Since(start); err == nil || elapsed < 500*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("SMTP timeout was not applied: %v", err)
	}
	previous, _ := cache.Get(keys.code)
	if cache.Exists(keys.cooldown) || previous != "previous OTP" {
		t.Fatal("failed delivery left a resend lock or replaced the previous OTP")
	}
}
