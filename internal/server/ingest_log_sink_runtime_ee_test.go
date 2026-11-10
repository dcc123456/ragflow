//go:build ee

package server

import (
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"ragflow/internal/common"
	"ragflow/internal/server/config"
)

func configWithIngestLogType(t *testing.T, typ string) *config.Config {
	t.Helper()
	c := &config.Config{}
	v := viper.New()
	v.SetConfigType("yaml")
	const tmpl = `ingestion_log:
  type: "%s"`
	yaml := fmt.Sprintf(tmpl, typ)
	if err := v.ReadConfig(strings.NewReader(yaml)); err != nil {
		t.Fatalf("read config: %v", err)
	}
	if err := c.ParseIngestLogConfig(v); err != nil {
		t.Fatalf("ParseIngestLogConfig: %v", err)
	}
	if got := c.IngestLogType(); got != typ {
		t.Fatalf("fixture broken: IngestLogType = %q, want %q", got, typ)
	}
	return c
}

// TestClickHouseCaseIsSafeWithoutDriver: in the unit tier no ClickHouse driver is
// initialized, so the clickhouse case must stay a safe no-op (no panic, no error,
// and it must NOT replace the already-installed sink). The actual sink wiring for
// a live driver is covered by the clickhouse-package test TestRegisterIngestLogWiresSink
// plus the AST test proving the switch calls RegisterIngestLog.
func TestClickHouseCaseIsSafeWithoutDriver(t *testing.T) {
	clicked := false
	common.SetIngestLogSink(func(common.IngestionLog) error { clicked = true; return nil })
	defer common.SetIngestLogSink(nil)
	cfg := configWithIngestLogType(t, "clickhouse")
	if err := wireIngestLogSink(cfg, "api-server"); err != nil {
		t.Fatalf("wireIngestLogSink(clickhouse) returned error: %v", err)
	}
	common.LogIngest(&common.IngestionLog{TaskID: "probe"})
	if !clicked {
		t.Fatal("clickhouse case replaced the installed sink before the driver was up")
	}
}

// Adversarial: every unhandled type must be a no-op - the previously installed
// sink is preserved, no error, no panic. Includes the downstream-planned "file"
// type, a case mismatch, Unicode, and empty.
func TestUnhandledTypesPreserveSink(t *testing.T) {
	for _, typ := range []string{"file", "database", "none", "", "CLICKHOUSE", "未知类型"} {
		t.Run("type="+typ, func(t *testing.T) {
			clicked := false
			common.SetIngestLogSink(func(common.IngestionLog) error { clicked = true; return nil })
			defer common.SetIngestLogSink(nil)
			cfg := configWithIngestLogType(t, typ)
			if err := wireIngestLogSink(cfg, "svc"); err != nil {
				t.Fatalf("unhandled type %q must not error: %v", typ, err)
			}
			common.LogIngest(&common.IngestionLog{TaskID: "probe"})
			if !clicked {
				t.Fatalf("unhandled type %q must not replace the installed sink", typ)
			}
		})
	}
}

func TestNilConfigIsNoOp(t *testing.T) {
	clicked := false
	common.SetIngestLogSink(func(common.IngestionLog) error { clicked = true; return nil })
	defer common.SetIngestLogSink(nil)
	if err := wireIngestLogSink(nil, "svc"); err != nil {
		t.Fatalf("nil cfg must not error: %v", err)
	}
	common.LogIngest(&common.IngestionLog{TaskID: "probe"})
	if !clicked {
		t.Fatal("nil cfg must not replace the installed sink")
	}
}
