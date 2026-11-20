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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

// feishuSupportedExtensions is the set of downloadable Wiki file extensions the
// connector accepts, mirroring the Python connector's allowlist.
var feishuSupportedExtensions = map[string]struct{}{
	".csv": {}, ".doc": {}, ".docx": {}, ".eml": {}, ".gif": {}, ".html": {},
	".jpeg": {}, ".jpg": {}, ".json": {}, ".md": {}, ".mdx": {}, ".pdf": {},
	".png": {}, ".ppt": {}, ".pptx": {}, ".tif": {}, ".txt": {}, ".xls": {},
	".xlsx": {},
}

// feishuAttachmentContent downloads one attachment node and derives the
// extension of the document row it becomes.
func (c *FeishuWikiConnector) feishuAttachmentContent(ctx context.Context, node feishuWikiNode) ([]byte, string, error) {
	blob, err := c.downloadFile(ctx, node.objToken)
	if err != nil {
		return nil, "", err
	}
	return blob, strings.ToLower(path.Ext(node.title)), nil
}

// downloadFile streams one Wiki file, enforcing the configured size limit and
// rejecting Feishu JSON error envelopes so they are never ingested as content.
func (c *FeishuWikiConnector) downloadFile(ctx context.Context, objectToken string) ([]byte, error) {
	token, err := c.getAccessToken(ctx)
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("%s/open-apis/drive/v1/files/%s/download", c.baseURL, url.PathEscape(objectToken))
	resp, err := c.feishuDoRequest(ctx, http.MethodGet, endpoint, nil, map[string]string{"Authorization": "Bearer " + token})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, &ConnectorValidationError{Message: "Feishu permission denied while downloading a Wiki file"}
	}
	if resp.StatusCode >= 400 {
		code, message := feishuErrorEnvelope(resp)
		if code != 0 {
			return nil, &ConnectorValidationError{Message: fmt.Sprintf("Feishu API error %d (HTTP %d) while downloading a Wiki file: %s", code, resp.StatusCode, message)}
		}
		return nil, &ConnectorValidationError{Message: fmt.Sprintf("Feishu HTTP %d while downloading a Wiki file", resp.StatusCode)}
	}

	if declared := resp.Header.Get("Content-Length"); declared != "" {
		if declaredSize, parseErr := strconv.ParseInt(strings.TrimSpace(declared), 10, 64); parseErr == nil && declaredSize > c.maxFileSizeBytes {
			return nil, &ConnectorValidationError{Message: fmt.Sprintf("Feishu Wiki file size exceeds the configured %d-byte limit", c.maxFileSizeBytes)}
		}
	}

	// Feishu can return HTTP 200 with a JSON error envelope (non-zero code)
	// instead of file bytes. A legitimate JSON file comes back with code 0 and
	// is returned as content below.
	if contentType := resp.Header.Get("Content-Type"); strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "application/json") {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, c.maxFileSizeBytes+1))
		if readErr != nil {
			return nil, &ConnectorValidationError{Message: "Failed to read a Feishu Wiki file"}
		}
		if int64(len(body)) > c.maxFileSizeBytes {
			return nil, &ConnectorValidationError{Message: fmt.Sprintf("Feishu Wiki file size exceeds the configured %d-byte limit", c.maxFileSizeBytes)}
		}
		var envelope struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		if json.Unmarshal(body, &envelope) == nil && envelope.Code != 0 {
			return nil, &ConnectorValidationError{Message: fmt.Sprintf("Feishu returned a JSON error envelope instead of Wiki file content (code %d): %s", envelope.Code, envelope.Msg)}
		}
		return body, nil
	}

	blob, readErr := io.ReadAll(io.LimitReader(resp.Body, c.maxFileSizeBytes+1))
	if readErr != nil {
		return nil, &ConnectorValidationError{Message: "Failed to download a Feishu Wiki file"}
	}
	if int64(len(blob)) > c.maxFileSizeBytes {
		return nil, &ConnectorValidationError{Message: fmt.Sprintf("Feishu Wiki file size exceeds the configured %d-byte limit", c.maxFileSizeBytes)}
	}
	return blob, nil
}
