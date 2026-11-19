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

package chunker

import (
	"strings"
	"unicode/utf8"
)

// cadTitleField is one title-block label family. Key is the persisted field
// name; an empty Key marks a label that only classifies title-block text.
type cadTitleField struct {
	Key    string
	Labels []string
}

// cadTitleFields is the single label table shared by title-block classification
// and metadata extraction. Matching is punctuation-insensitive, so "DWG NO."
// and "dwg no" are the same label.
var cadTitleFields = []cadTitleField{
	{Key: "drawing_no", Labels: []string{"dwg no", "drawing no", "drawing number", "图号"}},
	{Key: "drawing_title", Labels: []string{"title", "drawing title", "图名"}},
	{Key: "scale", Labels: []string{"scale", "scala", "比例"}},
	{Key: "sheet", Labels: []string{"sheet", "sht", "tavola"}},
	{Key: "revision", Labels: []string{"rev", "revision", "版本"}},
	{Key: "", Labels: []string{
		"材料", "设计", "制图", "审核", "校对", "日期", "重量", "单位名称",
		"material", "drawn", "designed", "checked", "reviewed", "approved",
		"date", "weight", "formato", "data", "nama", "nim", "tanggal",
		"no. project", "projek ke", "tugas ke",
	}},
}

// cadTitleFieldLabelList flattens the table for title-block classification.
func cadTitleFieldLabelList() []string {
	var labels []string
	for _, field := range cadTitleFields {
		labels = append(labels, field.Labels...)
	}
	return labels
}

// cadNormalizeTitleLabel lowercases a label and drops the separators CAD text
// uses around it ("SCALE:", "DWG NO.", "图号：").
func cadNormalizeTitleLabel(text string) string {
	text = strings.ToLower(strings.Join(strings.Fields(text), " "))
	return strings.TrimRight(text, ":：.。,， ")
}

// cadTitleFieldKey maps one label to its persisted field name.
func cadTitleFieldKey(label string) string {
	label = cadNormalizeTitleLabel(label)
	if label == "" {
		return ""
	}
	for _, field := range cadTitleFields {
		if field.Key == "" {
			continue
		}
		for _, candidate := range field.Labels {
			if cadNormalizeTitleLabel(candidate) == label {
				return field.Key
			}
		}
	}
	return ""
}

// cadSplitTitleField splits a "LABEL: value" line on the first ASCII or
// full-width colon. CAD title blocks use both separators.
func cadSplitTitleField(text string) (label, value string, ok bool) {
	index := strings.IndexAny(text, ":：")
	if index < 0 {
		return "", "", false
	}
	_, size := utf8.DecodeRuneInString(text[index:])
	label = strings.TrimSpace(text[:index])
	value = strings.TrimLeft(text[index+size:], ":： \t")
	value = strings.Join(strings.Fields(value), " ")
	if label == "" || value == "" {
		return "", "", false
	}
	return label, value, true
}

// cadNormalizeInlineTitleField rewrites an inline "图号：M1" as "图号: M1" so the
// chunk text and the metadata parser use one separator.
func cadNormalizeInlineTitleField(text string) string {
	if !cadMatchesAnyFieldLabel(text) {
		return text
	}
	label, value, ok := cadSplitTitleField(text)
	if !ok {
		return text
	}
	return label + ": " + value
}

// cadTitleMetadata extracts only explicitly labelled title-block fields. A
// value is never inferred from position or from an unlabelled line.
func cadTitleMetadata(content string) map[string]string {
	fields := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		label, value, ok := cadSplitTitleField(line)
		if !ok {
			continue
		}
		key := cadTitleFieldKey(label)
		if key == "" || fields[key] != "" {
			continue
		}
		fields[key] = value
	}
	return fields
}

// cadChunkMetadata derives document-level drawing identity and sheet-local
// scale/sheet/revision fields from title-block chunks. The document fields are
// aggregated by the indexer; the sheet fields stay on each chunk's CAD extra.
func cadChunkMetadata(chunks []cadChunk) (document map[string]string, bySheet map[string]map[string]string) {
	document = make(map[string]string)
	bySheet = make(map[string]map[string]string)
	for _, chunk := range chunks {
		if chunk.Kind != cadKindTitleBlock {
			continue
		}
		fields := cadTitleMetadata(chunk.Content)
		if len(fields) == 0 {
			continue
		}
		for _, key := range []string{"drawing_no", "drawing_title"} {
			if document[key] == "" && fields[key] != "" {
				document[key] = fields[key]
			}
		}
		delete(fields, "drawing_no")
		delete(fields, "drawing_title")
		if len(fields) == 0 {
			continue
		}
		// Several title blocks can share a sheet; keep the first non-empty
		// value per field instead of letting a later block overwrite it.
		sheet := bySheet[chunk.SheetID]
		if sheet == nil {
			sheet = make(map[string]string, len(fields))
			bySheet[chunk.SheetID] = sheet
		}
		for key, value := range fields {
			if sheet[key] == "" {
				sheet[key] = value
			}
		}
	}
	return document, bySheet
}
