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
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/ldap"

	"go.uber.org/zap"
)

const (
	// ldapChannelPrefix prefixes every LDAP setting name and source. The
	// console mounts a server as `ldap|<id>`, so `ldap|default` and the
	// `ldap|server-<uuid>` entries a custom server gets all live below it.
	ldapChannelPrefix = "ldap"
	// ldapVarSeparator separates `<source>` from `<key>`: `ldap|default.url`.
	ldapVarSeparator = "."
	// ldapDefaultID is the id the seeded server carries.
	ldapDefaultID = "default"
)

// LDAP settings keys as the console writes them.
const (
	ldapKeyEnabled      = "enabled"
	ldapKeyName         = "name"
	ldapKeyURL          = "url"
	ldapKeyBindDN       = "dn"
	ldapKeyBindPassword = "password"
	ldapKeySearchFilter = "search_filter"
	ldapKeyAttributes   = "attribute_list"
)

// LDAPServer is one configured directory server, assembled from every setting
// whose source is that server. Channel is the source itself (`ldap|default`),
// which is also the value the login page posts back to /auth/login/:channel.
type LDAPServer struct {
	Channel      string
	DisplayName  string
	Enabled      bool
	URL          string
	BindDN       string
	BindPassword string
	SearchFilter string
	Attributes   []string
}

// IsLDAPChannel reports whether a login channel belongs to the directory
// login rather than to an OAuth provider. Every LDAP source starts with
// `ldap` (the seeded `ldap|default` and any custom `ldap|<id>`), so the prefix
// is the whole test.
func IsLDAPChannel(channel string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(channel)), ldapChannelPrefix)
}

// loadLDAPServers returns the usable directory servers, sorted by channel.
//
// A server is usable only when it is enabled and carries the three settings a
// bind needs; an entry the console has half-filled is skipped rather than
// surfacing on the login page as a server that always fails.
func loadLDAPServers(ctx context.Context) []LDAPServer {
	settings, err := dao.NewSystemSettingsDAO().GetByNamePrefix(ctx, dao.DB, ldapChannelPrefix)
	if err != nil {
		common.Warn("load ldap settings failed", zap.Error(err))
		return nil
	}

	servers := make([]LDAPServer, 0, len(settings))
	for _, server := range groupLDAPSettings(settings) {
		if server.usable() {
			servers = append(servers, server)
		}
	}
	sort.Slice(servers, func(i, j int) bool { return servers[i].Channel < servers[j].Channel })
	return servers
}

// lookupLDAPServer resolves the channel a login request names.
func lookupLDAPServer(ctx context.Context, channel string) (LDAPServer, bool) {
	wanted := strings.TrimSpace(channel)
	for _, server := range loadLDAPServers(ctx) {
		if strings.EqualFold(server.Channel, wanted) {
			return server, true
		}
	}
	return LDAPServer{}, false
}

// groupLDAPSettings folds the flat setting rows into one server per source,
// which is what makes `ldap|default` and a custom `ldap|server-<uuid>` behave
// alike: the grouping key is the source, never the literal `default` id.
func groupLDAPSettings(settings []entity.SystemSettings) []LDAPServer {
	values := make(map[string]map[string]string, len(settings))
	order := make([]string, 0, len(settings))
	for _, setting := range settings {
		source, key, ok := ldapSettingGroup(setting)
		if !ok {
			continue
		}
		if _, seen := values[source]; !seen {
			values[source] = make(map[string]string, 8)
			order = append(order, source)
		}
		values[source][key] = setting.Value
	}

	servers := make([]LDAPServer, 0, len(order))
	for _, source := range order {
		servers = append(servers, ldapServerFrom(source, values[source]))
	}
	return servers
}

// ldapSettingGroup splits a setting name into the source that groups it and
// the key within that source.
//
// The source comes from the name's `<source>.<key>` prefix because the console
// stamps `source` with "admin" on the rows it creates, so the column alone
// cannot group a custom server. When the column does carry an LDAP source it is
// preferred, so a row whose name and source disagree still joins the server its
// source names.
func ldapSettingGroup(setting entity.SystemSettings) (source, key string, ok bool) {
	trimmed := strings.TrimSpace(setting.Name)
	separator := strings.LastIndex(trimmed, ldapVarSeparator)
	if separator <= 0 || separator == len(trimmed)-1 {
		return "", "", false
	}
	derived, key := trimmed[:separator], strings.ToLower(strings.TrimSpace(trimmed[separator+1:]))
	if key == "" || !strings.HasPrefix(strings.ToLower(derived), ldapChannelPrefix) {
		return "", "", false
	}
	if declared := strings.TrimSpace(setting.Source); strings.HasPrefix(strings.ToLower(declared), ldapChannelPrefix) {
		derived = declared
	}
	return derived, key, true
}

func ldapServerFrom(source string, values map[string]string) LDAPServer {
	server := LDAPServer{
		Channel:      source,
		Enabled:      parseLDAPBool(values[ldapKeyEnabled]),
		URL:          strings.TrimSpace(values[ldapKeyURL]),
		BindDN:       strings.TrimSpace(values[ldapKeyBindDN]),
		BindPassword: strings.TrimSpace(values[ldapKeyBindPassword]),
		SearchFilter: strings.TrimSpace(values[ldapKeySearchFilter]),
		Attributes:   splitLDAPAttributes(values[ldapKeyAttributes]),
	}
	server.DisplayName = strings.TrimSpace(values[ldapKeyName])
	if server.DisplayName == "" {
		server.DisplayName = ldapDisplayName(source)
	}
	return server
}

func (s LDAPServer) usable() bool {
	return s.Enabled && s.URL != "" && s.BindDN != "" && s.SearchFilter != ""
}

func (s LDAPServer) ldapConfig() ldap.Config {
	return ldap.Config{
		URL:          s.URL,
		BindDN:       s.BindDN,
		BindPassword: s.BindPassword,
		SearchFilter: s.SearchFilter,
		Attributes:   s.Attributes,
	}
}

// ldapDisplayName names a server the administrator did not name: the id part of
// its source, with the seeded `default` server spelled out.
func ldapDisplayName(source string) string {
	_, id, found := strings.Cut(source, "|")
	id = strings.TrimSpace(id)
	if !found || id == "" {
		return source
	}
	if strings.EqualFold(id, ldapDefaultID) {
		return "Default"
	}
	return id
}

func splitLDAPAttributes(value string) []string {
	parts := strings.Split(value, ",")
	attributes := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			attributes = append(attributes, trimmed)
		}
	}
	return attributes
}

func parseLDAPBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
