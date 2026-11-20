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

func TestFeishuWikiBitableMarkdown(t *testing.T) {
	server := newFeishuWikiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/open-apis/wiki/v2/spaces/"):
			feishuWriteJSON(w, feishuListResponse([]map[string]any{
				feishuListNode("bit-node", "bitable", "app-token", "经营表", false, "1700000000"),
			}, false, ""))
		case r.URL.Path == "/open-apis/bitable/v1/apps/app-token/tables":
			feishuWriteJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
				"items":      []any{map[string]any{"table_id": "tbl-1", "name": "门店销售"}},
				"has_more":   false,
				"page_token": "",
			}})
		case r.URL.Path == "/open-apis/bitable/v1/apps/app-token/tables/tbl-1/fields":
			feishuWriteJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
				"items": []any{
					map[string]any{"field_name": "门店"},
					map[string]any{"field_name": "销量"},
				},
				"has_more": false,
			}})
		case r.URL.Path == "/open-apis/bitable/v1/apps/app-token/tables/tbl-1/records":
			if r.URL.Query().Get("page_token") == "" {
				feishuWriteJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
					"items": []any{
						map[string]any{"record_id": "r1", "fields": map[string]any{"门店": "静安店", "销量": 4972}},
						map[string]any{"record_id": "r2", "fields": map[string]any{"门店": []any{map[string]any{"text": "徐汇店"}}, "销量": map[string]any{"value": []any{1}}}},
					},
					"has_more":   true,
					"page_token": "page-2",
				}})
				return
			}
			feishuWriteJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
				"items": []any{
					map[string]any{"record_id": "r3", "fields": map[string]any{
						"门店": map[string]any{"link": "https://feishu.cn/x", "text": "浦东店"},
						"销量": true,
					}},
				},
				"has_more": false,
			}})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	connector := newFeishuWikiTestConnector(t, server.server, nil)

	document := feishuWikiFirstDocument(t, connector)
	if document.Extension != ".md" {
		t.Fatalf("extension = %q, want .md", document.Extension)
	}

	markdown := string(document.Blob)
	for _, want := range []string{
		"## 门店销售",
		"| 门店 | 销量 |",
		"| 静安店 | 4972 |",
		"| 徐汇店 | {\"value\":[1]} |",
		"| 浦东店 | true |",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
}

// feishuBitableServer serves one bitable node whose tables, fields and records
// responses come from the caller, so pagination behaviour can be asserted
// request by request.
func feishuBitableServer(t *testing.T, handle http.HandlerFunc) *feishuWikiTestServer {
	t.Helper()
	return newFeishuWikiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/open-apis/wiki/v2/spaces/") {
			feishuWriteJSON(w, feishuListResponse([]map[string]any{
				feishuListNode("bit-node", "bitable", "app-token", "经营表", false, "1700000000"),
			}, false, ""))
			return
		}
		handle(w, r)
	})
}

func TestFeishuWikiBitableStopsWhenHasMoreFalse(t *testing.T) {
	var tablesCalls, fieldsCalls, recordsCalls int
	server := feishuBitableServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/bitable/v1/apps/app-token/tables":
			tablesCalls++
			// Feishu also returns a page token on the last page.
			feishuWriteJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
				"items":      []any{map[string]any{"table_id": "tbl-1", "name": "门店销售"}},
				"has_more":   false,
				"page_token": "tblStale",
			}})
		case "/open-apis/bitable/v1/apps/app-token/tables/tbl-1/fields":
			fieldsCalls++
			feishuWriteJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
				"items":      []any{map[string]any{"field_name": "门店"}, map[string]any{"field_name": "销量"}},
				"has_more":   false,
				"page_token": "fldStale",
			}})
		case "/open-apis/bitable/v1/apps/app-token/tables/tbl-1/records":
			recordsCalls++
			feishuWriteJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
				"items":      []any{map[string]any{"record_id": "r1", "fields": map[string]any{"门店": "静安店", "销量": 4972}}},
				"has_more":   false,
				"page_token": "recStale",
			}})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	connector := newFeishuWikiTestConnector(t, server.server, nil)

	document := feishuWikiFirstDocument(t, connector)
	markdown := string(document.Blob)
	for _, want := range []string{"## 门店销售", "| 门店 | 销量 |", "| 静安店 | 4972 |"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
	if tablesCalls != 1 || fieldsCalls != 1 || recordsCalls != 1 {
		t.Fatalf("requests = tables:%d fields:%d records:%d, want one each", tablesCalls, fieldsCalls, recordsCalls)
	}
}

func TestFeishuWikiBitableTablesPagination(t *testing.T) {
	var tablesCalls int
	var pageTokens []string
	server := feishuBitableServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/bitable/v1/apps/app-token/tables":
			tablesCalls++
			pageToken := r.URL.Query().Get("page_token")
			pageTokens = append(pageTokens, pageToken)
			if pageToken == "" {
				feishuWriteJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
					"items":      []any{map[string]any{"table_id": "tbl-1", "name": "门店销售"}},
					"has_more":   true,
					"page_token": "page-2",
				}})
				return
			}
			feishuWriteJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
				"items":      []any{map[string]any{"table_id": "tbl-2", "name": "门店库存"}},
				"has_more":   false,
				"page_token": "tblStale",
			}})
		case "/open-apis/bitable/v1/apps/app-token/tables/tbl-1/fields",
			"/open-apis/bitable/v1/apps/app-token/tables/tbl-2/fields":
			feishuWriteJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
				"items":    []any{map[string]any{"field_name": "门店"}},
				"has_more": false,
			}})
		case "/open-apis/bitable/v1/apps/app-token/tables/tbl-1/records",
			"/open-apis/bitable/v1/apps/app-token/tables/tbl-2/records":
			feishuWriteJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
				"items":    []any{map[string]any{"record_id": "r1", "fields": map[string]any{"门店": "静安店"}}},
				"has_more": false,
			}})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	connector := newFeishuWikiTestConnector(t, server.server, nil)

	document := feishuWikiFirstDocument(t, connector)
	markdown := string(document.Blob)
	for _, want := range []string{"## 门店销售", "## 门店库存"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
	if tablesCalls != 2 {
		t.Fatalf("tables requests = %d, want 2", tablesCalls)
	}
	if len(pageTokens) != 2 || pageTokens[0] != "" || pageTokens[1] != "page-2" {
		t.Fatalf("page tokens = %v, want [\"\" \"page-2\"]", pageTokens)
	}
}

func TestFeishuWikiBitableRepeatedPageToken(t *testing.T) {
	cases := []struct {
		name  string
		path  string
		other string
	}{
		{
			name:  "tables",
			path:  "/open-apis/bitable/v1/apps/app-token/tables",
			other: "",
		},
		{
			name:  "records",
			path:  "/open-apis/bitable/v1/apps/app-token/tables/tbl-1/records",
			other: "/open-apis/bitable/v1/apps/app-token/tables",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			calls := 0
			server := feishuBitableServer(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case testCase.other:
					// The records case needs the table list to terminate.
					feishuWriteJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
						"items":    []any{map[string]any{"table_id": "tbl-1", "name": "门店销售"}},
						"has_more": false,
					}})
				case "/open-apis/bitable/v1/apps/app-token/tables/tbl-1/fields":
					feishuWriteJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
						"items":    []any{map[string]any{"field_name": "门店"}},
						"has_more": false,
					}})
				case testCase.path:
					calls++
					items := []any{map[string]any{"table_id": "tbl-1", "name": "门店销售"}}
					if testCase.name == "records" {
						items = []any{map[string]any{"record_id": "r1", "fields": map[string]any{"门店": "静安店"}}}
					}
					// A paginator that keeps claiming more data with the same
					// token must fail instead of requesting forever.
					feishuWriteJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
						"items":      items,
						"has_more":   true,
						"page_token": "page-1",
					}})
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
			_, err = session.NextBatch(context.Background())
			if err == nil || !strings.Contains(err.Error(), "repeated a page token") {
				t.Fatalf("NextBatch error = %v, want a repeated page token error", err)
			}
			if calls > 2 {
				t.Fatalf("%s requests = %d, want at most 2", testCase.name, calls)
			}
		})
	}
}
