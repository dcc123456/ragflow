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

//go:build ee

package main

import (
	"os"
	"reflect"
	"testing"
)

// Value-taking long flags accept both space and equals forms.
func TestParseArgsStandardSyntaxMatrix(t *testing.T) {
	cases := []struct {
		flag  string
		value string
	}{
		{"--log-level", "debug"},
		{"--config", "cfg.yaml"},
		{"--deepdoc-device", "cpu"},
	}
	for _, c := range cases {
		long := c.flag
		forms := [][]string{
			{"ragflow_server", "--api", long, c.value},
			{"ragflow_server", "--api", long + "=" + c.value},
		}
		want, err := parseArgsFrom(forms[0])
		if err != nil {
			t.Fatalf("parseArgsFrom(%v) error = %v", forms[0], err)
		}
		for _, form := range forms[1:] {
			got, err := parseArgsFrom(form)
			if err != nil {
				t.Fatalf("parseArgsFrom(%v) error = %v", form, err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("form %v parsed differently from %v:\n got  = %+v\n want = %+v", form, forms[0], got, want)
			}
		}
	}
}

// The config flag exposes -f as its short alias.
func TestParseArgsShortAliases(t *testing.T) {
	forms := [][]string{
		{"ragflow_server", "--api", "--config", "cfg.yaml"},
		{"ragflow_server", "--api", "--config=cfg.yaml"},
		{"ragflow_server", "--api", "-f", "cfg.yaml"},
	}
	want, err := parseArgsFrom(forms[0])
	if err != nil {
		t.Fatalf("parseArgsFrom(%v) error = %v", forms[0], err)
	}
	for _, form := range forms[1:] {
		got, err := parseArgsFrom(form)
		if err != nil {
			t.Fatalf("parseArgsFrom(%v) error = %v", form, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("form %v parsed differently:\n got  = %+v\n want = %+v", form, got, want)
		}
	}
}

// Boolean flags use the bare name to enable and the --no- prefix to disable.
func TestParseArgsBoolFlagForms(t *testing.T) {
	for _, tc := range []struct {
		flag string
		want bool
	}{
		{"--profile", true},
		{"--no-profile", false},
	} {
		args, err := parseArgsFrom([]string{"ragflow_server", "--api", tc.flag})
		if err != nil {
			t.Fatalf("parseArgsFrom(%q) error = %v", tc.flag, err)
		}
		if args.EnablePProf == nil || *args.EnablePProf != tc.want {
			t.Errorf("%q enablePProf = %v, want %v", tc.flag, args.EnablePProf, tc.want)
		}
	}
	for _, form := range []string{"--profile=true", "--profile=false", "--profile=maybe"} {
		if _, err := parseArgsFrom([]string{"ragflow_server", "--api", form}); err == nil {
			t.Errorf("parseArgsFrom(%q) error = nil, want rejection", form)
		}
	}
}

// Mode flags use long names; a disabled mode selects nothing.
func TestParseArgsModeFlags(t *testing.T) {
	for _, mode := range []string{"admin", "api", "ingestor", "syncer", "chatter", "migrate"} {
		args, err := parseArgsFrom([]string{"ragflow_server", "--" + mode})
		if err != nil {
			t.Fatalf("parseArgsFrom(--%s) error = %v", mode, err)
		}
		if args.Mode == nil || *args.Mode != mode {
			t.Errorf("parseArgsFrom(--%s) mode = %v, want %s", mode, args.Mode, mode)
		}
	}

	args, err := parseArgsFrom([]string{"ragflow_server", "--no-api"})
	if err != nil {
		t.Fatalf("--no-api error = %v", err)
	}
	if args.Mode != nil {
		t.Errorf("--no-api mode = %v, want nil", args.Mode)
	}
	// Boolean flags do not accept an equals value.
	for _, form := range []string{"--api=true", "--api=false", "--api=maybe", "-migrate=1.5"} {
		if _, err := parseArgsFrom([]string{"ragflow_server", form}); err == nil {
			t.Errorf("parseArgsFrom(%q) error = nil, want rejection", form)
		}
	}
}

// TestParseArgsDoubleDashTerminator covers the standard "--" terminator: it
// ends flag parsing, and anything after it is positional (rejected here).
func TestParseArgsDoubleDashTerminator(t *testing.T) {
	args, err := parseArgsFrom([]string{"ragflow_server", "--api", "--"})
	if err != nil {
		t.Fatalf("--api -- error = %v", err)
	}
	if args.Mode == nil || *args.Mode != "api" {
		t.Errorf("--api -- mode = %v, want api", args.Mode)
	}

	args, err = parseArgsFrom([]string{"ragflow_server", "--"})
	if err != nil {
		t.Fatalf("bare -- error = %v", err)
	}
	if args.Mode != nil {
		t.Errorf("bare -- mode = %v, want nil", args.Mode)
	}

	for _, argv := range [][]string{
		{"--api", "--", "--port", "80"},
		{"--api", "--", "extra"},
		{"--", "--api"},
	} {
		if _, err := parseArgsFrom(append([]string{"ragflow_server"}, argv...)); err == nil {
			t.Errorf("parseArgsFrom(%v) error = nil, want rejection", argv)
		}
	}
}

// This binary has no positional arguments.
func TestParseArgsPositionalRejected(t *testing.T) {
	for _, argv := range [][]string{
		{"apii"},
		{"--api", "leftover"},
		{"--api", "42"},
		{"--migrate", "--config", "cfg.yaml", "trailing"},
	} {
		if _, err := parseArgsFrom(append([]string{"ragflow_server"}, argv...)); err == nil {
			t.Errorf("parseArgsFrom(%v) error = nil, want rejection", argv)
		}
	}
}

// TestParseArgsUnknownFlagRejected covers unknown long/short flags in every
// spelling, including the equals form and the degenerate "-" and "--=x".
func TestParseArgsUnknownFlagRejected(t *testing.T) {
	for _, argv := range [][]string{
		{"--bogus"},
		{"--bogus=value"},
		{"-bogus"},
		{"-bogus=value"},
		{"-x"},
		{"-"},
		{"--=value"},
		{"--api", "--debug"},
		{"--api", "--enable-mcpserver"},
		{"--api", "--mcp-port=9382"},
		{"--api", "-p", "9380"},
		{"--api", "--init-superuser"},
		{"--ingestor", "--ingestor-tier=low"},
		{"--ingestor", "--deepdoc-inference-concurrency=4"},
		{"--ingestor", "--deepdoc-inference-cpu-cores=2"},
		{"--deepdoc"},
		{"-api"},
		{"-config", "cfg.yaml"},
	} {
		if _, err := parseArgsFrom(append([]string{"ragflow_server"}, argv...)); err == nil {
			t.Errorf("parseArgsFrom(%v) error = nil, want rejection", argv)
		}
	}
}

// TestParseArgsValueEdgeCases covers adversarial values around the "=" split
// and the space form.
func TestParseArgsValueEdgeCases(t *testing.T) {
	// A value containing '=' keeps everything after the first '='.
	args, err := parseArgsFrom([]string{"ragflow_server", "--config=foo=bar"})
	if err != nil {
		t.Fatalf("--config=foo=bar error = %v", err)
	}
	if args.ConfigPath == nil || *args.ConfigPath != "foo=bar" {
		t.Errorf("--config=foo=bar name = %v, want foo=bar", args.ConfigPath)
	}

	// Use the equals form for a value that starts with a dash.
	args, err = parseArgsFrom([]string{"ragflow_server", "--config=--api"})
	if err != nil {
		t.Fatalf("--config=--api error = %v", err)
	}
	if args.ConfigPath == nil || *args.ConfigPath != "--api" {
		t.Errorf("--config=--api name = %v, want --api", args.ConfigPath)
	}
	if args.Mode != nil {
		t.Errorf("--config=--api mode = %v, want nil", args.Mode)
	}

	// An explicitly empty string value is a valid (if odd) string value.
	args, err = parseArgsFrom([]string{"ragflow_server", "--api", "--config="})
	if err != nil {
		t.Fatalf("--config= error = %v", err)
	}
	if args.ConfigPath == nil || *args.ConfigPath != "" {
		t.Errorf("--config= name = %v, want empty string", args.ConfigPath)
	}
	if _, err := parseArgsFrom([]string{"ragflow_server", "--api", "--log-level="}); err == nil {
		t.Error("--log-level= error = nil, want rejection")
	}
}

// Every value-taking flag requires an argument.
func TestParseArgsMissingValue(t *testing.T) {
	valueFlags := []string{
		"--log-level", "--config", "--deepdoc-device", "--port", "-f",
	}
	for _, flag := range valueFlags {
		argv := append([]string{"ragflow_server", "--api"}, flag)
		if _, err := parseArgsFrom(argv); err == nil {
			t.Errorf("parseArgsFrom(%v) error = nil, want rejection", argv)
		}
	}
}

func TestParseArgsDefaults(t *testing.T) {
	args, err := parseArgsFrom([]string{"ragflow_server", "--api"})
	if err != nil {
		t.Fatalf("parseArgsFrom error = %v", err)
	}
	for name, got := range map[string]bool{
		"port":                        args.Port != nil,
		"deepdocInferenceConcurrency": args.DeepdocInferenceConcurrency != nil,
		"deepdocInferenceCPUCores":    args.DeepdocInferenceCPUCores != nil,
	} {
		if got {
			t.Errorf("%s set with no flag on the command line", name)
		}
	}

	if args.LogLevel != nil {
		t.Errorf("logLevel = %v, want nil", args.LogLevel)
	}
	if args.ConfigPath == nil || *args.ConfigPath != "" {
		t.Errorf("configPath = %v, want empty string", args.ConfigPath)
	}
	if args.DeepdocDevice == nil || *args.DeepdocDevice != "" {
		t.Errorf("deepdocDevice = %v, want empty string", args.DeepdocDevice)
	}
	if args.EnablePProf == nil || *args.EnablePProf {
		t.Errorf("enablePProf = %v, want false", args.EnablePProf)
	}
}

// Help and version return before mode selection and server validation.
func TestParseArgsHelpVersion(t *testing.T) {
	for _, form := range []string{"-h", "--help"} {
		args, err := parseArgsFrom([]string{"ragflow_server", "--api", form})
		if err != nil {
			t.Fatalf("parseArgsFrom(%q) error = %v", form, err)
		}
		if args.HelpFlag == nil || !*args.HelpFlag {
			t.Errorf("%q helpFlag = false, want true", form)
		}
		if args.Mode != nil {
			t.Errorf("%q mode = %v, want nil", form, args.Mode)
		}
	}
	for _, form := range []string{"-v", "--version"} {
		args, err := parseArgsFrom([]string{"ragflow_server", form})
		if err != nil {
			t.Fatalf("parseArgsFrom(%q) error = %v", form, err)
		}
		if args.VersionFlag == nil || !*args.VersionFlag {
			t.Errorf("%q versionFlag = false, want true", form)
		}
	}
}

// TestParseArgsDoesNotMutateInput: parseArgsFrom must not rewrite the caller's
// argv slice (main passes os.Args straight through).
func TestParseArgsDoesNotMutateInput(t *testing.T) {
	in := []string{"ragflow_server", "--api", "--log-level=debug", "--config=a=b"}
	want := append([]string(nil), in...)
	if _, err := parseArgsFrom(in); err != nil {
		t.Fatalf("parseArgsFrom error = %v", err)
	}
	if !reflect.DeepEqual(in, want) {
		t.Errorf("parseArgsFrom mutated its input: in = %v, want %v", in, want)
	}
}

// argv includes the program name, matching os.Args in main.
func TestParseArgsNoFlags(t *testing.T) {
	args, err := parseArgsFrom([]string{"ragflow_server"})
	if err != nil {
		t.Fatalf("parseArgsFrom error = %v", err)
	}
	if args.Mode != nil {
		t.Errorf("mode = %v, want nil", args.Mode)
	}
}

// Dash-prefixed string values require the equals form.
func TestParseArgsDashValues(t *testing.T) {
	for _, argv := range [][]string{{"--config", "--api"}, {"--config", "--"}} {
		if _, err := parseArgsFrom(append([]string{"ragflow_server"}, argv...)); err == nil {
			t.Errorf("parseArgsFrom(%v) error = nil, want rejection", argv)
		}
	}
	if _, err := parseArgsFrom([]string{"ragflow_server", "--api", "--log-level", "-x"}); err == nil {
		t.Error("--log-level -x error = nil, want rejection")
	}
	args, err := parseArgsFrom([]string{"ragflow_server", "--config=--"})
	if err != nil {
		t.Fatalf("--config=-- error = %v", err)
	}
	if args.ConfigPath == nil || *args.ConfigPath != "--" {
		t.Errorf("--config=-- name = %v, want --", args.ConfigPath)
	}
}

func TestParseArgsRepeatedFlagRejected(t *testing.T) {
	for _, argv := range [][]string{
		{"--api", "--log-level", "debug", "--log-level", "error"},
		{"--api", "-f", "a.yaml", "--config", "b.yaml"},
	} {
		if _, err := parseArgsFrom(append([]string{"ragflow_server"}, argv...)); err == nil {
			t.Errorf("parseArgsFrom(%v) error = nil, want rejection", argv)
		}
	}
}

// TestParseArgsBoolDoesNotConsumePositional: boolean flags never consume the
// next argument, so a stray token after them is a positional (rejected).
func TestParseArgsBoolDoesNotConsumePositional(t *testing.T) {
	if _, err := parseArgsFrom([]string{"ragflow_server", "--admin", "--profile", "stray"}); err == nil {
		t.Error("stray token after boolean flags error = nil, want rejection")
	}
	args, err := parseArgsFrom([]string{"ragflow_server", "--admin", "--profile"})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if args.EnablePProf == nil || !*args.EnablePProf || !args.InitSuperUser {
		t.Errorf("enablePProf = %v, initSuperUser = %v, want both true", args.EnablePProf, args.InitSuperUser)
	}
}

// TestParseArgsDoesNotMutateOsArgs is the os.Args-level version of the same
// guard, passing os.Args directly as main does.
func TestParseArgsDoesNotMutateOsArgs(t *testing.T) {
	orig := os.Args
	defer func() { os.Args = orig }()
	os.Args = []string{"ragflow_server", "--api", "--log-level=debug"}
	snapshot := append([]string(nil), os.Args...)
	if _, err := parseArgsFrom(os.Args); err != nil {
		t.Fatalf("parseArgs error = %v", err)
	}
	if !reflect.DeepEqual(os.Args, snapshot) {
		t.Errorf("parseArgs mutated os.Args: got %v, want %v", os.Args, snapshot)
	}
}

func TestParseArgsChatterSelection(t *testing.T) {
	args, err := parseArgsFrom([]string{"ragflow_server", "--chatter"})
	if err != nil {
		t.Fatal(err)
	}
	if args.Mode == nil || *args.Mode != "chatter" {
		t.Fatalf("mode = %v, want chatter", args.Mode)
	}
}
