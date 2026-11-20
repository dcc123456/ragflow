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

// sheetMarkdown renders one workbook's worksheets to Markdown, one section per
// worksheet.
func (c *DingTalkWikiConnector) sheetMarkdown(ctx context.Context, node dingTalkWikiNode) ([]byte, error) {
	workbookID := node.dentryUUID
	sheets, err := c.listWorkbookSheets(ctx, workbookID)
	if err != nil {
		return nil, err
	}
	var out strings.Builder
	for _, sheet := range sheets {
		sheetID := stringConfig(sheet["id"])
		if sheetID == "" {
			continue
		}
		detail, err := c.getWorkbookSheet(ctx, workbookID, sheetID)
		if err != nil {
			return nil, err
		}
		rows := configInt(detail["rowCount"], sheetDefaultRows)
		columns := configInt(detail["columnCount"], sheetDefaultColumns)
		values, truncated, err := c.sheetValues(ctx, workbookID, sheetID, rows, columns)
		if err != nil {
			return nil, err
		}
		table := markdownTable(sheetTrim(sheetRows(values)))
		if table == "" {
			continue
		}
		title := firstNonEmpty(stringConfig(sheet["name"]), stringConfig(detail["name"]), sheetID)
		out.WriteString("## " + title + "\n\n" + table)
		if truncated {
			out.WriteString(fmt.Sprintf("_Truncated: only the first %d rows are synced._\n\n", sheetMaxRows))
		}
	}
	return []byte(strings.TrimSpace(out.String())), nil
}

// listWorkbookSheets lists one workbook's worksheets.
func (c *DingTalkWikiConnector) listWorkbookSheets(ctx context.Context, workbookID string) ([]map[string]any, error) {
	path := fmt.Sprintf("/v1.0/doc/workbooks/%s/sheets", url.PathEscape(workbookID))
	payload, err := c.dingTalkGet(ctx, path, c.operatorQuery())
	if err != nil {
		return nil, err
	}
	return mapItems(payload["value"]), nil
}

// getWorkbookSheet reads one worksheet's metadata, which carries its dimensions.
func (c *DingTalkWikiConnector) getWorkbookSheet(ctx context.Context, workbookID, sheetID string) (map[string]any, error) {
	path := fmt.Sprintf("/v1.0/doc/workbooks/%s/sheets/%s", url.PathEscape(workbookID), url.PathEscape(sheetID))
	return c.dingTalkGet(ctx, path, c.operatorQuery())
}

// sheetValues reads a worksheet's cells in row chunks bounded by the per-request
// cell limit, stopping at the sync row cap.
func (c *DingTalkWikiConnector) sheetValues(ctx context.Context, workbookID, sheetID string, rowCount, columnCount int) ([][]any, bool, error) {
	chunkRows := sheetCellsPerRequest / columnCount
	if chunkRows < 1 {
		chunkRows = 1
	}
	if chunkRows > sheetMaxChunkRows {
		chunkRows = sheetMaxChunkRows
	}
	lastRow := rowCount
	truncated := false
	if lastRow > sheetMaxRows {
		lastRow = sheetMaxRows
		truncated = true
	}
	lastColumn := sheetColumnLetter(columnCount)
	var values [][]any
	for start := 1; start <= lastRow; start += chunkRows {
		end := start + chunkRows - 1
		if end > lastRow {
			end = lastRow
		}
		rows, err := c.getWorkbookRange(ctx, workbookID, sheetID, fmt.Sprintf("A%d:%s%d", start, lastColumn, end))
		if err != nil {
			return nil, false, err
		}
		values = append(values, rows...)
	}
	return values, truncated, nil
}

// getWorkbookRange reads one cell range of a worksheet.
func (c *DingTalkWikiConnector) getWorkbookRange(ctx context.Context, workbookID, sheetID, rangeRef string) ([][]any, error) {
	path := fmt.Sprintf("/v1.0/doc/workbooks/%s/sheets/%s/ranges/%s",
		url.PathEscape(workbookID), url.PathEscape(sheetID), url.PathEscape(rangeRef))
	payload, err := c.dingTalkGet(ctx, path, c.operatorQuery())
	if err != nil {
		return nil, err
	}
	raw, _ := payload["values"].([]any)
	rows := make([][]any, 0, len(raw))
	for _, row := range raw {
		if cells, ok := row.([]any); ok {
			rows = append(rows, cells)
		}
	}
	return rows, nil
}
