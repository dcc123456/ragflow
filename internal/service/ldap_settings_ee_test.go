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
	"testing"

	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"

	"gorm.io/gorm"
)

func ldapSetting(name, value, source string) entity.SystemSettings {
	return entity.SystemSettings{Name: name, Value: value, Source: source, DataType: "string"}
}

func setupLDAPSettings(t *testing.T, settings ...entity.SystemSettings) *gorm.DB {
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

// ldapDefaultServer seeds the server the console ships with, whose rows carry
// the source the naming system gives them.
func ldapDefaultServer() []entity.SystemSettings {
	return []entity.SystemSettings{
		ldapSetting("ldap|default.enabled", "true", "ldap|default"),
		ldapSetting("ldap|default.url", "ldap.example.com", "ldap|default"),
		ldapSetting("ldap|default.dn", "cn=admin,dc=example,dc=com", "ldap|default"),
		ldapSetting("ldap|default.password", "s3cret", "ldap|default"),
		ldapSetting("ldap|default.search_filter", "(|(userPrincipalName={username})(mail={username}))", "ldap|default"),
		ldapSetting("ldap|default.attribute_list", "cn, mail ,uid", "ldap|default"),
	}
}

// TestLoadLDAPServersGroupsBySource is the core of the naming system: one server
// is every row that shares a source, so the seeded `ldap|default` and a custom
// `ldap|server-<uuid>` are loaded the same way, without any code naming the
// `default` id.
func TestLoadLDAPServersGroupsBySource(t *testing.T) {
	setupLDAPSettings(t, append(ldapDefaultServer(),
		// A server added through the console: SetVariable stamps its rows with
		// source "admin", so only the name can group them.
		ldapSetting("ldap|server-abc123.enabled", "true", "admin"),
		ldapSetting("ldap|server-abc123.name", "Corp Directory", "admin"),
		ldapSetting("ldap|server-abc123.url", "ldaps://dir.corp.example", "admin"),
		ldapSetting("ldap|server-abc123.dn", "cn=reader,dc=corp,dc=example", "admin"),
		ldapSetting("ldap|server-abc123.password", "reader-pass", "admin"),
		ldapSetting("ldap|server-abc123.search_filter", "(uid={username})", "admin"),
	)...)

	servers := loadLDAPServers(context.Background())
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %d: %+v", len(servers), servers)
	}

	defaultServer := servers[0]
	if defaultServer.Channel != "ldap|default" {
		t.Fatalf("expected the seeded server first, got %q", defaultServer.Channel)
	}
	if defaultServer.DisplayName != "Default" {
		t.Errorf("expected the seeded server to fall back to a readable name, got %q", defaultServer.DisplayName)
	}
	if defaultServer.BindDN != "cn=admin,dc=example,dc=com" {
		t.Errorf("expected the bind dn from its own source, got %q", defaultServer.BindDN)
	}
	if got := defaultServer.Attributes; len(got) != 3 || got[0] != "cn" || got[1] != "mail" || got[2] != "uid" {
		t.Errorf("expected the attribute list split and trimmed, got %v", got)
	}

	custom := servers[1]
	if custom.Channel != "ldap|server-abc123" {
		t.Fatalf("expected the custom server to keep its source as channel, got %q", custom.Channel)
	}
	if custom.DisplayName != "Corp Directory" {
		t.Errorf("expected the configured name, got %q", custom.DisplayName)
	}
	if custom.URL != "ldaps://dir.corp.example" {
		t.Errorf("expected the custom server url, got %q", custom.URL)
	}
}

// TestLoadLDAPServersSkipsUnusableServers keeps an entry the console has only
// half-filled, or one an administrator disabled, off the login page instead of
// offering a server that can never authenticate anyone.
func TestLoadLDAPServersSkipsUnusableServers(t *testing.T) {
	setupLDAPSettings(t, append(ldapDefaultServer(),
		ldapSetting("ldap|server-off.enabled", "false", "ldap|server-off"),
		ldapSetting("ldap|server-off.url", "ldap.off.example", "ldap|server-off"),
		ldapSetting("ldap|server-off.dn", "cn=admin,dc=off", "ldap|server-off"),
		ldapSetting("ldap|server-off.search_filter", "(uid={username})", "ldap|server-off"),
		ldapSetting("ldap|server-broken.enabled", "true", "ldap|server-broken"),
		ldapSetting("ldap|server-broken.url", "ldap.broken.example", "ldap|server-broken"),
		ldapSetting("ldap|server-broken.dn", "cn=admin,dc=broken", "ldap|server-broken"),
		// No search_filter: nothing to look the login name up with.
		ldapSetting("google|sso.enabled", "true", "google|sso"),
	)...)

	servers := loadLDAPServers(context.Background())
	if len(servers) != 1 || servers[0].Channel != "ldap|default" {
		t.Fatalf("expected only the usable server, got %+v", servers)
	}
}

func TestLDAPSettingGroup(t *testing.T) {
	tests := []struct {
		name       string
		setting    entity.SystemSettings
		wantSource string
		wantKey    string
		wantOK     bool
	}{
		{
			name:       "seeded row",
			setting:    ldapSetting("ldap|default.url", "ldap.example.com", "ldap|default"),
			wantSource: "ldap|default",
			wantKey:    "url",
			wantOK:     true,
		},
		{
			name:       "console row with admin source falls back to the name",
			setting:    ldapSetting("ldap|server-1.url", "ldap.example.com", "admin"),
			wantSource: "ldap|server-1",
			wantKey:    "url",
			wantOK:     true,
		},
		{
			name:       "source wins when the row names one",
			setting:    ldapSetting("ldap|server-1.url", "ldap.example.com", "ldap|server-2"),
			wantSource: "ldap|server-2",
			wantKey:    "url",
			wantOK:     true,
		},
		{
			name:       "key case is normalised",
			setting:    ldapSetting("ldap|default.Search_Filter", "(uid={username})", "ldap|default"),
			wantSource: "ldap|default",
			wantKey:    "search_filter",
			wantOK:     true,
		},
		{name: "oauth row", setting: ldapSetting("google|sso.url", "x", "google|sso")},
		{name: "no key", setting: ldapSetting("ldap|default", "x", "ldap|default")},
		{name: "no separator", setting: ldapSetting("ldapurl", "x", "ldap")},
		{name: "empty key", setting: ldapSetting("ldap|default.", "x", "ldap|default")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source, key, ok := ldapSettingGroup(test.setting)
			if ok != test.wantOK {
				t.Fatalf("expected ok=%v, got %v", test.wantOK, ok)
			}
			if source != test.wantSource || key != test.wantKey {
				t.Fatalf("expected (%q, %q), got (%q, %q)", test.wantSource, test.wantKey, source, key)
			}
		})
	}
}

func TestIsLDAPChannel(t *testing.T) {
	for channel, want := range map[string]bool{
		"ldap|default":   true,
		"ldap":           true,
		"ldap|custom-1":  true,
		" LDAP|default ": true,
		"ldapx":          true,
		"google":         false,
		"github":         false,
		"":               false,
	} {
		if got := IsLDAPChannel(channel); got != want {
			t.Errorf("IsLDAPChannel(%q) = %v, want %v", channel, got, want)
		}
	}
}

func TestLDAPDisplayName(t *testing.T) {
	for source, want := range map[string]string{
		"ldap|default":    "Default",
		"ldap|server-abc": "server-abc",
		"ldap":            "ldap",
	} {
		if got := ldapDisplayName(source); got != want {
			t.Errorf("ldapDisplayName(%q) = %q, want %q", source, got, want)
		}
	}
}

func TestParseLDAPBool(t *testing.T) {
	for value, want := range map[string]bool{
		"true": true, "TRUE": true, "1": true, "on": true, "yes": true,
		"false": false, "0": false, "": false, "disabled": false,
	} {
		if got := parseLDAPBool(value); got != want {
			t.Errorf("parseLDAPBool(%q) = %v, want %v", value, got, want)
		}
	}
}
