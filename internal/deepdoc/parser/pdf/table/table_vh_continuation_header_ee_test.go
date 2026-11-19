//go:build ee

package table

import (
	"html"
	"regexp"
	"strings"
	"testing"

	pdf "ragflow/internal/deepdoc/parser/pdf/type"
)

var (
	vhChTrRe = regexp.MustCompile("(?s)<tr>(.*?)</tr>")
	vhChTdRe = regexp.MustCompile("(?s)<td[^>]*>(.*?)</td>")
)

// vhChRows parses rendered HTML into logical rows of plain cell text.
func vhChRows(s string) [][]string {
	var rows [][]string
	for _, rm := range vhChTrRe.FindAllStringSubmatch(s, -1) {
		var cells []string
		for _, cm := range vhChTdRe.FindAllStringSubmatch(rm[1], -1) {
			cells = append(cells, strings.TrimSpace(html.UnescapeString(cm[1])))
		}
		rows = append(rows, cells)
	}
	return rows
}

// vhChSig joins cell text with the same unit separator the production row
// signature uses, so an exact-text assertion also encodes cell boundaries.
func vhChSig(cells []string) string {
	parts := make([]string, len(cells))
	for i, c := range cells {
		parts[i] = strings.TrimSpace(c)
	}
	return strings.Join(parts, "\x1f")
}

func vhChCountRows(rows [][]string, sig string) int {
	n := 0
	for _, r := range rows {
		if vhChSig(r) == sig {
			n++
		}
	}
	return n
}

func vhChHasCell(rows [][]string, want string) bool {
	for _, r := range rows {
		for _, c := range r {
			if c == want {
				return true
			}
		}
	}
	return false
}

// vhChBuildPage builds one page of cells+boxes. Each row's cells sit at y=ri*10
// .. +10, each column at x=ci*20 .. +20; a box exactly covers its cell so the
// 0.85 overlay threshold is met deterministically. Boxes carry page metadata
// so ConstructTableVHPaged offers them only to their own page.
func vhChBuildPage(page int, rows ...[]string) ([]pdf.TSRCell, []pdf.TextBox) {
	var cells []pdf.TSRCell
	var boxes []pdf.TextBox
	for ri, texts := range rows {
		y0 := float64(ri * 10)
		y1 := y0 + 10
		for ci, txt := range texts {
			x0 := float64(ci * 20)
			x1 := x0 + 20
			cells = append(cells, pdf.TSRCell{X0: x0, Y0: y0, X1: x1, Y1: y1})
			boxes = append(boxes, pdf.TextBox{
				X0: x0, Top: y0, X1: x1, Bottom: y1, Text: txt,
				PageNumber: page, HasPageNumber: true,
			})
		}
	}
	return cells, boxes
}

func vhChRenderOnTwoPages(homeRows, contRows [][]string) string {
	hc, hb := vhChBuildPage(0, homeRows...)
	cc, cb := vhChBuildPage(1, contRows...)
	cells := append(append([]pdf.TSRCell{}, hc...), cc...)
	boxes := append(append([]pdf.TextBox{}, hb...), cb...)
	spans := []pdf.VHPageSpan{
		{Page: 0, CellStart: 0, CellEnd: len(hc), Scale: 1},
		{Page: 1, CellStart: len(hc), CellEnd: len(cells), Scale: 1},
	}
	return ConstructTableVHPaged(cells, boxes, spans, "")
}

var vhChHeader = []string{"序号", "学校名称", "专业代码", "专业名称", "学位授予门类"}

// Happy path: both pages carry the same header; the continuation replica must
// be stripped while the home header and every data row survive.
func TestVHContinuationHeaderStripped(t *testing.T) {
	got := vhChRenderOnTwoPages(
		[][]string{vhChHeader, {"205", "甲", "01", "AA", "工学"}, {"206", "乙", "02", "BB", "理学"}},
		[][]string{vhChHeader, {"230", "丙", "03", "CC", "文学"}, {"231", "丁", "04", "DD", "医学"}},
	)
	rows := vhChRows(got)
	sig := vhChSig(vhChHeader)
	if n := vhChCountRows(rows, sig); n != 1 {
		t.Errorf("header signature %q appeared %d times, want 1; html=%s", sig, n, got)
	}
	for _, want := range []string{"205", "206", "230", "231", "甲", "乙", "丙", "丁"} {
		if !vhChHasCell(rows, want) {
			t.Errorf("data cell %q lost after header strip; html=%s", want, got)
		}
	}
	if len(rows) != 5 {
		t.Errorf("rows=%d, want 5 (1 header + 4 data); html=%s", len(rows), got)
	}
}

// Adversarial: a continuation "header" that is NOT byte-identical to the home
// header must be preserved (avoid deleting genuine data rows).
func TestVHContinuationHeaderDifferingNotStripped(t *testing.T) {
	diffHeader := []string{"序号", "学校名称", "专业代码", "专业名称", "不同门类"}
	got := vhChRenderOnTwoPages(
		[][]string{vhChHeader, {"205", "甲", "01", "AA", "工学"}},
		[][]string{diffHeader, {"230", "丙", "03", "CC", "文学"}},
	)
	rows := vhChRows(got)
	if !vhChHasCell(rows, "不同门类") {
		t.Errorf("differing continuation header row was wrongly stripped; html=%s", got)
	}
	if vhChCountRows(rows, vhChSig(vhChHeader)) != 1 {
		t.Errorf("home header should appear exactly once; html=%s", got)
	}
	if len(rows) != 4 {
		t.Errorf("rows=%d, want 4 (nothing stripped); html=%s", len(rows), got)
	}
}

// Edge: single-page table must behave exactly as before.
func TestVHSinglePageUnchanged(t *testing.T) {
	hc, hb := vhChBuildPage(0, vhChHeader, []string{"205", "甲", "01", "AA", "工学"}, []string{"206", "乙", "02", "BB", "理学"})
	spans := []pdf.VHPageSpan{{Page: 0, CellStart: 0, CellEnd: len(hc), Scale: 1}}
	got := ConstructTableVHPaged(hc, hb, spans, "")
	rows := vhChRows(got)
	if vhChCountRows(rows, vhChSig(vhChHeader)) != 1 {
		t.Errorf("home header should appear once; html=%s", got)
	}
	if len(rows) != 3 {
		t.Errorf("rows=%d, want 3; html=%s", len(rows), got)
	}
}

// Edge: continuation with no repeated header — all its rows must survive.
func TestVHContinuationWithoutRepeatedHeaderUnchanged(t *testing.T) {
	got := vhChRenderOnTwoPages(
		[][]string{vhChHeader, {"205", "甲", "01", "AA", "工学"}},
		[][]string{{"230", "丙", "03", "CC", "文学"}, {"231", "丁", "04", "DD", "医学"}},
	)
	rows := vhChRows(got)
	if len(rows) != 4 {
		t.Errorf("rows=%d, want 4; html=%s", len(rows), got)
	}
	for _, want := range []string{"230", "231", "丙", "丁"} {
		if !vhChHasCell(rows, want) {
			t.Errorf("data %q lost; html=%s", want, got)
		}
	}
}

// Adversarial: home first row is NOT a header (data). A coincidental first-row
// match must not strip a genuine continuation data row. Here first rows differ,
// so nothing should be stripped.
func TestVHHomeWithoutHeaderNoStrip(t *testing.T) {
	got := vhChRenderOnTwoPages(
		[][]string{{"205", "甲", "01", "AA", "工学"}, {"206", "乙", "02", "BB", "理学"}},
		[][]string{{"230", "丙", "03", "CC", "文学"}, {"231", "丁", "04", "DD", "医学"}},
	)
	rows := vhChRows(got)
	if len(rows) != 4 {
		t.Errorf("rows=%d, want 4 (header-less pages untouched); html=%s", len(rows), got)
	}
}

// Adversarial: two rows with DIFFERENT cell boundaries must not be treated as
// the same header just because their raw text concatenations would collide
// (["序号","学校名称"] vs ["序号学校","名称"] both read as "序号学校名称"
// if joined with no separator). The production signature encodes cell
// boundaries with a unit separator, so neither is mistaken for the other.
func TestVHContinuationHeaderSignatureCollisionNotStripped(t *testing.T) {
	got := vhChRenderOnTwoPages(
		[][]string{{"序号", "学校名称"}, {"205", "甲"}},
		[][]string{{"序号学校", "名称"}, {"230", "丙"}},
	)
	rows := vhChRows(got)
	if !vhChHasCell(rows, "序号学校") || !vhChHasCell(rows, "名称") {
		t.Errorf("continuation row with colliding join was wrongly stripped; html=%s", got)
	}
	if len(rows) != 4 {
		t.Errorf("rows=%d, want 4 (collision must not strip); html=%s", len(rows), got)
	}
}

// Edge (handoff §7): an empty first row on the home page is not a header. It
// must not produce a signature that causes a matching empty continuation row
// to be stripped.
func TestVHHomeEmptyHeaderRowNotTreatedAsHeader(t *testing.T) {
	blank := []string{"", "", ""}
	got := vhChRenderOnTwoPages(
		[][]string{blank, {"205", "甲", "01"}},
		[][]string{blank, {"230", "丙", "03"}},
	)
	rows := vhChRows(got)
	if len(rows) != 4 {
		t.Errorf("rows=%d, want 4 (empty home row is not a header; nothing stripped); html=%s", len(rows), got)
	}
	for _, want := range []string{"205", "230", "甲", "丙"} {
		if !vhChHasCell(rows, want) {
			t.Errorf("data %q lost; html=%s", want, got)
		}
	}
}

// Finding 1 (review): header comparison must normalize case and internal
// whitespace, matching tableHeaderOf's normalizeTableHeaderCell. Here the
// continuation header differs only by lowercasing and a doubled space, so it
// is still the SAME header and must be stripped. vhChSig is deliberately
// raw-text (no case folding) so the assertion sees both spellings.
func TestVHContinuationHeaderNormalizedByCaseAndWhitespace(t *testing.T) {
	homeHeader := []string{"序号", "Name", "School  Code"}
	contHeader := []string{"序号", "name", "school code"} // case + collapsed whitespace
	got := vhChRenderOnTwoPages(
		[][]string{homeHeader, {"205", "甲", "01"}},
		[][]string{contHeader, {"230", "丙", "03"}},
	)
	rows := vhChRows(got)
	// Raw exact-text count: the continuation spelling must be gone (stripped),
	// the home spelling must remain exactly once.
	if n := vhChCountRows(rows, vhChSig(contHeader)); n != 0 {
		t.Errorf("continuation header (normalized-equal) not stripped; appeared %d times; html=%s", n, got)
	}
	if n := vhChCountRows(rows, vhChSig(homeHeader)); n != 1 {
		t.Errorf("home header should remain once; got %d; html=%s", n, got)
	}
	if !vhChHasCell(rows, "230") || !vhChHasCell(rows, "丙") {
		t.Errorf("continuation data lost; html=%s", got)
	}
}

// Finding 1 adversarial: a header differing in MEANINGFUL text (not just
// case/whitespace) must still be preserved — normalization must not become so
// aggressive that it erases a genuine data row.
func TestVHNormalizationDoesNotStripMeaningfullyDifferentHeader(t *testing.T) {
	homeHeader := []string{"序号", "Name"}
	contHeader := []string{"序号", "Address"} // genuinely different
	got := vhChRenderOnTwoPages(
		[][]string{homeHeader, {"205", "甲"}},
		[][]string{contHeader, {"230", "丙"}},
	)
	rows := vhChRows(got)
	if !vhChHasCell(rows, "Address") {
		t.Errorf("meaningfully different row wrongly stripped; html=%s", got)
	}
	if len(rows) != 4 {
		t.Errorf("rows=%d, want 4; html=%s", len(rows), got)
	}
}

// Finding 2 (review): a two-row header repeated on the continuation page must
// be stripped in full. Current code strips only one row, leaving a duplicate.
func TestVHTwoRowContinuationHeaderFullyStripped(t *testing.T) {
	h1 := []string{"序号", "学校名称"}
	h2 := []string{"代码", "专业名称"}
	got := vhChRenderOnTwoPages(
		[][]string{h1, h2, {"205", "甲"}, {"206", "乙"}},
		[][]string{h1, h2, {"230", "丙"}, {"231", "丁"}},
	)
	rows := vhChRows(got)
	if n := vhChCountRows(rows, vhChSig(h1)); n != 1 {
		t.Errorf("header row 1 appeared %d times, want 1; html=%s", n, got)
	}
	if n := vhChCountRows(rows, vhChSig(h2)); n != 1 {
		t.Errorf("header row 2 appeared %d times, want 1; html=%s", n, got)
	}
	for _, want := range []string{"205", "206", "230", "231"} {
		if !vhChHasCell(rows, want) {
			t.Errorf("data %q lost; html=%s", want, got)
		}
	}
	if len(rows) != 6 {
		t.Errorf("rows=%d, want 6 (1+1 header + 4 data); html=%s", len(rows), got)
	}
}

// Finding 2 adversarial: when the continuation's second row is genuinely
// different from the home's second header row, only the matching leading row
// may be stripped — the different row must be preserved as data.
func TestVHTwoRowHeaderPartialMatchOnlyStripsMatching(t *testing.T) {
	h1 := []string{"序号", "学校名称"}
	h2 := []string{"代码", "专业名称"}
	diff := []string{"代码", "不同列"}
	got := vhChRenderOnTwoPages(
		[][]string{h1, h2, {"205", "甲"}},
		[][]string{h1, diff, {"230", "丙"}},
	)
	rows := vhChRows(got)
	if n := vhChCountRows(rows, vhChSig(h1)); n != 1 {
		t.Errorf("matching header row should appear once; got %d; html=%s", n, got)
	}
	if !vhChHasCell(rows, "不同列") {
		t.Errorf("differing second row wrongly stripped; html=%s", got)
	}
	if len(rows) != 5 {
		t.Errorf("rows=%d, want 5 (strip only 1 leading row); html=%s", len(rows), got)
	}
}
