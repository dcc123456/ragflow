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
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type dingTalkWikiTestServer struct {
	server    *httptest.Server
	mu        sync.Mutex
	authCalls int
}

func (s *dingTalkWikiTestServer) authCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authCalls
}

func dingTalkWriteJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func newDingTalkWikiTestServer(t *testing.T, handle http.HandlerFunc) *dingTalkWikiTestServer {
	t.Helper()
	fixture := &dingTalkWikiTestServer{}
	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1.0/oauth2/accessToken" {
			fixture.mu.Lock()
			fixture.authCalls++
			fixture.mu.Unlock()
			dingTalkWriteJSON(w, map[string]any{"accessToken": "test-token", "expireIn": 7200})
			return
		}
		// Only the API calls carry the access token; the signed download URL is
		// fetched from the storage host without it.
		if strings.HasPrefix(r.URL.Path, "/v1.0/") {
			if got := r.Header.Get("x-acs-dingtalk-access-token"); got != "test-token" {
				t.Errorf("access token header = %q, want test-token", got)
			}
		}
		handle(w, r)
	}))
	t.Cleanup(fixture.server.Close)
	return fixture
}

func newDingTalkWikiTestConnector(t *testing.T, server *httptest.Server, config map[string]any) *DingTalkWikiConnector {
	t.Helper()
	merged := map[string]any{
		"credentials": map[string]any{"app_key": "key", "app_secret": "secret"},
		"operator_id": "union-1",
		"space_id":    "space-1",
	}
	for key, value := range config {
		merged[key] = value
	}
	connector, err := NewDingTalkWikiConnector(merged)
	if err != nil {
		t.Fatalf("NewDingTalkWikiConnector: %v", err)
	}
	connector.baseURL = server.URL
	connector.httpClient = server.Client()
	return connector
}

func newDingTalkDriveTestConnector(t *testing.T, server *httptest.Server, spaceID string) *DingTalkWikiConnector {
	t.Helper()
	connector, err := NewDingTalkDriveConnector(map[string]any{
		"credentials": map[string]any{"app_key": "key", "app_secret": "secret"},
		"operator_id": "union-1",
		"space_id":    spaceID,
	})
	if err != nil {
		t.Fatalf("NewDingTalkDriveConnector: %v", err)
	}
	connector.baseURL = server.URL
	connector.httpClient = server.Client()
	return connector
}

func dingTalkDentry(dentryUUID, dentryID, name, dentryType, extension string, hasChildren bool, updatedAt any) map[string]any {
	dentry := map[string]any{
		"dentryUuid":  dentryUUID,
		"dentryId":    dentryID,
		"name":        name,
		"dentryType":  dentryType,
		"extension":   extension,
		"hasChildren": hasChildren,
	}
	if updatedAt != nil {
		dentry["updatedTime"] = updatedAt
	}
	return dentry
}

func TestDingTalkWikiTokenCaching(t *testing.T) {
	server := newDingTalkWikiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		dingTalkWriteJSON(w, map[string]any{"children": []any{}})
	})
	connector := newDingTalkWikiTestConnector(t, server.server, nil)

	for i := 0; i < 2; i++ {
		if _, err := connector.collectNodes(context.Background()); err != nil {
			t.Fatalf("collectNodes: %v", err)
		}
	}
	if got := server.authCount(); got != 1 {
		t.Fatalf("auth calls = %d, want 1 cached token", got)
	}
}

func TestDingTalkWikiCollectNodes(t *testing.T) {
	server := newDingTalkWikiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("operatorId") != "union-1" {
			t.Errorf("operatorId = %q", r.URL.Query().Get("operatorId"))
		}
		if !strings.HasPrefix(r.URL.Path, "/v2.0/doc/spaces/space-1/directories") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		parent := r.URL.Query().Get("dentryId")
		nextToken := r.URL.Query().Get("nextToken")
		switch parent {
		case "":
			if nextToken == "" {
				dingTalkWriteJSON(w, map[string]any{
					"children":  []any{dingTalkDentry("uuid-1", "dentry-1", "a.pdf", "file", "pdf", false, 1700000000)},
					"hasMore":   true,
					"nextToken": "page-2",
				})
				return
			}
			// DingTalk sends a nextToken even on the last page, so only hasMore
			// may end the loop.
			dingTalkWriteJSON(w, map[string]any{
				"children":  []any{dingTalkDentry("uuid-2", "dentry-2", "season", "folder", "", true, nil)},
				"nextToken": "pos:65536.0",
			})
		case "dentry-2":
			dingTalkWriteJSON(w, map[string]any{
				"children": []any{dingTalkDentry("uuid-3", "dentry-3", "b.docx", "file", "docx", false, 1700000001)},
			})
		default:
			dingTalkWriteJSON(w, map[string]any{"children": []any{}})
		}
	})
	connector := newDingTalkWikiTestConnector(t, server.server, nil)

	nodes, err := connector.collectNodes(context.Background())
	if err != nil {
		t.Fatalf("collectNodes: %v", err)
	}
	got := make([]string, 0, len(nodes))
	for _, node := range nodes {
		got = append(got, node.dentryUUID)
	}
	want := []string{"uuid-1", "uuid-2", "uuid-3"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("collected %v, want %v", got, want)
	}
	if connector.sourceID(nodes[0]) != "dingtalk_wiki:space-1:uuid-1" {
		t.Fatalf("source id = %q", connector.sourceID(nodes[0]))
	}
	if nodes[0].extension != ".pdf" {
		t.Fatalf("extension = %q, want .pdf", nodes[0].extension)
	}
	if !nodes[0].hasUpdatedAt || !nodes[0].updatedAt.Equal(timeUnix(1700000000)) {
		t.Fatalf("first node updatedAt = %v (has=%v)", nodes[0].updatedAt, nodes[0].hasUpdatedAt)
	}
}

func TestDingTalkWikiFiltersAndSyncableNodes(t *testing.T) {
	server := newDingTalkWikiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		dingTalkWriteJSON(w, map[string]any{"children": []any{
			dingTalkDentry("uuid-1", "dentry-1", "Project Handbook.pdf", "file", "pdf", false, 1700000000),
			dingTalkDentry("uuid-2", "dentry-2", "Handbook Draft.pdf", "file", "pdf", false, 1700000001),
			dingTalkDentry("uuid-3", "dentry-3", "report.exe", "file", "exe", false, 1700000002),
			dingTalkDentry("uuid-4", "dentry-4", "Project Handbook.adoc", "file", "adoc", false, 1700000003),
		}})
	})
	connector := newDingTalkWikiTestConnector(t, server.server, map[string]any{
		"include_extensions": []any{"pdf"},
		"include_keywords":   []any{"handbook"},
		"exclude_keywords":   []any{"draft"},
	})

	session, err := connector.OpenSync(context.Background(), SyncRequest{})
	if err != nil {
		t.Fatalf("OpenSync: %v", err)
	}
	syncSession := session.(*dingTalkWikiSyncSession)
	// The native document bypasses the extension allowlist.
	if len(syncSession.nodes) != 2 || syncSession.nodes[0].dentryUUID != "uuid-1" || syncSession.nodes[1].dentryUUID != "uuid-4" {
		t.Fatalf("accepted nodes = %+v, want the handbook pdf and doc", syncSession.nodes)
	}
}

func TestDingTalkDriveSpaceSkipsAttachments(t *testing.T) {
	var downloadInfoCalls int
	server := newDingTalkWikiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2.0/doc/spaces/29999417290/directories":
			dingTalkWriteJSON(w, map[string]any{"children": []any{
				dingTalkDentry("uuid-1", "dentry-1", "截图.png", "file", "png", false, 1700000000),
				dingTalkDocDentry("uuid-2", "dentry-2", "会议纪要.adoc", "adoc", 1700000001),
			}})
		case strings.Contains(r.URL.Path, "/downloadInfos/query"):
			downloadInfoCalls++
			http.Error(w, "drive attachments must not be downloaded", http.StatusInternalServerError)
		case r.URL.Path == "/v1.0/doc/suites/documents/doc-key-uuid-2/blocks":
			dingTalkWriteJSON(w, map[string]any{"result": map[string]any{"data": []any{
				map[string]any{"blockType": "paragraph", "index": 0, "paragraph": map[string]any{"text": "会议内容"}},
			}}, "success": true})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	connector := newDingTalkDriveTestConnector(t, server.server, "29999417290")

	session, err := connector.OpenSync(context.Background(), SyncRequest{})
	if err != nil {
		t.Fatalf("OpenSync: %v", err)
	}
	syncSession := session.(*dingTalkWikiSyncSession)
	if len(syncSession.nodes) != 1 || syncSession.nodes[0].dentryUUID != "uuid-2" {
		t.Fatalf("accepted nodes = %+v, want only the native document", syncSession.nodes)
	}
	batch, err := session.NextBatch(context.Background())
	if err != nil {
		t.Fatalf("NextBatch: %v", err)
	}
	if got := string(batch.Documents[0].Blob); got != "会议内容" {
		t.Fatalf("body = %q, want the document body", got)
	}
	if syncSession.connector.sourceID(syncSession.nodes[0]) != "dingtalk_drive:29999417290:uuid-2" {
		t.Fatalf("source id = %q", syncSession.connector.sourceID(syncSession.nodes[0]))
	}
	if downloadInfoCalls != 0 {
		t.Fatalf("download info calls = %d, want 0", downloadInfoCalls)
	}
}

func TestDingTalkDriveSourceSkipsAttachments(t *testing.T) {
	var downloadInfoCalls int
	server := newDingTalkWikiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2.0/doc/spaces/29999417290/directories":
			dingTalkWriteJSON(w, map[string]any{"children": []any{
				dingTalkDentry("uuid-1", "dentry-1", "合同.pdf", "file", "pdf", false, 1700000000),
				dingTalkDocDentry("uuid-2", "dentry-2", "会议纪要.adoc", "adoc", 1700000001),
			}})
		case strings.Contains(r.URL.Path, "/downloadInfos/query"):
			downloadInfoCalls++
			http.Error(w, "drive attachments must not be downloaded", http.StatusInternalServerError)
		case r.URL.Path == "/v1.0/doc/suites/documents/doc-key-uuid-2/blocks":
			dingTalkWriteJSON(w, map[string]any{"result": map[string]any{"data": []any{
				map[string]any{"blockType": "paragraph", "index": 0, "paragraph": map[string]any{"text": "会议内容"}},
			}}, "success": true})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	merged := map[string]any{
		"credentials": map[string]any{"app_key": "key", "app_secret": "secret"},
		"operator_id": "union-1",
		"space_id":    "29999417290",
	}
	connector, err := NewDingTalkDriveConnector(merged)
	if err != nil {
		t.Fatalf("NewDingTalkDriveConnector: %v", err)
	}
	connector.baseURL = server.server.URL
	connector.httpClient = server.server.Client()

	session, err := connector.OpenSync(context.Background(), SyncRequest{})
	if err != nil {
		t.Fatalf("OpenSync: %v", err)
	}
	syncSession := session.(*dingTalkWikiSyncSession)
	if len(syncSession.nodes) != 1 || syncSession.nodes[0].dentryUUID != "uuid-2" {
		t.Fatalf("accepted nodes = %+v, want only the native document", syncSession.nodes)
	}
	if downloadInfoCalls != 0 {
		t.Fatalf("download info calls = %d, want 0", downloadInfoCalls)
	}
}

// dingTalkPruneSourceIDs drains one prune session and returns its source ids
// together with the number of batches it produced.
func dingTalkPruneSourceIDs(t *testing.T, session PruneSession) ([]string, int) {
	t.Helper()
	var sourceIDs []string
	batches := 0
	for {
		batch, err := session.NextBatch(context.Background())
		if errors.Is(err, io.EOF) {
			return sourceIDs, batches
		}
		if err != nil {
			t.Fatalf("NextBatch: %v", err)
		}
		batches++
		for _, document := range batch.Documents {
			sourceIDs = append(sourceIDs, document.SourceID)
		}
	}
}

func TestDingTalkWikiPruneSnapshot(t *testing.T) {
	var contentCalls int
	server := newDingTalkWikiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/downloadInfos/") || strings.HasSuffix(r.URL.Path, "/blocks") {
			contentCalls++
			http.Error(w, "prune must not read document content", http.StatusInternalServerError)
			return
		}
		if r.URL.Path != "/v2.0/doc/spaces/space-1/directories" {
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		docWithChild := dingTalkDocDentry("uuid-doc", "dentry-doc", "会议纪要.adoc", "adoc", 1700000004)
		docWithChild["hasChildren"] = true
		switch r.URL.Query().Get("dentryId") {
		case "":
			dingTalkWriteJSON(w, map[string]any{"children": []any{
				dingTalkDentry("uuid-folder", "dentry-folder", "资料", "folder", "", true, 1700000000),
				dingTalkDentry("uuid-pdf", "dentry-pdf", "合同.pdf", "file", "pdf", false, 1700000001),
				dingTalkDentry("uuid-exe", "dentry-exe", "工具.exe", "file", "exe", false, 1700000002),
				dingTalkDentry("uuid-draft", "dentry-draft", "季度 draft.pdf", "file", "pdf", false, 1700000003),
				docWithChild,
			}})
		case "dentry-doc":
			dingTalkWriteJSON(w, map[string]any{"children": []any{
				dingTalkDentry("uuid-nested", "dentry-nested", "nested.pdf", "file", "pdf", false, 1700000005),
			}})
		case "dentry-folder":
			dingTalkWriteJSON(w, map[string]any{"children": []any{}})
		default:
			t.Errorf("unexpected parent %q", r.URL.Query().Get("dentryId"))
			dingTalkWriteJSON(w, map[string]any{"children": []any{}})
		}
	})
	connector := newDingTalkWikiTestConnector(t, server.server, map[string]any{
		"batch_size":       2,
		"exclude_keywords": []any{"draft"},
	})

	session, err := connector.OpenPrune(context.Background(), PruneRequest{})
	if err != nil {
		t.Fatalf("OpenPrune: %v", err)
	}
	defer session.Close()

	got, batches := dingTalkPruneSourceIDs(t, session)
	want := []string{
		"dingtalk_wiki:space-1:uuid-doc",
		"dingtalk_wiki:space-1:uuid-nested",
		"dingtalk_wiki:space-1:uuid-pdf",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("prune source ids = %v, want %v", got, want)
	}
	if batches != 2 {
		t.Fatalf("prune batches = %d, want 2", batches)
	}
	if contentCalls != 0 {
		t.Fatalf("prune read document content %d times, want 0", contentCalls)
	}
}

func TestDingTalkDrivePruneSnapshot(t *testing.T) {
	var contentCalls int
	server := newDingTalkWikiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2.0/doc/spaces/29999417290/directories":
			dingTalkWriteJSON(w, map[string]any{"children": []any{
				dingTalkDentry("uuid-pdf", "dentry-pdf", "合同.pdf", "file", "pdf", false, 1700000000),
				dingTalkDocDentry("uuid-doc", "dentry-doc", "会议纪要.adoc", "adoc", 1700000001),
				dingTalkDocDentry("uuid-sheet", "dentry-sheet", "数据.axls", "axls", 1700000002),
			}})
		case strings.Contains(r.URL.Path, "/downloadInfos/") || strings.HasSuffix(r.URL.Path, "/blocks"):
			contentCalls++
			http.Error(w, "prune must not read document content", http.StatusInternalServerError)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	connector, err := NewDingTalkDriveConnector(map[string]any{
		"credentials": map[string]any{"app_key": "key", "app_secret": "secret"},
		"operator_id": "union-1",
		"space_id":    "29999417290",
	})
	if err != nil {
		t.Fatalf("NewDingTalkDriveConnector: %v", err)
	}
	connector.baseURL = server.server.URL
	connector.httpClient = server.server.Client()

	session, err := connector.OpenPrune(context.Background(), PruneRequest{})
	if err != nil {
		t.Fatalf("OpenPrune: %v", err)
	}
	defer session.Close()

	got, _ := dingTalkPruneSourceIDs(t, session)
	want := []string{
		"dingtalk_drive:29999417290:uuid-doc",
		"dingtalk_drive:29999417290:uuid-sheet",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("prune source ids = %v, want %v", got, want)
	}
	if contentCalls != 0 {
		t.Fatalf("prune read document content %d times, want 0", contentCalls)
	}
}

func TestDingTalkWikiPruneFailsOnPartialListing(t *testing.T) {
	server := newDingTalkWikiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2.0/doc/spaces/space-1/directories" {
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("dentryId") == "dentry-doc" {
			w.WriteHeader(http.StatusForbidden)
			dingTalkWriteJSON(w, map[string]any{"code": "Forbidden.AccessDenied", "message": "no permission"})
			return
		}
		docWithChild := dingTalkDocDentry("uuid-doc", "dentry-doc", "会议纪要.adoc", "adoc", 1700000001)
		docWithChild["hasChildren"] = true
		dingTalkWriteJSON(w, map[string]any{"children": []any{docWithChild}})
	})
	connector := newDingTalkWikiTestConnector(t, server.server, nil)

	if _, err := connector.OpenPrune(context.Background(), PruneRequest{}); err == nil {
		t.Fatalf("OpenPrune succeeded on an incomplete snapshot")
	}
}
