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
	"math"
	"regexp"
	"sort"
	"strings"

	"ragflow/internal/parser/cadsource"
)

// cadNumericValue matches a drawing value rather than a prose label. It is
// intentionally narrower than "contains a digit": tag IDs such as FCV-2 stay
// labels, while 26.8 m2 and 1:50 are values.
var cadNumericValue = regexp.MustCompile(`^[+\-−]?\d[\d.,]*(?:\s*(?:m2|m²|mm|cm|m|in|"|%))?$`)

type cadLayoutColumn struct {
	minX, maxX float64
	boxes      []cadTextBox
}

type cadLayoutBoxRow struct {
	y     float64
	boxes []cadTextBox
}

// cadLayoutSections splits a chunk at large horizontal whitespace bands. The
// sections stay inside one chunk; the split only gives table/prose detection a
// local view instead of a whole-sheet stream.
func cadLayoutSections(boxes []cadTextBox, scale float64) [][]cadTextBox {
	if len(boxes) <= 1 {
		return [][]cadTextBox{boxes}
	}
	ordered := append([]cadTextBox(nil), boxes...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].item.y != ordered[j].item.y {
			return ordered[i].item.y > ordered[j].item.y
		}
		return ordered[i].minX < ordered[j].minX
	})
	bounds := cadBoxesBounds(ordered)
	sectionWidth := math.Max(bounds.width(), 1)
	threshold := math.Max(3*scale, 0.01*sectionWidth)
	var sections [][]cadTextBox
	var current []cadTextBox
	lastBottom := math.Inf(1)
	for _, box := range ordered {
		if len(current) > 0 {
			gap := lastBottom - box.maxY
			if gap > threshold {
				sections = append(sections, current)
				current = nil
			}
		}
		current = append(current, box)
		if box.minY < lastBottom {
			lastBottom = box.minY
		}
	}
	if len(current) > 0 {
		sections = append(sections, current)
	}
	return sections
}

func cadSerializeLayoutSection(boxes []cadTextBox, scale float64) string {
	if len(boxes) == 0 {
		return ""
	}
	if cadIsScaleBarSection(boxes) {
		return cadFallbackContent(boxes)
	}
	columns := cadLayoutColumns(boxes, scale)
	rows := cadLayoutBoxRows(boxes, scale)
	if text, ok := cadSerializeLabelValueGrid(columns, rows); ok {
		return text
	}
	if text, ok := cadSerializeDimensionSection(boxes, rows); ok {
		return text
	}
	if text, ok := cadSerializeColumnPairs(columns); ok {
		return text
	}
	if text, ok := cadSerializeTable(columns, rows); ok {
		return text
	}
	if text, ok := cadSerializeProseColumns(columns); ok {
		return text
	}
	return cadFallbackContent(boxes)
}

func cadLayoutColumns(boxes []cadTextBox, scale float64) []cadLayoutColumn {
	ordered := append([]cadTextBox(nil), boxes...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].minX < ordered[j].minX })
	var columns []cadLayoutColumn
	for _, box := range ordered {
		placed := false
		for i := range columns {
			if box.minX <= columns[i].maxX+2*scale {
				columns[i].boxes = append(columns[i].boxes, box)
				columns[i].maxX = math.Max(columns[i].maxX, box.maxX)
				columns[i].minX = math.Min(columns[i].minX, box.minX)
				placed = true
				break
			}
		}
		if !placed {
			columns = append(columns, cadLayoutColumn{minX: box.minX, maxX: box.maxX, boxes: []cadTextBox{box}})
		}
	}
	return columns
}

func cadLayoutBoxRows(boxes []cadTextBox, scale float64) []cadLayoutBoxRow {
	ordered := append([]cadTextBox(nil), boxes...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].item.y != ordered[j].item.y {
			return ordered[i].item.y > ordered[j].item.y
		}
		return ordered[i].minX < ordered[j].minX
	})
	var rows []cadLayoutBoxRow
	for _, box := range ordered {
		placed := false
		for i := range rows {
			if math.Abs(box.item.y-rows[i].y) <= 0.6*scale {
				rows[i].boxes = append(rows[i].boxes, box)
				placed = true
				break
			}
		}
		if !placed {
			rows = append(rows, cadLayoutBoxRow{y: box.item.y, boxes: []cadTextBox{box}})
		}
	}
	return rows
}

func cadColumnIndex(columns []cadLayoutColumn, box cadTextBox) int {
	center := (box.minX + box.maxX) / 2
	best, bestDistance := -1, math.Inf(1)
	for i, column := range columns {
		if center >= column.minX && center <= column.maxX {
			return i
		}
		mid := (column.minX + column.maxX) / 2
		if distance := math.Abs(center - mid); distance < bestDistance {
			best, bestDistance = i, distance
		}
	}
	return best
}

func cadCellForColumn(row cadLayoutBoxRow, columns []cadLayoutColumn, index int) (cadTextBox, bool) {
	for _, box := range row.boxes {
		if cadColumnIndex(columns, box) == index {
			return box, true
		}
	}
	return cadTextBox{}, false
}

func cadSerializeLabelValueGrid(columns []cadLayoutColumn, rows []cadLayoutBoxRow) (string, bool) {
	if len(columns) < 2 || len(rows) < 2 || len(rows) > 3 {
		return "", false
	}
	// A label/value grid has one label cell and one value cell per column. More
	// boxes in a column means a schedule or prose block, where pairing the first
	// two cells would transpose the content.
	for _, column := range columns {
		if len(column.boxes) > 2 {
			return "", false
		}
	}
	valueRow := -1
	for i, row := range rows {
		values := 0
		for _, box := range row.boxes {
			if cadIsValue(box.item.text) {
				values++
			}
		}
		if values*5 >= len(row.boxes)*3 && values >= 2 {
			valueRow = i
			break
		}
	}
	if valueRow < 0 {
		return "", false
	}
	labelRow := valueRow - 1
	if labelRow < 0 {
		labelRow = valueRow + 1
	}
	if labelRow < 0 || labelRow >= len(rows) {
		return "", false
	}
	labels := 0
	for _, box := range rows[labelRow].boxes {
		if !cadIsValue(box.item.text) {
			labels++
		}
	}
	if labels*5 < len(rows[labelRow].boxes)*3 {
		return "", false
	}
	var pairs []string
	for i := range columns {
		label, okLabel := cadCellForColumn(rows[labelRow], columns, i)
		value, okValue := cadCellForColumn(rows[valueRow], columns, i)
		if okLabel && okValue && !cadIsValue(label.item.text) && len(strings.Fields(label.item.text)) < 5 && cadIsValue(value.item.text) {
			pairs = append(pairs, strings.TrimSpace(label.item.text)+": "+strings.TrimSpace(value.item.text))
		}
	}
	if len(pairs) < 2 {
		return "", false
	}
	var lines []string
	for i, row := range rows {
		if i == labelRow || i == valueRow {
			continue
		}
		for _, box := range row.boxes {
			lines = append(lines, box.item.text)
		}
	}
	lines = append(lines, pairs...)
	return strings.Join(lines, "\n"), true
}

func cadSerializeDimensionSection(boxes []cadTextBox, rows []cadLayoutBoxRow) (string, bool) {
	dimensions := 0
	for _, box := range boxes {
		if cadIsDimension(box.item) {
			dimensions++
		}
	}
	if dimensions*5 < len(boxes)*4 {
		return "", false
	}
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		ordered := append([]cadTextBox(nil), row.boxes...)
		sort.SliceStable(ordered, func(a, b int) bool { return ordered[a].minX < ordered[b].minX })
		values := make([]string, 0, len(ordered))
		for _, box := range ordered {
			if text := strings.TrimSpace(box.item.text); text != "" {
				values = append(values, text)
			}
		}
		if len(values) > 0 {
			lines = append(lines, "  "+strings.Join(values, ", "))
		}
	}
	if len(lines) == 0 {
		return "", false
	}
	return "Dimensions:\n" + strings.Join(lines, "\n"), true
}

func cadSerializeTable(columns []cadLayoutColumn, rows []cadLayoutBoxRow) (string, bool) {
	if len(columns) < 2 || len(rows) < 2 || len(columns) > 8 || len(rows) > 40 {
		return "", false
	}
	cells, words, longCells := 0, 0, 0
	for _, row := range rows {
		for _, box := range row.boxes {
			cells++
			wordCount := len(strings.Fields(box.item.text))
			words += wordCount
			if wordCount >= 5 {
				longCells++
			}
		}
	}
	if cells*2 < len(columns)*len(rows) || cells == 0 || words > 6*cells {
		return "", false
	}
	// Long cells are prose columns, not table cells; row-major output would
	// interleave the paragraphs (the belt ARR case).
	if longCells*3 >= cells {
		return "", false
	}
	// Two columns with a numeric value column are row pairs, e.g. a room/area
	// schedule. Preserve one field per line instead of a bare "a | b" row.
	if len(columns) == 2 {
		var other, pairs []string
		for _, row := range rows {
			if len(row.boxes) != 2 {
				for _, box := range row.boxes {
					other = append(other, strings.TrimSpace(box.item.text))
				}
				continue
			}
			left, right := row.boxes[0], row.boxes[1]
			if cadIsValue(left.item.text) && !cadIsValue(right.item.text) {
				left, right = right, left
			}
			if cadIsValue(right.item.text) && !cadIsValue(left.item.text) {
				pairs = append(pairs, strings.TrimSpace(left.item.text)+": "+strings.TrimSpace(right.item.text))
			} else {
				other = append(other, strings.TrimSpace(left.item.text), strings.TrimSpace(right.item.text))
			}
		}
		if len(pairs) >= 2 {
			return strings.Join(append(other, pairs...), "\n"), true
		}
	}
	var lines []string
	for _, row := range rows {
		if len(row.boxes) == 0 {
			continue
		}
		sort.SliceStable(row.boxes, func(i, j int) bool { return row.boxes[i].minX < row.boxes[j].minX })
		parts := make([]string, len(row.boxes))
		for i, box := range row.boxes {
			parts[i] = strings.TrimSpace(box.item.text)
		}
		lines = append(lines, strings.Join(parts, " | "))
	}
	if len(lines) < 2 {
		return "", false
	}
	return strings.Join(lines, "\n"), true
}

func cadSerializeProseColumns(columns []cadLayoutColumn) (string, bool) {
	if len(columns) < 2 {
		return "", false
	}
	long := 0
	total := 0
	for _, column := range columns {
		for _, box := range column.boxes {
			total++
			if len(strings.Fields(box.item.text)) >= 5 {
				long++
			}
		}
	}
	if total == 0 || long*3 < total {
		return "", false
	}
	var blocks []string
	for _, column := range columns {
		ordered := append([]cadTextBox(nil), column.boxes...)
		sort.SliceStable(ordered, func(i, j int) bool {
			if ordered[i].item.y != ordered[j].item.y {
				return ordered[i].item.y > ordered[j].item.y
			}
			return ordered[i].minX < ordered[j].minX
		})
		lines := make([]string, len(ordered))
		for i, box := range ordered {
			lines[i] = box.item.text
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	return strings.Join(blocks, "\n\n"), true
}

func cadIsScaleBarSection(boxes []cadTextBox) bool {
	for _, box := range boxes {
		text := strings.ToLower(strings.TrimSpace(box.item.text))
		if strings.Contains(text, "scala grafica") || strings.Contains(text, "graphic scale") {
			return true
		}
	}
	return false
}

func cadFallbackContent(boxes []cadTextBox) string {
	items := cadBoxesToItems(boxes)
	sortCADTextItems(items)
	lines := make([]string, len(items))
	for i, item := range items {
		lines[i] = item.text
	}
	return strings.Join(lines, "\n")
}

func cadIsValue(text string) bool {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(strings.ToUpper(text), "S =") {
		return true
	}
	return cadNumericValue.MatchString(text)
}

// cadSerializeColumnPairs handles label-above-value columns that do not share
// baselines, such as room names with their area lines.
func cadSerializeColumnPairs(columns []cadLayoutColumn) (string, bool) {
	if len(columns) < 2 {
		return "", false
	}
	var pairs, others []string
	paired := 0
	for _, column := range columns {
		if len(column.boxes) > 2 {
			return "", false
		}
		for _, box := range column.boxes {
			if len(strings.Fields(box.item.text)) >= 5 {
				return "", false
			}
		}
		boxes := append([]cadTextBox(nil), column.boxes...)
		sort.SliceStable(boxes, func(i, j int) bool { return boxes[i].item.y > boxes[j].item.y })
		if len(boxes) == 2 && !cadIsValue(boxes[0].item.text) && cadIsValue(boxes[1].item.text) {
			pairs = append(pairs, strings.TrimSpace(boxes[0].item.text)+": "+strings.TrimSpace(boxes[1].item.text))
			paired++
			continue
		}
		for _, box := range boxes {
			others = append(others, strings.TrimSpace(box.item.text))
		}
	}
	if paired < 2 || len(others) > 1 {
		return "", false
	}
	return strings.Join(append(others, pairs...), "\n"), true
}

func cadIsDimension(item cadTextItem) bool {
	if item.kind == cadsource.KindDimension {
		return true
	}
	return cadNumericValue.MatchString(strings.TrimSpace(item.text))
}
