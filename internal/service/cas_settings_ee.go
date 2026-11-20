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
	"net/url"
	"regexp"
	"strings"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/service/cas"

	"go.uber.org/zap"
)

const (
	casChannelPrefix = "cas"
	casCallbackPath  = "/api/v1/auth/cas/callback"
)

var casChannelPattern = regexp.MustCompile(`^cas(-[0-9a-f]{32})?$`)

// IsCASChannel reports whether a channel is reserved for a CAS provider.
func IsCASChannel(channel string) bool {
	return casChannelPattern.MatchString(channel)
}

type casProvider struct {
	DisplayName string
	Config      cas.Config
}

// loadCASProviders groups the SSO variables by CAS channel and returns usable providers.
func loadCASProviders(ctx context.Context) map[string]casProvider {
	settings, err := dao.NewSystemSettingsDAO().GetByNamePrefix(ctx, dao.DB, casChannelPrefix)
	if err != nil {
		common.Warn("load CAS settings failed", zap.Error(err))
		return nil
	}
	grouped := make(map[string]map[string]string)
	for _, setting := range settings {
		channel, key, ok := splitSSOSetting(setting.Name)
		if !ok || !IsCASChannel(channel) {
			continue
		}
		if grouped[channel] == nil {
			grouped[channel] = make(map[string]string)
		}
		grouped[channel][key] = strings.TrimSpace(setting.Value)
	}
	providers := make(map[string]casProvider, len(grouped))
	for channel, values := range grouped {
		if provider, ok := parseCASProvider(channel, values); ok {
			providers[channel] = provider
		}
	}
	return providers
}

func parseCASProvider(channel string, values map[string]string) (casProvider, bool) {
	if !parseSSOBool(values["enabled"]) {
		return casProvider{}, false
	}
	provider := casProvider{
		DisplayName: values["name"],
		Config: cas.Config{
			LoginURL: values["login_url"], ValidationURL: values["validation_url"],
			ServiceURL: values["redirect_uri"], ProtocolVersion: values["protocol_version"],
			EmailSource: cas.AttributeSource(values["email_source"]), EmailAttribute: values["email_attribute"],
			NicknameSource: cas.AttributeSource(values["nickname_source"]), NicknameAttribute: values["nickname_attribute"],
		},
	}
	if provider.DisplayName == "" {
		provider.DisplayName = "CAS"
	}
	if _, err := cas.NewClient(provider.Config); err != nil {
		common.Warn("CAS channel skipped: invalid configuration", zap.String("channel", channel), zap.Error(err))
		return casProvider{}, false
	}
	callback, _ := url.Parse(provider.Config.ServiceURL)
	if callback.EscapedPath() != casCallbackPath {
		common.Warn("CAS channel skipped: incorrect callback path", zap.String("channel", channel))
		return casProvider{}, false
	}
	return provider, true
}
