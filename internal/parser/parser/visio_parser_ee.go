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

package parser

import (
	"bytes"
	"context"
	"fmt"
	"strings"
)

var (
	maxVisioInputBytes = 100 << 20
	maxVisioPages      = 200
)

// visioParser selects the decoder and produces one text item per non-empty foreground page.
type visioParser struct{}

func (visioParser) ParseWithResult(ctx context.Context, filename string, data []byte) ParseResult {
	if err := ctx.Err(); err != nil {
		return ParseResult{Err: err}
	}
	if len(data) == 0 {
		return ParseResult{Err: fmt.Errorf("visio: empty file %q", filename)}
	}
	if len(data) > maxVisioInputBytes {
		return ParseResult{Err: fmt.Errorf("visio: file size %d exceeds %d byte limit", len(data), maxVisioInputBytes)}
	}

	// Both extensions share a parser family. The signature selects the
	// decoder, which then validates the Visio-specific document structures.
	var format string
	var decode func(context.Context, []byte) ([]visioPage, error)
	switch {
	case bytes.HasPrefix(data, CompoundDocumentSignature):
		format, decode = "vsd", parseVSD
	case bytes.HasPrefix(data, []byte("PK\x03\x04")):
		format, decode = "vsdx", parseVSDX
	default:
		return ParseResult{Err: fmt.Errorf("visio: unrecognized container in %q", filename)}
	}
	pages, err := decode(ctx, data)
	if err != nil {
		return ParseResult{Err: fmt.Errorf("%s: %w", format, err)}
	}
	return visioParseResult(filename, format, pages)
}

type visioPage struct {
	VisioID     int
	DisplayNum  int
	Name        string
	Connections []visioConnection
	ExtraTexts  []string
}

type visioConnection struct {
	FromText string
	ToText   string
	Label    string
	Dir      visioDirection
}

type visioDirection int

const (
	visioDirUndirected visioDirection = iota
	visioDirForward
	visioDirBackward
	visioDirBidirectional
)

func visioParseResult(filename, format string, pages []visioPage) ParseResult {
	items := make([]DocItem, 0, len(pages))
	for _, page := range pages {
		text := formatVisioPageText(page)
		if text == "" {
			continue
		}
		items = append(items, TextItem{
			Text: text,
			Extra: map[string]any{
				"positions": [][]float64{{float64(page.DisplayNum), float64(page.VisioID), 0, 0, 0}},
			},
		})
	}

	return ParseResult{
		File: map[string]any{
			"name":       filename,
			"format":     format,
			"page_count": len(pages),
		},
		JSON: items,
	}
}

func formatVisioPageText(page visioPage) string {
	if len(page.Connections) == 0 && len(page.ExtraTexts) == 0 {
		return ""
	}
	var b strings.Builder
	if page.Name != "" {
		fmt.Fprintf(&b, "[Page %d: %s]", page.DisplayNum, page.Name)
	} else {
		fmt.Fprintf(&b, "[Page %d]", page.DisplayNum)
	}

	if len(page.Connections) > 0 {
		b.WriteString("\nConnections:")
		for _, conn := range page.Connections {
			b.WriteString("\n- ")
			b.WriteString(formatVisioConnectionSentence(conn))
		}
	}
	if len(page.ExtraTexts) > 0 {
		b.WriteString("\nOther shapes and notes:")
		for _, t := range page.ExtraTexts {
			b.WriteString("\n- ")
			b.WriteString(t)
		}
	}
	return b.String()
}

func formatVisioConnectionSentence(c visioConnection) string {
	var base string
	switch c.Dir {
	case visioDirForward, visioDirBackward:
		base = fmt.Sprintf("%s points to %s", c.FromText, c.ToText)
	case visioDirBidirectional:
		base = fmt.Sprintf("%s is bidirectionally connected to %s", c.FromText, c.ToText)
	default:
		base = fmt.Sprintf("%s is connected to %s", c.FromText, c.ToText)
	}
	if c.Label != "" {
		return base + fmt.Sprintf(", labeled %q.", c.Label)
	}
	return base + "."
}
