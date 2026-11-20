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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"ragflow/internal/common"
)

const (
	dingTalkWikiSource         = "dingtalk_wiki"
	dingTalkDriveSource        = "dingtalk_drive"
	dingTalkNodePageSize       = 50
	defaultDingTalkBatchSize   = 2
	maxDingTalkBatchSize       = 10
	defaultDingTalkMaxFileSize = 50 * 1024 * 1024
	dingTalkWikiFileObjectType = "file"
	dingTalkDocExtension       = ".adoc"
	dingTalkSheetExtension     = ".axls"
	dingTalkMarkdownExtension  = ".md"
)

// DingTalkWikiConnector syncs one DingTalk space: a knowledge base space or a
// DingTalk drive space. Both are walked through the same document API; only a
// drive space skips attachment nodes, whose download API needs a higher
// enterprise authorization level.
type DingTalkWikiConnector struct {
	dingTalkAPIClient
	spaceID           string
	rootNodeID        string
	driveSpace        bool
	includeExtensions map[string]struct{}
	includeKeywords   map[string]struct{}
	excludeKeywords   map[string]struct{}
	batchSize         int
	maxFileSizeBytes  int64
}

// NewDingTalkWikiConnector creates a DingTalk knowledge base connector from
// config. It performs synchronous config validation but no network I/O.
func NewDingTalkWikiConnector(config map[string]any) (*DingTalkWikiConnector, error) {
	return newDingTalkConnector(config, false)
}

// NewDingTalkDriveConnector creates a connector over a DingTalk drive space. It
// shares the knowledge base connector because both spaces expose the same
// document API.
func NewDingTalkDriveConnector(config map[string]any) (*DingTalkWikiConnector, error) {
	return newDingTalkConnector(config, true)
}

func newDingTalkConnector(config map[string]any, driveSpace bool) (*DingTalkWikiConnector, error) {
	credentials, _ := config["credentials"].(map[string]any)
	batchSize, err := parseConfigInt(config["batch_size"], defaultDingTalkBatchSize)
	if err != nil {
		return nil, &ConnectorValidationError{Message: "DingTalk batch_size must be an integer"}
	}
	maxFileSizeBytes, err := parseConfigInt64(config["max_file_size_bytes"], defaultDingTalkMaxFileSize)
	if err != nil {
		return nil, &ConnectorValidationError{Message: "DingTalk max_file_size_bytes must be an integer"}
	}
	connector := &DingTalkWikiConnector{
		dingTalkAPIClient: dingTalkAPIClient{
			appKey:     strings.TrimSpace(stringConfig(credentials["app_key"])),
			appSecret:  strings.TrimSpace(stringConfig(credentials["app_secret"])),
			operatorID: strings.TrimSpace(stringConfig(config["operator_id"])),
			baseURL:    dingTalkAPIBaseURL,
			httpClient: &http.Client{Timeout: dingTalkRequestTimeout},
		},
		spaceID:           strings.TrimSpace(stringConfig(config["space_id"])),
		rootNodeID:        strings.TrimSpace(stringConfig(config["root_node_id"])),
		driveSpace:        driveSpace,
		includeExtensions: extensionSet(config["include_extensions"]),
		includeKeywords:   keywordSet(config["include_keywords"]),
		excludeKeywords:   keywordSet(config["exclude_keywords"]),
		batchSize:         batchSize,
		maxFileSizeBytes:  maxFileSizeBytes,
	}
	if err := connector.validateConfig(); err != nil {
		return nil, err
	}
	return connector, nil
}

func (c *DingTalkWikiConnector) validateConfig() error {
	if c.appKey == "" || c.appSecret == "" {
		return &ConnectorMissingCredentialError{Message: "DingTalk"}
	}
	if c.operatorID == "" {
		return &ConnectorValidationError{Message: "DingTalk operator_id is required"}
	}
	if c.spaceID == "" {
		return &ConnectorValidationError{Message: "DingTalk knowledge base space_id is required"}
	}
	if c.batchSize < 1 || c.batchSize > maxDingTalkBatchSize {
		return &ConnectorValidationError{Message: fmt.Sprintf("DingTalk batch_size must be between 1 and %d", maxDingTalkBatchSize)}
	}
	if c.maxFileSizeBytes <= 0 {
		return &ConnectorValidationError{Message: "DingTalk max_file_size_bytes must be a positive integer"}
	}
	for ext := range c.includeExtensions {
		if _, ok := dingTalkSupportedExtensions[ext]; !ok {
			return &ConnectorValidationError{Message: fmt.Sprintf("Unsupported DingTalk file extension: %s", ext)}
		}
	}
	return nil
}

// Validate authenticates and lists the first page of the configured root node.
func (c *DingTalkWikiConnector) Validate(ctx context.Context) error {
	if _, err := c.getAccessToken(ctx); err != nil {
		return err
	}
	if _, _, _, err := c.listDirectories(ctx, c.rootNodeID, ""); err != nil {
		return fmt.Errorf("DingTalk validation failed: %w", err)
	}
	return nil
}

// ValidateConnectorSetting validates DingTalk settings from an unsaved config.
func (c *DingTalkWikiConnector) ValidateConnectorSetting(ctx context.Context, request map[string]any) error {
	ctx, cancel := context.WithTimeout(ctx, connectorSettingValidationTimeout)
	defer cancel()
	candidate, err := NewDingTalkWikiConnector(request)
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

// OpenSync opens one synchronization session over the configured node subtree.
func (c *DingTalkWikiConnector) OpenSync(ctx context.Context, request SyncRequest) (SyncSession, error) {
	nodes, err := c.collectNodes(ctx)
	if err != nil {
		return nil, err
	}
	end := request.WindowEnd
	if end.IsZero() {
		end = time.Now().UTC()
	}
	accepted := make([]dingTalkWikiNode, 0, len(nodes))
	skippedAttachments := 0
	for _, node := range nodes {
		if !dingTalkSyncableObjectType(node.objType) || !c.matchesFilters(node) {
			continue
		}
		if c.driveSpace && !dingTalkNativeExtension(node.extension) {
			skippedAttachments++
			continue
		}
		if request.WindowStart != nil || !request.WindowEnd.IsZero() {
			var updatedAt *time.Time
			if node.hasUpdatedAt {
				updatedAt = &node.updatedAt
			}
			if !connectorWithinWindow(updatedAt, request.WindowStart, &end) {
				continue
			}
		}
		accepted = append(accepted, node)
	}
	if skippedAttachments > 0 {
		common.Warn("dingtalk drive: attachment skipped, downloading needs a higher enterprise authorization level",
			zap.String("space_id", c.spaceID), zap.Int("skipped", skippedAttachments))
	}
	sort.Slice(accepted, func(i, j int) bool {
		return c.sourceID(accepted[i]) < c.sourceID(accepted[j])
	})
	session := &dingTalkWikiSyncSession{
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

// OpenPrune opens one complete prune snapshot session over the configured space.
// The snapshot is the whole subtree reduced to source ids under the same filters
// OpenSync applies, so a node that never produced a document is not retained.
func (c *DingTalkWikiConnector) OpenPrune(ctx context.Context, request PruneRequest) (PruneSession, error) {
	nodes, err := c.collectNodes(ctx)
	if err != nil {
		return nil, err
	}
	documents := make([]SlimDocument, 0, len(nodes))
	for _, node := range nodes {
		if !dingTalkSyncableObjectType(node.objType) || !c.matchesFilters(node) {
			continue
		}
		if c.driveSpace && !dingTalkNativeExtension(node.extension) {
			continue
		}
		documents = append(documents, SlimDocument{SourceID: c.sourceID(node)})
	}
	sort.Slice(documents, func(i, j int) bool { return documents[i].SourceID < documents[j].SourceID })
	return &dingTalkWikiPruneSession{documents: documents, batchSize: c.batchSize}, nil
}

// dingTalkWikiNode is the metadata of one knowledge base dentry.
type dingTalkWikiNode struct {
	dentryUUID   string
	dentryID     string
	parentID     string
	title        string
	path         string
	objType      string
	extension    string
	docKey       string
	updatedAt    time.Time
	hasUpdatedAt bool
	hasChildren  bool
}

func (c *DingTalkWikiConnector) sourceID(node dingTalkWikiNode) string {
	return c.sourceName() + ":" + c.spaceID + ":" + node.dentryUUID
}

// dingTalkSyncableObjectType reports whether a dentry kind carries content.
// DingTalk returns both attachments and native objects as dentries of type
// "file"; only folders are excluded here.
func dingTalkSyncableObjectType(objType string) bool {
	return objType == dingTalkWikiFileObjectType
}

// dingTalkNativeExtension reports whether the extension names a native DingTalk
// object that is read through its own API instead of being downloaded.
func dingTalkNativeExtension(extension string) bool {
	return extension == dingTalkDocExtension || extension == dingTalkSheetExtension
}

// sourceName is the connector source this space belongs to, which prefixes the
// document ids.
func (c *DingTalkWikiConnector) sourceName() string {
	if c.driveSpace {
		return dingTalkDriveSource
	}
	return dingTalkWikiSource
}

// dingTalkWikiSyncSession streams accepted nodes in fixed-size batches.
type dingTalkWikiSyncSession struct {
	connector *DingTalkWikiConnector
	nodes     []dingTalkWikiNode
	batchSize int
	windowEnd time.Time
	index     int
}

// NextBatch returns the next source document batch.
func (s *dingTalkWikiSyncSession) NextBatch(ctx context.Context) (SyncBatch, error) {
	if s.index >= len(s.nodes) {
		return SyncBatch{}, io.EOF
	}
	documents := make([]SourceDocument, 0, s.batchSize)
	var last dingTalkWikiNode
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
	return SyncBatch{Documents: documents, Checkpoint: dingTalkWikiCheckpoint(s.connector, last)}, nil
}

// Close releases connector resources.
func (s *dingTalkWikiSyncSession) Close() error { return nil }

// applyResume advances the session past the last committed SourceID anchor.
func (s *dingTalkWikiSyncSession) applyResume(checkpoint *SyncCheckpoint) error {
	if checkpoint == nil {
		return nil
	}
	sourceID := firstNonEmpty(checkpoint.SourceID, checkpoint.Cursor)
	if sourceID == "" {
		return fmt.Errorf("dingtalk wiki sync checkpoint has no source anchor: %w", ErrSyncResumeInvalid)
	}
	for index, node := range s.nodes {
		if s.connector.sourceID(node) == sourceID {
			s.index = index + 1
			return nil
		}
	}
	return fmt.Errorf("dingtalk wiki resume anchor %q was not found in the current listing: %w", sourceID, ErrSyncResumeInvalid)
}

// dingTalkWikiPruneSession streams the prune snapshot in fixed-size batches.
type dingTalkWikiPruneSession struct {
	documents []SlimDocument
	batchSize int
	index     int
}

// NextBatch returns the next prune snapshot batch.
func (s *dingTalkWikiPruneSession) NextBatch(ctx context.Context) (PruneBatch, error) {
	if s.index >= len(s.documents) {
		return PruneBatch{}, io.EOF
	}
	end := s.index + s.batchSize
	if end > len(s.documents) {
		end = len(s.documents)
	}
	batch := PruneBatch{Documents: s.documents[s.index:end]}
	s.index = end
	return batch, nil
}

// Close releases connector resources.
func (s *dingTalkWikiPruneSession) Close() error {
	return nil
}

func dingTalkWikiCheckpoint(connector *DingTalkWikiConnector, node dingTalkWikiNode) *SyncCheckpoint {
	sourceID := connector.sourceID(node)
	checkpoint := &SyncCheckpoint{
		Cursor:   sourceID,
		SourceID: sourceID,
	}
	if node.hasUpdatedAt {
		updatedAt := node.updatedAt
		checkpoint.UpdatedAt = &updatedAt
	}
	return checkpoint
}

// buildDocument maps one node to a SourceDocument: the downloaded bytes for an
// attachment, the Markdown rendering of the body for a native document.
func (c *DingTalkWikiConnector) buildDocument(ctx context.Context, node dingTalkWikiNode, fallbackUpdatedAt time.Time) (SourceDocument, error) {
	var blob []byte
	var err error
	extension := node.extension
	switch node.extension {
	case dingTalkDocExtension:
		blob, err = c.docMarkdown(ctx, node)
		extension = dingTalkMarkdownExtension
	case dingTalkSheetExtension:
		blob, err = c.sheetMarkdown(ctx, node)
		extension = dingTalkMarkdownExtension
	default:
		blob, err = c.downloadAttachment(ctx, node)
	}
	if err != nil {
		return SourceDocument{}, err
	}
	updatedAt := fallbackUpdatedAt
	if node.hasUpdatedAt {
		updatedAt = node.updatedAt
	}
	sha := sha256.Sum256(blob)
	shaHex := hex.EncodeToString(sha[:])
	metadata := map[string]any{
		"source":               c.sourceName(),
		"dingtalk_space_id":    c.spaceID,
		"dingtalk_dentry_uuid": node.dentryUUID,
		"dingtalk_dentry_id":   node.dentryID,
		"dingtalk_parent_id":   node.parentID,
		"source_updated_at":    updatedAt.UTC().Format(time.RFC3339),
		"content_sha256":       shaHex,
	}
	return SourceDocument{
		SourceID:           c.sourceID(node),
		SemanticIdentifier: syncNodePath(node.path, firstNonEmpty(node.title, node.dentryUUID)),
		Extension:          extension,
		Blob:               blob,
		UpdatedAt:          updatedAt,
		SizeBytes:          int64(len(blob)),
		Fingerprint:        shaHex[:32],
		Metadata:           metadata,
	}, nil
}

// matchesFilters applies the keyword filters to every node and the extension
// allowlist to attachment nodes; native documents and workbooks bypass it.
func (c *DingTalkWikiConnector) matchesFilters(node dingTalkWikiNode) bool {
	if node.objType == dingTalkWikiFileObjectType && !dingTalkNativeExtension(node.extension) {
		ext := node.extension
		if _, ok := dingTalkSupportedExtensions[ext]; !ok {
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

// collectNodes walks the configured subtree and returns every dentry it can see.
func (c *DingTalkWikiConnector) collectNodes(ctx context.Context) ([]dingTalkWikiNode, error) {
	var nodes []dingTalkWikiNode
	// paths maps every listed dentry, folders included, to its folder chain so a
	// synced name carries the directories it lives in.
	paths := make(map[string]string)
	if rootTitle := c.rootTitle(ctx); rootTitle != "" {
		paths[c.rootNodeID] = rootTitle
	}
	stack := []string{c.rootNodeID}
	for len(stack) > 0 {
		parent := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		nextToken := ""
		for {
			items, hasMore, next, err := c.listDirectories(ctx, parent, nextToken)
			if err != nil {
				return nil, err
			}
			for _, item := range items {
				title := stringConfig(item["name"])
				parentPath := paths[parent]
				node := dingTalkWikiNode{
					dentryUUID: stringConfig(item["dentryUuid"]),
					dentryID:   stringConfig(item["dentryId"]),
					parentID:   parent,
					title:      title,
					path:       parentPath,
					objType:    strings.ToLower(stringConfig(item["dentryType"])),
					extension:  dingTalkNodeExtension(item["extension"]),
					docKey:     stringConfig(item["docKey"]),
				}
				if node.dentryID != "" {
					paths[node.dentryID] = syncNodePath(parentPath, title)
				}
				node.hasChildren, _ = item["hasChildren"].(bool)
				node.updatedAt, node.hasUpdatedAt = dingTalkParseTimestamp(item["updatedTime"])
				if node.dentryUUID != "" {
					nodes = append(nodes, node)
				}
				if node.hasChildren && node.dentryID != "" {
					stack = append(stack, node.dentryID)
				}
			}
			if !hasMore {
				break
			}
			if next == "" {
				return nil, &ConnectorValidationError{Message: "DingTalk pagination indicated more data without a next token"}
			}
			nextToken = next
		}
	}
	return nodes, nil
}

// rootTitle reads the configured root dentry's own name so it becomes the first
// path segment. A failed lookup only drops the prefix.
func (c *DingTalkWikiConnector) rootTitle(ctx context.Context) string {
	if c.rootNodeID == "" {
		return ""
	}
	path := fmt.Sprintf("/v1.0/storage/spaces/%s/dentries/%s", url.PathEscape(c.spaceID), url.PathEscape(c.rootNodeID))
	payload, err := c.dingTalkGet(ctx, path, c.unionIDQuery())
	if err != nil {
		return ""
	}
	return firstNonEmpty(stringConfig(payload["name"]), stringConfig(payload["dentryName"]))
}

// listDirectories lists one page of the child dentries of a directory.
func (c *DingTalkWikiConnector) listDirectories(ctx context.Context, parentID, nextToken string) ([]map[string]any, bool, string, error) {
	query := c.operatorQuery()
	query.Set("maxResults", fmt.Sprint(dingTalkNodePageSize))
	if parentID != "" {
		query.Set("dentryId", parentID)
	}
	if nextToken != "" {
		query.Set("nextToken", nextToken)
	}
	path := fmt.Sprintf("/v2.0/doc/spaces/%s/directories", url.PathEscape(c.spaceID))
	payload, err := c.dingTalkGet(ctx, path, query)
	if err != nil {
		return nil, false, "", err
	}
	next := stringConfig(payload["nextToken"])
	hasMore, _ := payload["hasMore"].(bool)
	return mapItems(payload["children"]), hasMore, next, nil
}

// dingTalkNodeExtension normalizes the dentry's extension to the dotted, lower
// case form the allowlist uses.
func dingTalkNodeExtension(value any) string {
	extension := strings.ToLower(stringConfig(value))
	if extension == "" {
		return ""
	}
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	return extension
}
