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
	"strings"
)

const (
	feishuDocxBlocksPageSize = 500
	feishuDocxMaxDepth       = 32
)

// feishuDocxBlockKeys lists the block payload fields the renderer recognises.
// Native documents carry exactly one of them per block, so the renderer never
// needs the numeric block_type column.
var feishuDocxBlockKeys = []string{
	"page", "text",
	"heading1", "heading2", "heading3", "heading4", "heading5",
	"heading6", "heading7", "heading8", "heading9",
	"bullet", "ordered", "code", "quote", "quote_container", "todo", "divider",
	"table", "table_cell", "callout", "grid", "grid_column",
	"image", "file", "iframe", "sheet", "bitable", "mindnote",
}

// docxMarkdown renders one native document's block tree to Markdown.
func (c *FeishuWikiConnector) docxMarkdown(ctx context.Context, documentID string) ([]byte, error) {
	var blocks []map[string]any
	pageToken := ""
	for {
		items, hasMore, next, err := c.listDocxBlocks(ctx, documentID, pageToken)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, items...)
		if !hasMore {
			break
		}
		if next == "" {
			return nil, &ConnectorValidationError{Message: "Feishu document pagination indicated more data without a page token"}
		}
		pageToken = next
	}
	return []byte(feishuDocxRender(blocks)), nil
}

// listDocxBlocks lists one page of a document's blocks.
func (c *FeishuWikiConnector) listDocxBlocks(ctx context.Context, documentID, pageToken string) ([]map[string]any, bool, string, error) {
	endpoint := fmt.Sprintf("%s/open-apis/docx/v1/documents/%s/blocks?page_size=%d", c.baseURL, url.PathEscape(documentID), feishuDocxBlocksPageSize)
	if pageToken != "" {
		endpoint += "&page_token=" + url.QueryEscape(pageToken)
	}
	payload, err := c.feishuGet(ctx, endpoint, "list document blocks")
	if err != nil {
		return nil, false, "", err
	}
	data, _ := payload["data"].(map[string]any)
	if data == nil {
		return nil, false, "", &ConnectorValidationError{Message: "Feishu document block response contained invalid data"}
	}
	hasMore, _ := data["has_more"].(bool)
	return mapItems(data["items"]), hasMore, stringConfig(data["page_token"]), nil
}

// feishuDocxRender renders a flat block list into Markdown by walking the tree
// from its page block.
func feishuDocxRender(blocks []map[string]any) string {
	index := make(map[string]map[string]any, len(blocks))
	for _, block := range blocks {
		if id := stringConfig(block["block_id"]); id != "" {
			index[id] = block
		}
	}
	var out strings.Builder
	for _, block := range blocks {
		if kind, _ := feishuDocxKind(block); kind == "page" {
			feishuDocxAppendBlocks(&out, feishuDocxStrings(block["children"]), index, 0)
			return strings.TrimSpace(out.String())
		}
	}
	// A response without a page block is rendered from its orphan roots.
	for _, block := range blocks {
		parentID := stringConfig(block["parent_id"])
		if _, hasParent := index[parentID]; parentID != "" && hasParent {
			continue
		}
		feishuDocxAppendBlock(&out, block, index, 0, 0)
	}
	return strings.TrimSpace(out.String())
}

// feishuDocxKind returns the block's type name and that type's payload.
func feishuDocxKind(block map[string]any) (string, map[string]any) {
	for _, key := range feishuDocxBlockKeys {
		value, ok := block[key]
		if !ok {
			continue
		}
		object, _ := value.(map[string]any)
		return key, object
	}
	return "", nil
}

// feishuDocxAppendBlocks renders sibling blocks in order. The counter numbers
// consecutive ordered-list items.
func feishuDocxAppendBlocks(out *strings.Builder, ids []string, index map[string]map[string]any, depth int) {
	number := 0
	for _, id := range ids {
		block, ok := index[id]
		if !ok {
			continue
		}
		if kind, _ := feishuDocxKind(block); kind == "ordered" {
			number++
		} else {
			number = 0
		}
		feishuDocxAppendBlock(out, block, index, depth, number)
	}
}

// feishuDocxAppendBlock renders one block and its children.
func feishuDocxAppendBlock(out *strings.Builder, block map[string]any, index map[string]map[string]any, depth, number int) {
	if depth > feishuDocxMaxDepth {
		return
	}
	kind, object := feishuDocxKind(block)
	children := feishuDocxStrings(block["children"])
	indent := strings.Repeat("  ", depth)
	switch kind {
	case "text":
		feishuDocxAppendParagraph(out, feishuDocxInline(object["elements"]))
	case "heading1", "heading2", "heading3", "heading4", "heading5",
		"heading6", "heading7", "heading8", "heading9":
		level := int(kind[len("heading")] - '0')
		if level > 6 {
			level = 6
		}
		text := strings.TrimSpace(feishuDocxInline(object["elements"]))
		if text != "" {
			out.WriteString(strings.Repeat("#", level) + " " + text + "\n\n")
		}
	case "bullet", "ordered", "todo":
		marker := "- "
		switch kind {
		case "ordered":
			marker = fmt.Sprintf("%d. ", number)
		case "todo":
			if done, _ := object["done"].(bool); done {
				marker = "- [x] "
			} else {
				marker = "- [ ] "
			}
		}
		out.WriteString(indent + marker + strings.TrimSpace(feishuDocxInline(object["elements"])) + "\n")
		feishuDocxAppendBlocks(out, children, index, depth+1)
		if len(children) == 0 {
			out.WriteString("\n")
		}
	case "code":
		content := strings.TrimRight(feishuDocxPlainText(object["elements"]), "\n")
		fence := "```"
		if strings.Contains(content, fence) {
			fence = "````"
		}
		out.WriteString(indent + fence + "\n" + content + "\n" + indent + fence + "\n\n")
	case "quote", "quote_container":
		var inner strings.Builder
		if text := strings.TrimSpace(feishuDocxInline(object["elements"])); text != "" {
			inner.WriteString(text + "\n")
		}
		feishuDocxAppendBlocks(&inner, children, index, depth)
		feishuDocxAppendQuote(out, inner.String())
	case "divider":
		out.WriteString(indent + "---\n\n")
	case "table":
		out.WriteString(feishuDocxTable(block, index))
	case "callout", "grid", "grid_column":
		feishuDocxAppendBlocks(out, children, index, depth)
	}
	// page, table_cell (rendered by its table), image, file and unknown block
	// types contribute nothing on their own.
}

// feishuDocxAppendQuote prefixes every line of a block quote body.
func feishuDocxAppendQuote(out *strings.Builder, body string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" {
			out.WriteString(">\n")
			continue
		}
		out.WriteString("> " + line + "\n")
	}
	out.WriteString("\n")
}

func feishuDocxAppendParagraph(out *strings.Builder, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	out.WriteString(text + "\n\n")
}

// feishuDocxTable renders one table block as a GFM table whose header row is
// the table's first row.
func feishuDocxTable(block map[string]any, index map[string]map[string]any) string {
	table, _ := block["table"].(map[string]any)
	grid := feishuDocxTableGrid(table)
	rows := make([][]string, 0, len(grid))
	for _, row := range grid {
		values := make([]string, 0, len(row))
		for _, cellID := range row {
			values = append(values, feishuDocxTableCell(cellID, index))
		}
		rows = append(rows, values)
	}
	return markdownTable(rows)
}

// feishuDocxTableGrid returns the table's cell block ids as rows. Feishu sends
// one flat cell list plus the column count, so the list is reshaped by
// column_size; a nested list is accepted as-is.
func feishuDocxTableGrid(table map[string]any) [][]string {
	cells, _ := table["cells"].([]any)
	if len(cells) == 0 {
		return nil
	}
	if _, nested := cells[0].([]any); nested {
		grid := make([][]string, 0, len(cells))
		for _, row := range cells {
			rowCells, _ := row.([]any)
			ids := make([]string, 0, len(rowCells))
			for _, cellID := range rowCells {
				ids = append(ids, stringConfig(cellID))
			}
			grid = append(grid, ids)
		}
		return grid
	}
	property, _ := table["property"].(map[string]any)
	columns := feishuIntDefault(property["column_size"], 0)
	if columns <= 0 {
		columns = len(cells)
	}
	grid := make([][]string, 0, len(cells)/columns+1)
	for start := 0; start < len(cells); start += columns {
		end := start + columns
		if end > len(cells) {
			end = len(cells)
		}
		row := make([]string, 0, columns)
		for _, cellID := range cells[start:end] {
			row = append(row, stringConfig(cellID))
		}
		grid = append(grid, row)
	}
	return grid
}

// feishuDocxTableCell flattens one table cell's blocks into a single table cell.
func feishuDocxTableCell(cellID string, index map[string]map[string]any) string {
	cell, ok := index[cellID]
	if !ok {
		return ""
	}
	parts := make([]string, 0, 2)
	for _, childID := range feishuDocxStrings(cell["children"]) {
		child, ok := index[childID]
		if !ok {
			continue
		}
		_, object := feishuDocxKind(child)
		if text := strings.TrimSpace(feishuDocxPlainText(object["elements"])); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "<br>")
}

// feishuDocxInline renders a block's inline elements with their markdown markup.
func feishuDocxInline(elements any) string {
	var out strings.Builder
	for _, element := range mapItems(elements) {
		if run, ok := element["text_run"].(map[string]any); ok {
			out.WriteString(feishuDocxStyledRun(feishuDocxContent(run["content"]), run["text_element_style"]))
			continue
		}
		if equation, ok := element["equation"].(map[string]any); ok {
			if expression := stringConfig(equation["expression"]); expression != "" {
				out.WriteString("$" + expression + "$")
			}
			continue
		}
		if mention, ok := element["mention_doc"].(map[string]any); ok {
			out.WriteString(stringConfig(mention["title"]))
		}
	}
	return out.String()
}

// feishuDocxPlainText renders inline elements without any markdown markup.
func feishuDocxPlainText(elements any) string {
	var out strings.Builder
	for _, element := range mapItems(elements) {
		if run, ok := element["text_run"].(map[string]any); ok {
			out.WriteString(feishuDocxContent(run["content"]))
			continue
		}
		if equation, ok := element["equation"].(map[string]any); ok {
			out.WriteString(stringConfig(equation["expression"]))
			continue
		}
		if mention, ok := element["mention_doc"].(map[string]any); ok {
			out.WriteString(stringConfig(mention["title"]))
		}
	}
	return out.String()
}

// feishuDocxStyledRun wraps one text run in the markup its style asks for. The
// run's own boundary whitespace stays outside the markup: a styled space run is
// how a word processor separates two words, and `** **` would render literally.
func feishuDocxStyledRun(content string, styleValue any) string {
	core := strings.TrimSpace(content)
	if core == "" {
		return content
	}
	leading := content[:len(content)-len(strings.TrimLeft(content, " \t\r\n"))]
	trailing := content[len(strings.TrimRight(content, " \t\r\n")):]
	style, _ := styleValue.(map[string]any)
	text := core
	if inlineCode, _ := style["inline_code"].(bool); inlineCode {
		text = "`" + text + "`"
	} else {
		if bold, _ := style["bold"].(bool); bold {
			text = "**" + text + "**"
		}
		if italic, _ := style["italic"].(bool); italic {
			text = "*" + text + "*"
		}
		if strikethrough, _ := style["strikethrough"].(bool); strikethrough {
			text = "~~" + text + "~~"
		}
	}
	link, _ := style["link"].(map[string]any)
	if url := strings.TrimSpace(stringConfig(link["url"])); url != "" {
		text = "[" + text + "](" + url + ")"
	}
	return leading + text + trailing
}

// feishuDocxStrings flattens an id list, preserving case: block ids are matched
// against the raw block_id index.
func feishuDocxStrings(value any) []string {
	raw, _ := value.([]any)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		text, _ := item.(string)
		if text = strings.TrimSpace(text); text != "" {
			out = append(out, text)
		}
	}
	return out
}

// feishuDocxContent returns a text run's content unchanged: trimming it would
// join words that are separated only by a styled space.
func feishuDocxContent(value any) string {
	content, _ := value.(string)
	return content
}
