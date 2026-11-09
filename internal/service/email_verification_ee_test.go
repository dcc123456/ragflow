//go:build ee

package service

import (
	"fmt"
	"regexp"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/utility"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestEmailVerificationPurposeIsolation(t *testing.T) {
	cache := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: cache.Addr()})
	t.Cleanup(func() { client.Close() })
	v := emailVerifier{cache: client}
	registration := registrationCacheKeys("user@example.com")
	reset := passwordResetCacheKeys("user@example.com")
	salt := []byte("verification-isolation-salt")
	cache.Set(registration.code, utility.EncodeOTPStorageValue(utility.HashOTPCode("ABCD", salt), salt))
	cache.Set(reset.code, utility.EncodeOTPStorageValue(utility.HashOTPCode("WXYZ", salt), salt))
	proof := registrationProofHash("browser-proof")
	if err := v.verifyOTP(t.Context(), reset, "ABCD", "1"); verificationErrorCode(err) != common.CodeAuthenticationError {
		t.Fatalf("registration code accepted for password reset: %v", err)
	}
	if err := v.verifyOTP(t.Context(), registration, "WXYZ", proof); verificationErrorCode(err) != common.CodeAuthenticationError {
		t.Fatalf("password reset code accepted for registration: %v", err)
	}
	if err := v.verifyOTP(t.Context(), registration, "ABCD", proof); err != nil {
		t.Fatal(err)
	}
	if !cache.Exists(reset.code) || cache.Exists(reset.verified) {
		t.Fatal("registration verification changed password reset state")
	}
	if err := v.verifyOTP(t.Context(), reset, "WXYZ", "1"); err != nil {
		t.Fatal(err)
	}
	registrationProof, _ := cache.Get(registration.verified)
	resetVerified, _ := cache.Get(reset.verified)
	if registrationProof != proof || resetVerified != "1" {
		t.Fatal("verification results were not kept separate")
	}
}

func TestEmailVerificationPreservesSendingReservation(t *testing.T) {
	for _, purpose := range []struct {
		name string
		keys func(string) emailVerificationKeys
	}{
		{"registration", registrationCacheKeys},
		{"password reset", passwordResetCacheKeys},
	} {
		t.Run(purpose.name, func(t *testing.T) {
			fixture, cache := registrationTestVerifier(t)
			v := &fixture.emailVerifier
			const email = "user@example.com"
			keys := purpose.keys(email)
			salt := []byte("previous-code-salt")
			cache.Set(keys.code, utility.EncodeOTPStorageValue(utility.HashOTPCode("ABCD", salt), salt))
			verified := make(chan error, 1)
			mailbox := registrationTestSMTP(t, fixture, func() {
				if err := v.verifyOTP(t.Context(), keys, "ABCD", "proof"); err != nil {
					verified <- err
					return
				}
				if !cache.Exists(keys.cooldown) {
					verified <- fmt.Errorf("previous OTP verification released the sending reservation")
					return
				}
				cache.Set(keys.captcha, "NEXT")
				err := v.sendOTP(t.Context(), keys, email, "NEXT", "Verification", "Your code")
				if verificationErrorCode(err) != common.CodeNotEffective {
					verified <- fmt.Errorf("another send entered before delivery completed: %v", err)
					return
				}
				verified <- nil
			})
			cache.Set(keys.captcha, "TEST")
			if err := v.sendOTP(t.Context(), keys, email, "TEST", "Verification", "Your code"); err != nil {
				t.Fatal(err)
			}
			if err := <-verified; err != nil {
				t.Fatal(err)
			}
			if cache.TTL(keys.cooldown) != utility.OTPResendCooldown {
				t.Fatal("successful delivery lost its resend cooldown")
			}
			code := regexp.MustCompile(`Your code is: ([A-Z]{4})`).FindStringSubmatch(<-mailbox)
			if len(code) != 2 {
				t.Fatal("delivered email had no OTP")
			}
			if err := v.verifyOTP(t.Context(), keys, code[1], "proof"); err != nil {
				t.Fatalf("delivered OTP was unusable: %v", err)
			}
			if cache.Exists(keys.cooldown) {
				t.Fatal("verification did not clear the completed send's cooldown")
			}
		})
	}
}

func TestEmailVerificationRejectsLostSendingReservation(t *testing.T) {
	fixture, cache := registrationTestVerifier(t)
	v := &fixture.emailVerifier
	const email = "user@example.com"
	keys := registrationCacheKeys(email)
	registrationTestSMTP(t, fixture, func() {
		cache.Set(keys.cooldown, "newer-reservation")
		cache.Set(keys.code, "newer-code")
		cache.Set(keys.attempts, "2")
	})
	cache.Set(keys.captcha, "TEST")
	err := v.sendOTP(t.Context(), keys, email, "TEST", "Verification", "Your code")
	if verificationErrorCode(err) != common.CodeNotEffective {
		t.Fatalf("send with a lost reservation succeeded: %v", err)
	}
	for key, want := range map[string]string{keys.cooldown: "newer-reservation", keys.code: "newer-code", keys.attempts: "2"} {
		if got, _ := cache.Get(key); got != want {
			t.Fatalf("older send changed newer state: got %q, want %q", got, want)
		}
	}
}
