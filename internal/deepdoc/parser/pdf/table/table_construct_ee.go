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

package table

import (
	"fmt"
	"html"
	"math"
	"regexp"
	"strings"

	pdf "ragflow/internal/deepdoc/parser/pdf/type"
)

// vhCellTextTailRE mirrors tsr.py construct_table's rule for inserting a
// space before appending a box's text to a cell that already carries text:
// Python adds a space when the existing text ends with >=2 letters/punct.
var vhCellTextTailRE = regexp.MustCompile(`[a-zA-Z,:;'!.]{2,}$`)

// ConstructTableVH renders the EE V/H table to HTML by replicating the
// 0.26.4 deepdoc/vision/tsr.py construct_table algorithm, so its output is
// equivalent (after attribute parsing) to the Python EE reference. It
// intentionally does NOT use the OSS dense-grid renderer (ExpandGrid +
// ToTableData + RenderTableHTML): Python lays cells out by streaming them in
// Assemble order and breaking rows on geometry, producing a ragged table with
// no empty placeholder cells. The dense renderer would inflate real tables
// (whose line edges jitter by a pixel or two) into a much larger fixed-width
// grid that cannot be made structurally equal to Python's output.
//
// cells is the flat assembled cell list (one entry per origin position, with
// ColSpan/RowSpan set for merged cells) -- i.e. pdf.TableItem.Cells for the
// VH builder. boxes carries the OCR/synthetic text to overlay.
func ConstructTableVH(cells []pdf.TSRCell, boxes []pdf.TextBox, caption string) string {
	if len(cells) == 0 {
		return ""
	}
	work := make([]pdf.TSRCell, len(cells))
	for i := range cells {
		work[i] = cells[i]
	}
	// Mirror tsr.py: match each text box to the cell with the largest
	// overlap (max of overlap-area/box-area and overlap-area/cell-area),
	// threshold 0.85, and append the box text to that cell.
	for _, b := range boxes {
		if strings.TrimSpace(b.Text) == "" {
			continue
		}
		bx := pdf.TSRCell{X0: b.X0, Y0: b.Top, X1: b.X1, Y1: b.Bottom}
		best := -1
		bestOv := 0.85
		for i := range work {
			if ov := vhBoxCellOverlap(bx, work[i]); ov >= bestOv {
				best = i
				bestOv = ov
			}
		}
		if best < 0 {
			continue
		}
		if work[best].Text != "" && vhCellTextTailRE.MatchString(work[best].Text) {
			work[best].Text += " "
		}
		work[best].Text += b.Text
	}

	var sb strings.Builder
	sb.WriteString("<table>")
	if caption != "" {
		sb.WriteString("<caption>")
		sb.WriteString(html.EscapeString(caption))
		sb.WriteString("</caption>")
	}
	sb.WriteString("<tr>")
	for j := range work {
		c := work[j]
		if j > 0 {
			prev := work[j-1]
			if c.Y0 >= prev.Y1-2 || c.X1-2 <= prev.X0 {
				sb.WriteString("</tr><tr>")
			}
		}
		sb.WriteString("<td")
		if c.ColSpan > 1 {
			sb.WriteString(fmt.Sprintf(" colspan=%d", c.ColSpan))
		}
		if c.RowSpan > 1 {
			sb.WriteString(fmt.Sprintf(" rowspan=%d", c.RowSpan))
		}
		sb.WriteString(">")
		sb.WriteString(html.EscapeString(c.Text))
		sb.WriteString("</td>")
	}
	sb.WriteString("</tr></table>")
	return sb.String()
}

// vhBoxCellOverlap mirrors Recognizer.find_overlapped_with_threshold:
// the larger of (intersection / box area) and (intersection / cell area).
func vhBoxCellOverlap(box, cell pdf.TSRCell) float64 {
	if cell.X0 > box.X1 || cell.X1 < box.X0 || cell.Y1 < box.Y0 || cell.Y0 > box.Y1 {
		return 0
	}
	x0 := math.Max(cell.X0, box.X0)
	x1 := math.Min(cell.X1, box.X1)
	y0 := math.Max(cell.Y0, box.Y0)
	y1 := math.Min(cell.Y1, box.Y1)
	inter := math.Max(0, x1-x0) * math.Max(0, y1-y0)
	if inter <= 0 {
		return 0
	}
	boxArea := math.Max((box.X1-box.X0)*(box.Y1-box.Y0), 1e-6)
	cellArea := math.Max((cell.X1-cell.X0)*(cell.Y1-cell.Y0), 1e-6)
	return math.Max(inter/boxArea, inter/cellArea)
}

// vhOverlayCells overlays page boxes onto a copy of the cells using the same
// 0.85 max-overlap rule as ConstructTableVH. Kept as a separate step so the
// paged renderer can inspect overlaid row text before rendering.
func vhOverlayCells(cells []pdf.TSRCell, boxes []pdf.TextBox) []pdf.TSRCell {
	work := make([]pdf.TSRCell, len(cells))
	copy(work, cells)
	for _, b := range boxes {
		if strings.TrimSpace(b.Text) == "" {
			continue
		}
		bx := pdf.TSRCell{X0: b.X0, Y0: b.Top, X1: b.X1, Y1: b.Bottom}
		best, bestOv := -1, 0.85
		for i := range work {
			if ov := vhBoxCellOverlap(bx, work[i]); ov >= bestOv {
				best, bestOv = i, ov
			}
		}
		if best < 0 {
			continue
		}
		if work[best].Text != "" && vhCellTextTailRE.MatchString(work[best].Text) {
			work[best].Text += " "
		}
		work[best].Text += b.Text
	}
	return work
}

// vhSplitRows groups overlaid cells into logical rows using the exact geometry
// rule the renderer uses to break <tr>: a new row starts when the cell's top is
// at/past the previous cell's bottom (minus 2px) or its left is at/past the
// previous cell's right (minus 2px).
func vhSplitRows(work []pdf.TSRCell) [][]pdf.TSRCell {
	if len(work) == 0 {
		return nil
	}
	rows := [][]pdf.TSRCell{{work[0]}}
	for j := 1; j < len(work); j++ {
		c, prev := work[j], work[j-1]
		if c.Y0 >= prev.Y1-2 || c.X1-2 <= prev.X0 {
			rows = append(rows, []pdf.TSRCell{c})
		} else {
			rows[len(rows)-1] = append(rows[len(rows)-1], c)
		}
	}
	return rows
}

// vhRowSig returns a normalized text signature for a logical row: each cell's
// text normalized with normalizeTableHeaderCell (same as tableHeaderOf) and
// joined with a unit separator so two rows with different cell boundaries
// never collide (e.g. ["序号","学校名称"] vs ["序号学校","名称"]). Reusing the
// shared normalizer keeps case/whitespace tolerance identical to the grid-based
// de-dup path. Comparing signatures (not <th>/<td> tags) is essential because a
// continuation header is rendered as plain <td>.
func vhRowSig(row []pdf.TSRCell) string {
	parts := make([]string, len(row))
	for i, c := range row {
		parts[i] = normalizeTableHeaderCell(c.Text)
	}
	return strings.Join(parts, "\x1f")
}

// vhRowHasText reports whether a row carries any non-empty cell text. An empty
// leading row (e.g. a spacer) must not become a header signature.
func vhRowHasText(row []pdf.TSRCell) bool {
	for _, c := range row {
		if strings.TrimSpace(c.Text) != "" {
			return true
		}
	}
	return false
}

// vhRenderRows emits <tr>/<td> for pre-split rows using the same geometry and
// attributes as the original single-pass renderer.
func vhRenderRows(rows [][]pdf.TSRCell) string {
	if len(rows) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<tr>")
	for ri, row := range rows {
		if ri > 0 {
			sb.WriteString("</tr><tr>")
		}
		for _, c := range row {
			sb.WriteString("<td")
			if c.ColSpan > 1 {
				sb.WriteString(fmt.Sprintf(" colspan=%d", c.ColSpan))
			}
			if c.RowSpan > 1 {
				sb.WriteString(fmt.Sprintf(" rowspan=%d", c.RowSpan))
			}
			sb.WriteString(">")
			sb.WriteString(html.EscapeString(c.Text))
			sb.WriteString("</td>")
		}
	}
	sb.WriteString("</tr>")
	return sb.String()
}

// ConstructTableVHPaged renders a cross-page V/H table by processing each page
// independently, mirroring Python's deepdoc/vision/tsr.py construct_table loop:
// for each (img, cells) pair, overlay ONLY that page's boxes onto that page's
// cells, then emit a fresh <tr> and break rows on geometry. This keeps the two
// pages' identical crop-space coordinates from collapsing into one row (the
// "101 127" / "序号 序号" bug) without any cross-page Y translation.
//
// The EE V/H builder renders its own HTML and carries no grid/HeaderRows, so
// the shared cross-page header de-dup (dropTableHeaderRows) never fires on this
// path. Instead, each continuation page whose leading row's text signature is
// identical to the home page's leading row (its header) is dropped before
// rendering. Only leading rows on continuation pages are ever removed; the
// home header and all data rows are untouched. Page-less boxes are offered to
// every page so callers without page metadata still overlay correctly.
func ConstructTableVHPaged(cells []pdf.TSRCell, boxes []pdf.TextBox, spans []pdf.VHPageSpan, caption string) string {
	var valid []pdf.VHPageSpan
	for _, sp := range spans {
		if sp.CellStart >= 0 && sp.CellEnd > sp.CellStart && sp.CellEnd <= len(cells) {
			valid = append(valid, sp)
		}
	}
	if len(valid) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<table>")
	if caption != "" {
		sb.WriteString("<caption>")
		sb.WriteString(html.EscapeString(caption))
		sb.WriteString("</caption>")
	}
	// Leading row signatures of the home page. A continuation page repeats some
	// number of leading header rows (one for a single-row header, N for an N-row
	// header); stripping the longest matching prefix removes all of them while
	// preserving any row that genuinely differs.
	var homeHeaderSigs []string
	for i, sp := range valid {
		pageCells := make([]pdf.TSRCell, sp.CellEnd-sp.CellStart)
		copy(pageCells, cells[sp.CellStart:sp.CellEnd])
		// Map this page's boxes into the same crop-pixel frame as its cells.
		var pageBoxes []pdf.TextBox
		for _, b := range boxes {
			if b.HasPageNumber && b.PageNumber != sp.Page {
				continue
			}
			pageBoxes = append(pageBoxes, BoxToCropSpace(b, sp.Scale, sp.CropOffX, sp.CropOffY))
		}
		work := vhOverlayCells(pageCells, pageBoxes)
		rows := vhSplitRows(work)
		if i == 0 {
			if len(rows) > 0 && vhRowHasText(rows[0]) {
				homeHeaderSigs = make([]string, len(rows))
				for k, row := range rows {
					homeHeaderSigs[k] = vhRowSig(row)
				}
			}
		} else if len(homeHeaderSigs) > 0 && len(rows) > 0 {
			strip := 0
			for strip < len(rows) && strip < len(homeHeaderSigs) {
				if vhRowSig(rows[strip]) != homeHeaderSigs[strip] {
					break
				}
				strip++
			}
			if strip > 0 {
				rows = rows[strip:]
			}
		}
		sb.WriteString(vhRenderRows(rows))
	}
	sb.WriteString("</table>")
	return sb.String()
}
