//go:build ee

package service

import (
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"
)

func TestPublicConfigExtras(t *testing.T) {
	v, _ := registrationTestVerifier(t)
	t.Cleanup(testutil.ReplaceDBForTest(t, v.db))
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{value: "true", want: true},
		{value: "false", want: false},
		{value: "invalid", want: false},
	} {
		t.Run(tc.value, func(t *testing.T) {
			setRegistrationSetting(t, v, "email_verification.enabled", tc.value)
			extras, err := loadConfigExtras(t.Context())
			if err != nil || extras.EmailVerificationEnabled != tc.want {
				t.Fatalf("public config for %q: extras=%+v error=%v", tc.value, extras, err)
			}
		})
	}
	if code, err := v.consumeVerification(t.Context(), "invalid-setting@example.com"); err == nil || code != common.CodeServerError {
		t.Fatalf("invalid configuration allowed registration: code=%d error=%v", code, err)
	}
	if err := v.db.Migrator().DropTable(&entity.SystemSettings{}); err != nil {
		t.Fatal(err)
	}
	extras, err := loadConfigExtras(t.Context())
	if err != nil || extras.EmailVerificationEnabled {
		t.Fatalf("database failure did not return public config defaults: extras=%+v error=%v", extras, err)
	}
	if code, err := v.consumeVerification(t.Context(), "unavailable-setting@example.com"); err == nil || code != common.CodeServerError {
		t.Fatalf("database failure allowed registration: code=%d error=%v", code, err)
	}
}
