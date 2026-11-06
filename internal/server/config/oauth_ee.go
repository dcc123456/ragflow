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

package config

import "github.com/spf13/viper"

type OAuthConfig struct {
	Channels map[string]OAuthChannel `mapstructure:"oauth"`
}

// OAuthChannel OAuth configuration for a channel.
type OAuthChannel struct {
	DisplayName      string `mapstructure:"display_name"`
	Icon             string `mapstructure:"icon"`
	Type             string `mapstructure:"type"`
	ClientID         string `mapstructure:"client_id"`
	ClientSecret     string `mapstructure:"client_secret"`
	AuthorizationURL string `mapstructure:"authorization_url"`
	TokenURL         string `mapstructure:"token_url"`
	UserinfoURL      string `mapstructure:"userinfo_url"`
	RedirectURI      string `mapstructure:"redirect_uri"`
	Scope            string `mapstructure:"scope"`
	Issuer           string `mapstructure:"issuer"`

	// TrustEmailVerified marks the provider as institutionally trusted for the
	// address it asserts, so an OIDC login is accepted even when it omits
	// `email_verified`. Leave false for public providers: without a verified
	// address someone could claim another user's email and take over that
	// account, because SSO logins bind to local accounts by email.
	TrustEmailVerified bool `mapstructure:"trust_email_verified"`

	// Claim-precedence lists; the first non-empty match wins. Empty falls back
	// to the OIDC defaults (username/preferred_username/sub, email,
	// nickname/name, avatar_url/picture), so a provider that exposes a bespoke
	// id claim can be wired up without a code change:
	//
	//   username_claims: ["employee_id", "preferred_username", "sub"]
	UsernameClaims []string `mapstructure:"username_claims"`
	EmailClaims    []string `mapstructure:"email_claims"`
	NicknameClaims []string `mapstructure:"nickname_claims"`
	AvatarClaims   []string `mapstructure:"avatar_claims"`
}

func (c *Config) ParseOAuthConfig(v *viper.Viper) error {
	if !v.IsSet("oauth") {
		return nil
	}
	sub := v.Sub("oauth")
	if sub == nil {
		return nil
	}
	c.oAuth.Channels = make(map[string]OAuthChannel)
	err := sub.Unmarshal(&c.oAuth.Channels)
	if err != nil {
		return err
	}
	return nil
}

func (c *Config) GetOAuthChannels() map[string]OAuthChannel {
	return c.oAuth.Channels
}
