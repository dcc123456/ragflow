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
	"errors"
	"strings"
	"testing"

	"ragflow/internal/common"
)

// TestOAuthLoginInitiateExposesQRParameters covers the embedded QR login flow:
// the SPA renders the provider's code on the login card from the public
// parameters, so the initiate result must carry client_id and redirect_uri
// alongside the state and authorization URL — without leaking the secret.
func TestOAuthLoginInitiateExposesQRParameters(t *testing.T) {
	setupSSOSettings(t,
		ssoSetting("feishu|sso.enabled", "true"),
		ssoSetting("feishu|sso.client_id", "cli_app"),
		ssoSetting("feishu|sso.client_secret", "feishu-secret"),
		ssoSetting("feishu|sso.redirect_uri", "https://ragflow.example/api/v1/auth/oauth/feishu/callback"),
	)

	init, code, err := NewUserService().OAuthLoginInitiate(context.Background(), "feishu", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != common.CodeSuccess {
		t.Fatalf("code = %v, want %v", code, common.CodeSuccess)
	}
	if init.State == "" {
		t.Fatal("state is empty")
	}
	if init.ClientID != "cli_app" {
		t.Fatalf("client_id = %q, want %q", init.ClientID, "cli_app")
	}
	if init.RedirectURI != "https://ragflow.example/api/v1/auth/oauth/feishu/callback" {
		t.Fatalf("redirect_uri = %q", init.RedirectURI)
	}
	if init.Channel != "feishu" {
		t.Fatalf("channel = %q, want %q", init.Channel, "feishu")
	}
	if !strings.Contains(init.AuthURL, "client_id=cli_app") {
		t.Fatalf("auth url %q does not carry the client id", init.AuthURL)
	}
	if !strings.Contains(init.AuthURL, "state="+init.State) {
		t.Fatalf("auth url %q does not carry the state", init.AuthURL)
	}
	if !strings.Contains(init.AuthURL, "redirect_uri=") {
		t.Fatalf("auth url %q does not carry the redirect uri", init.AuthURL)
	}
	if strings.Contains(init.AuthURL, "feishu-secret") {
		t.Fatalf("auth url %q leaked the client secret", init.AuthURL)
	}
}

// TestOAuthLoginInitiateInvalidChannel keeps the JSON endpoint from disclosing
// anything about an unknown provider: it answers with the sentinel error and a
// data-error code, the same contract the redirect route maps to ?error=.
func TestOAuthLoginInitiateInvalidChannel(t *testing.T) {
	setupSSOSettings(t)

	_, code, err := NewUserService().OAuthLoginInitiate(context.Background(), "feishu", nil)
	if !errors.Is(err, ErrOAuthInvalidChannel) {
		t.Fatalf("expected ErrOAuthInvalidChannel, got %v", err)
	}
	if code != common.CodeDataError {
		t.Fatalf("code = %v, want %v", code, common.CodeDataError)
	}
}

// TestGetLoginChannelsReportsClientID covers the channels endpoint feeding the
// login card: SSO channels expose their public client id so the page can embed
// the QR widget without a second request, and the field stays empty for
// channels that do not carry one.
func TestGetLoginChannelsReportsClientID(t *testing.T) {
	setupSSOSettings(t,
		ssoSetting("feishu|sso.enabled", "true"),
		ssoSetting("feishu|sso.client_id", "cli_app"),
		ssoSetting("feishu|sso.client_secret", "feishu-secret"),
	)

	channels, _, err := NewUserService().GetLoginChannels()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var feishu *LoginChannel
	for _, channel := range channels {
		if channel.Channel == "feishu" {
			feishu = channel
			break
		}
	}
	if feishu == nil {
		t.Fatalf("feishu channel missing from %v", channels)
	}
	if feishu.ClientID != "cli_app" {
		t.Fatalf("feishu client_id = %q, want %q", feishu.ClientID, "cli_app")
	}
	if strings.Contains(feishu.DisplayName, "feishu-secret") {
		t.Fatalf("display name %q leaked the client secret", feishu.DisplayName)
	}
}
