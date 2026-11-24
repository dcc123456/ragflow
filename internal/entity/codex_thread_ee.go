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

package entity

// CodexThreadMap maps a RAGFlow conversation session to the Codex thread that carries
// its model-side state (mode 8). It is the persistent form of codexagent.ThreadRecord:
// the scope and history fingerprints decide whether a turn may reuse the thread or must
// rebuild it (see docs/develop/mode-8-codex-mcp-agent.md, §5.2).
type CodexThreadMap struct {
	TenantID  string `gorm:"column:tenant_id;primaryKey;size:64" json:"tenant_id"`
	SessionID string `gorm:"column:session_id;primaryKey;size:64" json:"session_id"`
	ThreadID  string `gorm:"column:thread_id;size:64;not null" json:"thread_id"`
	// ScopeFingerprint hashes the tenant + KB set baked into the thread's MCP URL; a
	// change forces a rebuild because thread/resume does not reload mcp_servers.
	ScopeFingerprint string `gorm:"column:scope_fingerprint;size:64;not null" json:"scope_fingerprint"`
	// HistoryFingerprint hashes the history the thread has already seen, so a deleted
	// or edited message (drift from RAGFlow's truth) forces a rebuild + replay.
	HistoryFingerprint string `gorm:"column:history_fingerprint;size:64;not null" json:"history_fingerprint"`
	// Ticket is the MCP ticket token baked into the thread, kept for revocation.
	Ticket     string `gorm:"column:ticket;size:512" json:"ticket"`
	CreateTime int64  `gorm:"column:create_time" json:"create_time"`
	UpdateTime int64  `gorm:"column:update_time" json:"update_time"`
}

func (CodexThreadMap) TableName() string {
	return "codex_thread_map"
}
