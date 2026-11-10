//go:build ee

package server

import (
	"os"
	"strings"
	"testing"
)

const sinkFileName = "ingest_log_sink_ee.go"

func readSinkSource(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile(sinkFileName)
	if err != nil {
		t.Fatalf("%s must exist: %v", sinkFileName, err)
	}
	return string(src)
}

// The dispatch must be a compile-time switch, not a registry map.
func TestIngestLogSinkUsesCompileTimeSwitch(t *testing.T) {
	src := readSinkSource(t)
	for _, want := range []string{
		"func wireIngestLogSink",
		"switch",
		`case "clickhouse":`,
		"default:",
		"common.Warn",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("%s must contain %q", sinkFileName, want)
		}
	}
	for _, banned := range []string{
		"ingestLogSinkInstallers",
		"dispatchIngestLogSink",
		"var ingestLogSinkInstaller",
	} {
		if strings.Contains(src, banned) {
			t.Fatalf("%s must not contain %q (the switch replaces the registry)", sinkFileName, banned)
		}
	}
}

// The warn must live in the default (unhandled-type) branch.
func TestWarnLivesInDefaultBranch(t *testing.T) {
	src := readSinkSource(t)
	def := strings.Index(src, "default:")
	warn := strings.Index(src, "common.Warn")
	if def < 0 || warn < 0 || warn < def {
		t.Fatal("common.Warn must be emitted from the default (unhandled-type) branch")
	}
}

func TestServerEEStillCallsWireOnce(t *testing.T) {
	src, err := os.ReadFile("server_ee.go")
	if err != nil {
		t.Fatalf("read server_ee.go: %v", err)
	}
	s := string(src)
	if !strings.Contains(s, "wireIngestLogSink(config, serverName)") {
		t.Fatal("StartServer must call wireIngestLogSink(config, serverName)")
	}
	if strings.Contains(s, "IngestLogEnabled()") {
		t.Fatal("the old IngestLogEnabled() toggle must be gone from server_ee.go")
	}
	if n := strings.Count(s, "wireIngestLogSink("); n != 1 {
		t.Fatalf("StartServer must call wireIngestLogSink exactly once, got %d", n)
	}
}
