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
	"net/url"
	"strings"
	"testing"
	"time"
)

func timeUnix(seconds int64) time.Time {
	return time.Unix(seconds, 0).UTC()
}

// dingTalkAllowLoopbackResources lets a signed-URL download reach the httptest
// server, whose address the SSRF guard rejects as non-public.
func dingTalkAllowLoopbackResources(t *testing.T) {
	t.Helper()
	origAssert := connectorAssertURLSafe
	connectorAssertURLSafe = func(rawURL string) (string, string, error) {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return "", "", err
		}
		return parsed.Hostname(), "127.0.0.1", nil
	}
	t.Cleanup(func() { connectorAssertURLSafe = origAssert })
}

func TestDingTalkWikiAttachmentDownload(t *testing.T) {
	dingTalkAllowLoopbackResources(t)

	var server *dingTalkWikiTestServer
	server = newDingTalkWikiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2.0/doc/spaces/space-1/directories":
			dingTalkWriteJSON(w, map[string]any{"children": []any{
				dingTalkDentry("uuid-1", "dentry-1", "handbook.pdf", "file", "pdf", false, 1700000000),
			}})
		case r.URL.Path == "/v1.0/storage/spaces/space-1/dentries/dentry-1/downloadInfos/query":
			if r.Method != http.MethodPost {
				t.Errorf("download info method = %s, want POST", r.Method)
			}
			if got := r.URL.Query().Get("unionId"); got != "union-1" {
				t.Errorf("unionId = %q, want union-1", got)
			}
			if got := r.URL.Query().Get("operatorId"); got != "" {
				t.Errorf("operatorId = %q, want it unset", got)
			}
			dingTalkWriteJSON(w, map[string]any{
				"headerSignatureInfo": map[string]any{
					"resourceUrls": []any{server.server.URL + "/cdn/handbook.pdf"},
					"headers":      map[string]any{"x-signature": "signed"},
				},
			})
		case r.URL.Path == "/cdn/handbook.pdf":
			// The signed URL points back at this server; the SSRF guard needs a
			// public host, so the test hook allows the loopback address.
			if got := r.Header.Get("x-signature"); got != "signed" {
				t.Errorf("signature header = %q, want signed", got)
			}
			_, _ = w.Write([]byte("%PDF-1.4 handbook"))
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
	syncSession := session.(*dingTalkWikiSyncSession)

	batch, err := syncSession.NextBatch(context.Background())
	if err != nil {
		t.Fatalf("NextBatch: %v", err)
	}
	if len(batch.Documents) != 1 {
		t.Fatalf("batch = %d documents, want 1", len(batch.Documents))
	}
	document := batch.Documents[0]
	if string(document.Blob) != "%PDF-1.4 handbook" {
		t.Fatalf("blob = %q", document.Blob)
	}
	if document.Extension != ".pdf" {
		t.Fatalf("extension = %q", document.Extension)
	}
	sum := sha256.Sum256(document.Blob)
	if document.Fingerprint != hex.EncodeToString(sum[:])[:32] {
		t.Fatalf("fingerprint = %q, want the content hash", document.Fingerprint)
	}
}

func TestDingTalkWikiAttachmentSizeLimit(t *testing.T) {
	dingTalkAllowLoopbackResources(t)

	var server *dingTalkWikiTestServer
	server = newDingTalkWikiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2.0/doc/spaces/space-1/directories":
			dingTalkWriteJSON(w, map[string]any{"children": []any{
				dingTalkDentry("uuid-1", "dentry-1", "big.pdf", "file", "pdf", false, 1700000000),
			}})
		case r.URL.Path == "/v1.0/storage/spaces/space-1/dentries/dentry-1/downloadInfos/query":
			dingTalkWriteJSON(w, map[string]any{
				"headerSignatureInfo": map[string]any{
					"resourceUrls": []any{server.server.URL + "/cdn/big.pdf"},
				},
			})
		case r.URL.Path == "/cdn/big.pdf":
			_, _ = w.Write([]byte("0123456789"))
		default:
			http.NotFound(w, r)
		}
	})
	connector := newDingTalkWikiTestConnector(t, server.server, map[string]any{"max_file_size_bytes": 4})

	session, err := connector.OpenSync(context.Background(), SyncRequest{})
	if err != nil {
		t.Fatalf("OpenSync: %v", err)
	}
	syncSession := session.(*dingTalkWikiSyncSession)

	_, err = syncSession.NextBatch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "exceeds the configured") {
		t.Fatalf("NextBatch error = %v, want a size limit error", err)
	}
}
