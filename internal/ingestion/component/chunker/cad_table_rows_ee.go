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
	"sort"
	"strings"
	"unicode"
)

// cadTableRow is one confirmed table row. Rows stay inside the chunk text:
// the cells are formatted into the block.
type cadTableRow struct {
	Cells []cadTableRowCell
}

type cadTableRowCell struct {
	Column string
	Text   string
}

type cadHeaderColumn struct {
	Text       string
	MinX, MaxX float64
	Bottom     float64
	Boxes      []cadTextBox
}

// cadBuildLayout orders one block and extracts confirmed rows. It never
// changes the block boundary.
func cadBuildLayout(boxes []cadTextBox, scale float64) string {
	if len(boxes) == 0 {
		return ""
	}
	if scale <= 0 {
		scale = cadMedianItemHeight(cadBoxesToItems(boxes))
	}
	if scale <= 0 {
		scale = 1
	}
	var parts []string
	for _, section := range cadLayoutSections(boxes, scale) {
		rest, rows := cadExtractRows(section, scale)
		text := cadWithSectionHeading(section, cadSerializeLayoutSection(rest, scale))
		for _, row := range rows {
			if text != "" {
				text += "\n"
			}
			text += cadTableRowText(row)
		}
		if strings.TrimSpace(text) != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func cadLayoutContent(boxes []cadTextBox, scale float64) string {
	return cadBuildLayout(boxes, scale)
}

// cadWithSectionHeading marks a native drawing heading (DETAIL, SCHEDULE,
// LEGEND, ...) as a section heading. It never invents one: the heading must be
// a text item already present in the section.
func cadWithSectionHeading(section []cadTextBox, text string) string {
	heading := cadNativeSectionHeading(section)
	if heading == "" || strings.TrimSpace(text) == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	normalized := cadNormalizeHeadingText(heading)
	for i, line := range lines {
		if cadNormalizeHeadingText(line) == normalized {
			lines = append(lines[:i], lines[i+1:]...)
			break
		}
	}
	body := strings.Join(lines, "\n")
	if strings.TrimSpace(body) == "" {
		return "## " + heading
	}
	return "## " + heading + "\n" + body
}

func cadNativeSectionHeading(section []cadTextBox) string {
	type candidate struct {
		text string
		y    float64
	}
	var best candidate
	for _, box := range section {
		if box.item.legendRow {
			continue
		}
		text := strings.Join(strings.Fields(box.item.text), " ")
		if !cadSectionHeadingText(text) {
			continue
		}
		if best.text == "" || box.item.y > best.y {
			best = candidate{text: text, y: box.item.y}
		}
	}
	return best.text
}

func cadNormalizeHeadingText(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

func cadSectionHeadingText(text string) bool {
	upper := strings.ToUpper(strings.TrimSpace(text))
	if upper == "" || len([]rune(upper)) > 80 {
		return false
	}
	for _, prefix := range []string{"SEE ", "REF ", "FOR ", "TO "} {
		if strings.HasPrefix(upper, prefix) {
			return false
		}
	}
	for _, token := range []string{
		" DETAIL", "DETAILS", "SCHEDULE", "LEGEND", "BILL OF MATERIALS",
		"DISTINTA DEI PEZZI", "SUPERFICI UTILI", "TECHNICAL REQUIREMENTS",
	} {
		if strings.Contains(" "+upper, token) {
			return true
		}
	}
	return upper == "LEGEND" || upper == "NOTES" || upper == "NOTE"
}

func cadExtractRows(section []cadTextBox, scale float64) ([]cadTextBox, []cadTableRow) {
	rest := append([]cadTextBox(nil), section...)
	rest, legendRows, legendDetected := cadExtractLegendRows(rest)
	if legendDetected {
		return rest, legendRows
	}
	rest, tableRows := cadExtractHeaderRows(rest, scale)
	if len(tableRows) > 0 {
		return rest, tableRows
	}
	rest, areaRows := cadExtractAreaRows(rest, scale)
	return rest, areaRows
}

func cadTableRowText(row cadTableRow) string {
	parts := make([]string, 0, len(row.Cells))
	for _, cell := range row.Cells {
		if strings.TrimSpace(cell.Text) == "" {
			continue
		}
		parts = append(parts, cell.Column+": "+cell.Text)
	}
	return strings.Join(parts, " | ")
}

func cadExtractLegendRows(boxes []cadTextBox) ([]cadTextBox, []cadTableRow, bool) {
	detected := false
	for _, box := range boxes {
		if box.item.legendRow {
			detected = true
			break
		}
	}
	if !detected {
		return boxes, nil, false
	}
	var rest []cadTextBox
	var rows []cadTableRow
	for _, box := range boxes {
		if !box.item.legendRow {
			rest = append(rest, box)
			continue
		}
		tag, description := cadSplitLegendRow(box.item.text)
		if tag == "" || description == "" {
			rest = append(rest, box)
			continue
		}
		rows = append(rows, cadTableRow{Cells: []cadTableRowCell{
			{Column: "TAG NUMBER", Text: tag},
			{Column: "DESCRIPTION", Text: description},
		}})
	}
	return rest, rows, true
}

func cadSplitLegendRow(text string) (string, string) {
	parts := strings.SplitN(text, "\t", 2)
	if len(parts) != 2 {
		return "", ""
	}
	tag := strings.Join(strings.Fields(parts[0]), " ")
	description := strings.Join(strings.Fields(parts[1]), " ")
	return tag, description
}

func cadExtractHeaderRows(boxes []cadTextBox, scale float64) ([]cadTextBox, []cadTableRow) {
	columns, headerBottom, ok := cadFindTableHeader(boxes, scale)
	if !ok {
		return boxes, nil
	}
	key := -1
	for i, column := range columns {
		if cadTableKeyHeader(column.Text) {
			key = i
			break
		}
	}
	if key < 0 {
		return boxes, nil
	}
	anchorTolerance := math.Max(0.6*scale, 0.12*cadMedianColumnGap(columns))
	var anchors []int
	for i, box := range boxes {
		if box.item.legendRow || cadHeaderBox(box, columns) {
			continue
		}
		if math.Abs(box.minX-columns[key].MinX) > anchorTolerance {
			continue
		}
		if box.item.y >= headerBottom-0.2*scale || box.item.y < headerBottom-120*scale {
			continue
		}
		anchors = append(anchors, i)
	}
	sort.SliceStable(anchors, func(i, j int) bool { return boxes[anchors[i]].item.y > boxes[anchors[j]].item.y })
	var kept []int
	for i, index := range anchors {
		if i > 0 && boxes[kept[len(kept)-1]].item.y-boxes[index].item.y > 6*scale {
			break
		}
		kept = append(kept, index)
	}
	if len(kept) < 2 {
		return boxes, nil
	}
	anchorBoxes := make([]cadTextBox, len(kept))
	for i, index := range kept {
		anchorBoxes[i] = boxes[index]
	}
	tableMinX := columns[0].MinX - 2*scale
	tableMaxX := columns[len(columns)-1].MaxX + 5*scale
	for _, column := range columns {
		tableMaxX = math.Max(tableMaxX, column.MaxX+2*scale)
	}
	minY := boxes[kept[len(kept)-1]].minY - 2*scale
	maxY := headerBottom + 0.5*scale

	assigned := make([]bool, len(boxes))
	for _, index := range kept {
		assigned[index] = true
	}
	cellsByRow := make([][]cadTextBox, len(kept))
	indicesByRow := make([][]int, len(kept))
	for i, index := range kept {
		cellsByRow[i] = []cadTextBox{boxes[index]}
		indicesByRow[i] = []int{index}
	}
	columnTolerance := math.Max(0.6*scale, 0.12*cadMedianColumnGap(columns))
	for i := range boxes {
		box := &boxes[i]
		if assigned[i] || box.item.legendRow || cadHeaderBox(*box, columns) {
			continue
		}
		if box.minX < tableMinX || box.minX > tableMaxX || box.item.y > maxY || box.minY < minY {
			continue
		}
		column := cadNearestHeaderColumn(*box, columns, columnTolerance)
		if column < 0 {
			continue
		}
		row := cadRowForBox(*box, anchorBoxes)
		if row < 0 {
			continue
		}
		cellsByRow[row] = append(cellsByRow[row], *box)
		indicesByRow[row] = append(indicesByRow[row], i)
		assigned[i] = true
	}

	var rows []cadTableRow
	used := make([]bool, len(boxes))
	for i := range anchorBoxes {
		cells := cadBindRowCells(cellsByRow[i], columns, columnTolerance)
		if len(cells) == 0 {
			continue
		}
		rows = append(rows, cadTableRow{Cells: cells})
		for _, idx := range indicesByRow[i] {
			used[idx] = true
		}
	}
	if len(rows) < 2 {
		return boxes, nil
	}

	var rest []cadTextBox
	for i := range boxes {
		if !used[i] {
			rest = append(rest, boxes[i])
		}
	}
	return rest, rows
}

func cadFindTableHeader(boxes []cadTextBox, scale float64) ([]cadHeaderColumn, float64, bool) {
	var candidates []cadTextBox
	for _, box := range boxes {
		if box.item.legendRow {
			continue
		}
		if cadTableHeaderText(box.item.text) {
			candidates = append(candidates, box)
		}
	}
	if len(candidates) < 2 {
		return nil, 0, false
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].item.y > candidates[j].item.y })
	type group struct {
		y     float64
		boxes []cadTextBox
	}
	var groups []group
	for _, box := range candidates {
		placed := false
		for i := range groups {
			if math.Abs(box.item.y-groups[i].y) <= 1.2*scale {
				groups[i].boxes = append(groups[i].boxes, box)
				placed = true
				break
			}
		}
		if !placed {
			groups = append(groups, group{y: box.item.y, boxes: []cadTextBox{box}})
		}
	}
	best := -1
	bestScore := 0.0
	for i, group := range groups {
		if len(group.boxes) < 2 {
			continue
		}
		minX, maxX := math.Inf(1), math.Inf(-1)
		key := false
		for _, box := range group.boxes {
			minX = math.Min(minX, box.minX)
			maxX = math.Max(maxX, box.maxX)
			if cadTableKeyHeader(box.item.text) {
				key = true
			}
		}
		if !key || maxX-minX < 3*scale {
			continue
		}
		score := float64(len(group.boxes)) + (maxX-minX)/scale
		if score > bestScore {
			best, bestScore = i, score
		}
	}
	if best < 0 {
		return nil, 0, false
	}
	chosenY := groups[best].y
	var headerBoxes []cadTextBox
	for _, box := range candidates {
		if math.Abs(box.item.y-chosenY) <= 3*scale {
			headerBoxes = append(headerBoxes, box)
		}
	}
	sort.SliceStable(headerBoxes, func(i, j int) bool { return headerBoxes[i].minX < headerBoxes[j].minX })
	var columns []cadHeaderColumn
	for _, box := range headerBoxes {
		placed := false
		for i := range columns {
			if math.Abs(box.minX-columns[i].MinX) <= 0.75*scale {
				columns[i].Boxes = append(columns[i].Boxes, box)
				columns[i].MinX = math.Min(columns[i].MinX, box.minX)
				columns[i].MaxX = math.Max(columns[i].MaxX, box.maxX)
				columns[i].Bottom = math.Min(columns[i].Bottom, box.minY)
				placed = true
				break
			}
		}
		if !placed {
			columns = append(columns, cadHeaderColumn{
				MinX: box.minX, MaxX: box.maxX, Bottom: box.minY,
				Boxes: []cadTextBox{box},
			})
		}
	}
	for i := range columns {
		sort.SliceStable(columns[i].Boxes, func(a, b int) bool { return columns[i].Boxes[a].item.y > columns[i].Boxes[b].item.y })
		var texts []string
		for _, box := range columns[i].Boxes {
			texts = append(texts, strings.Join(strings.Fields(box.item.text), " "))
		}
		columns[i].Text = strings.Join(texts, " ")
	}
	headerBottom := math.Inf(1)
	for _, column := range columns {
		headerBottom = math.Min(headerBottom, column.Bottom)
	}
	if len(columns) < 2 || math.IsInf(headerBottom, 0) {
		return nil, 0, false
	}
	return columns, headerBottom, true
}

func cadHeaderBox(box cadTextBox, columns []cadHeaderColumn) bool {
	for _, column := range columns {
		for _, header := range column.Boxes {
			if box.item.handle == header.item.handle && box.item.text == header.item.text && box.minX == header.minX && box.minY == header.minY {
				return true
			}
		}
	}
	return false
}

func cadNearestHeaderColumn(box cadTextBox, columns []cadHeaderColumn, tolerance float64) int {
	best, distance := -1, math.Inf(1)
	for i, column := range columns {
		d := math.Abs(box.minX - column.MinX)
		if d <= tolerance && d < distance {
			best, distance = i, d
		}
	}
	return best
}

func cadRowForBox(box cadTextBox, anchors []cadTextBox) int {
	center := (box.minY + box.maxY) / 2
	best, distance := -1, math.Inf(1)
	for i, anchor := range anchors {
		d := math.Abs(center - anchor.item.y)
		if d < distance {
			best, distance = i, d
		}
	}
	return best
}

func cadBindRowCells(boxes []cadTextBox, columns []cadHeaderColumn, tolerance float64) []cadTableRowCell {
	if len(boxes) == 0 {
		return nil
	}
	cells := make([]cadTableRowCell, len(columns))
	for i, column := range columns {
		cells[i].Column = column.Text
	}
	for _, box := range boxes {
		index := cadNearestHeaderColumn(box, columns, tolerance)
		if index < 0 {
			continue
		}
		text := strings.Join(strings.Fields(box.item.text), " ")
		if strings.TrimSpace(cells[index].Text) == "" {
			cells[index].Text = text
		} else if !strings.Contains(cells[index].Text, text) {
			cells[index].Text += " " + text
		}
	}
	return cells
}

func cadMedianColumnGap(columns []cadHeaderColumn) float64 {
	if len(columns) < 2 {
		return 1
	}
	var gaps []float64
	for i := 1; i < len(columns); i++ {
		gaps = append(gaps, columns[i].MinX-columns[i-1].MinX)
	}
	sort.Float64s(gaps)
	return cadMedian(gaps)
}

func cadTableHeaderText(text string) bool {
	text = strings.ToUpper(strings.Join(strings.Fields(text), " "))
	if text == "" {
		return false
	}
	for _, token := range []string{
		"DESIG", "DESIGNATION", "TAG NUMBER", "DESCRIPTION", "DENOMINAZIONE",
		"MATERIALE", "MATERIAL", "Q.TA", "QTY", "QUANTITY", "REMARKS",
		"NOTE", "SIZE", "GPM", "PSID", "PART NUMBER", "PART NO.", "40% EG",
	} {
		if strings.Contains(text, token) {
			return true
		}
	}
	for _, token := range []string{"CV", "POS.", "POS", "ITEM", "NO.", "DN", "FLOW", "MIN.", "MAX."} {
		if text == token || strings.HasPrefix(text, token+" ") {
			return true
		}
	}
	return false
}

func cadTableKeyHeader(text string) bool {
	text = strings.ToUpper(strings.Join(strings.Fields(text), " "))
	for _, token := range []string{"DESIG", "DESIGNATION", "TAG NUMBER", "TAG", "POS.", "POS", "ITEM", "PART NUMBER", "PART NO.", "NO.", "NO"} {
		if text == token || strings.HasPrefix(text, token+" ") {
			return true
		}
	}
	return false
}

func cadExtractAreaRows(boxes []cadTextBox, scale float64) ([]cadTextBox, []cadTableRow) {
	titleIndex := -1
	for i, box := range boxes {
		if cadAreaTitle(box.item.text) {
			titleIndex = i
			break
		}
	}
	if titleIndex < 0 {
		return boxes, nil
	}
	titleBox := boxes[titleIndex]
	var candidates []int
	for i, box := range boxes {
		if i == titleIndex || box.item.y >= titleBox.maxY {
			continue
		}
		candidates = append(candidates, i)
	}
	sort.SliceStable(candidates, func(i, j int) bool { return boxes[candidates[i]].item.y > boxes[candidates[j]].item.y })
	if len(candidates) < 2 {
		return boxes, nil
	}
	lastY := boxes[candidates[0]].maxY
	var window []int
	for _, index := range candidates {
		box := boxes[index]
		if cadScaleBarText(box.item.text) && len(window) > 0 {
			break
		}
		if lastY-box.maxY > 3*scale && len(window) > 0 {
			break
		}
		window = append(window, index)
		lastY = math.Min(lastY, box.minY)
	}
	used := make(map[int]bool, len(window))
	var pairs [][2]int
	for wi, i := range window {
		label := boxes[i]
		if used[i] || cadIsValue(label.item.text) || !cadHasLetter(label.item.text) {
			continue
		}
		best := -1
		bestDistance := math.Inf(1)
		labelCenter := (label.minY + label.maxY) / 2
		for wj, j := range window {
			if wi == wj || used[j] {
				continue
			}
			value := boxes[j]
			if !cadIsValue(value.item.text) {
				continue
			}
			dx := math.Abs(value.minX - label.minX)
			dy := math.Abs((value.minY+value.maxY)/2 - labelCenter)
			if dx > 2*scale && !(dy <= 0.8*scale && value.minX > label.minX && value.minX-label.minX <= 40*scale) {
				continue
			}
			distance := dy + dx/10
			if distance < bestDistance {
				best, bestDistance = j, distance
			}
		}
		if best < 0 {
			continue
		}
		used[i], used[best] = true, true
		pairs = append(pairs, [2]int{i, best})
	}
	if len(pairs) < 2 {
		return boxes, nil
	}
	var rows []cadTableRow
	for _, pair := range pairs {
		label, value := boxes[pair[0]], boxes[pair[1]]
		rows = append(rows, cadTableRow{Cells: []cadTableRowCell{
			{Column: "ITEM", Text: strings.Join(strings.Fields(label.item.text), " ")},
			{Column: "AREA", Text: strings.Join(strings.Fields(value.item.text), " ")},
		}})
	}
	var rest []cadTextBox
	for i, box := range boxes {
		if !used[i] {
			rest = append(rest, box)
		}
	}
	return rest, rows
}

func cadAreaTitle(text string) bool {
	upper := strings.ToUpper(strings.Join(strings.Fields(text), " "))
	return strings.Contains(upper, "SUPERFICI UTILI") || strings.Contains(upper, "ROOM SCHEDULE") || strings.Contains(upper, "AREA SCHEDULE")
}

func cadScaleBarText(text string) bool {
	upper := strings.ToUpper(strings.Join(strings.Fields(text), " "))
	return strings.Contains(upper, "SCALA GRAFICA") || strings.Contains(upper, "GRAPHIC SCALE")
}

func cadHasLetter(text string) bool {
	for _, r := range text {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}
