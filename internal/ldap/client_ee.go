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

// Package ldap authenticates a login name and password against an LDAP
// directory. It speaks only the protocol: server discovery, the settings a
// server is built from, and account provisioning live in internal/service.
package ldap

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	ldapv3 "github.com/go-ldap/ldap/v3"
)

const (
	// UsernamePlaceholder is the token a search filter uses for the login name.
	// It is replaced with a filter-escaped value, so a login name containing
	// `*`, `(` or `\` cannot widen the search.
	UsernamePlaceholder = "{username}"

	// DefaultTimeout bounds a single connect/search/bind step.
	DefaultTimeout = 15 * time.Second

	defaultPort    = "389"
	defaultTLSPort = "636"
)

// identityAttributes are always requested in addition to the configured
// attribute list: they are what the caller needs to match an account and to
// shape the profile of a first-time user.
var identityAttributes = []string{"mail", "userPrincipalName", "displayName", "givenName", "cn", "jpegPhoto"}

var (
	// ErrConfigIncomplete reports a server entry that cannot be used at all.
	ErrConfigIncomplete = errors.New("ldap configuration is incomplete")
	// ErrMissingCredentials reports an empty login name or password. The
	// directory is never contacted in that case.
	ErrMissingCredentials = errors.New("username and password are required")
	// ErrUserNotFound reports that the search filter matched no entry.
	ErrUserNotFound = errors.New("ldap user not found")
	// ErrInvalidCredentials reports that the directory rejected the password.
	ErrInvalidCredentials = errors.New("invalid ldap credentials")
)

// Config is one LDAP server as the login flow sees it.
type Config struct {
	// URL is the directory endpoint. A missing scheme means ldap:// and a
	// missing port means 389, or 636 for ldaps://.
	URL string
	// BindDN and BindPassword are the service account used to search. The
	// search base is derived from BindDN: the entry and everything below it.
	BindDN       string
	BindPassword string
	// SearchFilter locates the entry for a login name and must contain
	// UsernamePlaceholder.
	SearchFilter string
	// Attributes is the configured attribute list, used to fill
	// Identity.Attributes.
	Attributes []string
	// Timeout overrides DefaultTimeout when set.
	Timeout time.Duration
}

// Identity is the directory record a successful authentication resolved.
type Identity struct {
	// DN is the distinguished name the password was verified against.
	DN string
	// Email is the address the account is matched and provisioned by, empty
	// when the directory exposes none.
	Email string
	// Nickname is the display name to greet the user with.
	Nickname string
	// AvatarURL is a data URL carrying the directory photo, empty when the
	// entry has none.
	AvatarURL string
	// Attributes holds every attribute the server returned, keyed by the
	// lower-cased attribute name.
	Attributes map[string]string
}

// Authenticate resolves username to a directory entry and verifies password
// against it. The service account binds first to search, then the resolved
// entry binds with the supplied password, which is what makes the directory -
// not this code - the authority on the credential.
func Authenticate(ctx context.Context, cfg Config, username, password string) (*Identity, error) {
	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	login := strings.TrimSpace(username)
	if login == "" || password == "" {
		return nil, ErrMissingCredentials
	}
	filter, err := cfg.filterFor(login)
	if err != nil {
		return nil, err
	}

	conn, err := dial(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to ldap server: %w", err)
	}
	defer conn.Close()

	if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
		return nil, fmt.Errorf("bind as service account: %w", err)
	}

	result, err := conn.Search(&ldapv3.SearchRequest{
		BaseDN:       cfg.baseDN(),
		Scope:        ldapv3.ScopeWholeSubtree,
		DerefAliases: ldapv3.NeverDerefAliases,
		// SizeLimit 2 keeps the query cheap while still telling a filter that
		// matches several entries apart from one that matches a single entry.
		SizeLimit:  2,
		TimeLimit:  int(cfg.timeout() / time.Second),
		TypesOnly:  false,
		Filter:     filter,
		Attributes: cfg.requestAttributes(),
	})
	if err != nil {
		return nil, fmt.Errorf("search ldap entry: %w", err)
	}
	if len(result.Entries) == 0 {
		return nil, ErrUserNotFound
	}

	entry := result.Entries[0]
	if err := conn.Bind(entry.DN, password); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCredentials, err)
	}
	return identityFromEntry(entry, login), nil
}

func (c Config) withDefaults() Config {
	if len(c.Attributes) == 0 {
		c.Attributes = identityAttributes
	}
	return c
}

func (c Config) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return DefaultTimeout
}

// validate reports the settings without which no bind can be attempted.
func (c Config) validate() error {
	if strings.TrimSpace(c.URL) == "" {
		return fmt.Errorf("%w: url is empty", ErrConfigIncomplete)
	}
	if strings.TrimSpace(c.BindDN) == "" {
		return fmt.Errorf("%w: bind dn is empty", ErrConfigIncomplete)
	}
	if strings.TrimSpace(c.SearchFilter) == "" {
		return fmt.Errorf("%w: search_filter is empty", ErrConfigIncomplete)
	}
	return nil
}

// baseDN is the search base implied by the service account: the entry it binds
// as, and everything below it. A bind DN that is a single RDN searches the
// whole directory, which is the only base left to infer.
func (c Config) baseDN() string {
	_, rest, found := strings.Cut(strings.TrimSpace(c.BindDN), ",")
	if !found {
		return ""
	}
	return strings.TrimSpace(rest)
}

func (c Config) filterFor(username string) (string, error) {
	filter := strings.TrimSpace(c.SearchFilter)
	if !strings.Contains(filter, UsernamePlaceholder) {
		return "", fmt.Errorf("%w: search_filter must contain %s", ErrConfigIncomplete, UsernamePlaceholder)
	}
	return strings.ReplaceAll(filter, UsernamePlaceholder, ldapv3.EscapeFilter(username)), nil
}

// requestAttributes is the configured list plus the attributes the login flow
// needs, de-duplicated case-insensitively so a configured entry is not asked
// for twice.
func (c Config) requestAttributes() []string {
	attributes := make([]string, 0, len(c.Attributes)+len(identityAttributes))
	seen := make(map[string]struct{}, len(c.Attributes)+len(identityAttributes))
	for _, attribute := range append(append([]string{}, c.Attributes...), identityAttributes...) {
		name := strings.TrimSpace(attribute)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		attributes = append(attributes, name)
	}
	return attributes
}

func dial(ctx context.Context, cfg Config) (*ldapv3.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	endpoint, err := normalizeURL(cfg.URL)
	if err != nil {
		return nil, err
	}

	options := []ldapv3.DialOpt{ldapv3.DialWithDialer(&net.Dialer{Timeout: cfg.timeout()})}
	if strings.HasPrefix(strings.ToLower(endpoint), "ldaps://") {
		parsed, err := url.Parse(endpoint)
		if err != nil {
			return nil, fmt.Errorf("%w: parse url: %v", ErrConfigIncomplete, err)
		}
		options = append(options, ldapv3.DialWithTLSConfig(&tls.Config{
			ServerName: parsed.Hostname(),
			MinVersion: tls.VersionTLS12,
		}))
	}

	conn, err := ldapv3.DialURL(endpoint, options...)
	if err != nil {
		return nil, err
	}
	conn.SetTimeout(cfg.timeout())
	return conn, nil
}

// normalizeURL accepts the forms an administrator types into the console
// (`ldap.example.com`, `ldap://host:1389`, `ldaps://host`) and returns a URL the
// client can dial.
func normalizeURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("%w: url is empty", ErrConfigIncomplete)
	}
	if !strings.Contains(value, "://") {
		value = "ldap://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("%w: parse url: %v", ErrConfigIncomplete, err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "ldap", "ldaps":
	default:
		return "", fmt.Errorf("%w: unsupported scheme %q", ErrConfigIncomplete, parsed.Scheme)
	}
	if parsed.Hostname() == "" {
		return "", fmt.Errorf("%w: url has no host", ErrConfigIncomplete)
	}
	if parsed.Port() == "" {
		port := defaultPort
		if strings.EqualFold(parsed.Scheme, "ldaps") {
			port = defaultTLSPort
		}
		parsed.Host = net.JoinHostPort(parsed.Hostname(), port)
	}
	return parsed.String(), nil
}

func identityFromEntry(entry *ldapv3.Entry, login string) *Identity {
	identity := &Identity{
		DN:         entry.DN,
		Attributes: make(map[string]string, len(entry.Attributes)),
	}
	for _, attribute := range entry.Attributes {
		if len(attribute.Values) == 0 {
			continue
		}
		identity.Attributes[strings.ToLower(attribute.Name)] = attribute.Values[0]
	}

	// The directory address wins over the login name: the login name is only
	// known to be an address when the filter matched on one, and directories
	// that publish `mail` are the ones that can be matched to a local account.
	identity.Email = firstNonEmpty(
		identity.Attributes["mail"],
		identity.Attributes["userprincipalname"],
		identity.Attributes["email"],
	)
	if identity.Email == "" && strings.Contains(login, "@") {
		identity.Email = login
	}
	identity.Nickname = firstNonEmpty(
		identity.Attributes["givenname"],
		identity.Attributes["displayname"],
		identity.Attributes["cn"],
		login,
	)
	identity.AvatarURL = photoDataURL(entry.GetEqualFoldRawAttributeValue("jpegPhoto"))
	return identity
}

// photoDataURL turns the directory photo into something an <img> can render.
// A directory stores the bytes without a media type, so the type is sniffed and
// anything that is not an image is dropped rather than served as a broken
// avatar.
func photoDataURL(photo []byte) string {
	if len(photo) == 0 {
		return ""
	}
	mediaType := http.DetectContentType(photo)
	if !strings.HasPrefix(mediaType, "image/") {
		return ""
	}
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(photo)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
