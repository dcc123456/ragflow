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

package codexagent

import (
	"context"
	"encoding/json"

	codexgo "github.com/yuzhichang/codex-app-server-go"
)

// approvalKindMcpToolCall is codex's `_meta.codex_approval_kind` marker on an elicitation
// that is really an MCP tool-call approval request.
const approvalKindMcpToolCall = "mcp_tool_call"

// controlledMCPServer is the only MCP server whose tool calls mode 8 auto-approves. Other
// servers merged in from the shared Codex's own config are NOT approved: their side
// effects are outside this process's read-only sandbox.
const controlledMCPServer = "ragflow"

// Dispatcher is mode 8's server-request posture. Verified against codex 0.162.0: an MCP
// tool call is gated by an `mcpServer/elicitation/request` whose `_meta.codex_approval_kind`
// is "mcp_tool_call" (message: "Allow the <server> MCP server to run tool <name>?"). With no
// handler codex declines it and the tool never runs — which silently kills the whole
// retrieval path.
//
// mode 8 therefore ACCEPTS that specific elicitation (the retrieval tools must run) and
// leaves every other axis at the SDK's default refusal: permissions and any other
// elicitation are declined, and so are command/file approvals IF one is ever asked. Note
// the last clause is defense in depth, not the active gate: mode 8 runs ApprovalNever, so
// codex does not prompt for exec/file at all — it allows non-dangerous commands and lets
// the read-only, network-isolated sandbox bound them (a local script for arithmetic is
// wanted; writes and the web are not). This handler simply guarantees that a stray approval
// request can never be auto-granted.
func Dispatcher() *codexgo.Dispatcher {
	return &codexgo.Dispatcher{Elicitation: mcpToolApproval{}}
}

type mcpToolApproval struct{}

func (mcpToolApproval) HandleElicitation(_ context.Context, req codexgo.McpServerElicitationRequestParams) (codexgo.McpServerElicitationRequestResponse, error) {
	if req.ServerName == controlledMCPServer && isMcpToolCallApproval(req.Meta) {
		return codexgo.McpServerElicitationRequestResponse{
			Action:  codexgo.McpServerElicitationActionAccept,
			Content: json.RawMessage(`{}`),
		}, nil
	}
	return codexgo.McpServerElicitationRequestResponse{Action: codexgo.McpServerElicitationActionDecline}, nil
}

func isMcpToolCallApproval(meta json.RawMessage) bool {
	if len(meta) == 0 {
		return false
	}
	var m struct {
		Kind string `json:"codex_approval_kind"`
	}
	if err := json.Unmarshal(meta, &m); err != nil {
		return false
	}
	return m.Kind == approvalKindMcpToolCall
}
