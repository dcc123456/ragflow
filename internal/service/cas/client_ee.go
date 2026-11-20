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

// Package cas authenticates browser users through CAS service tickets.
package cas

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"
)

// Config describes one CAS identity provider.
type Config struct {
	LoginURL          string
	ValidationURL     string
	ServiceURL        string
	ProtocolVersion   string
	EmailSource       AttributeSource
	EmailAttribute    string
	NicknameSource    AttributeSource
	NicknameAttribute string
}

// AttributeSource selects the CAS principal or a named returned attribute.
type AttributeSource string

const (
	SourceUsername  AttributeSource = "username"
	SourceAttribute AttributeSource = "attribute"
)

// Client implements CAS 1.0, 2.0 and 3.0 browser login.
type Client struct {
	cfg           Config
	httpClient    *http.Client
	loginURL      *url.URL
	validationURL *url.URL
	serviceURL    *url.URL
}

const (
	httpRequestTimeout = 7 * time.Second
	protocolVersion1   = "1"
)

// UserInfo is the normalized identity returned by CAS ticket validation.
type UserInfo struct {
	Email    string
	Username string
	Nickname string
}

// NewClient validates a provider configuration and constructs its CAS client.
func NewClient(cfg Config) (*Client, error) {
	loginURL, err := parseURL(cfg.LoginURL)
	if err != nil {
		return nil, fmt.Errorf("CAS login URL: %w", err)
	}
	validationURL, err := parseURL(cfg.ValidationURL)
	if err != nil {
		return nil, fmt.Errorf("CAS validation URL: %w", err)
	}
	serviceURL, err := parseURL(cfg.ServiceURL)
	if err != nil {
		return nil, fmt.Errorf("CAS callback URL: %w", err)
	}
	if cfg.ProtocolVersion == "" {
		cfg.ProtocolVersion = "3"
	}
	switch cfg.ProtocolVersion {
	case protocolVersion1, "2", "3":
	default:
		return nil, fmt.Errorf("CAS protocol version must be 1, 2 or 3")
	}
	if cfg.ProtocolVersion != protocolVersion1 {
		if err := validateSource(cfg.EmailSource, cfg.EmailAttribute); err != nil {
			return nil, fmt.Errorf("CAS account email: %w", err)
		}
		if err := validateSource(cfg.NicknameSource, cfg.NicknameAttribute); err != nil {
			return nil, fmt.Errorf("CAS display name: %w", err)
		}
	}
	return &Client{
		cfg: cfg, loginURL: loginURL, validationURL: validationURL, serviceURL: serviceURL,
		httpClient: &http.Client{
			Timeout:       httpRequestTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

func validateSource(source AttributeSource, attribute string) error {
	switch source {
	case "", SourceUsername:
		return nil
	case SourceAttribute:
		if attribute != "" {
			return nil
		}
		return errors.New("enter an attribute name")
	default:
		return errors.New("select the CAS username or a returned attribute")
	}
}

func parseURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return nil, fmt.Errorf("provide an absolute URL without credentials, query parameters or fragments")
	}
	host := parsed.Hostname()
	loopback := strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopback) {
		return nil, fmt.Errorf("HTTPS is required, with HTTP allowed for localhost development")
	}
	return parsed, nil
}

// ServiceURL returns the callback URL used for both login and ticket validation.
func (c *Client) ServiceURL(state string) string {
	callback := *c.serviceURL
	query := callback.Query()
	if state != "" {
		query.Set("state", state)
	}
	callback.RawQuery = query.Encode()
	return callback.String()
}

// AuthorizationURL returns the CAS login URL for this browser session.
func (c *Client) AuthorizationURL(state string) string {
	endpoint := *c.loginURL
	query := endpoint.Query()
	query.Set("service", c.ServiceURL(state))
	endpoint.RawQuery = query.Encode()
	return endpoint.String()
}

// ErrInvalidTicket indicates a rejected ticket or invalid validation response.
var ErrInvalidTicket = errors.New("CAS ticket validation failed")

const xmlNamespace = "http://www.yale.edu/tp/cas"

type casValidationResponse struct {
	XMLName xml.Name `xml:"http://www.yale.edu/tp/cas serviceResponse"`
	Success []struct {
		Users      []string `xml:"http://www.yale.edu/tp/cas user"`
		Attributes struct {
			Values []struct {
				XMLName xml.Name
				Value   string `xml:",chardata"`
			} `xml:",any"`
		} `xml:"http://www.yale.edu/tp/cas attributes"`
	} `xml:"http://www.yale.edu/tp/cas authenticationSuccess"`
	Failure []struct{} `xml:"http://www.yale.edu/tp/cas authenticationFailure"`
}

// ValidateTicket verifies a ticket against the exact service URL used at login.
func (c *Client) ValidateTicket(ctx context.Context, serviceURL, ticket string) (*UserInfo, error) {
	if ticket == "" || len(ticket) > 4096 {
		return nil, ErrInvalidTicket
	}
	endpoint := *c.validationURL
	query := endpoint.Query()
	query.Set("service", serviceURL)
	query.Set("ticket", ticket)
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, ErrInvalidTicket
	}
	if c.cfg.ProtocolVersion == protocolVersion1 {
		req.Header.Set("Accept", "text/plain")
	} else {
		req.Header.Set("Accept", "application/xml")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		// HTTP errors can contain the request URL and its authentication ticket.
		return nil, fmt.Errorf("%w: validation request failed", ErrInvalidTicket)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: HTTP %d", ErrInvalidTicket, resp.StatusCode)
	}
	const maxResponseSize = 1 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil || len(body) > maxResponseSize {
		return nil, ErrInvalidTicket
	}
	if c.cfg.ProtocolVersion == protocolVersion1 {
		return parseV1UserInfo(body)
	}
	return c.parseUserInfo(body)
}

// parseV1UserInfo reads the principal after the success line. CAS 1.0 does
// not carry attributes, so an email-shaped principal is the only email source.
func parseV1UserInfo(body []byte) (*UserInfo, error) {
	lines := strings.SplitN(string(body), "\n", 3)
	if len(lines) != 3 || strings.TrimSuffix(lines[0], "\r") != "yes" || lines[2] != "" {
		return nil, ErrInvalidTicket
	}
	username := strings.TrimSpace(lines[1])
	if username == "" {
		return nil, ErrInvalidTicket
	}

	info := &UserInfo{Username: username, Nickname: username}
	if address, err := mail.ParseAddress(username); err == nil && address.Address == username {
		info.Email = address.Address
	}
	return info, nil
}

func (c *Client) parseUserInfo(body []byte) (*UserInfo, error) {
	var response casValidationResponse
	if err := xml.Unmarshal(body, &response); err != nil || len(response.Success) != 1 || len(response.Failure) != 0 {
		return nil, ErrInvalidTicket
	}
	success := response.Success[0]
	if len(success.Users) != 1 || strings.TrimSpace(success.Users[0]) == "" {
		return nil, ErrInvalidTicket
	}
	username := strings.TrimSpace(success.Users[0])
	attributes := make(map[string][]string)
	for _, attribute := range success.Attributes.Values {
		if attribute.XMLName.Space == "" || attribute.XMLName.Space == xmlNamespace {
			name := attribute.XMLName.Local
			attributes[name] = append(attributes[name], strings.TrimSpace(attribute.Value))
		}
	}
	emails := sourceValues(c.cfg.EmailSource, c.cfg.EmailAttribute, "mail", username, attributes)
	if len(emails) > 1 {
		return nil, fmt.Errorf("%w: CAS returned multiple email attributes", ErrInvalidTicket)
	}
	email := ""
	if len(emails) == 1 && emails[0] != "" {
		address, err := mail.ParseAddress(emails[0])
		if err != nil || address.Address != emails[0] {
			return nil, fmt.Errorf("%w: CAS email attribute is invalid", ErrInvalidTicket)
		}
		email = address.Address
	}
	nickname := username
	if names := sourceValues(c.cfg.NicknameSource, c.cfg.NicknameAttribute, "displayName", username, attributes); len(names) == 1 && names[0] != "" {
		nickname = names[0]
	}
	return &UserInfo{Username: username, Email: email, Nickname: nickname}, nil
}

func sourceValues(source AttributeSource, attribute, defaultAttribute, username string, attributes map[string][]string) []string {
	if source == SourceUsername || (source == "" && attribute == "user") {
		return []string{username}
	}
	if attribute == "" {
		attribute = defaultAttribute
	}
	return attributes[attribute]
}
