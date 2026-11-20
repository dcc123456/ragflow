//
//  Copyright (c) 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//
//  This file is part of the RAGFlow Enterprise Edition and is licensed,
//  not sold, under the RAGFlow Enterprise Commercial License Agreement.
//  Unauthorized copying, modification, distribution, or disclosure of this
//  file, via any medium, is strictly prohibited.
//
//  Portions of this file may incorporate code from RAGFlow Community Edition,
//  which is licensed under the Apache License, Version 2.0.
//
//  For commercial licensing inquiries: sales@infiniflow.ai
//

//go:build ee

package connector

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	feishuOpenBaseURL        = "https://open.feishu.cn"
	larkOpenBaseURL          = "https://open.larksuite.com"
	feishuRequestTimeout     = 60 * time.Second
	defaultFeishuBatchSize   = 2
	maxFeishuBatchSize       = 10
	feishuNodePageSize       = 50
	defaultFeishuMaxFileSize = 50 * 1024 * 1024
	feishuMaxResponseBody    = 4 * 1024 * 1024
)

// Platforms the connector can talk to: Feishu (China) and Lark (international)
// are separate open platforms with their own hosts and app credentials.
const (
	feishuDomainFeishu = "feishu"
	feishuDomainLark   = "lark"
)

// Wiki node content types the connector syncs, and the extension every rendered
// node type is stored with.
const (
	feishuWikiFileObjectType    = "file"
	feishuWikiDocxObjectType    = "docx"
	feishuWikiSheetObjectType   = "sheet"
	feishuWikiBitableObjectType = "bitable"
	feishuWikiMarkdownExtension = ".md"
)

// Retry/backoff knobs for Feishu API calls. They are package variables so tests
// can shrink the delays (mirrors rest_api.go).
var (
	feishuRetryTries     = 5
	feishuRetryBaseDelay = time.Second
	feishuRetryMaxDelay  = 30 * time.Second
	feishuRetryBackoff   = 2.0
	feishuRetryJitter    = time.Second
	feishu429MaxWaits    = 5
	feishu429DefaultWait = 5 * time.Second
)

// FeishuWikiConnector reads one Feishu Wiki subtree using an application's
// tenant credentials: attachment files as-is, native documents as Markdown.
type FeishuWikiConnector struct {
	domain            string
	appID             string
	appSecret         string
	spaceID           string
	rootNodeToken     string
	includeExtensions map[string]struct{}
	includeKeywords   map[string]struct{}
	excludeKeywords   map[string]struct{}
	batchSize         int
	maxFileSizeBytes  int64
	baseURL           string
	httpClient        *http.Client

	mu                   sync.Mutex
	accessToken          string
	accessTokenExpiresAt time.Time
}

// feishuHostBaseURL maps the configured platform to its open-platform host.
// Only these two hosts are reachable, so the platform setting never becomes a
// free-form outbound URL.
func feishuHostBaseURL(domain string) string {
	if strings.EqualFold(strings.TrimSpace(domain), feishuDomainLark) {
		return larkOpenBaseURL
	}
	return feishuOpenBaseURL
}

// NewFeishuWikiConnector creates a Feishu Wiki connector from Python-compatible
// config. It performs synchronous config validation but no network I/O.
func NewFeishuWikiConnector(config map[string]any) (*FeishuWikiConnector, error) {
	credentials, _ := config["credentials"].(map[string]any)
	batchSize, err := parseConfigInt(config["batch_size"], defaultFeishuBatchSize)
	if err != nil {
		return nil, &ConnectorValidationError{Message: "Feishu Wiki batch_size must be an integer"}
	}
	maxFileSizeBytes, err := parseConfigInt64(config["max_file_size_bytes"], defaultFeishuMaxFileSize)
	if err != nil {
		return nil, &ConnectorValidationError{Message: "Feishu Wiki max_file_size_bytes must be an integer"}
	}
	connector := &FeishuWikiConnector{
		appID:             strings.TrimSpace(stringConfig(credentials["app_id"])),
		appSecret:         strings.TrimSpace(stringConfig(credentials["app_secret"])),
		spaceID:           strings.TrimSpace(stringConfig(config["space_id"])),
		rootNodeToken:     strings.TrimSpace(stringConfig(config["root_node_token"])),
		includeExtensions: extensionSet(config["include_extensions"]),
		includeKeywords:   keywordSet(config["include_keywords"]),
		excludeKeywords:   keywordSet(config["exclude_keywords"]),
		batchSize:         batchSize,
		domain:            strings.ToLower(strings.TrimSpace(stringConfig(config["domain"]))),
		maxFileSizeBytes:  maxFileSizeBytes,
		baseURL:           feishuHostBaseURL(stringConfig(config["domain"])),
		httpClient:        &http.Client{Timeout: feishuRequestTimeout},
	}
	if err := connector.validateConfig(); err != nil {
		return nil, err
	}
	return connector, nil
}

// validateConfig validates configuration values without network I/O.
func (c *FeishuWikiConnector) validateConfig() error {
	if c.appID == "" || c.appSecret == "" {
		return &ConnectorMissingCredentialError{Message: "Feishu Wiki"}
	}
	if c.spaceID == "" {
		return &ConnectorValidationError{Message: "Feishu Wiki space ID is required"}
	}
	if c.rootNodeToken == "" {
		return &ConnectorValidationError{Message: "Feishu Wiki root node token is required"}
	}
	if c.batchSize < 1 || c.batchSize > maxFeishuBatchSize {
		return &ConnectorValidationError{Message: fmt.Sprintf("Feishu Wiki batch_size must be between 1 and %d", maxFeishuBatchSize)}
	}
	if c.maxFileSizeBytes <= 0 {
		return &ConnectorValidationError{Message: "Feishu Wiki max_file_size_bytes must be a positive integer"}
	}
	if c.domain != "" && c.domain != feishuDomainFeishu && c.domain != feishuDomainLark {
		return &ConnectorValidationError{Message: "Feishu Wiki platform must be feishu or lark"}
	}
	for ext := range c.includeExtensions {
		if _, ok := feishuSupportedExtensions[ext]; !ok {
			return &ConnectorValidationError{Message: fmt.Sprintf("Unsupported Feishu Wiki file extension: %s", ext)}
		}
	}
	return nil
}

// Validate validates configuration and credentials by authenticating and
// listing the first page of the configured root node.
func (c *FeishuWikiConnector) Validate(ctx context.Context) error {
	if err := c.validateConfig(); err != nil {
		return err
	}
	if _, err := c.getAccessToken(ctx); err != nil {
		return err
	}
	if _, _, _, err := c.listChildPage(ctx, c.rootNodeToken, ""); err != nil {
		return fmt.Errorf("Feishu Wiki validation failed: %w", err)
	}
	return nil
}

// ValidateConnectorSetting validates Feishu Wiki settings from an unsaved config.
func (c *FeishuWikiConnector) ValidateConnectorSetting(ctx context.Context, request map[string]any) error {
	ctx, cancel := context.WithTimeout(ctx, connectorSettingValidationTimeout)
	defer cancel()
	candidate, err := NewFeishuWikiConnector(request)
	if err != nil {
		return err
	}
	if c != nil {
		if c.httpClient != nil {
			candidate.httpClient = c.httpClient
		}
		if c.baseURL != "" {
			candidate.baseURL = c.baseURL
		}
	}
	return candidate.Validate(ctx)
}

// OpenSync opens one Feishu Wiki synchronization session.
func (c *FeishuWikiConnector) OpenSync(ctx context.Context, request SyncRequest) (SyncSession, error) {
	nodes, err := c.collectNodes(ctx)
	if err != nil {
		return nil, err
	}
	end := request.WindowEnd
	if end.IsZero() {
		end = time.Now().UTC()
	}
	accepted := make([]feishuWikiNode, 0, len(nodes))
	for _, node := range nodes {
		if request.WindowStart != nil || !request.WindowEnd.IsZero() {
			var updatedAt *time.Time
			if node.hasUpdatedAt {
				updatedAt = &node.updatedAt
			}
			if !connectorWithinWindow(updatedAt, request.WindowStart, &end) {
				continue
			}
		}
		if !c.matchesFilters(node) {
			continue
		}
		accepted = append(accepted, node)
	}
	sort.Slice(accepted, func(i, j int) bool {
		return accepted[i].sourceID(c.spaceID) < accepted[j].sourceID(c.spaceID)
	})
	session := &feishuWikiSyncSession{
		connector: c,
		nodes:     accepted,
		batchSize: c.batchSize,
		windowEnd: end,
	}
	if err := session.applyResume(request.Resume); err != nil {
		return nil, err
	}
	return session, nil
}

// OpenPrune opens one Feishu Wiki prune snapshot session. The snapshot is the
// whole subtree reduced to source ids under the same filters OpenSync applies,
// so a node excluded from syncing is never retained.
func (c *FeishuWikiConnector) OpenPrune(ctx context.Context, request PruneRequest) (PruneSession, error) {
	nodes, err := c.collectNodes(ctx)
	if err != nil {
		return nil, err
	}
	documents := make([]SlimDocument, 0, len(nodes))
	for _, node := range nodes {
		if !c.matchesFilters(node) {
			continue
		}
		documents = append(documents, SlimDocument{SourceID: node.sourceID(c.spaceID)})
	}
	sort.Slice(documents, func(i, j int) bool { return documents[i].SourceID < documents[j].SourceID })
	return &feishuWikiPruneSession{documents: documents, batchSize: c.batchSize}, nil
}

// feishuWikiNode is the metadata for one synced Wiki node: an attachment that is
// downloaded as it is, or a native resource whose body is rendered to Markdown.
type feishuWikiNode struct {
	nodeToken       string
	objToken        string
	title           string
	path            string
	objType         string
	parentNodeToken string
	updatedAt       time.Time
	hasUpdatedAt    bool
}

func (n feishuWikiNode) sourceID(spaceID string) string {
	return "feishu_wiki:" + spaceID + ":" + n.nodeToken
}

// feishuWikiSyncSession streams accepted Wiki nodes in fixed-size batches,
// resuming from a committed SourceID anchor.
type feishuWikiSyncSession struct {
	connector *FeishuWikiConnector
	nodes     []feishuWikiNode
	batchSize int
	windowEnd time.Time
	index     int
}

// NextBatch returns the next source document batch.
func (s *feishuWikiSyncSession) NextBatch(ctx context.Context) (SyncBatch, error) {
	if s.index >= len(s.nodes) {
		return SyncBatch{}, io.EOF
	}
	documents := make([]SourceDocument, 0, s.batchSize)
	var last feishuWikiNode
	for attempts := 0; attempts < s.batchSize && s.index < len(s.nodes); attempts++ {
		node := s.nodes[s.index]
		s.index++
		last = node
		document, err := s.connector.buildDocument(ctx, node, s.windowEnd)
		if err != nil {
			return SyncBatch{}, err
		}
		documents = append(documents, document)
	}
	return SyncBatch{Documents: documents, Checkpoint: feishuWikiCheckpoint(s.connector.spaceID, last)}, nil
}

// Close releases connector resources.
func (s *feishuWikiSyncSession) Close() error {
	return nil
}

// applyResume advances the session past the last committed SourceID anchor.
func (s *feishuWikiSyncSession) applyResume(checkpoint *SyncCheckpoint) error {
	if checkpoint == nil {
		return nil
	}
	sourceID := firstNonEmpty(checkpoint.SourceID, checkpoint.Cursor)
	if sourceID == "" {
		return fmt.Errorf("feishu wiki sync checkpoint has no source anchor: %w", ErrSyncResumeInvalid)
	}
	for index, node := range s.nodes {
		if node.sourceID(s.connector.spaceID) == sourceID {
			s.index = index + 1
			return nil
		}
	}
	return fmt.Errorf("feishu wiki resume anchor %q was not found in the current listing: %w", sourceID, ErrSyncResumeInvalid)
}

// feishuWikiPruneSession streams the prune snapshot in fixed-size batches.
type feishuWikiPruneSession struct {
	documents []SlimDocument
	batchSize int
	index     int
}

// NextBatch returns the next prune snapshot batch.
func (s *feishuWikiPruneSession) NextBatch(ctx context.Context) (PruneBatch, error) {
	if s.index >= len(s.documents) {
		return PruneBatch{}, io.EOF
	}
	batchSize := s.batchSize
	if batchSize < 1 {
		batchSize = defaultFeishuBatchSize
	}
	end := s.index + batchSize
	if end > len(s.documents) {
		end = len(s.documents)
	}
	documents := s.documents[s.index:end]
	s.index = end
	return PruneBatch{Documents: documents}, nil
}

// Close releases connector resources.
func (s *feishuWikiPruneSession) Close() error {
	return nil
}

func feishuWikiCheckpoint(spaceID string, node feishuWikiNode) *SyncCheckpoint {
	checkpoint := &SyncCheckpoint{
		Cursor:   node.sourceID(spaceID),
		SourceID: node.sourceID(spaceID),
	}
	if node.hasUpdatedAt {
		updatedAt := node.updatedAt
		checkpoint.UpdatedAt = &updatedAt
	}
	return checkpoint
}

// buildDocument maps one Wiki node to a SourceDocument: the downloaded bytes for
// an attachment node, the Markdown rendering of the body for a native resource.
func (c *FeishuWikiConnector) buildDocument(ctx context.Context, node feishuWikiNode, fallbackUpdatedAt time.Time) (SourceDocument, error) {
	blob, extension, err := c.nodeContent(ctx, node)
	if err != nil {
		return SourceDocument{}, err
	}
	updatedAt := fallbackUpdatedAt
	if node.hasUpdatedAt {
		updatedAt = node.updatedAt
	}
	parentNodeToken := node.parentNodeToken
	if parentNodeToken == "" {
		parentNodeToken = c.rootNodeToken
	}
	sha := sha256.Sum256(blob)
	shaHex := hex.EncodeToString(sha[:])
	return SourceDocument{
		SourceID:           node.sourceID(c.spaceID),
		SemanticIdentifier: syncNodePath(node.path, firstNonEmpty(node.title, node.nodeToken)),
		Extension:          extension,
		Blob:               blob,
		UpdatedAt:          updatedAt,
		SizeBytes:          int64(len(blob)),
		Fingerprint:        shaHex[:32],
		Metadata: map[string]any{
			"source":                 "feishu_wiki",
			"wiki_space_id":          c.spaceID,
			"wiki_node_token":        node.nodeToken,
			"wiki_object_token":      node.objToken,
			"wiki_object_type":       node.objType,
			"wiki_parent_node_token": parentNodeToken,
			"wiki_url":               fmt.Sprintf("https://feishu.cn/wiki/%s", node.nodeToken),
			"source_updated_at":      updatedAt.UTC().Format(time.RFC3339),
			"content_sha256":         shaHex,
		},
	}, nil
}

// nodeContent returns one node's bytes and the extension its document row gets.
func (c *FeishuWikiConnector) nodeContent(ctx context.Context, node feishuWikiNode) ([]byte, string, error) {
	render := func(blob []byte, err error) ([]byte, string, error) {
		if err != nil {
			return nil, "", err
		}
		if strings.TrimSpace(string(blob)) == "" {
			blob = []byte(strings.TrimSpace(node.title))
		}
		return blob, feishuWikiMarkdownExtension, nil
	}
	switch node.objType {
	case feishuWikiDocxObjectType:
		return render(c.docxMarkdown(ctx, node.objToken))
	case feishuWikiSheetObjectType:
		return render(c.sheetMarkdown(ctx, node.objToken))
	case feishuWikiBitableObjectType:
		return render(c.bitableMarkdown(ctx, node.objToken))
	}
	return c.feishuAttachmentContent(ctx, node)
}

// matchesFilters applies the keyword filters to every node and the extension
// allowlist to attachment nodes, since a native document has no file extension.
func (c *FeishuWikiConnector) matchesFilters(node feishuWikiNode) bool {
	if node.objType == feishuWikiFileObjectType {
		ext := strings.ToLower(path.Ext(node.title))
		if _, ok := feishuSupportedExtensions[ext]; !ok {
			return false
		}
		if len(c.includeExtensions) > 0 {
			if _, ok := c.includeExtensions[ext]; !ok {
				return false
			}
		}
	}
	normalizedTitle := strings.ToLower(node.title)
	if len(c.includeKeywords) > 0 && !containsAny(normalizedTitle, c.includeKeywords) {
		return false
	}
	return !containsAny(normalizedTitle, c.excludeKeywords)
}

// collectNodes walks the configured Wiki subtree and returns the nodes it can
// sync (filters are applied later so the window can be applied at open).
func (c *FeishuWikiConnector) collectNodes(ctx context.Context) ([]feishuWikiNode, error) {
	var nodes []feishuWikiNode
	// paths maps every listed node, folders included, to its folder chain so a
	// synced name carries the directories it lives in.
	paths := make(map[string]string)
	if rootTitle := c.rootNodeTitle(ctx); rootTitle != "" {
		paths[c.rootNodeToken] = rootTitle
	}
	stack := []string{c.rootNodeToken}
	for len(stack) > 0 {
		parent := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		pageToken := ""
		for {
			items, hasMore, next, err := c.listChildPage(ctx, parent, pageToken)
			if err != nil {
				return nil, err
			}
			for _, node := range items {
				nodeToken := stringConfig(node["node_token"])
				objType := stringConfig(node["obj_type"])
				objToken := stringConfig(node["obj_token"])
				title := stringConfig(node["title"])
				parentPath := paths[parent]
				if nodeToken != "" {
					paths[nodeToken] = syncNodePath(parentPath, title)
				}
				hasChild, _ := node["has_child"].(bool)
				if feishuWikiSyncableObjectType(objType) {
					updatedAt, hasUpdatedAt := feishuParseTimestamp(node["obj_edit_time"])
					nodes = append(nodes, feishuWikiNode{
						nodeToken:       nodeToken,
						objToken:        objToken,
						title:           title,
						path:            parentPath,
						objType:         objType,
						parentNodeToken: stringConfig(node["parent_node_token"]),
						updatedAt:       updatedAt,
						hasUpdatedAt:    hasUpdatedAt,
					})
				}
				if hasChild && nodeToken != "" {
					stack = append(stack, nodeToken)
				}
			}
			if !hasMore {
				break
			}
			if next == "" {
				return nil, &ConnectorValidationError{Message: "Feishu Wiki pagination indicated more data without a page token"}
			}
			pageToken = next
		}
	}
	return nodes, nil
}

// feishuGet performs one authenticated GET and decodes the JSON envelope.
func (c *FeishuWikiConnector) feishuGet(ctx context.Context, endpoint, operation string) (map[string]any, error) {
	token, err := c.getAccessToken(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := c.feishuDoRequest(ctx, http.MethodGet, endpoint, nil, map[string]string{"Authorization": "Bearer " + token})
	if err != nil {
		return nil, err
	}
	return feishuDecodeResponse(resp, operation)
}

// feishuWikiSyncableObjectType reports whether a Wiki node's content type is
// synced: attachments, native documents, spreadsheets and bitables.
func feishuWikiSyncableObjectType(objType string) bool {
	switch objType {
	case feishuWikiFileObjectType, feishuWikiDocxObjectType, feishuWikiSheetObjectType, feishuWikiBitableObjectType:
		return true
	}
	return false
}

// rootNodeTitle reads the configured root node's own title so it becomes the
// first path segment. A failed lookup only drops the prefix.
func (c *FeishuWikiConnector) rootNodeTitle(ctx context.Context) string {
	if c.rootNodeToken == "" {
		return ""
	}
	endpoint := fmt.Sprintf("%s/open-apis/wiki/v2/spaces/get_node?token=%s&obj_type=wiki",
		c.baseURL, url.QueryEscape(c.rootNodeToken))
	payload, err := c.feishuGet(ctx, endpoint, "read the root wiki node")
	if err != nil {
		return ""
	}
	data, _ := payload["data"].(map[string]any)
	node, _ := data["node"].(map[string]any)
	if node == nil {
		node = data
	}
	return firstNonEmpty(stringConfig(node["title"]))
}

// listChildPage lists one page of child nodes for a parent node.
func (c *FeishuWikiConnector) listChildPage(ctx context.Context, parentNodeToken, pageToken string) ([]map[string]any, bool, string, error) {
	endpoint := fmt.Sprintf("%s/open-apis/wiki/v2/spaces/%s/nodes?page_size=%d", c.baseURL, url.PathEscape(c.spaceID), feishuNodePageSize)
	if parentNodeToken != "" {
		endpoint += "&parent_node_token=" + url.QueryEscape(parentNodeToken)
	}
	if pageToken != "" {
		endpoint += "&page_token=" + url.QueryEscape(pageToken)
	}
	payload, err := c.feishuGet(ctx, endpoint, "list Wiki nodes")
	if err != nil {
		return nil, false, "", err
	}
	data, _ := payload["data"].(map[string]any)
	if data == nil {
		return nil, false, "", &ConnectorValidationError{Message: "Feishu Wiki node response contained invalid data"}
	}
	items := mapItems(data["items"])
	hasMore, _ := data["has_more"].(bool)
	return items, hasMore, stringConfig(data["page_token"]), nil
}

// getAccessToken returns a cached tenant access token, refreshing it when
// expired or absent.
func (c *FeishuWikiConnector) getAccessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accessToken != "" && time.Now().Before(c.accessTokenExpiresAt) {
		return c.accessToken, nil
	}
	body, err := json.Marshal(map[string]string{"app_id": c.appID, "app_secret": c.appSecret})
	if err != nil {
		return "", err
	}
	resp, err := c.feishuDoRequest(ctx, http.MethodPost, c.baseURL+"/open-apis/auth/v3/tenant_access_token/internal", body, nil)
	if err != nil {
		return "", err
	}
	payload, err := feishuDecodeResponse(resp, "authenticate")
	if err != nil {
		return "", err
	}
	accessToken := stringConfig(payload["tenant_access_token"])
	if accessToken == "" {
		return "", &ConnectorValidationError{Message: "Feishu authentication response did not contain a tenant access token"}
	}
	expiresIn := feishuIntDefault(payload["expire"], 7200)
	if expiresIn < 60 {
		expiresIn = 7200
	}
	c.accessToken = accessToken
	c.accessTokenExpiresAt = time.Now().Add(time.Duration(expiresIn-60) * time.Second)
	return accessToken, nil
}

// feishuDoRequest performs one request with bounded 429/5xx retry, honoring
// Feishu's x-ogw-ratelimit-reset (and Retry-After) headers.
func (c *FeishuWikiConnector) feishuDoRequest(ctx context.Context, method, endpoint string, body []byte, headers map[string]string) (*http.Response, error) {
	delay := feishuRetryBaseDelay
	var lastErr error
	rateWaits := 0
	for attempt := 0; attempt < feishuRetryTries; {
		resp, err := c.feishuSend(ctx, method, endpoint, body, headers)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt == feishuRetryTries-1 {
				break
			}
			if !feishuSleep(ctx, delay+time.Duration(rand.Float64()*float64(feishuRetryJitter))) {
				return nil, ctx.Err()
			}
			delay = time.Duration(float64(delay) * feishuRetryBackoff)
			if delay > feishuRetryMaxDelay {
				delay = feishuRetryMaxDelay
			}
			attempt++
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			if rateWaits >= feishu429MaxWaits {
				lastErr = &RateLimitTriedTooManyTimesError{Message: "Feishu rate limit exceeded while requesting " + endpoint}
				break
			}
			rateWaits++
			retryAfter := feishu429DefaultWait
			if raw := resp.Header.Get("x-ogw-ratelimit-reset"); raw != "" {
				if seconds, parseErr := strconv.Atoi(strings.TrimSpace(raw)); parseErr == nil && seconds >= 0 {
					retryAfter = time.Duration(seconds) * time.Second
				}
			} else if raw := resp.Header.Get("Retry-After"); raw != "" {
				if seconds, parseErr := strconv.Atoi(strings.TrimSpace(raw)); parseErr == nil && seconds >= 0 {
					retryAfter = time.Duration(seconds) * time.Second
				}
			}
			if !feishuSleep(ctx, retryAfter) {
				return nil, ctx.Err()
			}
			continue
		}
		if resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("Feishu HTTP %d while requesting %s", resp.StatusCode, endpoint)
			if attempt == feishuRetryTries-1 {
				break
			}
			if !feishuSleep(ctx, delay+time.Duration(rand.Float64()*float64(feishuRetryJitter))) {
				return nil, ctx.Err()
			}
			delay = time.Duration(float64(delay) * feishuRetryBackoff)
			if delay > feishuRetryMaxDelay {
				delay = feishuRetryMaxDelay
			}
			attempt++
			continue
		}
		return resp, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("Feishu request failed: %s", endpoint)
	}
	return nil, lastErr
}

// feishuSend performs one HTTP request without retry.
func (c *FeishuWikiConnector) feishuSend(ctx context.Context, method, endpoint string, body []byte, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpClient.Do(req)
}

func feishuSleep(ctx context.Context, duration time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(duration):
		return true
	}
}

// feishuDecodeResponse decodes a JSON API response and enforces Feishu error
// semantics (permission/HTTP/business codes surfaced with code and msg).
func feishuDecodeResponse(resp *http.Response, operation string) (map[string]any, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, feishuMaxResponseBody+1))
	if err != nil {
		return nil, &ConnectorValidationError{Message: fmt.Sprintf("Failed to read Feishu response while attempting to %s", operation)}
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil || payload == nil {
		return nil, &ConnectorValidationError{Message: fmt.Sprintf("Feishu returned an invalid response while attempting to %s", operation)}
	}
	code := feishuCode(payload["code"])
	message := stringConfig(payload["msg"])
	if message == "" {
		message = "unknown Feishu API error"
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		detail := ""
		if message != "unknown Feishu API error" {
			detail = ": " + message
		}
		return nil, &ConnectorValidationError{Message: fmt.Sprintf("Feishu permission denied (HTTP %d) while attempting to %s%s", resp.StatusCode, operation, detail)}
	}
	if resp.StatusCode >= 400 {
		if code != 0 {
			return nil, &ConnectorValidationError{Message: fmt.Sprintf("Feishu API error %d (HTTP %d) while attempting to %s: %s", code, resp.StatusCode, operation, message)}
		}
		return nil, &ConnectorValidationError{Message: fmt.Sprintf("Feishu HTTP %d while attempting to %s", resp.StatusCode, operation)}
	}
	if code != 0 {
		return nil, &ConnectorValidationError{Message: fmt.Sprintf("Feishu API error %d while attempting to %s: %s", code, operation, message)}
	}
	return payload, nil
}

// feishuErrorEnvelope reads a response body (bounded) and returns the Feishu
// business code and message for diagnostics.
func feishuErrorEnvelope(resp *http.Response) (int, string) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, feishuMaxResponseBody+1))
	if err != nil {
		return 0, ""
	}
	var envelope struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return 0, ""
	}
	return envelope.Code, envelope.Msg
}

func feishuCode(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	case string:
		typed = strings.TrimSpace(typed)
		if typed == "" {
			return 0
		}
		if parsed, err := strconv.Atoi(typed); err == nil {
			return parsed
		}
	}
	return 0
}

func feishuIntDefault(value any, fallback int) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
			return parsed
		}
	}
	return fallback
}

func feishuParseTimestamp(value any) (time.Time, bool) {
	if value == nil {
		return time.Time{}, false
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" {
		return time.Time{}, false
	}
	if seconds, err := strconv.ParseFloat(text, 64); err == nil {
		return time.Unix(int64(seconds), 0).UTC(), true
	}
	if parsed, err := time.Parse(time.RFC3339, text); err == nil {
		return parsed.UTC(), true
	}
	return time.Time{}, false
}
