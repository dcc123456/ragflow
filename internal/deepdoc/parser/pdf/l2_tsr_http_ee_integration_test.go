//go:build cgo && integration

package pdf

// L2 end-to-end verification of the EE TSR (Table Structure Recognition)
// feature.
//
// The EE DeepDoc backend is a standalone service (ragflow_server --deepdoc).
// The pdf parser reaches it through an HTTP DocAnalyzer (deepdoc/client):
// DLA/TSR/OCR requests are POSTed to /predict/dla, /predict/tsr, /predict/ocr_det,
// /predict/ocr_rec. The TSR endpoint answers with native.RunTSREE -- the EE
// 2-class model that loads tsr.ee.cpu.ort directly. It never touches tsr.ort.
//
// This test boots that exact service in-process, points the parser's analyzer
// at it, parses a real table PDF, and asserts the assembled table HTML carries
// a merged cell (colspan/rowspan). If TSR had not run through the EE path, the
// table would collapse to a plain grid with no spans -- so the assertion closes
// the loop opened by L0 (/predict/tsr returns boxes) and L1 (RunTSREE == Python
// EE). Requires EE_MODEL_DIR (EE weights) and BATCH_PARITY_PDF_DIR (real PDFs).

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/deepdoc/client"
	"ragflow/internal/deepdoc/native"
	pdf "ragflow/internal/deepdoc/parser/pdf/type"
	deepdocserver "ragflow/internal/deepdoc/server"
)

// startEEDeepDocServer boots the EE DeepDoc inference service in-process and
// returns its base URL plus a stop func. It calls native.InitCPUEE so the
// server validates EE_MODEL_DIR against the *.ee.cpu.ort manifest and serves
// the EE DLA/TSR/det/rec weights (RunTSREE loads tsr.ee.cpu.ort directly).
func startEEDeepDocServer(t *testing.T) (string, func()) {
	t.Helper()
	modelDir := os.Getenv("EE_MODEL_DIR")
	if modelDir == "" {
		t.Skip("EE_MODEL_DIR required (EE DeepDoc server integration)")
	}
	if err := native.InitORT(); err != nil {
		t.Fatalf("InitORT: %v", err)
	}
	native.InitCPUEE()

	port := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- deepdocserver.Run(ctx, port) }()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	if !waitHealth(t, baseURL) {
		cancel()
		select {
		case err := <-runErr:
			t.Fatalf("EE DeepDoc server failed to become ready: %v", err)
		default:
			t.Fatal("EE DeepDoc server /health not ready (no Run error captured)")
		}
	}
	return baseURL, cancel
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func waitHealth(t *testing.T, baseURL string) bool {
	t.Helper()
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

// TestL2EETableParseViaHTTP parses a real table PDF (1.pdf, whose header spans
// 6 of 8 columns in the Python golden) through the EE HTTP analyzer and asserts
// the resulting table HTML carries a merged cell. This proves the EE 2-class
// TSR was exercised end-to-end via /predict/tsr -> RunTSREE.
func TestL2EETableParseViaHTTP(t *testing.T) {
	baseURL, stop := startEEDeepDocServer(t)
	defer stop()

	eeAnalyzer := client.New(baseURL)
	if !eeAnalyzer.Health() {
		t.Fatal("EE client reports unhealthy")
	}

	pdfDir := common.GetEnv("BATCH_PARITY_PDF_DIR")
	if pdfDir == "" {
		pdfDir = "testdata/real_pdfs"
	}
	data, err := os.ReadFile(filepath.Join(pdfDir, "1.pdf"))
	if err != nil {
		t.Skipf("1.pdf not found in %s (set BATCH_PARITY_PDF_DIR to a dir containing 1.pdf)", pdfDir)
	}

	cfg := pdf.DefaultParserConfig()
	p := NewParser(cfg)
	result, err := p.Parse(t.Context(), data, eeAnalyzer)
	if err != nil {
		t.Fatalf("Parse 1.pdf via EE: %v", err)
	}
	if len(result.Tables) == 0 {
		t.Fatal("1.pdf: no tables detected through EE path")
	}

	var allHTML strings.Builder
	for _, s := range result.Sections {
		allHTML.WriteString(s.Text)
	}
	html := allHTML.String()
	hasColspan := strings.Contains(html, "colspan")
	hasRowspan := strings.Contains(html, "rowspan")
	if !hasColspan && !hasRowspan {
		t.Errorf("1.pdf via EE: table HTML has no merged cells (colspan/rowspan). "+
			"TSR likely did not run through the EE /predict/tsr endpoint.\nHTML excerpt:\n%s",
			l2htmlExcerpt(html, 600))
	}
	t.Logf("1.pdf via EE: %d tables; merged cells present: colspan=%v rowspan=%v",
		len(result.Tables), hasColspan, hasRowspan)
}

func l2htmlExcerpt(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}
