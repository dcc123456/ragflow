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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// citationMarkerRe matches the backend-assigned [ID:N] citation markers the chat service
// inserts into the final answer before it is persisted. The service inserts them as
// " [ID:N]" (leading space), so the optional whitespace is part of the match — otherwise
// "chunk_id: c1" and "chunk_id: c1 [ID:0]" would still hash differently.
var citationMarkerRe = regexp.MustCompile(`\s*\[ID:\d+\]`)

// MessagesToItems converts RAGFlow conversation messages ({role, content}) into the raw
// Responses API items Codex accepts via thread/inject_items.
//
// system messages are skipped: Codex carries its instructions on the thread, not as
// injected history. Blank-content messages are dropped (an empty input_text item is
// rejected upstream). The caller passes the history WITHOUT the current user message —
// that one is the turn input, not a replay.
func MessagesToItems(messages []map[string]interface{}) []json.RawMessage {
	items := make([]json.RawMessage, 0, len(messages))
	for _, msg := range messages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}
		var item map[string]interface{}
		switch role {
		case "user":
			item = responsesMessageItem("user", "input_text", content)
		case "assistant":
			item = responsesMessageItem("assistant", "output_text", content)
		default:
			continue
		}
		if raw, err := json.Marshal(item); err == nil {
			items = append(items, raw)
		}
	}
	return items
}

func responsesMessageItem(role, textType, text string) map[string]interface{} {
	return map[string]interface{}{
		"type": "message",
		"role": role,
		"content": []map[string]interface{}{
			{"type": textType, "text": text},
		},
	}
}

// ScopeFingerprint is a stable hash of the tenant + dataset scope. A change (KB added,
// removed or rebound, or tenant changed) means the thread's baked-in MCP URL no longer
// matches the conversation and the thread must be rebuilt.
func ScopeFingerprint(tenantID string, datasetIDs []string) string {
	sorted := append([]string(nil), datasetIDs...)
	sort.Strings(sorted)
	h := sha256.New()
	h.Write([]byte(tenantID))
	h.Write([]byte{0})
	h.Write([]byte(strings.Join(sorted, "\x1f")))
	return hex.EncodeToString(h.Sum(nil))
}

// HistoryFingerprint is a stable hash of the visible conversation history. It detects
// when the thread's history has drifted from RAGFlow's (a deleted message, a turn that
// ran on a different engine, an out-of-band edit), which requires a rebuild + replay.
//
// Backend-assigned [ID:N] citation markers are stripped first: the chat service persists
// the decorated answer (raw answer + markers), while a fresh turn records the raw answer,
// so without this a citation-bearing conversation would look drifted and rebuild forever.
func HistoryFingerprint(messages []map[string]interface{}) string {
	h := sha256.New()
	for _, msg := range messages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		h.Write([]byte(role))
		h.Write([]byte{0})
		h.Write([]byte(citationMarkerRe.ReplaceAllString(content, "")))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// WebSearchFingerprint is the scope-key component for a turn's web-search decision.
// Codex caches an MCP tool catalog for the life of a thread, so the decision is part of
// the thread's identity: turning the conversation's web search on or off must rebuild
// the thread for the bridge's new tool set to be advertised (see the mode 8 plan, §5.7③).
func WebSearchFingerprint(on bool) string {
	if on {
		return "web_search=on"
	}
	return "web_search=off"
}

// HostedToolsFingerprint is the scope-key component for the set of provider-hosted
// (server-side) tools mode 8 leaves enabled. Codex bakes the tool list into a thread at
// create, and thread/resume does NOT reload it — measured against a mock provider, where
// a RESUMED thread kept the provider's `web_search` tool despite an override that
// disables it on a fresh thread. So a policy change must rebuild existing threads;
// folding this constant into the reuse key does that exactly once per change, and a
// thread created afterwards carries no hosted tool at all (they are all disabled; see
// Runner.threadOptions). Bump the suffix whenever the policy changes.
const hostedToolsPolicy = "hosted_tools=none-v1"

// HostedToolsFingerprint returns the current hosted-tools policy marker (see
// hostedToolsPolicy).
func HostedToolsFingerprint() string { return hostedToolsPolicy }

// CombineFingerprints folds several fingerprints into one 64-char hash, so a fingerprint
// that composes other fingerprints still fits a size:64 column.
func CombineFingerprints(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// HistoryWithoutCurrent returns the messages excluding the final one, which the caller
// treats as the current turn input. It is the history a fresh thread must be seeded with.
func HistoryWithoutCurrent(messages []map[string]interface{}) []map[string]interface{} {
	if len(messages) == 0 {
		return nil
	}
	return messages[:len(messages)-1]
}

// InstructionsFingerprint is a stable hash of a thread's citation contract. It is folded
// into the scope fingerprint because the contract is baked into the thread at create time
// and thread/resume does not reload it: a change (e.g. toggling the citation contract) must
// force a rebuild.
func InstructionsFingerprint(instructions string) string {
	sum := sha256.Sum256([]byte(instructions))
	return hex.EncodeToString(sum[:])
}

// instructionsPolicy marks HOW the citation contract is delivered and which Codex tools
// mode 8 enables — both baked into a thread at create and not reloaded on resume:
//   - the contract rides DEVELOPER instructions (additive), never base instructions, so
//     Codex keeps its own agentic prompt (planning / verification / answer shape);
//   - the plan tool (`tools.update_plan`) is enabled;
//   - the approval policy is `on-request`: a restricted (read-only) sandbox lets codex run
//     a non-escalated, non-dangerous command without prompting (local computation works),
//     while the elicitation path stays alive for MCP tool calls (which `never` would deny);
//   - Codex memory + its dedicated tools are enabled (`features.memories`,
//     `memories.dedicated_tools`) — note memory is a single store per CODEX_HOME, so on a
//     shared app-server this is cross-tenant (see docs §5.5);
//   - extra default-off capabilities are on: `features.current_time_reminder`,
//     `features.sleep_tool` (mode always_on) and `features.multi_agent_v2` (+ dynamic
//     tools), with the spawn tool's model/reasoning overrides hidden so a child inherits
//     the thread's model. `features.token_budget` is deliberately NOT among them: it
//     replaces auto-compaction with a summarization-free window reset, which drops the
//     question from history.
//   - a delegated sub-agent gets its own developer fragment
//     (`features.multi_agent_v2.subagent_developer_instructions`) instead of inheriting the
//     root's contract — a create-time config value, so a change rebuilds the thread.
//
// Bump the suffix whenever any of these changes, so threads built under the old policy
// rebuild exactly once (the same device as hostedToolsPolicy).
const instructionsPolicy = "instructions=developer-v1;update_plan=on-v1;approval=on-request-v1;memory=on-v1;extras=clock,sleep,agents-v4"

// InstructionsPolicyFingerprint returns the current instruction/tool policy marker (see
// instructionsPolicy).
func InstructionsPolicyFingerprint() string { return instructionsPolicy }

// ModelParamsFingerprint is the scope-key component for the config mode 8 bakes into a
// thread at create: the dialog model name, the model context window and the
// auto-compaction limit (which decide when Codex compacts) and the reasoning effort. Codex
// reads all of these from config at thread create and does NOT reload them on
// thread/resume — the same reason the hosted-tool policy and the base instructions are
// folded in — so a change must rebuild the thread.
//
// The model name is here because it is baked into the thread config (to seed fresh
// sub-agents, see Runner.threadOptions): switching a conversation's dialog model must
// rebuild the thread so the new name reaches a fresh child.
//
// An unknown model gets Codex's fallback metadata (context_window = None, no auto-compaction);
// passing the dialog model's real window is what makes a full-history rebuild safe over the
// window. Zero means "not resolved, leave Codex's own value" and is part of the key so
// gaining/losing a resolved value also rebuilds.
func ModelParamsFingerprint(model string, contextWindow, autoCompactLimit int, reasoningEffort string) string {
	return fmt.Sprintf("model=%s;ctx=%d;auto_compact=%d;effort=%s", model, contextWindow, autoCompactLimit, reasoningEffort)
}
