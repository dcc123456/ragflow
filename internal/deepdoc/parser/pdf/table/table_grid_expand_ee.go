//go:build ee

package table

import (
	"sort"
	"strings"

	pdf "ragflow/internal/deepdoc/parser/pdf/type"
)

// ExpandGrid turns the VHTableAssembler's flat origin-cell list into the
// 2D grid + covered/spanInfo maps that ToTableData consumes, so the EE path
// reuses the exact same downstream as the OSS GroupCells path (single
// canonical renderer, no second code path).
//
// The assembler's flat list already holds exactly one entry per grid
// position: a merged cell appears once with ColSpan/RowSpan set, and every
// other position is a plain cell. Because the row/column grid lines are
// exactly the union of the cells' own edges, ExpandGrid recovers the line
// arrays directly from the input and needs no extra geometry.
//
// covered marks every position an origin's span engulfs; spanInfo records
// each origin's [colspan, rowspan]. Both are keyed by [row, col].
func (VHTableAssembler) ExpandGrid(cells []pdf.TSRCell) ([][]pdf.TSRCell, map[[2]int]bool, map[[2]int][2]int) {
	if len(cells) == 0 {
		return nil, nil, nil
	}

	var ys, xs []float64
	ySeen, xSeen := map[float64]bool{}, map[float64]bool{}
	for _, c := range cells {
		if !ySeen[c.Y0] {
			ySeen[c.Y0] = true
			ys = append(ys, c.Y0)
		}
		if !ySeen[c.Y1] {
			ySeen[c.Y1] = true
			ys = append(ys, c.Y1)
		}
		if !xSeen[c.X0] {
			xSeen[c.X0] = true
			xs = append(xs, c.X0)
		}
		if !xSeen[c.X1] {
			xSeen[c.X1] = true
			xs = append(xs, c.X1)
		}
	}
	sort.Float64s(ys)
	sort.Float64s(xs)
	yIdx := make(map[float64]int, len(ys))
	for i, v := range ys {
		yIdx[v] = i
	}
	xIdx := make(map[float64]int, len(xs))
	for i, v := range xs {
		xIdx[v] = i
	}
	rows, cols := len(ys)-1, len(xs)-1

	grid := make([][]pdf.TSRCell, rows)
	for r := range grid {
		grid[r] = make([]pdf.TSRCell, cols)
		for c := range grid[r] {
			grid[r][c] = pdf.TSRCell{X0: xs[c], Y0: ys[r], X1: xs[c+1], Y1: ys[r+1]}
		}
	}

	covered := map[[2]int]bool{}
	spanInfo := map[[2]int][2]int{}

	for _, c := range cells {
		r, ok := yIdx[c.Y0]
		if !ok {
			continue
		}
		cc, ok := xIdx[c.X0]
		if !ok {
			continue
		}
		cs := c.ColSpan
		if cs < 1 {
			cs = 1
		}
		rs := c.RowSpan
		if rs < 1 {
			rs = 1
		}
		grid[r][cc] = pdf.TSRCell{
			X0:      xs[cc],
			Y0:      ys[r],
			X1:      xs[cc+1],
			Y1:      ys[r+1],
			Text:    c.Text,
			Label:   c.Label,
			Score:   c.Score,
			ColSpan: c.ColSpan,
			RowSpan: c.RowSpan,
		}
		spanInfo[[2]int{r, cc}] = [2]int{cs, rs}
		for dr := 0; dr < rs; dr++ {
			for dc := 0; dc < cs; dc++ {
				if dr == 0 && dc == 0 {
					continue
				}
				if r+dr < rows && cc+dc < cols {
					covered[[2]int{r + dr, cc + dc}] = true
				}
			}
		}
	}
	return grid, covered, spanInfo
}

// OverlayOCRText returns a copy of grid with OCR text overlaid from boxes.
//
// boxes must already carry R/C annotations (from AnnotateBoxesWithGrid on the
// same grid), so each box's text lands in grid[b.R][b.C] - the same
// coordinate frame the span geometry came from, which keeps the overlaid grid
// coordinate-consistent with spanInfo/covered. A box whose target slot is
// covered (it sits inside a merged region but not on the origin) is redirected
// to the spanning origin via spanInfo, so text only ever lands on origin
// cells - exactly how ToTableData renders (covered slots carry no text).
//
// Multiple boxes mapping to one cell are joined with a single space in reading
// order (Top, then X0) for deterministic output.
func OverlayOCRText(grid [][]pdf.TSRCell, boxes []pdf.TextBox, spanInfo map[[2]int][2]int, covered map[[2]int]bool) [][]pdf.TSRCell {
	if len(grid) == 0 {
		return grid
	}
	out := make([][]pdf.TSRCell, len(grid))
	for r := range grid {
		out[r] = make([]pdf.TSRCell, len(grid[r]))
		copy(out[r], grid[r])
	}

	originOf := make(map[[2]int][2]int)
	for r, row := range grid {
		for c := range row {
			if covered[[2]int{r, c}] {
				continue
			}
			cs, rs := 1, 1
			if s, ok := spanInfo[[2]int{r, c}]; ok {
				cs, rs = s[0], s[1]
			}
			for dr := 0; dr < rs; dr++ {
				for dc := 0; dc < cs; dc++ {
					if dr == 0 && dc == 0 {
						continue
					}
					originOf[[2]int{r + dr, c + dc}] = [2]int{r, c}
				}
			}
		}
	}

	order := make([]int, len(boxes))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool {
		if boxes[order[a]].Top != boxes[order[b]].Top {
			return boxes[order[a]].Top < boxes[order[b]].Top
		}
		return boxes[order[a]].X0 < boxes[order[b]].X0
	})

	accum := make(map[[2]int][]string)
	for _, i := range order {
		b := boxes[i]
		if b.R < 0 || b.C < 0 || b.R >= len(out) || b.C >= len(out[b.R]) {
			continue
		}
		r, c := b.R, b.C
		if covered[[2]int{r, c}] {
			if o, ok := originOf[[2]int{r, c}]; ok {
				r, c = o[0], o[1]
			} else {
				continue
			}
		}
		if strings.TrimSpace(b.Text) == "" {
			continue
		}
		k := [2]int{r, c}
		accum[k] = append(accum[k], b.Text)
	}
	for k, texts := range accum {
		out[k[0]][k[1]].Text = strings.Join(texts, " ")
	}
	return out
}
