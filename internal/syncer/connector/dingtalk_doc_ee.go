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
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	// dingTalkBlockPageSize is how many blocks one document read requests; the
	// API pages by block index instead of a cursor.
	dingTalkBlockPageSize = 200
	// dingTalkMaxBlocks caps one document so a broken page cannot loop forever.
	dingTalkMaxBlocks = 10000
)

// docMarkdown renders one native document's blocks to Markdown.
func (c *DingTalkWikiConnector) docMarkdown(ctx context.Context, node dingTalkWikiNode) ([]byte, error) {
	if node.docKey == "" {
		return nil, &ConnectorValidationError{Message: fmt.Sprintf("DingTalk document %q carries no doc key", node.title)}
	}
	blocks, err := c.listDocBlocks(ctx, node.docKey)
	if err != nil {
		return nil, err
	}
	markdown := strings.TrimSpace(dingTalkDocRender(blocks))
	if markdown == "" {
		markdown = strings.TrimSpace(node.title)
	}
	return []byte(markdown), nil
}

// listDocBlocks reads every block of one document, starting at a block index.
func (c *DingTalkWikiConnector) listDocBlocks(ctx context.Context, docKey string) ([]map[string]any, error) {
	var blocks []map[string]any
	path := fmt.Sprintf("/v1.0/doc/suites/documents/%s/blocks", url.PathEscape(docKey))
	for start := 0; ; {
		query := c.operatorQuery()
		query.Set("startIndex", strconv.Itoa(start))
		query.Set("endIndex", strconv.Itoa(start+dingTalkBlockPageSize-1))
		payload, err := c.dingTalkGet(ctx, path, query)
		if err != nil {
			return nil, err
		}
		result, _ := payload["result"].(map[string]any)
		page := mapItems(result["data"])
		blocks = append(blocks, page...)
		if len(page) < dingTalkBlockPageSize {
			return blocks, nil
		}
		if len(blocks) > dingTalkMaxBlocks {
			return nil, &ConnectorValidationError{Message: fmt.Sprintf("DingTalk document block listing exceeded %d blocks", dingTalkMaxBlocks)}
		}
		start += len(page)
	}
}

// dingTalkDocRender renders blocks in document order.
func dingTalkDocRender(blocks []map[string]any) string {
	var out strings.Builder
	for _, block := range blocks {
		dingTalkDocAppendBlock(&out, block)
	}
	return out.String()
}

// dingTalkDocAppendBlock appends one block's Markdown. Block types the API has
// not shown yet contribute nothing.
func dingTalkDocAppendBlock(out *strings.Builder, block map[string]any) {
	switch strings.ToLower(stringConfig(block["blockType"])) {
	case "paragraph":
		paragraph, _ := block["paragraph"].(map[string]any)
		text := strings.TrimSpace(dingTalkDocParagraphText(paragraph))
		if text != "" {
			out.WriteString(text + "\n\n")
		}
	case "heading":
		heading, _ := block["heading"].(map[string]any)
		text := strings.TrimSpace(stringConfig(heading["text"]))
		if text != "" {
			level := dingTalkDocHeadingLevel(stringConfig(heading["level"]))
			out.WriteString(strings.Repeat("#", level) + " " + text + "\n\n")
		}
	case "table":
		table, _ := block["table"].(map[string]any)
		out.WriteString(dingTalkDocTable(table))
	}
}

// dingTalkDocParagraphText returns a paragraph's text, falling back to the text
// runs of its elements. Image elements carry no text.
func dingTalkDocParagraphText(paragraph map[string]any) string {
	if text := stringConfig(paragraph["text"]); text != "" {
		return text
	}
	var out strings.Builder
	for _, element := range mapItems(paragraph["elements"]) {
		if strings.ToLower(stringConfig(element["elementType"])) == "image" {
			continue
		}
		out.WriteString(stringConfig(element["text"]))
	}
	return out.String()
}

// dingTalkDocHeadingLevel parses the API's "heading-N" level, clamped to 1..6.
func dingTalkDocHeadingLevel(level string) int {
	number, err := strconv.Atoi(strings.TrimPrefix(strings.ToLower(level), "heading-"))
	if err != nil || number < 1 {
		return 1
	}
	if number > 6 {
		return 6
	}
	return number
}

// dingTalkDocTable renders a table block: cells[row] holds that row's cells,
// and a cell is either the text itself or a list of text fragments.
func dingTalkDocTable(table map[string]any) string {
	rows := make([][]string, 0, 2)
	rawRows, _ := table["cells"].([]any)
	for _, rawRow := range rawRows {
		rawCells, ok := rawRow.([]any)
		if !ok {
			continue
		}
		cells := make([]string, 0, len(rawCells))
		for _, rawCell := range rawCells {
			cells = append(cells, dingTalkDocCell(rawCell))
		}
		rows = append(rows, cells)
	}
	return markdownTable(rows)
}

// dingTalkDocCell renders one cell. The API sends its text directly, with a list
// of fragments for a cell that holds several blocks.
func dingTalkDocCell(value any) string {
	fragments, ok := value.([]any)
	if !ok {
		return stringConfig(value)
	}
	texts := make([]string, 0, len(fragments))
	for _, fragment := range fragments {
		if text := stringConfig(fragment); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n")
}
