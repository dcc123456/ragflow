//go:build ee

package service

import (
	"testing"

	"ragflow/internal/entity"
)

func TestCASSettings(t *testing.T) {
	for _, tc := range []struct {
		name, key, value string
		available        bool
	}{
		{name: "complete", available: true},
		{name: "disabled", key: "enabled", value: "false"},
		{name: "missing login", key: "login_url"},
		{name: "missing validation", key: "validation_url"},
		{name: "unsupported version", key: "protocol_version", value: "4"},
		{name: "insecure provider", key: "login_url", value: "http://sso.example/login"},
		{name: "wrong callback", key: "redirect_uri", value: "https://ragflow.example/api/v1/auth/oauth/cas/callback"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{
				"enabled": "true", "name": "Company login", "login_url": "https://sso.example/login",
				"validation_url": "https://validation.example/serviceValidate", "protocol_version": "2",
				"redirect_uri": "https://ragflow.example/api/v1/auth/cas/callback",
			}
			if tc.key != "" {
				values[tc.key] = tc.value
			}
			settings := make([]entity.SystemSettings, 0, len(values))
			for key, value := range values {
				settings = append(settings, ssoSetting("cas|sso."+key, value))
			}
			setupSSOSettings(t, settings...)
			provider, ok := loadCASProviders(t.Context())["cas"]
			if ok != tc.available {
				t.Fatalf("available = %t, want %t", ok, tc.available)
			}
			channels, _, err := NewUserService().GetLoginChannels()
			if err != nil {
				t.Fatal(err)
			}
			if tc.available {
				if len(channels) != 1 || channels[0].Channel != "cas" || channels[0].DisplayName != provider.DisplayName || channels[0].Icon != "cas" {
					t.Fatalf("incorrect CAS channel: %+v", channels)
				}
			} else if len(channels) != 0 {
				t.Fatalf("unusable CAS was advertised: %+v", channels)
			}
		})
	}
}
