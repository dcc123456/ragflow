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
