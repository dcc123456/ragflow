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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	codexgo "github.com/yuzhichang/codex-app-server-go"

	"ragflow/internal/retrievalaccount"
)

// ThreadRecord is the persisted mapping from a RAGFlow session to a Codex thread.
type ThreadRecord struct {
	TenantID           string
	SessionID          string
	ThreadID           string
	ScopeFingerprint   string
	HistoryFingerprint string
	// Ticket is the stable per-session MCP handle baked into this thread's config, kept
	// so a reuse can check it is still alive and a session delete can revoke it.
	Ticket string
}

// ThreadStore persists session -> thread mappings. A nil store is allowed for
// ephemeral-only use (a session id with no store always runs a one-shot thread).
type ThreadStore interface {
	Get(ctx context.Context, tenantID, sessionID string) (ThreadRecord, bool, error)
	Put(ctx context.Context, rec ThreadRecord) error
	Delete(ctx context.Context, tenantID, sessionID string) error
}

// TurnRequest is one mode 8 turn.
type TurnRequest struct {
	TenantID  string
	SessionID string // empty => ephemeral one-shot thread (no persistence)
	// DatasetIDs is the conversation's current KB scope; it selects the ticket and
	// is folded into the scope fingerprint.
	DatasetIDs []string
	// WebSearch is this turn's web-search decision (the chat has a provider AND the
	// request enabled internet). It is part of the thread's reuse key: Codex caches
	// the MCP tool catalog for a thread's life, so flipping web search must rebuild
	// the thread — otherwise the bridge's new tool set is never listed (see the mode
	// 8 plan, §5.7③).
	WebSearch bool

	// Model / provider: the resolved dialog model (see the plan, D4). Model is the
	// real model name, not the composite llm_id.
	Model           string
	ProviderID      string
	ProviderBaseURL string
	ProviderToken   string

	// ModelContextWindow and ModelMaxOutput are the dialog model's real context window
	// and output cap (conf/all_models.json context_length / max_output). They are handed
	// to Codex as thread config because a model outside Codex's own manifest resolves to
	// fallback metadata with context_window = None — Codex then neither knows the window
	// nor auto-compacts (context_window.rs), so a full-history rebuild can overflow the
	// provider's real window. Zero means "unknown; leave Codex's own value".
	ModelContextWindow int
	ModelMaxOutput     int
	// ReasoningEffort is the fixed Codex reasoning effort this deployment uses
	// ("minimal" | "low" | "medium" | "high"). Empty leaves Codex's configured default.
	ReasoningEffort string

	// SandboxMode is this deployment's Codex sandbox posture ("read-only" |
	// "workspace-write" | "danger-full-access"). Empty keeps read-only. It is NOT
	// per-conversation: the sandbox bounds what the shared app-server may do on its
	// host, so moving the boundary (e.g. into a container that has no corpus mount)
	// is an operator decision applied to every thread.
	SandboxMode string

	// MCPURL is the full /mcp/codex/<handle> URL this process exposes to Codex.
	MCPURL string
	// Ticket is the stable session handle inside MCPURL. It is persisted per thread so a
	// reuse can check it is still alive and a session delete can revoke it.
	Ticket string
	// Binding is the per-turn authorization scope installed on Ticket for the duration of
	// this turn, and removed when the turn ends. Its concrete type is owned by the
	// injected TurnBinder; opaque here (nil skips binding).
	Binding any
	// Instructions is the citation contract (see the plan, D5).
	Instructions string
	// SubagentInstructions is the developer fragment a DELEGATED sub-agent gets, in place
	// of Instructions. Codex otherwise copies the parent thread's developer instructions
	// into every child, so without this a sub-agent would be handed a prompt written for
	// the end-user-facing root. It is delivered through Codex's seam
	// features.multi_agent_v2.subagent_developer_instructions; Codex still layers its own
	// sub-agent role prompt on top. Empty means "let Codex inherit the parent's".
	SubagentInstructions string

	TurnTimeout time.Duration
	// Messages is the full history with the current user message LAST.
	Messages []map[string]interface{}

	// OnContent receives answer deltas; OnReasoning receives reasoning deltas. OnTool
	// receives each MCP tool call the turn makes (call and outcome), so the caller can
	// narrate the retrieval steps into the thinking stream — mode 8 has no client-side
	// tool loop to report from otherwise.
	OnContent   func(string)
	OnReasoning func(string)
	OnTool      func(RoundTrip)
	// OnCompaction is called once per turn (at most) when Codex compacts its context.
	// Mode 8 narrates it into the thinking stream so a watching user learns the model's
	// history was summarized; it does not change how the thread is rebuilt.
	OnCompaction func()
	// HandleAlive reports whether a stored handle is still live. When it returns false a
	// reuse is refused and the thread is rebuilt, so a thread whose MCP URL carries a
	// dead handle (a restart lost it, or it expired between turns) is never resumed. nil
	// means "assume alive".
	HandleAlive func(token string) bool
	// ForceRebuild makes this turn ignore any stored thread and never persist the one it
	// builds. It is how a provider-failover retry gets a thread bound to the new provider:
	// the stored thread carries the failed provider's base_url/token, and provider identity
	// is deliberately NOT part of the reuse key, so the retry must neither resume nor
	// overwrite that mapping. Empty is the normal path.
	ForceRebuild bool
}

// TurnBinder installs and removes the per-turn authorization a turn's MCP handle needs.
// Because the binding is a single atomically-acquired KV value, it doubles as the
// per-session turn lock: Bind returns an error (e.g. another turn already holds it) when
// the turn must not proceed. The implementation also owns the binding's keep-alive
// heartbeat — started on a successful Bind and stopped on Unbind — so codexagent stays free
// of a dependency on the store. Binding is opaque: the caller owns the scope type.
type TurnBinder interface {
	// Bind atomically acquires the turn's binding; a non-nil error (e.g. another turn is
	// running) aborts the turn.
	Bind(ctx context.Context, handle string, binding any) error
	// Unbind releases the binding and stops its keep-alive.
	Unbind(ctx context.Context, handle string)
}

// RoundTrip is one observed tool call: the call itself (with its arguments) and its
// outcome. Mode 8 has no client-side tool loop to report from, so these are read off the
// thread items the Codex server emits; the caller narrates them into the thinking stream.
type RoundTrip struct {
	// Tool is the MCP tool name as the model called it (e.g. "search_bm25_chunks"), or
	// "shell" for a local command execution.
	Tool string
	// Kind separates the two item kinds this carries: RoundTripKindMCP for an MCP tool
	// call, RoundTripKindCommand for a `commandExecution` (the local shell mode 8 now
	// permits for computation). The caller renders the two differently.
	Kind string
	// Args is the call's arguments, compacted for display (may be truncated); for a
	// command it is the command line.
	Args string
	// Status is "running", "completed" or "failed".
	Status string
	// DurationMS is the server-reported call duration (0 while running / unknown).
	DurationMS int64
	// ExitCode is a command execution's process exit status; nil for an MCP tool call,
	// whose outcome is the tool result rather than a process exit.
	ExitCode *int
}

const (
	// RoundTripKindMCP is an MCP tool call (the retrieval path).
	RoundTripKindMCP = "mcp"
	// RoundTripKindCommand is a local shell command execution.
	RoundTripKindCommand = "command"
	// RoundTripKindAgent is a collaboration (sub-agent) tool call — spawnAgent,
	// sendMessage, wait, listAgents, … Enabled by features.multi_agent_v2.
	RoundTripKindAgent = "agent"
	// RoundTripKindBuiltin is any other Codex built-in action item (plan, fileChange,
	// dynamicToolCall, webSearch, imageView, sleep, review mode, image generation), reported
	// briefly so the thinking stream shows every tool the model used, not only MCP ones.
	RoundTripKindBuiltin = "builtin"
	// commandToolName is the display name for a command execution in the thinking
	// stream and in tool_call_counts (a distinct key, so a shell call never inflates the
	// benchmark's retrieval "Search Calls", which sums only the configured search tools).
	commandToolName = "shell"
	// agentToolPrefix namespaces a collaboration tool in tool_call_counts (e.g.
	// "agent:spawnAgent"), so a sub-agent call is never mistaken for an MCP tool.
	agentToolPrefix = "agent:"
)

// RoundTrip statuses (the v2 McpToolCallStatus values).
const (
	ToolStatusRunning   = "running"
	ToolStatusCompleted = "completed"
	ToolStatusFailed    = "failed"
)

// toolTally counts a turn's MCP tool calls by name, and how many of them failed. It mirrors
// the in-process engines' ToolCallCounts / ToolCallErrors so a mode-8 benchmark row is
// directly comparable with theirs.
type toolTally struct {
	counts map[string]int
	errors map[string]int
}

// call records one tool invocation.
func (t *toolTally) call(tool string) {
	if tool == "" {
		return
	}
	if t.counts == nil {
		t.counts = map[string]int{}
	}
	t.counts[tool]++
}

// outcome records how one invocation ended. Only a failure is interesting: a call that
// completed is already counted.
func (t *toolTally) outcome(rt RoundTrip) {
	if rt.Tool == "" || rt.Status != ToolStatusFailed {
		return
	}
	if t.errors == nil {
		t.errors = map[string]int{}
	}
	t.errors[rt.Tool]++
}

// retrievalAccounting accumulates a turn's retrieval accounting from the MCP tool
// results, through the same classifier the in-process engines feed
// (retrievalaccount.AccountToolOutput) — one implementation of the rules, so a mode-8 row is
// comparable with a mode-5 row rather than merely similar.
type retrievalAccounting struct {
	retrieved  map[string]struct{}
	served     map[string]int
	deep       int
	shallow    int
	deepIDs    map[string]struct{}
	shallowIDs map[string]struct{}
}

func (a *retrievalAccounting) add(acc retrievalaccount.ToolAccounting) {
	for _, d := range acc.RetrievedDocs {
		if d == "" {
			continue
		}
		if a.retrieved == nil {
			a.retrieved = map[string]struct{}{}
		}
		a.retrieved[d] = struct{}{}
	}
	// One increment per document per CALL, not per occurrence: the rendered output
	// repeats a document once per chunk, and the weight is meant to say how often the
	// retrieval insisted on the document, not how many chunks it had. (Mode 5 weights by
	// distinct QUERY; a caller that only sees the rendered output cannot attribute hits
	// to individual queries, so a call is the unit.)
	servedThisCall := map[string]struct{}{}
	for _, d := range acc.ServedDocs {
		if d != "" {
			servedThisCall[d] = struct{}{}
		}
	}
	if len(servedThisCall) > 0 && a.served == nil {
		a.served = map[string]int{}
	}
	for d := range servedThisCall {
		a.served[d]++
	}
	a.deep += acc.DeepChunks
	a.shallow += acc.ShallowChunks
	a.addIDs(&a.deepIDs, acc.DeepChunkIDs)
	a.addIDs(&a.shallowIDs, acc.ShallowChunkIDs)
}

func (a *retrievalAccounting) addIDs(set *map[string]struct{}, ids []string) {
	for _, id := range ids {
		if id == "" {
			continue
		}
		if *set == nil {
			*set = map[string]struct{}{}
		}
		(*set)[id] = struct{}{}
	}
}

// apply writes the accumulated accounting onto the outcome, in the same shape (and with
// the same ordering rules) the in-process engines ship: ids sorted, served documents
// ranked most-served first.
func (a *retrievalAccounting) apply(out *TurnOutcome) {
	out.RetrievedDocIDs = sortedSet(a.retrieved)
	out.ServedDocIDs = rankedByCount(a.served)
	out.DeepReadChunks = a.deep
	out.ShallowReadChunks = a.shallow
	out.DeepReadChunkIDs = sortedSet(a.deepIDs)
	out.ShallowReadChunkIDs = sortedSet(a.shallowIDs)
}

func sortedSet(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func rankedByCount(counts map[string]int) []string {
	if len(counts) == 0 {
		return nil
	}
	out := make([]string, 0, len(counts))
	for id := range counts {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool {
		if counts[out[i]] != counts[out[j]] {
			return counts[out[i]] > counts[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}

// TurnOutcome reports the terminal state of one turn.
type TurnOutcome struct {
	ThreadID  string
	Content   string
	Reasoning string
	// Usage is this turn's token accounting, when the server reported it. Codex emits
	// thread/tokenUsage/updated once per model call, so Usage sums those calls — the same
	// shape the in-process engines feed their benchmarks.
	Usage *TurnUsage
	// ToolCallCounts tallies the turn's MCP tool calls by tool name (e.g.
	// {"search_bm25_chunks": 16, "list_chunks": 12}); ToolCallErrors counts how many of
	// them failed. Same shape as the in-process engines' fields.
	ToolCallCounts map[string]int
	ToolCallErrors map[string]int
	// Retrieval accounting, read off the MCP tool results (see retrievalAccounting) and
	// reported in the in-process engines' shape: RetrievedDocIDs are the documents a
	// full-content tool opened; ServedDocIDs are the documents any corpus tool put in
	// front of the model, ranked most-served first; the read counts split by depth, with
	// the ids behind them.
	RetrievedDocIDs     []string
	ServedDocIDs        []string
	DeepReadChunks      int
	ShallowReadChunks   int
	DeepReadChunkIDs    []string
	ShallowReadChunkIDs []string
	// Rebuilt is true when the turn ran on a freshly created thread (first turn, or
	// a rebuild after scope/history drift).
	Rebuilt bool
	// Compactions counts the context compactions Codex performed during this turn (0 or
	// 1; a turn's context is compacted at most once). It is observability, not behavior:
	// mode 8 never trims a rebuilt thread's history, so a compaction costs the model's
	// detail only inside the live thread, never in RAGFlow's stored conversation.
	Compactions int
	// ContextWindow is the model context window Codex reported for this turn
	// (ThreadTokenUsage.ModelContextWindow). Zero means Codex does not know the window —
	// the dialog model resolved to fallback metadata and auto-compaction is disabled.
	ContextWindow int
}

// TurnUsage is one turn's token cost. Field names match service.TurnUsage so a caller maps
// it straight across (and a benchmark can compare mode 8 with modes 5/6/7).
type TurnUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	// LLMCalls counts the model calls the turn made — the number of usage notifications,
	// since Codex reports one per upstream Responses completion.
	LLMCalls int `json:"llm_calls"`
}

// Runner executes mode 8 turns against a shared Codex client.
//
// Per-session mutual exclusion is NOT a process-local lock here: it comes from the turn's
// MCP binding, acquired atomically in KV (TurnBinder.Bind) and released when the turn ends.
// That serializes same-session turns across processes and replicas, and a second concurrent
// turn fails fast with mcp.ErrCodexTurnBusy instead of queueing.
type Runner struct {
	client *codexgo.Client
	store  ThreadStore
	binder TurnBinder
}

// NewRunner returns a runner. store may be nil (ephemeral-only); binder may be nil (no
// per-turn MCP binding).
func NewRunner(client *codexgo.Client, store ThreadStore, binder TurnBinder) *Runner {
	return &Runner{client: client, store: store, binder: binder}
}

// bindTurn acquires the turn's MCP binding. A non-nil error (e.g. another turn is already
// running for the session) aborts the turn: the binding is the per-session turn lock, so
// proceeding without it would either run unauthorized or clobber the other turn's scope.
func (r *Runner) bindTurn(ctx context.Context, req TurnRequest) error {
	if r.binder == nil || req.Ticket == "" || req.Binding == nil {
		return nil
	}
	if err := r.binder.Bind(ctx, req.Ticket, req.Binding); err != nil {
		return fmt.Errorf("codexagent: bind MCP handle %q: %w", req.Ticket, err)
	}
	return nil
}

// unbindTurn removes the turn's MCP binding so the handle stops being authorized once the
// turn is over. It always uses a detached, bounded context: the turn's ctx may already be
// canceled, and a failed unbind would leave the handle authorized until its TTL.
func (r *Runner) unbindTurn(req TurnRequest) {
	if r.binder == nil || req.Ticket == "" || req.Binding == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r.binder.Unbind(ctx, req.Ticket)
}

// RunTurn runs one turn, reusing or (re)creating the session's thread as needed.
func (r *Runner) RunTurn(ctx context.Context, req TurnRequest) (*TurnOutcome, error) {
	if req.MCPURL == "" {
		return nil, errors.New("codexagent: empty MCP URL")
	}
	if len(req.Messages) == 0 {
		return nil, errors.New("codexagent: no messages")
	}
	current := req.Messages[len(req.Messages)-1]

	// Bind BEFORE any thread work: the binding IS the per-session turn lock (one atomically
	// acquired KV value), so if another turn already holds it, Bind returns
	// mcp.ErrCodexTurnBusy and this turn must not proceed. It stays held until the turn's
	// deferred unbind — there is no separate in-process lock.
	if err := r.bindTurn(ctx, req); err != nil {
		return nil, err
	}
	defer r.unbindTurn(req)

	// Ephemeral: no session id or no store => one-shot thread, never persisted.
	if req.SessionID == "" || r.store == nil {
		return r.runOnThread(ctx, req, current, req.Messages)
	}

	scopeFP := ScopeFingerprint(req.TenantID, req.DatasetIDs)
	// The web-search capability is decided per turn, but Codex caches an MCP tool
	// catalog for the life of a thread: a change must rebuild the thread, or the new
	// tool set is never advertised. Fold the decision into the reuse key.
	scopeFP = CombineFingerprints(scopeFP, WebSearchFingerprint(req.WebSearch))
	// The hosted (server-side) tool policy is baked into the thread at create as well, and
	// thread/resume does NOT reload it (measured: a resumed thread kept the provider's
	// web_search tool). Fold the policy marker in so threads built under an older policy
	// rebuild once, instead of keeping a server-side tool we have since disabled.
	scopeFP = CombineFingerprints(scopeFP, HostedToolsFingerprint())
	// The citation contract is baked into the thread at create and not reloaded on
	// resume, so it is part of the reuse key too. The sub-agent contract rides the same
	// create-time config (features.multi_agent_v2.subagent_developer_instructions), so it
	// is folded in as well. Each is folded in as ONE further hash: concatenating 64-char
	// hex strings would exceed the size:64 column.
	if req.Instructions != "" {
		scopeFP = CombineFingerprints(scopeFP, InstructionsFingerprint(req.Instructions))
	}
	if req.SubagentInstructions != "" {
		scopeFP = CombineFingerprints(scopeFP, InstructionsFingerprint(req.SubagentInstructions))
	}
	// HOW the contract is delivered and which tools we enable are also baked at create,
	// so the policy marker is part of the reuse key — it also forces a one-time rebuild
	// of threads built under the old policy (see instructionsPolicy).
	scopeFP = CombineFingerprints(scopeFP, InstructionsPolicyFingerprint())
	// The model window / auto-compaction limit / reasoning effort are baked into the
	// thread's config at create and not reloaded on resume, so they are part of the reuse
	// key too: a corrected window or a changed effort must rebuild the thread.
	autoCompactLimit := 0
	if req.ModelContextWindow > 0 && req.ModelContextWindow > req.ModelMaxOutput {
		autoCompactLimit = req.ModelContextWindow - req.ModelMaxOutput
	}
	scopeFP = CombineFingerprints(scopeFP, ModelParamsFingerprint(req.Model, req.ModelContextWindow, autoCompactLimit, req.ReasoningEffort))
	// The history the thread must ALREADY have is everything before the current user
	// message. Normal growth (the previous answer + this question) is not drift; a
	// deletion or edit makes this differ from what we recorded after the last turn.
	seenFP := HistoryFingerprint(HistoryWithoutCurrent(req.Messages))

	rec, found, err := r.store.Get(ctx, req.TenantID, req.SessionID)
	if err != nil {
		return nil, err
	}

	// Reuse the thread only when scope AND the already-seen history still match
	// RAGFlow's truth AND the stored handle is still alive. thread/resume does not reload
	// mcp_servers, so a scope change still rebuilds (see the plan, D3); a history change
	// must replay (see P1#6); a dead handle means the thread's baked-in MCP URL would 401,
	// so it MUST rebuild (see P1#4). ForceRebuild is a provider-failover retry: the stored
	// thread is bound to the failed provider, and provider identity is deliberately NOT in
	// the reuse key, so the retry must never resume it.
	handleAlive := req.HandleAlive == nil || rec.Ticket == "" || req.HandleAlive(rec.Ticket)
	if shouldReuseThread(found, rec, scopeFP, seenFP, handleAlive, req.ForceRebuild) {
		if th, err := r.client.ResumeThread(ctx, rec.ThreadID, r.threadOptions(req)...); err == nil {
			defer th.Close()
			out, err := r.consume(ctx, th, req, current)
			if err != nil {
				// Resume connected but the turn failed on a live thread: surface it
				// rather than silently rebuilding and double-running.
				return nil, err
			}
			out.ThreadID = rec.ThreadID
			// Advance the recorded history so the NEXT turn reuses: include this turn's
			// answer. (A failed write only costs a rebuild next turn.) Skip it if the
			// session was deleted mid-turn (handle revoked) — otherwise this Put would
			// resurrect the mapping the delete just removed.
			if r.handleLive(req) {
				_ = r.store.Put(ctx, ThreadRecord{
					TenantID:           req.TenantID,
					SessionID:          req.SessionID,
					ThreadID:           rec.ThreadID,
					ScopeFingerprint:   scopeFP,
					HistoryFingerprint: HistoryFingerprint(appendAssistant(req.Messages, out.Content)),
					Ticket:             rec.Ticket,
				})
			}
			return out, nil
		}
		// Resume failed (server restarted / thread reclaimed): fall through to rebuild.
	}

	// (Re)build: first turn, scope/history drift, or resume failure. The handle is
	// per-session and stable, so the rebuilt thread reuses the same MCP URL — there is
	// nothing to revoke.
	out, threadID, err := r.createAndRun(ctx, req, current)
	if err != nil {
		return nil, err
	}
	out.ThreadID = threadID
	out.Rebuilt = true
	// Record the history the thread now has: the incoming history PLUS this turn's
	// answer. The NEXT turn's already-seen history must equal this to reuse. Skip it if
	// the session was deleted mid-turn (handle revoked) — otherwise this Put would
	// resurrect the mapping the delete just removed.
	//
	// A ForceRebuild (provider-failover) attempt is also NOT persisted: its thread is bound
	// to a different provider, and provider identity is deliberately not fingerprinted, so
	// storing it would let a later primary turn match the record and silently resume a thread
	// pointing at the backup endpoint. Leaving the mapping untouched keeps "primary first"
	// semantics for the next turn.
	if !req.ForceRebuild && r.handleLive(req) {
		if err := r.store.Put(ctx, ThreadRecord{
			TenantID:           req.TenantID,
			SessionID:          req.SessionID,
			ThreadID:           threadID,
			ScopeFingerprint:   scopeFP,
			HistoryFingerprint: HistoryFingerprint(appendAssistant(req.Messages, out.Content)),
			Ticket:             req.Ticket,
		}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// handleLive reports whether this turn's handle is still alive, i.e. the session was not
// deleted mid-turn. When no liveness hook is set the turn is assumed live.
func (r *Runner) handleLive(req TurnRequest) bool {
	if req.HandleAlive == nil || req.Ticket == "" {
		return true
	}
	return req.HandleAlive(req.Ticket)
}

// shouldReuseThread reports whether the stored record may be resumed for this turn: it must
// exist, the thread's scope and already-seen history must still match RAGFlow's, its baked-in
// MCP handle must be alive, and the turn must not be a provider-failover retry (forceRebuild),
// whose stored thread points at the failed provider.
func shouldReuseThread(found bool, rec ThreadRecord, scopeFP, seenFP string, handleAlive, forceRebuild bool) bool {
	return found && !forceRebuild && rec.ScopeFingerprint == scopeFP &&
		rec.HistoryFingerprint == seenFP && handleAlive
}

// appendAssistant returns msgs with an assistant message carrying content appended.
func appendAssistant(msgs []map[string]interface{}, content string) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(msgs)+1)
	out = append(out, msgs...)
	return append(out, map[string]interface{}{"role": "assistant", "content": content})
}

// createAndRun starts a fresh thread, replays the history (minus the current message),
// then runs the current turn.
func (r *Runner) createAndRun(ctx context.Context, req TurnRequest, current map[string]interface{}) (*TurnOutcome, string, error) {
	th, err := r.client.StartThread(ctx, r.threadOptions(req)...)
	if err != nil {
		return nil, "", err
	}
	defer th.Close()

	items := MessagesToItems(HistoryWithoutCurrent(req.Messages))
	if len(items) > 0 {
		if err := r.client.ThreadInjectItems(ctx, codexgo.ThreadInjectItemsRequest{ThreadID: th.ID(), Items: items}); err != nil {
			return nil, th.ID(), err
		}
	}
	out, err := r.consume(ctx, th, req, current)
	return out, th.ID(), err
}

// runOnThread is the ephemeral path: a one-shot thread with the whole history replayed.
func (r *Runner) runOnThread(ctx context.Context, req TurnRequest, current map[string]interface{}, all []map[string]interface{}) (*TurnOutcome, error) {
	th, err := r.client.StartThread(ctx, r.threadOptions(req)...)
	if err != nil {
		return nil, err
	}
	defer th.Close()
	items := MessagesToItems(HistoryWithoutCurrent(all))
	if len(items) > 0 {
		if err := r.client.ThreadInjectItems(ctx, codexgo.ThreadInjectItemsRequest{ThreadID: th.ID(), Items: items}); err != nil {
			return nil, err
		}
	}
	out, err := r.consume(ctx, th, req, current)
	if err != nil {
		return nil, err
	}
	out.ThreadID = th.ID()
	out.Rebuilt = true
	return out, nil
}

// threadSandboxMode maps the configured posture onto the SDK's thread sandbox. An
// unset or unrecognized value falls back to read-only: mode 8's default is to reason and
// call MCP tools, and a typo in operator config must not widen the surface.
func threadSandboxMode(configured string) codexgo.SandboxMode {
	switch codexgo.SandboxMode(configured) {
	case codexgo.SandboxWorkspaceWrite:
		return codexgo.SandboxWorkspaceWrite
	case codexgo.SandboxFullAccess:
		return codexgo.SandboxFullAccess
	default:
		return codexgo.SandboxReadOnly
	}
}

func (r *Runner) threadOptions(req TurnRequest) []codexgo.ThreadOption {
	opts := []codexgo.ThreadOption{
		codexgo.WithThreadConfigOverride("mcp_servers.ragflow", map[string]interface{}{
			"url": req.MCPURL,
		}),
		// Turn off Codex's own provider-hosted (server-side) tools. For a custom
		// Responses provider the only hosted tool Codex emits is the built-in
		// `web_search`: the PROVIDER runs it and bills it (e.g. MiniMax's tools
		// pricing), and it is advertised to the model regardless of the conversation's
		// settings, so a chat with web search off would still reach the web. We never
		// want that — retrieval and any web search must go through this process, where
		// the conversation's scope and enablement are enforced. `web_search` is a
		// top-level config key (WebSearchMode; "disabled" removes the tool entirely);
		// image generation, the other hosted tool, is never emitted for a custom
		// provider (it requires OpenAI-issued auth), so this single switch closes every
		// server-side tool Codex could expose here.
		codexgo.WithThreadConfigOverride("web_search", "disabled"),
		// Enable Codex's plan tool. It is OFF by default (config_toml's
		// UpdatePlanToolConfig.enabled defaults to false, and resolve_update_plan_enabled
		// treats an absent `tools` table as disabled), so without this a mode-8 thread
		// advertises no plan tool AND — with the default prompt back in play — the
		// prompt's own `## Planning` / `## update_plan` guidance would describe a tool
		// that does not exist. See the plan doc §5.9.
		codexgo.WithThreadConfigOverride("tools.update_plan", map[string]interface{}{"enabled": true}),
		// Codex memory (and its dedicated tools) stay OFF. Two reasons, either one enough:
		//
		//  1. NO TENANT ISOLATION. Memory's root is codex_home/<memories|memories_v2> with no
		//     tenant/project/thread namespace, and codex_home is process-level (NOT a
		//     thread-overridable key), so a SHARED app-server makes one tenant's memories
		//     reachable in another tenant's thread. Isolation would need one CODEX_HOME per
		//     tenant (docs §5.5).
		//  2. IT CORRUPTS BENCHMARK ACCURACY. Memory persists what a thread already answered
		//     and re-injects it into later turns, so a question can be answered from a
		//     remembered answer instead of retrieved evidence — inflating accuracy and making
		//     runs incomparable with a memory-free baseline.
		//
		// (Secondary: with these models unset, memory's auxiliary turns fall back to Codex's
		// bundled catalog — gpt-5.6-terra / gpt-5.6-luna — which the dialog provider does not
		// serve; every such call failed 11102/2013, ~150 times in 12h.)
		codexgo.WithThreadConfigOverride("features.memories", false),
		codexgo.WithThreadConfigOverride("memories.dedicated_tools", false),
		// Additional default-off capabilities, enabled deliberately for the same
		// capability-ceiling test:
		//  - current_time_reminder: registers the `current_time` tool (clock questions).
		//  - sleep_tool: the feature is already on by default, but its mode defaults to
		//    `model_driven`, which registers the tool only when the clock reminder is on.
		//    `always_on` registers it outright (SleepToolConfigToml.mode).
		//  - multi_agent_v2_dynamic_tools: the V2 collaboration tools' dynamic-tool
		//    surface. features.multi_agent_v2 itself is added below, as a TABLE, so it can
		//    also carry the sub-agent developer contract.
		// `features.token_budget` is deliberately LEFT OFF. It is a
		// `Stage::UnderDevelopment` / default-off feature (features/src/lib.rs) and it does
		// two things, one harmless and one destructive:
		//  - it registers `get_context_remaining` + `new_context`. In 815 mode-8 threads the
		//    model called `new_context` ZERO times and `get_context_remaining` 7 times, every
		//    one returning "You have 0 tokens left" — i.e. only once the budget was already
		//    spent, where the number is useless.
		//  - it REPLACES auto-compaction with `compact_token_budget::run_inline_auto_compact_task`
		//    (core/src/session/turn.rs: `if features.enabled(Feature::TokenBudget) { ... }`),
		//    which SKIPS model summarization and installs a bare context window. That path
		//    writes `CompactedItem.message = ""` and drops every user message from
		//    `replacement_history`, so the next turn sees no question at all and answers
		//    "What would you like me to work on?". All 501 mode-8 compactions were empty
		//    summaries; the 127 `codex-tui`/`codex_vscode`/`codex_exec` compactions, which
		//    never enable this feature, all carried a real summary.
		// Without it the thread takes the normal `run_auto_compact` path: for a provider
		// without `RemoteCompactionSupport::V2` (every non-OpenAI/Azure provider, MiniMax
		// included) that is `compact.rs`'s local compaction, which keeps the user messages
		// and asks the model for a real summary.
		codexgo.WithThreadConfigOverride("features.current_time_reminder", true),
		codexgo.WithThreadConfigOverride("features.sleep_tool", map[string]interface{}{
			"enabled": true,
			"mode":    "always_on",
		}),
		codexgo.WithThreadConfigOverride("features.multi_agent_v2_dynamic_tools", true),
		// Capability isolation (see the plan, §5.5). The default is read-only; a
		// deployment whose boundary is the container instead (no corpus mount, so the
		// inner sandbox has nothing left to confine) selects danger-full-access.
		codexgo.WithThreadSandbox(threadSandboxMode(req.SandboxMode)),
		// OnRequest, NOT never. Under a RESTRICTED (read-only) sandbox, exec_policy allows
		// any non-escalated, non-dangerous command without prompting (it lets the sandbox
		// enforce the restrictions), so a short local script for arithmetic still runs —
		// while `Never` would DENY every MCP tool call that requires approval
		// ("MCP tool call requires approval, but approval policy is never",
		// core/src/mcp_tool_call.rs) and kill the retrieval path outright. OnRequest keeps
		// the elicitation path alive, and our Dispatcher accepts exactly the
		// `mcp_tool_call` elicitation for the ragflow server (and refuses everything else,
		// including sandbox-escalation requests, so the sandbox still bounds what runs).
		codexgo.WithThreadApprovalPolicy(codexgo.ApprovalOnRequest()),
	}
	// features.multi_agent_v2 is passed as a TABLE, not the bare `true` shorthand the config
	// schema also accepts, so the thread can carry the SUB-AGENT developer contract too.
	// Codex applies subagent_developer_instructions to a spawned child in place of the
	// parent's own developer instructions (core/src/agent/child_config.rs) and still adds
	// its own sub-agent role prompt — this is the delivery seam for
	// codexSubagentContract. The V2 collaboration tools (spawn/send_message/followup_task/
	// list_agents/wait_agent) come with it; sub-agents mean extra threads per turn, so more
	// tokens and concurrency.
	multiAgent := map[string]interface{}{
		"enabled": true,
		// Hide the spawn tool's `model`/`reasoning_effort` parameters. They default to
		// EXPOSED, and their catalog comes from Codex's OWN model manifest
		// (core/src/tools/handlers/multi_agents_spec.rs), which does NOT list the dialog
		// model. The root then fills `model` with either a manifest name the provider
		// cannot serve, or a name Codex cannot resolve at all — the latter aborts the
		// spawn outright ("Unknown model `X` for spawn_agent", core/src/agent/
		// child_config.rs) and stalls the turn on the parent's `wait_agent`. With the
		// parameters gone a spawned child inherits this thread's model, which is the
		// dialog model we bake below for exactly that reason.
		"expose_spawn_agent_model_overrides": false,
		// Deliver collaboration tasks as an ordinary plaintext user message. Codex's
		// default wraps them in an `agent_message` item, a first-party OpenAI Responses
		// type that a third-party endpoint ignores — the child then reports that no task
		// arrived. This replaces the CODEX_PLAINTEXT_COLLABORATION_MESSAGES environment
		// variable the pre-0.166 patched Codex needed.
		"message_delivery": "plaintext",
	}
	if req.SubagentInstructions != "" {
		multiAgent["subagent_developer_instructions"] = req.SubagentInstructions
	}
	opts = append(opts, codexgo.WithThreadConfigOverride("features.multi_agent_v2", multiAgent))
	if req.Model != "" {
		// The dialog model goes in TWICE, on purpose:
		//   - thread/start's top-level `model` param selects it for this thread;
		//   - the `model` config entry bakes it into the thread's config, which is what a
		//     FRESH (non-forked, `fork_turns:"none"`) sub-agent inherits. A full-history
		//     fork carries the parent's model through the forked context, but a fresh child
		//     builds its config from the thread config — and without this entry that config
		//     still holds the shared app-server's DEFAULT model. The provider IS inherited,
		//     so the child then calls the dialog provider with the app-server's model name
		//     (e.g. `gpt-6.1-sol`, not in the provider's catalog) and every request is
		//     rejected with "unknown model"; the child dies before it can call any tool and
		//     the parent's `wait_agent` times out, stalling the whole turn.
		opts = append(opts,
			codexgo.WithThreadModel(req.Model),
			codexgo.WithThreadConfigOverride("model", req.Model),
			// Memory's auxiliary turns pick their model from memories.extract_model /
			// consolidation_model and, unset, fall back to Codex's BUNDLED catalog
			// (gpt-5.6-terra / gpt-5.6-luna) — models the dialog's provider does not serve.
			// Memory is off (see above); pinning these strings keeps a future default flip
			// from spending requests on catalog slugs the provider cannot answer.
			codexgo.WithThreadConfigOverride("memories.extract_model", req.Model),
			codexgo.WithThreadConfigOverride("memories.consolidation_model", req.Model),
		)
	}
	// Tell Codex the dialog model's REAL window. Codex resolves any model outside its own
	// manifest to fallback metadata (context_window = None), and with no window it neither
	// knows the limit nor auto-compacts — so a full-history rebuild can overflow the
	// provider's window and fail there. Baking the window in lets Codex compact before it.
	// The auto-compaction limit is the input budget (window minus the model's output cap),
	// which also overrides whatever global default the shared server carries.
	if req.ModelContextWindow > 0 {
		opts = append(opts, codexgo.WithThreadConfigOverride("model_context_window", req.ModelContextWindow))
		if limit := req.ModelContextWindow - req.ModelMaxOutput; limit > 0 {
			opts = append(opts, codexgo.WithThreadConfigOverride("model_auto_compact_token_limit", limit))
		}
	}
	// A fixed reasoning effort (this deployment's choice); a change is folded into the
	// scope fingerprint so the thread rebuilds, since Codex reads it at create.
	if req.ReasoningEffort != "" {
		opts = append(opts, codexgo.WithThreadConfigOverride("model_reasoning_effort", req.ReasoningEffort))
	}
	// Only declare a per-thread provider when one is supplied; otherwise the thread
	// uses whatever provider the shared Codex is configured with.
	if req.ProviderID != "" {
		opts = append(opts,
			codexgo.WithThreadModelProvider(req.ProviderID),
			codexgo.WithThreadConfigOverride("model_providers."+req.ProviderID, map[string]interface{}{
				"name":                      req.ProviderID,
				"base_url":                  req.ProviderBaseURL,
				"wire_api":                  "responses",
				"experimental_bearer_token": req.ProviderToken,
			}),
		)
	}
	if req.Instructions != "" {
		// Deliver the citation contract as DEVELOPER instructions, not as base
		// instructions. Base instructions REPLACE Codex's own agentic prompt (the
		// Responses `instructions` field; see session/mod.rs's precedence: config >
		// history > model default), so overriding it threw away the model's default
		// prompt — planning, verification discipline, answer shape — and left a thin
		// contract in its place. Developer instructions are an ADDITIVE developer-role
		// fragment in the world state (context/developer_instructions.rs), so Codex keeps
		// its default prompt and also gets the citation rules.
		opts = append(opts, codexgo.WithThreadDeveloperInstructions(req.Instructions))
	}
	// Deliberately NO WithThreadCWD. Codex resolves a thread's cwd on the APP-SERVER host,
	// so a directory RAGFlow creates locally is meaningless once the two run as separate
	// pods — and a cwd that does not exist there makes EVERY shell command fail ("No such
	// file or directory"), spinning the turn on retries instead of failing fast. The
	// mechanism this replaced claimed to isolate the host, but the read-only sandbox never
	// confined READS in the first place (it forbids writes and network only), so cwd bought
	// no isolation and merely broke local computation off-host. Omitting it lets Codex use
	// the app-server's own working directory, which exists.
	//
	// Both facts are reproducible: see cwd_missing_integration_test.go (a non-existent cwd
	// fails every command; omitting cwd works) and read_barrier_integration_test.go (the
	// shell reads outside cwd regardless).
	return opts
}

// consume runs one turn on an already-connected thread via RunStreamed (the sync Run
// is incompatible with the app-server: it polls thread/read, which this codex does not
// support) and assembles the answer from the event stream.
//
// A stream that closes without a terminal turn is a failure, never a partial success: a
// dropped connection must not ship a half answer as final (P1#5).
func (r *Runner) consume(ctx context.Context, th *codexgo.SessionThread, req TurnRequest, current map[string]interface{}) (*TurnOutcome, error) {
	runCtx := ctx
	if req.TurnTimeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, req.TurnTimeout)
		defer cancel()
	}

	// RunStreamedTurn returns the server-assigned turn id synchronously, before any event
	// is delivered, so a timeout can always interrupt the remote turn — even if no
	// id-carrying event was consumed yet. The event stream is otherwise identical to
	// RunStreamed (subscribe-before-start included).
	events, turnID, err := th.RunStreamedTurn(runCtx, currentText(current))
	if err != nil {
		return nil, err
	}

	out := &TurnOutcome{}
	var content, reasoning strings.Builder
	var items []codexgo.Item
	completed := false
	// A detail-less error notification captured before the terminal turn.
	var streamErr error
	// The server reports usage once per model call; sum the per-call figures.
	var usage TurnUsage
	// Reasoning item ids that streamed deltas. A provider that does NOT stream reasoning
	// (measured against a non-OpenAI Responses backend) delivers one finished `reasoning`
	// item and no delta notifications at all — the think panel would then stay empty. The
	// id set lets a completed item be used as a fallback without double-counting the
	// providers that do stream, where the finished item repeats what the deltas already
	// carried.
	reasoningDeltaItems := map[string]bool{}
	// MCP tool call item ids already reported as a call, so a completed item does not
	// narrate the same call twice when item/started did arrive.
	toolCallsSeen := map[string]bool{}
	// Per-tool tallies, so a mode-8 turn reports tool_call_counts / tool_call_errors in
	// the same shape the in-process engines do (a benchmark reads them either way).
	var tally toolTally
	// Retrieval accounting, from the same tool results the narration reads.
	var retrieval retrievalAccounting
	// A turn's context is compacted at most once, but two surfaces can announce it — the
	// `contextCompaction` thread item and the `thread/compacted` notification — so this
	// flag makes the count a count of compactions, not of announcements.
	compacted := false
	noteCompaction := func() {
		if compacted {
			return
		}
		compacted = true
		out.Compactions++
		if req.OnCompaction != nil {
			req.OnCompaction()
		}
	}
	// A sub-agent activity item is the only signal that a sub-agent was actually spawned
	// (see subAgentRoundTrip). Only the "spawn" marker counts as a call, so the tally reads
	// "N agents started" rather than counting each agent twice.
	noteSubAgent := func(item codexgo.Item) {
		if toolCallsSeen[item.ID] {
			return
		}
		rt, ok := subAgentRoundTrip(item)
		if !ok {
			return
		}
		toolCallsSeen[item.ID] = true
		if rt.Status == ToolStatusRunning {
			tally.call(rt.Tool)
		}
		if req.OnTool != nil {
			req.OnTool(rt)
		}
	}
	// Any other Codex built-in action item, narrated briefly (see builtinRoundTrip). These
	// are completion-only items, so one line each.
	noteBuiltin := func(item codexgo.Item) {
		if toolCallsSeen[item.ID] {
			return
		}
		rt, ok := builtinRoundTrip(item)
		if !ok {
			return
		}
		toolCallsSeen[item.ID] = true
		tally.call(rt.Tool)
		if req.OnTool != nil {
			req.OnTool(rt)
		}
	}

	for {
		select {
		case <-runCtx.Done():
			// The local cancel only frees this consumer; the server-side turn keeps
			// running (calling MCP, burning model budget) unless we interrupt it.
			r.interrupt(th, turnID)
			return nil, runCtx.Err()
		case ev, ok := <-events:
			if !ok {
				// ctx cancellation also tears the turn's subscription down, so this
				// branch — not only the ctx.Done case above — is a cancellation exit.
				// Interrupt the remote turn on any non-terminal exit.
				if !completed {
					r.interrupt(th, turnID)
					if err := runCtx.Err(); err != nil {
						return nil, err
					}
					if streamErr != nil {
						return nil, streamErr
					}
					return nil, errors.New("codexagent: event stream closed before the turn completed")
				}
				out.Content = content.String()
				out.Reasoning = reasoning.String()
				out.Usage = usageOrNil(usage)
				out.ToolCallCounts, out.ToolCallErrors = tally.counts, tally.errors
				retrieval.apply(out)
				return out, nil
			}
			switch e := ev.Raw.(type) {
			case codexgo.TurnStartedEvent:
			case codexgo.ItemStartedEvent:
				// The call half of an MCP tool round trip: server + tool + arguments.
				// Some providers jump straight to item/completed, so the completed half
				// emits the call too when this one never arrived (see toolCallsSeen).
				if e.Item != nil && e.Item.Kind == codexgo.ItemKindMCPToolCall {
					if rt, ok := mcpToolRoundTrip(*e.Item); ok {
						toolCallsSeen[e.Item.ID] = true
						tally.call(rt.Tool)
						if req.OnTool != nil {
							req.OnTool(rt)
						}
					}
				}
				if e.Item != nil && e.Item.Kind == codexgo.ItemKindCommandExecution {
					// The call half of a local shell execution: command line only (no
					// exit code yet). Narrated so a user sees the model computing.
					if rt, ok := commandRoundTrip(*e.Item); ok {
						toolCallsSeen[e.Item.ID] = true
						tally.call(rt.Tool)
						if req.OnTool != nil {
							req.OnTool(rt)
						}
					}
				}
				if e.Item != nil && e.Item.Kind == codexgo.ItemKindCollabAgentToolCall {
					// The call half of a sub-agent collaboration step (multi_agent_v2).
					if rt, ok := collabRoundTrip(*e.Item); ok {
						toolCallsSeen[e.Item.ID] = true
						tally.call(rt.Tool)
						if req.OnTool != nil {
							req.OnTool(rt)
						}
					}
				}
				if e.Item != nil && e.Item.Kind == codexgo.ItemKindSubAgentActivity {
					noteSubAgent(*e.Item)
				}
				if e.Item != nil && e.Item.Kind == codexgo.ItemKindContextCompaction {
					noteCompaction()
				}
			case codexgo.ItemCompletedEvent:
				if e.Item != nil {
					items = append(items, *e.Item)
					if e.Item.Kind == codexgo.ItemKindContextCompaction {
						noteCompaction()
					}
					// Fallback for providers that deliver reasoning as a finished item:
					// only when this item never streamed deltas (see reasoningDeltaItems).
					if e.Item.Kind == codexgo.ItemKindReasoning && !reasoningDeltaItems[e.Item.ID] {
						if text := reasoningItemText(*e.Item); text != "" {
							reasoning.WriteString(text)
							if req.OnReasoning != nil {
								req.OnReasoning(text)
							}
						}
					}
					// The outcome half of an MCP tool round trip.
					if e.Item.Kind == codexgo.ItemKindMCPToolCall {
						if rt, ok := mcpToolRoundTrip(*e.Item); ok {
							if !toolCallsSeen[e.Item.ID] {
								// No item/started was delivered: report the call here so
								// the caller still sees the tool and its arguments.
								toolCallsSeen[e.Item.ID] = true
								call := rt
								call.Status = ToolStatusRunning
								call.DurationMS = 0
								tally.call(rt.Tool)
								if req.OnTool != nil {
									req.OnTool(call)
								}
							}
							tally.outcome(rt)
							retrieval.add(retrievalaccount.AccountToolOutput(rt.Tool, mcpToolOutput(*e.Item)))
							if req.OnTool != nil {
								req.OnTool(rt)
							}
						}
					}
					// The outcome half of a local shell execution. No retrieval
					// accounting: a command touches no corpus document, so it must not
					// enter retrieved/served sets.
					if e.Item.Kind == codexgo.ItemKindCommandExecution {
						if rt, ok := commandRoundTrip(*e.Item); ok {
							if !toolCallsSeen[e.Item.ID] {
								// No item/started was delivered: report the call here so
								// the caller still sees the command that ran.
								toolCallsSeen[e.Item.ID] = true
								call := rt
								call.Status = ToolStatusRunning
								call.DurationMS = 0
								call.ExitCode = nil
								tally.call(rt.Tool)
								if req.OnTool != nil {
									req.OnTool(call)
								}
							}
							tally.outcome(rt)
							if req.OnTool != nil {
								req.OnTool(rt)
							}
						}
					}
					// The outcome half of a sub-agent collaboration step. No retrieval
					// accounting either: it is not a corpus tool.
					if e.Item.Kind == codexgo.ItemKindCollabAgentToolCall {
						if rt, ok := collabRoundTrip(*e.Item); ok {
							if !toolCallsSeen[e.Item.ID] {
								toolCallsSeen[e.Item.ID] = true
								call := rt
								call.Status = ToolStatusRunning
								tally.call(rt.Tool)
								if req.OnTool != nil {
									req.OnTool(call)
								}
							}
							tally.outcome(rt)
							if req.OnTool != nil {
								req.OnTool(rt)
							}
						}
					}
					// A sub-agent spawn/complete marker (see noteSubAgent).
					if e.Item.Kind == codexgo.ItemKindSubAgentActivity {
						noteSubAgent(*e.Item)
					}
					// Every other built-in action item, briefly (see noteBuiltin).
					switch e.Item.Kind {
					case codexgo.ItemKindPlan, codexgo.ItemKindFileChange, codexgo.ItemKindDynamicToolCall,
						codexgo.ItemKindWebSearch, codexgo.ItemKindImageView, codexgo.ItemKindSleep,
						codexgo.ItemKindEnteredReviewMode, codexgo.ItemKindExitedReviewMode,
						codexgo.ItemKindImageGeneration:
						noteBuiltin(*e.Item)
					}
				}
			case codexgo.ItemAgentMessageDeltaEvent:
				if e.Text != "" {
					content.WriteString(e.Text)
					if req.OnContent != nil {
						req.OnContent(e.Text)
					}
				}
			case codexgo.ItemReasoningTextDeltaEvent:
				if e.Text != "" {
					reasoningDeltaItems[e.ItemID] = true
					reasoning.WriteString(e.Text)
					if req.OnReasoning != nil {
						req.OnReasoning(e.Text)
					}
				}
			case codexgo.ItemReasoningSummaryTextDeltaEvent:
				if e.Text != "" {
					reasoningDeltaItems[e.ItemID] = true
					reasoning.WriteString(e.Text)
					if req.OnReasoning != nil {
						req.OnReasoning(e.Text)
					}
				}
			case codexgo.ThreadCompactedEvent:
				// The notification half of the same event (see noteCompaction); only
				// reaches us now that eventMatchesTurn routes thread/compacted.
				noteCompaction()
			case codexgo.ThreadTokenUsageUpdatedEvent:
				// One notification per model call. `Last` is that call's usage, so the
				// running sum is the turn's cost; `Total` is thread-cumulative and would
				// over-count across turns of the same thread.
				if e.TokenUsage != nil {
					usage.LLMCalls++
					usage.PromptTokens += int(e.TokenUsage.Last.InputTokens)
					usage.CompletionTokens += int(e.TokenUsage.Last.OutputTokens)
					usage.TotalTokens += int(e.TokenUsage.Last.TotalTokens)
					// Codex reports the window it resolved for this model here; zero
					// means it does not know one (fallback metadata), which disables
					// auto-compaction and is what makes a full-history rebuild racy
					// against the provider's real limit.
					if e.TokenUsage.ModelContextWindow > 0 {
						out.ContextWindow = int(e.TokenUsage.ModelContextWindow)
					}
				}
			case codexgo.ErrorEvent:
				if e.WillRetry {
					// The server retries this one in place and the turn keeps running
					// (upstream: "if true, this will not interrupt a turn"). It is a
					// notification, not a terminal state — treating it as terminal used
					// to abort turns that were about to resume.
					continue
				}
				// The detail is NESTED under `error` (TurnError); reading the flat
				// message field reported "no detail" for every real error. Classify it so
				// a caller can decide whether the failure is a provider one worth failing
				// over (see TurnError).
				kind, httpStatus := e.Classify()
				msg := e.Detail()
				if kind != codexgo.CodexErrorUnknown || msg != "" {
					return nil, &TurnError{Kind: kind, HTTPStatus: httpStatus, Message: msg}
				}
				// A non-retryable error with no message at all: keep the bare
				// notification and keep reading so that a failed turn/completed's own
				// turn.error can supply the richer cause (it wins in turnErrorDetail).
				if streamErr == nil {
					streamErr = errors.New("codexagent: the Codex server reported an error with no detail")
				}
			case codexgo.TurnCompletedEvent:
				completed = true
				// A terminal turn that failed or was interrupted is NOT success.
				if status := turnStatus(e); status == codexgo.TurnStatusFailed || status == codexgo.TurnStatusInterrupted {
					kind, httpStatus, _ := codexgo.ClassifyTurnError(e.Turn)
					return nil, &TurnError{
						Status:     string(status),
						Kind:       kind,
						HTTPStatus: httpStatus,
						Message:    turnErrorDetail(e.Turn, streamErr),
					}
				}
				// The authoritative answer is the turn's final agent message. Prefer
				// the completed message items (item/completed), then the turn's own
				// items, then the raw delta stream — which can carry earlier process
				// narration ("Searching…") and can miss text after a reconnect.
				res := &codexgo.TurnResult{Items: items, DeltaText: content.String()}
				if e.Turn != nil {
					res.Turn = *e.Turn
				}
				out.Content = res.FinalAgentText()
				if out.Content == "" {
					out.Content = content.String()
				}
				out.Reasoning = reasoning.String()
				out.Usage = usageOrNil(usage)
				out.ToolCallCounts, out.ToolCallErrors = tally.counts, tally.errors
				retrieval.apply(out)
				return out, nil
			}
		}
	}
}

func turnStatus(e codexgo.TurnCompletedEvent) codexgo.TurnStatus {
	if e.Turn != nil && e.Turn.Status != "" {
		return e.Turn.Status
	}
	return e.Status
}

// mcpToolRoundTrip reads a v2 mcpToolCall item — {server, tool, status, arguments,
// durationMs} — into a RoundTrip. An item that names no tool yields false, so a malformed
// payload produces no narration rather than an empty step.
func mcpToolRoundTrip(item codexgo.Item) (RoundTrip, bool) {
	var payload struct {
		Tool       string          `json:"tool"`
		Status     string          `json:"status"`
		Arguments  json.RawMessage `json:"arguments"`
		DurationMS int64           `json:"durationMs"`
	}
	if err := item.Decode(&payload); err != nil || payload.Tool == "" {
		return RoundTrip{}, false
	}
	// The v2 enum is camelCase ("inProgress"/"completed"/"failed"); normalize the
	// in-flight value to our constant so callers can branch on it.
	status := payload.Status
	if status == "" || status == "inProgress" {
		status = ToolStatusRunning
	}
	return RoundTrip{
		Tool:       payload.Tool,
		Kind:       RoundTripKindMCP,
		Args:       compactArgs(payload.Arguments),
		Status:     status,
		DurationMS: payload.DurationMS,
	}, true
}

// commandRoundTrip reads a v2 `commandExecution` item — {command, status, exitCode,
// durationMs, aggregatedOutput} — into a RoundTrip for the thinking stream. It is the local
// shell mode 8 now permits (approval `never`; the read-only, network-isolated sandbox is the
// boundary), used for arithmetic the model would otherwise have to do in its head.
//
// A command has no MCP tool name, so Tool is the fixed "shell" (a distinct key in
// tool_call_counts that never inflates the retrieval "Search Calls"). Status maps the
// command enum: a non-zero exit is a failure even when the server says "completed".
func commandRoundTrip(item codexgo.Item) (RoundTrip, bool) {
	var payload struct {
		Command    string `json:"command"`
		Status     string `json:"status"`
		ExitCode   *int   `json:"exitCode"`
		DurationMS int64  `json:"durationMs"`
	}
	if err := item.Decode(&payload); err != nil || strings.TrimSpace(payload.Command) == "" {
		return RoundTrip{}, false
	}
	status := payload.Status
	switch status {
	case "", "inProgress":
		status = ToolStatusRunning
	case "declined":
		// The sandbox refused it (a write outside the read-only profile, or the network).
		status = ToolStatusFailed
	case "completed":
		if payload.ExitCode != nil && *payload.ExitCode != 0 {
			status = ToolStatusFailed
		}
	}
	return RoundTrip{
		Tool:       commandToolName,
		Kind:       RoundTripKindCommand,
		Args:       compactString(payload.Command),
		Status:     status,
		DurationMS: payload.DurationMS,
		ExitCode:   payload.ExitCode,
	}, true
}

// builtinRoundTrip reports any OTHER Codex built-in action item briefly: plan (update_plan),
// fileChange (apply_patch), dynamicToolCall, webSearch, imageView, sleep, review mode,
// imageGeneration. These are narrated for visibility — the thinking stream should show every
// tool the model reached for, not only the MCP retrieval ones — so the detail is a
// best-effort one-liner rather than a typed model. The tool carries the one field that names
// what happened; failing that, a count of changes.
func builtinRoundTrip(item codexgo.Item) (RoundTrip, bool) {
	var payload map[string]any
	if err := item.Decode(&payload); err != nil {
		return RoundTrip{}, false
	}
	detail := ""
	for _, key := range []string{"prompt", "query", "command", "path", "tool", "name", "text", "review"} {
		if v, ok := payload[key].(string); ok && strings.TrimSpace(v) != "" {
			detail = v
			break
		}
	}
	if detail == "" {
		if changes, ok := payload["changes"].([]any); ok && len(changes) > 0 {
			detail = fmt.Sprintf("%d change(s)", len(changes))
		}
	}
	return RoundTrip{
		Tool:   "builtin:" + string(item.Kind),
		Kind:   RoundTripKindBuiltin,
		Args:   compactString(detail),
		Status: ToolStatusCompleted,
	}, true
}

// compactString renders a command line on one line and caps its length, the same display
// treatment compactArgs gives tool arguments.
func compactString(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const cap = 300
	if len(s) > cap {
		return s[:cap-3] + "..."
	}
	return s
}

// subAgentRoundTrip reads a v2 `subAgentActivity` item — the STARTED/COMPLETED marker for a
// sub-agent thread (features.multi_agent_v2). It is the only signal that a sub-agent was
// actually spawned: `spawn_agent` itself arrives as a plain Responses function call, which
// the app-server does not surface as a thread item, so without this a delegated run shows
// its `wait` steps but never the agents themselves. `agentPath` is the model's own name for
// the sub-agent (e.g. "/root/quantum_core").
//
// The two markers are DIFFERENT tools ("agent:spawn" / "agent:done") so the call tally reads
// "N agents started" rather than counting each agent twice.
func subAgentRoundTrip(item codexgo.Item) (RoundTrip, bool) {
	var payload struct {
		Kind      string `json:"kind"`
		AgentPath string `json:"agentPath"`
		Model     string `json:"model"`
	}
	if err := item.Decode(&payload); err != nil || strings.TrimSpace(payload.AgentPath) == "" {
		return RoundTrip{}, false
	}
	name, status := "spawn", ToolStatusRunning
	switch payload.Kind {
	case "completed":
		name, status = "done", ToolStatusCompleted
	case "failed", "interrupted":
		name, status = "done", ToolStatusFailed
	}
	return RoundTrip{
		Tool:   agentToolPrefix + name,
		Kind:   RoundTripKindAgent,
		Args:   compactString(payload.AgentPath),
		Status: status,
	}, true
}

// enabled by features.multi_agent_v2 — into a RoundTrip for the thinking stream. `tool` is
// the CollabAgentTool enum (spawnAgent, sendInput, resumeAgent, wait, closeAgent,
// sendMessage, followupTask, interruptAgent, listAgents) and `status` is
// CollabAgentToolCallStatus. It is reported under the "agent:" namespace so a sub-agent call
// is never mistaken for an MCP retrieval tool, and counted under the same key.
func collabRoundTrip(item codexgo.Item) (RoundTrip, bool) {
	var payload struct {
		Tool              string   `json:"tool"`
		Status            string   `json:"status"`
		Prompt            string   `json:"prompt"`
		ReceiverThreadIDs []string `json:"receiverThreadIds"`
	}
	if err := item.Decode(&payload); err != nil || strings.TrimSpace(payload.Tool) == "" {
		return RoundTrip{}, false
	}
	status := payload.Status
	switch status {
	case "", "inProgress":
		status = ToolStatusRunning
	case "completed":
		status = ToolStatusCompleted
	case "failed", "interrupted":
		status = ToolStatusFailed
	}
	// wait/listAgents/interrupt carry no prompt; show how many agents were targeted so the
	// line is still informative.
	args := compactString(payload.Prompt)
	if args == "" && len(payload.ReceiverThreadIDs) > 0 {
		args = fmt.Sprintf("%d agent(s)", len(payload.ReceiverThreadIDs))
	}
	return RoundTrip{
		Tool:   agentToolPrefix + payload.Tool,
		Kind:   RoundTripKindAgent,
		Args:   args,
		Status: status,
	}, true
}

// mcpToolOutput returns the text of an MCP tool call's result — what the bridge returned
// to Codex, which is the same rendered text the in-process engines classify. A result with
// no text content (an error, or a non-text block) yields "".
func mcpToolOutput(item codexgo.Item) string {
	var payload struct {
		Result *struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := item.Decode(&payload); err != nil || payload.Result == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range payload.Result.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

// compactArgs renders a call's arguments on one line and caps the length: the thinking
// stream is read by a human following along, and a search call can carry a long query list.
func compactArgs(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return ""
	}
	s := buf.String()
	if s == "null" {
		return ""
	}
	const max = 300
	if len(s) > max {
		s = s[:max] + "..."
	}
	return s
}

// usageOrNil reports the turn's usage, or nil when the server reported none — so a caller
// can distinguish "no accounting" from "zero tokens".
func usageOrNil(u TurnUsage) *TurnUsage {
	if u.LLMCalls == 0 {
		return nil
	}
	return &u
}

// reasoningItemText extracts a finished reasoning item's text (see the completed-item
// fallback in consume). The v2 reasoning item carries `summary` — a model-written summary,
// often empty for the non-OpenAI providers that trigger this path — and `content`, the raw
// reasoning those providers send. Prefer content; fall back to summary.
func reasoningItemText(item codexgo.Item) string {
	var payload struct {
		Summary []string `json:"summary"`
		Content []string `json:"content"`
	}
	if err := item.Decode(&payload); err != nil {
		return ""
	}
	parts := payload.Content
	if len(parts) == 0 {
		parts = payload.Summary
	}
	return strings.Join(parts, "")
}

// turnErrorDetail returns the most specific failure text available: the turn's own
// error payload when present, otherwise a detail-less error notification captured
// earlier, otherwise a fixed placeholder.
func turnErrorDetail(t *codexgo.Turn, streamErr error) string {
	if t != nil && len(t.Error) > 0 {
		return string(t.Error)
	}
	if streamErr != nil {
		return streamErr.Error()
	}
	return "no error detail"
}

// interrupt asks the server to stop a turn we have given up waiting on, in its own short
// deadline so a dead transport cannot wedge the caller.
func (r *Runner) interrupt(th *codexgo.SessionThread, turnID string) {
	if turnID == "" {
		return
	}
	ictx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = th.Interrupt(ictx, turnID)
}

func currentText(msg map[string]interface{}) string {
	if msg == nil {
		return ""
	}
	s, _ := msg["content"].(string)
	return s
}
