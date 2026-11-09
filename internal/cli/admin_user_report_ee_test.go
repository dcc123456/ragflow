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

package cli

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAdminUserReportRoutingEE(t *testing.T) {
	for _, tt := range []struct{ command, endpoint, query string }{
		{"LIST USERS INACTIVE DAYS 30;", "/reports", "days=30&status=INACTIVE"},
		{"LIST USERS INACTIVE DAYS 30 PAGE 2 PAGE_SIZE 10;", "/reports", "days=30&page=2&page_size=10&status=INACTIVE"},
		{"LIST USERS PLAN 'Starter';", "/reports", "plan=Starter"},
		{"LIST USERS PLAN 'Starter' DAYS 30;", "/reports", "days=30&plan=Starter"},
		{"LIST USERS ACTIVE DAYS 7;", "", "days=7&status=ACTIVE"},
		{"LIST USERS STORAGE TOP 3;", "", "order=STORAGE&top=3"},
		{"LIST USERS DOCUMENTS TOP 2;", "", "order=DOCUMENTS&top=2"},
		{"LIST USERS INDEX TOP 2;", "", "order=INDEX&top=2"},
		{"LIST USERS QUOTA 80 PAGE 2 PAGE_SIZE 10;", "", "page=2&page_size=10&quota=80"},
		{"LIST USERS PLAN 'Pro' QUOTA 80 DAYS 7;", "", "days=7&plan=Pro&quota=80"},
	} {
		t.Run(tt.command, func(t *testing.T) {
			client := newTestHTTPClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/admin/users"+tt.endpoint || r.URL.RawQuery != tt.query || len(body) != 0 || r.Header.Get("Authorization") != "admin-token" {
					t.Errorf("request=%s %s body=%q", r.Method, r.URL, body)
				}
				_, _ = io.WriteString(w, `{"code":0,"data":[{"user":"alice@example.com"}],"total":1}`)
			}))
			token := "admin-token"
			client.LoginToken = &token
			c := &CLI{Config: &CommandLineConfig{CLIMode: AdminMode, OutputFormat: OutputFormatJSON}, AdminServerClient: client}
			cmd, err := NewParser(tt.command).Parse(AdminMode)
			if err != nil {
				t.Fatal(err)
			}
			response, err := c.ExecuteCommand(1, cmd)
			if err != nil {
				t.Fatal(err)
			}
			result, ok := response.(*OrderedCommonResponse)
			if !ok || len(result.Data) != 1 || result.Data[0]["user"] != "alice@example.com" || result.OutputFormat != OutputFormatJSON {
				t.Fatalf("response=%#v", response)
			}
		})
	}
}

func TestAdminUserReportErrorsEE(t *testing.T) {
	for _, tt := range []struct {
		status        int
		body, message string
	}{
		{http.StatusForbidden, `{"code":403}`, "HTTP 403"},
		{http.StatusOK, `{"code":400,"message":"invalid report"}`, "invalid report"},
		{http.StatusOK, `{"code":0,"data":`, "invalid JSON"},
	} {
		t.Run(tt.message, func(t *testing.T) {
			client := newTestHTTPClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			token := "admin-token"
			client.LoginToken = &token
			c := &CLI{Config: &CommandLineConfig{CLIMode: AdminMode}, AdminServerClient: client}
			cmd, err := NewParser("LIST USERS INACTIVE DAYS 30;").Parse(AdminMode)
			if err != nil {
				t.Fatal(err)
			}
			response, err := c.ExecuteCommand(1, cmd)
			if response != nil || err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("response=%v error=%v", response, err)
			}
		})
	}
}
