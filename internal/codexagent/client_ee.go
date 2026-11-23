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

// Package codexagent is the mode 8 engine: it brokers a shared Codex app-server for a
// conversation and lets Codex call this process's agent_rag retrieval tools over MCP.
//
// See docs/develop/mode-8-codex-mcp-agent.md for the full design.

//go:build ee

package codexagent

import (
	"context"
	"fmt"

	codexgo "github.com/yuzhichang/codex-app-server-go"
)

// ClientConfig is the connection half of the server's CodexConfig.
type ClientConfig struct {
	// Endpoint is the shared Codex app-server WebSocket URL (ws:// or wss://).
	Endpoint string
	// BearerToken authenticates the WebSocket connection.
	BearerToken string
}

// Dial connects to the shared Codex app-server over WebSocket and enables the SDK's
// auto-reconnect supervisor. The returned client is long-lived: threads are resumed on
// it across turns and sessions, so it must be closed only at process shutdown.
func Dial(ctx context.Context, cfg ClientConfig) (*codexgo.Client, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("codexagent: no endpoint configured")
	}
	// Order matters: WithReconnectingWSTransport consumes the auth headers when it
	// builds the transport, so the bearer must be registered BEFORE it.
	opts := make([]codexgo.Option, 0, 3)
	if cfg.BearerToken != "" {
		opts = append(opts, codexgo.WithWSBearerToken(cfg.BearerToken))
	}
	opts = append(opts,
		codexgo.WithReconnectingWSTransport(ctx, cfg.Endpoint),
		codexgo.WithAutoReconnect(),
		// MCP tool calls are gated by an elicitation request (see approval.go); without
		// this handler codex declines them and mode 8's retrieval tools never run.
		codexgo.WithRequestHandler(Dispatcher()),
	)
	return codexgo.New(opts...)
}
