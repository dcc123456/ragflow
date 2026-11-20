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
	"strings"

	"ragflow/internal/dao"
	"ragflow/internal/server/config"

	"go.uber.org/zap"
)

// ssoSettingSeparator splits a system setting name into the provider id and the
// provider's own key: "google|sso.client_id" yields ("google", "client_id").
const ssoSettingSeparator = "|sso."

// splitSSOSetting splits "<id>|sso.<key>" into its provider id and key. Names
// outside that shape, or with an empty key, are not SSO settings.
func splitSSOSetting(name string) (providerID, key string, ok bool) {
	idx := strings.Index(name, ssoSettingSeparator)
	if idx <= 0 {
		return "", "", false
	}
	key = name[idx+len(ssoSettingSeparator):]
	if key == "" {
		return "", "", false
	}
	return name[:idx], key, true
}

// ssoProviderSpec maps a provider's flat `<id>|sso.<key>` settings onto the
// channel shape the OAuth client consumes.
type ssoProviderSpec struct {
	// Type selects the client flavor implemented by internal/service/oauth.
	Type string
	// Issuer is the OIDC discovery base; empty for non-OIDC flavors.
	Issuer string
	// ClientID and Secret name the settings holding the credentials.
	ClientID string
	Secret   string
	// RedirectURI names the setting holding the callback URL. Empty means the
	// provider never receives one, not even when the admin console stores a
	// value. When it is named but unset the parameter is omitted from the
	// authorization and token requests, which lets the provider fall back to
	// the callback registered with the application.
	RedirectURI string
	// RedirectURIRequired marks the callback as mandatory: a provider whose
	// login fails without one (Google) has the channel skipped while the
	// setting is missing. A provider that remembers a single callback out of
	// band (Feishu) leaves it false. Only meaningful when RedirectURI is set.
	RedirectURIRequired bool
	// Scope is the space-separated scope list sent with the authorization
	// request. Empty means the provider's default.
	Scope string
	// ScopeSetting names the setting that can widen Scope to the permission
	// list this deployment was granted. Empty means Scope is always used
	// verbatim. Only providers whose useful scopes differ per tenant need it.
	ScopeSetting string
	// AgentID names the setting holding the WeCom self-built application id.
	// Empty for the other flavors. It is mandatory: the wwlogin consent URL
	// cannot be built without it.
	AgentID string
}

// ssoProviderSpecs is keyed by the provider id used in the setting names. A
// provider missing from this table is not exposed as a login channel even when
// the admin console has stored settings for it.
var ssoProviderSpecs = map[string]ssoProviderSpec{
	"google": {
		Type:                "oidc",
		Issuer:              "https://accounts.google.com",
		ClientID:            "client_id",
		Secret:              "client_secret",
		RedirectURI:         "redirect_uri",
		RedirectURIRequired: true,
		Scope:               "openid email profile",
	},
	"github": {
		Type:     "github",
		ClientID: "client_id",
		Secret:   "client_secret",
	},
	// Feishu grants per-tenant capabilities, so a deployment that has been
	// given the contact permission widens the requested list itself: without
	// it only union/open ids come back and there is no email to bind the
	// login to.
	//
	// The callback is optional: when the app registers exactly one redirect
	// URL, Feishu uses it unless the request names one, which is the same
	// out-of-band arrangement GitHub relies on.
	"feishu": {
		Type:         "lark",
		ClientID:     "client_id",
		Secret:       "client_secret",
		RedirectURI:  "redirect_uri",
		ScopeSetting: "scope",
	},
	// WeCom identifies the tenant by corp id (client_id) and the self-built
	// application by agent_id; both ride on the wwlogin consent URL, so
	// neither may be missing. The callback URL is mandatory too: the consent
	// page has no out-of-band callback registration to fall back on, and the
	// embedded QR panel needs the exact URL to hand the code to.
	//
	// Login binding is by the member's biz_mail (the tenant's WeCom
	// mailbox): a corp without the mailbox service enabled produces members
	// the login cannot bind, reported as email_missing.
	"wecom": {
		Type:                "wecom",
		ClientID:            "client_id",
		Secret:              "client_secret",
		AgentID:             "agent_id",
		RedirectURI:         "redirect_uri",
		RedirectURIRequired: true,
	},
	// DingTalk binds the login to the member's unionId, which stays stable
	// across re-installing the app in a different organization. The callback
	// URL is mandatory: the embedded QR widget hands the authCode to it, and
	// DingTalk requires the page rendering the QR to share the redirect's
	// origin, so there is no out-of-band registration to fall back on.
	//
	// Email requires the "通讯录个人信息读" permission (Contact.User.Read) on
	// the DingTalk app; an app without it produces members the login cannot
	// bind, reported as email_missing.
	"dingtalk": {
		Type:                "dingtalk",
		ClientID:            "client_id",
		Secret:              "client_secret",
		RedirectURI:         "redirect_uri",
		RedirectURIRequired: true,
		ScopeSetting:        "scope",
	},
}

// parseSSOBool reads the enabled flag the admin console stores. The console
// writes "true"/"false", but a setting seeded outside it may carry one of the
// other spellings a YAML or env loader would accept.
func parseSSOBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

// ssoRequiredSettings lists the settings that must be non-empty before a
// provider is reported as a channel.
func ssoRequiredSettings(spec ssoProviderSpec) []string {
	required := []string{spec.ClientID, spec.Secret}
	if spec.RedirectURIRequired && spec.RedirectURI != "" {
		required = append(required, spec.RedirectURI)
	}
	if spec.AgentID != "" {
		required = append(required, spec.AgentID)
	}
	return required
}

// loadSSOChannels builds the OAuth channel table from the settings the admin
// console writes through PUT /api/v1/admin/variables. A provider is included
// only when it is enabled and every credential it needs is present, so a
// half-filled form cannot surface a channel whose login fails at the provider.
//
// A read failure yields an empty table: the login page then offers no SSO
// buttons, which is the same state as "nothing configured" and keeps a
// database hiccup from breaking the password login path.
func loadSSOChannels(ctx context.Context) map[string]config.OAuthChannel {
	settings, err := dao.NewSystemSettingsDAO().GetAll(ctx, dao.DB)
	if err != nil {
		zap.L().Warn("failed to load system settings for SSO channels", zap.Error(err))
		return map[string]config.OAuthChannel{}
	}

	values := make(map[string]map[string]string)
	for _, setting := range settings {
		id, key, ok := splitSSOSetting(setting.Name)
		if !ok {
			continue
		}
		if values[id] == nil {
			values[id] = make(map[string]string)
		}
		values[id][key] = setting.Value
	}

	channels := make(map[string]config.OAuthChannel, len(values))
	for id, providerValues := range values {
		spec, ok := ssoProviderSpecs[id]
		if !ok {
			continue
		}
		if !parseSSOBool(providerValues["enabled"]) {
			continue
		}

		missing := make([]string, 0, len(ssoRequiredSettings(spec)))
		for _, key := range ssoRequiredSettings(spec) {
			if strings.TrimSpace(providerValues[key]) == "" {
				missing = append(missing, key)
			}
		}
		if len(missing) > 0 {
			zap.L().Warn("SSO channel skipped: incomplete configuration",
				zap.String("channel", id),
				zap.Strings("missing", missing),
			)
			continue
		}

		scope := spec.Scope
		if override := strings.TrimSpace(providerValues[spec.ScopeSetting]); spec.ScopeSetting != "" && override != "" {
			scope = override
		}

		channels[id] = config.OAuthChannel{
			Type:         spec.Type,
			Issuer:       spec.Issuer,
			Scope:        scope,
			ClientID:     providerValues[spec.ClientID],
			ClientSecret: providerValues[spec.Secret],
			RedirectURI:  providerValues[spec.RedirectURI],
			AgentID:      providerValues[spec.AgentID],
			// Endpoint overrides mirror the YAML keys of the same name. Every
			// flavor has its official endpoints built in, so an unset key changes
			// nothing; a set one lets an operator point the channel at a proxied
			// deployment, and the router-level integration tests at a stub server.
			AuthorizationURL: providerValues["authorization_url"],
			TokenURL:         providerValues["token_url"],
			UserinfoURL:      providerValues["userinfo_url"],
			DirectoryURL:     providerValues["directory_url"],
		}
	}
	return channels
}

// lookupOAuthConfig returns an enabled OAuth channel by name. The lookup is
// case-insensitive so a callback URL spelling the provider differently still
// resolves to the configured channel.
func lookupOAuthConfig(ctx context.Context, channel string) (config.OAuthChannel, bool) {
	channels := loadSSOChannels(ctx)
	if found, ok := channels[channel]; ok {
		return found, true
	}
	want := strings.ToLower(channel)
	for id, found := range channels {
		if strings.ToLower(id) == want {
			return found, true
		}
	}
	return config.OAuthChannel{}, false
}
