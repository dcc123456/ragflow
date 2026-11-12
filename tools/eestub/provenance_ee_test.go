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

package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func gitFixture(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Source Developer", "GIT_AUTHOR_EMAIL=source@example.com", "GIT_COMMITTER_NAME=Source Developer", "GIT_COMMITTER_EMAIL=source@example.com")
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, data)
	}
	return strings.TrimSpace(string(data))
}

func commitFixture(t *testing.T, root, message string) string {
	t.Helper()
	gitFixture(t, root, "add", ".")
	gitFixture(t, root, "-c", "core.hooksPath=/dev/null", "commit", "-qm", message)
	return gitFixture(t, root, "rev-parse", "HEAD")
}

func TestReportAttributesMovedStubToSourcePR(t *testing.T) {
	root := t.TempDir()
	gitFixture(t, root, "init", "-q")
	putFile(t, root, "shared.go", "package fixture\nfunc Hook() {}\n")
	putFile(t, root, "stub.go", "//go:build !ee\n\npackage fixture\n")
	base := commitFixture(t, root, "Shared hook")
	putFile(t, root, "shared.go", "package fixture\n")
	putFile(t, root, "stub.go", "//go:build !ee\n\npackage fixture\nfunc Hook() {}\n")
	commit := commitFixture(t, root, "Move hook to stub (#42)")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/oss/commits/"+commit+"/pulls" {
			t.Errorf("unexpected provenance request: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing GitHub API authentication")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"number":42,"html_url":"https://github.com/acme/oss/pull/42","title":"Move hook","merge_commit_sha":"` + commit + `","merged_at":"2026-11-01T12:00:00Z","user":{"login":"source-dev"}}]`))
	}))
	defer server.Close()
	summary := filepath.Join(t.TempDir(), "summary.md")
	var report bytes.Buffer
	err := runCheck(root, []string{"."}, true, &report, reportOptions{baseRef: base, repository: "acme/enterprise", upstream: "acme/oss", apiURL: server.URL, token: "test-token", summaryPath: summary})
	if err == nil {
		t.Fatal("missing implementation must fail")
	}
	for _, want := range []string{
		"\x1b[31m  Source PR:      https://github.com/acme/oss/pull/42",
		"\x1b[31m  PR author:      @source-dev",
		"Location:       stub.go:4",
		"Origin:         Detected in this merge",
		"Source commit:  " + commit,
		"Moved from shared shared.go into a !ee stub",
	} {
		if !strings.Contains(report.String(), want) {
			t.Fatalf("missing %q: %s", want, &report)
		}
	}
	data, err := os.ReadFile(summary)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[acme/oss#42](https://github.com/acme/oss/pull/42)", "@source-dev", "Detected in this merge", "> [!CAUTION]"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("summary missing %q: %s", want, data)
		}
	}
	if strings.Contains(report.String(), "Investigation") || strings.Contains(string(data), "\x1b[") {
		t.Fatal("report must contain facts and clean Markdown")
	}
}

func TestReportDistinguishesPreexistingGapFromRemovedEEImplementation(t *testing.T) {
	for _, hadImplementation := range []bool{false, true} {
		t.Run(map[bool]string{false: "preexisting", true: "removed_ee"}[hadImplementation], func(t *testing.T) {
			root := t.TempDir()
			gitFixture(t, root, "init", "-q")
			putFile(t, root, "stub.go", "//go:build !ee\n\npackage fixture\nfunc Hook() {}\n")
			if hadImplementation {
				putFile(t, root, "hook_ee.go", "//go:build ee\n\npackage fixture\nfunc Hook() {}\n")
			}
			base := commitFixture(t, root, "Baseline")
			if hadImplementation {
				if err := os.Remove(filepath.Join(root, "hook_ee.go")); err != nil {
					t.Fatal(err)
				}
			}
			putFile(t, root, "note.txt", "unrelated change\n")
			commitFixture(t, root, "Next change")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "denied", http.StatusForbidden) }))
			defer server.Close()
			var report bytes.Buffer
			if err := runCheck(root, []string{"."}, true, &report, reportOptions{baseRef: base, repository: "acme/enterprise", upstream: "acme/oss", apiURL: server.URL}); err == nil {
				t.Fatal("missing declaration must fail even when PR lookup fails")
			}
			want := "Pre-existing"
			if hadImplementation {
				want = "Detected in this merge"
			}
			if !strings.Contains(report.String(), "Origin:         "+want) || !strings.Contains(report.String(), "Source PR:      Unknown") {
				t.Fatalf("incorrect attribution: %s", &report)
			}
		})
	}
}

func TestReportShowsSignatureChange(t *testing.T) {
	root := t.TempDir()
	gitFixture(t, root, "init", "-q")
	putFile(t, root, "stub.go", "//go:build !ee\n\npackage fixture\nfunc Hook(string) error { return nil }\n")
	putFile(t, root, "hook_ee.go", "//go:build ee\n\npackage fixture\nfunc Hook(string) error { return nil }\n")
	base := commitFixture(t, root, "Original signature")
	putFile(t, root, "stub.go", "//go:build !ee\n\npackage fixture\nfunc Hook(string) (string, error) { return \"\", nil }\n")
	commit := commitFixture(t, root, "Change hook results")
	var report bytes.Buffer
	if err := runCheck(root, []string{"."}, true, &report, reportOptions{baseRef: base}); err == nil {
		t.Fatal("signature mismatch must fail")
	}
	for _, want := range []string{"ERROR [MISMATCH]", "Changed declaration signature", "Previous:       func Hook(string) error", "Expected:       func Hook(string) (string, error)", "EE signature:   func Hook(string) error", "Source commit:  " + commit} {
		if !strings.Contains(report.String(), want) {
			t.Fatalf("missing %q: %s", want, &report)
		}
	}
}

func TestSummaryGroupsDeclarationsAndCachesPRLookup(t *testing.T) {
	root := t.TempDir()
	gitFixture(t, root, "init", "-q")
	for _, name := range []string{"first", "second"} {
		if err := os.Mkdir(filepath.Join(root, name), 0755); err != nil {
			t.Fatal(err)
		}
		putFile(t, filepath.Join(root, name), "stub.go", "//go:build !ee\n\npackage fixture\n")
	}
	base := commitFixture(t, root, "Empty stubs")
	for _, name := range []string{"first", "second"} {
		putFile(t, filepath.Join(root, name), "stub.go", "//go:build !ee\n\npackage fixture\nfunc Hook() {}\n")
	}
	commit := commitFixture(t, root, "Add hooks (#42)")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`[{"number":42,"html_url":"https://github.com/acme/oss/pull/42","merge_commit_sha":"` + commit + `","user":{"login":"source-dev"}}]`))
	}))
	defer server.Close()
	summary := filepath.Join(t.TempDir(), "summary.md")
	var report bytes.Buffer
	if err := runCheck(root, []string{"."}, true, &report, reportOptions{baseRef: base, upstream: "acme/oss", apiURL: server.URL, summaryPath: summary}); err == nil {
		t.Fatal("missing hooks must fail")
	}
	if requests.Load() != 1 {
		t.Fatalf("same source commit queried %d times", requests.Load())
	}
	data, err := os.ReadFile(summary)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "> [!CAUTION]") != 1 || !strings.Contains(string(data), "first.Hook") || !strings.Contains(string(data), "second.Hook") {
		t.Fatalf("incorrect PR grouping: %s", data)
	}
}

func TestAttributionDoesNotAssignLaterEnterpriseSyncPR(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/acme/oss/") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`[{"number":999,"html_url":"https://github.com/acme/enterprise/pull/999","merge_commit_sha":"other-commit","merged_at":"2026-11-02T12:00:00Z","user":{"login":"merge-author"}}]`))
	}))
	defer server.Close()
	resolver := newSourceResolver(t.TempDir(), reportOptions{upstream: "acme/oss", repository: "acme/enterprise", apiURL: server.URL})
	if pr := resolver.lookupPR("original-commit", "Source change (#42)"); pr.URL != "" {
		t.Fatalf("sync PR mistaken for source PR: %+v", pr)
	}
}
