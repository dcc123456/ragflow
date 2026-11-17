//go:build ee

package table

import (
	"html"
	"regexp"
	"strings"
	"testing"

	pdf "ragflow/internal/deepdoc/parser/pdf/type"
)

var vhPagedTdRe = regexp.MustCompile(`(?s)<td[^>]*>(.*?)</td>`)

func vhPagedPlain(s string) string {
	return strings.TrimSpace(html.UnescapeString(vhPagedTdRe.ReplaceAllString(s, "$1")))
}

// TestConstructTableVHPagedPerPageOverlay is the TDD RED/GREEN guard for the
// cross-page V/H row-collapse bug. Two pages have cells in their OWN crop frame
// (Y starting at 0 on each page, exactly what Assemble emits per crop) and each
// page carries its own text boxes. Contract:
//   - a box is overlaid only onto a cell of its OWN page (no cross-page match);
//   - each page starts a fresh <tr> (Python construct_table's per-pair loop).
//
// A flat single-frame overlay collapses the pages' identical coordinates and
// yields "101 127" / "序号 序号"; the per-page renderer does not.
func TestConstructTableVHPagedPerPageOverlay(t *testing.T) {
	cells := []pdf.TSRCell{
		// page 0
		{X0: 0, Y0: 0, X1: 40, Y1: 10},
		{X0: 40, Y0: 0, X1: 100, Y1: 10},
		{X0: 0, Y0: 10, X1: 40, Y1: 20},
		{X0: 40, Y0: 10, X1: 100, Y1: 20},
		// page 1 (identical crop-space geometry)
		{X0: 0, Y0: 0, X1: 40, Y1: 10},
		{X0: 40, Y0: 0, X1: 100, Y1: 10},
		{X0: 0, Y0: 10, X1: 40, Y1: 20},
		{X0: 40, Y0: 10, X1: 100, Y1: 20},
	}
	spans := []pdf.VHPageSpan{
		{Page: 0, CellStart: 0, CellEnd: 4, Scale: 1, CropOffX: 0, CropOffY: 0},
		{Page: 1, CellStart: 4, CellEnd: 8, Scale: 1, CropOffX: 0, CropOffY: 0},
	}
	boxes := []pdf.TextBox{
		{PageNumber: 0, HasPageNumber: true, X0: 0, Top: 0, X1: 40, Bottom: 10, Text: "序号"},
		{PageNumber: 0, HasPageNumber: true, X0: 0, Top: 11, X1: 40, Bottom: 19, Text: "101"},
		{PageNumber: 1, HasPageNumber: true, X0: 0, Top: 0, X1: 40, Bottom: 10, Text: "序号"},
		{PageNumber: 1, HasPageNumber: true, X0: 0, Top: 11, X1: 40, Bottom: 19, Text: "127"},
	}

	html := ConstructTableVHPaged(cells, boxes, spans, "")

	for _, want := range []string{"101", "127"} {
		if !vhPagedCellHas(html, want) {
			t.Errorf("expected a cell carrying %q, got: %s", want, html)
		}
	}
	if strings.Contains(html, "101 127") || strings.Contains(html, "127 101") {
		t.Errorf("cross-page boxes collapsed into one cell: %s", html)
	}
	if strings.Contains(html, "序号 序号") || strings.Contains(html, "序号序号") {
		t.Errorf("two pages' headers collapsed into one cell: %s", html)
	}
	if strings.Count(html, "<tr>") < 2 {
		t.Errorf("expected a fresh <tr> per page, got %d <tr>: %s", strings.Count(html, "<tr>"), html)
	}
}

func vhPagedCellHas(htmlStr, want string) bool {
	for _, m := range vhPagedTdRe.FindAllStringSubmatch(htmlStr, -1) {
		if strings.Contains(vhPagedPlain(m[1]), want) {
			return true
		}
	}
	return false
}
