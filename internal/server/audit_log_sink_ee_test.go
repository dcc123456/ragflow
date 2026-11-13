//go:build ee

package server

import (
	"strings"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/engine/clickhouse"
	"ragflow/internal/server/config"

	"github.com/spf13/viper"
)

func TestWireAuditLogSink(t *testing.T) {
	if clickhouse.GetDriver() != nil {
		t.Fatal("unit test requires an uninitialized ClickHouse driver")
	}
	for _, test := range []struct {
		name, typ, errorText string
		nilConfig            bool
	}{
		{name: "file preserves sink", typ: "file"},
		{name: "database requires driver", typ: "database", errorText: "ClickHouse driver is not initialized"},
		{name: "unsupported type", typ: "invalid", errorText: `unsupported audit.type "invalid"`},
		{name: "nil config", nilConfig: true, errorText: "audit configuration is nil"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var cfg *config.Config
			if !test.nilConfig {
				cfg = &config.Config{}
				v := viper.New()
				v.Set("audit.type", test.typ)
				if err := cfg.ParseAuditConfig(v); err != nil {
					t.Fatal(err)
				}
			}
			var captured *common.OperationLog
			common.SetAuditLogSink(func(event *common.OperationLog) error { captured = event; return nil })
			t.Cleanup(func() { common.SetAuditLogSink(nil) })
			err := wireAuditLogSink(cfg)
			if test.errorText == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.errorText) {
				t.Fatalf("error=%v, want %q", err, test.errorText)
			}
			event := &common.OperationLog{Operation: "audit_probe"}
			common.Audit(event)
			if captured != event {
				t.Fatal("audit wiring replaced an existing sink")
			}
		})
	}
}
