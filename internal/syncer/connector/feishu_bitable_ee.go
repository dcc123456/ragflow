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
	feishuBitableMetaPageSize = 100
	feishuBitablePageSize     = 500
	feishuBitableMaxRecords   = 2000
)

// bitableMarkdown renders every data table of one Bitable app to Markdown, one
// section per table.
func (c *FeishuWikiConnector) bitableMarkdown(ctx context.Context, appToken string) ([]byte, error) {
	tables, err := c.listBitableTables(ctx, appToken)
	if err != nil {
		return nil, err
	}
	var out strings.Builder
	for _, table := range tables {
		if stringConfig(table["table_id"]) == "" {
			continue
		}
		section, err := c.bitableTableSection(ctx, appToken, table, "")
		if err != nil {
			return nil, err
		}
		out.WriteString(section)
	}
	return []byte(strings.TrimSpace(out.String())), nil
}

// bitableTableSection renders one data table as a Markdown section. An empty
// title falls back to the table name; a table without fields or records renders
// as an empty string.
func (c *FeishuWikiConnector) bitableTableSection(ctx context.Context, appToken string, table map[string]any, title string) (string, error) {
	tableID := stringConfig(table["table_id"])
	fields, err := c.listBitableFields(ctx, appToken, tableID)
	if err != nil {
		return "", err
	}
	if len(fields) == 0 {
		return "", nil
	}
	records, truncated, err := c.listBitableRecords(ctx, appToken, tableID)
	if err != nil {
		return "", err
	}
	if len(records) == 0 {
		return "", nil
	}
	rows := make([][]string, 0, len(records)+1)
	rows = append(rows, fields)
	for _, record := range records {
		recordFields, _ := record["fields"].(map[string]any)
		cells := make([]string, 0, len(fields))
		for _, field := range fields {
			cells = append(cells, cellValue(recordFields[field]))
		}
		rows = append(rows, cells)
	}
	rendered := markdownTable(rows)
	if rendered == "" {
		return "", nil
	}
	var out strings.Builder
	out.WriteString("## " + firstNonEmpty(title, stringConfig(table["name"]), tableID) + "\n\n" + rendered)
	if truncated {
		out.WriteString(fmt.Sprintf("_Truncated: only the first %d records are synced._\n\n", feishuBitableMaxRecords))
	}
	return out.String(), nil
}

// listBitableTables lists one Bitable app's data tables.
func (c *FeishuWikiConnector) listBitableTables(ctx context.Context, appToken string) ([]map[string]any, error) {
	return c.listBitableItems(ctx, fmt.Sprintf("%s/open-apis/bitable/v1/apps/%s/tables", c.baseURL, url.PathEscape(appToken)), "list bitable tables")
}

// listBitableFields lists one data table's field names in column order.
func (c *FeishuWikiConnector) listBitableFields(ctx context.Context, appToken, tableID string) ([]string, error) {
	items, err := c.listBitableItems(ctx, fmt.Sprintf("%s/open-apis/bitable/v1/apps/%s/tables/%s/fields",
		c.baseURL, url.PathEscape(appToken), url.PathEscape(tableID)), "list bitable fields")
	if err != nil {
		return nil, err
	}
	fields := make([]string, 0, len(items))
	for _, item := range items {
		if name := stringConfig(item["field_name"]); name != "" {
			fields = append(fields, name)
		}
	}
	return fields, nil
}

// listBitableRecords lists one data table's records up to the sync cap.
func (c *FeishuWikiConnector) listBitableRecords(ctx context.Context, appToken, tableID string) ([]map[string]any, bool, error) {
	endpoint := fmt.Sprintf("%s/open-apis/bitable/v1/apps/%s/tables/%s/records?page_size=%d",
		c.baseURL, url.PathEscape(appToken), url.PathEscape(tableID), feishuBitablePageSize)
	var records []map[string]any
	pageToken := ""
	usedTokens := map[string]struct{}{}
	for {
		pageEndpoint := endpoint
		if pageToken != "" {
			pageEndpoint += "&page_token=" + url.QueryEscape(pageToken)
		}
		page, next, err := c.listBitablePage(ctx, pageEndpoint, "list bitable records")
		if err != nil {
			return nil, false, err
		}
		records = append(records, page...)
		if len(records) >= feishuBitableMaxRecords {
			return records[:feishuBitableMaxRecords], len(records) > feishuBitableMaxRecords || next != "", nil
		}
		if next == "" {
			return records, false, nil
		}
		if err := feishuBitableAcceptPageToken(usedTokens, next, "list bitable records"); err != nil {
			return nil, false, err
		}
		pageToken = next
	}
}

// listBitableItems lists every item of a paginated Bitable metadata endpoint.
func (c *FeishuWikiConnector) listBitableItems(ctx context.Context, endpoint, operation string) ([]map[string]any, error) {
	var items []map[string]any
	pageToken := ""
	usedTokens := map[string]struct{}{}
	for {
		pageEndpoint := endpoint + fmt.Sprintf("?page_size=%d", feishuBitableMetaPageSize)
		if pageToken != "" {
			pageEndpoint += "&page_token=" + url.QueryEscape(pageToken)
		}
		page, next, err := c.listBitablePage(ctx, pageEndpoint, operation)
		if err != nil {
			return nil, err
		}
		items = append(items, page...)
		if next == "" {
			return items, nil
		}
		if err := feishuBitableAcceptPageToken(usedTokens, next, operation); err != nil {
			return nil, err
		}
		pageToken = next
	}
}

// listBitablePage lists one page of a Bitable endpoint and returns its items and
// next page token. The token is only returned when has_more is set: Feishu also
// fills page_token on the last page, and replaying that token answers
// 1254001 WrongRequestBody.
func (c *FeishuWikiConnector) listBitablePage(ctx context.Context, endpoint, operation string) ([]map[string]any, string, error) {
	payload, err := c.feishuGet(ctx, endpoint, operation)
	if err != nil {
		return nil, "", err
	}
	data, _ := payload["data"].(map[string]any)
	if data == nil {
		return nil, "", &ConnectorValidationError{Message: "Feishu bitable response contained invalid data"}
	}
	hasMore, _ := data["has_more"].(bool)
	next := stringConfig(data["page_token"])
	if !hasMore {
		next = ""
	} else if next == "" {
		return nil, "", &ConnectorValidationError{Message: "Feishu bitable pagination indicated more data without a page token"}
	}
	return mapItems(data["items"]), next, nil
}

// feishuBitableAcceptPageToken records the page token that is about to be
// consumed and rejects a token the loop already used, so a misbehaving
// paginator fails with context instead of requesting pages forever.
func feishuBitableAcceptPageToken(used map[string]struct{}, next, operation string) error {
	if _, repeated := used[next]; repeated {
		return &ConnectorValidationError{Message: "Feishu bitable pagination repeated a page token while attempting to " + operation}
	}
	used[next] = struct{}{}
	return nil
}
