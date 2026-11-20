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

package cas

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestCASConfig(t *testing.T) {
	values := Config{
		LoginURL: "https://sso.example/cas/login", ValidationURL: "https://sso.example/cas/p3/serviceValidate", ServiceURL: "https://ragflow.example/api/v1/auth/cas/callback",
	}
	client, err := NewClient(values)
	if err != nil || client.cfg.ProtocolVersion != "3" {
		t.Fatalf("CAS defaults = %+v, %v", client, err)
	}
	for _, value := range []string{
		"", "/cas", "http://sso.example/cas", "ftp://localhost/cas", "https://user:secret@sso.example/cas",
		"https://sso.example/cas?ticket=x", "https://sso.example/cas#fragment", "https://sso.example/cas?",
	} {
		if _, err := parseURL(value); err == nil {
			t.Errorf("accepted invalid CAS URL %q", value)
		}
	}
	for _, value := range []string{"https://sso.example/cas", "http://localhost:8080/cas", "http://127.0.0.1/cas", "http://[::1]/cas"} {
		if _, err := parseURL(value); err != nil {
			t.Errorf("rejected valid CAS URL %q: %v", value, err)
		}
	}
	values.ProtocolVersion = "4"
	if _, err := NewClient(values); err == nil {
		t.Fatal("accepted unsupported CAS protocol version")
	}
}

func TestCASIdentity(t *testing.T) {
	client, err := NewClient(Config{
		LoginURL: "https://sso.example/cas/login", ValidationURL: "https://sso.example/cas/p3/serviceValidate", ServiceURL: "https://ragflow.example/api/v1/auth/cas/callback",
	})
	if err != nil {
		t.Fatal(err)
	}
	wrap := func(body string) string {
		return `<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas">` + body + `</cas:serviceResponse>`
	}
	valid := `<cas:authenticationSuccess><cas:user>employee</cas:user><cas:attributes><cas:mail>employee@example.com</cas:mail><cas:displayName>Employee</cas:displayName></cas:attributes></cas:authenticationSuccess>`
	identity, err := client.parseUserInfo([]byte(wrap(valid)))
	if err != nil || identity.Email != "employee@example.com" || identity.Nickname != "Employee" || identity.Username != "employee" {
		t.Fatalf("CAS identity = %+v, %v", identity, err)
	}
	for name, body := range map[string]string{
		"failure":             `<cas:authenticationFailure code="INVALID_TICKET">invalid ticket</cas:authenticationFailure>`,
		"empty":               "",
		"duplicate success":   valid + valid,
		"success and failure": valid + `<cas:authenticationFailure/>`,
		"duplicate email":     strings.ReplaceAll(valid, `<cas:mail>employee@example.com</cas:mail>`, `<cas:mail>employee@example.com</cas:mail><cas:mail>other@example.com</cas:mail>`),
		"invalid email":       strings.ReplaceAll(valid, "employee@example.com", "not-an-email"),
		"email display name":  strings.ReplaceAll(valid, "employee@example.com", "Employee &lt;employee@example.com&gt;"),
		"empty user":          strings.ReplaceAll(valid, ">employee</cas:user>", "></cas:user>"),
		"duplicate user":      strings.ReplaceAll(valid, "</cas:user>", "</cas:user><cas:user>another</cas:user>"),
		"wrong namespace":     strings.ReplaceAll(valid, "cas:", "foreign:"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := client.parseUserInfo([]byte(wrap(body))); !errors.Is(err, ErrInvalidTicket) {
				t.Fatalf("accepted invalid CAS response: %v", err)
			}
		})
	}
	client.cfg.EmailAttribute = "user"
	identity, err = client.parseUserInfo([]byte(wrap(`<cas:authenticationSuccess><cas:user>employee@example.com</cas:user></cas:authenticationSuccess>`)))
	if err != nil || identity.Email != "employee@example.com" || identity.Nickname != "employee@example.com" {
		t.Fatalf("CAS username email mapping = %+v, %v", identity, err)
	}
	mapped := wrap(`<cas:authenticationSuccess><cas:user>principal@example.com</cas:user><cas:attributes><cas:user>attribute@example.com</cas:user><cas:displayName>Employee</cas:displayName></cas:attributes></cas:authenticationSuccess>`)
	client.cfg.EmailSource = SourceUsername
	client.cfg.NicknameSource = SourceUsername
	identity, err = client.parseUserInfo([]byte(mapped))
	if err != nil || identity.Email != "principal@example.com" || identity.Nickname != "principal@example.com" {
		t.Fatalf("explicit username mapping = %+v, %v", identity, err)
	}
	client.cfg.EmailSource = SourceAttribute
	identity, err = client.parseUserInfo([]byte(mapped))
	if err != nil || identity.Email != "attribute@example.com" {
		t.Fatalf("explicit attribute mapping = %+v, %v", identity, err)
	}
}

func TestCASAuthorizationURL(t *testing.T) {
	client, err := NewClient(Config{
		LoginURL: "https://sso.example/cas/login", ValidationURL: "https://sso.example/cas/p3/serviceValidate", ServiceURL: "https://ragflow.example/callback",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"", "nonce + / & ?"} {
		login, err := url.Parse(client.AuthorizationURL(state))
		if err != nil {
			t.Fatal(err)
		}
		serviceURL := login.Query().Get("service")
		callback, err := url.Parse(serviceURL)
		if err != nil || login.Path != "/cas/login" || callback.Query().Get("state") != state || serviceURL != client.ServiceURL(state) {
			t.Fatalf("incorrect CAS authorization URL for state %q: %s, %v", state, login, err)
		}
		if state == "" && serviceURL != "https://ragflow.example/callback" {
			t.Fatalf("CAS changed the service URL for an empty state: %q", serviceURL)
		}
	}
}

func TestCASIdentityWithoutEmail(t *testing.T) {
	client, err := NewClient(Config{
		LoginURL: "https://sso.example/cas/login", ValidationURL: "https://sso.example/cas/p3/serviceValidate", ServiceURL: "https://ragflow.example/callback", ProtocolVersion: "2",
	})
	if err != nil {
		t.Fatal(err)
	}
	response := `<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas"><cas:authenticationSuccess><cas:user>employee</cas:user></cas:authenticationSuccess></cas:serviceResponse>`
	identity, err := client.parseUserInfo([]byte(response))
	if err != nil || identity.Username != "employee" || identity.Email != "" {
		t.Fatalf("CAS username-only response = %+v, %v", identity, err)
	}
}

func TestCASTicketValidation(t *testing.T) {
	for _, version := range []string{"2", "3"} {
		t.Run("version "+version, func(t *testing.T) {
			path := "/cas/serviceValidate"
			if version == "3" {
				path = "/cas/p3/serviceValidate"
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != path || r.URL.Query().Get("ticket") != "ST-test" || r.URL.Query().Get("service") != "https://ragflow.example/callback?state=nonce" {
					t.Errorf("incorrect CAS validation request %s", r.URL.Path)
				}
				fmt.Fprint(w, `<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas"><cas:authenticationSuccess><cas:user>employee</cas:user><cas:attributes><cas:mail>employee@example.com</cas:mail></cas:attributes></cas:authenticationSuccess></cas:serviceResponse>`)
			}))
			defer server.Close()
			client, err := NewClient(Config{
				LoginURL: server.URL + "/cas/login", ValidationURL: server.URL + path, ServiceURL: "https://ragflow.example/callback", ProtocolVersion: version,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.ValidateTicket(t.Context(), "https://ragflow.example/callback?state=nonce", "ST-test"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCASV1TicketValidation(t *testing.T) {
	const serviceURL = "https://ragflow.example/callback?state=nonce"
	for _, tt := range []struct {
		name     string
		body     string
		username string
		email    string
	}{
		{name: "valid email", body: "yes\nemployee@example.com\n", username: "employee@example.com", email: "employee@example.com"},
		{name: "CRLF", body: "yes\r\nemployee@example.com\r\n", username: "employee@example.com", email: "employee@example.com"},
		{name: "username without email", body: "yes\nemployee\n", username: "employee"},
		{name: "rejected", body: "no\n"},
		{name: "success without principal", body: "yes\n"},
		{name: "blank principal", body: "yes\n \n"},
		{name: "multiple principals", body: "yes\nemployee@example.com\nother@example.com\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/cas/validate" || r.Header.Get("Accept") != "text/plain" ||
					r.URL.Query().Get("service") != serviceURL || r.URL.Query().Get("ticket") != "ST-test" {
					t.Errorf("incorrect CAS 1.0 validation request")
				}
				w.Header().Set("Content-Type", "text/plain")
				fmt.Fprint(w, tt.body)
			}))
			defer server.Close()

			client, err := NewClient(Config{
				LoginURL: server.URL + "/cas/login", ValidationURL: server.URL + "/cas/validate",
				ServiceURL: "https://ragflow.example/callback", ProtocolVersion: "1",
			})
			if err != nil {
				t.Fatal(err)
			}
			info, err := client.ValidateTicket(t.Context(), serviceURL, "ST-test")
			if tt.username == "" {
				if !errors.Is(err, ErrInvalidTicket) || info != nil {
					t.Fatalf("invalid response accepted: %+v, %v", info, err)
				}
				return
			}
			if err != nil || info.Username != tt.username || info.Email != tt.email || info.Nickname != tt.username {
				t.Fatalf("incorrect CAS 1.0 identity: %+v, %v", info, err)
			}
		})
	}
}

func TestCASValidationRejectsRedirectsAndOversizedResponses(t *testing.T) {
	for _, status := range []int{http.StatusFound, http.StatusInternalServerError, http.StatusOK} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "http://localhost/leak")
				w.WriteHeader(status)
				if status == http.StatusOK {
					fmt.Fprint(w, strings.Repeat("x", (1<<20)+1))
				}
			}))
			defer server.Close()
			client, err := NewClient(Config{
				LoginURL: server.URL + "/login", ValidationURL: server.URL + "/serviceValidate", ServiceURL: "https://ragflow.example/callback",
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.ValidateTicket(t.Context(), client.serviceURL.String(), "ST-secret-ticket")
			if !errors.Is(err, ErrInvalidTicket) || strings.Contains(err.Error(), "ST-secret-ticket") {
				t.Fatalf("unsafe CAS error: %v", err)
			}
			if status == http.StatusFound && !strings.Contains(err.Error(), "HTTP 302") {
				t.Fatalf("CAS validation followed a redirect: %v", err)
			}
		})
	}
}

func TestCASIndependentEndpointsAndAttributeMapping(t *testing.T) {
	client, err := NewClient(Config{
		LoginURL:        "https://login.example/cas-idp/app/login",
		ValidationURL:   "https://api.example/cas-idp/app/serviceValidate",
		ServiceURL:      "https://ragflow.example/api/v1/auth/cas/callback",
		ProtocolVersion: "2", EmailAttribute: "email", NicknameAttribute: "nickname",
	})
	if err != nil {
		t.Fatal(err)
	}
	login, _ := url.Parse(client.AuthorizationURL("nonce"))
	if login.Host != "login.example" || login.Path != "/cas-idp/app/login" || client.validationURL.Host != "api.example" {
		t.Fatal("CAS endpoints were derived instead of using the configured URLs")
	}
	response := `<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas"><cas:authenticationSuccess><cas:user>employee-id</cas:user><cas:attributes><cas:email>employee@example.com</cas:email><cas:nickname>Employee</cas:nickname></cas:attributes></cas:authenticationSuccess></cas:serviceResponse>`
	identity, err := client.parseUserInfo([]byte(response))
	if err != nil || identity.Email != "employee@example.com" || identity.Nickname != "Employee" {
		t.Fatalf("custom attributes: %+v, %v", identity, err)
	}
}
