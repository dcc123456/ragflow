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

package config

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func clearCodexEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"ENDPOINT", "BEARER_TOKEN", "MCP_PUBLIC_BASE", "APPROVAL_POLICY", "SANDBOX_MODE", "TURN_TIMEOUT", "RESPONSES_PROVIDERS", "REASONING_EFFORT"} {
		name := "RAGFLOW_CODEX_" + key
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
}

func parseCodexYAML(t *testing.T, text string) (CodexConfig, error) {
	t.Helper()
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetEnvPrefix("RAGFLOW")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	if err := v.ReadConfig(strings.NewReader(text)); err != nil {
		t.Fatal(err)
	}
	var cfg Config
	err := cfg.ParseAPIServerConfig(v)
	return cfg.GetAPIServerConfig().Codex, err
}

func TestCodexConfigurationDefaults(t *testing.T) {
	clearCodexEnv(t)
	got, err := parseCodexYAML(t, "{}")
	if err != nil {
		t.Fatalf("defaults: %v", err)
	}
	if got.Endpoint != "" || got.BearerToken != "" || got.MCPPublicBase != "" {
		t.Fatalf("unexpected default connection fields: %+v", got)
	}
	if got.ApprovalPolicy != "untrusted" {
		t.Fatalf("approval_policy default = %q, want untrusted (most restrictive)", got.ApprovalPolicy)
	}
	if got.SandboxMode != "read-only" {
		t.Fatalf("sandbox_mode default = %q, want read-only", got.SandboxMode)
	}
	if got.TurnTimeout != defaultCodexTurnTimeout {
		t.Fatalf("turn timeout default = %v, want %v", got.TurnTimeout, defaultCodexTurnTimeout)
	}
	if len(got.ResponsesProviders) == 0 || got.ResponsesProviders[0] != "openai" {
		t.Fatalf("responses_providers default = %v, want a non-empty known-capable set including openai", got.ResponsesProviders)
	}
	// The default set is the maintained known-capable list. A gateway that must be probed
	// rather than trusted (openai-api-compatible) and a provider with no Responses route
	// (siliconflow) must NOT be in it.
	has := func(name string) bool {
		for _, p := range got.ResponsesProviders {
			if p == name {
				return true
			}
		}
		return false
	}
	for _, want := range []string{"minimax", "deepseek", "tongyi-qianwen", "zhipu", "moonshot", "tencent hunyuan"} {
		if !has(want) {
			t.Fatalf("responses_providers default = %v, want it to contain %q", got.ResponsesProviders, want)
		}
	}
	for _, absent := range []string{"openai-api-compatible", "siliconflow"} {
		if has(absent) {
			t.Fatalf("responses_providers default = %v, must not contain %q", got.ResponsesProviders, absent)
		}
	}
}

func TestCodexResponsesProvidersParsing(t *testing.T) {
	clearCodexEnv(t)
	got, err := parseCodexYAML(t, "codex: {responses_providers: 'OpenAI, Minimax ,'}")
	if err != nil {
		t.Fatalf("responses_providers: %v", err)
	}
	if len(got.ResponsesProviders) != 2 || got.ResponsesProviders[0] != "openai" || got.ResponsesProviders[1] != "minimax" {
		t.Fatalf("provider allowlist = %v, want [openai minimax]", got.ResponsesProviders)
	}
}

func TestCodexConfigurationFromYAML(t *testing.T) {
	clearCodexEnv(t)
	yaml := `codex:
  endpoint: wss://codex.internal/ws
  bearer_token: secret
  mcp_public_base: https://ragflow.internal/
  approval_policy: never
  sandbox_mode: workspace-write
  turn_timeout: 120
`
	got, err := parseCodexYAML(t, yaml)
	if err != nil {
		t.Fatalf("yaml: %v", err)
	}
	if got.Endpoint != "wss://codex.internal/ws" || got.BearerToken != "secret" {
		t.Fatalf("connection fields: %+v", got)
	}
	if got.MCPPublicBase != "https://ragflow.internal" {
		t.Fatalf("mcp_public_base should be trailing-slash trimmed: %q", got.MCPPublicBase)
	}
	if got.ApprovalPolicy != "never" || got.SandboxMode != "workspace-write" {
		t.Fatalf("isolation fields: %+v", got)
	}
	if got.TurnTimeout != 120*time.Second {
		t.Fatalf("turn timeout = %v, want 120s", got.TurnTimeout)
	}
}

func TestCodexEnvironmentOverridesYAML(t *testing.T) {
	clearCodexEnv(t)
	overrides := map[string]string{
		"ENDPOINT":        "ws://env-codex:1455",
		"BEARER_TOKEN":    "env-secret",
		"MCP_PUBLIC_BASE": "https://env.example/",
		"APPROVAL_POLICY": "on-request",
		"SANDBOX_MODE":    "read-only",
		"TURN_TIMEOUT":    "90",
	}
	for k, v := range overrides {
		t.Setenv("RAGFLOW_CODEX_"+k, v)
	}
	got, err := parseCodexYAML(t, `codex: {endpoint: wss://file.example, turn_timeout: 1}`)
	if err != nil {
		t.Fatalf("env override: %v", err)
	}
	if got.Endpoint != "ws://env-codex:1455" || got.ApprovalPolicy != "on-request" || got.TurnTimeout != 90*time.Second {
		t.Fatalf("env did not win over yaml: %+v", got)
	}
}

func TestCodexValidation(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{"bad endpoint scheme", "codex: {endpoint: 'http://codex.internal/mcp'}"},
		{"bad approval", "codex: {approval_policy: always}"},
		{"bad sandbox", "codex: {sandbox_mode: 'full-auto'}"},
		{"zero timeout", "codex: {turn_timeout: 0}"},
		{"negative timeout", "codex: {turn_timeout: -1}"},
		{"non-numeric timeout", "codex: {turn_timeout: soon}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearCodexEnv(t)
			if _, err := parseCodexYAML(t, tc.yaml); err == nil {
				t.Fatal("invalid codex configuration accepted")
			}
		})
	}
}

func TestCodexDisabledWhenEndpointEmpty(t *testing.T) {
	clearCodexEnv(t)
	got, err := parseCodexYAML(t, `codex: {approval_policy: untrusted}`)
	if err != nil {
		t.Fatalf("empty endpoint must be valid (mode 8 disabled): %v", err)
	}
	if got.Endpoint != "" {
		t.Fatalf("endpoint = %q, want empty", got.Endpoint)
	}
}
