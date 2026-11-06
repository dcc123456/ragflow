//go:build ee

package table

import (
	"math"
	"sort"

	pdf "ragflow/internal/deepdoc/parser/pdf/type"
)

// VHTableAssembler rebuilds a table grid from 2-class v/h TSR wire by
// edge-clustering, porting deepdoc/vision/tsr.py TableStructureRecognizer
// (0.26.4 production). It deliberately ignores the v/h class label (Python
// drops it too) and treats every wire box as a geometry edge whose x edges
// feed the column lines and y edges feed the row lines.
type VHTableAssembler struct{}

// vhLine is a clustered grid line position with the count of edges that
// contributed to it (Python row_lines[median] = len(avg)).
type vhLine struct {
	Pos   float64
	Count int
}

// Assemble ports tsr.py __call__: edge-cluster the wire boxes into column
// and row boundary lines, cross-multiply into a full virtual grid, suppress
// cells already covered by a real box, then count internal lines per cell to
// derive colspan/rowspan. imgH is the crop image height (tsr.py uses
// images[n].size[1] for the bottom-stitch compensation).
func (VHTableAssembler) Assemble(wire []pdf.TSRCell, imgH float64) []pdf.TSRCell {
	cells := make([]pdf.TSRCell, 0, len(wire))
	for _, c := range wire {
		if c.X1 > c.X0 && c.Y1 > c.Y0 {
			cells = append(cells, c)
		}
	}
	if len(cells) == 0 {
		return nil
	}
	// tsr.py: min_height = max(3, min(h)); min_width = max(5, min(w))
	mh, mw := math.MaxFloat64, math.MaxFloat64
	for _, c := range cells {
		if h := c.Y1 - c.Y0; h < mh {
			mh = h
		}
		if w := c.X1 - c.X0; w < mw {
			mw = w
		}
	}
	minHeight := math.Max(3.0, mh)
	minWidth := math.Max(5.0, mw)
	SortYFirstly(cells, minHeight/1.1)
	cells = vhMergeOverlapped(cells)
	SortXFirstly(cells, minWidth/1.1)
	cells = vhMergeOverlapped(cells)

	var hs, ws []float64
	for _, c := range cells {
		hs = append(hs, c.Y1-c.Y0)
		ws = append(ws, c.X1-c.X0)
	}
	meanHeight := median(hs)
	meanWidth := median(ws)
	mh, mw = math.MaxFloat64, math.MaxFloat64
	for _, h := range hs {
		if h < mh {
			mh = h
		}
	}
	for _, w := range ws {
		if w < mw {
			mw = w
		}
	}
	minHeight = math.Max(3.0, mh)
	minWidth = math.Max(5.0, mw)

	colLines := vhBorders(cells, 0, 2, meanWidth/3.0)
	rowLines := vhBorders(cells, 1, 3, meanHeight/3.0)
	if len(colLines) > 0 && colLines[0].Pos >= meanWidth {
		for i := range cells {
			if math.Abs(cells[i].X0-colLines[0].Pos) < minWidth/2.0 {
				cells[i].X0 = minWidth / 2.0
			}
		}
		colLines = vhBorders(cells, 0, 2, math.Min(5.0, meanWidth/5.0))
	}
	if len(rowLines) > 0 && imgH-rowLines[len(rowLines)-1].Pos > 2.0*meanHeight {
		rowLines = append(rowLines, vhLine{Pos: rowLines[len(rowLines)-1].Pos + meanHeight, Count: 1})
	}

	var virtuals []pdf.TSRCell
	for i := 0; i < len(rowLines)-1; i++ {
		for j := 0; j < len(colLines)-1; j++ {
			virtuals = append(virtuals, pdf.TSRCell{
				X0: colLines[j].Pos,
				X1: colLines[j+1].Pos,
				Y0: rowLines[i].Pos,
				Y1: rowLines[i+1].Pos,
			})
		}
	}
	for _, v := range virtuals {
		covered := false
		for _, c := range cells {
			if vhOverlapRatio(v, c) >= 0.3 {
				covered = true
				break
			}
		}
		if !covered {
			cells = append(cells, v)
		}
	}

	SortYFirstly(cells, minHeight*0.75)
	for i := range cells {
		if span := crossBoders(cells[i].X0, cells[i].X1, colLines, minWidth*0.8); span > 0 {
			cells[i].ColSpan = span + 1
		}
		if span := crossBoders(cells[i].Y0, cells[i].Y1, rowLines, meanHeight*0.8); span > 0 {
			cells[i].RowSpan = span + 1
		}
	}
	return cells
}

func vhMergeOverlapped(cells []pdf.TSRCell) []pdf.TSRCell {
	i := 0
	for i+1 < len(cells) {
		ov := math.Max(vhOverlapRatio(cells[i], cells[i+1]), vhOverlapRatio(cells[i+1], cells[i]))
		if ov < 0.3 {
			i++
			continue
		}
		cells[i].X0 = math.Min(cells[i+1].X0, cells[i].X0)
		cells[i].X1 = math.Max(cells[i+1].X1, cells[i].X1)
		cells[i].Y0 = math.Min(cells[i+1].Y0, cells[i].Y0)
		cells[i].Y1 = math.Max(cells[i+1].Y1, cells[i].Y1)
		cells[i].Score = math.Max(cells[i+1].Score, cells[i].Score)
		cells = append(cells[:i+1], cells[i+2:]...)
	}
	return cells
}

func vhBorders(cells []pdf.TSRCell, k1, k2 int, distance float64) []vhLine {
	var ys []float64
	for _, c := range cells {
		ys = append(ys, float64(int(coordOf(c, k1))), float64(int(coordOf(c, k2))))
	}
	sort.Float64s(ys)
	rowLines := map[float64]int{}
	if len(ys) == 0 {
		return nil
	}
	avg := []float64{ys[0]}
	for i := 1; i < len(ys); i++ {
		if ys[i]-median(avg) >= distance {
			y := median(avg)
			rowLines[y] = len(avg)
			avg = []float64{ys[i]}
		} else {
			avg = append(avg, ys[i])
		}
	}
	if len(avg) > 0 {
		rowLines[median(avg)] = len(avg)
	}
	lines := make([]vhLine, 0, len(rowLines))
	for p, c := range rowLines {
		lines = append(lines, vhLine{Pos: p, Count: c})
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].Pos < lines[j].Pos })
	return lines
}

func coordOf(c pdf.TSRCell, k int) float64 {
	switch k {
	case 0:
		return c.X0
	case 1:
		return c.Y0
	case 2:
		return c.X1
	default:
		return c.Y1
	}
}

func vhOverlapRatio(a, b pdf.TSRCell) float64 {
	if b.X0 > a.X1 || b.X1 < a.X0 || b.Y1 < a.Y0 || b.Y0 > a.Y1 {
		return 0
	}
	x0 := math.Max(b.X0, a.X0)
	x1 := math.Min(b.X1, a.X1)
	y0 := math.Max(b.Y0, a.Y0)
	y1 := math.Min(b.Y1, a.Y1)
	area := math.Max(0, x1-x0) * math.Max(0, y1-y0)
	base := math.Max((a.X1-a.X0)*(a.Y1-a.Y0), 1e-6)
	return area / base
}

func crossBoders(left, right float64, lines []vhLine, thr float64) int {
	c := 0
	for _, l := range lines {
		b := l.Pos
		if b <= left || b >= right {
			continue
		}
		if math.Min(math.Abs(b-left), math.Abs(b-right)) < thr {
			continue
		}
		c++
	}
	return c
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2.0
}
