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
	sheetCellsPerRequest = 5000
	sheetMaxRows         = 5000
	sheetDefaultRows     = 1000
	sheetDefaultColumns  = 26
	sheetMaxChunkRows    = 500
)

// sheetMarkdown renders one spreadsheet's visible worksheets to Markdown, one
// section per worksheet.
func (c *FeishuWikiConnector) sheetMarkdown(ctx context.Context, spreadsheetToken string) ([]byte, error) {
	sheets, err := c.listSheets(ctx, spreadsheetToken)
	if err != nil {
		return nil, err
	}
	var out strings.Builder
	for _, sheet := range sheets {
		if hidden, _ := sheet["hidden"].(bool); hidden {
			continue
		}
		sheetID := stringConfig(sheet["sheet_id"])
		if sheetID == "" {
			continue
		}
		title := firstNonEmpty(stringConfig(sheet["title"]), sheetID)
		if stringConfig(sheet["resource_type"]) == feishuWikiBitableObjectType {
			section, err := c.embeddedBitableMarkdown(ctx, spreadsheetToken, sheetID, title)
			if err != nil {
				return nil, err
			}
			out.WriteString(section)
			continue
		}
		grid, _ := sheet["grid_properties"].(map[string]any)
		rows := feishuIntDefault(grid["row_count"], sheetDefaultRows)
		if rows <= 0 {
			rows = sheetDefaultRows
		}
		columns := feishuIntDefault(grid["column_count"], sheetDefaultColumns)
		if columns <= 0 {
			columns = sheetDefaultColumns
		}
		values, truncated, err := c.sheetValues(ctx, spreadsheetToken, sheetID, rows, columns)
		if err != nil {
			return nil, err
		}
		table := markdownTable(sheetTrim(sheetRows(values)))
		if table == "" {
			continue
		}
		out.WriteString("## " + title + "\n\n" + table)
		if truncated {
			out.WriteString(fmt.Sprintf("_Truncated: only the first %d rows are synced._\n\n", sheetMaxRows))
		}
	}
	return []byte(strings.TrimSpace(out.String())), nil
}

// embeddedBitableMarkdown renders one worksheet whose resource_type is bitable:
// the worksheet embeds a data table of a Bitable app, so its cells are read
// through the Bitable API instead of the spreadsheet values API.
func (c *FeishuWikiConnector) embeddedBitableMarkdown(ctx context.Context, spreadsheetToken, sheetID, title string) (string, error) {
	blockInfo, err := c.getSheetBlockInfo(ctx, spreadsheetToken, sheetID)
	if err != nil {
		return "", err
	}
	blockToken := stringConfig(blockInfo["blockToken"])
	if blockToken == "" {
		return "", &ConnectorValidationError{Message: fmt.Sprintf("Feishu worksheet %q is a bitable block but exposes no app token", title)}
	}
	appToken, tableID := feishuSheetBlockToken(blockToken)
	if appToken == "" {
		return "", &ConnectorValidationError{Message: fmt.Sprintf("Feishu worksheet %q has an unrecognised bitable block token %q", title, blockToken)}
	}
	tables, err := c.listBitableTables(ctx, appToken)
	if err != nil {
		return "", err
	}
	for _, table := range tables {
		if stringConfig(table["table_id"]) == tableID {
			return c.bitableTableSection(ctx, appToken, table, title)
		}
	}
	return "", &ConnectorValidationError{Message: fmt.Sprintf("Feishu worksheet %q references bitable table %q, which the app does not expose", title, tableID)}
}

// getSheetBlockInfo returns one worksheet's blockInfo from the spreadsheet
// metadata. Worksheets without an embedded block return a nil map.
func (c *FeishuWikiConnector) getSheetBlockInfo(ctx context.Context, spreadsheetToken, sheetID string) (map[string]any, error) {
	endpoint := fmt.Sprintf("%s/open-apis/sheets/v2/spreadsheets/%s/metainfo", c.baseURL, url.PathEscape(spreadsheetToken))
	payload, err := c.feishuGet(ctx, endpoint, "read spreadsheet metadata")
	if err != nil {
		return nil, err
	}
	data, _ := payload["data"].(map[string]any)
	for _, sheet := range mapItems(data["sheets"]) {
		if stringConfig(sheet["sheetId"]) != sheetID {
			continue
		}
		blockInfo, _ := sheet["blockInfo"].(map[string]any)
		return blockInfo, nil
	}
	return nil, nil
}

// feishuSheetBlockToken splits a worksheet's block token into the embedded
// bitable app token and table id. Feishu joins them with "_", so the table id is
// the tbl-prefixed suffix after the last separator.
func feishuSheetBlockToken(blockToken string) (string, string) {
	separator := strings.LastIndex(blockToken, "_")
	if separator <= 0 || separator == len(blockToken)-1 {
		return "", ""
	}
	tableID := blockToken[separator+1:]
	if !strings.HasPrefix(tableID, "tbl") {
		return "", ""
	}
	return blockToken[:separator], tableID
}

// listSheets lists one spreadsheet's worksheets.
func (c *FeishuWikiConnector) listSheets(ctx context.Context, spreadsheetToken string) ([]map[string]any, error) {
	endpoint := fmt.Sprintf("%s/open-apis/sheets/v3/spreadsheets/%s/sheets/query", c.baseURL, url.PathEscape(spreadsheetToken))
	payload, err := c.feishuGet(ctx, endpoint, "list spreadsheet worksheets")
	if err != nil {
		return nil, err
	}
	data, _ := payload["data"].(map[string]any)
	if data == nil {
		return nil, &ConnectorValidationError{Message: "Feishu spreadsheet worksheet response contained invalid data"}
	}
	return mapItems(data["sheets"]), nil
}

// sheetValues reads a worksheet's cells in row chunks bounded by the per-request
// cell limit, stopping at the sync row cap.
func (c *FeishuWikiConnector) sheetValues(ctx context.Context, spreadsheetToken, sheetID string, rowCount, columnCount int) ([][]any, bool, error) {
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
		rangeRef := fmt.Sprintf("%s!A%d:%s%d", url.PathEscape(sheetID), start, lastColumn, end)
		rows, err := c.listSheetValues(ctx, spreadsheetToken, rangeRef)
		if err != nil {
			return nil, false, err
		}
		values = append(values, rows...)
	}
	return values, truncated, nil
}

// listSheetValues reads one cell range of a spreadsheet.
func (c *FeishuWikiConnector) listSheetValues(ctx context.Context, spreadsheetToken, rangeRef string) ([][]any, error) {
	endpoint := fmt.Sprintf("%s/open-apis/sheets/v2/spreadsheets/%s/values/%s?valueRenderOption=ToString&dateTimeRenderOption=FormattedString",
		c.baseURL, url.PathEscape(spreadsheetToken), rangeRef)
	payload, err := c.feishuGet(ctx, endpoint, "read spreadsheet values")
	if err != nil {
		return nil, err
	}
	data, _ := payload["data"].(map[string]any)
	valueRange, _ := data["valueRange"].(map[string]any)
	raw, _ := valueRange["values"].([]any)
	rows := make([][]any, 0, len(raw))
	for _, row := range raw {
		if cells, ok := row.([]any); ok {
			rows = append(rows, cells)
		}
	}
	return rows, nil
}
