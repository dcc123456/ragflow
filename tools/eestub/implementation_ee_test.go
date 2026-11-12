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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHookImplementationPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, stub, ee, status   string
		wantError, wantGenerated bool
	}{
		{"community_body", `func Hook() { println("work") }`, "", "OSS_IMPLEMENTED", false, false},
		{"forwarding_body", `func Hook() error { return work() }\nfunc work() error { println("work"); return nil }`, "", "OSS_IMPLEMENTED", false, false},
		{"empty_hook", `func Hook() {}`, "", "GENERATED", true, true},
		{"default_return", `func Hook() error { return nil }`, "", "GENERATED", true, true},
		{"empty_callback", `func Hook() func() { return func() {} }`, "", "GENERATED", true, true},
		{"passthrough", `func Hook(value string) (string, func()) { return value, func() {} }`, "", "GENERATED", true, true},
		{"empty_helper", `func Hook() { noop() }\nfunc noop() {}`, "", "GENERATED", true, true},
		{"not_implemented_helper", `func Hook() { unavailable() }\nfunc unavailable() { panic("not implemented") }`, "", "GENERATED", true, true},
		{"uncertain_control_flow", `func Hook(enabled bool) error { if enabled { return nil }; return nil }`, "", "REVIEW", false, false},
		{"uncertain_constant", `func Hook() bool { return true }`, "", "REVIEW", false, false},
		{"existing_ee_noop", `func Hook() {}`, `func Hook() {}`, "EXISTS", false, false},
		{"existing_ee_error_body", `func Hook() error { return nil }`, `func Hook() error { panic("not implemented") }`, "EXISTS", false, false},
		{"community_body_ee_noop", `func Hook() { println("work") }`, `func Hook() {}`, "EXISTS", false, false},
		{"ee_body", `func Hook() {}`, `func Hook() { println("EE work") }`, "EXISTS", false, false},
		{"helper_argument_work", `func Hook() { noop(work()) }\nfunc noop(int) {}\nfunc work() int { println("work"); return 0 }`, "", "OSS_IMPLEMENTED", false, false},
		{"dynamic_allocation", `func Hook(size int) []byte { return make([]byte, size) }`, "", "REVIEW", false, false},
		{"address_of_input", `func Hook(value int) *int { return &value }`, "", "OSS_IMPLEMENTED", false, false},
		{"callback_identity", `func Hook() func(int) int { return func(v int) int { return v } }`, "", "GENERATED", true, true},
		{"recursive_helper", `func Hook() { helper() }\nfunc helper() { Hook() }`, "", "REVIEW", false, false},
		{"not_implemented_response", `import "ragflow/internal/common"\nfunc Hook() { common.RespondError(nil, common.CodeNotImplemented, "not implemented") }`, "", "GENERATED", true, true},
		{"constructor", `type Service struct{}\nfunc NewService() *Service { return &Service{} }`, "", "OSS_IMPLEMENTED", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			putFile(t, dir, "stub.go", "//go:build !ee\n\npackage fixture\n"+strings.ReplaceAll(tc.stub, `\n`, "\n"))
			if tc.ee != "" {
				putFile(t, dir, "hook_ee.go", "//go:build ee\n\npackage fixture\n"+tc.ee)
			}
			var report bytes.Buffer
			err := generate(dir, []string{"."}, true, &report)
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v; report: %s", err, &report)
			}
			if !strings.Contains(report.String(), tc.status) {
				t.Fatalf("want %s: %s", tc.status, &report)
			}
			generated, _ := filepath.Glob(filepath.Join(dir, generatedPrefix+"*.go"))
			if (len(generated) > 0) != tc.wantGenerated {
				t.Fatalf("generated=%v: %s", generated, &report)
			}
			if tc.ee != "" {
				data, _ := os.ReadFile(filepath.Join(dir, "hook_ee.go"))
				if string(data) != "//go:build ee\n\npackage fixture\n"+tc.ee {
					t.Fatal("existing EE declaration was changed")
				}
			}
		})
	}
}

func TestCommunityImplementationWarningDoesNotFailOrGenerate(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	dir := t.TempDir()
	putFile(t, dir, "stub.go", "//go:build !ee\n\npackage fixture\nfunc Hook() { println(\"work\") }\n")
	var report bytes.Buffer
	if err := generate(dir, []string{"."}, true, &report); err != nil {
		t.Fatalf("warning failed check: %v", err)
	}
	if !strings.Contains(report.String(), "::warning ") || strings.Contains(report.String(), "::error ") || !strings.Contains(report.String(), "Result: PASSED WITH WARNINGS") {
		t.Fatalf("wrong warning severity: %s", &report)
	}
}

func TestConcreteCommunityBodyRemovesStaleFallback(t *testing.T) {
	dir := t.TempDir()
	putFile(t, dir, "stub.go", "//go:build !ee\n\npackage fixture\nfunc Hook() {}\n")
	var report bytes.Buffer
	if err := generate(dir, []string{"."}, true, &report); err == nil {
		t.Fatal("empty hook should fail")
	}
	putFile(t, dir, "stub.go", "//go:build !ee\n\npackage fixture\nfunc Hook() { println(\"work\") }\n")
	if err := generate(dir, []string{"."}, true, &report); err != nil {
		t.Fatalf("community body should only warn: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, generatedPrefix+"*.go"))
	if len(files) != 0 {
		t.Fatalf("stale fallbacks remain: %v", files)
	}
}

func TestWarningsDoNotHideBlockingHooks(t *testing.T) {
	dir := t.TempDir()
	putFile(t, dir, "stub.go", "//go:build !ee\n\npackage fixture\nfunc Community() { println(\"work\") }\nfunc First() {}\nfunc Second() error { return nil }\n")
	var report bytes.Buffer
	err := generate(dir, []string{"."}, true, &report)
	if err == nil || !strings.Contains(err.Error(), "2 blocking hook issues") {
		t.Fatalf("incorrect blocking count: %v; %s", err, &report)
	}
	for _, want := range []string{"OSS_IMPLEMENTED ..Community", "GENERATED ..First", "GENERATED ..Second", "Warnings: 1", "Generated panic fallbacks: 2"} {
		if !strings.Contains(report.String(), want) {
			t.Fatalf("missing %q: %s", want, &report)
		}
	}
	checkEnterpriseFiles(t, dir, true)
}

func TestWarningSummaryHasWarningSeverity(t *testing.T) {
	dir := t.TempDir()
	putFile(t, dir, "stub.go", "//go:build !ee\n\npackage fixture\nfunc Hook() { println(\"work\") }\n")
	summary := filepath.Join(t.TempDir(), "summary.md")
	var report bytes.Buffer
	if err := runCheck(dir, []string{"."}, true, &report, reportOptions{summaryPath: summary}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(summary)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "> [!WARNING]") || strings.Contains(string(data), "> [!CAUTION]") || !strings.Contains(string(data), "| WARNING | OSS_IMPLEMENTED |") {
		t.Fatalf("warning rendered as blocking error: %s", data)
	}
}
