//go:build ee

package table

import (
	"strings"
	"testing"

	pdf "ragflow/internal/deepdoc/parser/pdf/type"
)

// buildEdgePhantomGrid returns a rows x cols grid where every cell in the
// given emptyCols set is left without text (a phantom sub-band), mimicking
// what VHTableAssembler.ExpandGrid produces when a stray TSR vertical line
// splits a column. Coordinates are grid-line aligned so the streaming
// renderer can recover rows from geometry.
func buildEdgePhantomGrid(rows, cols int, emptyCols map[int]bool) [][]pdf.TSRCell {
	xs := make([]float64, cols+1)
	ys := make([]float64, rows+1)
	for c := 0; c <= cols; c++ {
		xs[c] = float64(c * 100)
	}
	for r := 0; r <= rows; r++ {
		ys[r] = float64(r * 50)
	}
	grid := make([][]pdf.TSRCell, rows)
	letters := "ABCDEFGHIJKLMNOP"
	k := 0
	for r := 0; r < rows; r++ {
		grid[r] = make([]pdf.TSRCell, cols)
		for c := 0; c < cols; c++ {
			text := ""
			if !emptyCols[c] {
				text = string(letters[k%len(letters)])
				k++
			}
			grid[r][c] = pdf.TSRCell{
				X0:      xs[c],
				Y0:      ys[r],
				X1:      xs[c+1],
				Y1:      ys[r+1],
				Text:    text,
				ColSpan: 1,
				RowSpan: 1,
			}
		}
	}
	return grid
}

// TestReproLastColumnPhantom: an empty LAST column (edge phantom) must be
// removed, not rendered as a collapsed empty <td>.
func TestReproLastColumnPhantom(t *testing.T) {
	grid := buildEdgePhantomGrid(3, 4, map[int]bool{3: true}) // col 3 (last) empty
	item := &pdf.TableItem{RenderVH: true, Grid: grid, GridUnified: true}
	out := ConstructTable(nil, nil, "", item)
	t.Logf("HTML: %s", out)

	if !strings.Contains(out, "<table>") {
		t.Fatalf("no <table> produced: %q", out)
	}
	rows := strings.Split(out, "</tr>")
	var perRow []int
	for _, tr := range rows {
		perRow = append(perRow, strings.Count(tr, "<td"))
	}
	t.Logf("td-per-segment=%v", perRow)
	for _, n := range perRow {
		if n != 0 && n != 3 {
			t.Errorf("expected 3 cells per row, got %d (html=%s)", n, out)
		}
	}
	if !strings.Contains(out, ">C<") || !strings.Contains(out, ">I<") {
		t.Errorf("trailing content column missing: %s", out)
	}
	if strings.Contains(out, "<td></td>") {
		t.Errorf("empty trailing td still present (collapse not fixed): %s", out)
	}
}

// TestReproInteriorPhantom: an empty INTERIOR column must also be removed.
func TestReproInteriorPhantom(t *testing.T) {
	grid := buildEdgePhantomGrid(3, 4, map[int]bool{1: true}) // col 1 empty
	item := &pdf.TableItem{RenderVH: true, Grid: grid, GridUnified: true}
	out := ConstructTable(nil, nil, "", item)
	t.Logf("HTML: %s", out)
	if strings.Contains(out, "<td></td>") {
		t.Errorf("empty interior td still present: %s", out)
	}
	if !strings.Contains(out, ">A<") || !strings.Contains(out, ">C<") {
		t.Errorf("columns around dropped interior column wrong: %s", out)
	}
}
