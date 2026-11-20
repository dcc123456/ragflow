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
	"sort"
	"strings"

	"ragflow/internal/common"
)

type LoginChannel struct {
	Channel     string `json:"channel"`
	DisplayName string `json:"display_name"`
	Icon        string `json:"icon"`
	// ClientID is the provider-issued application id, which is public
	// information. The login page needs it to embed a provider's QR login
	// widget — the Feishu SDK renders its code from it — without first
	// navigating to the redirect route. LDAP channels leave it empty.
	ClientID string `json:"client_id,omitempty"`
}

// GetLoginChannels gets all enabled authentication channels. The result is
// sorted by channel name so the login page renders a stable order across
// requests (Go map iteration order is randomized).
//
// The caller is the render-facing handler, which has no request-scoped purpose
// of its own to thread through, so the settings read uses a background context.
func (s *UserService) GetLoginChannels() ([]*LoginChannel, common.ErrorCode, error) {
	ctx := context.Background()
	configured := loadSSOChannels(ctx)
	channels := make([]*LoginChannel, 0, len(configured))
	for channel, cfg := range configured {
		channels = append(channels, &LoginChannel{
			Channel:     channel,
			DisplayName: strings.Title(channel),
			Icon:        "sso",
			ClientID:    cfg.ClientID,
		})
	}
	// Directory servers log in with a form of their own rather than by
	// redirect, but they are advertised through the same list so the login page
	// knows which servers to offer and what to call them.
	for _, server := range loadLDAPServers(ctx) {
		channels = append(channels, &LoginChannel{
			Channel:     server.Channel,
			DisplayName: server.DisplayName,
			Icon:        "ldap",
		})
	}
	for channel, provider := range loadCASProviders(ctx) {
		channels = append(channels, &LoginChannel{Channel: channel, DisplayName: provider.DisplayName, Icon: "cas"})
	}

	sort.Slice(channels, func(i, j int) bool {
		return channels[i].Channel < channels[j].Channel
	})

	return channels, common.CodeSuccess, nil
}
