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

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"ragflow/internal/agentic_rag"
	"ragflow/internal/codexagent"
	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/mcp"
	"ragflow/internal/modelcontext"
	"ragflow/internal/permission"
	serverconfig "ragflow/internal/server/config"
)

// versionSegmentRe matches an API version path segment such as "/v1", "/v1beta" or
// "/v2/". Used to decide whether a provider base URL already names a version.
var versionSegmentRe = regexp.MustCompile(`/v[0-9]+[a-z0-9]*(/|$)`)

// normalizeResponsesBaseURL makes a provider base URL usable as the Codex Responses
// endpoint. The Codex client appends "/responses", so a base that carries no version
// segment — e.g. MiniMax's "https://api.minimaxi.com" (its chat URL is built from a
// separate "v1/text/chatcompletion_v2" suffix) — would call ".../responses" and 404.
// Append "/v1" unless the path already names a version (OpenAI's ".../v1" is left
// alone). Query/fragment are preserved and inspected apart from the path.
func normalizeResponsesBaseURL(raw string) string {
	base := strings.TrimSpace(raw)
	if base == "" {
		return base
	}
	suffix := ""
	if i := strings.IndexAny(base, "?#"); i >= 0 {
		suffix = base[i:]
		base = base[:i]
	}
	base = strings.TrimRight(base, "/")
	if versionSegmentRe.MatchString(base) {
		return base + suffix
	}
	return base + "/v1" + suffix
}

// codexConfig holds the mode 8 Codex configuration. It is installed once at server
// boot (see cmd/ragflow_server.go) and read on the chat path. A mutex makes the swap
// and the reads race-free.
var codexConfig struct {
	mu  sync.RWMutex
	cfg *serverconfig.CodexConfig
}

// SetCodexConfig installs the mode 8 configuration. A nil argument disables mode 8.
func SetCodexConfig(cfg *serverconfig.CodexConfig) {
	codexConfig.mu.Lock()
	codexConfig.cfg = cfg
	codexConfig.mu.Unlock()
}

// getCodexConfig returns the installed mode 8 configuration, or nil when none is set.
func getCodexConfig() *serverconfig.CodexConfig {
	codexConfig.mu.RLock()
	defer codexConfig.mu.RUnlock()
	return codexConfig.cfg
}

// codexConfigured reports whether mode 8 can run: a shared Codex app-server endpoint
// and the public MCP base URL are configured. Without them, AsyncChat drops an
// engineCodex turn to the regular pipeline rather than dispatching to an engine that
// cannot connect.
func codexConfigured() bool {
	cfg := getCodexConfig()
	return cfg != nil && cfg.Endpoint != "" && cfg.MCPPublicBase != ""
}

// codexRunner is the process-wide runner (one Codex connection, reused across turns and
// sessions), built lazily on first use.
var (
	codexRunnerMu sync.Mutex
	codexRunner   *codexagent.Runner
)

// codexBinder adapts the shared (KV-backed) mode 8 handle/binding store to the
// codexagent.TurnBinder seam — so codexagent depends only on the interface, not the bridge
// store — and owns the turn's keep-alive: Bind starts a goroutine that renews the handle
// TTL and the binding lease every codexLeaseRenewInterval, and Unbind stops it. Tying the
// heartbeat to the Bind/Unbind lifecycle means the lease is refreshed exactly while the
// binding is held, and a crashed turn's lease simply expires.
type codexBinder struct {
	mu     sync.Mutex
	leases map[string]func()
}

const codexLeaseRenewInterval = time.Minute

func newCodexBinder() *codexBinder {
	return &codexBinder{leases: map[string]func(){}}
}

func (b *codexBinder) Bind(ctx context.Context, handle string, binding any) error {
	scope, ok := binding.(mcp.CodexScope)
	if !ok {
		return fmt.Errorf("codex: invalid binding type %T", binding)
	}
	if err := mcp.DefaultCodexTickets().Bind(ctx, handle, scope); err != nil {
		return err
	}
	b.startLease(handle)
	return nil
}

func (b *codexBinder) Unbind(ctx context.Context, handle string) {
	b.stopLease(handle)
	mcp.DefaultCodexTickets().Unbind(ctx, handle)
}

// startLease runs the keep-alive heartbeat for handle until stopLease(handle) is called.
func (b *codexBinder) startLease(handle string) {
	stop := make(chan struct{})
	b.mu.Lock()
	if prev := b.leases[handle]; prev != nil {
		prev() // defensive: Bind is exclusive, so this should not happen
	}
	b.leases[handle] = func() { close(stop) }
	b.mu.Unlock()

	go func() {
		ticker := time.NewTicker(codexLeaseRenewInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				mcp.DefaultCodexTickets().Renew(context.Background(), handle)
			}
		}
	}()
}

func (b *codexBinder) stopLease(handle string) {
	b.mu.Lock()
	stop := b.leases[handle]
	delete(b.leases, handle)
	b.mu.Unlock()
	if stop != nil {
		stop()
	}
}

// getCodexRunner builds the process-wide runner on first use. The initial connection has
// its own bounded deadline — a stalled handshake must not block every mode 8 request
// forever — but the client built on success lives for the process: it is deliberately NOT
// bound to a request context, and the transport reconnects on its own. A failure is not
// cached: the next turn retries.
func getCodexRunner() (*codexagent.Runner, error) {
	codexRunnerMu.Lock()
	defer codexRunnerMu.Unlock()
	if codexRunner != nil {
		return codexRunner, nil
	}
	cfg := getCodexConfig()
	if cfg == nil {
		return nil, fmt.Errorf("codex (mode 8) is not configured")
	}
	dialCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, err := codexagent.Dial(dialCtx, codexagent.ClientConfig{
		Endpoint:    cfg.Endpoint,
		BearerToken: cfg.BearerToken,
	})
	if err != nil {
		return nil, fmt.Errorf("codex dial: %w", err)
	}
	codexRunner = codexagent.NewRunner(client, dao.NewCodexThreadStore(dao.DB), newCodexBinder())
	return codexRunner, nil
}

// InstallCodexWebSearchResolver wires the MCP bridge's web-search seam. The bridge must
// not import this layer, so the API process installs a resolver that reads the turn's
// conversation and builds its provider callback on demand. Call once at boot.
//
// The callback is rebuilt per request from the DB and never persisted, so a turn bound
// on one replica still resolves its provider when the MCP request lands on another — and
// the provider's credentials never enter the shared KV.
func InstallCodexWebSearchResolver() {
	svc := NewChatPipelineService()
	mcp.SetCodexWebSearchResolver(func(ctx context.Context, scope mcp.CodexScope) agentic_rag.WebSearchFunc {
		if scope.ChatID == "" || dao.DB == nil {
			return nil
		}
		chat, err := dao.NewChatDAO().GetByID(ctx, dao.DB, scope.ChatID)
		if err != nil || chat == nil {
			common.WarnCtx(ctx, "codex: cannot resolve chat for web search",
				zap.String("chat_id", scope.ChatID), zap.Error(err))
			return nil
		}
		// nil when the conversation has no web-search provider: the bridge then
		// simply does not advertise the tool.
		return svc.webSearchFunc(chat.PromptConfig)
	})
}

// codexProviderKnownCapable reports whether a provider is in the maintained set of
// providers KNOWN to speak the Responses wire API Codex requires. It is only a skip-probe
// fast path: an unknown provider is not refused, it is probed (codexResponsesUsable). An
// unset or empty set means nothing is known capable, so every provider is probed.
func codexProviderKnownCapable(providerName string) bool {
	cfg := getCodexConfig()
	if cfg == nil || len(cfg.ResponsesProviders) == 0 {
		return false
	}
	name := strings.ToLower(providerName)
	for _, p := range cfg.ResponsesProviders {
		if p == name {
			return true
		}
	}
	return false
}

// codexResponsesProbeControlPath is a path no provider serves. The probe compares the
// "/responses" status against this control: only a difference proves the Responses route
// was routed somewhere other than the catch-all.
const codexResponsesProbeControlPath = "/__codex_responses_probe_absent__"

const (
	// codexResponsesProbeTTL bounds how long a probe verdict is reused. Positive and
	// negative results are both cached, so an unavailable endpoint is not re-probed on
	// every mode-8 turn.
	codexResponsesProbeTTL = 10 * time.Minute
	// codexResponsesProbeTimeout bounds ONE probe request. The probe makes two, so a
	// cold cache costs at most ~2x this on the first mode-8 turn per endpoint.
	codexResponsesProbeTimeout = 5 * time.Second
)

// codexResponsesProbeClient performs the probe. The scheme-safe client (not the strict
// SSRF client) is required: the providers that reach probing are exactly the custom/local
// ones whose base_url may point at a private address or loopback, and it honours
// ProxyFromEnvironment like the model drivers do. Overridable in tests.
var codexResponsesProbeClient = common.GetSchemeSafeHTTPClient()

// codexResponsesProbeEntry is one cached verdict.
type codexResponsesProbeEntry struct {
	exists    bool
	expiresAt time.Time
}

var codexResponsesProbeCache = struct {
	mu sync.Mutex
	m  map[string]codexResponsesProbeEntry
}{m: map[string]codexResponsesProbeEntry{}}

// resetCodexResponsesProbeCache clears the probe cache. Test-only.
func resetCodexResponsesProbeCache() {
	codexResponsesProbeCache.mu.Lock()
	codexResponsesProbeCache.m = map[string]codexResponsesProbeEntry{}
	codexResponsesProbeCache.mu.Unlock()
}

// codexResponsesProbeKey keys a verdict by endpoint AND credential: the same base_url with
// a different (e.g. a stale/rotated) key can probe differently under an auth-first gateway,
// so the two must not share a cache slot. The key is hashed so the raw secret is not kept
// in memory for the TTL.
func codexResponsesProbeKey(baseURL, apiKey string) string {
	sum := sha256.Sum256([]byte(apiKey))
	return baseURL + "\x00" + hex.EncodeToString(sum[:8])
}

func codexResponsesProbeLookup(baseURL, apiKey string) (bool, bool) {
	now := time.Now()
	codexResponsesProbeCache.mu.Lock()
	defer codexResponsesProbeCache.mu.Unlock()
	entry, ok := codexResponsesProbeCache.m[codexResponsesProbeKey(baseURL, apiKey)]
	if !ok || now.After(entry.expiresAt) {
		return false, false
	}
	return entry.exists, true
}

func codexResponsesProbeStore(baseURL, apiKey string, exists bool) {
	codexResponsesProbeCache.mu.Lock()
	codexResponsesProbeCache.m[codexResponsesProbeKey(baseURL, apiKey)] = codexResponsesProbeEntry{
		exists:    exists,
		expiresAt: time.Now().Add(codexResponsesProbeTTL),
	}
	codexResponsesProbeCache.mu.Unlock()
}

// probeResponsesPathStatus POSTs an empty body to rawURL with the instance's own key and
// returns the HTTP status. ok is false when the request could not be completed (dial
// error, timeout, malformed URL), which the caller treats as "cannot confirm". The body is
// deliberately empty: it routes to the handler (so validation/auth answers) without asking
// the model to generate anything.
func probeResponsesPathStatus(ctx context.Context, rawURL, apiKey string) (int, bool) {
	reqCtx, cancel := context.WithTimeout(ctx, codexResponsesProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, rawURL, strings.NewReader("{}"))
	if err != nil {
		return 0, false
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := codexResponsesProbeClient.Do(req)
	if err != nil {
		return 0, false
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	return resp.StatusCode, true
}

// codexResponsesRouteExists reports whether baseURL exposes a Responses endpoint Codex can
// POST to. It is a DIFFERENTIAL probe: it sends the identical request to "/responses" and to
// a path nothing serves, and counts the route as present only when the two statuses differ.
//
// The differential is what makes an auth-first gateway readable: with a valid key it answers
// "/responses" differently from the catch-all (typically 400 vs 404), whereas a gateway
// without the route answers the same 404 to both — and a gateway that checks auth before
// routing answers the same 401 to both. The instance's real API key must be sent, or an
// auth-first gateway collapses both answers to 401 and the route looks absent.
func codexResponsesRouteExists(ctx context.Context, baseURL, apiKey string) bool {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return false
	}
	if exists, ok := codexResponsesProbeLookup(base, apiKey); ok {
		return exists
	}
	probeStatus, probeOK := probeResponsesPathStatus(ctx, base+"/responses", apiKey)
	controlStatus, controlOK := probeResponsesPathStatus(ctx, base+codexResponsesProbeControlPath, apiKey)
	exists := probeOK && controlOK && probeStatus != controlStatus
	// Log only on a real probe (a cache hit returns above), so the dynamic decision — the
	// thing that used to be invisible when mode 8 silently degraded — is diagnosable.
	common.InfoCtx(ctx, "codex (mode 8): probed the dialog model endpoint for a Responses route",
		zap.String("base_url", base),
		zap.Int("responses_status", probeStatus),
		zap.Int("control_status", controlStatus),
		zap.Bool("responses_reachable", probeOK),
		zap.Bool("control_reachable", controlOK),
		zap.Bool("responses_supported", exists))
	codexResponsesProbeStore(base, apiKey, exists)
	return exists
}

// codexResponsesUsable reports whether a resolved target can carry a mode-8 turn, and why
// not when it cannot. A provider in the known-capable set is trusted outright; any other
// provider is probed for a Responses route at its instance base_url. The reason string is
// for the log and the failover narration.
func codexResponsesUsable(ctx context.Context, target *ResolvedModelTarget) (string, bool) {
	if target == nil {
		return "the model target could not be resolved", false
	}
	if strings.TrimSpace(target.BaseURL) == "" {
		return "the model instance does not declare a base_url", false
	}
	name := ""
	if target.Driver != nil {
		name = target.Driver.Name()
	}
	if codexProviderKnownCapable(name) {
		return "", true
	}
	if codexResponsesRouteExists(ctx, target.BaseURL, target.APIKey) {
		return "", true
	}
	return fmt.Sprintf("provider %q is not in codex.responses_providers and %s exposes no Responses route", name, target.BaseURL), false
}

// codexResponsesCapable reports whether the dialog's model provider can carry a mode-8
// turn: either it is in the known-capable set, or its instance base_url exposes a Responses
// route (see codexResponsesUsable). Otherwise mode 8 falls back to the regular pipeline.
func (s *ChatPipelineService) codexResponsesCapable(ctx context.Context, userID string, chat *entity.Chat) bool {
	target, err := s.resolveCodexModelTarget(ctx, userID, chat)
	if err != nil || target == nil {
		common.WarnCtx(ctx, "codex (mode 8) could not resolve the dialog model; using the regular pipeline",
			zap.String("chat_id", chat.ID), zap.Error(err))
		return false
	}
	reason, ok := codexResponsesUsable(ctx, target)
	if !ok {
		common.WarnCtx(ctx, "codex (mode 8) requires a Responses-capable dialog model; using the regular pipeline",
			zap.String("chat_id", chat.ID), zap.String("provider", target.ProviderName), zap.String("reason", reason))
	}
	return ok
}

// resolveCodexModelTarget resolves the dialog's chat model (or the tenant default) to its
// provider details. Unlike the general model path (which only needs a constructed model),
// mode 8 must hand Codex the provider driver, endpoint and token, so it uses the
// ModelFactory's credential-exposing accessor.
func (s *ChatPipelineService) resolveCodexModelTarget(ctx context.Context, userID string, chat *entity.Chat) (*ResolvedModelTarget, error) {
	// The model factory resolves access from the context identity; bind the
	// turn's user and the dialog's tenant before resolving.
	ctx = permission.WithIdentity(ctx, userID, chat.TenantID)
	var (
		target *ResolvedModelTarget
		err    error
	)
	if chat.LLMID == "" {
		target, err = s.ModelFactory.ResolveDefaultChatTarget(ctx)
	} else {
		target, err = s.ModelFactory.ResolveChatTarget(ctx, chat.LLMID)
	}
	if err != nil || target == nil {
		return target, err
	}
	// Codex appends "/responses" to this base; normalize the version segment first.
	target.BaseURL = normalizeResponsesBaseURL(target.BaseURL)
	return target, nil
}

// codexTurnTarget is one provider a mode-8 turn may run on: the dialog's own model, or a member
// of the dialog's failover roster.
type codexTurnTarget struct {
	Target *ResolvedModelTarget
	// ContextWindow and MaxOutput are resolved with THIS candidate's llm id, so a failover
	// thread tells Codex the right window instead of the primary's.
	ContextWindow int
	MaxOutput     int
}

// codexTurnTargets returns the ordered providers a mode-8 turn may use: the dialog's own model
// first, then the members of the dialog's `llm_setting.failover_llm_ids` — the same roster the
// in-process eino engines fail over with (agenticModelChain). The roster is best-effort: a
// member that cannot be resolved, cannot carry a Responses turn, or duplicates an earlier
// candidate is skipped with a warning rather than failing the turn.
func (s *ChatPipelineService) codexTurnTargets(ctx context.Context, userID string, chat *entity.Chat, primary *ResolvedModelTarget) []codexTurnTarget {
	// The failover lookups below resolve access from the context identity, the
	// same way resolveCodexModelTarget does for the primary.
	ctx = permission.WithIdentity(ctx, userID, chat.TenantID)
	targets := []codexTurnTarget{s.codexTurnTargetFor(ctx, chat.TenantID, chat.LLMID, primary)}
	seen := map[string]bool{}
	if primary != nil {
		seen[candidateKey(primary)] = true
	}
	for _, llmID := range agenticFailoverModelIDs(chat) {
		if llmID == "" || llmID == chat.LLMID {
			continue
		}
		target, err := s.ModelFactory.ResolveChatTarget(ctx, llmID)
		if err != nil || target == nil {
			common.WarnCtx(ctx, "codex: cannot resolve a failover model; skipping it",
				zap.String("chat_id", chat.ID), zap.String("llm_id", llmID), zap.Error(err))
			continue
		}
		target.BaseURL = normalizeResponsesBaseURL(target.BaseURL)
		if reason, ok := codexResponsesUsable(ctx, target); !ok {
			common.WarnCtx(ctx, "codex: failover model cannot carry a Responses turn; skipping it",
				zap.String("chat_id", chat.ID), zap.String("llm_id", llmID), zap.String("reason", reason))
			continue
		}
		if key := candidateKey(target); seen[key] {
			continue
		} else {
			seen[key] = true
		}
		targets = append(targets, s.codexTurnTargetFor(ctx, chat.TenantID, llmID, target))
	}
	if len(targets) > 1 {
		common.InfoCtx(ctx, "codex: failover chain built",
			zap.String("chat_id", chat.ID), zap.Int("candidates", len(targets)))
	}
	return targets
}

// codexTurnTargetFor pairs a resolved target with the context window and output cap its model
// declares in the provider catalog. Zero means "unknown, leave Codex's own value".
func (s *ChatPipelineService) codexTurnTargetFor(ctx context.Context, tenantID, llmID string, target *ResolvedModelTarget) codexTurnTarget {
	t := codexTurnTarget{Target: target}
	if target == nil {
		return t
	}
	t.ContextWindow = modelcontext.ResolveModelContentLength(ctx, nil, tenantID, llmID, target.Driver.Name(), target.ModelName)
	t.MaxOutput = modelcontext.ResolveModelMaxOutput(ctx, nil, tenantID, llmID, target.Driver.Name(), target.ModelName)
	return t
}

// candidateKey identifies a candidate for dedup: the endpoint, the model it serves, and the
// CREDENTIAL that serves it.
//
// The credential is part of the identity because the failover group this exists for is
// exactly "one vendor, several keys": instances of a provider routinely share the base_url
// and differ only by API key (a per-key rate limit is what such a roster works around).
// Keying on (base_url, model) alone made every such roster member a duplicate of the
// primary, so a configured failover_llm_ids silently collapsed to a one-candidate chain.
func candidateKey(target *ResolvedModelTarget) string {
	return target.BaseURL + "\x00" + target.ModelName + "\x00" + credentialFingerprint(target.APIKey)
}

// credentialFingerprint identifies a credential without keeping the secret itself in the
// dedup key. An empty key hashes like any other, so keyless targets still compare equal.
func credentialFingerprint(apiKey string) string {
	sum := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(sum[:8])
}

// codexFailoverPlan returns the candidate indexes a turn attempts: one full circle starting
// at current, so the tail candidate is followed by the head and every candidate is tried
// exactly once.
func codexFailoverPlan(candidates, current int) []int {
	if candidates <= 0 {
		return nil
	}
	if current < 0 {
		current = 0
	}
	start := current % candidates
	plan := make([]int, candidates)
	for i := range plan {
		plan[i] = (start + i) % candidates
	}
	return plan
}

// codexProviderCursor remembers, per chat, which failover candidate last COMPLETED a turn.
//
// The next turn starts its circle there, so a conversation sticks to a provider that works
// instead of paying for a dead primary on every turn. It is a routing hint only: a failed
// attempt never moves the cursor, and a chat with no entry starts at the dialog's own model.
// The map is process-local and keyed by chat id, so a restart simply falls back to the
// primary.
type codexProviderCursor struct {
	mu   sync.Mutex
	last map[string]string
}

// Start returns the candidate index the circle begins at for chatID.
func (c *codexProviderCursor) Start(chatID string, candidates []codexTurnTarget) int {
	if c == nil || chatID == "" || len(candidates) == 0 {
		return 0
	}
	c.mu.Lock()
	key := c.last[chatID]
	c.mu.Unlock()
	if key == "" {
		return 0
	}
	for i, candidate := range candidates {
		if candidate.Target != nil && candidateKey(candidate.Target) == key {
			return i
		}
	}
	return 0
}

// Mark records the candidate that just completed a turn.
func (c *codexProviderCursor) Mark(chatID string, target *ResolvedModelTarget) {
	if c == nil || chatID == "" || target == nil {
		return
	}
	key := candidateKey(target)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.last == nil {
		c.last = make(map[string]string, 4)
	}
	c.last[chatID] = key
}

// codexCursors is the process-wide routing hint shared by every mode-8 turn.
var codexCursors = &codexProviderCursor{}

// codexFailoverEligible reports whether a failed turn should be retried on the next provider,
// returning the typed failure for the log and the thinking stream. It refuses the cases where a
// retry is wrong rather than merely unhelpful:
//
//   - the per-session binding is held: that is a lock, not a provider failure, and retrying
//     would race the turn that holds it;
//   - the request context is done: the client is gone, so a retry has nobody to answer;
//   - answer text already reached the client: a second attempt would append a second answer
//     after the first, and the first is already the user's;
//   - the failure is not typed as a provider one (see codexagent.TurnError, whose classification
//     is deliberately conservative).
func codexFailoverEligible(ctx context.Context, runErr error, emittedContent int) (*codexagent.TurnError, bool) {
	if runErr == nil || ctx.Err() != nil || emittedContent > 0 {
		return nil, false
	}
	if errors.Is(runErr, mcp.ErrCodexTurnBusy) {
		return nil, false
	}
	turnErr, ok := codexagent.AsTurnError(runErr)
	if !ok || !turnErr.RetryableProviderFailure() {
		return nil, false
	}
	return turnErr, true
}

// failoverReason renders a typed turn failure for the thinking stream.
func failoverReason(turnErr *codexagent.TurnError) string {
	if turnErr == nil {
		return "unknown error"
	}
	switch {
	case turnErr.Kind != "" && turnErr.HTTPStatus != 0:
		return fmt.Sprintf("%s (http %d)", turnErr.Kind, turnErr.HTTPStatus)
	case turnErr.Kind != "":
		return string(turnErr.Kind)
	default:
		return turnErr.Message
	}
}

// codexTargetLabel names a candidate for humans: the model, qualified by the instance serving
// it. Two failover candidates routinely share provider and model and differ only by instance —
// that IS the situation a failover exists for — so without the qualification the narration
// degenerates to "<model> failed; retrying on <model>".
func codexTargetLabel(target *ResolvedModelTarget) string {
	if target == nil {
		return "unknown model"
	}
	if target.InstanceName == "" {
		return target.ModelName
	}
	return fmt.Sprintf("%s(%s)", target.ModelName, target.InstanceName)
}

// codexAgent runs one mode 8 (Codex) turn: it brokers the session's Codex thread (via
// internal/codexagent) and lets Codex call the agent_rag retrieval tools through this
// process's MCP bridge.
//
// It mirrors the channel contract of iterativeSynthesis/agenticRag: reasoning deltas
// wrapped in <think> boundaries, then answer deltas, then a Final result carrying the
// complete answer and the citation payload.
func (s *ChatPipelineService) codexAgent(
	ctx context.Context,
	userID string,
	chat *entity.Chat,
	messages []map[string]interface{},
	stream bool,
	kwargs map[string]interface{},
	useWebSearch bool,
	quote bool,
) (<-chan AsyncChatResult, error) {
	cfg := getCodexConfig()
	if cfg == nil || cfg.Endpoint == "" || cfg.MCPPublicBase == "" {
		return nil, fmt.Errorf("codex (mode 8) is not configured")
	}
	target, err := s.resolveCodexModelTarget(ctx, userID, chat)
	if err != nil || target == nil {
		return nil, fmt.Errorf("codex: resolve dialog model: %w", err)
	}
	if strings.TrimSpace(target.BaseURL) == "" {
		return nil, fmt.Errorf("codex: the dialog model instance must set base_url to a Responses-capable endpoint")
	}

	sessionID := common.SessionIDFromContext(ctx)
	// A non-persistent request (store_history_messages=false) supplies its full history
	// every turn and stores nothing, so it must not own a persistent thread: force the
	// one-shot path (see the plan, §5.2 / P1#7).
	if store, storeErr := ResolveStoreHistoryMessages(kwargs); storeErr == nil && !store {
		sessionID = ""
	}
	datasetIDs := chatKBIDs(chat)

	// The ordered providers this turn may run on: the dialog's own model first, then its
	// failover roster. A provider failure advances to the next candidate (see
	// codexFailoverEligible). Each candidate carries its model's real context window and output
	// cap from the provider catalog (conf/all_models.json): Codex resolves any model outside its
	// own manifest to fallback metadata with no context window — and then neither knows the
	// limit nor auto-compacts (context_window.rs), so a full-history rebuild could overflow the
	// provider's real window and fail there. Zero means "unknown, leave Codex's own value".
	candidates := s.codexTurnTargets(ctx, userID, chat, target)

	runner, err := getCodexRunner()
	if err != nil {
		return nil, err
	}

	out := make(chan AsyncChatResult, 16)
	go func() {
		defer close(out)
		started := time.Now()

		// A stable per-session handle is baked into the Codex thread's MCP URL. It carries
		// no authority: the turn's scope is bound to it for the duration of the turn (by
		// the runner, under the session lock) and removed when the turn ends, so the URL
		// authorizes nothing between turns.
		handle, handleErr := mcp.DefaultCodexTickets().HandleForSession(ctx, chat.TenantID, sessionID)
		if handleErr != nil {
			out <- AsyncChatResult{Answer: fmt.Sprintf("**ERROR**: codex: acquire MCP handle: %v", handleErr), Final: true}
			return
		}
		mcpURL := cfg.MCPPublicBase + "/mcp/codex/" + handle
		// A one-shot turn (no session) owns a throwaway handle, revoked at the end. A
		// session handle outlives the turn — later turns reuse it and the session delete
		// revokes it.
		if sessionID == "" {
			defer mcp.DefaultCodexTickets().RevokeHandle(context.Background(), handle)
		}

		var (
			thinking     bool
			contentDelta strings.Builder
		)
		// The last byte written to the reasoning stream, so a narrated tool step can be
		// separated from the text around it by EXACTLY one newline: the model's reasoning
		// runs straight into the step otherwise ("...characteristics.search_bm25_chunks
		// {...}"), which is unreadable. A fresh think block starts at a line boundary.
		var reasoningTail byte
		emitReasoning := func(text string) {
			if text == "" {
				return
			}
			if !thinking {
				out <- AsyncChatResult{Final: false, StartToThink: true}
				thinking = true
				reasoningTail = 0
			}
			out <- AsyncChatResult{Final: false, Reasoning: text}
			reasoningTail = text[len(text)-1]
		}
		emitContent := func(text string) {
			if text == "" {
				return
			}
			if thinking {
				out <- AsyncChatResult{Final: false, EndToThink: true}
				thinking = false
			}
			contentDelta.WriteString(text)
			out <- AsyncChatResult{Final: false, Answer: text}
		}
		// A retrieval step rides the thinking stream: mode 8 has no client-side tool loop,
		// so without this a user watching the run sees reasoning but never which tool the
		// model reached for or with what arguments. The in-process engines narrate steps
		// into their thinking text for the same reason; the structured ThinkEvent travels
		// alongside for a client that renders steps rather than paragraphs.
		emitTool := func(rt codexagent.RoundTrip) {
			// A local shell execution is narrated like a tool step, but under its own
			// stage and with exit-status wording: a command's outcome is a process exit,
			// not a tool result.
			stage, line := "MCP", toolNarration(rt)
			switch rt.Kind {
			case codexagent.RoundTripKindCommand:
				stage, line = "shell", commandNarration(rt)
			case codexagent.RoundTripKindAgent:
				stage, line = "agent", agentNarration(rt)
			case codexagent.RoundTripKindBuiltin:
				stage, line = "builtin", builtinNarration(rt)
			}
			if line == "" {
				return
			}
			if !thinking {
				out <- AsyncChatResult{Final: false, StartToThink: true}
				thinking = true
				reasoningTail = 0
			}
			out <- AsyncChatResult{
				Final:     false,
				Reasoning: narrationChunk(reasoningTail, line),
				ThinkEvent: &ThinkEvent{
					Kind:       toolThinkKind(rt.Status),
					Stage:      stage,
					Tool:       rt.Tool,
					Args:       rt.Args,
					Status:     rt.Status,
					DurationMS: rt.DurationMS,
					Summary:    line,
				},
			}
			reasoningTail = '\n'
		}
		// A context compaction is narrated like a tool step. It is a real event in the
		// turn — Codex summarized the model's history to fit the window — and the think
		// panel is the only place a user watches the run, so it belongs there. It does
		// not change how the thread is rebuilt: mode 8 never trims a rebuilt thread's
		// history, so RAGFlow's stored conversation keeps every turn.
		emitCompaction := func() {
			line := "context compacted: Codex summarized the earlier conversation to fit the model's context window"
			if !thinking {
				out <- AsyncChatResult{Final: false, StartToThink: true}
				thinking = true
				reasoningTail = 0
			}
			out <- AsyncChatResult{
				Final:     false,
				Reasoning: narrationChunk(reasoningTail, line),
				ThinkEvent: &ThinkEvent{
					Kind:    "context_compaction",
					Stage:   "context",
					Summary: line,
				},
			}
			reasoningTail = '\n'
		}
		// A provider failover is a model-level event, like a compaction: narrate it into the
		// thinking stream (and the log) so a user watching the panel learns the turn restarted
		// on another model rather than staring at a gap.
		emitFailover := func(from, to *ResolvedModelTarget, turnErr *codexagent.TurnError) {
			line := fmt.Sprintf("provider failover: %s failed (%s); retrying on %s",
				codexTargetLabel(from), failoverReason(turnErr), codexTargetLabel(to))
			common.WarnCtx(ctx, "codex: provider failover",
				zap.String("chat_id", chat.ID),
				zap.String("from", codexTargetLabel(from)),
				zap.String("from_provider", from.Driver.Name()),
				zap.String("to", codexTargetLabel(to)),
				zap.String("kind", string(turnErr.Kind)),
				zap.Int("http_status", turnErr.HTTPStatus),
				zap.Error(turnErr))
			if !thinking {
				out <- AsyncChatResult{Final: false, StartToThink: true}
				thinking = true
				reasoningTail = 0
			}
			out <- AsyncChatResult{
				Final:     false,
				Reasoning: narrationChunk(reasoningTail, line),
				ThinkEvent: &ThinkEvent{
					Kind:    "provider_failover",
					Stage:   "provider",
					Summary: line,
				},
			}
			reasoningTail = '\n'
		}

		// Everything that does not depend on which provider answers the turn. A provider cannot
		// be changed on an existing Codex thread, so each attempt below is its own request.
		base := codexagent.TurnRequest{
			TenantID:        chat.TenantID,
			SessionID:       sessionID,
			DatasetIDs:      datasetIDs,
			WebSearch:       useWebSearch,
			ReasoningEffort: cfg.ReasoningEffort,
			SandboxMode:     cfg.SandboxMode,
			MCPURL:          mcpURL,
			Ticket:          handle,
			// The binding carries the conversation id and this turn's web-search
			// decision; the bridge resolves the provider from them at request time,
			// so the provider's credentials stay in the DB and never reach the
			// KV-serialized binding (see D11).
			Binding: mcp.CodexScope{
				TenantID:   chat.TenantID,
				DatasetIDs: datasetIDs,
				UserID:     userID,
				ChatID:     chat.ID,
				WebSearch:  useWebSearch,
			},
			// The root thread's contract; a delegated sub-agent gets the child-scoped one
			// through Codex's subagent_developer_instructions seam (see codexSubagentContract).
			Instructions:         codexRootContract(quote),
			SubagentInstructions: codexSubagentContract(quote),
			TurnTimeout:          cfg.TurnTimeout,
			Messages:             messages,
			OnContent:            emitContent,
			OnReasoning:          emitReasoning,
			OnTool:               emitTool,
			OnCompaction:         emitCompaction,
			// A thread is only resumed when its stored handle is still alive; otherwise
			// its baked-in MCP URL would 401 (a restart lost the handle) and the
			// retrieval path would silently break.
			HandleAlive: func(tok string) bool {
				alive, _ := mcp.DefaultCodexTickets().HandleAlive(ctx, tok)
				return alive
			},
		}

		// Walk one full circle of the candidates, STARTING at the one that last completed a
		// turn for this chat: a provider that answered 429/500 earlier may be fine minutes
		// later, and re-hitting a dead primary on every turn only burns latency. A failed
		// attempt does not move the start — only a completed turn does (codexCursors.Mark).
		//
		// The attempt on the dialog's own model keeps the normal thread-reuse path; starting
		// on a failover member (sticky start) and every retry must be bound to ITS provider,
		// so they rebuild and leave the stored mapping alone (ForceRebuild) — see
		// codexFailoverEligible for when a retry is allowed at all.
		//
		// Each per-thread provider declares exactly the instance's base_url (normalized to the
		// versioned Responses endpoint Codex appends "/responses" to); there is deliberately no
		// fallback to the provider factory's default URL (see the plan, §13 / P2-13).
		var (
			outcome *codexagent.TurnOutcome
			runErr  error
		)
		plan := codexFailoverPlan(len(candidates), codexCursors.Start(chat.ID, candidates))
		for attempt, idx := range plan {
			candidate := candidates[idx]
			req := base
			req.Model = candidate.Target.ModelName
			req.ProviderID = candidate.Target.Driver.Name()
			req.ProviderBaseURL = candidate.Target.BaseURL
			req.ProviderToken = candidate.Target.APIKey
			req.ModelContextWindow = candidate.ContextWindow
			req.ModelMaxOutput = candidate.MaxOutput
			req.ForceRebuild = attempt > 0 || idx != 0

			outcome, runErr = runner.RunTurn(ctx, req)
			if runErr == nil {
				codexCursors.Mark(chat.ID, candidate.Target)
				break
			}
			turnErr, eligible := codexFailoverEligible(ctx, runErr, contentDelta.Len())
			if !eligible || attempt == len(plan)-1 {
				break
			}
			emitFailover(candidate.Target, candidates[plan[attempt+1]].Target, turnErr)
		}
		if runErr != nil {
			if thinking {
				out <- AsyncChatResult{Final: false, EndToThink: true}
				thinking = false
			}
			// The binding is the per-session turn lock: a losing turn reports the session
			// as busy rather than clobbering the running one.
			msg := runErr.Error()
			if errors.Is(runErr, mcp.ErrCodexTurnBusy) {
				msg = fmt.Sprintf("session %s is busy with another turn; try again when it finishes", sessionID)
			}
			out <- AsyncChatResult{Answer: fmt.Sprintf("**ERROR**: %s", msg), Final: true}
			return
		}
		// The runner has already unbound this turn's scope; a session handle lives on for
		// later turns, and a one-shot handle was revoked by the deferred cleanup above.

		if thinking {
			out <- AsyncChatResult{Final: false, EndToThink: true}
			thinking = false
		}
		// Prefer the runner's authoritative answer (the turn's final agent message); the
		// delta accumulation can include process narration and can miss text after a
		// reconnect.
		final := outcome.Content
		if final == "" {
			final = contentDelta.String()
		}
		reference := map[string]interface{}{}
		if quote {
			// Citations come from the model's provenance tuples: a corpus chunk_id is
			// verified against this conversation's datasets (fetchChunksByIDs); a web url
			// is the model's own claim, since the turn records nothing about what a search
			// served. There is deliberately no served-chunk ledger, so a corpus chunk the
			// model names but never retrieved still resolves if it is in scope (D12).
			reference, final = s.buildCodexReference(ctx, chat.TenantID, datasetIDs, final)
		}
		// Codex reports one token-usage notification per model call; the runner sums them
		// into the same shape the in-process engines produce, so a mode-8 benchmark row
		// carries prompt/completion/total/llm_calls exactly like mode 5's.
		var usage *TurnUsage
		if outcome.Usage != nil {
			usage = &TurnUsage{
				PromptTokens:     outcome.Usage.PromptTokens,
				CompletionTokens: outcome.Usage.CompletionTokens,
				TotalTokens:      outcome.Usage.TotalTokens,
				LLMCalls:         outcome.Usage.LLMCalls,
			}
		}
		out <- AsyncChatResult{
			Answer:              final,
			Reference:           reference,
			Final:               true,
			Usage:               usage,
			ToolCallCounts:      outcome.ToolCallCounts,
			ToolCallErrors:      outcome.ToolCallErrors,
			RetrievedDocIDs:     outcome.RetrievedDocIDs,
			ServedDocIDs:        outcome.ServedDocIDs,
			DeepReadChunks:      outcome.DeepReadChunks,
			ShallowReadChunks:   outcome.ShallowReadChunks,
			DeepReadChunkIDs:    outcome.DeepReadChunkIDs,
			ShallowReadChunkIDs: outcome.ShallowReadChunkIDs,
			Compactions:         outcome.Compactions,
			ElapsedSeconds:      time.Since(started).Seconds(),
		}
	}()
	return out, nil
}

// codexRootContract is the ROOT thread's citation contract (see the plan, D5/§4.7). It is
// delivered as DEVELOPER instructions — additive to Codex's own prompt — and covers the
// tool split the model would otherwise get wrong plus the provenance format (the model ENDS
// each sourced line with that passage's tuple, and the backend, not the model, assigns
// [ID:N]).
//
// It is DELIBERATELY generic about web search. The contract is baked into a thread at
// create and thread/resume does not reload it, while web search is a per-turn decision;
// encoding the decision here would make the prompt and the tool list two copies of one fact
// that must be kept in agreement. Instead the MCP tool list is the single source of truth —
// the bridge advertises its web search tool only when the conversation enables it — and the
// prompt points at that list ("use exactly the retrieval tools it contains"). The prompt
// therefore never goes stale: it is the same whether web search is on or off.
//
// The shell is a LOCAL COMPUTATION channel, not a retrieval one (see codexShellPolicy).
//
// It carries NO instruction about the answer's prose shape: Codex's own agentic prompt
// already produces prose, so a "answer the user in prose" line earns nothing here and is
// exactly the root-oriented phrasing that mis-fits a sub-agent.
func codexRootContract(quote bool) string {
	var b strings.Builder
	b.WriteString("If the web search tool is available and the knowledge base does not cover the question, use that tool; " +
		"otherwise answer from the retrieved passages and say plainly what they do not cover. ")
	b.WriteString(codexRetrievalToolsLine())
	b.WriteString(codexShellPolicy())
	b.WriteString(codexProvenanceContract(quote))
	return b.String()
}

// codexSubagentContract is the developer fragment a delegated sub-agent gets in place of the
// root contract (delivered via features.multi_agent_v2.subagent_developer_instructions).
//
// Codex OWNS a sub-agent's identity/role prompt (its <multi_agent_role> / <multi_agent_mode>
// fragments, chosen from the fork mode, depth and role) AND owns where a developer fragment
// comes from: by default it copies the parent thread's developer instructions into every
// child, which would hand a worker a prompt written for the end-user-facing root. So mode 8
// supplies what Codex cannot know — which tools may retrieve, the shell/network policy, and the
// citation tuple format. It stays free of the root's end-user framing (the fallback-and-answer
// line): that is Codex's role prompt's business, and a worker reports findings rather than
// answering the user.
//
// The shell policy IS included, deliberately reversing an earlier "root-only" choice: it was
// dropped on the theory that the sandbox enforces the boundary by itself, and a q11 root run
// then showed the boundary is askable-around (`sandbox_permissions: require_escalated`). A child
// has the same shell, the same sandbox and the same ask, so it gets the same sentence.
func codexSubagentContract(quote bool) string {
	return codexRetrievalToolsLine() + codexShellPolicy() + codexProvenanceContract(quote)
}

// codexRetrievalToolsLine is the part both the root agent and a sub-agent get: which tools
// may retrieve, and that the tool list (not the prompt) is authoritative — so neither
// invents a capability it does not have.
func codexRetrievalToolsLine() string {
	return "Your RETRIEVAL tools are the MCP tools provided for this conversation: the knowledge-base retrieval tools, " +
		"and — when this conversation enables web search — a web search tool. The tool list you are given is " +
		"authoritative: use exactly the retrieval tools it contains, and never assume a retrieval capability it does " +
		"not list. "
}

// codexShellPolicy scopes the shell for BOTH the root agent and a sub-agent.
//
// It matters because mode 8 runs with a read-only sandbox whose network_access is false, so a
// short script (e.g. `python3 -c ...`) runs for arithmetic but cannot reach the network —
// enabling exact computation without giving the model a way around the conversation's
// web-search setting. Left unsaid, the model either refuses to compute, or burns the turn on
// `curl`.
//
// It must name BOTH halves of the rule, because the sandbox does not enforce the boundary on
// its own. A delegated child has the same shell and the same sandbox, so it needs the same
// sentence — dropping it (on the theory that the sandbox suffices) is what let a q11 run try
// `curl` twice. Observed: the model read "no network access" as a condition to be LIFTED, and
// passed `sandbox_permissions: require_escalated` with a justification ("the exec_command
// requires user approval for network access"), which the harness's own tool schema invites.
// So the policy has to say "no network by any means" AND "do not ask to be let out".
func codexShellPolicy() string {
	return "You also have a shell for LOCAL computation only: when a task needs arithmetic, counting or a short " +
		"script, run it (e.g. python3 or node) instead of guessing — but only to compute on what you already have. " +
		"The shell has NO network access: never fetch, download or search the web from it, by any means — no " +
		"curl/wget, and no HTTP call from a script either (python `urllib`/`requests`/`http.client`/`socket`, node " +
		"`fetch`/`http`, or any equivalent). Never ask to be let out to the network either: do not set " +
		"`sandbox_permissions`/`require_escalated` and do not request approval to reach the network — such requests " +
		"are declined. All retrieval therefore comes from the tools above. "
}

// codexProvenanceContract is the citation tuple format, present only when the conversation
// asks for citations (quote). It is role-neutral on purpose: the root agent and a sub-agent
// emit the same tuples, so a sub-agent's findings carry provenance the root can reuse
// verbatim instead of re-deriving ids.
func codexProvenanceContract(quote bool) string {
	if !quote {
		return ""
	}
	return "Every line that states a fact backed by a retrieved passage must END with that passage's provenance, in exactly this form:\n" +
		"(doc: <doc_name>, doc_id: <doc_id>, chunk_id: <chunk_id>, snippet: \"<short verbatim excerpt>\")\n" +
		"A passage returned by the web search tool uses this form instead:\n" +
		"(source: web, url: <url>, title: <title>, snippet: \"<short verbatim excerpt>\")\n" +
		"Copy every value verbatim from that passage's tool output; never invent ids, urls or titles. " +
		"Do not write [ID:N] markers - the system assigns them."
}

// chatKBIDs extracts the dialog's knowledge-base ids as strings.
// narrationChunk frames one narrated tool line for the thinking stream: exactly one
// newline before it (only when the stream is mid-line — a fresh block already starts on a
// line boundary) and exactly one after, so the line never runs into the reasoning text
// around it.
func narrationChunk(tail byte, line string) string {
	if tail != 0 && tail != '\n' {
		return "\n" + line + "\n"
	}
	return line + "\n"
}

// toolNarration is the thinking-stream line for one MCP tool step: the call with its
// arguments, then the outcome. emitTool frames it with exactly one newline on each side.
func toolNarration(rt codexagent.RoundTrip) string {
	if rt.Tool == "" {
		return ""
	}
	switch rt.Status {
	case codexagent.ToolStatusCompleted:
		return fmt.Sprintf("%s returned in %dms", rt.Tool, rt.DurationMS)
	case codexagent.ToolStatusFailed:
		return fmt.Sprintf("%s failed in %dms", rt.Tool, rt.DurationMS)
	default:
		if rt.Args == "" {
			return rt.Tool
		}
		return rt.Tool + " " + rt.Args
	}
}

// commandNarration renders one local shell execution for the thinking stream: the command
// line, then its exit status and duration. It mirrors toolNarration, whose "returned in"
// wording is about a tool result rather than a process exit.
func commandNarration(rt codexagent.RoundTrip) string {
	if rt.Args == "" {
		return rt.Tool
	}
	switch rt.Status {
	case codexagent.ToolStatusCompleted:
		return fmt.Sprintf("%s: %s (exit 0, %dms)", rt.Tool, rt.Args, rt.DurationMS)
	case codexagent.ToolStatusFailed:
		if rt.ExitCode != nil {
			return fmt.Sprintf("%s: %s (exit %d, %dms)", rt.Tool, rt.Args, *rt.ExitCode, rt.DurationMS)
		}
		return fmt.Sprintf("%s: %s (failed, %dms)", rt.Tool, rt.Args, rt.DurationMS)
	default:
		return fmt.Sprintf("%s: %s", rt.Tool, rt.Args)
	}
}

// agentNarration renders one sub-agent collaboration step (features.multi_agent_v2) for the
// thinking stream: the collaboration tool and the delegated task (or the targeted agents).
// The tool arrives namespaced ("agent:spawnAgent"); the line reads better un-namespaced.
func agentNarration(rt codexagent.RoundTrip) string {
	name := strings.TrimPrefix(rt.Tool, "agent:")
	switch {
	case rt.Args == "":
		return "agent: " + name
	case rt.Status == codexagent.ToolStatusRunning:
		return fmt.Sprintf("agent: %s %s", name, rt.Args)
	default:
		return fmt.Sprintf("agent: %s %s (%s)", name, rt.Args, rt.Status)
	}
}

// builtinNarration renders any other Codex built-in action (plan, fileChange,
// dynamicToolCall, webSearch, imageView, sleep, review mode, image generation) as one brief
// line, so the thinking stream shows every tool the model used, not only MCP ones.
func builtinNarration(rt codexagent.RoundTrip) string {
	name := strings.TrimPrefix(rt.Tool, "builtin:")
	if rt.Args == "" {
		return "builtin: " + name
	}
	return fmt.Sprintf("builtin: %s %s", name, rt.Args)
}

// toolThinkKind maps a round trip's status onto the shared ThinkEvent kinds
// (runtime.ThinkKindToolCall / ThinkKindToolResult).
func toolThinkKind(status string) string {
	if status == codexagent.ToolStatusRunning {
		return "tool_call"
	}
	return "tool_result"
}

func chatKBIDs(chat *entity.Chat) []string {
	ids := make([]string, 0, len(chat.KBIDs))
	for _, raw := range chat.KBIDs {
		if id, ok := raw.(string); ok && id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// cleanupCodexThread drops a deleted session's mode 8 state: the persisted thread mapping
// and the session's MCP handle (so the thread can no longer reach this process — see the
// plan, §5.2). The remote Codex thread is left to the server's own lifecycle. Per-session
// mutual exclusion is the KV binding, not a lock held here, so a delete can run alongside
// an in-flight turn — that turn's mapping write is guarded on the handle still being alive.
func cleanupCodexThread(ctx context.Context, sessionID string) {
	if sessionID == "" {
		return
	}
	if dao.DB == nil {
		return
	}
	handle, err := dao.NewCodexThreadStore(dao.DB).DeleteBySessionID(ctx, sessionID)
	if err != nil {
		common.WarnCtx(ctx, "codex: failed to clear thread mapping",
			zap.String("session_id", sessionID), zap.Error(err))
		return
	}
	if handle != "" {
		mcp.DefaultCodexTickets().RevokeHandle(ctx, handle)
	}
}
