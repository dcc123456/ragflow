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
	"net/http"
	"strings"
	"testing"
)

func TestDingTalkWikiSheetMarkdown(t *testing.T) {
	var valueRanges []string
	server := newDingTalkWikiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2.0/doc/spaces/space-1/directories":
			dingTalkWriteJSON(w, map[string]any{"children": []any{
				dingTalkDentry("uuid-1", "dentry-1", "季度销售.axls", "file", "axls", false, nil),
			}})
		case r.URL.Path == "/v1.0/doc/workbooks/uuid-1/sheets":
			dingTalkWriteJSON(w, map[string]any{"value": []any{
				map[string]any{"id": "s1", "name": "销售"},
				map[string]any{"id": "s2", "name": "库存"},
				map[string]any{"id": "s3", "name": "空表"},
			}})
		case strings.HasPrefix(r.URL.Path, "/v1.0/doc/workbooks/uuid-1/sheets/") && !strings.Contains(r.URL.Path, "/ranges/"):
			sheetID := strings.TrimPrefix(r.URL.Path, "/v1.0/doc/workbooks/uuid-1/sheets/")
			dimensions := map[string][2]int{"s1": {3, 3}, "s2": {3, 2}, "s3": {2, 2}}[sheetID]
			dingTalkWriteJSON(w, map[string]any{"id": sheetID, "name": sheetID, "rowCount": dimensions[0], "columnCount": dimensions[1]})
		case strings.HasPrefix(r.URL.Path, "/v1.0/doc/workbooks/uuid-1/sheets/") && strings.Contains(r.URL.Path, "/ranges/"):
			parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/v1.0/doc/workbooks/uuid-1/sheets/"), "/ranges/", 2)
			sheetID, rangeRef := parts[0], parts[1]
			valueRanges = append(valueRanges, sheetID+"|"+rangeRef)
			switch sheetID {
			case "s1":
				dingTalkWriteJSON(w, map[string]any{"values": []any{
					[]any{"品类", "本周销量", "上周销量"},
					[]any{"a|b", 556, 478},
				}})
			case "s2":
				dingTalkWriteJSON(w, map[string]any{"values": []any{
					[]any{"门店", "库存"},
					[]any{"静安店", 12},
					[]any{nil, nil},
				}})
			default:
				dingTalkWriteJSON(w, map[string]any{"values": []any{
					[]any{nil, nil},
				}})
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
		"## 销售",
		"| 品类 | 本周销量 | 上周销量 |",
		"| a\\|b | 556 | 478 |",
		"## 库存",
		"| 门店 | 库存 |",
		"| 静安店 | 12 |",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
	if strings.Contains(markdown, "空表") {
		t.Fatalf("an empty worksheet must not become a section:\n%s", markdown)
	}
	if len(valueRanges) != 3 {
		t.Fatalf("range requests = %v, want one per worksheet", valueRanges)
	}
	if valueRanges[0] != "s1|A1:C3" {
		t.Fatalf("first range = %q, want s1|A1:C3", valueRanges[0])
	}
}
