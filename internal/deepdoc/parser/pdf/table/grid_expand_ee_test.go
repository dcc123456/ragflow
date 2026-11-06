//go:build ee

package table

import (
	"fmt"
	"testing"

	pdf "ragflow/internal/deepdoc/parser/pdf/type"
)

func setText(cells []pdf.TSRCell) {
	for i := range cells {
		cells[i].Text = fmt.Sprintf("c%d", i)
	}
}

// TestExpandGridRowSpan feeds the row-spanning assembly through ExpandGrid
// and verifies the 2D grid, covered set, spanInfo, and the real downstream
// ToTableData round-trip all agree: a cell merged across two rows at (0,0)
// must produce RowSpan=2 and a Covered marker at (1,0).
func TestExpandGridRowSpan(t *testing.T) {
	wire := []pdf.TSRCell{
		mkCell(0, 0, 100, 100, 0.9),     // merged across rows 0-1, col 0
		mkCell(100, 0, 200, 50, 0.9),    // col 1, row 0
		mkCell(100, 50, 200, 100, 0.9),  // col 1, row 1
		mkCell(0, 100, 100, 150, 0.9),   // col 0, row 2
		mkCell(100, 100, 200, 150, 0.9), // col 1, row 2
	}
	flat := VHTableAssembler{}.Assemble(wire, 150)
	setText(flat)
	grid, covered, spanInfo := VHTableAssembler{}.ExpandGrid(flat)
	if len(grid) != 3 || len(grid[0]) != 2 {
		t.Fatalf("rowspan: grid dims got %dx%d want 3x2", len(grid), len(grid[0]))
	}
	if !covered[[2]int{1, 0}] {
		t.Errorf("rowspan: expected (1,0) covered by the 2-row span")
	}
	if s := spanInfo[[2]int{0, 0}]; s != [2]int{1, 2} {
		t.Errorf("rowspan: spanInfo[0,0] got %v want [1 2]", s)
	}
	td := ToTableData(grid, "", map[int]bool{}, spanInfo, covered)
	if len(td.Grid) != 3 || len(td.Grid[0]) != 2 {
		t.Fatalf("rowspan: entity grid dims got %dx%d want 3x2", len(td.Grid), len(td.Grid[0]))
	}
	if td.Grid[0][0].RowSpan != 2 {
		t.Errorf("rowspan: entity origin RowSpan got %d want 2", td.Grid[0][0].RowSpan)
	}
	if !td.Grid[1][0].Covered {
		t.Errorf("rowspan: entity (1,0) should be a Covered marker")
	}
}

// TestExpandGridColSpan mirrors the column direction: a cell merged across
// two columns at (0,0) must produce ColSpan=2 and a Covered marker at (0,1).
func TestExpandGridColSpan(t *testing.T) {
	wire := []pdf.TSRCell{
		mkCell(0, 0, 200, 50, 0.9),     // merged across cols 0-1, row 0
		mkCell(200, 0, 300, 50, 0.9),   // col 2, row 0
		mkCell(0, 50, 100, 100, 0.9),   // col 0, row 1
		mkCell(100, 50, 200, 100, 0.9), // col 1, row 1
		mkCell(200, 50, 300, 100, 0.9), // col 2, row 1
	}
	flat := VHTableAssembler{}.Assemble(wire, 100)
	setText(flat)
	grid, covered, spanInfo := VHTableAssembler{}.ExpandGrid(flat)
	if len(grid) != 2 || len(grid[0]) != 3 {
		t.Fatalf("colspan: grid dims got %dx%d want 2x3", len(grid), len(grid[0]))
	}
	if !covered[[2]int{0, 1}] {
		t.Errorf("colspan: expected (0,1) covered by the 2-col span")
	}
	if s := spanInfo[[2]int{0, 0}]; s != [2]int{2, 1} {
		t.Errorf("colspan: spanInfo[0,0] got %v want [2 1]", s)
	}
	td := ToTableData(grid, "", map[int]bool{}, spanInfo, covered)
	if td.Grid[0][0].ColSpan != 2 {
		t.Errorf("colspan: entity origin ColSpan got %d want 2", td.Grid[0][0].ColSpan)
	}
	if !td.Grid[0][1].Covered {
		t.Errorf("colspan: entity (0,1) should be a Covered marker")
	}
}

// TestExpandGridEmpty confirms the empty-input contract matches GroupCells
// (nil grid, nil maps) so callers can treat EE and OSS uniformly.
func TestExpandGridEmpty(t *testing.T) {
	grid, covered, spanInfo := VHTableAssembler{}.ExpandGrid(nil)
	if grid != nil || covered != nil || spanInfo != nil {
		t.Errorf("empty: expected all nil, got grid=%v covered=%v spanInfo=%v", grid, covered, spanInfo)
	}
}
