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
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func generate(root string, dirs []string, write bool, out io.Writer) error {
	return runCheck(root, dirs, write, out, reportOptions{})
}

func putFile(t *testing.T, dir, name, source string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateMissingDeclarations(t *testing.T) {
	dir := t.TempDir()
	putFile(t, dir, "shared.go", `package fixture
type Service struct{}
func use(s *Service) { _, _ = missing(nil); s.Run() }
`)
	putFile(t, dir, "stub.go", `//go:build !ee

package fixture
import (
 ctx "context"
 "fmt"
)
func Present(ctx.Context) error { return nil }
func missing(c ctx.Context) (ctx.Context, error) { return nil, nil }
func CommunityBody(c ctx.Context) { fmt.Println(c) }
func (*Service) Run() { helper() }
func helper() {}
func Shadow(panic string) (err error) { return nil }
func init() {}
`)
	putFile(t, dir, "present_ee.go", `//go:build ee

package fixture
import "context"
func Present(c context.Context) error { println("implemented"); return nil }
`)
	var report bytes.Buffer
	if err := generate(dir, []string{"."}, true, &report); err == nil {
		t.Fatal("generated fallbacks must fail the check")
	}
	if !strings.Contains(report.String(), "GENERATED") || strings.Contains(report.String(), "helper:") {
		t.Fatalf("unexpected report: %s", &report)
	}
	checkEnterpriseFiles(t, dir, true)
	// Re-running must neither treat generated code as a real implementation
	// nor append duplicate declarations.
	if err := generate(dir, []string{"."}, true, &report); err == nil {
		t.Fatal("generated fallbacks must not count as real implementations")
	}
	checkEnterpriseFiles(t, dir, true)
	putFile(t, dir, "real_ee.go", `//go:build ee

package fixture
import "context"
func missing(c context.Context) (context.Context, error) { println("implemented"); return c, nil }
func (*Service) Run() { println("implemented") }
func Shadow(string) error { println("implemented"); return nil }
`)
	if err := generate(dir, []string{"."}, true, &report); err != nil {
		t.Fatal(err)
	}
	checkEnterpriseFiles(t, dir, false)
}

// Type-check the actual enterprise file set, including generated code. This
// catches missing signature imports, unused body imports and duplicate symbols.
func checkEnterpriseFiles(t *testing.T, dir string, wantFallback bool) {
	t.Helper()
	ctx := enterpriseContext()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	fallbacks := 0
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		match, err := ctx.MatchFile(dir, entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if !match {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, entry.Name()), nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(entry.Name(), generatedPrefix) {
			fallbacks++
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				if len(fn.Body.List) != 1 {
					t.Fatal("fallback must contain only panic")
				}
				stmt, ok := fn.Body.List[0].(*ast.ExprStmt)
				if !ok {
					t.Fatal("fallback must panic")
				}
				call, ok := stmt.X.(*ast.CallExpr)
				if !ok {
					t.Fatal("fallback must panic")
				}
				name, ok := call.Fun.(*ast.Ident)
				if !ok || name.Name != "panic" {
					t.Fatal("fallback must panic")
				}
			}
		}
		files = append(files, file)
	}
	if (fallbacks > 0) != wantFallback {
		t.Fatalf("fallback files = %d, want fallback = %v", fallbacks, wantFallback)
	}
	config := types.Config{Importer: importer.Default()}
	if _, err := config.Check("fixture", fset, files, nil); err != nil {
		t.Fatal(err)
	}
}

func TestExistingSignaturesAreNeverOverwritten(t *testing.T) {
	for _, tc := range []struct {
		name, stub, implementation, status string
	}{
		{"aliases_and_groups", `func Hook(c context.Context, a, b string) (err error) { return nil }`, `func Hook(ctx other.Context, x string, y string) error { println("implemented"); return nil }`, "EXISTS"},
		{"nested_function_names", `func Hook() func(err *error) { return nil }`, `func Hook() func(*error) { return func(*error) { println("implemented") } }`, "EXISTS"},
		{"return_type", `func Hook() error { return nil }`, `func Hook() bool { return false }`, "MISMATCH"},
		{"variadic", `func Hook(x ...string) {}`, `func Hook(x []string) {}`, "MISMATCH"},
		{"receiver", `func (*Service) Hook() {}`, `func (Service) Hook() {}`, "MISMATCH"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			putFile(t, dir, "shared.go", "package fixture\ntype Service struct{}\n")
			putFile(t, dir, "stub.go", "//go:build !ee\n\npackage fixture\nimport \"context\"\n"+tc.stub)
			// An untagged implementation is also part of the enterprise build.
			implementation := "package fixture\nimport other \"context\"\n" + tc.implementation
			putFile(t, dir, "implementation.go", implementation)
			var report bytes.Buffer
			err := generate(dir, []string{"."}, true, &report)
			if (err != nil) != (tc.status == "MISMATCH") {
				t.Fatalf("unexpected check result: %v; report: %s", err, &report)
			}
			if !strings.Contains(report.String(), tc.status) {
				t.Fatalf("want %s: %s", tc.status, &report)
			}
			got, _ := os.ReadFile(filepath.Join(dir, "implementation.go"))
			if string(got) != implementation {
				t.Fatal("implementation was modified")
			}
			matches, _ := filepath.Glob(filepath.Join(dir, generatedPrefix+"*.go"))
			if len(matches) != 0 {
				t.Fatalf("existing declarations got fallbacks: %v", matches)
			}
		})
	}
}

func TestUnsupportedDeclarationsAreReportedWithoutGeneration(t *testing.T) {
	for _, tc := range []struct{ name, stub, extra string }{
		{"missing_receiver", "type Service struct{}\nfunc (*Service) Hook() {}", ""},
		{"missing_type", "type Result struct{}\nfunc Hook() Result { return Result{} }", ""},
		{"generics", "func Hook[T any](v T) T { return v }", ""},
		{"conditional_implementation", "func Hook() {}", "//go:build ee && custom\n\npackage fixture\nfunc Hook() {}\n"},
		{"dot_import", "import . \"context\"\nfunc Hook(Context) {}", ""},
		{"cgo_preamble", "import \"C\"\nfunc Hook(C.int) {}", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			putFile(t, dir, "stub.go", "//go:build !ee\n\npackage fixture\n"+tc.stub)
			if tc.extra != "" {
				putFile(t, dir, "conditional_ee.go", tc.extra)
			}
			var report bytes.Buffer
			if err := generate(dir, []string{"."}, true, &report); err == nil {
				t.Fatal("unsupported declarations must fail the check")
			}
			if !strings.Contains(report.String(), "UNSUPPORTED") {
				t.Fatalf("unsupported declaration not reported: %s", &report)
			}
			matches, _ := filepath.Glob(filepath.Join(dir, generatedPrefix+"*.go"))
			if len(matches) != 0 {
				t.Fatalf("unsupported declaration got a fallback: %v", matches)
			}
		})
	}
}

func TestDryRunDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	putFile(t, dir, "stub.go", "//go:build !ee\n\npackage fixture\nfunc Hook() {}\n")
	var report bytes.Buffer
	if err := generate(dir, []string{"."}, false, &report); err == nil {
		t.Fatal("missing declarations must fail dry run")
	}
	if !strings.Contains(report.String(), "MISSING") {
		t.Fatalf("missing declaration not reported: %s", &report)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatal("dry run wrote files")
	}
}

func TestRemoveOrphanedFallback(t *testing.T) {
	dir := t.TempDir()
	putFile(t, dir, "stub.go", "//go:build !ee\n\npackage fixture\nfunc Hook() {}\n")
	var report bytes.Buffer
	if err := generate(dir, []string{"."}, true, &report); err == nil {
		t.Fatal("generated fallbacks must fail the check")
	}
	if err := os.Remove(filepath.Join(dir, "stub.go")); err != nil {
		t.Fatal(err)
	}
	if err := generate(dir, []string{"."}, true, &report); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("fallback survived removal of its source stub")
	}
}

func TestUnsupportedDependencies(t *testing.T) {
	for _, tc := range []struct{ name, stub, shared string }{
		{"non_type_dependency", "type Result struct{}\nfunc Hook() Result { return Result{} }", "var Result = 1"},
		{"import_shadows_panic", "import panic \"context\"\nfunc Hook(panic.Context) {}", ""},
		{"nonlocal_receiver", "type Service struct{}\nfunc (*Service) Hook() {}", "type Service = int"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			putFile(t, dir, "stub.go", "//go:build !ee\n\npackage fixture\n"+tc.stub)
			putFile(t, dir, "implementation_ee.go", "//go:build ee\n\npackage fixture\n"+tc.shared)
			var report bytes.Buffer
			if err := generate(dir, []string{"."}, true, &report); err == nil {
				t.Fatal("unsupported dependencies must fail the check")
			}
			if !strings.Contains(report.String(), "UNSUPPORTED") {
				t.Fatalf("unsafe signature dependency not reported: %s", &report)
			}
		})
	}
}

func TestRefuseHandwrittenOutput(t *testing.T) {
	dir := t.TempDir()
	putFile(t, dir, "stub.go", "//go:build !ee\n\npackage fixture\nfunc Hook() {}\n")
	const handwritten = "//go:build ee\n\npackage fixture\n// Maintained by hand.\n"
	name := generatedPrefix + "stub_ee.go"
	putFile(t, dir, name, handwritten)
	var report bytes.Buffer
	if err := generate(dir, []string{"."}, true, &report); err == nil {
		t.Fatal("expected refusal to overwrite handwritten output")
	}
	data, _ := os.ReadFile(filepath.Join(dir, name))
	if string(data) != handwritten {
		t.Fatal("handwritten output was modified")
	}
	if strings.Contains(report.String(), "GENERATED") {
		t.Fatal("reported a fallback that was never written")
	}
}

func TestComplexStubConstraintsAreNotGenerated(t *testing.T) {
	dir := t.TempDir()
	putFile(t, dir, "stub.go", "//go:build !ee && static\n\npackage fixture\nfunc Hook() {}\n")
	var report bytes.Buffer
	if err := generate(dir, []string{"."}, true, &report); err == nil {
		t.Fatal("unsupported constraints must fail the check")
	}
	if !strings.Contains(report.String(), "UNSUPPORTED") {
		t.Fatalf("complex constraint not reported: %s", &report)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatal("generated a fallback with unsupported constraints")
	}
}

func TestFailOnlyAfterAllPackagesAreScannedAndGenerated(t *testing.T) {
	root := t.TempDir()
	for _, pkg := range []string{"first", "second"} {
		dir := filepath.Join(root, pkg)
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
		putFile(t, dir, "stub.go", "//go:build !ee\n\npackage fixture\nfunc Hook() {}\n")
	}
	var report bytes.Buffer
	if err := generate(root, []string{"."}, true, &report); err == nil {
		t.Fatal("missing implementations must fail the check after generation")
	}
	for _, pkg := range []string{"first", "second"} {
		if !strings.Contains(report.String(), "GENERATED "+pkg+".Hook") {
			t.Fatalf("scan stopped before %s: %s", pkg, &report)
		}
		checkEnterpriseFiles(t, filepath.Join(root, pkg), true)
		putFile(t, filepath.Join(root, pkg), "real_ee.go", "//go:build ee\n\npackage fixture\nfunc Hook() { println(\"implemented\") }\n")
	}
	report.Reset()
	if err := generate(root, []string{"."}, true, &report); err != nil {
		t.Fatalf("real implementations should pass: %v", err)
	}
	for _, pkg := range []string{"first", "second"} {
		checkEnterpriseFiles(t, filepath.Join(root, pkg), false)
	}
}

func TestPackageErrorDoesNotHideOtherMissingImplementations(t *testing.T) {
	root := t.TempDir()
	for _, pkg := range []string{"broken", "valid"} {
		if err := os.Mkdir(filepath.Join(root, pkg), 0755); err != nil {
			t.Fatal(err)
		}
	}
	putFile(t, filepath.Join(root, "broken"), "stub.go", "//go:build !ee\n\npackage fixture\nfunc Hook(\n")
	putFile(t, filepath.Join(root, "valid"), "stub.go", "//go:build !ee\n\npackage fixture\nfunc Hook() {}\n")
	var report bytes.Buffer
	if err := generate(root, []string{"."}, true, &report); err == nil {
		t.Fatal("package errors and missing implementations must fail the check")
	}
	if !strings.Contains(report.String(), "ERROR ") || !strings.Contains(report.String(), "GENERATED valid.Hook") {
		t.Fatalf("incomplete report: %s", &report)
	}
	checkEnterpriseFiles(t, filepath.Join(root, "valid"), true)
}

func TestGeneratedFunctionsAreHighlightedBeforeFailure(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	root := t.TempDir()
	dir := filepath.Join(root, "service")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	putFile(t, dir, "stub.go", "//go:build !ee\n\npackage service\nfunc First() {}\nfunc Second() {}\n")
	var report bytes.Buffer
	if err := generate(root, []string{"."}, true, &report); err == nil {
		t.Fatal("generated implementations must fail the check")
	}
	text := report.String()
	failure := strings.Index(text, "Result: FAILED")
	for _, name := range []string{"service.First", "service.Second"} {
		annotation := strings.Index(text, "::error title=Missing EE implementation::"+name)
		if annotation < 0 || annotation > failure {
			t.Fatalf("%s must be annotated before final failure: %s", name, text)
		}
		if !strings.Contains(text, "\x1b[31m  - "+name) {
			t.Fatalf("%s must be shown in red", name)
		}
	}
}
