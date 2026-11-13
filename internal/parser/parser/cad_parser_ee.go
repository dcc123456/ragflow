//go:build ee

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

package parser

import (
	"context"

	"ragflow/internal/parser/cadsource"
)

// CADParser extracts the structured CAD document from DWG/DXF/DXFB bytes. It
// implements ParseResultProducer so the standard ingestion pipeline can consume
// it once the CAD file type is registered via RegisterParserFactory.
type CADParser struct{}

// NewCADParser builds a CADParser. The textOnly flag is part of the parser
// factory contract; CAD extraction always produces both the structured document
// and its text projection, so the flag does not change the result.
func NewCADParser(_ bool) *CADParser {
	return &CADParser{}
}

// String identifies the parser in logs and diagnostics.
func (p *CADParser) String() string { return "CADParser" }

// ParseWithResult implements ParseResultProducer. The structured CAD document
// travels on File["cad"] for the CAD chunker; JSON carries the flat text
// projection used by previews and by pipelines without a CAD chunker.
func (p *CADParser) ParseWithResult(ctx context.Context, filename string, data []byte) ParseResult {
	if err := ctx.Err(); err != nil {
		return ParseResult{Err: err}
	}
	doc, err := cadsource.Extract(ctx, filename, data)
	if err != nil {
		return ParseResult{Err: err}
	}
	return ParseResult{
		File: map[string]any{
			"name":   filename,
			"size":   len(data),
			"format": "cad",
			"cad":    doc,
		},
		JSON:     CADTextItems(doc),
		Warnings: doc.Diagnostics.Warnings,
	}
}
