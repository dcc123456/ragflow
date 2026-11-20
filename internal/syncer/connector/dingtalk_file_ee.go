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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"ragflow/internal/utility"
)

// dingTalkSupportedExtensions is the set of attachment extensions the connector
// accepts.
var dingTalkSupportedExtensions = map[string]struct{}{
	".csv": {}, ".doc": {}, ".docx": {}, ".eml": {}, ".gif": {}, ".html": {},
	".jpeg": {}, ".jpg": {}, ".json": {}, ".md": {}, ".mdx": {}, ".pdf": {},
	".png": {}, ".ppt": {}, ".pptx": {}, ".tif": {}, ".txt": {}, ".xls": {},
	".xlsx": {},
}

// downloadAttachment fetches one attachment node: the entry's download info
// carries a signed URL that is fetched with a size limit.
func (c *DingTalkWikiConnector) downloadAttachment(ctx context.Context, node dingTalkWikiNode) ([]byte, error) {
	resourceURL, headers, err := c.downloadInfo(ctx, node)
	if err != nil {
		return nil, err
	}
	return c.fetchSignedResource(ctx, resourceURL, headers)
}

// fetchSignedResource downloads one pre-signed resource under the connector's
// size limit. The signed URL points at a storage host, so the validated address
// is pinned for the dial instead of re-resolving it.
func (c *DingTalkWikiConnector) fetchSignedResource(ctx context.Context, resourceURL string, headers map[string]string) ([]byte, error) {
	hostname, resolvedIP, err := connectorAssertURLSafe(resourceURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resourceURL, nil)
	if err != nil {
		return nil, err
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := utility.PinnedHTTPClient(hostname, resolvedIP, dingTalkRequestTimeout).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, &ConnectorValidationError{Message: "DingTalk permission denied while downloading an attachment"}
	}
	if resp.StatusCode >= 400 {
		return nil, &ConnectorValidationError{Message: fmt.Sprintf("DingTalk HTTP %d while downloading an attachment", resp.StatusCode)}
	}
	if declared := resp.Header.Get("Content-Length"); declared != "" {
		if size, parseErr := strconv.ParseInt(strings.TrimSpace(declared), 10, 64); parseErr == nil && size > c.maxFileSizeBytes {
			return nil, &ConnectorValidationError{Message: fmt.Sprintf("DingTalk attachment size exceeds the configured %d-byte limit", c.maxFileSizeBytes)}
		}
	}
	blob, err := io.ReadAll(io.LimitReader(resp.Body, c.maxFileSizeBytes+1))
	if err != nil {
		return nil, &ConnectorValidationError{Message: "Failed to download a DingTalk attachment"}
	}
	if int64(len(blob)) > c.maxFileSizeBytes {
		return nil, &ConnectorValidationError{Message: fmt.Sprintf("DingTalk attachment size exceeds the configured %d-byte limit", c.maxFileSizeBytes)}
	}
	return blob, nil
}

// downloadInfo resolves the signed download URL of one attachment entry plus the
// headers that URL requires.
func (c *DingTalkWikiConnector) downloadInfo(ctx context.Context, node dingTalkWikiNode) (string, map[string]string, error) {
	if node.dentryID == "" {
		return "", nil, &ConnectorValidationError{Message: fmt.Sprintf("DingTalk node %q carries no downloadable entry", node.title)}
	}
	path := fmt.Sprintf("/v1.0/storage/spaces/%s/dentries/%s/downloadInfos/query",
		url.PathEscape(c.spaceID), url.PathEscape(node.dentryID))
	payload, err := c.dingTalkPost(ctx, path, c.unionIDQuery(), map[string]any{})
	if err != nil {
		return "", nil, err
	}
	signature, _ := payload["headerSignatureInfo"].(map[string]any)
	headers := map[string]string{}
	for name, value := range signatureHeaders(signature["headers"]) {
		headers[name] = value
	}
	for _, candidate := range signatureResources(signature["resourceUrls"]) {
		return candidate, headers, nil
	}
	return "", nil, &ConnectorValidationError{Message: fmt.Sprintf("DingTalk returned no download URL for node %q", node.title)}
}

// signatureHeaders flattens the signature header map.
func signatureHeaders(value any) map[string]string {
	headers := map[string]string{}
	raw, _ := value.(map[string]any)
	for name, item := range raw {
		if text := stringConfig(item); text != "" {
			headers[name] = text
		}
	}
	return headers
}

// signatureResources returns the first signed resource URL, which the API may
// send as a single string or a list.
func signatureResources(value any) []string {
	switch typed := value.(type) {
	case string:
		if typed != "" {
			return []string{typed}
		}
	case []any:
		resources := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := stringConfig(item); text != "" {
				resources = append(resources, text)
			}
		}
		return resources
	}
	return nil
}
