//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

//go:build ee

package service

import (
	"context"
	"testing"

	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"

	"gorm.io/gorm"
)

func ssoSetting(name, value string) entity.SystemSettings {
	return entity.SystemSettings{Name: name, Value: value, Source: "admin", DataType: "string"}
}

func setupSSOSettings(t *testing.T, settings ...entity.SystemSettings) *gorm.DB {
	t.Helper()

	db := testutil.SetupTestDB(t, &entity.SystemSettings{})
	t.Cleanup(testutil.ReplaceDBForTest(t, db))

	for i := range settings {
		if err := db.Create(&settings[i]).Error; err != nil {
			t.Fatalf("seed %s: %v", settings[i].Name, err)
		}
	}
	return db
}

// TestLoadSSOChannelsBuildsFromSystemSettings is the core of the YAML-to-database
// move: the channel table the login page and the authorization redirect consume
// is derived from the `<id>|sso.<key>` rows the admin console writes.
func TestLoadSSOChannelsBuildsFromSystemSettings(t *testing.T) {
	setupSSOSettings(t,
		ssoSetting("google|sso.enabled", "true"),
		ssoSetting("google|sso.client_id", "google-client"),
		ssoSetting("google|sso.client_secret", "google-secret"),
		ssoSetting("google|sso.redirect_uri", "https://ragflow.example/api/v1/auth/oauth/google/callback"),
		// The admin console stores a JSON boolean, which lands here as "true";
		// a hand-seeded row may spell it the way a YAML loader would.
		ssoSetting("github|sso.enabled", "True"),
		ssoSetting("github|sso.client_id", "github-client"),
		ssoSetting("github|sso.client_secret", "github-secret"),
		// Feishu settings exist but the provider has no client implementation.
		ssoSetting("feishu|sso.enabled", "true"),
		ssoSetting("feishu|sso.app_id", "cli_app"),
		ssoSetting("feishu|sso.app_secret", "feishu-secret"),
		ssoSetting("feishu|sso.app_access_token_url", "https://open.feishu.cn/token"),
		ssoSetting("feishu|sso.user_access_token_url", "https://open.feishu.cn/user_token"),
		// Unrelated settings, including one that happens to use the separator.
		ssoSetting("mail|sso.enabled", "true"),
		ssoSetting("mail.server", "smtp.example.com"),
	)

	channels := loadSSOChannels(context.Background())
	if len(channels) != 2 {
		t.Fatalf("got %d channels (%v), want google and github only", len(channels), channels)
	}

	google, ok := channels["google"]
	if !ok {
		t.Fatal("google channel missing")
	}
	wantGoogle := struct {
		Type, Issuer, Scope, ClientID, ClientSecret, RedirectURI string
	}{
		Type:         "oidc",
		Issuer:       "https://accounts.google.com",
		Scope:        "openid email profile",
		ClientID:     "google-client",
		ClientSecret: "google-secret",
		RedirectURI:  "https://ragflow.example/api/v1/auth/oauth/google/callback",
	}
	if google.Type != wantGoogle.Type || google.Issuer != wantGoogle.Issuer || google.Scope != wantGoogle.Scope ||
		google.ClientID != wantGoogle.ClientID || google.ClientSecret != wantGoogle.ClientSecret ||
		google.RedirectURI != wantGoogle.RedirectURI {
		t.Fatalf("google channel = %+v, want %+v", google, wantGoogle)
	}

	github, ok := channels["github"]
	if !ok {
		t.Fatal("github channel missing")
	}
	if github.Type != "github" || github.ClientID != "github-client" || github.ClientSecret != "github-secret" {
		t.Fatalf("github channel = %+v", github)
	}
	// GitHub registers the callback with the app, so no redirect_uri is sent.
	if github.RedirectURI != "" {
		t.Fatalf("github redirect_uri = %q, want empty", github.RedirectURI)
	}

	if _, ok := channels["feishu"]; ok {
		t.Fatal("feishu has no client implementation and must not be reported")
	}
	if _, ok := channels["mail"]; ok {
		t.Fatal("a setting using the separator without a provider spec must be ignored")
	}
}

// TestLoadSSOChannelsSkipsDisabledAndIncomplete keeps a half-filled admin form
// from advertising a channel whose login would fail at the provider.
func TestLoadSSOChannelsSkipsDisabledAndIncomplete(t *testing.T) {
	tests := []struct {
		name     string
		settings []entity.SystemSettings
		channel  string
	}{
		{
			name: "disabled",
			settings: []entity.SystemSettings{
				ssoSetting("google|sso.enabled", "false"),
				ssoSetting("google|sso.client_id", "id"),
				ssoSetting("google|sso.client_secret", "secret"),
				ssoSetting("google|sso.redirect_uri", "https://x/cb"),
			},
			channel: "google",
		},
		{
			name: "missing client secret",
			settings: []entity.SystemSettings{
				ssoSetting("github|sso.enabled", "true"),
				ssoSetting("github|sso.client_id", "id"),
			},
			channel: "github",
		},
		{
			name: "missing redirect uri",
			settings: []entity.SystemSettings{
				ssoSetting("google|sso.enabled", "true"),
				ssoSetting("google|sso.client_id", "id"),
				ssoSetting("google|sso.client_secret", "secret"),
			},
			channel: "google",
		},
		{
			name: "blank credentials are not present",
			settings: []entity.SystemSettings{
				ssoSetting("github|sso.enabled", "true"),
				ssoSetting("github|sso.client_id", "   "),
				ssoSetting("github|sso.secret_key", "secret"),
			},
			channel: "github",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupSSOSettings(t, tt.settings...)

			if _, ok := loadSSOChannels(context.Background())[tt.channel]; ok {
				t.Fatalf("%s must not be reported as a channel", tt.channel)
			}
		})
	}
}

// TestLoadSSOChannelsPrefersClientSecret pins the GitHub secret key name. The
// admin form writes "client_secret", while "secret_key" survives only as a
// seeded row and a stale type declaration, so a database holding both must read
// the one the form populates.
func TestLoadSSOChannelsPrefersClientSecret(t *testing.T) {
	setupSSOSettings(t,
		ssoSetting("github|sso.enabled", "true"),
		ssoSetting("github|sso.client_id", "github-client"),
		ssoSetting("github|sso.client_secret", "the-live-secret"),
		ssoSetting("github|sso.secret_key", ""),
	)

	cfg, ok := loadSSOChannels(context.Background())["github"]
	if !ok {
		t.Fatal("github channel missing")
	}
	if cfg.ClientSecret != "the-live-secret" {
		t.Fatalf("client_secret = %q, want %q", cfg.ClientSecret, "the-live-secret")
	}
}

func TestLoadSSOChannelsWithoutSettings(t *testing.T) {
	setupSSOSettings(t)

	if channels := loadSSOChannels(context.Background()); len(channels) != 0 {
		t.Fatalf("got %d channels, want none", len(channels))
	}
}

// TestLookupOAuthConfigCaseInsensitive covers a callback URL that spells the
// provider differently from the setting prefix.
func TestLookupOAuthConfigCaseInsensitive(t *testing.T) {
	setupSSOSettings(t,
		ssoSetting("github|sso.enabled", "true"),
		ssoSetting("github|sso.client_id", "github-client"),
		ssoSetting("github|sso.client_secret", "github-secret"),
	)

	cfg, ok := lookupOAuthConfig(context.Background(), "GitHub")
	if !ok {
		t.Fatal("lookup of \"GitHub\" failed, want a case-insensitive match")
	}
	if cfg.ClientID != "github-client" {
		t.Fatalf("client_id = %q, want %q", cfg.ClientID, "github-client")
	}

	if _, ok := lookupOAuthConfig(context.Background(), "gitlab"); ok {
		t.Fatal("unconfigured provider must not resolve")
	}
}
