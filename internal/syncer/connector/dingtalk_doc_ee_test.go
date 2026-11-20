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
	"strconv"
	"strings"
	"testing"
)

// dingTalkDocDentry builds a native document dentry the way the directory API
// returns it: a file entry whose extension marks the kind and which carries the
// doc key its content is read with.
func dingTalkDocDentry(dentryUUID, dentryID, name, extension string, updatedAt any) map[string]any {
	dentry := dingTalkDentry(dentryUUID, dentryID, name, "file", extension, false, updatedAt)
	dentry["docKey"] = "doc-key-" + dentryUUID
	return dentry
}

func TestDingTalkWikiDocMarkdown(t *testing.T) {
	server := newDingTalkWikiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2.0/doc/spaces/space-1/directories":
			dingTalkWriteJSON(w, map[string]any{"children": []any{
				dingTalkDocDentry("uuid-1", "dentry-1", "季度报告.adoc", "adoc", 1700000000),
			}})
		case r.URL.Path == "/v1.0/doc/suites/documents/doc-key-uuid-1/blocks":
			if got := r.URL.Query().Get("operatorId"); got != "union-1" {
				t.Errorf("operatorId = %q, want union-1", got)
			}
			switch r.URL.Query().Get("startIndex") {
			case "0":
				if got := r.URL.Query().Get("endIndex"); got != "199" {
					t.Errorf("endIndex = %q, want 199", got)
				}
				dingTalkWriteJSON(w, map[string]any{"result": map[string]any{"data": []any{
					map[string]any{"blockType": "paragraph", "index": 0, "paragraph": map[string]any{"text": "123jklfdasjjkfl"}},
					map[string]any{"blockType": "heading", "index": 1, "heading": map[string]any{"level": "heading-2", "text": "概述"}},
					map[string]any{"blockType": "table", "index": 2, "table": map[string]any{
						"colSize": 2,
						"rowSize": 2,
						"cells": []any{
							[]any{"名称", "值"},
							[]any{[]any{"a|b", "第二行"}, "1"},
						},
					}},
					map[string]any{"blockType": "paragraph", "index": 3, "paragraph": map[string]any{
						"text": "",
						"elements": []any{
							map[string]any{"elementType": "image", "image": map[string]any{"src": "/core/api/resources/img/img-token"}},
						},
					}},
					map[string]any{"blockType": "divider", "index": 4},
				}}, "success": true})
			default:
				t.Errorf("unexpected startIndex %q", r.URL.Query().Get("startIndex"))
				http.NotFound(w, r)
			}
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	connector := newDingTalkWikiTestConnector(t, server.server, nil)

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
	if document.SourceID != "dingtalk_wiki:space-1:uuid-1" {
		t.Fatalf("source id = %q", document.SourceID)
	}
	if document.Metadata["dingtalk_dentry_uuid"] != "uuid-1" {
		t.Fatalf("metadata dentry uuid = %v", document.Metadata["dingtalk_dentry_uuid"])
	}
	markdown := string(document.Blob)
	for _, want := range []string{
		"123jklfdasjjkfl",
		"## 概述",
		"| 名称 | 值 |\n| --- | --- |\n| a\\|b<br>第二行 | 1 |",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
	if strings.Contains(markdown, "img-token") {
		t.Fatalf("image element leaked into the body:\n%s", markdown)
	}
	sum := sha256.Sum256(document.Blob)
	if document.Fingerprint != hex.EncodeToString(sum[:])[:32] {
		t.Fatalf("fingerprint = %q, want the content hash", document.Fingerprint)
	}
}

func TestDingTalkWikiDocBlockPaging(t *testing.T) {
	page := make([]any, 0, dingTalkBlockPageSize)
	for index := 0; index < dingTalkBlockPageSize; index++ {
		page = append(page, map[string]any{
			"blockType": "paragraph",
			"index":     index,
			"paragraph": map[string]any{"text": "第" + strconv.Itoa(index) + "段"},
		})
	}
	pages := 0
	server := newDingTalkWikiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2.0/doc/spaces/space-1/directories":
			dingTalkWriteJSON(w, map[string]any{"children": []any{
				dingTalkDocDentry("uuid-1", "dentry-1", "长文档.adoc", "adoc", nil),
			}})
		case r.URL.Path == "/v1.0/doc/suites/documents/doc-key-uuid-1/blocks":
			pages++
			if r.URL.Query().Get("startIndex") == "0" {
				dingTalkWriteJSON(w, map[string]any{"result": map[string]any{"data": page}})
				return
			}
			if got := r.URL.Query().Get("startIndex"); got != strconv.Itoa(dingTalkBlockPageSize) {
				t.Errorf("startIndex = %q, want %d", got, dingTalkBlockPageSize)
			}
			dingTalkWriteJSON(w, map[string]any{"result": map[string]any{"data": []any{
				map[string]any{"blockType": "paragraph", "index": dingTalkBlockPageSize, "paragraph": map[string]any{"text": "最后一段"}},
			}}})
		default:
			http.NotFound(w, r)
		}
	})
	connector := newDingTalkWikiTestConnector(t, server.server, nil)

	session, err := connector.OpenSync(context.Background(), SyncRequest{})
	if err != nil {
		t.Fatalf("OpenSync: %v", err)
	}
	batch, err := session.NextBatch(context.Background())
	if err != nil {
		t.Fatalf("NextBatch: %v", err)
	}
	markdown := string(batch.Documents[0].Blob)
	if !strings.Contains(markdown, "第0段") || !strings.Contains(markdown, "最后一段") {
		t.Fatalf("markdown lost a page:\n%.200s", markdown)
	}
	if pages != 2 {
		t.Fatalf("pages = %d, want 2", pages)
	}
}

func TestDingTalkWikiDocEmptyBody(t *testing.T) {
	server := newDingTalkWikiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2.0/doc/spaces/space-1/directories":
			dingTalkWriteJSON(w, map[string]any{"children": []any{
				dingTalkDocDentry("uuid-1", "dentry-1", "空白文档.adoc", "adoc", nil),
			}})
		case r.URL.Path == "/v1.0/doc/suites/documents/doc-key-uuid-1/blocks":
			dingTalkWriteJSON(w, map[string]any{"result": map[string]any{"data": []any{}}})
		default:
			http.NotFound(w, r)
		}
	})
	connector := newDingTalkWikiTestConnector(t, server.server, nil)

	session, err := connector.OpenSync(context.Background(), SyncRequest{})
	if err != nil {
		t.Fatalf("OpenSync: %v", err)
	}
	batch, err := session.NextBatch(context.Background())
	if err != nil {
		t.Fatalf("NextBatch: %v", err)
	}
	document := batch.Documents[0]
	if string(document.Blob) != "空白文档.adoc" {
		t.Fatalf("body = %q, want the node title", document.Blob)
	}
	if document.Extension != ".md" {
		t.Fatalf("extension = %q, want .md", document.Extension)
	}
}

func TestDingTalkWikiDocMissingDocKey(t *testing.T) {
	server := newDingTalkWikiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2.0/doc/spaces/space-1/directories":
			dentry := dingTalkDocDentry("uuid-1", "dentry-1", "无键文档.adoc", "adoc", nil)
			delete(dentry, "docKey")
			dingTalkWriteJSON(w, map[string]any{"children": []any{dentry}})
		default:
			http.NotFound(w, r)
		}
	})
	connector := newDingTalkWikiTestConnector(t, server.server, nil)

	session, err := connector.OpenSync(context.Background(), SyncRequest{})
	if err != nil {
		t.Fatalf("OpenSync: %v", err)
	}
	if _, err := session.NextBatch(context.Background()); err == nil {
		t.Fatal("NextBatch succeeded, want a missing doc key error")
	}
}
