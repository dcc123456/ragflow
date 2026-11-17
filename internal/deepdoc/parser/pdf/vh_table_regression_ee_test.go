//go:build cgo && ee && manual

package pdf

import (
	"context"
	"html"
	"os"
	"regexp"
	"strings"
	"testing"

	"ragflow/internal/deepdoc/client"
	pdftype "ragflow/internal/deepdoc/parser/pdf/type"
)

var (
	vhTrRe   = regexp.MustCompile(`(?s)<tr[^>]*>(.*?)</tr>`)
	vhTdRe   = regexp.MustCompile(`(?s)<t[dh][^>]*>(.*?)</t[dh]>`)
	vhCodeRe = regexp.MustCompile(`\d{6}[A-Za-z]?`)
	vhHanRe  = regexp.MustCompile(`[\p{Han}]{2,}`)
)

func vhPlain(s string) string {
	return strings.TrimSpace(html.UnescapeString(vhTdRe.ReplaceAllString(s, "$1")))
}

func vhAllDigits(fields []string) bool {
	if len(fields) == 0 {
		return false
	}
	for _, f := range fields {
		for _, r := range f {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

func vhUniq(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	if len(out) > 10 {
		return out[:10]
	}
	return out
}

// parseVHRealPDF parses the user's PDF once through the full production path.
func parseVHRealPDF(t *testing.T) *pdftype.ParseResult {
	t.Helper()
	pdfPath := os.Getenv("VH_PDF")
	if pdfPath == "" {
		pdfPath = "/home/shenyushi/Downloads/学士学位授权名单.pdf"
	}
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		t.Fatalf("read pdf: %v", err)
	}
	eeURL := os.Getenv("DEEPDOC_CPU_EE_URL")
	if eeURL == "" {
		eeURL = "http://127.0.0.1:8001"
	}
	analyzer := client.New(eeURL)
	if !analyzer.Health() {
		t.Fatalf("deepdoc not healthy at %s", eeURL)
	}
	SetPageConcurrency(1)
	res, err := NewParser(pdftype.DefaultParserConfig()).Parse(context.Background(), data, analyzer)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return res
}

// TestVHNoMergedSequenceCells guards bug 1: a cross-page continuation table
// must not have two different page's rows collapsed into one cell (e.g. the
// sequence numbers 101 and 127 appearing together as "101 127"), and the
// next page's header row must not be appended into the same table as a data
// row (e.g. "序号 序号").
func TestVHNoMergedSequenceCells(t *testing.T) {
	res := parseVHRealPDF(t)
	defer res.Close()

	var bad []string
	for _, sec := range res.Sections {
		if sec.LayoutType != "table" && sec.DocTypeKwd != "table" {
			continue
		}
		for _, rm := range vhTrRe.FindAllStringSubmatch(sec.Text, -1) {
			cells := vhTdRe.FindAllStringSubmatch(rm[1], -1)
			if len(cells) == 0 {
				continue
			}
			first := vhPlain(cells[0][1])
			if strings.Contains(first, "序号 序号") || strings.Contains(first, "序号序号") {
				bad = append(bad, "duplicated header: "+first)
			}
			if vhAllDigits(strings.Fields(first)) && len(strings.Fields(first)) >= 2 {
				bad = append(bad, "two seq numbers in one cell: "+first)
			}
		}
	}
	if len(bad) > 0 {
		t.Errorf("cross-page table row collapse detected: %v", vhUniq(bad))
	}
}

// TestVHNoCrossColumnMergedText guards bug 2: two adjacent columns
// (专业代码 / 专业名称) must not have their text merged into a single cell.
// A healthy cell holds either a Chinese name OR a 6-digit major code, never
// both (the pathological sample is "应急管理 120111T").
func TestVHNoCrossColumnMergedText(t *testing.T) {
	res := parseVHRealPDF(t)
	defer res.Close()

	var bad []string
	for _, sec := range res.Sections {
		if sec.LayoutType != "table" && sec.DocTypeKwd != "table" {
			continue
		}
		for _, cm := range vhTdRe.FindAllStringSubmatch(sec.Text, -1) {
			txt := vhPlain(cm[1])
			if vhHanRe.MatchString(txt) && vhCodeRe.MatchString(txt) {
				bad = append(bad, txt)
			}
		}
	}
	if len(bad) > 0 {
		t.Errorf("adjacent-column text merged into one cell: %v", vhUniq(bad))
	}
}
