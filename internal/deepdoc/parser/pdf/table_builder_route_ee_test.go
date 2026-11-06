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
	"image"
	"testing"

	"ragflow/internal/deepdoc/client"
	"ragflow/internal/deepdoc/parser/pdf/table"
	pdf "ragflow/internal/deepdoc/parser/pdf/type"
)

// docStub is a minimal pdf.DocAnalyzer that is intentionally NOT a
// *client.Client, used to assert the non-EE fallback stays on the OSS
// builder.
type docStub struct{}

func (docStub) DLA(_ context.Context, _ image.Image) ([]pdf.DLARegion, error) {
	return nil, nil
}
func (docStub) TSR(_ context.Context, _ image.Image) ([]pdf.TSRCell, error) {
	return nil, nil
}
func (docStub) OCRDetect(_ context.Context, _ image.Image) ([]pdf.OCRBox, error) {
	return nil, nil
}
func (docStub) OCRRecognize(_ context.Context, _ image.Image) ([]pdf.OCRText, error) {
	return nil, nil
}
func (docStub) Health() bool { return false }

// TestNewTableBuilderForVH verifies that the EE routing closure routes a
// *client.Client (deepdoc_cpu_ee) to the VH builder, while other analyzers
// keep the OSS DeepDocTableBuilder. The route init() installs this same
// closure via RegisterTableBuilder; we re-install it here (with save/restore)
// so the assertion is isolated from other tests that also call
// RegisterTableBuilder.
func TestNewTableBuilderForVH(t *testing.T) {
	prev := tableBuilderFactory
	defer func() { tableBuilderFactory = prev }()
	RegisterTableBuilder(func(doc pdf.DocAnalyzer) pdf.TableBuilder {
		if c, ok := doc.(*client.Client); ok {
			return table.NewDeepDocVHTableBuilder(c)
		}
		return table.NewDeepDocTableBuilder(doc)
	})

	ee := client.New("http://example.invalid")
	if tb, ok := NewTableBuilderFor(ee).(*table.DeepDocVHTableBuilder); !ok {
		t.Fatalf("expected *table.DeepDocVHTableBuilder for *client.Client, got %T", tb)
	}
	if tb, ok := NewTableBuilderFor(docStub{}).(*table.DeepDocTableBuilder); !ok {
		t.Fatalf("expected *table.DeepDocTableBuilder for non-client doc, got %T", tb)
	}
}
