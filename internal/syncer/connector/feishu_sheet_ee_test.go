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

func TestFeishuWikiSheetMarkdown(t *testing.T) {
	var valueRanges []string
	server := newFeishuWikiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/open-apis/wiki/v2/spaces/"):
			feishuWriteJSON(w, feishuListResponse([]map[string]any{
				feishuListNode("sht-node", "sheet", "sht-token", "销售表", false, "1700000000"),
			}, false, ""))
		case r.URL.Path == "/open-apis/sheets/v3/spreadsheets/sht-token/sheets/query":
			feishuWriteJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
				"sheets": []any{
					map[string]any{
						"sheet_id":        "sheet-1",
						"title":           "销售",
						"hidden":          false,
						"grid_properties": map[string]any{"row_count": 600, "column_count": 3},
					},
					map[string]any{
						"sheet_id":        "sheet-hidden",
						"title":           "隐藏页",
						"hidden":          true,
						"grid_properties": map[string]any{"row_count": 1, "column_count": 1},
					},
				},
			}})
		case strings.Contains(r.URL.Path, "/open-apis/sheets/v2/spreadsheets/sht-token/values/"):
			// EscapedPath keeps the client's raw encoding, so an encoded
			// separator (`%21` instead of `!`) fails this test instead of
			// slipping through the server-side path decoding.
			escapedPath := r.URL.EscapedPath()
			if strings.Contains(escapedPath, "%21") {
				t.Errorf("range separator was encoded in %q", escapedPath)
			}
			valueRange := escapedPath[strings.Index(escapedPath, "/values/")+len("/values/"):]
			valueRanges = append(valueRanges, valueRange)
			var values []any
			if valueRange == "sheet-1!A1:C500" {
				values = []any{
					[]any{"品类", "本周销量", "上周销量"},
					[]any{"a|b", "556", "478"},
				}
			} else {
				// The trailing all-empty row is what the grid returns for unused rows.
				values = []any{
					[]any{"总账", "10", "9"},
					[]any{nil, nil, nil},
				}
			}
			feishuWriteJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
				"valueRange": map[string]any{"values": values},
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
	if document.SourceID != "feishu_wiki:space-1:sht-node" {
		t.Fatalf("source id = %q", document.SourceID)
	}
	if document.SemanticIdentifier != "销售表" {
		t.Fatalf("semantic identifier = %q", document.SemanticIdentifier)
	}

	markdown := string(document.Blob)
	for _, want := range []string{
		"## 销售",
		"| 品类 | 本周销量 | 上周销量 |",
		"| --- | --- | --- |",
		"| a\\|b | 556 | 478 |",
		"| 总账 | 10 | 9 |",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
	if strings.Contains(markdown, "隐藏页") {
		t.Fatalf("hidden worksheet was synced:\n%s", markdown)
	}
	if strings.Contains(markdown, "|  |  |  |") {
		t.Fatalf("trailing empty row was not trimmed:\n%s", markdown)
	}
	if len(valueRanges) != 2 {
		t.Fatalf("value ranges = %v, want two row chunks", valueRanges)
	}
	if valueRanges[0] != "sheet-1!A1:C500" || valueRanges[1] != "sheet-1!A501:C600" {
		t.Fatalf("value ranges = %v, want sheet-1!A1:C500 and sheet-1!A501:C600", valueRanges)
	}
}

// newFeishuEmbeddedBitableServer serves the wiki listing, the worksheet list and
// the cell values of a spreadsheet that embeds one bitable worksheet, so each
// test only supplies the metadata and bitable responses it cares about.
func newFeishuEmbeddedBitableServer(t *testing.T, metainfo, bitable http.HandlerFunc) *feishuWikiTestServer {
	t.Helper()
	return newFeishuWikiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/open-apis/wiki/v2/spaces/"):
			feishuWriteJSON(w, feishuListResponse([]map[string]any{
				feishuListNode("mix-node", "sheet", "mix-token", "混合表", false, "1700000000"),
			}, false, ""))
		case r.URL.Path == "/open-apis/sheets/v3/spreadsheets/mix-token/sheets/query":
			feishuWriteJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
				"sheets": []any{
					map[string]any{
						"sheet_id":        "sheet-1",
						"title":           "销售",
						"resource_type":   "sheet",
						"hidden":          false,
						"grid_properties": map[string]any{"row_count": 3, "column_count": 2},
					},
					map[string]any{
						"sheet_id":      "sheet-bit",
						"title":         "项目甘特图",
						"resource_type": "bitable",
						"hidden":        false,
					},
				},
			}})
		case strings.Contains(r.URL.Path, "/open-apis/sheets/v2/spreadsheets/mix-token/values/"):
			feishuWriteJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
				"valueRange": map[string]any{"values": []any{
					[]any{"任务", "状态"},
					[]any{"发货", "待办"},
				}},
			}})
		case r.URL.Path == "/open-apis/sheets/v2/spreadsheets/mix-token/metainfo":
			metainfo(w, r)
		case strings.HasPrefix(r.URL.Path, "/open-apis/bitable/v1/apps/"):
			bitable(w, r)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
}

func feishuSheetBlockMetainfo(blockToken string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sheet := map[string]any{"sheetId": "sheet-bit", "title": "项目甘特图"}
		if blockToken != "" {
			sheet["blockInfo"] = map[string]any{"blockToken": blockToken, "blockType": "BITABLE_BLOCK"}
		}
		feishuWriteJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
			"sheets": []any{
				map[string]any{"sheetId": "sheet-1", "title": "销售"},
				sheet,
			},
		}})
	}
}

func TestFeishuWikiSheetEmbeddedBitable(t *testing.T) {
	metainfoCalls := 0
	server := newFeishuEmbeddedBitableServer(t, func(w http.ResponseWriter, r *http.Request) {
		metainfoCalls++
		feishuSheetBlockMetainfo("appTok_tblAbc")(w, r)
	}, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/bitable/v1/apps/appTok/tables":
			feishuWriteJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
				"items":    []any{map[string]any{"table_id": "tblAbc", "name": "项目甘特图"}},
				"has_more": false,
			}})
		case "/open-apis/bitable/v1/apps/appTok/tables/tblAbc/fields":
			feishuWriteJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
				"items":    []any{map[string]any{"field_name": "任务"}, map[string]any{"field_name": "状态"}},
				"has_more": false,
			}})
		case "/open-apis/bitable/v1/apps/appTok/tables/tblAbc/records":
			if r.URL.Query().Get("page_token") == "" {
				feishuWriteJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
					"items":      []any{map[string]any{"record_id": "r1", "fields": map[string]any{"任务": "推广", "状态": "进行中"}}},
					"has_more":   true,
					"page_token": "page-2",
				}})
				return
			}
			feishuWriteJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
				"items":    []any{map[string]any{"record_id": "r2", "fields": map[string]any{"任务": "复盘", "状态": "完成"}}},
				"has_more": false,
			}})
		default:
			t.Errorf("unexpected bitable path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	connector := newFeishuWikiTestConnector(t, server.server, nil)

	document := feishuWikiFirstDocument(t, connector)
	markdown := string(document.Blob)
	for _, want := range []string{
		"## 销售",
		"| 任务 | 状态 |",
		"| 发货 | 待办 |",
		"## 项目甘特图",
		"| 推广 | 进行中 |",
		"| 复盘 | 完成 |",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
	if metainfoCalls != 1 {
		t.Fatalf("metainfo calls = %d, want one (only the bitable worksheet)", metainfoCalls)
	}
}

func TestFeishuWikiSheetEmbeddedBitableErrors(t *testing.T) {
	cases := []struct {
		name      string
		blockInfo string
		tables    []map[string]any
		want      string
	}{
		{
			name:      "missing app token",
			blockInfo: "",
			tables:    nil,
			want:      `worksheet "项目甘特图" is a bitable block but exposes no app token`,
		},
		{
			name:      "unknown block token",
			blockInfo: "appTokWithoutTableSuffix",
			tables:    nil,
			want:      `unrecognised bitable block token`,
		},
		{
			name:      "table not exposed",
			blockInfo: "appTok_tblMissing",
			tables:    []map[string]any{{"table_id": "tblAbc", "name": "项目甘特图"}},
			want:      `references bitable table "tblMissing"`,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := newFeishuEmbeddedBitableServer(t, feishuSheetBlockMetainfo(testCase.blockInfo), func(w http.ResponseWriter, r *http.Request) {
				feishuWriteJSON(w, map[string]any{"code": 0, "msg": "success", "data": map[string]any{
					"items":    testCase.tables,
					"has_more": false,
				}})
			})
			connector := newFeishuWikiTestConnector(t, server.server, nil)

			session, err := connector.OpenSync(context.Background(), SyncRequest{})
			if err != nil {
				t.Fatalf("OpenSync: %v", err)
			}
			_, err = session.NextBatch(context.Background())
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("NextBatch error = %v, want %q", err, testCase.want)
			}
		})
	}
}
