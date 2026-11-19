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

package chunker

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"ragflow/internal/parser/cadsource"
)

var cadLegendTag = regexp.MustCompile(`^(?:[A-Z][A-Z0-9-]*\s*[- ]?\s*\d{1,4}[A-Z]?|\d{1,3}[A-Z]?)$`)

// cadLegendEntry is one structured legend row. RowText is the exact line the
// layout serializer emits for the row, so chunk matching does not depend on the
// pre-formatting tab layout. Sources and Bounds point back to the source texts.
type cadLegendEntry struct {
	Tag         string
	Description string
	RowText     string
	Sources     []cadSourceFact
	Bounds      [4]float64
}

// cadJoinLegendRows keeps the tag and description under explicit column
// headers together. The source identities and union of their boxes survive.
func cadJoinLegendRows(items []cadTextItem) ([]cadTextItem, map[string]cadLegendEntry) {
	used := make([]bool, len(items))
	var rows []cadTextItem
	legend := map[string]cadLegendEntry{}
	for _, header := range items {
		if strings.ToUpper(strings.TrimSpace(header.text)) != "DESCRIPTION" {
			continue
		}
		h := math.Max(header.height, 1)
		tagX := math.Inf(-1)
		for _, label := range items {
			if strings.Contains(strings.ToUpper(label.text), "TAG NUMBER") && label.x < header.x && math.Abs(label.y-header.y) <= h && label.x > tagX {
				tagX = label.x
			}
		}
		if math.IsInf(tagX, -1) {
			continue
		}
		for i, tag := range items {
			name := strings.Join(strings.Fields(tag.text), " ")
			if used[i] || !cadLegendTag.MatchString(name) || math.Abs(tag.x-tagX) > 2*h || tag.y >= header.y || tag.y < header.y-150*h {
				continue
			}
			best := -1
			distance := math.Inf(1)
			for j, description := range items {
				text := strings.TrimSpace(description.text)
				first, _ := utf8.DecodeRuneInString(text)
				if used[j] || j == i || math.Abs(description.x-header.x) > 2*h || !unicode.IsLetter(first) || cadMatchesAnyFieldLabel(text) {
					continue
				}
				dy := math.Abs(description.y - tag.y)
				if dy <= 0.65*h && dy < distance {
					best, distance = j, dy
				}
			}
			if best < 0 {
				continue
			}
			description := items[best]
			row := tag
			row.legendRow = true
			row.text = name + "\t" + description.text
			row.sourceIDs = append(append([]string(nil), tag.sourceIDs...), description.sourceIDs...)
			row.facts = append(append([]cadSourceFact(nil), tag.facts...), description.facts...)
			boxes := []cadTextBox{cadTextBoxFor(tag, h), cadTextBoxFor(description, h)}
			bounds := cadBoxesBounds(boxes)
			row.bounds = &bounds
			rows = append(rows, row)
			used[i], used[best] = true, true
			entry := cadLegendEntry{
				Tag:         name,
				Description: description.text,
				RowText: cadTableRowText(cadTableRow{Cells: []cadTableRowCell{
					{Column: "TAG NUMBER", Text: name},
					{Column: "DESCRIPTION", Text: strings.Join(strings.Fields(description.text), " ")},
				}}),
				Sources: cadItemFacts([]cadTextItem{tag, description}),
				Bounds:  [4]float64{bounds.minX, bounds.minY, bounds.maxX, bounds.maxY},
			}
			if previous, ok := legend[name]; !ok {
				legend[name] = entry
			} else if previous.Description != entry.Description {
				// Conflicting descriptions must not be propagated as fact.
				legend[name] = cadLegendEntry{Tag: name}
			}
		}
	}
	out := make([]cadTextItem, 0, len(items))
	for i, item := range items {
		if !used[i] {
			out = append(out, item)
		}
	}
	out = append(out, rows...)
	sortCADTextItems(out)
	return out, legend
}

func cadMatchesAnyFieldLabel(text string) bool {
	for _, label := range cadTitleBlockLabels {
		if cadMatchesFieldLabel(text, label) {
			return true
		}
	}
	return false
}

func cadAddLegendContext(chunks []cadChunk, legend map[string]cadLegendEntry) {
	keys := make([]string, 0, len(legend))
	descriptions := map[string]string{}
	conflicts := map[string]bool{}
	for tag := range legend {
		keys = append(keys, tag)
		canonical := strings.Join(strings.Fields(tag), "")
		description := legend[tag].Description
		if previous, ok := descriptions[canonical]; ok && previous != description {
			conflicts[canonical] = true
		}
		descriptions[canonical] = description
		if description == "" {
			conflicts[canonical] = true
		}
	}
	sort.Strings(keys)
	for i := range chunks {
		body := " " + strings.Join(strings.Fields(chunks[i].Content), " ") + " "
		wrapped := "\n" + chunks[i].Content + "\n"
		for _, tag := range keys {
			if conflicts[strings.Join(strings.Fields(tag), "")] {
				continue
			}
			entry := legend[tag]
			description := entry.Description
			ownRow := entry.RowText != "" && strings.Contains(wrapped, "\n"+entry.RowText+"\n")
			if description == "" || !(cadLegendMatchesBody(body, tag) || ownRow) {
				continue
			}
			chunks[i].Keywords = cadAppendKeywordAliases(chunks[i].Keywords, tag)
			if strings.Contains(body, strings.Join(strings.Fields(description), " ")) {
				continue
			}
			chunks[i].Context += "Legend: " + tag + " — " + description + "\n"
			chunks[i].ContextSources = cadAppendSourceFacts(chunks[i].ContextSources, entry.Sources)
		}
	}
}

// cadLegendMatchesBody matches complete equipment tag aliases. Bare numeric
// row identifiers require the joined legend row itself as evidence.
func cadLegendMatchesBody(body, tag string) bool {
	if _, err := strconv.Atoi(tag); err == nil {
		return false
	}
	for _, alias := range cadAppendKeywordAliases(nil, tag) {
		if strings.Contains(body, " "+alias+" ") {
			return true
		}
	}
	return false
}

// cadAppendKeywordAliases adds the original tag plus compact/spaced aliases,
// deduplicated. It keeps the original spelling so leading zeroes survive.
func cadAppendKeywordAliases(keywords []string, tag string) []string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return keywords
	}
	aliases := []string{tag}
	compact := strings.NewReplacer(" ", "", "\t", "").Replace(tag)
	if compact != tag {
		aliases = append(aliases, compact)
	}
	// PS147 / FC095 -> PS 147 / FC 095. Only split when the tag has letters
	// followed directly by digits; numeric suffixes such as 095A are untouched.
	for i := 1; i < len(tag); i++ {
		if (tag[i-1] >= 'A' && tag[i-1] <= 'Z') && tag[i] >= '0' && tag[i] <= '9' {
			spaced := tag[:i] + " " + tag[i:]
			if spaced != tag {
				aliases = append(aliases, spaced)
			}
			break
		}
	}
	seen := make(map[string]bool, len(keywords)+len(aliases))
	for _, keyword := range keywords {
		seen[keyword] = true
	}
	for _, alias := range aliases {
		if !seen[alias] {
			seen[alias] = true
			keywords = append(keywords, alias)
		}
	}
	return keywords
}

// cadAppendSourceFacts appends facts without duplicating IDs.
func cadAppendSourceFacts(dst, src []cadSourceFact) []cadSourceFact {
	seen := make(map[string]bool, len(dst)+len(src))
	for _, fact := range dst {
		seen[fact.ID] = true
	}
	for _, fact := range src {
		if fact.ID == "" || seen[fact.ID] {
			continue
		}
		seen[fact.ID] = true
		dst = append(dst, fact)
	}
	return dst
}

// cadExtractTitleFields finds local title rectangles inside a larger part.
// Each candidate needs multiple field names; geometry alone is insufficient.
func cadExtractTitleFields(items []cadTextItem, bounds cadBounds) ([]cadTextItem, [][]cadTextItem) {
	var anchors []cadTextItem
	for _, item := range items {
		if cadMatchesAnyFieldLabel(item.text) {
			anchors = append(anchors, item)
		}
	}
	var groups [][]cadTextItem
	for _, anchor := range anchors {
		target := -1
		for i, group := range groups {
			for _, other := range group {
				h := math.Max(anchor.height, other.height)
				if h > 0 && math.Abs(anchor.x-other.x) <= 50*h && math.Abs(anchor.y-other.y) <= 12*h {
					target = i
					break
				}
			}
			if target >= 0 {
				break
			}
		}
		if target < 0 {
			groups = append(groups, []cadTextItem{anchor})
		} else {
			groups[target] = append(groups[target], anchor)
		}
	}
	used := make([]bool, len(items))
	var titles [][]cadTextItem
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		boxes := make([]cadTextBox, len(group))
		for i, item := range group {
			boxes[i] = cadTextBoxFor(item, 1)
		}
		hits := cadTitleBlockLabelHits(boxes)
		if hits < 2 {
			continue
		}
		b := cadBoxesBounds(boxes)
		identified := false
		for _, item := range group {
			for _, label := range []string{"图号", "图名", "dwg no", "drawing no", "title", "tavola", "nama", "nim", "no. project"} {
				if cadMatchesFieldLabel(item.text, label) {
					identified = true
					break
				}
			}
		}
		// Generic fields such as MATERIAL/WEIGHT/DATE also occur in schedules.
		// Extra matches do not establish a title block outside the lower sheet.
		if !identified && b.maxY > bounds.minY+0.5*bounds.height() {
			continue
		}
		h := cadMedianItemHeight(group)
		if h <= 0 {
			continue
		}
		// Header text and paired values share the fields' local rectangle. These
		// margins are in text heights, so paper and model units stay independent.
		b.minX -= 3 * h
		b.maxX += 45 * h
		b.minY -= 10 * h
		topMargin := 22.0
		for _, anchor := range group {
			if cadMatchesFieldLabel(anchor.text, "nama") || cadMatchesFieldLabel(anchor.text, "nim") {
				topMargin = 8
				break
			}
		}
		b.maxY += topMargin * h
		var part []cadTextItem
		for i, item := range items {
			// Joined legend rows already have an explicit table relationship.
			if item.legendRow {
				continue
			}
			if !used[i] && item.x >= b.minX && item.x <= b.maxX && item.y >= b.minY && item.y <= b.maxY {
				part = append(part, item)
				used[i] = true
			}
		}
		if len(part) > 0 {
			titles = append(titles, part)
		}
	}
	var rest []cadTextItem
	for i, item := range items {
		if !used[i] {
			rest = append(rest, item)
		}
	}
	return rest, titles
}

func cadMedianItemHeight(items []cadTextItem) float64 {
	var heights []float64
	for _, item := range items {
		if item.height > 0 {
			heights = append(heights, item.height)
		}
	}
	return cadMedian(heights)
}

// cadFilterFrameGrid removes ordered sequences only along a detected frame's
// boundary. Fallback text extents do not prove that labels are a frame grid.
func cadFilterFrameGrid(items []cadTextItem, sheet cadsource.Sheet) ([]cadTextItem, int) {
	bounds := cadBounds{minX: sheet.Bounds[0], minY: sheet.Bounds[1], maxX: sheet.Bounds[2], maxY: sheet.Bounds[3]}
	if sheet.Index <= 0 || !bounds.valid() || (sheet.Source != "insert" && sheet.Source != "polyline" && sheet.Source != "lines") {
		return items, 0
	}
	removed := map[int]bool{}
	for _, vertical := range []bool{true, false} {
		axis := bounds.height()
		if vertical {
			axis = bounds.width()
		}
		var groups [][]int
		ranks := map[int]int{}
		for i, item := range items {
			if item.height <= 0 || item.kind == cadsource.KindDimension {
				continue
			}
			// Grid labels sit just inside the frame line, not necessarily on it.
			// A percentage of the sheet keeps this independent of text height,
			// while the height term covers very small drawings.
			margin := math.Min(math.Max(4*item.height, 0.04*axis), 0.05*axis)
			if item.x < bounds.minX-margin || item.x > bounds.maxX+margin || item.y < bounds.minY-margin || item.y > bounds.maxY+margin {
				continue
			}
			edge := math.Min(math.Abs(item.y-bounds.minY), math.Abs(item.y-bounds.maxY))
			if vertical {
				edge = math.Min(math.Abs(item.x-bounds.minX), math.Abs(item.x-bounds.maxX))
			}
			if edge > margin {
				continue
			}
			text := strings.TrimSpace(item.text)
			rank := 0
			if vertical {
				if len(text) == 1 && text[0] >= 'A' && text[0] <= 'H' {
					rank = int(text[0]-'A') + 1
				}
			} else if n, err := strconv.Atoi(text); err == nil && n >= 1 && n <= 12 && strconv.Itoa(n) == text {
				rank = n
			}
			if rank == 0 {
				continue
			}
			ranks[i] = rank
			target := -1
			for g, group := range groups {
				other := items[group[0]]
				distance := math.Abs(item.y - other.y)
				if vertical {
					distance = math.Abs(item.x - other.x)
				}
				if distance <= math.Max(0.7*math.Max(item.height, other.height), 0.005*axis) {
					target = g
					break
				}
			}
			if target < 0 {
				groups = append(groups, []int{i})
			} else {
				groups[target] = append(groups[target], i)
			}
		}
		for _, group := range groups {
			sort.SliceStable(group, func(i, j int) bool {
				if vertical {
					return items[group[i]].y > items[group[j]].y
				}
				return items[group[i]].x < items[group[j]].x
			})
			var unique []int
			valid := true
			for _, index := range group {
				if len(unique) > 0 {
					prev := unique[len(unique)-1]
					if ranks[index] == ranks[prev] {
						continue
					}
					// A missing grid label is common (cropped/blank sheet edge).
					// Require monotonic order, not a gap-free sequence.
					if ranks[index] < ranks[prev] {
						valid = false
						break
					}
				}
				unique = append(unique, index)
			}
			if !valid || len(unique) < 6 {
				continue
			}
			minGap, maxGap := math.Inf(1), 0.0
			for i := 1; i < len(unique); i++ {
				a, b := items[unique[i-1]], items[unique[i]]
				gap := math.Abs(a.x - b.x)
				if vertical {
					gap = math.Abs(a.y - b.y)
				}
				minGap = math.Min(minGap, gap)
				maxGap = math.Max(maxGap, gap)
			}
			if minGap <= 0 || maxGap > 3*minGap {
				continue
			}
			for _, index := range group {
				removed[index] = true
			}
		}
	}
	out := make([]cadTextItem, 0, len(items))
	for i, item := range items {
		if !removed[i] {
			out = append(out, item)
		}
	}
	return out, len(removed)
}

func cadTitleChunks(docName string, sheet cadsource.Sheet, parts [][]cadTextItem, page *cadInkPage, scale float64) []cadChunk {
	var out []cadChunk
	for i, part := range parts {
		boxes := make([]cadTextBox, len(part))
		for j, item := range part {
			boxes[j] = cadTextBoxFor(item, scale)
		}
		chunk := cadChunkFromBoxes(
			docName, sheet, fmt.Sprintf("T%d", i+1), cadKindTitleBlock,
			[]cadEvidence{{Level: cadEvidenceDerived, Source: "layout", Method: "title_block"}},
			boxes, cadLocalTextBounds(boxes, page),
		)
		chunk.Content = cadTitleContent(part)
		out = append(out, chunk)
	}
	return out
}

// cadTitlePairMaxDistance bounds a same-row value to the right of its label, in
// text heights. A wider gap belongs to another cell of the title-block grid.
const cadTitlePairMaxDistance = 15.0

// cadTitleContent pairs field labels with their nearest value. Title blocks
// are cell tables; global reading order loses label/value adjacency.
func cadTitleContent(items []cadTextItem) string {
	if len(items) == 0 {
		return ""
	}
	ordered := append([]cadTextItem(nil), items...)
	sortCADTextItems(ordered)
	height := cadMedianItemHeight(ordered)
	if height <= 0 {
		height = 1
	}
	rowTolerance := 0.8 * height
	var rows [][]int
	rowOf := make([]int, len(ordered))
	for i, item := range ordered {
		placed := false
		for r := range rows {
			ref := ordered[rows[r][0]]
			if math.Abs(item.y-ref.y) <= rowTolerance {
				rows[r] = append(rows[r], i)
				rowOf[i] = r
				placed = true
				break
			}
		}
		if !placed {
			rows = append(rows, []int{i})
			rowOf[i] = len(rows) - 1
		}
	}
	used := make([]bool, len(ordered))
	lines := make([]string, 0, len(ordered))
	rowHasLabel := make([]bool, len(rows))
	for r := range rows {
		for _, i := range rows[r] {
			if cadMatchesAnyFieldLabel(ordered[i].text) && !cadHasInlineFieldValue(ordered[i].text) {
				rowHasLabel[r] = true
				break
			}
		}
	}
	for r := range rows {
		// Pair labels before emitting sibling values: a value may sort above
		// its label by a few tenths of a text height.
		for _, i := range rows[r] {
			item := ordered[i]
			if used[i] || !cadMatchesAnyFieldLabel(item.text) || cadHasInlineFieldValue(item.text) {
				continue
			}
			pair := -1
			// A row with several field labels is a header row or a multi-cell
			// band (SCALA/FORMATO, DRAWN/DATE). Pairing across it would attach
			// the wrong value; stacked-cell pairing below handles those cases.
			labelCount := 0
			for _, j := range rows[rowOf[i]] {
				if cadMatchesAnyFieldLabel(ordered[j].text) {
					labelCount++
				}
			}
			if labelCount == 1 {
				bestX := math.Inf(1)
				for _, j := range rows[rowOf[i]] {
					if j == i || used[j] || cadMatchesAnyFieldLabel(ordered[j].text) {
						continue
					}
					if ordered[j].x <= item.x || ordered[j].x-item.x > cadTitlePairMaxDistance*height {
						continue
					}
					if ordered[j].x < bestX {
						bestX = ordered[j].x
						pair = j
					}
				}
			}
			if pair < 0 {
				pair = cadNearestTitleValue(ordered, used, i, height, false, rowOf, rowHasLabel)
				if pair < 0 {
					pair = cadNearestTitleValue(ordered, used, i, height, true, rowOf, rowHasLabel)
				}
			}
			if pair >= 0 {
				label := strings.TrimRight(strings.TrimSpace(item.text), ":：")
				lines = append(lines, label+": "+strings.TrimSpace(ordered[pair].text))
				used[i], used[pair] = true, true
			}
		}
		for _, i := range rows[r] {
			if used[i] {
				continue
			}
			lines = append(lines, cadNormalizeInlineTitleField(ordered[i].text))
			used[i] = true
		}
	}
	return strings.Join(lines, "\n")
}

// cadNearestTitleValue finds a non-label value in the same column, either below
// (above=false) or above the label. It is used for stacked cell layouts such as
// SCALA/FORMATO where the label sits above its value. A value row that also
// carries a field label belongs to that label, not to this one.
func cadNearestTitleValue(items []cadTextItem, used []bool, label int, height float64, above bool, rowOf []int, rowHasLabel []bool) int {
	best, bestDistance := -1, math.Inf(1)
	for j, other := range items {
		if j == label || used[j] || cadMatchesAnyFieldLabel(other.text) {
			continue
		}
		if rowHasLabel[rowOf[j]] {
			continue
		}
		if math.Abs(other.x-items[label].x) > 12*height {
			continue
		}
		var distance float64
		if above {
			if other.y <= items[label].y {
				continue
			}
			distance = other.y - items[label].y
		} else {
			if other.y >= items[label].y {
				continue
			}
			distance = items[label].y - other.y
		}
		if distance > 20*height {
			continue
		}
		if distance < bestDistance {
			best, bestDistance = j, distance
		}
	}
	return best
}

func cadHasInlineFieldValue(text string) bool {
	for _, sep := range []string{":", "："} {
		if _, after, ok := strings.Cut(text, sep); ok && strings.TrimSpace(after) != "" {
			return true
		}
	}
	return false
}
