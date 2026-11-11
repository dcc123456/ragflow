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
	"fmt"
	"path/filepath"
	"strings"

	cad "github.com/unitedrhino/go-cad"
)

// CADParser extracts text from CAD drawings (DWG/DXF/DXFB). It implements
// ParseResultProducer so the standard ingestion pipeline can consume it once
// the CAD file type is registered via RegisterParserFactory.
type CADParser struct {
	textOnly bool
}

// NewCADParser builds a CADParser. textOnly is retained for a future visual
// rendering path (sheet PNG); v1 extracts vector text only.
func NewCADParser(textOnly bool) *CADParser {
	return &CADParser{textOnly: textOnly}
}

// String identifies the parser in logs and diagnostics.
func (p *CADParser) String() string { return "CADParser" }

// ParseWithResult implements ParseResultProducer. DXF uses go-cad's text
// decoder, DWG the bit-level decoder; every extracted drawing text becomes a
// text DocItem.
func (p *CADParser) ParseWithResult(ctx context.Context, filename string, data []byte) ParseResult {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
	isDXF := ext == "dxf" || ext == "dxfb"
	texts, err := extractCADText(data, isDXF)
	if err != nil {
		return ParseResult{Err: err}
	}
	items := make([]DocItem, 0, len(texts))
	for _, t := range texts {
		items = append(items, NewTextJSONItem(t.Text))
	}
	return ParseResult{
		File: map[string]any{
			"name":   filename,
			"size":   len(data),
			"format": "cad",
		},
		JSON: items,
	}
}

// cadText mirrors go-cad's drawing.TextInfo: extracted drawing text plus its
// world coordinates and layer handle.
type cadText struct {
	Text  string
	Layer uint64
	X, Y  float64
}

// extractCADText parses raw DWG/DXF bytes and extracts all drawing text (TEXT,
// MTEXT, ATTRIB and block-inserted attributes) with world coordinates and
// layer. It is a thin wrapper over github.com/unitedrhino/go-cad.
func extractCADText(data []byte, isDXF bool) ([]cadText, error) {
	var (
		doc *cad.Document
		err error
	)
	if isDXF {
		doc, err = cad.ParseDXF(data)
	} else {
		doc, err = cad.Parse(data)
	}
	if err != nil {
		return nil, fmt.Errorf("cad: parse: %w", err)
	}
	infos := doc.Texts()
	out := make([]cadText, 0, len(infos))
	for _, t := range infos {
		out = append(out, cadText{Text: t.Text, Layer: t.Layer, X: t.X, Y: t.Y})
	}
	return out, nil
}
