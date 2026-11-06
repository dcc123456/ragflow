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

package ldap

import (
	"errors"
	"strings"
	"testing"

	ldapv3 "github.com/go-ldap/ldap/v3"
)

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "bare host gets the default scheme and port", raw: "ldap.example.com", want: "ldap://ldap.example.com:389"},
		{name: "explicit endpoint is kept", raw: "ldap://ldap.example.com:1389", want: "ldap://ldap.example.com:1389"},
		{name: "ldaps gets the tls port", raw: "ldaps://ldap.example.com", want: "ldaps://ldap.example.com:636"},
		{name: "ipv6 host", raw: "ldaps://[::1]", want: "ldaps://[::1]:636"},
		{name: "trims surrounding space", raw: "  ldap.example.com  ", want: "ldap://ldap.example.com:389"},
		{name: "empty", raw: "   ", wantErr: true},
		{name: "unsupported scheme", raw: "http://ldap.example.com", wantErr: true},
		{name: "no host", raw: "ldap://", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeURL(test.raw)
			if test.wantErr {
				if err == nil || !errors.Is(err, ErrConfigIncomplete) {
					t.Fatalf("expected an incomplete-config error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != test.want {
				t.Fatalf("expected %q, got %q", test.want, got)
			}
		})
	}
}

func TestBaseDN(t *testing.T) {
	tests := map[string]string{
		"cn=admin,dc=example,dc=com": "dc=example,dc=com",
		"uid=reader,ou=people,dc=x":  "ou=people,dc=x",
		"cn=admin":                   "",
		"":                           "",
	}
	for bindDN, want := range tests {
		if got := (Config{BindDN: bindDN}).baseDN(); got != want {
			t.Errorf("baseDN(%q) = %q, want %q", bindDN, got, want)
		}
	}
}

func TestFilterFor(t *testing.T) {
	cfg := Config{SearchFilter: "(|(userPrincipalName={username})(mail={username}))"}

	got, err := cfg.filterFor("john.doe@corp.example")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "(|(userPrincipalName=john.doe@corp.example)(mail=john.doe@corp.example))"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}

	// A login name must not be able to change the filter's shape: every
	// character that would close a clause is escaped, so the name stays data.
	got, err = cfg.filterFor("*)(objectClass=*")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	escaped := `\2a\29\28objectClass=\2a`
	want = "(|(userPrincipalName=" + escaped + ")(mail=" + escaped + "))"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}

	if _, err := (Config{SearchFilter: "(uid=fixed)"}).filterFor("john"); err == nil {
		t.Fatal("expected a filter without the placeholder to be rejected")
	}
}

func TestValidate(t *testing.T) {
	complete := Config{URL: "ldap.example.com", BindDN: "cn=admin,dc=x", SearchFilter: "(uid={username})"}
	if err := complete.validate(); err != nil {
		t.Fatalf("expected a complete config to validate, got %v", err)
	}

	for name, cfg := range map[string]Config{
		"missing url":    {BindDN: "cn=admin,dc=x", SearchFilter: "(uid={username})"},
		"missing binddn": {URL: "ldap.example.com", SearchFilter: "(uid={username})"},
		"missing filter": {URL: "ldap.example.com", BindDN: "cn=admin,dc=x"},
	} {
		if err := cfg.validate(); !errors.Is(err, ErrConfigIncomplete) {
			t.Errorf("%s: expected an incomplete-config error, got %v", name, err)
		}
	}
}

func TestRequestAttributes(t *testing.T) {
	got := Config{Attributes: []string{"uid", "MAIL", "cn", " "}}.requestAttributes()

	if got[0] != "uid" || got[1] != "MAIL" || got[2] != "cn" {
		t.Fatalf("expected the configured attributes first, got %v", got)
	}
	seen := make(map[string]int, len(got))
	for _, attribute := range got {
		seen[strings.ToLower(attribute)]++
	}
	if seen["mail"] != 1 {
		t.Errorf("expected mail to be requested once, got %v", got)
	}
	if seen["jpegphoto"] != 1 || seen["givenname"] != 1 || seen["userprincipalname"] != 1 {
		t.Errorf("expected the identity attributes to be requested, got %v", got)
	}
}

func TestIdentityFromEntry(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n")

	entry := &ldapv3.Entry{
		DN: "cn=John Doe,ou=people,dc=corp,dc=example",
		Attributes: []*ldapv3.EntryAttribute{
			{Name: "mail", Values: []string{"john.doe@corp.example"}},
			{Name: "givenName", Values: []string{"John"}},
			{Name: "cn", Values: []string{"John Doe"}},
			{Name: "sAMAccountName", Values: []string{"jdoe"}},
			{Name: "jpegPhoto", Values: []string{string(png)}, ByteValues: [][]byte{png}},
		},
	}

	identity := identityFromEntry(entry, "jdoe")
	if identity.DN != entry.DN {
		t.Errorf("expected the entry dn, got %q", identity.DN)
	}
	if identity.Email != "john.doe@corp.example" {
		t.Errorf("expected the directory mail, got %q", identity.Email)
	}
	if identity.Nickname != "John" {
		t.Errorf("expected givenName to name the user, got %q", identity.Nickname)
	}
	if identity.Attributes["samaccountname"] != "jdoe" {
		t.Errorf("expected attributes keyed by lower-cased name, got %v", identity.Attributes)
	}
	if !strings.HasPrefix(identity.AvatarURL, "data:image/png;base64,") {
		t.Errorf("expected a data url for the photo, got %q", identity.AvatarURL)
	}
}

func TestIdentityFromEntryFallsBackForEmailAndNickname(t *testing.T) {
	entry := &ldapv3.Entry{
		DN: "cn=Jane,dc=corp",
		Attributes: []*ldapv3.EntryAttribute{
			{Name: "userPrincipalName", Values: []string{"jane@corp.example"}},
			{Name: "cn", Values: []string{"Jane Roe"}},
			{Name: "jpegPhoto", Values: []string{"not an image"}, ByteValues: [][]byte{[]byte("not an image")}},
		},
	}

	identity := identityFromEntry(entry, "jane")
	if identity.Email != "jane@corp.example" {
		t.Errorf("expected userPrincipalName to stand in for mail, got %q", identity.Email)
	}
	if identity.Nickname != "Jane Roe" {
		t.Errorf("expected cn to name the user, got %q", identity.Nickname)
	}
	if identity.AvatarURL != "" {
		t.Errorf("expected a non-image attribute to be dropped, got %q", identity.AvatarURL)
	}

	// Nothing addressable in the entry: a login name that looks like an
	// address is the only identity left.
	identity = identityFromEntry(&ldapv3.Entry{DN: "cn=x"}, "user@corp.example")
	if identity.Email != "user@corp.example" {
		t.Errorf("expected the login name to stand in for a missing address, got %q", identity.Email)
	}
	if identity.Nickname != "user@corp.example" {
		t.Errorf("expected the login name to be the last nickname fallback, got %q", identity.Nickname)
	}
}

func TestAuthenticateRejectsMissingCredentials(t *testing.T) {
	cfg := Config{
		URL:          "ldap.example.com",
		BindDN:       "cn=admin,dc=x",
		BindPassword: "secret",
		SearchFilter: "(uid={username})",
	}

	for name, credentials := range map[string][2]string{
		"empty username": {"", "secret"},
		"empty password": {"john", ""},
	} {
		_, err := Authenticate(t.Context(), cfg, credentials[0], credentials[1])
		if !errors.Is(err, ErrMissingCredentials) {
			t.Errorf("%s: expected ErrMissingCredentials, got %v", name, err)
		}
	}
}
