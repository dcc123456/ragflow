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
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"ragflow/internal/server/bootstrap"
	"strings"
	"testing"
	"time"
)

func parseArgsForTest(t *testing.T, argv ...string) (*bootstrap.ServerArgs, error) {
	t.Helper()
	orig := os.Args
	os.Args = append([]string{"ragflow_server"}, argv...)
	defer func() { os.Args = orig }()
	return parseArgsFrom(os.Args)
}

func TestParseArgsMigrateSelectsMode(t *testing.T) {
	args, err := parseArgsForTest(t, "--migrate")
	if err != nil {
		t.Fatalf("parseArgs(--migrate) error = %v", err)
	}
	if args.Mode == nil || *args.Mode != "migrate" {
		t.Fatalf("mode = %v, want migrate", args.Mode)
	}
}

func TestParseArgsLogLevel(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error"} {
		for _, argv := range [][]string{{"--api", "--log-level", level}, {"--migrate", "--log-level=" + level}} {
			args, err := parseArgsForTest(t, argv...)
			if err != nil {
				t.Fatalf("parseArgs(%v) error = %v", argv, err)
			}
			if got := bootstrap.SelectedLogLevel(args, "info"); got != level {
				t.Errorf("selectedLogLevel(%v) = %q, want %q", argv, got, level)
			}
		}
	}

	for _, argv := range [][]string{{"--api", "--log-level"}, {"--api", "--log-level=trace"}, {"--api", "--log-level", "fatal"}} {
		if _, err := parseArgsForTest(t, argv...); err == nil {
			t.Errorf("parseArgs(%v) error = nil, want error", argv)
		}
	}
}

func TestSelectedLogLevelPrecedence(t *testing.T) {
	args, err := parseArgsForTest(t, "--api")
	if err != nil {
		t.Fatalf("parseArgs(--api) error = %v", err)
	}
	if got := bootstrap.SelectedLogLevel(args, ""); got != "warn" {
		t.Errorf("default log level = %q, want warn", got)
	}
	if got := bootstrap.SelectedLogLevel(args, "info"); got != "info" {
		t.Errorf("configured log level = %q, want info", got)
	}

	args, err = parseArgsForTest(t, "--api", "--log-level", "error")
	if err != nil {
		t.Fatalf("parseArgs with --log-level error = %v", err)
	}
	if got := bootstrap.SelectedLogLevel(args, "info"); got != "error" {
		t.Errorf("--log-level log level = %q, want error", got)
	}
	if _, err = parseArgsForTest(t, "--api", "--debug"); err == nil {
		t.Error("parseArgs(--api --debug) error = nil, want rejection")
	} else if !strings.Contains(err.Error(), "unknown long flag '--debug'") {
		t.Errorf("parseArgs(--api --debug) error = %q, want unknown long flag", err)
	}
}

func TestParseArgsMigrateConflictsWithServerMode(t *testing.T) {
	for _, mode := range []string{"--api", "--admin", "--ingestor", "--syncer"} {
		for _, argv := range [][]string{{mode, "--migrate"}, {"--migrate", mode}} {
			if _, err := parseArgsForTest(t, argv...); err == nil || !strings.Contains(err.Error(), "only one mode flag") {
				t.Errorf("parseArgs(%v) error = %v, want conflicting modes", argv, err)
			}
		}
	}
}

func TestParseArgsKeepsMCPOutOfCLI(t *testing.T) {
	t.Setenv("RAGFLOW_MCP_PORT", "invalid")
	for _, mode := range []string{"--api", "--admin", "--ingestor", "--syncer", "--migrate", "--help", "--version"} {
		if _, err := parseArgsForTest(t, mode); err != nil {
			t.Fatalf("%s depends on MCP environment: %v", mode, err)
		}
	}
	for _, flag := range []string{"--enable-mcpserver", "--mcp-host=localhost", "--mcp-port=9382", "--mcp-mode=host", "--mcp-host-api-key=unused", "--transport-sse-enabled", "--no-transport-sse-enabled", "--transport-streamable-http-enabled", "--no-transport-streamable-http-enabled", "--json-response", "--no-json-response"} {
		if _, err := parseArgsForTest(t, "--api", flag); err == nil {
			t.Fatalf("obsolete MCP flag accepted: %s", flag)
		}
	}
}

func TestMainMigrateReachesConfiguration(t *testing.T) {
	if os.Getenv("RAGFLOW_TEST_MIGRATE_MAIN") == "1" {
		os.Args = append([]string{os.Args[0]}, os.Args[len(os.Args)-3:]...)
		main()
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	dir := t.TempDir()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMainMigrateReachesConfiguration$", "--", "--migrate", "--config", filepath.Join(dir, "missing.yaml"))
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "RAGFLOW_TEST_MIGRATE_MAIN=1")
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "initialize configuration") || strings.Contains(string(output), "Usage:") {
		t.Fatalf("migration did not reach config: %v\n%s", err, output)
	}
}
