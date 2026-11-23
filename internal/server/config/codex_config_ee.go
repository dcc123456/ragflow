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
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// CodexConfig configures the mode 8 Codex engine: the shared Codex app-server this
// process dials over WebSocket, plus the capability-isolation posture. It does NOT
// carry a model: the model is chosen per thread from the dialog's configuration at
// thread creation (see the mode 8 plan, D4).
type CodexConfig struct {
	// Endpoint is the shared Codex app-server WebSocket URL (ws:// or wss://).
	// Empty disables mode 8 (the engine degrades to the regular pipeline).
	Endpoint string
	// BearerToken authenticates the WebSocket connection.
	BearerToken string
	// MCPPublicBase is the base URL the Codex server uses to reach this process's
	// MCP bridge, e.g. https://ragflow.internal. When empty it is derived from the
	// API server config at request time.
	MCPPublicBase string
	// ApprovalPolicy is the Codex approval policy. Default "untrusted" — the most
	// restrictive usable value. Note "never" means "never ask", not "never run".
	ApprovalPolicy string
	// SandboxMode is the Codex sandbox mode. Default "read-only": mode 8 must only
	// reason and call the MCP retrieval tools, never execute or write. It is applied to
	// every thread. "danger-full-access" is for a deployment whose isolation boundary is
	// the app-server's own container (no corpus mount, no host FS): the inner sandbox
	// would then have nothing left to confine, and requires bwrap/landlock privileges a
	// container usually does not grant.
	SandboxMode string
	// ResponsesProviders is the set of dialog-model provider names (the driver
	// Name(), lower-case) KNOWN to speak the Responses wire API Codex requires.
	// It is a skip-probe fast path: a provider listed here is trusted without any
	// network call. A provider NOT listed is probed at request time for a
	// "/responses" route (see codexResponsesUsable) and falls back to the regular
	// pipeline only when that probe fails. An empty set disables the fast path
	// entirely, so every provider is probed.
	ResponsesProviders []string
	// ReasoningEffort is the fixed Codex reasoning effort mode 8 binds into every
	// thread ("none" | "minimal" | "low" | "medium" | "high", plus the extended
	// "xhigh" | "max" | "ultra" | "persistent"). It is fixed rather than
	// per-conversation because Codex reads it at thread create and does not reload it on
	// resume, so a change rebuilds the thread. Default "high" (the top of the standard
	// ladder); empty leaves Codex's configured default.
	ReasoningEffort string
	// TurnTimeout bounds one turn's wall-clock time.
	TurnTimeout time.Duration
}

const defaultCodexTurnTimeout = 30 * time.Minute

// defaultCodexReasoningEffort is the fixed reasoning effort mode 8 binds into every thread.
// The highest of the standard ladder; the extended values (xhigh/max/ultra/persistent) are
// accepted if an operator sets them, but they are OpenAI-internal and a third-party
// Responses provider may reject them.
const defaultCodexReasoningEffort = "high"

// defaultResponsesProviders are the providers KNOWN to speak the Responses API,
// verified by probing "POST <vendor base>/responses" (a routed-but-unauthorized
// path answers 401/400, never 404). A provider outside this set is not refused:
// it is probed dynamically instead (codexResponsesUsable), so a custom gateway
// (openai-api-compatible) needs no entry here.
//
// Deliberately absent:
//   - "openai-api-compatible": the whole point of dynamic probing; its instance
//     base_url decides, not its provider name.
//   - "siliconflow": a pure OpenAI Chat-Completions gateway; "POST <base>/responses"
//     returns 404, so Codex can never run a turn there.
var defaultResponsesProviders = []string{
	"openai", "azure-openai", "minimax",
	"deepseek", "tongyi-qianwen", "zhipu", "moonshot", "tencent hunyuan",
}

func (c *Config) parseCodexConfig(v *viper.Viper) error {
	// LookupEnv preserves explicitly empty overrides (see parseMCPConfig).
	value := func(key, fallback string) string {
		if value, ok := os.LookupEnv("RAGFLOW_CODEX_" + strings.ToUpper(key)); ok {
			return value
		}
		if v.IsSet("codex." + key) {
			return v.GetString("codex." + key)
		}
		return fallback
	}

	endpoint := strings.TrimSpace(value("endpoint", ""))
	if endpoint != "" && !hasWSScheme(endpoint) {
		return fmt.Errorf("invalid codex endpoint (want ws:// or wss://): %s", endpoint)
	}

	approval := value("approval_policy", "untrusted")
	switch approval {
	case "untrusted", "on-request", "never":
	default:
		return fmt.Errorf("invalid codex approval_policy: %s", approval)
	}

	sandbox := value("sandbox_mode", "read-only")
	switch sandbox {
	case "read-only", "workspace-write", "danger-full-access":
	default:
		return fmt.Errorf("invalid codex sandbox_mode: %s", sandbox)
	}

	timeoutSeconds, err := strconv.Atoi(value("turn_timeout", strconv.Itoa(int(defaultCodexTurnTimeout.Seconds()))))
	if err != nil || timeoutSeconds <= 0 {
		return fmt.Errorf("invalid codex turn_timeout: %s", value("turn_timeout", ""))
	}

	effort := strings.ToLower(strings.TrimSpace(value("reasoning_effort", defaultCodexReasoningEffort)))
	switch effort {
	case "", "none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra", "persistent":
	default:
		return fmt.Errorf("invalid codex reasoning_effort: %s", effort)
	}

	c.apiServer.Codex = CodexConfig{
		Endpoint:           endpoint,
		BearerToken:        value("bearer_token", ""),
		MCPPublicBase:      strings.TrimRight(value("mcp_public_base", ""), "/"),
		ApprovalPolicy:     approval,
		SandboxMode:        sandbox,
		ResponsesProviders: parseResponsesProviders(value("responses_providers", strings.Join(defaultResponsesProviders, ","))),
		ReasoningEffort:    effort,
		TurnTimeout:        time.Duration(timeoutSeconds) * time.Second,
	}
	return nil
}

// parseResponsesProviders splits a comma-separated known-capable provider set,
// lower-casing and dropping blanks. An empty result means "no provider is known
// capable", so every provider is resolved by the dynamic probe instead.
func parseResponsesProviders(raw string) []string {
	out := make([]string, 0, 4)
	for _, p := range strings.Split(raw, ",") {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func hasWSScheme(endpoint string) bool {
	return strings.HasPrefix(endpoint, "ws://") || strings.HasPrefix(endpoint, "wss://")
}
