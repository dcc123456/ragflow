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
	"encoding/json"
	"testing"
)

func TestMessagesToItems(t *testing.T) {
	items := MessagesToItems([]map[string]interface{}{
		{"role": "system", "content": "ignored"},
		{"role": "user", "content": "hello"},
		{"role": "assistant", "content": "hi there"},
		{"role": "user", "content": "   "},
		{"role": "tool", "content": "dropped"},
	})
	if len(items) != 2 {
		t.Fatalf("want 2 items (user+assistant), got %d: %s", len(items), items)
	}
	var first map[string]interface{}
	if err := json.Unmarshal(items[0], &first); err != nil {
		t.Fatal(err)
	}
	if first["type"] != "message" || first["role"] != "user" {
		t.Fatalf("user item shape: %v", first)
	}
	content, _ := first["content"].([]interface{})
	part, _ := content[0].(map[string]interface{})
	if part["type"] != "input_text" || part["text"] != "hello" {
		t.Fatalf("user content part: %v", part)
	}

	var second map[string]interface{}
	_ = json.Unmarshal(items[1], &second)
	content2, _ := second["content"].([]interface{})
	part2, _ := content2[0].(map[string]interface{})
	if part2["type"] != "output_text" || part2["text"] != "hi there" {
		t.Fatalf("assistant content part: %v", part2)
	}
}

func TestScopeFingerprint(t *testing.T) {
	a := ScopeFingerprint("t1", []string{"kb1", "kb2"})
	b := ScopeFingerprint("t1", []string{"kb2", "kb1"})
	if a != b {
		t.Fatal("scope fingerprint must be order-independent")
	}
	if a == ScopeFingerprint("t2", []string{"kb1", "kb2"}) {
		t.Fatal("different tenant must change the fingerprint")
	}
	if a == ScopeFingerprint("t1", []string{"kb1"}) {
		t.Fatal("removing a KB must change the fingerprint")
	}
	if ScopeFingerprint("t1", nil) == ScopeFingerprint("t1", []string{"kb1"}) {
		t.Fatal("no-KB must differ from one-KB")
	}
}

// The web-search decision is part of the thread's reuse key because Codex caches the MCP
// tool catalog for a thread's life: a toggle that did not rebuild the thread would never
// change the advertised tool set (see the mode 8 plan, §5.7③).
func TestWebSearchFingerprint(t *testing.T) {
	if WebSearchFingerprint(true) == WebSearchFingerprint(false) {
		t.Fatal("web-search on and off must fingerprint differently")
	}
	if WebSearchFingerprint(true) != WebSearchFingerprint(true) {
		t.Fatal("web-search fingerprint must be stable")
	}
	base := ScopeFingerprint("t1", []string{"kb1"})
	if CombineFingerprints(base, WebSearchFingerprint(false)) == CombineFingerprints(base, WebSearchFingerprint(true)) {
		t.Fatal("flipping web search must change the composed reuse key")
	}
}

// The hosted-tools policy is baked into a thread at create and resume does NOT reload it
// (measured), so the policy marker must participate in the reuse key: a version bump has
// to force existing threads to rebuild rather than keep a server-side tool we disabled.
func TestHostedToolsFingerprintChangesReuseKey(t *testing.T) {
	if HostedToolsFingerprint() == "" {
		t.Fatal("hosted-tools policy marker must not be empty")
	}
	if HostedToolsFingerprint() != HostedToolsFingerprint() {
		t.Fatal("hosted-tools marker must be stable")
	}
	base := ScopeFingerprint("t1", []string{"kb1"})
	withPolicy := CombineFingerprints(base, HostedToolsFingerprint())
	// The pre-policy key (what a stored row from before this change holds) must differ,
	// or those threads would resume with the hosted tool still enabled.
	if withPolicy == CombineFingerprints(base, "hosted_tools=none-v0") {
		t.Fatal("a policy version bump must change the composed reuse key")
	}
	if withPolicy == CombineFingerprints(base, WebSearchFingerprint(false)) {
		t.Fatal("the policy marker must contribute to the key, not be dropped")
	}
}

func TestHistoryFingerprintDetectsDrift(t *testing.T) {
	base := []map[string]interface{}{
		{"role": "user", "content": "q1"},
		{"role": "assistant", "content": "a1"},
	}
	same := []map[string]interface{}{
		{"role": "user", "content": "q1"},
		{"role": "assistant", "content": "a1"},
	}
	if HistoryFingerprint(base) != HistoryFingerprint(same) {
		t.Fatal("identical histories must hash equally")
	}
	// A deleted message (as DeleteSessionMessage would produce) must drift.
	deleted := []map[string]interface{}{{"role": "assistant", "content": "a1"}}
	if HistoryFingerprint(base) == HistoryFingerprint(deleted) {
		t.Fatal("a removed message must change the fingerprint")
	}
	edited := []map[string]interface{}{
		{"role": "user", "content": "q1"},
		{"role": "assistant", "content": "edited"},
	}
	if HistoryFingerprint(base) == HistoryFingerprint(edited) {
		t.Fatal("an edited message must change the fingerprint")
	}
}

func TestHistoryWithoutCurrent(t *testing.T) {
	if HistoryWithoutCurrent(nil) != nil {
		t.Fatal("nil history stays nil")
	}
	msgs := []map[string]interface{}{
		{"role": "user", "content": "old"},
		{"role": "assistant", "content": "old-a"},
		{"role": "user", "content": "current"},
	}
	got := HistoryWithoutCurrent(msgs)
	if len(got) != 2 || got[1]["content"] != "old-a" {
		t.Fatalf("HistoryWithoutCurrent = %v", got)
	}
}

// TestModelParamsFingerprintDistinguishesParams pins that the model name, the model window,
// the auto-compaction limit and the reasoning effort are part of the thread's reuse key.
// They are baked into the thread config at create and thread/resume does not reload them, so
// a change (a different dialog model, a corrected window, a different effort) must force a
// rebuild — otherwise the thread keeps the old value forever.
func TestModelParamsFingerprintDistinguishesParams(t *testing.T) {
	base := ModelParamsFingerprint("MiniMax-M3", 1024000, 892928, "high")
	if base == ModelParamsFingerprint("other-model", 1024000, 892928, "high") {
		t.Fatal("a different model name must change the fingerprint")
	}
	if base == ModelParamsFingerprint("MiniMax-M3", 128000, 892928, "high") {
		t.Fatal("a different context window must change the fingerprint")
	}
	if base == ModelParamsFingerprint("MiniMax-M3", 1024000, 400000, "high") {
		t.Fatal("a different auto-compaction limit must change the fingerprint")
	}
	if base == ModelParamsFingerprint("MiniMax-M3", 1024000, 892928, "low") {
		t.Fatal("a different reasoning effort must change the fingerprint")
	}
	if base == ModelParamsFingerprint("", 0, 0, "") {
		t.Fatal("resolving a window where none existed must change the fingerprint")
	}
	if base != ModelParamsFingerprint("MiniMax-M3", 1024000, 892928, "high") {
		t.Fatal("the fingerprint must be stable for identical params")
	}
}

// TestInstructionsPolicyFingerprintIsStable pins that the instruction/tool policy marker is
// a stable, non-empty component of the reuse key. It is folded unconditionally so that a
// policy change (delivering the contract as developer instructions, enabling the plan tool)
// rebuilds threads built under the previous policy exactly once.
func TestInstructionsPolicyFingerprintIsStable(t *testing.T) {
	policy := InstructionsPolicyFingerprint()
	if policy == "" {
		t.Fatal("the instructions policy marker must not be empty")
	}
	if policy != InstructionsPolicyFingerprint() {
		t.Fatal("the policy marker must be stable")
	}
}
