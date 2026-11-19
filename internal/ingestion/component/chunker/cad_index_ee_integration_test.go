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

//go:build ee && integration

package chunker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"ragflow/internal/engine/elasticsearch"
	"ragflow/internal/server/config"
)

// TestCADExtraUsesExistingTextMapping exercises the actual ES writer against
// an index created before CAD ingestion. Dynamic mapping on a new index would
// accept the wrong object shape and cannot protect this contract.
func TestCADExtraUsesExistingTextMapping(t *testing.T) {
	if os.Getenv("ES_TEST") != "1" {
		t.Skip("set ES_TEST=1 for real Elasticsearch")
	}
	cfg := config.ElasticsearchConfig{Hosts: os.Getenv("ES_HOST"), Username: os.Getenv("ES_USERNAME"), Password: os.Getenv("ES_PASSWORD")}
	if cfg.Hosts == "" {
		t.Fatal("ES_HOST is required")
	}
	index := fmt.Sprintf("ragflow_cad_extra_contract_%d", time.Now().UnixNano())
	request := func(method, path string, body []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, strings.TrimRight(cfg.Hosts, "/")+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.SetBasicAuth(cfg.Username, cfg.Password)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
		if res.StatusCode >= 300 {
			t.Fatalf("ES %s %s status=%d: %s", method, path, res.StatusCode, data)
		}
	}
	request("PUT", "/"+index, []byte(`{"settings":{"number_of_shards":1,"number_of_replicas":0},"mappings":{"properties":{"extra":{"type":"text"},"kb_id":{"type":"keyword"},"id":{"type":"keyword"}}}}`))
	defer request("DELETE", "/"+index, nil)
	engine, err := elasticsearch.NewEngine(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	chunk := cadTestChunkFromItems("drawing", cadTestSheet("s1", [4]float64{0, 0, 1000, 600}), "R1", cadKindRegion, []cadTextItem{{text: "FC 095", x: 10, y: 30, height: 5, sourceID: "source:1"}})
	wire := cadTestChunkDocs(t, []cadChunk{chunk})[0].ToMap()
	wire["id"] = "cadchunk"
	wire["doc_id"] = "caddoc"
	wire["content_with_weight"] = wire["text"]
	delete(wire, "text")
	if _, err := engine.InsertChunks(t.Context(), []map[string]any{wire}, index, "cadextra", ""); err != nil {
		t.Fatalf("CAD chunks must write to an existing extra:text mapping: %v", err)
	}
	value, err := engine.GetChunk(t.Context(), index, "cadchunk", []string{"cadextra"})
	if err != nil {
		t.Fatal(err)
	}
	stored, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("stored chunk type %T", value)
	}
	extra, ok := stored["extra"].(string)
	if !ok {
		t.Fatalf("stored extra type %T, want JSON string", stored["extra"])
	}
	var payload struct {
		CAD struct {
			Kind   string    `json:"kind"`
			Bounds []float64 `json:"bbox"`
		} `json:"cad"`
	}
	if err := json.Unmarshal([]byte(extra), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.CAD.Kind != "region" || len(payload.CAD.Bounds) != 4 {
		t.Fatalf("CAD provenance lost: %s", extra)
	}
}
