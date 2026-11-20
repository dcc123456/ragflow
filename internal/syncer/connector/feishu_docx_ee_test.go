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
	"net/http"
	"strings"
	"testing"
)

func feishuDocxBlocksResponse(items []map[string]any, hasMore bool, pageToken string) map[string]any {
	data := map[string]any{"items": items, "has_more": hasMore}
	if pageToken != "" {
		data["page_token"] = pageToken
	}
	return map[string]any{"code": 0, "msg": "success", "data": data}
}

func feishuDocxTextBlock(blockID, parentID, content string) map[string]any {
	return map[string]any{
		"block_id":  blockID,
		"parent_id": parentID,
		"text": map[string]any{
			"elements": []any{map[string]any{"text_run": map[string]any{"content": content}}},
		},
	}
}

func TestFeishuWikiDocxMarkdown(t *testing.T) {
	firstPage := []map[string]any{
		{
			"block_id":  "page",
			"parent_id": "",
			"children":  []any{"h1", "p1", "list1", "code", "quote", "table", "todo", "divider", "image"},
			"page":      map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "文档标题"}}}},
		},
		{
			"block_id":  "h1",
			"parent_id": "page",
			"heading1":  map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "概述"}}}},
		},
		{
			"block_id":  "p1",
			"parent_id": "page",
			"text": map[string]any{"elements": []any{
				map[string]any{"text_run": map[string]any{"content": "粗体", "text_element_style": map[string]any{"bold": true}}},
				map[string]any{"text_run": map[string]any{"content": " 与 "}},
				map[string]any{"text_run": map[string]any{"content": "链接", "text_element_style": map[string]any{"link": map[string]any{"url": "https://example.com"}}}},
			}},
		},
		{
			"block_id":  "list1",
			"parent_id": "page",
			"children":  []any{"list1a"},
			"bullet":    map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "项目一"}}}},
		},
		{
			"block_id":  "list1a",
			"parent_id": "list1",
			"bullet":    map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "子项"}}}},
		},
		{
			"block_id":  "code",
			"parent_id": "page",
			"code":      map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "fmt.Println(1)"}}}},
		},
		{
			"block_id":  "quote",
			"parent_id": "page",
			"quote":     map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "引用内容"}}}},
		},
		{
			"block_id":  "table",
			"parent_id": "page",
			"children":  []any{"c1", "c2", "c3", "c4"},
			"table":     map[string]any{"cells": []any{"c1", "c2", "c3", "c4"}, "property": map[string]any{"row_size": 2, "column_size": 2}},
		},
	}
	// The table cells and the trailing blocks arrive on the second page, so the
	// renderer can only resolve them once pagination is complete.
	secondPage := []map[string]any{
		{"block_id": "c1", "parent_id": "table", "children": []any{"c1t"}, "table_cell": map[string]any{}},
		feishuDocxTextBlock("c1t", "c1", "名称"),
		{"block_id": "c2", "parent_id": "table", "children": []any{"c2t"}, "table_cell": map[string]any{}},
		feishuDocxTextBlock("c2t", "c2", "值"),
		{"block_id": "c3", "parent_id": "table", "children": []any{"c3t"}, "table_cell": map[string]any{}},
		feishuDocxTextBlock("c3t", "c3", "a|b"),
		{"block_id": "c4", "parent_id": "table", "children": []any{"c4t"}, "table_cell": map[string]any{}},
		feishuDocxTextBlock("c4t", "c4", "1"),
		{
			"block_id":  "todo",
			"parent_id": "page",
			"todo":      map[string]any{"done": true, "elements": []any{map[string]any{"text_run": map[string]any{"content": "已完成"}}}},
		},
		{"block_id": "divider", "parent_id": "page", "divider": map[string]any{}},
		{"block_id": "image", "parent_id": "page", "image": map[string]any{"token": "img-token"}},
	}

	server := newFeishuWikiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/open-apis/wiki/v2/spaces/"):
			feishuWriteJSON(w, feishuListResponse([]map[string]any{
				feishuListNode("n1", "docx", "odoc", "季度报告", false, "1700000000"),
			}, false, ""))
		case r.URL.Path == "/open-apis/docx/v1/documents/odoc/blocks":
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Errorf("blocks Authorization = %q, want the cached tenant token", got)
			}
			if r.URL.Query().Get("page_token") == "" {
				feishuWriteJSON(w, feishuDocxBlocksResponse(firstPage, true, "page-2"))
				return
			}
			if r.URL.Query().Get("page_token") != "page-2" {
				t.Errorf("unexpected page_token %q", r.URL.Query().Get("page_token"))
			}
			feishuWriteJSON(w, feishuDocxBlocksResponse(secondPage, false, ""))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	connector := newFeishuWikiTestConnector(t, server.server, nil)

	session, err := connector.OpenSync(context.Background(), SyncRequest{})
	if err != nil {
		t.Fatalf("OpenSync: %v", err)
	}
	batch, err := session.NextBatch(context.Background())
	if err != nil {
		t.Fatalf("NextBatch: %v", err)
	}
	if len(batch.Documents) != 1 {
		t.Fatalf("batch = %d documents, want 1", len(batch.Documents))
	}
	document := batch.Documents[0]
	if document.Extension != ".md" {
		t.Fatalf("extension = %q, want .md", document.Extension)
	}
	if document.SourceID != "feishu_wiki:space-1:n1" {
		t.Fatalf("source id = %q", document.SourceID)
	}
	if document.SemanticIdentifier != "季度报告" {
		t.Fatalf("semantic identifier = %q", document.SemanticIdentifier)
	}
	sum := sha256.Sum256(document.Blob)
	if document.Fingerprint != hex.EncodeToString(sum[:])[:32] {
		t.Fatalf("fingerprint = %q, want the content fingerprint", document.Fingerprint)
	}

	markdown := string(document.Blob)
	for _, want := range []string{
		"# 概述",
		"**粗体** 与 [链接](https://example.com)",
		"- 项目一\n  - 子项",
		"```\nfmt.Println(1)\n```",
		"> 引用内容",
		"| 名称 | 值 |\n| --- | --- |\n| a\\|b | 1 |",
		"- [x] 已完成",
		"---",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
	if strings.Contains(markdown, "img-token") || strings.Contains(markdown, "文档标题") {
		t.Fatalf("markdown rendered a skipped block:\n%s", markdown)
	}
}

func TestFeishuWikiDocxBlocksError(t *testing.T) {
	server := newFeishuWikiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/open-apis/wiki/v2/spaces/") {
			feishuWriteJSON(w, feishuListResponse([]map[string]any{
				feishuListNode("n1", "docx", "odoc", "季度报告", false, "1700000000"),
			}, false, ""))
			return
		}
		w.WriteHeader(http.StatusForbidden)
		feishuWriteJSON(w, map[string]any{"code": 1770032, "msg": "no permission"})
	})
	connector := newFeishuWikiTestConnector(t, server.server, nil)

	session, err := connector.OpenSync(context.Background(), SyncRequest{})
	if err != nil {
		t.Fatalf("OpenSync: %v", err)
	}
	if _, err := session.NextBatch(context.Background()); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("NextBatch error = %v, want a permission error", err)
	}
}
