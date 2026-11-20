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
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// markdownTable renders rows as a GFM table whose header row is the first
// row. Empty rows are dropped.
func markdownTable(rows [][]string) string {
	var out strings.Builder
	rendered := 0
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		values := make([]string, 0, len(row))
		for _, value := range row {
			values = append(values, markdownCell(value))
		}
		out.WriteString("| " + strings.Join(values, " | ") + " |\n")
		if rendered == 0 {
			separators := make([]string, len(values))
			for i := range separators {
				separators[i] = "---"
			}
			out.WriteString("| " + strings.Join(separators, " | ") + " |\n")
		}
		rendered++
	}
	if rendered == 0 {
		return ""
	}
	return out.String() + "\n"
}

// markdownCell keeps one value inside a single GFM table cell: a cell
// holds inline content only, so line breaks become <br> and pipes are escaped.
func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\n", "<br>")
	return strings.ReplaceAll(value, "|", "\\|")
}

// cellValue renders one spreadsheet or table cell value as text.
func cellValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := cellValue(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		for _, key := range []string{"text", "name", "link"} {
			if text, ok := typed[key].(string); ok && text != "" {
				return text
			}
		}
	}
	blob, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(blob)
}

func mapItems(value any) []map[string]any {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	items := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if node, ok := item.(map[string]any); ok {
			items = append(items, node)
		}
	}
	return items
}

// sheetRows converts the value API's rows to cell strings.
func sheetRows(rows [][]any) [][]string {
	out := make([][]string, 0, len(rows))
	for _, row := range rows {
		cells := make([]string, 0, len(row))
		for _, value := range row {
			cells = append(cells, cellValue(value))
		}
		out = append(out, cells)
	}
	return out
}

// sheetTrim drops the trailing all-empty rows and columns a worksheet's
// full grid returns.
func sheetTrim(rows [][]string) [][]string {
	lastRow := -1
	for index, row := range rows {
		for _, value := range row {
			if strings.TrimSpace(value) != "" {
				lastRow = index
				break
			}
		}
	}
	if lastRow < 0 {
		return nil
	}
	rows = rows[:lastRow+1]
	lastColumn := -1
	for _, row := range rows {
		for index, value := range row {
			if strings.TrimSpace(value) != "" && index > lastColumn {
				lastColumn = index
			}
		}
	}
	if lastColumn < 0 {
		return nil
	}
	for index, row := range rows {
		if len(row) > lastColumn+1 {
			rows[index] = row[:lastColumn+1]
			continue
		}
		for len(row) < lastColumn+1 {
			row = append(row, "")
		}
		rows[index] = row
	}
	return rows
}

// sheetColumnLetter converts a 1-based column number to its letters.
func sheetColumnLetter(column int) string {
	name := ""
	for column > 0 {
		column--
		name = string(rune('A'+column%26)) + name
		column /= 26
	}
	return name
}

// parseConfigInt parses a positive integer config value; empty/nil uses the
// fallback and an invalid non-empty value returns an error.
func parseConfigInt(value any, fallback int) (int, error) {
	if value == nil {
		return fallback, nil
	}
	switch typed := value.(type) {
	case string:
		typed = strings.TrimSpace(typed)
		if typed == "" {
			return fallback, nil
		}
		parsed, err := strconv.Atoi(typed)
		if err != nil {
			return 0, err
		}
		return parsed, nil
	case int:
		return typed, nil
	case int64:
		return int(typed), nil
	case float64:
		return int(typed), nil
	}
	return 0, fmt.Errorf("invalid integer %v", value)
}

// parseConfigInt64 parses an int64 config value; empty/nil uses the fallback.
func parseConfigInt64(value any, fallback int64) (int64, error) {
	if value == nil {
		return fallback, nil
	}
	switch typed := value.(type) {
	case string:
		typed = strings.TrimSpace(typed)
		if typed == "" {
			return fallback, nil
		}
		parsed, err := strconv.ParseInt(typed, 10, 64)
		if err != nil {
			return 0, err
		}
		return parsed, nil
	case int:
		return int64(typed), nil
	case int64:
		return typed, nil
	case float64:
		return int64(typed), nil
	}
	return 0, fmt.Errorf("invalid integer %v", value)
}

// normalizeStrings flattens a comma string or a []any value into trimmed,
// casefolded strings.
func normalizeStrings(value any) []string {
	var raw []string
	switch typed := value.(type) {
	case string:
		raw = strings.Split(typed, ",")
	case []any:
		for _, item := range typed {
			raw = append(raw, fmt.Sprint(item))
		}
	case []string:
		raw = typed
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, strings.ToLower(item))
	}
	return out
}

func extensionSet(value any) map[string]struct{} {
	set := map[string]struct{}{}
	for _, term := range normalizeStrings(value) {
		term = strings.TrimLeft(term, ".")
		if term == "" {
			continue
		}
		set["."+term] = struct{}{}
	}
	return set
}

func keywordSet(value any) map[string]struct{} {
	set := map[string]struct{}{}
	for _, term := range normalizeStrings(value) {
		set[term] = struct{}{}
	}
	return set
}

func containsAny(value string, terms map[string]struct{}) bool {
	for term := range terms {
		if strings.Contains(value, term) {
			return true
		}
	}
	return false
}

// connectorWithinWindow reports whether a node update time falls in (start, end].
// Nodes without a timestamp are always included so content is not silently
// omitted.
func connectorWithinWindow(updatedAt *time.Time, start, end *time.Time) bool {
	if updatedAt == nil {
		return true
	}
	if start != nil && !start.Before(*updatedAt) {
		return false
	}
	return end == nil || !updatedAt.After(*end)
}

// syncNodePath joins one path segment onto its parent folder chain, skipping an
// empty segment so an untitled level cannot leave a trailing separator.
func syncNodePath(parentPath, segment string) string {
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return parentPath
	}
	if parentPath == "" {
		return segment
	}
	return parentPath + "/" + segment
}
