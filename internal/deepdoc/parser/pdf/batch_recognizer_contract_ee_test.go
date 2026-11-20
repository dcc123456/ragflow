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

package pdf

import (
	"context"
	"testing"

	"ragflow/internal/deepdoc/client"
	doctype "ragflow/internal/deepdoc/parser/type"
)

// Compile-time contract: the HTTP analyzer must satisfy the parser's optional
// batch OCR capability. Without this assertion a signature drift in the client
// would silently degrade every remote-backed parse to per-crop recognition,
// with no test or compiler error to catch it.
var _ batchRecognizer = (*client.Client)(nil)

// TestHTTPClientSatisfiesBatchRecognizer pins the runtime half of the contract:
// a DocAnalyzer produced by the HTTP analyzer factory is detected as batch-
// capable by the parser's type assertion, and an empty batch is a no-op.
func TestHTTPClientSatisfiesBatchRecognizer(t *testing.T) {
	var doc doctype.DocAnalyzer = client.New("http://127.0.0.1:1")
	br, ok := doc.(batchRecognizer)
	if !ok {
		t.Fatal("HTTP DeepDoc client must implement batchRecognizer")
	}
	got, err := br.OCRRecognizeBatch(context.Background(), nil)
	if err != nil || got != nil {
		t.Fatalf("empty batch should be a no-op, got %v, err=%v", got, err)
	}
}
