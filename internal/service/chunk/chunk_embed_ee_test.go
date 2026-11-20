//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

//go:build ee

package chunk

import (
	"context"
	"errors"
	"strings"
	"testing"

	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/service"
)

// stubChunkEmbedder records what the chunk service asked to embed and answers
// with a fixed vector (or a fixed error).
type stubChunkEmbedder struct {
	calls  []ChunkEmbedRequest
	field  string
	vector []float64
	err    error
}

// installStubChunkEmbedder swaps the production embedder for one test.
func installStubChunkEmbedder(t *testing.T, stub *stubChunkEmbedder) {
	t.Helper()
	previous := chunkEmbedderEE
	t.Cleanup(func() { chunkEmbedderEE = previous })
	SetChunkEmbedderEE(func(_ context.Context, req ChunkEmbedRequest) (ChunkEmbedding, error) {
		stub.calls = append(stub.calls, req)
		if stub.err != nil {
			return ChunkEmbedding{}, stub.err
		}
		return ChunkEmbedding{Field: stub.field, Vector: stub.vector}, nil
	})
}

// setChunkTestParserConfig writes a versioned parser config carrying a
// filename_embd_weight under the scoped tokenizer node.
func setChunkTestParserConfig(t *testing.T, kbID string, weight float64) {
	t.Helper()
	cfg := entity.JSONMap{
		"Tokenizer:TestTokens": map[string]interface{}{"filename_embd_weight": weight},
	}
	if err := dao.DB.Model(&entity.Knowledgebase{}).Where("id = ?", kbID).Update("parser_config", cfg).Error; err != nil {
		t.Fatalf("update parser config: %v", err)
	}
}

// TestUpdateChunkReembedsChangedContent pins the contract of an edit that
// changes the text: the stored vector describes the old content, so the service
// rebuilds it with the document title and the knowledgebase's filename weight,
// and writes it to q_<dim>_vec together with the new text and its tokens.
func TestUpdateChunkReembedsChangedContent(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")
	setChunkTestParserConfig(t, "kb-1", 0.25)

	engine := &updateChunkTestEngine{
		existingChunk: map[string]interface{}{
			"doc_id":              "doc-a",
			"content_with_weight": "old body",
			"available_int":       1,
		},
	}
	svc := chunkVersionTestService(engine)
	stub := &stubChunkEmbedder{field: "q_3_vec", vector: []float64{0.5, 0.25, 0.125}}
	installStubChunkEmbedder(t, stub)

	body := "new body"
	if err := svc.UpdateChunk(chunkTestContext(t, "user-1", "tenant-1"), &service.UpdateChunkRequest{
		DatasetID:  "kb-1",
		DocumentID: "doc-a",
		ChunkID:    "chunk-1",
		Content:    &body,
	}); err != nil {
		t.Fatalf("UpdateChunk: %v", err)
	}

	if len(stub.calls) != 1 {
		t.Fatalf("embed calls = %d, want 1", len(stub.calls))
	}
	if stub.calls[0].Text != "new body" {
		t.Fatalf("embedded text = %q, want new body", stub.calls[0].Text)
	}
	if stub.calls[0].Name != "doc-a.txt" {
		t.Fatalf("embedded title = %q, want doc-a.txt", stub.calls[0].Name)
	}
	if stub.calls[0].Weight != 0.25 {
		t.Fatalf("filename weight = %v, want 0.25", stub.calls[0].Weight)
	}

	// Text, tokens and vector land in one write: the chunk must never exist in
	// the state "new body, old vector".
	if len(engine.updateCalls) != 1 {
		t.Fatalf("engine writes = %d, want 1 (text and vector together)", len(engine.updateCalls))
	}
	write := engine.updateCalls[0]
	if got := write.newValue["content_with_weight"]; got != "new body" {
		t.Fatalf("write content = %v, want new body", got)
	}
	vector, ok := write.newValue["q_3_vec"].([]float64)
	if !ok || len(vector) != 3 || vector[0] != 0.5 {
		t.Fatalf("write vector = %#v, want q_3_vec [0.5 0.25 0.125]", write.newValue)
	}

}

// TestUpdateChunkFailsWhenEmbeddingFails pins the failure semantics: the vector
// is computed before anything is written, so a provider failure refuses the
// whole edit. The text is not stored, no version row opens, and the chunk keeps
// answering by the content it already had.
func TestUpdateChunkFailsWhenEmbeddingFails(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	engine := &updateChunkTestEngine{
		existingChunk: map[string]interface{}{
			"doc_id":              "doc-a",
			"content_with_weight": "old body",
			"available_int":       1,
		},
	}
	svc := chunkVersionTestService(engine)
	stub := &stubChunkEmbedder{err: errors.New("provider timeout")}
	installStubChunkEmbedder(t, stub)

	body := "new body"
	err := svc.UpdateChunk(chunkTestContext(t, "user-1", "tenant-1"), &service.UpdateChunkRequest{
		DatasetID:  "kb-1",
		DocumentID: "doc-a",
		ChunkID:    "chunk-1",
		Content:    &body,
	})
	if err == nil {
		t.Fatal("a failed embedding must fail the edit")
	}
	if !strings.Contains(err.Error(), "provider timeout") {
		t.Fatalf("error = %q, want it to carry the cause", err)
	}

	if len(engine.updateCalls) != 0 {
		t.Fatalf("engine writes = %d, want 0 (nothing is stored)", len(engine.updateCalls))
	}
	if _, err := dao.NewChunkVersionDAO().GetLatest(t.Context(), dao.DB, "chunk-1"); !errors.Is(err, dao.ErrChunkVersionNotFound) {
		t.Fatalf("version rows = %v, want none", err)
	}
}

// TestUpdateChunkSkipsReembedWithoutContentChange keeps the embedding cost off
// edits that cannot invalidate the vector (availability, keywords, questions).
func TestUpdateChunkSkipsReembedWithoutContentChange(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	engine := &updateChunkTestEngine{
		existingChunk: map[string]interface{}{
			"doc_id":              "doc-a",
			"content_with_weight": "same body",
			"available_int":       1,
		},
	}
	svc := chunkVersionTestService(engine)
	stub := &stubChunkEmbedder{field: "q_3_vec", vector: []float64{1, 0, 0}}
	installStubChunkEmbedder(t, stub)

	available := false
	if err := svc.UpdateChunk(chunkTestContext(t, "user-1", "tenant-1"), &service.UpdateChunkRequest{
		DatasetID:  "kb-1",
		DocumentID: "doc-a",
		ChunkID:    "chunk-1",
		Available:  &available,
	}); err != nil {
		t.Fatalf("UpdateChunk: %v", err)
	}

	if len(stub.calls) != 0 {
		t.Fatalf("embed calls = %d, want 0 for a non-content edit", len(stub.calls))
	}
	if len(engine.updateCalls) != 1 {
		t.Fatalf("engine writes = %d, want 1", len(engine.updateCalls))
	}
}

// TestRestoreVersionRestoresTheStoredBodyWithoutEmbedding covers the other way
// content changes: putting an old version back is not an edit, so it never asks
// the model for a vector. The stored row - text, tokens and vector - is written
// back as it was recorded.
func TestRestoreVersionRestoresTheStoredBodyWithoutEmbedding(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	engine := &updateChunkTestEngine{
		existingChunk: map[string]interface{}{
			"doc_id":              "doc-a",
			"content_with_weight": "v1 body",
			"available_int":       1,
		},
	}
	svc := chunkVersionTestService(engine)

	versionDAO := dao.NewChunkVersionDAO()
	for _, row := range []*entity.ChunkVersion{
		{ChunkID: "chunk-1", Version: 0, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1, Snapshot: fullTestSnapshot(entity.JSONMap{
			"content_with_weight": "v0 body",
			"available_int":       1,
		})},
		{ChunkID: "chunk-1", Version: 1, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1, Snapshot: fullTestSnapshot(entity.JSONMap{
			"content_with_weight": "v1 body",
			"available_int":       1,
		})},
	} {
		if err := versionDAO.Create(t.Context(), dao.DB, row); err != nil {
			t.Fatalf("seed version %d: %v", row.Version, err)
		}
	}

	stub := &stubChunkEmbedder{field: "q_3_vec", vector: []float64{0, 1, 0}}
	installStubChunkEmbedder(t, stub)

	if _, err := svc.RestoreVersion(chunkVersionManageContext(t, "tenant-1", "user-1", "doc-a"), &service.ChunkVersionRequest{
		DatasetID:  "kb-1",
		DocumentID: "doc-a",
		ChunkID:    "chunk-1",
		Version:    0,
	}); err != nil {
		t.Fatalf("RestoreVersion: %v", err)
	}

	if len(stub.calls) != 0 {
		t.Fatalf("embed calls = %#v, want none: a restore is not an edit", stub.calls)
	}
	if len(engine.updateCalls) != 1 {
		t.Fatalf("engine writes = %d, want 1", len(engine.updateCalls))
	}
	restoreWrite := engine.updateCalls[0]
	if body, _ := restoreWrite.newValue["content_with_weight"].(string); body != "v0 body" {
		t.Fatalf("restored content = %q, want the stored body", body)
	}
	vector, ok := restoreWrite.newValue["q_3_vec"].([]float64)
	if !ok || len(vector) != 3 || vector[0] != 1 || vector[1] != 2 || vector[2] != 3 {
		t.Fatalf("restored vector = %#v, want the stored one", restoreWrite.newValue["q_3_vec"])
	}
	// The version being restored carries no coordinates. They describe the source
	// region the chunk was parsed from, not the text being rewound, so the write
	// leaves the live ones alone.
	if value, ok := restoreWrite.newValue["position_int"]; ok {
		t.Fatalf("restore position_int = %#v, want the stored coordinates left alone", value)
	}
}

// TestRestoreVersionIgnoresAnUnavailableEmbeddingModel pins the other half of
// that decision: the restore does not depend on the provider, so a model that is
// down cannot block it.
func TestRestoreVersionIgnoresAnUnavailableEmbeddingModel(t *testing.T) {
	setupChunkVersionTestDB(t)

	insertChunkTestUserTenant(t, "user-1", "tenant-1")
	insertChunkTestKB(t, "kb-1", "tenant-1")
	insertChunkTestDoc(t, "doc-a", "kb-1")

	engine := &updateChunkTestEngine{
		existingChunk: map[string]interface{}{
			"doc_id":              "doc-a",
			"content_with_weight": "v1 body",
			"available_int":       1,
		},
	}
	svc := chunkVersionTestService(engine)

	versionDAO := dao.NewChunkVersionDAO()
	for _, row := range []*entity.ChunkVersion{
		{ChunkID: "chunk-1", Version: 0, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1, Snapshot: fullTestSnapshot(entity.JSONMap{
			"content_with_weight": "v0 body",
		})},
		{ChunkID: "chunk-1", Version: 1, DocID: "doc-a", DatasetID: "kb-1", DatasetVersion: 1, Snapshot: fullTestSnapshot(entity.JSONMap{
			"content_with_weight": "v1 body",
		})},
	} {
		if err := versionDAO.Create(t.Context(), dao.DB, row); err != nil {
			t.Fatalf("seed version %d: %v", row.Version, err)
		}
	}

	installStubChunkEmbedder(t, &stubChunkEmbedder{err: errors.New("provider timeout")})

	if _, err := svc.RestoreVersion(chunkVersionManageContext(t, "tenant-1", "user-1", "doc-a"), &service.ChunkVersionRequest{
		DatasetID:  "kb-1",
		DocumentID: "doc-a",
		ChunkID:    "chunk-1",
		Version:    0,
	}); err != nil {
		t.Fatalf("RestoreVersion with the model down: %v", err)
	}

	if len(engine.updateCalls) != 1 {
		t.Fatalf("engine writes = %d, want the restored body", len(engine.updateCalls))
	}
	latest, err := versionDAO.GetLatest(t.Context(), dao.DB, "chunk-1")
	if err != nil {
		t.Fatalf("load latest version: %v", err)
	}
	if latest.Version != 2 {
		t.Fatalf("latest version = %d, want the restore recorded as v2", latest.Version)
	}
}

// TestFilenameEmbdWeightIsDeterministicWithSeveralTokenizerNodes pins that a
// config carrying one Tokenizer node per parser picks the same weight on every
// call: ranging the map would let two re-embeds of one chunk use different
// weights and produce vectors that cannot be reproduced.
func TestFilenameEmbdWeightIsDeterministicWithSeveralTokenizerNodes(t *testing.T) {
	cfg := entity.JSONMap{
		"Tokenizer:Beta":  map[string]interface{}{"filename_embd_weight": 0.4},
		"Tokenizer:Alpha": map[string]interface{}{"filename_embd_weight": 0.3},
	}
	for i := 0; i < 20; i++ {
		if got := filenameEmbdWeight(cfg); got != 0.3 {
			t.Fatalf("run %d: filenameEmbdWeight = %v, want 0.3 (the first node in key order)", i, got)
		}
	}
}

// TestNumericEmbedWeightAcceptsStringConfig covers a config whose weight was
// submitted as text: silently falling back to the default would embed with a
// weight the dataset never asked for.
func TestNumericEmbedWeightAcceptsStringConfig(t *testing.T) {
	cases := []struct {
		in   interface{}
		want float64
		ok   bool
	}{
		{"0.15", 0.15, true},
		{" 0.2 ", 0.2, true},
		{"not-a-number", 0, false},
		{nil, 0, false},
	}
	for _, tc := range cases {
		got, ok := numericEmbedWeight(tc.in)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Fatalf("numericEmbedWeight(%#v) = (%v, %v), want (%v, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// TestFilenameEmbdWeightReadsFlatAndScopedConfig keeps the weight lookup
// aligned with both parser-config shapes the database holds.
func TestFilenameEmbdWeightReadsFlatAndScopedConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  entity.JSONMap
		want float64
	}{
		{"scoped", entity.JSONMap{"Tokenizer:X": map[string]interface{}{"filename_embd_weight": 0.3}}, 0.3},
		{"flat", entity.JSONMap{"filename_embd_weight": 0.2}, 0.2},
		{"default", entity.JSONMap{"chunk_token_num": 128}, defaultFilenameEmbdWeight},
		{"nil", nil, defaultFilenameEmbdWeight},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := filenameEmbdWeight(tc.cfg); got != tc.want {
				t.Fatalf("filenameEmbdWeight(%#v) = %v, want %v", tc.cfg, got, tc.want)
			}
		})
	}
}
